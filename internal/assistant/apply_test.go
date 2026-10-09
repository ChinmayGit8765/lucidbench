package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/setup"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

func TestImportFindsAgentsAndReadsOnlyTheirFields(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".claude", "agents", "fixture-helper.md"), "---\nname: fixture-helper\ndescription: Answers questions about the fixture.\nmodel: haiku\ntools: Read\n---\n\nYou help with the fixture. Be brief.\n")
	writeFile(t, filepath.Join(home, ".claude", "agents", "no-front.md"), "Just text, no front matter.\n")
	writeFile(t, filepath.Join(home, ".claude", ".credentials.json"), `{"token": "never-read"}`)
	writeFile(t, filepath.Join(home, ".codex", "agents", "fixture-reviewer.toml"), "name = \"fixture-reviewer\"\ndescription = \"Reviews fixture diffs.\"\nmodel = \"gpt-fixture\"\ndeveloper_instructions = \"\"\"\nReview the diff. List blockers first.\n\"\"\"\n")
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), `{"token": "never-read"}`)
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), "[profiles.fixture]\nmodel = \"x\"\n")
	writeFile(t, filepath.Join(home, ".grok", "agents", "fixture-scout.md"), "---\nname: fixture-scout\ndescription: >\n  Finds where things live.\nmodel: inherit\n---\nLook, do not touch.\n")
	writeFile(t, filepath.Join(home, ".grok", "personas", "fixture-terse.toml"), "instructions = \"Answer in one line.\"\ndescription = \"Terse answers.\"\n")

	e := newEnv(t, nil, projectsYAML)
	e.svc.Roots = func() accounts.Roots { return accounts.Roots{Home: home} }
	l := e.svc.ImportCandidates()
	if len(l.Candidates) != 4 {
		t.Fatalf("candidates %+v", l.Candidates)
	}
	byName := map[string]Candidate{}
	for _, c := range l.Candidates {
		byName[c.Name] = c
		if strings.Contains(c.Description, "never-read") || strings.Contains(c.File, string(filepath.Separator)) {
			t.Errorf("candidate leaks: %+v", c)
		}
	}
	if c := byName["fixture-helper"]; c.Provider != "claude" || c.Model != "haiku" || !c.HasBody || c.Description != "Answers questions about the fixture." {
		t.Errorf("claude agent %+v", c)
	}
	if c := byName["fixture-reviewer"]; c.Provider != "codex" || c.Model != "gpt-fixture" || !c.HasBody {
		t.Errorf("codex agent %+v", c)
	}
	if c := byName["fixture-scout"]; c.Provider != "grok" || c.Model != "" || c.Description != "Finds where things live." {
		t.Errorf("grok agent %+v", c)
	}
	if c := byName["fixture-terse"]; c.Provider != "grok" || c.Source != "grok personas" {
		t.Errorf("grok persona %+v", c)
	}
	for _, n := range l.Notes {
		if n.Found == 0 {
			t.Errorf("note %+v", n)
		}
	}

	// Without its instructions, the persona is the description.
	b, err := e.svc.Import(ImportRequest{Key: byName["fixture-helper"].Key})
	if err != nil {
		t.Fatal(err)
	}
	if b.Persona != "Answers questions about the fixture." || b.Provider != "claude" || b.Model != "haiku" || b.Source != "claude agents" {
		t.Errorf("imported %+v", b)
	}
	b, err = e.svc.Import(ImportRequest{Key: byName["fixture-reviewer"].Key, Persona: true})
	if err != nil {
		t.Fatal(err)
	}
	if b.Persona != "Review the diff. List blockers first." || b.ID != "fixture-reviewer" {
		t.Errorf("imported with instructions %+v", b)
	}
	if _, err := e.svc.Import(ImportRequest{Key: "claude:0:../../.credentials.json"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a key outside the list: %v", err)
	}

	// A machine with none of the folders says so per provider.
	e.svc.Roots = func() accounts.Roots { return accounts.Roots{Home: t.TempDir()} }
	l = e.svc.ImportCandidates()
	if len(l.Candidates) != 0 || len(l.Notes) != 3 || !strings.Contains(l.Notes[2].Note, "Grok CLI exposes no saved agents") {
		t.Errorf("empty machine %+v", l)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "init", "-q", "-b", "main")
	for _, kv := range [][2]string{{"user.name", "Test"}, {"user.email", "test@example.com"}, {"commit.gpgsign", "false"}, {"core.autocrlf", "false"}} {
		gitRun(t, repo, "config", kv[0], kv[1])
	}
	writeFile(t, filepath.Join(repo, "README.md"), "# demo\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// send makes the requests a proposal names against mux, with the confirm
// header, as the web UI does, and returns the last answer.
func send(t *testing.T, mux http.Handler, p Proposal) *httptest.ResponseRecorder {
	t.Helper()
	if !p.Valid || len(p.Requests) == 0 {
		t.Fatalf("%s is not appliable: %+v", p.Action, p)
	}
	var rec *httptest.ResponseRecorder
	for _, r := range p.Requests {
		body, _ := json.Marshal(r.Body)
		req := httptest.NewRequest(r.Method, r.Path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Lucid-Confirm", "yes")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code >= 300 {
			t.Fatalf("%s %s: %d %s", r.Method, r.Path, rec.Code, rec.Body.String())
		}
	}
	return rec
}

// TestApplyEachKindThroughTheRealHandlers applies every action kind the
// way the UI does: the proposal's requests, sent to the real routes.
func TestApplyEachKindThroughTheRealHandlers(t *testing.T) {
	f := newFake(t)
	repo := newRepo(t)
	newRepoDir := newRepo(t)
	data := t.TempDir()
	projFile := filepath.Join(data, "projects.yaml")
	writeFile(t, projFile, projectsYAML+"  - id: tool\n    name: Tooling\n    category: tool\n    status: active\n    visibility: private\n    local_path: "+quote(repo)+"\n")
	load := func() (*projects.List, error) { return projects.LoadFrom(projFile, ""), nil }
	v, err := memory.Open(filepath.Join(data, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	vault := func() (*memory.Vault, error) { return v, nil }
	mux := http.NewServeMux()
	memory.Register(mux, vault)
	boards.Register(mux, vault)
	setup.Register(mux, &setup.Service{DataDir: data, ProjectsPath: func() (string, error) { return projFile, nil }})
	csvc := council.New(filepath.Join(data, "council"), vault, &agentexec.Runner{LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	csvc.Projects = load
	council.Register(mux, csvc)
	wsvc := work.New(filepath.Join(data, "work"), f.runner(), vault, load)
	work.Register(mux, wsvc)

	svc := &Service{Dir: filepath.Join(data, "assistant"), Projects: load, Vault: vault}
	check := func(a Action) Proposal {
		t.Helper()
		p, err := svc.Check(CheckRequest{Action: a})
		if err != nil {
			t.Fatal(err)
		}
		return *p
	}

	// create_card with a body: a page in Inbox, then the card linking it.
	send(t, mux, check(act(ActCreateCard, map[string]any{"project": "demo", "title": "Write the README", "body": "Install and usage.", "column": "Ready"})))
	b, err := boards.Get(v, boards.DefaultBoard)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Cards) != 1 || b.Cards[0].Title != "Write the README" || b.Cards[0].Column != "Ready" || b.Cards[0].Project != "demo" || b.Cards[0].Memory != "Inbox/write-the-readme.md" {
		t.Fatalf("card %+v", b.Cards)
	}
	if pg, err := v.Read("Inbox/write-the-readme.md"); err != nil || !strings.Contains(pg.Body, "Install and usage.") {
		t.Errorf("card page %v %+v", err, pg)
	}
	// A second card with the same title gets a page of its own.
	if p := check(act(ActCreateCard, map[string]any{"title": "Write the README", "body": "again"})); p.Args["page"] != "Inbox/write-the-readme-2.md" {
		t.Errorf("second page %v", p.Args["page"])
	}

	// create_idea: an Inbox page and an Inbox card labelled idea.
	send(t, mux, check(act(ActCreateIdea, map[string]any{"title": "Rename photos by date", "body": "A small tool."})))
	b, _ = boards.Get(v, boards.DefaultBoard)
	if c := b.Cards[0]; c.Title != "Rename photos by date" || c.Column != "Inbox" || len(c.Labels) != 1 || c.Labels[0] != "idea" {
		t.Errorf("idea card %+v", b.Cards)
	}

	// create_page, then the same path again is refused.
	send(t, mux, check(act(ActCreatePage, map[string]any{"path": "Notes/plan", "markdown": "# Plan\n"})))
	if pg, err := v.Read("Notes/plan.md"); err != nil || !strings.Contains(pg.Body, "# Plan") {
		t.Errorf("page %v", err)
	}
	if p := check(act(ActCreatePage, map[string]any{"path": "Notes/plan", "markdown": "# Again\n"})); p.Valid {
		t.Error("an existing page passed the check before Apply")
	}

	// create_project with a checkout: appended to projects.yaml by setup.
	send(t, mux, check(act(ActCreateProject, map[string]any{"id": "fresh", "name": "Fresh", "kind": "portfolio", "local_path": newRepoDir})))
	l, _ := load()
	if p := l.Find("fresh"); p == nil || p.Category != "portfolio" || p.LocalPath != newRepoDir || len(l.Projects) != 4 {
		t.Errorf("projects %+v errors %v", l.Projects, l.Errors)
	}
	// Without one there is nothing to call: the snippet is the apply.
	if p := check(act(ActCreateProject, map[string]any{"id": "later", "name": "Later", "kind": "experiment"})); !p.Valid || len(p.Requests) != 0 || !strings.Contains(p.Snippet, "id: later") {
		t.Errorf("project without a checkout %+v", p)
	}
	// projects.yaml is never rewritten: needs and links are snippets to paste.
	for _, a := range []Action{
		act(ActAddNeeds, map[string]any{"project": "demo", "needs": []any{"a logo", map[string]any{"what": "an API", "from": "tool"}}}),
		act(ActLinkBuildsInto, map[string]any{"project": "tool", "target": "demo"}),
	} {
		p := check(a)
		if !p.Valid || len(p.Requests) != 0 || p.Snippet == "" {
			t.Errorf("%s %+v", a.Action, p)
		}
	}

	// start_council: the council's own route (no provider here, so it fails fast).
	rec := send(t, mux, check(act(ActStartCouncil, map[string]any{"braindump": "a tool that renames photos", "project": "demo"})))
	var cs council.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &cs); err != nil || cs.ID == "" || cs.Project != "demo" {
		t.Fatalf("council %v %s", err, rec.Body.String())
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := csvc.Get(cs.ID); err == nil && s.Status != council.StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// start_work: a real session in a worktree, run by the fake claude.
	rec = send(t, mux, check(act(ActStartWork, map[string]any{"project": "tool", "prompt": "Add a CONTRIBUTING file", "provider": "claude", "model": "haiku"})))
	var se work.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &se); err != nil || se.ID == "" || se.Project != "tool" || se.Model != "haiku" {
		t.Fatalf("work %v %s", err, rec.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if done, err := wsvc.Wait(ctx, se.ID); err != nil || (done.Status != work.StatusWaiting && done.Status != work.StatusDone) {
		t.Errorf("work ended %v %+v", err, done.Status)
	}
	if _, err := wsvc.Remove(se.ID, true); err != nil {
		t.Logf("remove worktree: %v", err)
	}
}
