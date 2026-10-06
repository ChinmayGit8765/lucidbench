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
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
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

// Preamble opens every prompt.
const Preamble = `You are working in a git worktree that Lucidbench created for this task, on its own branch.
- Work only inside this worktree (your current directory). Do not read or change files outside it.
- Commit your changes with clear, conventional commit messages as you go.
- Do not push, and do not open pull requests: the user reviews the diff and opens the PR.`

// Session is one agent run in one worktree.
type Session struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Profile  string `json:"profile,omitempty"`
	Harness  string `json:"harness"`
	Project  string `json:"project"`
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
	Events          int      `json:"events"`
	Diff            *Diff    `json:"diff,omitempty"`
	PR              string   `json:"pr_url,omitempty"`
	Pushed          bool     `json:"pushed,omitempty"`
	Removed         bool     `json:"removed,omitempty"`
}

// StartRequest is the body of POST /api/work/sessions.
type StartRequest struct {
	// Card is a card id on the work board, or "<board>/<card id>".
	Card     string `json:"card,omitempty"`
	Project  string `json:"project,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
	Provider string `json:"provider"`
	Profile  string `json:"profile,omitempty"`
	// Harness is "mine" (the CLI loads the user's settings, hooks and
	// skills) or "clean". Empty means mine for claude, clean for the others.
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	// AllowedCommands replaces the project's default list for this session:
	// plain command prefixes such as "go" or "git status". Nil means the
	// default (see DefaultAllowed); an empty list allows only the base set.
	AllowedCommands []string `json:"allowed_commands,omitempty"`
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
	done     chan struct{} // closed when the run has ended; nil when not running here
	eventsF  *os.File
	rawF     *os.File
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
}

// resolve checks a start request and finds its project, card and brief.
func (s *Service) resolve(req *StartRequest) (*task, error) {
	if _, ok := agentexec.Logins[req.Provider]; !ok {
		return nil, errf(ErrBadRequest, "provider must be claude, codex or grok")
	}
	if req.Harness == "" {
		req.Harness = agentexec.HarnessClean
		if req.Provider == "claude" {
			req.Harness = agentexec.HarnessMine
		}
	}
	if req.Harness != agentexec.HarnessMine && req.Harness != agentexec.HarnessClean {
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
	if t.title == "" {
		t.title = firstLine(req.Prompt, 80)
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

// prompt joins the preamble, the brief and the user's own words.
func prompt(t *task, extra string) string {
	var sb strings.Builder
	sb.WriteString(Preamble + "\n\n")
	if t.card != nil {
		sb.WriteString("# Task: " + t.title + "\n\n")
		sb.WriteString(strings.TrimSpace(t.body) + "\n")
		if strings.TrimSpace(extra) != "" {
			sb.WriteString("\n## Notes from the user\n\n" + strings.TrimSpace(extra) + "\n")
		}
	} else {
		sb.WriteString("# Task\n\n" + strings.TrimSpace(extra) + "\n")
	}
	return sb.String()
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

	allowed, err := sessionAllowed(&req, t.project)
	if err != nil {
		return Session{}, err
	}
	id := newID()
	wt, err := createWorktree(t.project.LocalPath, id, slug(t.title))
	if err != nil {
		return Session{}, err
	}
	se := Session{
		ID: id, Provider: req.Provider, Profile: req.Profile, Harness: req.Harness, Project: t.project.ID,
		RepoPath: wt.repo, RepoHint: s.hint(wt.repo), Branch: wt.branch, BaseRef: wt.baseRef, BaseSHA: wt.baseSHA,
		Worktree: wt.path, WorktreeHint: s.hint(wt.path), Title: t.title, Prompt: prompt(t, req.Prompt),
		Board: t.board, Brief: t.brief, AllowedCommands: allowed, Status: StatusRunning, Started: time.Now().UTC(),
	}
	if t.card != nil {
		se.Card = t.card.ID
	}
	e := &entry{s: se, changed: make(chan struct{}), events: []agentexec.Event{}, done: make(chan struct{})}
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
		s.moveCard(t.board, t.card.ID, ColumnInProgress, id)
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
	go s.run(ctx, e, areq)
	return se, nil
}

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
		s.moveCard(board, card, ColumnReview, id)
	}
}

// moveCard puts a card in column and links the session (or a PR URL) on its
// work:: line. A board without that column only gets the link.
func (s *Service) moveCard(board, cardID, column, work string) {
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
		if column == "" || boards.UpdateCard(v, board, c) != nil {
			c.Column = ""
			_ = boards.UpdateCard(v, board, c)
		}
		return
	}
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
