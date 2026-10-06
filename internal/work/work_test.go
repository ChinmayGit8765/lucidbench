package work

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The test binary doubles as the fake provider CLI and the fake gh: copied
// onto PATH as claude or gh, it acts on LUCID_WORK_FAKE.
func TestMain(m *testing.M) {
	if mode := os.Getenv("LUCID_WORK_FAKE"); mode != "" {
		name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
		switch name {
		case "claude":
			os.Exit(fakeClaude(mode))
		case "codex", "grok":
			os.Exit(fakeOther(name))
		case "gh":
			os.Exit(fakeGH())
		}
	}
	dir, err := os.MkdirTemp("", "lucid-work-fakes-")
	if err != nil {
		panic(err)
	}
	fakeDir = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// fakeDir holds the fake claude and gh, copied once: a new executable is slow
// to start the first time on some systems.
var fakeDir string

func out(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

type m = map[string]any

func fakeClaude(mode string) int {
	in, _ := io.ReadAll(os.Stdin)
	if log := os.Getenv("LUCID_WORK_LOG"); log != "" {
		wd, _ := os.Getwd()
		rec, _ := json.Marshal(m{"args": os.Args[1:], "stdin": string(in), "cwd": wd, "tmp": os.Getenv("TMP"), "temp": os.Getenv("TEMP"), "tmpdir": os.Getenv("TMPDIR"), "cdp": os.Getenv("LUCID_BROWSER_CDP")})
		_ = os.WriteFile(log, rec, 0o600)
	}
	// Scratch files go where the temp variables point.
	_ = os.WriteFile(filepath.Join(os.Getenv("TEMP"), "scratch.txt"), []byte("x"), 0o600)
	if mode == "fail" {
		out(m{"type": "result", "is_error": true, "result": "No conversation found with that session ID"})
		return 1
	}
	resumed := false
	for i, a := range os.Args {
		resumed = resumed || (a == "--resume" && i+1 < len(os.Args) && os.Args[i+1] == fakeSessionID)
	}
	if resumed || strings.Contains(string(in), "## Follow-up from the user") {
		return fakeClaudeAgain(mode, resumed)
	}
	if mode == "nosession" {
		// A CLI that never tells its session id: follow-ups fall back.
		out(m{"type": "system", "subtype": "init"})
	} else {
		out(m{"type": "system", "subtype": "init", "session_id": fakeSessionID})
	}
	out(m{"type": "assistant", "message": m{"content": []any{
		m{"type": "text", "text": "I'll add the line."},
		m{"type": "tool_use", "id": "t1", "name": "Write", "input": m{"file_path": "README.md", "content": "hello\n"}}}}})
	if mode == "slow" {
		time.Sleep(30 * time.Second)
		return 0
	}
	if err := os.WriteFile("README.md", []byte("hello\n"), 0o644); err != nil {
		return 1
	}
	out(m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t1", "content": "File written"}}}})
	if mode == "edit" || mode == "nosession" {
		out(m{"type": "assistant", "message": m{"content": []any{
			m{"type": "tool_use", "id": "t2", "name": "Bash", "input": m{"command": "git commit -am 'docs: say hello'"}}}}})
		for _, a := range [][]string{{"add", "README.md"}, {"commit", "-q", "-m", "docs: say hello"}} {
			if err := exec.Command("git", a...).Run(); err != nil {
				return 1
			}
		}
		out(m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t2", "content": "[lucid 1a2b3c] docs: say hello"}}}})
	}
	out(m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": "Done."}}}})
	out(m{"type": "result", "is_error": false, "result": "Done.", "total_cost_usd": 0.04,
		"usage": m{"input_tokens": 6, "output_tokens": 70}, "modelUsage": m{"claude-x": m{}}})
	return 0
}

// fakeSessionID is the session id the fake claude reports and resumes.
const fakeSessionID = "11111111-2222-4333-8444-555555555555"

// fakeClaudeAgain is a follow-up turn: it adds a line, commits it and says
// how it was reached, resumed or from a summary.
func fakeClaudeAgain(mode string, resumed bool) int {
	init := m{"type": "system", "subtype": "init"}
	if mode != "nosession" {
		init["session_id"] = fakeSessionID
	}
	out(init)
	how := "from the summary"
	if resumed {
		how = "resumed"
	}
	out(m{"type": "assistant", "message": m{"content": []any{
		m{"type": "text", "text": "Turn two, " + how + "."},
		m{"type": "tool_use", "id": "t3", "name": "Edit", "input": m{"file_path": "README.md", "old_string": "hello\n", "new_string": "hello\nagain\n"}}}}})
	if err := os.WriteFile("README.md", []byte("hello\nagain\n"), 0o644); err != nil {
		return 1
	}
	for _, a := range [][]string{{"add", "README.md"}, {"commit", "-q", "-m", "docs: say it again"}} {
		if err := exec.Command("git", a...).Run(); err != nil {
			return 1
		}
	}
	out(m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t3", "content": "edited"}}}})
	out(m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": "Done again."}}}})
	out(m{"type": "result", "is_error": false, "result": "Done again.", "total_cost_usd": 0.02,
		"usage": m{"input_tokens": 3, "output_tokens": 30}, "modelUsage": m{"claude-x": m{}}})
	return 0
}

// fakeOther is codex or grok in Work: it records its call and answers in
// its own stream format, "Done." on a first run and "Done again." on a resume.
func fakeOther(name string) int {
	in, _ := io.ReadAll(os.Stdin)
	if log := os.Getenv("LUCID_WORK_LOG"); log != "" {
		rec, _ := json.Marshal(m{"args": os.Args[1:], "stdin": string(in)})
		_ = os.WriteFile(log, rec, 0o600)
	}
	resumed := false
	for _, a := range os.Args[1:] {
		resumed = resumed || a == "resume" || a == "--resume"
	}
	text := "Done."
	if resumed {
		text = "Done again."
	}
	if name == "codex" {
		out(m{"type": "thread.started", "thread_id": "codex-thread-1"})
		out(m{"type": "item.completed", "item": m{"id": "i1", "type": "agent_message", "text": text}})
		out(m{"type": "turn.completed", "usage": m{"input_tokens": 10, "cached_input_tokens": 0, "output_tokens": 5}})
		return 0
	}
	out(m{"type": "text", "data": text})
	out(m{"type": "end", "stopReason": "end_turn", "total_cost_usd": 0.01, "usage": m{"input_tokens": 10, "output_tokens": 5}})
	return 0
}

func fakeGH() int {
	if log := os.Getenv("LUCID_WORK_GH_LOG"); log != "" {
		rec, _ := json.Marshal(m{"args": os.Args[1:]})
		_ = os.WriteFile(log, rec, 0o600)
	}
	fmt.Println("Creating draft pull request for lucid/x into main\n\nhttps://github.com/example/demo/pull/7")
	return 0
}

// installFakes puts the test binary on PATH as claude and gh, ahead of the
// real PATH so git still runs.
func installFakes(t *testing.T, mode string) (claudeLog, ghLog string) {
	t.Helper()
	dir := fakeDir
	for _, n := range []string{"claude", "codex", "grok", "gh"} {
		dst := filepath.Join(dir, n)
		if runtime.GOOS == "windows" {
			dst += ".exe"
		}
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LUCID_WORK_FAKE", mode)
	logs := t.TempDir()
	claudeLog, ghLog = filepath.Join(logs, "claude.json"), filepath.Join(logs, "gh.json")
	t.Setenv("LUCID_WORK_LOG", claudeLog)
	t.Setenv("LUCID_WORK_GH_LOG", ghLog)
	return claudeLog, ghLog
}

// canon is one spelling of a path for comparing: symlinks resolved and, on
// Windows, 8.3 short names (t.TempDir() can be a RUNNER~1 style name on CI) expanded.
func canon(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return longPath(filepath.Clean(p))
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	o, err := git(dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// newRepo makes <tmp>/demo with one commit on main, pushed to a bare
// <tmp>/origin.git. withHead also records origin/HEAD.
func newRepo(t *testing.T, withHead bool) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	repo := filepath.Join(root, "demo")
	run(t, root, "init", "-q", "--bare", "-b", "main", bare)
	run(t, root, "init", "-q", "-b", "main", repo)
	for _, kv := range [][2]string{{"user.name", "Test"}, {"user.email", "test@example.com"}, {"core.autocrlf", "false"}, {"commit.gpgsign", "false"}} {
		run(t, repo, "config", kv[0], kv[1])
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-q", "-m", "init")
	run(t, repo, "remote", "add", "origin", bare)
	run(t, repo, "push", "-q", "-u", "origin", "main")
	if withHead {
		run(t, repo, "remote", "set-head", "origin", "-a")
	}
	return repo
}

type fixture struct {
	svc   *Service
	repo  string
	vault *memory.Vault
}

func newFixture(t *testing.T, repo string) *fixture {
	t.Helper()
	v, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list := &projects.List{Configured: true, Projects: []projects.Project{
		{ID: "demo", Name: "Demo", Visibility: "public", LocalPath: repo},
		{ID: "secret", Name: "Secret", Visibility: "confidential", LocalPath: repo},
		{ID: "nopath", Name: "No path", Visibility: "private"},
		{ID: "notgit", Name: "Not git", Visibility: "private", LocalPath: t.TempDir()},
	}}
	// Only claude is "installed", whatever the real PATH holds.
	runner := &agentexec.Runner{LookPath: func(name string) (string, error) {
		if name != "claude" {
			return "", exec.ErrNotFound
		}
		return exec.LookPath(name)
	}}
	svc := New(t.TempDir(), runner, func() (*memory.Vault, error) { return v, nil },
		func() (*projects.List, error) { return list, nil })
	return &fixture{svc: svc, repo: repo, vault: v}
}

func (f *fixture) addCard(t *testing.T, title, body string, front map[string]any) boards.Card {
	t.Helper()
	if front == nil {
		front = map[string]any{}
	}
	front["title"] = title
	page := "Inbox/" + slug(title) + ".md"
	if err := f.vault.Write(&memory.Page{Path: page, Front: front, Body: body}); err != nil {
		t.Fatal(err)
	}
	c, err := boards.AddCard(f.vault, boards.DefaultBoard, boards.Card{Title: title, Column: "Ready", Project: "demo", Memory: page})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fixture) card(t *testing.T, id string) boards.Card {
	t.Helper()
	b, err := boards.Get(f.vault, boards.DefaultBoard)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range b.Cards {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("card %s gone", id)
	return boards.Card{}
}

func wait(t *testing.T, s *Service, id string) Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	se, err := s.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return se
}

// startCardSession runs the "edit" fake on a card and waits for it.
func startCardSession(t *testing.T, f *fixture) (Session, boards.Card, string) {
	t.Helper()
	claudeLog, _ := installFakes(t, "edit")
	c := f.addCard(t, "Add a README greeting", "Add a README line saying hello.\n\nDone when README.md says hello.", nil)
	se, err := f.svc.Start(StartRequest{Card: c.ID, Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return wait(t, f.svc, se.ID), c, claudeLog
}

func TestSessionOnCard(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, c, claudeLog := startCardSession(t, f)

	// A turn that ends cleanly leaves the session waiting for the user.
	if se.Status != StatusWaiting || se.Ended == nil {
		t.Fatalf("status %s (%s)", se.Status, se.Error)
	}
	if len(se.Turns) != 1 || se.Turns[0].Mode != TurnFirst || se.Turns[0].Status != StatusDone || se.Turns[0].Answer != "Done." || se.ResumeID != fakeSessionID {
		t.Errorf("turns %+v, resume id %q", se.Turns, se.ResumeID)
	}
	// Worktree next to the checkout, on a lucid/ branch from main.
	if want := filepath.Join(filepath.Dir(canon(f.repo)), "demo-lucid-"+se.ID); !strings.EqualFold(canon(se.Worktree), want) {
		t.Errorf("worktree %s, want %s", se.Worktree, want)
	}
	if !regexp.MustCompile(`^lucid/[a-f0-9]{8}-add-a-readme-greeting$`).MatchString(se.Branch) || !strings.Contains(se.Branch, se.ID) {
		t.Errorf("branch %q", se.Branch)
	}
	if se.BaseRef != "main" || se.BaseSHA != run(t, f.repo, "rev-parse", "main") {
		t.Errorf("base %s %s", se.BaseRef, se.BaseSHA)
	}
	if got := run(t, se.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != se.Branch {
		t.Errorf("worktree on %s", got)
	}
	if se.Harness != agentexec.HarnessMine || se.Project != "demo" || se.Card != c.ID || se.Brief != c.Memory {
		t.Errorf("session %+v", se)
	}

	// The prompt is the preamble plus the card's brief, run in the worktree.
	var rec struct {
		Args  []string `json:"args"`
		Stdin string   `json:"stdin"`
		Cwd   string   `json:"cwd"`
	}
	data, _ := os.ReadFile(claudeLog)
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.Stdin, Preamble) || !strings.Contains(rec.Stdin, "Add a README line saying hello.") ||
		!strings.Contains(rec.Stdin, "Done when README.md says hello.") ||
		!strings.Contains(rec.Stdin, "\n## Verify\n") || !strings.Contains(rec.Stdin, "\n## Report\n") ||
		!strings.Contains(rec.Stdin, "You may run only these commands without asking: `git status`") {
		t.Errorf("prompt:\n%s", rec.Stdin)
	}
	if !strings.EqualFold(canon(rec.Cwd), canon(se.Worktree)) {
		t.Errorf("ran in %s", rec.Cwd)
	}
	if strings.Contains(strings.Join(rec.Args, " "), "--safe-mode") {
		t.Errorf("harness mine ran clean: %v", rec.Args)
	}

	// Events persisted and streamed in order; a waiting session stays open.
	evs, open, _, err := f.svc.Events(se.ID, 0)
	if err != nil || !open {
		t.Fatal(err, open)
	}
	kinds := []string{}
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	if got := strings.Join(kinds, ","); got != "text,tool,diff,tool_result,tool,tool_result,text,done" {
		t.Errorf("events %s", got)
	}
	file := readEvents(filepath.Join(f.svc.Dir, se.ID, "events.jsonl"))
	if len(file) != len(evs) || se.Events != len(evs) {
		t.Errorf("events.jsonl has %d, session %d, memory %d", len(file), se.Events, len(evs))
	}
	if raw, _ := f.svc.RawLog(se.ID); !strings.Contains(string(raw), `"name":"Write"`) {
		t.Errorf("raw.log: %s", raw)
	}

	// Diff summary, usage and the card in Review.
	d := se.Diff
	if d == nil || len(d.Commits) != 1 || d.Commits[0].Subject != "docs: say hello" || len(d.Files) != 1 ||
		d.Files[0].Path != "README.md" || d.Files[0].Added != 1 || !strings.Contains(d.Files[0].Patch, "+hello") ||
		!strings.Contains(d.Stat, "README.md") || len(d.Uncommitted) != 0 {
		t.Errorf("diff %+v", d)
	}
	if se.Usage == nil || se.Usage.CostUSD != 0.04 || se.Usage.OutputTokens != 70 {
		t.Errorf("usage %+v", se.Usage)
	}
	if got := f.card(t, c.ID); got.Column != ColumnReview || got.Work != se.ID {
		t.Errorf("card %+v", got)
	}
	// A new service over the same folder reads the session back, waiting.
	again := New(f.svc.Dir, &agentexec.Runner{}, nil, nil)
	if l := again.List(); len(l) != 1 || l[0].ID != se.ID || l[0].Status != StatusWaiting {
		t.Errorf("reloaded %+v", l)
	}
	// End finishes it; the stream then ends too.
	got, err := f.svc.End(se.ID)
	if err != nil || got.Status != StatusDone {
		t.Fatalf("end: %v %+v", err, got)
	}
	if _, open, _, _ := f.svc.Events(se.ID, 0); open {
		t.Error("an ended session is still open")
	}
	if _, err := f.svc.End(se.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("second end: %v", err)
	}
	if _, err := f.svc.FollowUp(se.ID, "more"); !errors.Is(err, ErrConflict) {
		t.Errorf("follow-up after end: %v", err)
	}
	if got := f.card(t, c.ID); got.Column != ColumnReview {
		t.Errorf("card after end %+v", got)
	}
}

func TestFreePromptAndHarness(t *testing.T) {
	f := newFixture(t, newRepo(t, false))
	claudeLog, _ := installFakes(t, "edit")
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Say hello in the README", Provider: "claude", Harness: "clean"})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	if se.Status != StatusWaiting || se.Harness != "clean" || se.Title != "Say hello in the README" {
		t.Errorf("%+v", se)
	}
	data, _ := os.ReadFile(claudeLog)
	if !strings.Contains(string(data), "--safe-mode") || !strings.Contains(string(data), "Say hello in the README") {
		t.Errorf("clean harness call: %s", data)
	}
	// The agent's temp folder is inside its worktree, ignored by git, and gone
	// once the run ends.
	var call struct{ Tmp, Temp, Tmpdir string }
	_ = json.Unmarshal(data, &call)
	wantTmp := filepath.Join(se.Worktree, tmpDirName)
	if call.Tmp != wantTmp || call.Temp != wantTmp || call.Tmpdir != wantTmp {
		t.Errorf("temp variables %+v, want all %s", call, wantTmp)
	}
	if _, err := os.Stat(wantTmp); !os.IsNotExist(err) {
		t.Errorf("%s was not removed: %v", wantTmp, err)
	}
	if se.Diff == nil || len(se.Diff.Uncommitted) != 0 {
		t.Errorf("uncommitted after a clean run: %+v", se.Diff)
	}
	if ex, _ := git(se.Worktree, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude"); ex != "" {
		if b, _ := os.ReadFile(filepath.FromSlash(ex)); !excludes(string(b), tmpDirName) {
			t.Errorf("info/exclude does not list %s", tmpDirName)
		}
	}
	// No origin/HEAD: the current branch is the base.
	if se.BaseRef != "main" {
		t.Errorf("base %s", se.BaseRef)
	}
	// Harness defaults: mine for claude, clean for the others.
	for p, want := range map[string]string{"claude": "mine", "codex": "clean", "grok": "clean"} {
		req := StartRequest{Project: "demo", Prompt: "x", Provider: p}
		if _, err := f.svc.resolve(&req); err != nil || req.Harness != want {
			t.Errorf("%s: harness %q, %v", p, req.Harness, err)
		}
	}
}

func TestDefaultBase(t *testing.T) {
	repo := newRepo(t, true)
	if b, err := defaultBase(repo); err != nil || b != "main" {
		t.Errorf("with origin/HEAD: %q %v", b, err)
	}
	// origin/HEAD names a branch with no local copy.
	run(t, repo, "checkout", "-q", "-b", "dev")
	run(t, repo, "branch", "-q", "-D", "main")
	if b, _ := defaultBase(repo); b != "origin/main" {
		t.Errorf("remote only: %q", b)
	}
	// No origin/HEAD: the current branch.
	run(t, repo, "remote", "set-head", "origin", "-d")
	if b, _ := defaultBase(repo); b != "dev" {
		t.Errorf("fallback: %q", b)
	}
}

func TestStop(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	installFakes(t, "slow")
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "take your time", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	// Let the first event arrive, so the CLI is really running.
	deadline := time.Now().Add(20 * time.Second)
	for {
		evs, _, _, _ := f.svc.Events(se.ID, 0)
		if len(evs) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A follow-up cannot start while the turn runs.
	if _, err := f.svc.FollowUp(se.ID, "hurry up"); !errors.Is(err, ErrConflict) {
		t.Errorf("follow-up while running: %v", err)
	}
	start := time.Now()
	got, err := f.svc.Stop(se.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Stop ends the turn, not the session: it waits for the user again.
	if got.Status != StatusWaiting || got.Ended == nil || time.Since(start) > 14*time.Second {
		t.Errorf("after stop: %s in %s", got.Status, time.Since(start))
	}
	if len(got.Turns) != 1 || got.Turns[0].Status != StatusStopped {
		t.Errorf("turns %+v", got.Turns)
	}
	if got.Usage == nil || !strings.Contains(got.Usage.Note, "stopped before the CLI reported its final cost") {
		t.Errorf("a stopped turn needs a usage note: %+v", got.Usage)
	}
	evs, _, _, _ := f.svc.Events(se.ID, 0)
	if last := evs[len(evs)-1]; last.Kind != agentexec.KindNote || last.Title != "stopped" {
		t.Errorf("last event %+v", last)
	}
	if _, err := f.svc.Stop(se.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("second stop: %v", err)
	}
	// The CLI told its session id before it was stopped: the follow-up resumes it.
	if _, err := f.svc.FollowUp(se.ID, "carry on"); err != nil {
		t.Fatal(err)
	}
	got = wait(t, f.svc, se.ID)
	if got.Status != StatusWaiting || len(got.Turns) != 2 || got.Turns[1].Mode != TurnResume || got.Turns[1].Status != StatusDone {
		t.Errorf("after the follow-up: %s %+v", got.Status, got.Turns)
	}
}

// A follow-up resumes the CLI's own session in the same worktree: a second
// turn with its own events, usage and commits.
func TestFollowUpResumes(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, claudeLog := startCardSession(t, f)
	if _, err := f.svc.FollowUp(se.ID, "   "); !errors.Is(err, ErrBadRequest) {
		t.Errorf("empty follow-up: %v", err)
	}
	before := se.Events
	if _, err := f.svc.FollowUp(se.ID, "Say it again"); err != nil {
		t.Fatal(err)
	}
	got := wait(t, f.svc, se.ID)

	var rec struct {
		Args  []string `json:"args"`
		Stdin string   `json:"stdin"`
		Cwd   string   `json:"cwd"`
	}
	data, _ := os.ReadFile(claudeLog)
	_ = json.Unmarshal(data, &rec)
	args := strings.Join(rec.Args, " ")
	if !strings.Contains(args, "--resume "+fakeSessionID) || rec.Stdin != "Say it again" || !strings.EqualFold(canon(rec.Cwd), canon(se.Worktree)) {
		t.Errorf("turn 2 ran %v in %s with %q", rec.Args, rec.Cwd, rec.Stdin)
	}
	// The same harness and allow list every turn.
	if strings.Contains(args, "--safe-mode") || !strings.Contains(args, "Bash(git status:*)") {
		t.Errorf("turn 2 lost the session's settings: %v", rec.Args)
	}
	if got.Status != StatusWaiting || len(got.Turns) != 2 || got.Answer != "Done again." {
		t.Fatalf("%s %+v", got.Status, got.Turns)
	}
	t2 := got.Turns[1]
	if t2.N != 2 || t2.Mode != TurnResume || t2.Status != StatusDone || t2.Prompt != "Say it again" || t2.Event != before || t2.Usage == nil || t2.Usage.CostUSD != 0.02 {
		t.Errorf("turn 2 %+v", t2)
	}
	// Usage adds up across the turns.
	if u := got.Usage; u == nil || u.CostUSD < 0.0599 || u.CostUSD > 0.0601 || u.OutputTokens != 100 || u.InputTokens != 9 {
		t.Errorf("total usage %+v", got.Usage)
	}
	evs, _, _, _ := f.svc.Events(se.ID, 0)
	if sep := evs[before]; sep.Kind != agentexec.KindTurn || !strings.HasPrefix(sep.Title, "Turn 2 · ") || !strings.Contains(sep.Title, "claude --resume") || sep.Body != "Say it again" {
		t.Errorf("turn separator %+v", sep)
	}
	if evs[len(evs)-1].Kind != agentexec.KindDone || got.Events != len(evs) {
		t.Errorf("turn 2 events end with %+v (%d of %d)", evs[len(evs)-1], got.Events, len(evs))
	}
	if d := got.Diff; d == nil || len(d.Commits) != 2 || !strings.Contains(d.Files[0].Patch, "+again") {
		t.Errorf("diff after turn 2 %+v", got.Diff)
	}
	// A summary of the list carries the turns without their text.
	if sum := got.Summary(); len(sum.Turns) != 2 || sum.Turns[1].Prompt != "" || sum.Turns[0].Answer != "" {
		t.Errorf("summary turns %+v", sum.Turns)
	}
	// A PR can open while the session waits, and it keeps waiting.
	installFakes(t, "edit")
	if pr, err := f.svc.OpenPR(se.ID); err != nil || pr.PR == "" || pr.Status != StatusWaiting {
		t.Errorf("pr while waiting: %v %+v", err, pr.Status)
	}
}

// A CLI that never told its session id gets a fresh run whose prompt carries
// the session's prompt, a summary of the earlier turns and the follow-up.
func TestFollowUpFallsBackToASummary(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	claudeLog, _ := installFakes(t, "nosession")
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Say hello in the README", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	if se.Status != StatusWaiting || se.ResumeID != "" {
		t.Fatalf("%s resume %q", se.Status, se.ResumeID)
	}
	if _, err := f.svc.FollowUp(se.ID, "Now say it again"); err != nil {
		t.Fatal(err)
	}
	got := wait(t, f.svc, se.ID)
	var rec struct {
		Args  []string `json:"args"`
		Stdin string   `json:"stdin"`
	}
	data, _ := os.ReadFile(claudeLog)
	_ = json.Unmarshal(data, &rec)
	if strings.Contains(strings.Join(rec.Args, " "), "--resume") {
		t.Errorf("no session to resume, but %v", rec.Args)
	}
	for _, want := range []string{Preamble, "Say hello in the README", "## Earlier in this session", "### Turn 1", "> Done.", "README.md (+1 −0)", "commit: docs: say hello", "## Follow-up from the user\n\nNow say it again"} {
		if !strings.Contains(rec.Stdin, want) {
			t.Errorf("summary prompt lacks %q:\n%s", want, rec.Stdin)
		}
	}
	if len(got.Turns) != 2 || got.Turns[1].Mode != TurnSummary || got.Turns[1].Status != StatusDone || got.Answer != "Done again." {
		t.Errorf("turns %+v", got.Turns)
	}
	evs, _, _, _ := f.svc.Events(se.ID, 0)
	if sep := evs[got.Turns[1].Event]; sep.Kind != agentexec.KindTurn || !strings.Contains(sep.Title, "summary") {
		t.Errorf("turn separator %+v", sep)
	}
}

// Codex and grok resume natively too, each with its own flags.
func TestFollowUpOtherProviders(t *testing.T) {
	for _, p := range []string{"codex", "grok"} {
		t.Run(p, func(t *testing.T) {
			f := newFixture(t, newRepo(t, true))
			log, _ := installFakes(t, "edit")
			// Never the real CLI: the name must resolve to the fake.
			if bin, err := exec.LookPath(p); err != nil || !strings.EqualFold(canon(filepath.Dir(bin)), canon(fakeDir)) {
				t.Fatalf("%s resolves to %q (%v), not the fake in %s", p, bin, err, fakeDir)
			}
			f.svc.Runner.LookPath = exec.LookPath
			se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Say hello", Provider: p})
			if err != nil {
				t.Fatal(err)
			}
			se = wait(t, f.svc, se.ID)
			if se.Status != StatusWaiting || se.ResumeID == "" || se.Answer != "Done." {
				t.Fatalf("%s resume %q answer %q (%s)", se.Status, se.ResumeID, se.Answer, se.Error)
			}
			var rec struct{ Args []string }
			data, _ := os.ReadFile(log)
			_ = json.Unmarshal(data, &rec)
			first := strings.Join(rec.Args, " ")
			if p == "grok" && !strings.Contains(first, "--session-id "+se.ResumeID) {
				t.Errorf("grok was not given its session id: %v", rec.Args)
			}
			if _, err := f.svc.FollowUp(se.ID, "again"); err != nil {
				t.Fatal(err)
			}
			got := wait(t, f.svc, se.ID)
			data, _ = os.ReadFile(log)
			_ = json.Unmarshal(data, &rec)
			args := strings.Join(rec.Args, " ")
			want := "exec resume"
			if p == "grok" {
				want = "--resume " + se.ResumeID
			}
			if !strings.Contains(args, want) || (p == "codex" && !strings.HasSuffix(args, "codex-thread-1 -")) {
				t.Errorf("turn 2 args %v, want %q", rec.Args, want)
			}
			if got.Answer != "Done again." || got.Turns[1].Mode != TurnResume || got.Turns[1].Status != StatusDone {
				t.Errorf("%+v", got.Turns)
			}
			evs, _, _, _ := f.svc.Events(se.ID, 0)
			if sep := evs[got.Turns[1].Event]; !strings.Contains(sep.Title, p) {
				t.Errorf("separator %+v", sep)
			}
		})
	}
}

// A follow-up whose native resume fails leaves the session waiting, and the
// next one falls back to a summary.
func TestFailedResumeFallsBack(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	installFakes(t, "fail")
	if _, err := f.svc.FollowUp(se.ID, "break"); err != nil {
		t.Fatal(err)
	}
	got := wait(t, f.svc, se.ID)
	if got.Status != StatusWaiting || got.Turns[1].Status != StatusFailed || got.Turns[1].Error == "" || got.ResumeID != "" {
		t.Fatalf("%s %+v resume %q", got.Status, got.Turns, got.ResumeID)
	}
	installFakes(t, "edit")
	if _, err := f.svc.FollowUp(se.ID, "try again"); err != nil {
		t.Fatal(err)
	}
	if got = wait(t, f.svc, se.ID); got.Turns[2].Mode != TurnSummary || got.Turns[2].Status != StatusDone {
		t.Errorf("%+v", got.Turns)
	}
}

func TestOpenPRAndRemove(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, c, _ := startCardSession(t, f)
	_, ghLog := installFakes(t, "edit")

	// Unpushed work is not removed without a discard.
	if _, err := f.svc.Remove(se.ID, false); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "1 unpushed commit and 0 uncommitted changes") {
		t.Fatalf("remove unpushed: %v", err)
	}
	got, err := f.svc.OpenPR(se.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PR != "https://github.com/example/demo/pull/7" || !got.Pushed {
		t.Errorf("pr %+v", got)
	}
	// The branch is on the bare origin.
	bare := filepath.Join(filepath.Dir(f.repo), "origin.git")
	if run(t, bare, "rev-parse", se.Branch) != run(t, se.Worktree, "rev-parse", "HEAD") {
		t.Error("branch not pushed")
	}
	var rec struct{ Args []string }
	data, _ := os.ReadFile(ghLog)
	_ = json.Unmarshal(data, &rec)
	args := strings.Join(rec.Args, "\x00")
	for _, want := range []string{"pr\x00create\x00--draft", "--title\x00Add a README greeting", "--head\x00" + se.Branch, "--base\x00main"} {
		if !strings.Contains(args, want) {
			t.Errorf("gh args %q lack %q", rec.Args, want)
		}
	}
	body := ""
	for i, a := range rec.Args {
		if a == "--body" && i+1 < len(rec.Args) {
			body = rec.Args[i+1]
		}
	}
	if !strings.Contains(body, c.Memory) || !strings.Contains(body, "docs: say hello") || strings.Contains(body, filepath.Dir(f.repo)) {
		t.Errorf("body:\n%s", body)
	}
	if card := f.card(t, c.ID); card.Work != got.PR {
		t.Errorf("card work %q", card.Work)
	}
	// A second call returns the same PR.
	if again, err := f.svc.OpenPR(se.ID); err != nil || again.PR != got.PR {
		t.Errorf("again: %v", err)
	}
	// Pushed, so the worktree goes without a discard; the branch stays, and
	// the waiting session ends with its worktree.
	got, err = f.svc.Remove(se.ID, false)
	if err != nil || !got.Removed || got.Status != StatusDone {
		t.Fatal(err, got.Status)
	}
	if _, err := os.Stat(se.Worktree); !os.IsNotExist(err) {
		t.Errorf("worktree still there: %v", err)
	}
	run(t, f.repo, "rev-parse", "--verify", se.Branch)
	if _, err := f.svc.OpenPR(se.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("pr after remove: %v", err)
	}
}

func TestNoCommitsNoPR(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	installFakes(t, "nocommit")
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "write but do not commit", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	if se.Diff == nil || len(se.Diff.Commits) != 0 || len(se.Diff.Uncommitted) != 1 {
		t.Fatalf("diff %+v", se.Diff)
	}
	if _, err := f.svc.OpenPR(se.ID); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("pr: %v", err)
	}
	if _, err := f.svc.Remove(se.ID, false); !errors.Is(err, ErrConflict) {
		t.Errorf("remove dirty: %v", err)
	}
	// A commit made after the run shows up on refresh.
	run(t, se.Worktree, "add", "README.md")
	run(t, se.Worktree, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", "by hand")
	if got, err := f.svc.Refresh(se.ID); err != nil || len(got.Diff.Commits) != 1 || len(got.Diff.Uncommitted) != 0 {
		t.Errorf("refresh: %v %+v", err, got.Diff)
	}
	if got, err := f.svc.Remove(se.ID, true); err != nil || !got.Removed {
		t.Errorf("discard: %v", err)
	}
}

func TestRefusals(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	installFakes(t, "edit")
	secret := f.addCard(t, "Secret plan", "do not send", map[string]any{"confidential": true})
	cases := []struct {
		req  StartRequest
		want error
		msg  string
	}{
		{StartRequest{Project: "secret", Prompt: "x", Provider: "claude"}, ErrRefused, "confidential"},
		{StartRequest{Card: secret.ID, Provider: "claude"}, ErrRefused, "confidential"},
		{StartRequest{Project: "nopath", Prompt: "x", Provider: "claude"}, ErrBadRequest, "local_path"},
		{StartRequest{Project: "notgit", Prompt: "x", Provider: "claude"}, ErrBadRequest, "not a git repository"},
		{StartRequest{Project: "nope", Prompt: "x", Provider: "claude"}, ErrNotFound, "no project"},
		{StartRequest{Project: "demo", Provider: "claude"}, ErrBadRequest, "prompt"},
		{StartRequest{Project: "demo", Prompt: "x", Provider: "gpt"}, ErrBadRequest, "provider"},
		{StartRequest{Project: "demo", Prompt: "x", Provider: "claude", Harness: "yours"}, ErrBadRequest, "harness"},
		{StartRequest{Project: "demo", Prompt: "x", Provider: "codex"}, ErrBadRequest, "not on PATH"},
	}
	for _, c := range cases {
		_, err := f.svc.Start(c.req)
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.msg) {
			t.Errorf("%+v: %v", c.req, err)
		}
	}
	// Nothing was created next to the repo.
	entries, _ := os.ReadDir(filepath.Dir(f.repo))
	for _, e := range entries {
		if strings.Contains(e.Name(), "-lucid-") {
			t.Errorf("worktree made on refusal: %s", e.Name())
		}
	}
}

// A session whose turn was running when the daemon stopped waits for the
// user again, its turn marked interrupted, and can be followed up.
func TestRestartLeavesSessionsWaiting(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	// One saved before turns existed, one in its second turn.
	old := Session{ID: "0123abcd", Status: StatusRunning, Started: now}
	cur := Session{ID: "4567cdef", Status: StatusRunning, Started: now, ResumeID: fakeSessionID,
		Turns: []Turn{{N: 1, Mode: TurnFirst, Status: StatusDone, Started: now, Ended: &now}, {N: 2, Mode: TurnResume, Status: StatusRunning, Started: now, Event: 3}}}
	waiting := Session{ID: "89abcdef", Status: StatusWaiting, Started: now, Ended: &now, Turns: []Turn{{N: 1, Mode: TurnFirst, Status: StatusDone, Started: now}}}
	for _, se := range []*Session{&old, &cur, &waiting} {
		if err := (&Service{Dir: dir}).save(se); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(dir, nil, nil, nil)
	for _, id := range []string{old.ID, cur.ID} {
		got, err := svc.Get(id)
		if err != nil || got.Status != StatusWaiting || got.Error != "" || got.Ended == nil {
			t.Fatalf("%s: %+v %v", id, got, err)
		}
		last := got.Turns[len(got.Turns)-1]
		if last.Status != TurnInterrupted || last.Ended == nil {
			t.Errorf("%s: last turn %+v", id, last)
		}
		evs, open, _, _ := svc.Events(id, 0)
		if !open || len(evs) != 1 || evs[0].Kind != agentexec.KindNote || evs[0].Title != TurnInterrupted || got.Events != 1 {
			t.Errorf("%s: events %+v open %v", id, evs, open)
		}
		// Saved: a second restart finds it waiting, with the note once.
		if b, _ := os.ReadFile(filepath.Join(dir, id, "events.jsonl")); strings.Count(string(b), "\n") != 1 {
			t.Errorf("%s: events.jsonl %q", id, b)
		}
	}
	if got, _ := New(dir, nil, nil, nil).Get(cur.ID); got.Status != StatusWaiting || got.ResumeID != fakeSessionID || len(got.Turns) != 2 {
		t.Errorf("after a second restart %+v", got)
	}
	if got, _ := svc.Get(waiting.ID); got.Status != StatusWaiting || got.Turns[0].Status != StatusDone {
		t.Errorf("a waiting session changed on load: %+v", got)
	}
}

// After a restart the follow-up picks up from the events on disk.
func TestFollowUpAfterRestart(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	again := New(f.svc.Dir, f.svc.Runner, f.svc.Vault, f.svc.Projects)
	if _, err := again.FollowUp(se.ID, "after the restart"); err != nil {
		t.Fatal(err)
	}
	got := wait(t, again, se.ID)
	if got.Status != StatusWaiting || got.Turns[1].Event != se.Events || got.Turns[1].Mode != TurnResume {
		t.Fatalf("%s %+v", got.Status, got.Turns)
	}
	evs, _, _, _ := again.Events(se.ID, 0)
	if len(evs) != got.Events || evs[0].Kind != "text" || evs[se.Events].Kind != agentexec.KindTurn {
		t.Errorf("events after the restart: %d (record %d)", len(evs), got.Events)
	}
}

func TestRoutes(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	installFakes(t, "edit")
	mux := http.NewServeMux()
	Register(mux, f.svc)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	post := func(path, body string, confirm bool) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	start := `{"project":"demo","prompt":"hello","provider":"claude"}`
	if res := post("/api/work/sessions", start, false); res.StatusCode != http.StatusForbidden {
		t.Errorf("no confirm: %d", res.StatusCode)
	}
	if res := post("/api/work/sessions", `{"project":"secret","prompt":"x","provider":"claude"}`, true); res.StatusCode != http.StatusForbidden {
		t.Errorf("confidential: %d", res.StatusCode)
	}
	res := post("/api/work/sessions", start, true)
	if res.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("start: %d %s", res.StatusCode, b)
	}
	var se Session
	_ = json.NewDecoder(res.Body).Decode(&se)
	wait(t, f.svc, se.ID)

	// A follow-up needs the confirm header, a prompt, and a waiting session.
	followup := "/api/work/sessions/" + se.ID + "/followup"
	if r := post(followup, `{"prompt":"again"}`, false); r.StatusCode != http.StatusForbidden {
		t.Errorf("follow-up without confirm: %d", r.StatusCode)
	}
	if r := post(followup, `{"prompt":""}`, true); r.StatusCode != http.StatusBadRequest {
		t.Errorf("empty follow-up: %d", r.StatusCode)
	}
	if r := post(followup, `{"text":"again"}`, true); r.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown field: %d", r.StatusCode)
	}
	if r := post(followup, `{"prompt":"Say it again"}`, true); r.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(r.Body)
		t.Fatalf("follow-up: %d %s", r.StatusCode, b)
	}
	wait(t, f.svc, se.ID)
	if r := post("/api/work/sessions/"+se.ID+"/end", "", true); r.StatusCode != http.StatusOK {
		t.Errorf("end: %d", r.StatusCode)
	}
	if r := post(followup, `{"prompt":"more"}`, true); r.StatusCode != http.StatusConflict {
		t.Errorf("follow-up after end: %d", r.StatusCode)
	}
	if r := post("/api/work/sessions/"+se.ID+"/end", "", true); r.StatusCode != http.StatusConflict {
		t.Errorf("second end: %d", r.StatusCode)
	}
	ended, _ := f.svc.Get(se.ID)

	// The event stream replays both turns, then ends with the final record.
	sse, err := http.Get(srv.URL + "/api/work/sessions/" + se.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	if ct := sse.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %s", ct)
	}
	var data, events []string
	sc := bufio.NewScanner(sse.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "event: ") {
			events = append(events, strings.TrimPrefix(l, "event: "))
		}
		if strings.HasPrefix(l, "data: {\"time\"") {
			data = append(data, l)
		}
	}
	if len(data) != ended.Events || ended.Events != 15 || len(events) < 2 || events[len(events)-1] != "end" || events[len(events)-2] != "session" {
		t.Errorf("stream: %d events of %d, %v", len(data), ended.Events, events)
	}
	// Resume after the fifth event.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/work/sessions/"+se.ID+"/events", nil)
	req.Header.Set("Last-Event-ID", "4")
	if r2, err := http.DefaultClient.Do(req); err == nil {
		b, _ := io.ReadAll(r2.Body)
		r2.Body.Close()
		if n := strings.Count(string(b), "data: {\"time\""); n != ended.Events-5 {
			t.Errorf("resumed with %d events", n)
		}
	}

	for _, p := range []string{"/api/work/sessions", "/api/work/sessions/" + se.ID, "/api/work/sessions/" + se.ID + "/raw"} {
		r, err := http.Get(srv.URL + p)
		if err != nil || r.StatusCode != http.StatusOK {
			t.Errorf("GET %s: %v %d", p, err, r.StatusCode)
		}
	}
	if r, _ := http.Get(srv.URL + "/api/work/sessions/zzz"); r.StatusCode != http.StatusNotFound {
		t.Errorf("bad id: %d", r.StatusCode)
	}
	if r := post("/api/work/sessions/"+se.ID+"/stop", "", true); r.StatusCode != http.StatusConflict {
		t.Errorf("stop finished: %d", r.StatusCode)
	}
	if r := post("/api/work/sessions/"+se.ID+"/remove", `{}`, true); r.StatusCode != http.StatusConflict {
		t.Errorf("remove unpushed: %d", r.StatusCode)
	}
	if r := post("/api/work/sessions/"+se.ID+"/remove", `{"discard":true}`, true); r.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(r.Body)
		t.Errorf("remove discard: %d %s", r.StatusCode, b)
	}
}
