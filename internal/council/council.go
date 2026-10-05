// Package council turns a braindump into a brief. One proposer drafts it,
// critics run in parallel and answer with structured JSON, and the proposer
// revises. At most two rounds; the council stops early when no critic
// reports a blocker. The brief lands in Memory as a draft page, and approving
// it adds a card to the work board.
//
// Every model call goes through agentexec with no tools, on the user's own
// signed-in CLIs. A confidential project, or a braindump that links a
// confidential Memory page, is refused before any provider is called.
package council

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The system prompts, versioned with the code in prompts/.
var (
	//go:embed prompts/propose.md
	ProposePrompt string
	//go:embed prompts/critique.md
	CritiquePrompt string
	//go:embed prompts/synthesise.md
	SynthesisePrompt string
)

// Errors returned by the Service. The HTTP layer maps each to a status.
var (
	ErrBadRequest   = errors.New("bad request")
	ErrConfidential = errors.New("confidential")
	ErrNotFound     = errors.New("council session not found")
	ErrBusy         = errors.New("the council is still running")
	ErrApproved     = errors.New("the brief is already approved")
)

// Session statuses.
const (
	StatusRunning  = "running"
	StatusDraft    = "draft" // the brief is written and waits for approval
	StatusApproved = "approved"
	StatusFailed   = "failed"
)

// Stages of a running session, in order.
const (
	StagePropose    = "propose"
	StageCritique   = "critique"
	StageSynthesise = "synthesise"
	StageWrite      = "write"
	StageDone       = "done"
)

// Step roles.
const (
	RolePropose      = "propose"
	RoleCritique     = "critique"
	RoleSelfCritique = "self-critique"
	RoleSynthesise   = "synthesise"
)

// Modes.
const (
	ModeCouncil      = "council"       // a proposer and at least one other critic
	ModeSelfCritique = "self-critique" // one provider critiques its own draft
)

// Critique verdicts and point severities.
const (
	VerdictOK       = "ok"
	VerdictConcerns = "concerns"
	VerdictBlocker  = "blocker"

	SeverityBlocker = "blocker"
	SeverityMajor   = "major"
	SeverityMinor   = "minor"
)

// Defaults for a run.
var (
	DefaultProposer = "claude"
	DefaultCritics  = []string{"codex", "grok"}
)

// MaxRounds is the most critique rounds a run makes on its own.
const MaxRounds = 2

// MaxInput is the longest braindump or note accepted, in bytes.
const MaxInput = 20000

// DefaultTimeout bounds one model call.
const DefaultTimeout = 5 * time.Minute

// Point is one thing a critic wants changed.
type Point struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

// Step is one model call: the proposal, a critique or a synthesis.
type Step struct {
	Provider string           `json:"provider"`
	Role     string           `json:"role"`
	Running  bool             `json:"running"`
	Text     string           `json:"text,omitempty"`    // the draft, or a critique's raw answer when it could not be read
	Verdict  string           `json:"verdict,omitempty"` // critiques only
	Points   []Point          `json:"points,omitempty"`  // critiques only
	Skipped  bool             `json:"skipped,omitempty"` // the CLI is missing or not signed in
	Error    string           `json:"error,omitempty"`
	Usage    *agentexec.Usage `json:"usage,omitempty"`
	Started  time.Time        `json:"started"`
	Ended    time.Time        `json:"ended"`
}

// Round is one critique pass and the revision that follows it. Round 1 also
// holds the first draft.
type Round struct {
	N         int     `json:"n"`
	Notes     string  `json:"notes,omitempty"` // the user's notes, when they asked again
	Proposal  *Step   `json:"proposal,omitempty"`
	Critiques []*Step `json:"critiques"`
	Synthesis *Step   `json:"synthesis,omitempty"`
}

// Event is one line of a session's progress log.
type Event struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"` // stage | thinking | critique | skipped | round | done | error
	Round    int       `json:"round,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Text     string    `json:"text"`
}

// Session is one council run, persisted as <dir>/<id>.json.
type Session struct {
	ID           string            `json:"id"`
	Input        string            `json:"input"`
	Project      string            `json:"project,omitempty"`
	Title        string            `json:"title,omitempty"`
	Proposer     string            `json:"proposer"`
	Critics      []string          `json:"critics"` // the critics still taking part
	Mode         string            `json:"mode"`
	MaxRounds    int               `json:"max_rounds"`
	Rounds       []Round           `json:"rounds"`
	StoppedEarly bool              `json:"stopped_early"`
	Stage        string            `json:"stage"`
	Thinking     []string          `json:"thinking"` // providers with a call in flight
	Notes        []string          `json:"notes"`    // skipped providers and fallbacks
	Warnings     []string          `json:"warnings"` // the brief does not follow the format
	Log          []Event           `json:"log"`
	Brief        string            `json:"brief,omitempty"`
	BriefPath    string            `json:"brief_path,omitempty"`
	Status       string            `json:"status"`
	Error        string            `json:"error,omitempty"`
	Card         *boards.Card      `json:"card,omitempty"`
	Usage        []agentexec.Usage `json:"usage"`
	Prompts      map[string]string `json:"prompts"` // prompt name -> version
	Created      time.Time         `json:"created"`
	Updated      time.Time         `json:"updated"`
}

// StartRequest is the body of POST /api/council/sessions.
type StartRequest struct {
	Input    string   `json:"input"`
	Project  string   `json:"project,omitempty"`
	Proposer string   `json:"proposer,omitempty"`
	Critics  []string `json:"critics,omitempty"` // nil = the defaults; [] = self-critique
	Rounds   int      `json:"rounds,omitempty"`  // 1 or 2; 0 = 2
}

// Service runs and stores council sessions.
type Service struct {
	Dir      string                         // where session records live
	Vault    memory.Opener                  // where briefs are written
	Projects func() (*projects.List, error) // projects.yaml; nil = projects.Load
	Runner   *agentexec.Runner              // nil = the default runner
	Timeout  time.Duration                  // per model call; 0 = DefaultTimeout
	Now      func() time.Time

	mu      sync.Mutex
	live    map[string]*Session // sessions with a run in flight
	subs    map[string]map[chan update]struct{}
	writeMu sync.Mutex // serialises brief writes and approvals
}

// New returns a Service storing sessions in dir.
func New(dir string, vault memory.Opener, runner *agentexec.Runner) *Service {
	return &Service{Dir: dir, Vault: vault, Runner: runner}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) runner() *agentexec.Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return &agentexec.Runner{}
}

func (s *Service) lookPath(name string) error {
	look := s.runner().LookPath
	if look == nil {
		look = exec.LookPath
	}
	_, err := look(name)
	return err
}

func (s *Service) loadProjects() (*projects.List, error) {
	if s.Projects != nil {
		return s.Projects()
	}
	return projects.Load()
}

var providerSet = map[string]bool{"claude": true, "codex": true, "grok": true}

// promptVersion reads "<!-- name vN -->" from the first line of a prompt.
var promptVersionRE = regexp.MustCompile(`^<!--\s*\S+\s+(v\d+)\s*-->`)

func promptVersion(p string) string {
	if m := promptVersionRE.FindStringSubmatch(p); m != nil {
		return m[1]
	}
	return "unversioned"
}

// systemPrompt drops the version comment the model does not need.
func systemPrompt(p string) string {
	if i := strings.Index(p, "-->"); i >= 0 && strings.HasPrefix(p, "<!--") {
		return strings.TrimLeft(p[i+3:], "\r\n")
	}
	return p
}

func newID(now time.Time) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return now.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// idRE is what a session id may look like; anything else never reaches the
// file system.
var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// prepare validates a request and builds a new session. It refuses
// confidential input before anything is stored or any provider is called.
func (s *Service) prepare(in StartRequest) (*Session, error) {
	in.Input = strings.TrimSpace(in.Input)
	if in.Input == "" {
		return nil, fmt.Errorf("%w: write a braindump first", ErrBadRequest)
	}
	if len(in.Input) > MaxInput {
		return nil, fmt.Errorf("%w: the braindump is longer than %d characters", ErrBadRequest, MaxInput)
	}
	if in.Proposer == "" {
		in.Proposer = DefaultProposer
	}
	if !providerSet[in.Proposer] {
		return nil, fmt.Errorf("%w: proposer must be claude, codex or grok", ErrBadRequest)
	}
	critics := in.Critics
	if critics == nil {
		critics = DefaultCritics
	}
	var cs []string
	seen := map[string]bool{in.Proposer: true}
	for _, c := range critics {
		if !providerSet[c] {
			return nil, fmt.Errorf("%w: critic %q must be claude, codex or grok", ErrBadRequest, c)
		}
		if !seen[c] {
			seen[c] = true
			cs = append(cs, c)
		}
	}
	rounds := in.Rounds
	if rounds == 0 {
		rounds = MaxRounds
	}
	if rounds < 1 || rounds > MaxRounds {
		return nil, fmt.Errorf("%w: rounds must be 1 or 2", ErrBadRequest)
	}
	in.Project = strings.TrimSpace(in.Project)
	if err := s.checkConfidential(in.Project, in.Input); err != nil {
		return nil, err
	}
	now := s.now()
	sess := &Session{
		ID: newID(now), Input: in.Input, Project: in.Project, Proposer: in.Proposer, Critics: cs,
		Mode: ModeCouncil, MaxRounds: rounds, Stage: StagePropose, Status: StatusRunning,
		Thinking: []string{}, Notes: []string{}, Warnings: []string{}, Log: []Event{}, Rounds: []Round{}, Usage: []agentexec.Usage{},
		Prompts: map[string]string{
			"propose":    promptVersion(ProposePrompt),
			"critique":   promptVersion(CritiquePrompt),
			"synthesise": promptVersion(SynthesisePrompt),
		},
		Created: now, Updated: now,
	}
	if len(cs) == 0 {
		sess.Mode = ModeSelfCritique
	}
	return sess, nil
}

// Start runs a whole council and returns the finished session. onUpdate, when
// set, gets a copy of the session after every change.
func (s *Service) Start(ctx context.Context, in StartRequest, onUpdate func(Session)) (*Session, error) {
	sess, err := s.prepare(in)
	if err != nil {
		return nil, err
	}
	if err := s.register(sess); err != nil {
		return nil, err
	}
	s.run(ctx, sess, onUpdate)
	return s.Get(sess.ID)
}

// Begin starts a council in the background and returns its first state.
func (s *Service) Begin(in StartRequest) (*Session, error) {
	sess, err := s.prepare(in)
	if err != nil {
		return nil, err
	}
	if err := s.register(sess); err != nil {
		return nil, err
	}
	snap := clone(sess)
	go s.run(context.Background(), sess, nil)
	return snap, nil
}

// AskAgain runs one more round on a draft brief with the user's notes: the
// critics read the brief and the notes, and the proposer revises it.
func (s *Service) AskAgain(ctx context.Context, id, notes string, onUpdate func(Session)) (*Session, error) {
	sess, err := s.claimAgain(id, notes)
	if err != nil {
		return nil, err
	}
	s.again(ctx, sess, strings.TrimSpace(notes), onUpdate)
	return s.Get(id)
}

// BeginAgain is AskAgain in the background.
func (s *Service) BeginAgain(id, notes string) (*Session, error) {
	sess, err := s.claimAgain(id, notes)
	if err != nil {
		return nil, err
	}
	snap := clone(sess)
	go s.again(context.Background(), sess, strings.TrimSpace(notes), nil)
	return snap, nil
}

func (s *Service) claimAgain(id, notes string) (*Session, error) {
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return nil, fmt.Errorf("%w: say what the council should change", ErrBadRequest)
	}
	if len(notes) > MaxInput {
		return nil, fmt.Errorf("%w: the notes are longer than %d characters", ErrBadRequest, MaxInput)
	}
	sess, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	switch sess.Status {
	case StatusRunning:
		return nil, ErrBusy
	case StatusApproved:
		return nil, ErrApproved
	}
	if sess.Brief == "" {
		return nil, fmt.Errorf("%w: this session has no brief to revise", ErrBadRequest)
	}
	if err := s.checkConfidential(sess.Project, sess.Input, notes); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live[id] != nil {
		return nil, ErrBusy
	}
	sess.Status, sess.Stage, sess.Error = StatusRunning, StageCritique, ""
	if s.live == nil {
		s.live = map[string]*Session{}
	}
	s.live[id] = sess
	return sess, nil
}

// register stores a new session and marks it live.
func (s *Service) register(sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.live == nil {
		s.live = map[string]*Session{}
	}
	s.live[sess.ID] = sess
	if err := s.persistLocked(sess); err != nil {
		delete(s.live, sess.ID)
		return err
	}
	return nil
}

// change applies fn to the session under the lock, then saves and publishes
// the result.
func (s *Service) change(sess *Session, onUpdate func(Session), fn func()) {
	s.mu.Lock()
	fn()
	sess.Updated = s.now()
	_ = s.persistLocked(sess)
	snap := clone(sess)
	s.publishLocked(snap)
	s.mu.Unlock()
	if onUpdate != nil {
		onUpdate(*snap)
	}
}

func (s *Service) logLocked(sess *Session, kind string, round int, provider, text string) {
	sess.Log = append(sess.Log, Event{Time: s.now(), Kind: kind, Round: round, Provider: provider, Text: text})
}

func remove(list []string, v string) []string {
	out := []string{}
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// skippable reports whether err means "this provider cannot take part"
// rather than "this call went wrong".
func skippable(err error) bool {
	return errors.Is(err, agentexec.ErrCLIMissing) || errors.Is(err, agentexec.ErrNotSignedIn)
}

func skipNote(provider string, err error) string {
	if errors.Is(err, agentexec.ErrCLIMissing) {
		return provider + " is not installed, so it was skipped"
	}
	return provider + " is not signed in, so it was skipped"
}

// call runs one model call and records it in step.
func (s *Service) call(ctx context.Context, sess *Session, onUpdate func(Session), step *Step, system, prompt string) error {
	s.change(sess, onUpdate, func() {
		step.Running, step.Started = true, s.now()
		sess.Thinking = append(sess.Thinking, step.Provider)
		s.logLocked(sess, "thinking", 0, step.Provider, step.Provider+" is thinking ("+step.Role+")")
	})
	timeout := s.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	res, err := s.runner().Run(ctx, agentexec.Request{
		Provider: step.Provider, SystemPrompt: systemPrompt(system), Prompt: prompt,
		Tools: agentexec.ToolsNone, Timeout: timeout,
	})
	s.change(sess, onUpdate, func() {
		step.Running, step.Ended = false, s.now()
		sess.Thinking = remove(sess.Thinking, step.Provider)
		if res != nil {
			u := res.Usage
			step.Usage = &u
			if res.Usage.DurationMS > 0 || res.Usage.InputTokens > 0 || res.Usage.OutputTokens > 0 {
				sess.Usage = append(sess.Usage, u)
			}
		}
		switch {
		case err != nil && skippable(err):
			step.Skipped, step.Error = true, err.Error()
		case err != nil:
			step.Error = err.Error()
		default:
			step.Text = strings.TrimSpace(res.Text)
		}
	})
	return err
}

// providerUsable checks the CLI is on PATH, noting it when it is not.
func (s *Service) providerUsable(sess *Session, onUpdate func(Session), p string) bool {
	if err := s.lookPath(p); err != nil {
		s.change(sess, onUpdate, func() {
			sess.Notes = append(sess.Notes, p+" is not installed, so it was skipped")
			s.logLocked(sess, "skipped", 0, p, p+" is not installed; skipped")
		})
		return false
	}
	return true
}

// fail ends a run with an error.
func (s *Service) fail(sess *Session, onUpdate func(Session), msg string) {
	s.change(sess, onUpdate, func() {
		sess.Status, sess.Stage, sess.Error = StatusFailed, StageDone, msg
		sess.Thinking = []string{}
		s.logLocked(sess, "error", 0, "", msg)
	})
}

func (s *Service) finishLive(id string) {
	s.mu.Lock()
	delete(s.live, id)
	s.closeSubsLocked(id)
	s.mu.Unlock()
}

// run is the whole council: propose, then up to MaxRounds of critique and
// synthesis, then the brief is written to Memory.
func (s *Service) run(ctx context.Context, sess *Session, onUpdate func(Session)) {
	defer s.finishLive(sess.ID)

	// Who can take part at all.
	var critics []string
	for _, c := range sess.Critics {
		if s.providerUsable(sess, onUpdate, c) {
			critics = append(critics, c)
		}
	}
	proposer := sess.Proposer
	if !s.providerUsable(sess, onUpdate, proposer) {
		proposer = ""
	}
	project := s.projectContext(sess.Project)

	// Propose. A proposer that cannot take part hands over to a critic.
	s.change(sess, onUpdate, func() {
		sess.Rounds = append(sess.Rounds, Round{N: 1, Critiques: []*Step{}})
		s.logLocked(sess, "stage", 1, "", "Drafting the brief")
	})
	var draft string
	for {
		if proposer == "" {
			if len(critics) == 0 {
				s.fail(sess, onUpdate, "No provider can take part: install and sign in to claude, codex or grok, then try again.")
				return
			}
			proposer, critics = critics[0], critics[1:]
			s.change(sess, onUpdate, func() {
				sess.Notes = append(sess.Notes, proposer+" proposes instead")
			})
		}
		step := &Step{Provider: proposer, Role: RolePropose}
		s.change(sess, onUpdate, func() {
			sess.Proposer, sess.Critics = proposer, append([]string{}, critics...)
			sess.Rounds[0].Proposal = step
		})
		err := s.call(ctx, sess, onUpdate, step, ProposePrompt, proposePrompt(sess.Input, project))
		if err == nil {
			draft = cleanBrief(step.Text)
			if draft != "" {
				break
			}
			s.fail(sess, onUpdate, proposer+" returned an empty draft")
			return
		}
		if !skippable(err) {
			s.fail(sess, onUpdate, err.Error())
			return
		}
		s.change(sess, onUpdate, func() {
			sess.Notes = append(sess.Notes, skipNote(proposer, err))
			s.logLocked(sess, "skipped", 1, proposer, skipNote(proposer, err))
		})
		proposer = ""
	}

	for r := 1; r <= sess.MaxRounds; r++ {
		if r > 1 {
			s.change(sess, onUpdate, func() {
				sess.Rounds = append(sess.Rounds, Round{N: r, Critiques: []*Step{}})
				s.logLocked(sess, "round", r, "", fmt.Sprintf("Round %d started: a critic reported a blocker", r))
			})
		}
		revised, blocker, err := s.round(ctx, sess, onUpdate, r-1, draft, project, "")
		if err != nil {
			s.fail(sess, onUpdate, err.Error())
			return
		}
		draft = revised
		if !blocker {
			s.change(sess, onUpdate, func() {
				sess.StoppedEarly = r < sess.MaxRounds
				if sess.StoppedEarly {
					s.logLocked(sess, "stage", r, "", "No critic reported a blocker, so the council stops here")
				}
			})
			break
		}
	}
	s.writeBrief(sess, onUpdate, draft)
}

// again is one extra round on an existing brief, with the user's notes.
func (s *Service) again(ctx context.Context, sess *Session, notes string, onUpdate func(Session)) {
	defer s.finishLive(sess.ID)
	n := len(sess.Rounds) + 1
	s.change(sess, onUpdate, func() {
		sess.Rounds = append(sess.Rounds, Round{N: n, Notes: notes, Critiques: []*Step{}})
		s.logLocked(sess, "round", n, "", fmt.Sprintf("Round %d started: you asked again", n))
	})
	for _, c := range sess.Critics {
		if err := s.lookPath(c); err != nil {
			s.change(sess, onUpdate, func() {
				sess.Critics = remove(sess.Critics, c)
				sess.Notes = append(sess.Notes, c+" is not installed, so it was skipped")
			})
		}
	}
	revised, _, err := s.round(ctx, sess, onUpdate, n-1, sess.Brief, s.projectContext(sess.Project), notes)
	if err != nil {
		s.fail(sess, onUpdate, err.Error())
		return
	}
	s.writeBrief(sess, onUpdate, revised)
}

// round runs the critics on draft in parallel, then has the proposer revise
// it. It returns the revised brief and whether any critic reported a blocker.
// With notes the proposer always revises; without, a round where every critic
// is content keeps the draft as it is.
func (s *Service) round(ctx context.Context, sess *Session, onUpdate func(Session), idx int, draft, project, notes string) (string, bool, error) {
	n := idx + 1
	s.change(sess, onUpdate, func() {
		sess.Stage = StageCritique
		s.logLocked(sess, "stage", n, "", "Critics are reading the draft")
	})
	steps := s.critique(ctx, sess, onUpdate, idx, sess.Critics, RoleCritique, draft, project, notes)
	// Every critic dropped out: the proposer critiques its own draft.
	if usable(steps) == 0 {
		s.change(sess, onUpdate, func() {
			if sess.Mode != ModeSelfCritique {
				sess.Mode = ModeSelfCritique
				sess.Notes = append(sess.Notes, "No other critic could take part, so "+sess.Proposer+" critiqued its own draft")
			}
		})
		steps = append(steps, s.critique(ctx, sess, onUpdate, idx, []string{sess.Proposer}, RoleSelfCritique, draft, project, notes)...)
		if usable(steps) == 0 {
			return "", false, errors.New("no critique could be read: " + firstError(steps))
		}
	}
	blocker, content := false, true
	for _, st := range steps {
		if st.Verdict == VerdictBlocker {
			blocker = true
		}
		if st.Error == "" && (st.Verdict != VerdictOK || len(st.Points) > 0) {
			content = false
		}
	}
	if content && notes == "" {
		s.change(sess, onUpdate, func() {
			s.logLocked(sess, "stage", n, "", "Every critic is content; the draft stands")
		})
		return draft, false, nil
	}

	step := &Step{Provider: sess.Proposer, Role: RoleSynthesise}
	s.change(sess, onUpdate, func() {
		sess.Stage = StageSynthesise
		sess.Rounds[idx].Synthesis = step
		s.logLocked(sess, "stage", n, "", sess.Proposer+" is revising the brief")
	})
	if err := s.call(ctx, sess, onUpdate, step, SynthesisePrompt, synthesisePrompt(sess.Input, project, draft, steps, notes)); err != nil {
		return "", blocker, err
	}
	revised := cleanBrief(step.Text)
	if revised == "" {
		return "", blocker, errors.New(sess.Proposer + " returned an empty revision")
	}
	return revised, blocker, nil
}

// usable counts the critiques that produced a verdict.
func usable(steps []*Step) int {
	n := 0
	for _, st := range steps {
		if st.Error == "" && st.Verdict != "" {
			n++
		}
	}
	return n
}

func firstError(steps []*Step) string {
	for _, st := range steps {
		if st.Error != "" {
			return st.Error
		}
	}
	return "no critic took part"
}

// critique runs the given critics in parallel on draft and records each in
// round idx. A critic that is not signed in leaves the session.
func (s *Service) critique(ctx context.Context, sess *Session, onUpdate func(Session), idx int, who []string, role, draft, project, notes string) []*Step {
	steps := make([]*Step, len(who))
	s.change(sess, onUpdate, func() {
		for i, p := range who {
			steps[i] = &Step{Provider: p, Role: role}
			sess.Rounds[idx].Critiques = append(sess.Rounds[idx].Critiques, steps[i])
		}
	})
	prompt := critiquePrompt(sess.Input, project, draft, notes, role == RoleSelfCritique)
	var wg sync.WaitGroup
	for _, st := range steps {
		wg.Add(1)
		go func(st *Step) {
			defer wg.Done()
			err := s.call(ctx, sess, onUpdate, st, CritiquePrompt, prompt)
			s.change(sess, onUpdate, func() {
				switch {
				case err != nil && skippable(err):
					note := skipNote(st.Provider, err)
					sess.Notes = append(sess.Notes, note)
					sess.Critics = remove(sess.Critics, st.Provider)
					s.logLocked(sess, "skipped", idx+1, st.Provider, note)
				case err != nil:
					s.logLocked(sess, "error", idx+1, st.Provider, st.Provider+" failed: "+agentexec.Excerpt(err.Error()))
				default:
					verdict, points, perr := parseCritique(st.Text)
					if perr != nil {
						st.Error = "the critique was not the expected JSON: " + perr.Error()
						s.logLocked(sess, "error", idx+1, st.Provider, st.Provider+"'s critique could not be read")
						return
					}
					st.Verdict, st.Points = verdict, points
					st.Text = ""
					s.logLocked(sess, "critique", idx+1, st.Provider, fmt.Sprintf("%s's critique arrived: %s, %d %s", st.Provider, verdict, len(points), plural(len(points), "point", "points")))
				}
			})
		}(st)
	}
	wg.Wait()
	return steps
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// projectContext is the line about the project the models get, or "".
func (s *Service) projectContext(id string) string {
	if id == "" {
		return ""
	}
	l, err := s.loadProjects()
	if err != nil || l == nil {
		return ""
	}
	p := l.Find(id)
	if p == nil {
		return ""
	}
	line := fmt.Sprintf("%s (a %s %s, status %s)", p.Name, p.Category, p.Type, p.Status)
	if p.Summary != "" {
		line += ": " + oneLine(p.Summary)
	}
	return line
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Get returns a session by id: the live one when a run is in flight, else
// the stored record. A record left "running" by a daemon that stopped is
// reported as failed.
func (s *Service) Get(id string) (*Session, error) {
	if !idRE.MatchString(id) {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess := s.live[id]; sess != nil {
		return clone(sess), nil
	}
	sess, err := s.readLocked(id)
	if err != nil {
		return nil, err
	}
	if sess.Status == StatusRunning {
		sess.Status, sess.Stage = StatusFailed, StageDone
		sess.Error = "the run was interrupted: Lucidbench stopped before it finished"
		sess.Thinking = []string{}
		_ = s.persistLocked(sess)
	}
	return sess, nil
}
