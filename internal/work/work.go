// Package work runs a coding agent on a task in its own git worktree. A
// session is one run: Lucidbench makes a worktree on a new branch next to the
// project's checkout, runs the provider's CLI there with the user's own
// account, streams what the agent does, and shows the diff when it is done.
// Pushing the branch and opening a draft PR happen only when the user asks;
// merging is always the user's click on GitHub.
//
// Each session lives in <DataDir>/work/sessions/<id>/: session.json (the
// record), events.jsonl (normalised events) and raw.log (the CLI's own JSON
// for each event).
package work

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/prompts"
)

// Session statuses.
const (
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusStopped = "stopped"
)

// Board columns a session moves its card to.
const (
	ColumnInProgress = "In progress"
	ColumnReview     = "Review"
	ColumnDone       = "Done"
)

// Errors, each mapped to an HTTP status by the routes.
var (
	ErrBadRequest = errors.New("bad request")
	ErrNotFound   = errors.New("not found")
	ErrRefused    = errors.New("refused")
	ErrConflict   = errors.New("conflict")
)

// kindErr is an error of one of the kinds above whose text is only the
// message, so the UI can show it as is.
type kindErr struct {
	kind error
	msg  string
}

func (e kindErr) Error() string { return e.msg }
func (e kindErr) Unwrap() error { return e.kind }

func errf(kind error, format string, a ...any) error {
	return kindErr{kind: kind, msg: fmt.Sprintf(format, a...)}
}

// plural is "1 commit", "2 commits".
func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// builder is the template every Work prompt is built from (see
// internal/prompts/templates/builder.yaml). Always the built-in one: a user's
// override changes what Studio shows, never Work's safety rules.
var builder = mustBuilder()

func mustBuilder() prompts.Template {
	t, ok := prompts.Builtin("builder")
	if !ok {
		panic("work: no builder template")
	}
	return t
}

// Preamble opens every prompt: the builder template's role and constraints,
// the rules every session gets (work only in the worktree, commit, never
// push). The session's allowed commands follow as the last constraint.
var Preamble = strings.TrimSpace(prompts.Render([]prompts.Section{
	{ID: prompts.Role, Body: builder.Body(prompts.Role)},
	{ID: prompts.Constraints, Body: builder.Body(prompts.Constraints)},
}, nil, nil).Text)

// Session is one agent run in one worktree.
type Session struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Profile  string `json:"profile,omitempty"`
	Model    string `json:"model,omitempty"`
	Harness  string `json:"harness"`
	Project  string `json:"project"`
	// Team says where the builder defaults came from (a project's team), ""
	// when the request chose everything itself.
	Team string `json:"team,omitempty"`
	// RepoPath is the project's checkout and Worktree the session's; the
	// hints show them with the home folder as "~".
	RepoPath     string `json:"repo_path"`
	RepoHint     string `json:"repo_hint"`
	Branch       string `json:"branch"`
	BaseRef      string `json:"base_ref"`
	BaseSHA      string `json:"base_sha"`
	Worktree     string `json:"worktree"`
	WorktreeHint string `json:"worktree_hint"`
	Title        string `json:"title"`
	Prompt       string `json:"prompt"`
	// Card is the board card the session works on ("" for a free prompt),
	// Brief the vault page it was given.
	Card    string           `json:"card,omitempty"`
	Board   string           `json:"board,omitempty"`
	Brief   string           `json:"brief,omitempty"`
	Status  string           `json:"status"`
	Error   string           `json:"error,omitempty"`
	Started time.Time        `json:"started"`
	Ended   *time.Time       `json:"ended,omitempty"`
	Usage   *agentexec.Usage `json:"usage,omitempty"`
	Answer  string           `json:"answer,omitempty"`
	// AllowedCommands are the shell commands the agent may run without asking.
	AllowedCommands []string `json:"allowed_commands,omitempty"`
	// Browser says the agent browser was attached to this session.
	Browser bool   `json:"browser,omitempty"`
	Events  int    `json:"events"`
	Diff    *Diff  `json:"diff,omitempty"`
	PR      string `json:"pr_url,omitempty"`
	// PRState is draft, open, merged or closed as GitHub last said, "" when
	// it is not known (no gh, or not asked yet); PRChecks counts the PR's
	// status checks and PRChecked is when both were read.
	PRState   string    `json:"pr_state,omitempty"`
	PRChecks  *PRChecks `json:"pr_checks,omitempty"`
	PRChecked time.Time `json:"pr_checked,omitzero"`
	CardDone  bool      `json:"card_done,omitempty"` // the card was moved to Done after the merge
	Pushed    bool      `json:"pushed,omitempty"`
	Removed   bool      `json:"removed,omitempty"`
}

// StartRequest is the body of POST /api/work/sessions.
type StartRequest struct {
	// Card is a card id on the work board, or "<board>/<card id>".
	Card    string `json:"card,omitempty"`
	Project string `json:"project,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
	// Provider may be empty when the project's team has a builder.
	Provider string `json:"provider"`
	Profile  string `json:"profile,omitempty"`
	// Harness is "mine" (the CLI loads the user's settings, hooks and
	// skills) or "clean". Empty means mine for claude, clean for the others.
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	// AllowedCommands replaces the project's default list for this session:
	// plain command prefixes such as "go" or "git status". Nil means the
	// default (see DefaultAllowed); an empty list allows no command at all.
	AllowedCommands []string `json:"allowed_commands,omitempty"`
	// Browser attaches the agent browser (Live browser extension): the
	// session's environment gets LUCID_BROWSER_CDP and its prompt a note.
	Browser bool `json:"browser,omitempty"`
}

// Service runs and keeps the sessions.
type Service struct {
	// Dir holds one folder per session.
	Dir    string
	Runner *agentexec.Runner
	// Vault opens the Memory vault, for cards and briefs; nil means no cards.
	Vault memory.Opener
	// Projects loads the project list.
	Projects func() (*projects.List, error)
	// Home is shown as "~" in path hints.
	Home string
	// PRView reads a PR's state and checks; nil asks gh.
	PRView func(dir, url string) (PRInfo, error)
	// Browser starts the agent browser for a session that asks for one and
	// returns its DevTools address and a release func, called when the
	// session ends. nil means the extension is not available.
	Browser func(ctx context.Context, session string) (cdp string, release func(), err error)
	// Team returns a project's builder (see internal/team), or nil when the
	// project has no team of its own; nil means no teams.
	Team func(project string) (*Builder, error)

	mu       sync.Mutex
	loaded   bool
	sessions map[string]*entry
}

// entry is a session and, while it runs, its live state.
type entry struct {
	mu       sync.Mutex
	s        Session
	events   []agentexec.Event // without Raw; nil until read for a finished session
	changed  chan struct{}     // closed and replaced on every change
	cancel   context.CancelFunc
	stopping bool
	polling  bool          // a PR state refresh is in flight
	done     chan struct{} // closed when the run has ended; nil when not running here
	eventsF  *os.File
	rawF     *os.File
	release  func() // lets go of the agent browser when the run ends
}

// New returns a Service keeping its sessions in dir.
func New(dir string, runner *agentexec.Runner, vault memory.Opener, list func() (*projects.List, error)) *Service {
	home, _ := os.UserHomeDir()
	if runner == nil {
		runner = &agentexec.Runner{}
	}
	return &Service{Dir: dir, Runner: runner, Vault: vault, Projects: list, Home: home}
}

func (s *Service) hint(p string) string { return projects.HomeHint(p, s.Home) }

// load reads the saved sessions once. A session that was running when the
// daemon stopped is marked failed.
func (s *Service) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.sessions = map[string]*entry{}
	dirs, _ := os.ReadDir(s.Dir)
	for _, d := range dirs {
		if !d.IsDir() || !idRE.MatchString(d.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, d.Name(), "session.json"))
		if err != nil {
			continue
		}
		var se Session
		if json.Unmarshal(data, &se) != nil || se.ID != d.Name() {
			continue
		}
		e := &entry{s: se, changed: make(chan struct{})}
		if se.Status == StatusRunning {
			now := time.Now().UTC()
			e.s.Status, e.s.Error, e.s.Ended = StatusFailed, "Lucidbench stopped while this session was running", &now
			_ = s.save(&e.s)
		}
		s.sessions[se.ID] = e
	}
}

func (s *Service) get(id string) (*entry, error) {
	if !idRE.MatchString(id) {
		return nil, errf(ErrNotFound, "no session %q", id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	e, ok := s.sessions[id]
	if !ok {
		return nil, errf(ErrNotFound, "no session %q", id)
	}
	return e, nil
}

// List returns every session, newest first.
func (s *Service) List() []Session {
	s.mu.Lock()
	s.load()
	es := make([]*entry, 0, len(s.sessions))
	for _, e := range s.sessions {
		es = append(es, e)
	}
	s.mu.Unlock()
	out := make([]Session, 0, len(es))
	for _, e := range es {
		e.mu.Lock()
		out = append(out, e.s)
		e.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.After(out[j].Started) })
	return out
}

// Get returns one session.
func (s *Service) Get(id string) (Session, error) {
	e, err := s.get(id)
	if err != nil {
		return Session{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.s, nil
}

func (s *Service) dir(id string) string { return filepath.Join(s.Dir, id) }

// save writes session.json atomically.
func (s *Service) save(se *Session) error {
	d := s.dir(se.ID)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(se, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(d, "session.json.tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(d, "session.json"))
}

// update changes a session under its lock, saves it and wakes the watchers.
func (s *Service) update(e *entry, fn func(se *Session)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn(&e.s)
	_ = s.save(&e.s)
	e.notify()
}

// notify wakes every watcher; the caller holds e.mu.
func (e *entry) notify() {
	close(e.changed)
	e.changed = make(chan struct{})
}

var (
	idRE   = regexp.MustCompile(`^[a-f0-9]{8}$`)
	slugRE = regexp.MustCompile(`[^a-z0-9]+`)
)

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// slug makes a short branch-safe name from a title.
func slug(title string) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "task"
	}
	return s
}

// firstLine is the first non-empty line of s, cut to n characters.
func firstLine(s string, n int) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>-* "))
		if l != "" {
			if r := []rune(l); len(r) > n {
				return string(r[:n]) + "…"
			}
			return l
		}
	}
	return ""
}

// task is what a start request resolves to before anything is created.
type task struct {
	project projects.Project
	board   string
	card    *boards.Card
	brief   string // vault path of the brief page
	body    string // the brief's text, or the card title
	title   string
	team    *Builder // the project's builder, nil without a team
}

// Builder is a project's builder role as Work reads it (see internal/team):
// the defaults a new session on the project starts with.
type Builder struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Profile  string `json:"profile,omitempty"`
	// AllowedCommands replaces the stack part of the project's default list,
	// like projects.yaml work.allowed_commands; nil keeps the defaults.
	AllowedCommands []string `json:"allowed_commands,omitempty"`
	// Source is where the team came from (repo, data or config).
	Source string `json:"source"`
	// BudgetUSD and the recent average are shown beside the provider.
	BudgetUSD *float64 `json:"budget_usd,omitempty"`
	AvgUSD    float64  `json:"avg_usd,omitempty"`
	Runs      int      `json:"runs,omitempty"`
}

// builder returns the project's team builder, or nil.
func (s *Service) builder(project string) *Builder {
	if s.Team == nil {
		return nil
	}
	b, err := s.Team(project)
	if err != nil {
		return nil
	}
	return b
}

// resolve checks a start request and finds its project, card and brief.
func (s *Service) resolve(req *StartRequest) (*task, error) {
	if _, ok := agentexec.Logins[req.Provider]; !ok && req.Provider != "" {
		return nil, errf(ErrBadRequest, "provider must be claude, codex or grok")
	}
	if req.Harness != "" && req.Harness != agentexec.HarnessMine && req.Harness != agentexec.HarnessClean {
		return nil, errf(ErrBadRequest, "harness must be mine or clean")
	}
	t := &task{}
	if req.Card != "" {
		if err := s.resolveCard(req, t); err != nil {
			return nil, err
		}
	}
	if req.Project == "" {
		return nil, errf(ErrBadRequest, "choose a project")
	}
	if strings.TrimSpace(t.body) == "" && strings.TrimSpace(req.Prompt) == "" {
		return nil, errf(ErrBadRequest, "write a prompt or pick a card")
	}
	if s.Projects == nil {
		return nil, errf(ErrBadRequest, "no projects file")
	}
	list, err := s.Projects()
	if err != nil {
		return nil, err
	}
	found := false
	for _, p := range list.Projects {
		if p.ID == req.Project {
			t.project, found = p, true
		}
	}
	if !found {
		return nil, errf(ErrNotFound, "no project %q in projects.yaml", req.Project)
	}
	if t.project.Visibility == "confidential" {
		return nil, errf(ErrRefused, "%s is confidential; Work never sends a confidential project to a provider", t.project.Name)
	}
	if t.project.LocalPath == "" {
		return nil, errf(ErrBadRequest, "%s has no local_path; add `local_path: <its checkout>` to the project in projects.yaml", t.project.Name)
	}
	t.team = s.builder(t.project.ID)
	if b := t.team; b != nil {
		if req.Provider == "" {
			req.Provider = b.Provider
		}
		if req.Provider == b.Provider {
			if req.Model == "" {
				req.Model = b.Model
			}
			if req.Profile == "" {
				req.Profile = b.Profile
			}
		}
	}
	if req.Provider == "" {
		return nil, errf(ErrBadRequest, "choose a provider")
	}
	if req.Harness == "" {
		req.Harness = agentexec.HarnessClean
		if req.Provider == "claude" {
			req.Harness = agentexec.HarnessMine
		}
	}
	if t.title == "" {
		t.title = firstLine(taskPart(req.Prompt), 80)
	}
	return t, nil
}

// resolveCard finds the card, its project and its brief.
func (s *Service) resolveCard(req *StartRequest, t *task) error {
	if s.Vault == nil {
		return errf(ErrBadRequest, "cards need the Memory vault")
	}
	t.board, req.Card = boards.DefaultBoard, strings.TrimSpace(req.Card)
	id := req.Card
	if b, c, ok := strings.Cut(req.Card, "/"); ok {
		t.board, id = b, c
	}
	v, err := s.Vault()
	if err != nil {
		return fmt.Errorf("cannot open the vault: %w", err)
	}
	b, err := boards.Get(v, t.board)
	if err != nil {
		return errf(ErrNotFound, "%v", err)
	}
	for i := range b.Cards {
		if b.Cards[i].ID == id {
			c := b.Cards[i]
			t.card = &c
		}
	}
	if t.card == nil {
		return errf(ErrNotFound, "no card %q on board %s", id, t.board)
	}
	if req.Project == "" {
		req.Project = t.card.Project
	}
	t.title, t.body = t.card.Title, t.card.Title
	if t.card.Memory != "" {
		conf, err := v.IsConfidential(t.card.Memory)
		if err != nil {
			return errf(ErrBadRequest, "the card's brief %s: %v", t.card.Memory, err)
		}
		if conf {
			return errf(ErrRefused, "the card's brief %s is confidential; Work never sends it to a provider", t.card.Memory)
		}
		p, err := v.Read(t.card.Memory)
		if err != nil {
			return errf(ErrBadRequest, "the card's brief %s: %v", t.card.Memory, err)
		}
		t.brief, t.body = p.Path, p.Body
		if p.Title != "" {
			t.title = p.Title
		}
	}
	return nil
}

// prompt renders the builder template: its role and constraints (with the
// session's allowed commands), the task (the brief and the user's own words),
// then the verify and report sections. A prompt composed in Prompt Studio
// that brings its own Verify or Report section keeps it instead.
func prompt(t *task, extra string, allowed []string) string {
	extra = strings.TrimSpace(extra)
	var task string
	if t.card != nil {
		task = "# Task: " + t.title + "\n\n" + strings.TrimSpace(t.body)
		if extra != "" {
			task += "\n\n## Notes from the user\n\n" + extra
		}
	} else {
		task = extra
	}
	constraints := builder.Body(prompts.Constraints)
	// A composed prompt's own constraints join Work's, after the safety rules.
	if own, rest, ok := cutSection(task, "Constraints"); ok {
		task = rest
		if own != "" {
			constraints += "\n" + own
		}
	}
	if len(allowed) > 0 {
		constraints += "\n- You may run only these commands without asking: `" + strings.Join(allowed, "`, `") +
			"`. Anything else is refused; do not try to get around the list."
	}
	secs := []prompts.Section{
		{ID: prompts.Role, Body: builder.Body(prompts.Role)},
		{ID: prompts.Constraints, Body: constraints},
		{ID: prompts.Task, Body: task},
	}
	if !hasHeading(task, "Verify") {
		verify := builder.Body(prompts.Verify)
		if t.project.Assessment != nil {
			if checks := assess.Suggest(*t.project.Assessment).CheckCommands; len(checks) > 0 {
				verify += "\n- The checks this project needs: `" + strings.Join(checks, "`, `") + "`."
			}
		}
		secs = append(secs, prompts.Section{ID: prompts.Verify, Body: verify})
	}
	if !hasHeading(task, "Report") {
		secs = append(secs, prompts.Section{ID: prompts.Report, Body: builder.Body(prompts.Report)})
	}
	return prompts.Render(secs, nil, nil).Text
}

// cutSection takes the "## name" section out of text: its body (up to the
// next heading) and the text without it.
func cutSection(text, name string) (body, rest string, ok bool) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		h, isH := strings.CutPrefix(strings.TrimSpace(l), "## ")
		if !isH || !strings.EqualFold(strings.TrimSpace(h), name) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
				end = j
				break
			}
		}
		body = strings.TrimSpace(strings.Join(lines[i+1:end], "\n"))
		rest = strings.TrimSpace(strings.Join(append(append([]string{}, lines[:i]...), lines[end:]...), "\n"))
		return body, rest, true
	}
	return "", text, false
}

// taskPart is what follows a "# Task" heading, or all of text without one:
// a prompt composed in Prompt Studio opens with its context, not its task.
func taskPart(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "# Task") {
			return strings.Join(lines[i+1:], "\n")
		}
	}
	return text
}

// hasHeading reports whether text has a "## name" heading of its own.
func hasHeading(text, name string) bool {
	for _, l := range strings.Split(text, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(l), "## "); ok && strings.EqualFold(strings.TrimSpace(h), name) {
			return true
		}
	}
	return false
}

// Start makes the worktree and starts the agent. It returns once the run has
// begun; the run goes on in the background.
func (s *Service) Start(req StartRequest) (Session, error) {
	t, err := s.resolve(&req)
	if err != nil {
		return Session{}, err
	}
	r := s.Runner
	if r.InContainer != nil && r.InContainer() {
		return Session{}, errf(ErrBadRequest, "%v", agentexec.ErrInContainer)
	}
	look := r.LookPath
	if look == nil {
		look = lookPath
	}
	if _, err := look(req.Provider); err != nil {
		return Session{}, errf(ErrBadRequest, "%s is not on PATH; install it and sign in with `%s`", req.Provider, agentexec.Logins[req.Provider])
	}

	allowed, err := sessionAllowed(&req, t.project, teamCommands(t.team))
	if err != nil {
		return Session{}, err
	}
	id := newID()
	var cdp string
	var release func()
	started := false
	if req.Browser {
		if s.Browser == nil {
			return Session{}, errf(ErrBadRequest, "the Live browser extension is not available here")
		}
		// The first start may pull the image.
		bctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cdp, release, err = s.Browser(bctx, id)
		cancel()
		if err != nil {
			return Session{}, errf(ErrBadRequest, "the agent browser did not start: %v", err)
		}
		defer func() {
			if !started {
				release()
			}
		}()
	}
	wt, err := createWorktree(t.project.LocalPath, id, slug(t.title))
	if err != nil {
		return Session{}, err
	}
	se := Session{
		ID: id, Provider: req.Provider, Profile: req.Profile, Model: req.Model, Harness: req.Harness, Project: t.project.ID,
		RepoPath: wt.repo, RepoHint: s.hint(wt.repo), Branch: wt.branch, BaseRef: wt.baseRef, BaseSHA: wt.baseSHA,
		Worktree: wt.path, WorktreeHint: s.hint(wt.path), Title: t.title, Prompt: prompt(t, req.Prompt, allowed),
		Board: t.board, Brief: t.brief, AllowedCommands: allowed, Status: StatusRunning, Started: time.Now().UTC(),
	}
	if t.card != nil {
		se.Card = t.card.ID
	}
	if req.Browser {
		se.Browser = true
		se.Prompt += browserNote
	}
	if t.team != nil {
		se.Team = t.team.Source
	}
	e := &entry{s: se, changed: make(chan struct{}), events: []agentexec.Event{}, done: make(chan struct{}), release: release}
	if err := s.save(&e.s); err != nil {
		return Session{}, err
	}
	if e.eventsF, err = os.OpenFile(filepath.Join(s.dir(id), "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
		return Session{}, err
	}
	if e.rawF, err = os.OpenFile(filepath.Join(s.dir(id), "raw.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err != nil {
		e.eventsF.Close()
		return Session{}, err
	}
	s.mu.Lock()
	s.load()
	s.sessions[id] = e
	s.mu.Unlock()

	if t.card != nil {
		s.moveCard(t.board, t.card.ID, ColumnInProgress, id, false)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	areq := agentexec.Request{
		Provider: req.Provider, Profile: req.Profile, Prompt: se.Prompt, Dir: wt.path,
		Tools: agentexec.ToolsEdit, Model: req.Model, Harness: req.Harness,
		Allow: allowed, Deny: denyRules(),
		Env:     agentTempEnv(wt.path),
		OnEvent: func(ev agentexec.Event) { s.record(e, ev) },
	}
	if cdp != "" {
		areq.Env = append(areq.Env, BrowserEnv+"="+cdp)
	}
	started = true
	go s.run(ctx, e, areq)
	return se, nil
}

// BrowserEnv is the environment variable that holds the attached agent
// browser's DevTools address.
const BrowserEnv = "LUCID_BROWSER_CDP"

// browserNote closes the prompt of a session with the agent browser attached.
const browserNote = "\n\n## Browser\n\nA headless Chromium is attached. Its DevTools (CDP) address is in the environment variable " + BrowserEnv +
	"; you may drive it over CDP (for example with connectOverCDP). It is a separate browser with an empty profile: no logins, and it cannot see the user's own browser. " +
	"To open a server on this machine in it, use host.docker.internal instead of localhost. Use it only for what the task needs.\n"

// tmpDirName is the agent's scratch directory inside its worktree. There is no
// OS sandbox yet, so pointing the temp variables here keeps the files a CLI
// writes to "the system temp folder" inside the worktree, where the session
// can see and remove them.
const tmpDirName = ".lucid-tmp"

// agentTempEnv makes <worktree>/.lucid-tmp, keeps git from listing it, and
// returns the temp variables that point at it.
func agentTempEnv(worktree string) []string {
	tmp := filepath.Join(worktree, tmpDirName)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil
	}
	if p, err := git(worktree, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude"); err == nil && p != "" {
		p = filepath.FromSlash(p)
		if b, _ := os.ReadFile(p); !excludes(string(b), tmpDirName) {
			_ = os.MkdirAll(filepath.Dir(p), 0o755)
			if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
					_, _ = f.WriteString("\n")
				}
				_, _ = f.WriteString(tmpDirName + "/\n")
				f.Close()
			}
		}
	}
	return []string{"TMP=" + tmp, "TEMP=" + tmp, "TMPDIR=" + tmp}
}

// excludes reports whether an info/exclude body already lists name.
func excludes(body, name string) bool {
	for _, l := range strings.Split(body, "\n") {
		if l = strings.TrimSpace(l); l == name || l == name+"/" {
			return true
		}
	}
	return false
}

// record keeps one event: events.jsonl, raw.log and the watchers.
func (s *Service) record(e *entry, ev agentexec.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(ev.Raw) > 0 && e.rawF != nil {
		_, _ = e.rawF.Write(append(append([]byte(nil), ev.Raw...), '\n'))
	}
	ev.Raw = nil
	if e.eventsF != nil {
		if line, err := json.Marshal(ev); err == nil {
			_, _ = e.eventsF.Write(append(line, '\n'))
		}
	}
	e.events = append(e.events, ev)
	e.s.Events = len(e.events)
	e.notify()
}

// run runs the agent and settles the session.
func (s *Service) run(ctx context.Context, e *entry, req agentexec.Request) {
	defer close(e.done)
	if e.release != nil {
		defer e.release()
	}
	res, err := s.Runner.Run(ctx, req)
	_ = os.RemoveAll(filepath.Join(req.Dir, tmpDirName))
	diff, derr := summarise(req.Dir, e.s.BaseSHA)

	e.mu.Lock()
	now := time.Now().UTC()
	se := &e.s
	se.Ended = &now
	// A run that ended cleanly is done, even when Stop came in while the
	// diff was being read.
	switch {
	case err == nil:
		se.Status = StatusDone
	case e.stopping:
		se.Status = StatusStopped
	default:
		se.Status, se.Error = StatusFailed, err.Error()
	}
	if res != nil {
		u := res.Usage
		if se.Status == StatusStopped {
			u.Note = "stopped before the CLI reported its final cost"
		}
		se.Usage, se.Answer = &u, res.Text
	}
	if derr == nil {
		se.Diff = diff
	}
	if e.rawF != nil {
		if err != nil {
			fmt.Fprintf(e.rawF, "# lucidbench: the run ended: %v\n", err)
		}
		e.rawF.Close()
		e.rawF = nil
	}
	if e.eventsF != nil {
		e.eventsF.Close()
		e.eventsF = nil
	}
	_ = s.save(se)
	board, card, status, id := se.Board, se.Card, se.Status, se.ID
	e.notify()
	e.mu.Unlock()

	if card != "" && status == StatusDone {
		s.moveCard(board, card, ColumnReview, id, false)
	}
}

// moveCard puts a card in column and links the session (or a PR URL) on its
// work:: line. A board without that column only gets the link.
func (s *Service) moveCard(board, cardID, column, work string, done bool) {
	if s.Vault == nil {
		return
	}
	v, err := s.Vault()
	if err != nil {
		return
	}
	b, err := boards.Get(v, board)
	if err != nil {
		return
	}
	for _, c := range b.Cards {
		if c.ID != cardID {
			continue
		}
		c.Work = work
		c.Column = column
		c.Done = c.Done || done
		if column == "" || boards.UpdateCard(v, board, c) != nil {
			c.Column = ""
			_ = boards.UpdateCard(v, board, c)
		}
		return
	}
}

// AddEvent adds an event to a running session's timeline, such as a browser
// screenshot. A session that is not running refuses it.
func (s *Service) AddEvent(id string, ev agentexec.Event) error {
	e, err := s.get(id)
	if err != nil {
		return err
	}
	e.mu.Lock()
	running := e.s.Status == StatusRunning && e.done != nil
	e.mu.Unlock()
	if !running {
		return errf(ErrConflict, "the session is not running")
	}
	s.record(e, ev)
	return nil
}

// Stop cancels a running session.
func (s *Service) Stop(id string) (Session, error) {
	e, err := s.get(id)
	if err != nil {
		return Session{}, err
	}
	e.mu.Lock()
	if e.s.Status != StatusRunning || e.cancel == nil {
		se := e.s
		e.mu.Unlock()
		return se, errf(ErrConflict, "the session is not running")
	}
	e.stopping = true
	e.cancel()
	done := e.done
	e.mu.Unlock()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
	return s.Get(id)
}

// Wait blocks until a session started here has ended, or ctx is done.
func (s *Service) Wait(ctx context.Context, id string) (Session, error) {
	e, err := s.get(id)
	if err != nil {
		return Session{}, err
	}
	e.mu.Lock()
	done := e.done
	e.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return Session{}, ctx.Err()
		}
	}
	return s.Get(id)
}

// Events returns the events from index from on, whether the session is still
// running, and a channel closed on the next change.
func (s *Service) Events(id string, from int) ([]agentexec.Event, bool, <-chan struct{}, error) {
	e, err := s.get(id)
	if err != nil {
		return nil, false, nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.events == nil {
		e.events = readEvents(filepath.Join(s.dir(id), "events.jsonl"))
	}
	if from < 0 {
		from = 0
	}
	var out []agentexec.Event
	if from < len(e.events) {
		out = append(out, e.events[from:]...)
	}
	return out, e.s.Status == StatusRunning, e.changed, nil
}

func readEvents(path string) []agentexec.Event {
	data, err := os.ReadFile(path)
	if err != nil {
		return []agentexec.Event{}
	}
	out := []agentexec.Event{}
	for _, l := range strings.Split(string(data), "\n") {
		var ev agentexec.Event
		if strings.TrimSpace(l) != "" && json.Unmarshal([]byte(l), &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

// RawLog returns raw.log.
func (s *Service) RawLog(id string) ([]byte, error) {
	if _, err := s.get(id); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.dir(id), "raw.log"))
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, nil
	}
	return data, err
}

// settled returns a session that is not running, for the actions that need
// the agent to be finished.
func (s *Service) settled(id string) (*entry, Session, error) {
	e, err := s.get(id)
	if err != nil {
		return nil, Session{}, err
	}
	e.mu.Lock()
	se := e.s
	e.mu.Unlock()
	if se.Status == StatusRunning {
		return nil, se, errf(ErrConflict, "the agent is still running; stop it first")
	}
	if se.Removed {
		return nil, se, errf(ErrConflict, "the worktree was removed")
	}
	return e, se, nil
}
