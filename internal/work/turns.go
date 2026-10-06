package work

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// How a turn reached the CLI.
const (
	// TurnFirst is the session's first run.
	TurnFirst = "first"
	// TurnResume continued the CLI's own session: claude --resume, codex exec
	// resume, grok --resume.
	TurnResume = "resume"
	// TurnSummary started the CLI afresh with the session's prompt and a
	// short account of the earlier turns, for a CLI that did not tell its
	// session id or could not resume it.
	TurnSummary = "summary"
)

// TurnInterrupted is the status of a turn that was running when Lucidbench
// stopped. A turn is otherwise running, done, failed or stopped.
const TurnInterrupted = "interrupted"

// DefaultHoldIdle is how long a waiting session keeps the agent browser:
// the browser's own idle time.
const DefaultHoldIdle = 10 * time.Minute

// MaxFollowUp is the longest follow-up prompt.
const MaxFollowUp = 32 << 10

// Turn is one run of the CLI in a session.
type Turn struct {
	N int `json:"n"`
	// Prompt is the user's follow-up; "" for the first turn, whose prompt is
	// the session's.
	Prompt  string           `json:"prompt,omitempty"`
	Mode    string           `json:"mode"`
	Status  string           `json:"status"`
	Started time.Time        `json:"started"`
	Ended   *time.Time       `json:"ended,omitempty"`
	Usage   *agentexec.Usage `json:"usage,omitempty"`
	Answer  string           `json:"answer,omitempty"`
	Error   string           `json:"error,omitempty"`
	// Event is the index of the turn's first event in the timeline.
	Event int `json:"event"`
}

// legacyTurn gives a session saved before turns existed its one turn.
func legacyTurn(se *Session) {
	if len(se.Turns) > 0 {
		return
	}
	st := se.Status
	if st == StatusRunning || st == StatusWaiting {
		st = StatusRunning
	}
	se.Turns = []Turn{{N: 1, Mode: TurnFirst, Status: st, Started: se.Started, Ended: se.Ended, Usage: se.Usage, Answer: se.Answer, Error: se.Error}}
}

// interrupted settles a session whose turn was running when the daemon
// stopped: it waits for the user again, and the turn says why it ended.
func (s *Service) interrupted(e *entry) {
	now := time.Now().UTC()
	se := &e.s
	t := &se.Turns[len(se.Turns)-1]
	t.Status, t.Ended = TurnInterrupted, &now
	if t.Usage != nil {
		t.Usage.Note = "interrupted before the CLI reported its final cost"
	}
	se.Status, se.Error, se.Ended = StatusWaiting, "", &now
	se.Usage = totalUsage(se.Turns)
	s.recordLocked(e, agentexec.Event{Time: now, Kind: agentexec.KindNote, Title: TurnInterrupted,
		Body: fmt.Sprintf("Lucidbench stopped while turn %d was running. Its processes ended with it; the worktree keeps what it had done. Send a follow-up to carry on.", t.N)})
	_ = s.save(se)
}

// totalUsage adds up the turns' usage; nil when no turn reported any.
func totalUsage(turns []Turn) *agentexec.Usage {
	var total *agentexec.Usage
	models := map[string]bool{}
	for _, t := range turns {
		u := t.Usage
		if u == nil {
			continue
		}
		if total == nil {
			total = &agentexec.Usage{Provider: u.Provider}
		}
		total.InputTokens += u.InputTokens
		total.OutputTokens += u.OutputTokens
		total.CacheRead += u.CacheRead
		total.CacheWrite += u.CacheWrite
		total.CostUSD += u.CostUSD
		total.DurationMS += u.DurationMS
		for _, m := range strings.Split(u.Model, ", ") {
			if m != "" {
				models[m] = true
			}
		}
		if u.Note != "" {
			total.Note = fmt.Sprintf("turn %d: %s", t.N, u.Note)
		}
	}
	if total != nil {
		names := make([]string, 0, len(models))
		for m := range models {
			names = append(names, m)
		}
		sort.Strings(names)
		total.Model = strings.Join(names, ", ")
	}
	return total
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// holdLocked lets go of the agent browser once a waiting session has waited
// HoldIdle. The caller holds e.mu.
func (s *Service) holdLocked(e *entry) {
	if e.release == nil {
		return
	}
	e.holdGen++
	gen, idle := e.holdGen, s.HoldIdle
	if idle <= 0 {
		idle = DefaultHoldIdle
	}
	time.AfterFunc(idle, func() {
		e.mu.Lock()
		var release func()
		if e.holdGen == gen && e.s.Status != StatusRunning {
			release, e.release = e.release, nil
		}
		e.mu.Unlock()
		if release != nil {
			release()
		}
	})
}

// takeRelease cancels a pending hold release and returns the release func
// for the caller to call once it has let go of e.mu. The caller holds e.mu.
func (e *entry) takeRelease() func() {
	e.holdGen++
	r := e.release
	e.release = nil
	return r
}

// FollowUp runs a new turn of a waiting session in the same worktree. The
// CLI resumes its own session when it told its id; otherwise it starts
// afresh with a summary of the earlier turns. It returns once the turn has
// begun.
func (s *Service) FollowUp(id, prompt string) (Session, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return Session{}, errf(ErrBadRequest, "write a follow-up")
	}
	if len(prompt) > MaxFollowUp {
		return Session{}, errf(ErrBadRequest, "the follow-up is longer than %d KB", MaxFollowUp>>10)
	}
	e, err := s.get(id)
	if err != nil {
		return Session{}, err
	}
	r := s.Runner
	if r.InContainer != nil && r.InContainer() {
		return Session{}, errf(ErrBadRequest, "%v", agentexec.ErrInContainer)
	}

	// Claim the session: only one follow-up starts, however many are sent.
	e.mu.Lock()
	se := e.s
	switch {
	case se.Status == StatusRunning:
		e.mu.Unlock()
		return se, errf(ErrConflict, "the agent is still working on this session; wait for the turn to end or stop it")
	case se.Status != StatusWaiting:
		e.mu.Unlock()
		return se, errf(ErrConflict, "the session has ended; start a new one")
	case se.Removed:
		e.mu.Unlock()
		return se, errf(ErrConflict, "the worktree was removed")
	}
	e.s.Status = StatusRunning
	e.holdGen++ // the browser stays held through the turn
	held := e.release
	e.mu.Unlock()
	unclaim := func(err error) (Session, error) {
		e.mu.Lock()
		e.s.Status = StatusWaiting
		s.holdLocked(e)
		got := e.s
		e.mu.Unlock()
		return got, err
	}

	look := r.LookPath
	if look == nil {
		look = lookPath
	}
	if _, err := look(se.Provider); err != nil {
		return unclaim(errf(ErrBadRequest, "%s is not on PATH; install it and sign in with `%s`", se.Provider, agentexec.Logins[se.Provider]))
	}
	if st, err := os.Stat(se.Worktree); err != nil || !st.IsDir() {
		return unclaim(errf(ErrConflict, "the worktree %s is gone", se.WorktreeHint))
	}
	// The browser may have slept since: attach it again, for a fresh address.
	var cdp string
	var release func()
	if se.Browser {
		if s.Browser == nil {
			return unclaim(errf(ErrBadRequest, "the Live browser extension is not available here"))
		}
		bctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cdp, release, err = s.Browser(bctx, id)
		cancel()
		if err != nil {
			return unclaim(errf(ErrBadRequest, "the agent browser did not start: %v", err))
		}
	}

	mode, text := TurnResume, prompt
	if se.ResumeID == "" {
		mode, text = TurnSummary, summaryPrompt(&se, prompt)
	} else if se.Browser {
		text += "\n\n(The agent browser was attached again: read its DevTools address from " + BrowserEnv + " anew.)"
	}

	e.mu.Lock()
	if e.events == nil {
		e.events = readEvents(filepath.Join(s.dir(id), "events.jsonl"))
	}
	if err := s.openLogs(e); err != nil {
		e.mu.Unlock()
		if release != nil {
			release()
		}
		return unclaim(err)
	}
	now := time.Now().UTC()
	n := len(e.s.Turns) + 1
	e.s.Turns = append(e.s.Turns, Turn{N: n, Prompt: prompt, Mode: mode, Status: StatusRunning, Started: now, Event: len(e.events)})
	e.s.Ended, e.s.Error = nil, ""
	e.stopping = false
	e.done = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	if release != nil {
		e.release = release
	}
	s.recordLocked(e, agentexec.Event{Time: now, Kind: agentexec.KindTurn, Title: turnTitle(n, mode, se.Provider), Body: prompt})
	_ = s.save(&e.s)
	snap := e.s
	done := e.done
	e.notify()
	e.mu.Unlock()
	if release != nil && held != nil {
		held() // the new attach holds it now
	}

	areq := s.turnRequest(e, &snap, text, cdp)
	if mode == TurnResume {
		areq.Resume, areq.NewSession = snap.ResumeID, ""
	}
	go s.run(ctx, e, areq, done)
	return snap, nil
}

// turnTitle is the timeline's separator for a follow-up: its number and how
// it reached the CLI.
func turnTitle(n int, mode, provider string) string {
	how := "the CLI resumed its session"
	switch {
	case mode == TurnSummary:
		how = "a fresh run with a summary of the earlier turns"
	case provider == "claude":
		how = "resumed with claude --resume"
	case provider == "codex":
		how = "resumed with codex exec resume"
	case provider == "grok":
		how = "resumed with grok --resume"
	}
	return fmt.Sprintf("Turn %d · %s", n, how)
}

// maxSummaryAnswer is how much of each earlier answer a summary keeps.
const maxSummaryAnswer = 1500

// summaryPrompt is a follow-up for a CLI that cannot resume: the session's
// own prompt (its rules and task), the earlier follow-ups with the agent's
// final message for each, the files changed so far, then the follow-up.
func summaryPrompt(se *Session, followUp string) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(se.Prompt))
	sb.WriteString("\n\n## Earlier in this session\n\nThis is a follow-up in the same worktree. The work of the earlier turns is already there: build on it rather than starting over.\n")
	turns := se.Turns
	if len(turns) > 8 {
		sb.WriteString(fmt.Sprintf("\n(%d earlier turns are left out.)\n", len(turns)-8))
		turns = turns[len(turns)-8:]
	}
	for _, t := range turns {
		sb.WriteString(fmt.Sprintf("\n### Turn %d", t.N))
		if t.Status != StatusDone && t.Status != StatusRunning {
			sb.WriteString(" (" + t.Status + ")")
		}
		sb.WriteString("\n\n")
		if t.Prompt != "" {
			sb.WriteString("The user asked:\n\n" + quote(t.Prompt) + "\n\n")
		} else if t.N == 1 {
			sb.WriteString("The task above.\n\n")
		}
		if a := strings.TrimSpace(t.Answer); a != "" {
			if r := []rune(a); len(r) > maxSummaryAnswer {
				a = string(r[:maxSummaryAnswer]) + "…"
			}
			sb.WriteString("Your final message:\n\n" + quote(a) + "\n")
		}
	}
	if d := se.Diff; d != nil && (len(d.Files) > 0 || len(d.Uncommitted) > 0) {
		sb.WriteString("\n### Changed so far\n\n")
		for i, f := range d.Files {
			if i == 50 {
				sb.WriteString(fmt.Sprintf("- … and %d more files\n", len(d.Files)-50))
				break
			}
			sb.WriteString(fmt.Sprintf("- %s (+%d −%d)\n", f.Path, f.Added, f.Deleted))
		}
		for _, c := range d.Commits {
			sb.WriteString("- commit: " + c.Subject + "\n")
		}
		if len(d.Uncommitted) > 0 {
			sb.WriteString(fmt.Sprintf("- %s not committed yet\n", plural(len(d.Uncommitted), "change")))
		}
	}
	sb.WriteString("\n## Follow-up from the user\n\n" + strings.TrimSpace(followUp) + "\n")
	return sb.String()
}

func quote(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// End finishes a waiting session: no more follow-ups, and the agent browser
// is let go. The worktree, branch and any PR stay.
func (s *Service) End(id string) (Session, error) {
	e, err := s.get(id)
	if err != nil {
		return Session{}, err
	}
	e.mu.Lock()
	switch e.s.Status {
	case StatusWaiting:
	case StatusRunning:
		se := e.s
		e.mu.Unlock()
		return se, errf(ErrConflict, "the agent is still working; stop the turn first")
	default:
		se := e.s
		e.mu.Unlock()
		return se, errf(ErrConflict, "the session has already ended")
	}
	e.s.Status = StatusDone
	if e.s.Ended == nil {
		now := time.Now().UTC()
		e.s.Ended = &now
	}
	release := e.takeRelease()
	_ = s.save(&e.s)
	se := e.s
	e.notify()
	e.mu.Unlock()
	if release != nil {
		release()
	}
	return se, nil
}
