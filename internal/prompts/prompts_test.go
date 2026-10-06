package prompts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

func TestBuiltinsAreValid(t *testing.T) {
	want := []string{"builder", "critic", "scout", "researcher", "reviewer", "braindump"}
	got := map[string]Template{}
	for _, b := range Builtins() {
		got[b.ID] = b
		if b.Source != SourceBuiltin || b.ReadOnly {
			t.Errorf("%s: source %q read-only %v", b.ID, b.Source, b.ReadOnly)
		}
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("missing built-in %q", id)
		}
	}
	b := got["builder"]
	for _, id := range []string{Role, Constraints, Verify, Report} {
		if strings.TrimSpace(b.Body(id)) == "" {
			t.Errorf("builder has an empty %s section", id)
		}
	}
	if got["braindump"].Target != TargetCouncil || b.Target != TargetWork {
		t.Errorf("targets: braindump %q builder %q", got["braindump"].Target, b.Target)
	}
}

func TestRenderExpandsVariablesAndKeepsUnknownOnes(t *testing.T) {
	secs := []Section{
		{ID: Role, Body: "You build {{project.name}}."},
		{ID: Task, Body: "Fix {{ project.repo }} on {{date}}. Keep {{not.a.var}} and {{x}}"},
		{ID: Verify, Body: ""},
	}
	r := Render(secs, map[string]string{"project.name": "Demo", "project.repo": "you/demo"}, nil)
	if !strings.HasPrefix(r.Text, "You build Demo.\n\n# Task\n\nFix you/demo on {{date}}.") {
		t.Errorf("text:\n%s", r.Text)
	}
	if strings.Contains(r.Text, "## Verify") {
		t.Error("an empty section was rendered")
	}
	if strings.Join(r.Unfilled, ",") != "date,not.a.var,x" {
		t.Errorf("unfilled %v", r.Unfilled)
	}
	if r.Tokens != (r.Chars+3)/4 || r.Chars == 0 {
		t.Errorf("chars %d tokens %d", r.Chars, r.Tokens)
	}
}

func TestRenderPutsSourcesInContext(t *testing.T) {
	src := []Source{{Label: "Repo map: Demo", Text: "go.mod\ncmd/  (2 files)", Format: FormatCode}, {Label: "Page: Notes", Text: "hello"}}
	r := Render([]Section{{ID: Context, Body: "Background."}, {ID: Task, Body: "Do it."}}, nil, src)
	for _, want := range []string{"## Context\n\nBackground.\n\n### Repo map: Demo\n\n```\ngo.mod", "### Page: Notes\n\nhello", "# Task\n\nDo it."} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, r.Text)
		}
	}
	// A template without a context section still carries its sources.
	r = Render([]Section{{ID: Task, Body: "Do it."}}, nil, src)
	if !strings.HasPrefix(r.Text, "## Context\n\n### Repo map") {
		t.Errorf("text:\n%s", r.Text)
	}
}

func codes(fs []Finding) map[string]string {
	out := map[string]string{}
	for _, f := range fs {
		out[f.Code] = f.Severity
	}
	return out
}

func TestLint(t *testing.T) {
	bare := []Section{{ID: Task, Body: "Make the button blue."}}
	r := Render(bare, nil, nil)
	got := codes(Lint(bare, r, nil, TargetCopy))
	for _, c := range []string{"no-verify", "no-report", "no-done-criteria"} {
		if got[c] != SevWarning {
			t.Errorf("%s: %q in %v", c, got[c], got)
		}
	}
	// Work adds verify and report itself.
	got = codes(Lint(bare, r, nil, TargetWork))
	if got["no-verify"] != SevInfo || got["no-report"] != SevInfo {
		t.Errorf("work target: %v", got)
	}

	full := []Section{
		{ID: Task, Body: "Make the button blue.\n\nDone when the button is blue — proof: screenshot."},
		{ID: Verify, Body: "Run npm run build."},
		{ID: Report, Body: "Three buckets."},
	}
	if fs := Lint(full, Render(full, nil, nil), nil, TargetCopy); len(fs) != 0 {
		t.Errorf("a complete prompt has findings: %+v", fs)
	}

	empty := []Section{{ID: Task, Body: "  "}}
	if fs := Lint(empty, Render(empty, nil, nil), nil, TargetCopy); !Blocked(fs) || codes(fs)["empty-task"] != SevError {
		t.Errorf("empty task: %+v", fs)
	}

	// Key-shaped strings are built from parts, so no scanner mistakes the
	// test for a leaked key.
	secret := []Section{{ID: Task, Body: "Use " + "gh" + "p_" + strings.Repeat("a1B2", 9) + " and password = hunter2hunter2 — done when it works, proof: test"}}
	fs := Lint(secret, Render(secret, nil, nil), nil, TargetCopy)
	n := 0
	for _, f := range fs {
		if f.Code == "secret" {
			n++
			if strings.Contains(f.Message, strings.Repeat("a1B2", 9)) {
				t.Errorf("the secret is echoed whole: %s", f.Message)
			}
		}
	}
	if n != 2 {
		t.Errorf("want 2 secret findings, got %+v", fs)
	}
	for _, s := range []string{"-----BEGIN " + "RSA PRIVATE" + " KEY-----", "AK" + "IA" + strings.Repeat("Q", 16), "ey" + "J" + strings.Repeat("a", 12) + "." + strings.Repeat("b", 12) + "." + strings.Repeat("c", 12)} {
		if len(FindSecrets("x "+s+" y")) != 1 {
			t.Errorf("not flagged: %.12s…", s)
		}
	}
	if len(FindSecrets("token: env:GITHUB_TOKEN and sk-short")) != 0 {
		t.Error("a secret reference or a short sk- word was flagged")
	}
}

func TestLintBlocksConfidentialForProviders(t *testing.T) {
	secs := []Section{{ID: Task, Body: "Summarise — done when summarised, proof: read it"}, {ID: Verify, Body: "x"}, {ID: Report, Body: "y"}}
	src := []Source{{Label: "Page: Secret plan", Text: "plan", Confidential: true}}
	r := Render(secs, nil, src)
	for _, target := range []string{TargetWork, TargetCouncil} {
		fs := Lint(secs, r, src, target)
		if !Blocked(fs) || codes(fs)["confidential"] != SevError {
			t.Errorf("%s: not blocked: %+v", target, fs)
		}
	}
	if fs := Lint(secs, r, src, TargetCopy); Blocked(fs) {
		t.Errorf("copy is local and must not be blocked: %+v", fs)
	}
	long := []Section{{ID: Task, Body: strings.Repeat("x", MaxCouncilInput+1)}}
	if fs := Lint(long, Render(long, nil, nil), nil, TargetCouncil); codes(fs)["too-long"] != SevError {
		t.Errorf("a braindump over the council's limit: %+v", fs)
	}
}

func TestForWorkDropsWhatWorkAdds(t *testing.T) {
	b, _ := Builtin("builder")
	secs := ForWork(append(b.Sections[:0:0], b.Sections...))
	for _, s := range secs {
		if s.ID == Role {
			t.Error("the role was kept")
		}
		if s.ID == Constraints && strings.TrimSpace(s.Body) != "" {
			t.Errorf("Work's own constraints were kept: %q", s.Body)
		}
	}
	mine := ForWork([]Section{{ID: Constraints, Body: b.Body(Constraints) + "\n- Do not touch the CLI."}})
	if strings.TrimSpace(mine[0].Body) != "- Do not touch the CLI." {
		t.Errorf("own constraint: %q", mine[0].Body)
	}
}

func TestStoreOverridesAndSnippets(t *testing.T) {
	s := &Store{Dir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }}
	all := s.Templates()
	if len(all) != len(Builtins())+3 {
		t.Fatalf("%d templates", len(all))
	}
	c, err := s.Template("council-critique")
	if err != nil || !c.ReadOnly || c.Source != SourceCouncil || !strings.Contains(c.Body(Role), `"verdict"`) || strings.HasPrefix(c.Body(Role), "<!--") {
		t.Fatalf("council template: %+v %v", c, err)
	}
	if !strings.Contains(c.Note, "v1") {
		t.Errorf("note %q", c.Note)
	}
	if _, err := s.SaveTemplate(Template{ID: "council-critique", Name: "x", Target: TargetCopy, Sections: []Section{{ID: Task}}}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("saving over a council prompt: %v", err)
	}

	scout, _ := Builtin("scout")
	scout.Sections[0].Body = "You are my scout."
	saved, err := s.SaveTemplate(scout)
	if err != nil || !saved.Overrides || saved.Builtin == nil || saved.Body(Role) != "You are my scout." || saved.Source != SourceUser {
		t.Fatalf("override: %+v %v", saved, err)
	}
	if len(s.Templates()) != len(all) {
		t.Error("an override added a template instead of replacing one")
	}
	if _, err := s.SaveTemplate(Template{ID: "mine", Name: "Mine", Target: TargetCopy, Sections: []Section{{ID: "nope"}}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown section: %v", err)
	}
	if _, err := s.SaveTemplate(Template{ID: "mine", Name: "Mine", Target: TargetCopy, Sections: []Section{{ID: Task, Body: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if len(s.Templates()) != len(all)+1 {
		t.Error("a user template is not listed")
	}
	if err := s.DeleteTemplate("scout"); err != nil {
		t.Fatal(err)
	}
	if back, _ := s.Template("scout"); back.Overrides || back.Source != SourceBuiltin {
		t.Errorf("the built-in did not come back: %+v", back)
	}
	if err := s.DeleteTemplate("builder"); !errors.Is(err, ErrReadOnly) {
		t.Errorf("deleting a built-in: %v", err)
	}

	if _, err := s.SaveSnippet(Snippet{ID: "tests", Name: "Run tests", Section: Verify, Body: "- go test ./..."}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSnippet(Snippet{ID: "bad", Name: "x", Section: "nope", Body: "y"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad section: %v", err)
	}
	if sn := s.Snippets(); len(sn) != 1 || sn[0].Body != "- go test ./..." || sn[0].Updated.IsZero() {
		t.Errorf("snippets %+v", sn)
	}
	if err := s.DeleteSnippet("tests"); err != nil || len(s.Snippets()) != 0 {
		t.Errorf("delete snippet: %v", err)
	}
}

// fixture is a vault, a projects list, a checkout and a council directory.
type fixture struct {
	r     *Resolver
	vault *memory.Vault
	repo  string
	home  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	repo := filepath.Join(home, "code", "demo")
	for _, f := range []string{"go.mod", "README.md", "cmd/demo/main.go", "internal/a/a.go", "internal/a/b/c.go", "node_modules/x/index.js", ".git/HEAD", "dist/app.js", ".claude/settings.json", ".env", "CONTRIBUTING.md", "docs/BETA-CONTRACTS.md"} {
		p := filepath.Join(repo, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("contents of "+f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := memory.Open(filepath.Join(home, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]*memory.Page{
		"Notes/open.md":       {Front: map[string]any{"title": "Open notes"}, Body: "Public plan.\n"},
		"Notes/secret.md":     {Front: map[string]any{"title": "Secret plan", "confidential": true}, Body: "Private.\n"},
		"Inbox/brief.md":      {Front: map[string]any{"type": "brief", "title": "Do the thing"}, Body: "# Do the thing\n\n## Outcome\nThe thing is done.\n"},
		"Inbox/conf-brief.md": {Front: map[string]any{"confidential": true}, Body: "# Hidden\n"},
	}
	for p, pg := range pages {
		pg.Path = p
		if err := v.Write(pg); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []boards.Card{
		{Title: "Do the thing", Column: "Ready", Project: "demo", Memory: "Inbox/brief.md", Council: "20260101-000000-aaaaaa"},
		{Title: "Hidden work", Column: "Ready", Project: "demo", Memory: "Inbox/conf-brief.md"},
	} {
		if _, err := boards.AddCard(v, boards.DefaultBoard, c); err != nil {
			t.Fatal(err)
		}
	}
	cdir := filepath.Join(home, "council")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	sess := council.Session{
		ID: "20260101-000000-aaaaaa", Project: "demo", Title: "Do the thing", Status: council.StatusApproved,
		Brief: "# Do the thing\n\n## Outcome\nThe thing is done.\n\n## Risks\n- none\n", BriefPath: "Inbox/brief.md",
		Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, _ := json.Marshal(sess)
	if err := os.WriteFile(filepath.Join(cdir, sess.ID+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	list := &projects.List{Configured: true, Projects: []projects.Project{
		{ID: "demo", Name: "Demo", Category: "product", Type: "cli", Status: "active", Visibility: "public", Repo: "you/demo", LocalPath: repo, Summary: "A demo  tool.",
			Assessment: &assess.Assessment{Kind: "cli-library", Answers: map[string]string{"language": "go"}}},
		{ID: "vault", Name: "Vault", Category: "tool", Type: "cli", Status: "active", Visibility: "confidential", LocalPath: repo},
	}}
	return &fixture{
		r: &Resolver{
			Projects: func() (*projects.List, error) { return list, nil },
			Vault:    func() (*memory.Vault, error) { return v, nil },
			Council:  council.New(cdir, nil, nil),
			Home:     home,
		},
		vault: v, repo: repo, home: home,
	}
}

func TestContextSources(t *testing.T) {
	f := newFixture(t)
	p, err := f.r.Resolve(Ref{Kind: KindProject, Project: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	local := "~" + string(filepath.Separator) + filepath.Join("code", "demo")
	for _, want := range []string{"Project: Demo (id demo)", "Local checkout: " + local, "Summary: A demo tool.", "Assessment: kind cli-library", "Checks to run:"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("project text lacks %q:\n%s", want, p.Text)
		}
	}
	if strings.Contains(p.Text, f.home) || p.Confidential || p.Chars == 0 || p.Tokens == 0 {
		t.Errorf("project source: %+v", p)
	}
	if c, _ := f.r.Resolve(Ref{Kind: KindProject, Project: "vault"}); !c.Confidential {
		t.Error("a confidential project is not flagged")
	}

	pg, err := f.r.Resolve(Ref{Kind: KindPage, Path: "Notes/open.md"})
	if err != nil || pg.Label != "Page: Open notes" || pg.Text != "Public plan." || pg.Confidential {
		t.Errorf("page: %+v %v", pg, err)
	}
	if sp, _ := f.r.Resolve(Ref{Kind: KindPage, Path: "Notes/secret.md"}); !sp.Confidential {
		t.Error("a confidential page is not flagged")
	}
	if _, err := f.r.Resolve(Ref{Kind: KindPage, Path: "../x.md"}); !errors.Is(err, ErrBadRef) {
		t.Errorf("an escaping path: %v", err)
	}

	rules, err := f.r.Resolve(Ref{Kind: KindRules, Project: "demo"})
	if err != nil || strings.Join(rules.Files, ",") != "docs/BETA-CONTRACTS.md,CONTRIBUTING.md" || !strings.Contains(rules.Text, "#### CONTRIBUTING.md") {
		t.Errorf("rules: %+v %v", rules, err)
	}

	repo, err := f.r.Resolve(Ref{Kind: KindRepo, Project: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cmd/  (1 file)", "  demo/  (1 file)", "internal/  (2 files)", "  a/  (2 files)", "go.mod", "8 files in all"} {
		if !strings.Contains(repo.Text, want) {
			t.Errorf("repo map lacks %q:\n%s", want, repo.Text)
		}
	}
	for _, bad := range []string{"node_modules", "HEAD", "dist/", ".claude/", ".env", "c.go", "b/"} {
		if strings.Contains(strings.SplitN(repo.Text, "\n", 2)[1], bad) {
			t.Errorf("repo map shows %q:\n%s", bad, repo.Text)
		}
	}
	if repo.Format != FormatCode {
		t.Errorf("format %q", repo.Format)
	}

	b, _ := boards.Get(f.vault, boards.DefaultBoard)
	card, err := f.r.Resolve(Ref{Kind: KindCard, ID: b.Cards[0].ID})
	if err != nil || !strings.Contains(card.Text, "Its brief (Inbox/brief.md)") || card.Confidential {
		t.Errorf("card: %+v %v", card, err)
	}
	if hidden, _ := f.r.Resolve(Ref{Kind: KindCard, ID: b.Cards[1].ID}); !hidden.Confidential {
		t.Error("a card whose brief is confidential is not flagged")
	}

	dec, err := f.r.Resolve(Ref{Kind: KindDecisions, Project: "demo"})
	if err != nil || !strings.Contains(dec.Text, "2026-01-01 · Do the thing (approved), brief Inbox/brief.md") || !strings.Contains(dec.Text, "Outcome: The thing is done.") {
		t.Errorf("decisions: %+v %v", dec, err)
	}
	if _, err := f.r.Resolve(Ref{Kind: KindDecisions, Project: "vault"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("no decisions: %v", err)
	}
	if _, err := f.r.Resolve(Ref{Kind: "nope"}); !errors.Is(err, ErrBadRef) {
		t.Errorf("unknown kind: %v", err)
	}
	if v := f.r.Vars("demo"); v["project.local_path"] != local || v["project.kind"] != "cli-library" || v["project.checks"] == "" {
		t.Errorf("vars %v", v)
	}
}

func TestImproveRefusesBeforeAnyProvider(t *testing.T) {
	f := newFixture(t)
	im := &Improver{Resolver: f.r}
	for _, c := range []struct {
		req  ImproveRequest
		want error
	}{
		{ImproveRequest{Text: ""}, ErrBadRef},
		{ImproveRequest{Text: "fix it", Project: "vault"}, ErrConfidential},
		{ImproveRequest{Text: "see [[Notes/secret]]"}, ErrConfidential},
		{ImproveRequest{Text: "key " + "s" + "k-" + strings.Repeat("x", 30)}, ErrBadRef},
		{ImproveRequest{Text: "fix it", Provider: "gpt"}, ErrBadRef},
	} {
		if _, err := im.Improve(t.Context(), c.req); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.req, err, c.want)
		}
	}
}

// Each Improve call leaves a record the Usage page reads (internal/usage
// reads "created" and "usage" from <data dir>/prompts/runs/*.json).
func TestImproveRecordsUsage(t *testing.T) {
	dir := t.TempDir()
	im := &Improver{RunsDir: dir, Now: func() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC) }}
	im.record(agentexec.Usage{Provider: "claude", Model: "haiku", CostUSD: 0.002, DurationMS: 900}, nil)
	im.record(agentexec.Usage{Provider: "claude"}, errors.New("not on PATH")) // nothing reported: no record
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("%d records", len(files))
	}
	data, _ := os.ReadFile(files[0])
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || string(raw["created"]) != `"2026-02-03T04:05:06Z"` || !strings.Contains(string(raw["usage"]), `"cost_usd":0.002`) {
		t.Errorf("record %s", data)
	}
	(&Improver{}).record(agentexec.Usage{CostUSD: 1}, nil) // no RunsDir: nothing, no panic
}

func TestParseSections(t *testing.T) {
	got, err := ParseSections("Sure:\n```json\n{\"role\": \"You fix bugs.\", \"task\": \"Fix it.\", \"verify\": [\"go test\", \"- go vet\"], \"extra\": \"x\", \"report\": \"\"}\n```")
	if err != nil || len(got) != 3 || got[0].ID != Role || got[1].ID != Task || got[2].Body != "- go test\n- go vet" {
		t.Errorf("json: %+v %v", got, err)
	}
	got, err = ParseSections("## Task\nFix it.\n\n## Verify\nRun tests.\n## Other\nx")
	if err != nil || len(got) != 2 || got[1].Body != "Run tests." {
		t.Errorf("markdown: %+v %v", got, err)
	}
	if _, err := ParseSections("no idea"); err == nil {
		t.Error("an answer without sections was accepted")
	}
}

func TestRoutes(t *testing.T) {
	f := newFixture(t)
	mux := http.NewServeMux()
	Register(mux, &API{Store: &Store{Dir: t.TempDir()}, Resolver: f.r, Improver: &Improver{Resolver: f.r}, Now: func() time.Time { return time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC) }})
	do := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("GET", "/api/prompts/templates", "", false); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"council-propose"`) || !strings.Contains(rec.Body.String(), `"variables"`) {
		t.Errorf("templates: %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct{ method, path, body string }{
		{"PUT", "/api/prompts/templates/x", `{"name":"X","target":"copy","sections":[{"id":"task","body":"y"}]}`},
		{"DELETE", "/api/prompts/templates/x", ""},
		{"PUT", "/api/prompts/snippets/x", `{"name":"X","body":"y"}`},
		{"POST", "/api/prompts/improve", `{"text":"x"}`},
	} {
		if rec := do(c.method, c.path, c.body, false); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without confirm: %d", c.method, c.path, rec.Code)
		}
	}
	if rec := do("PUT", "/api/prompts/templates/x", `{"name":"X","target":"copy","sections":[{"id":"task","body":"y"}]}`, true); rec.Code != 200 {
		t.Errorf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := do("PUT", "/api/prompts/templates/council-propose", `{"name":"X","target":"copy","sections":[{"id":"task","body":"y"}]}`, true); rec.Code != http.StatusConflict {
		t.Errorf("save over council: %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/api/prompts/context?kind=page&path=Notes/secret.md", "", false); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"confidential":true`) {
		t.Errorf("context: %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/api/prompts/context?kind=rules&project=nope", "", false); rec.Code != 404 {
		t.Errorf("context for an unknown project: %d", rec.Code)
	}
	if rec := do("POST", "/api/prompts/improve", `{"text":"x","project":"vault"}`, true); rec.Code != http.StatusForbidden {
		t.Errorf("improve a confidential project: %d %s", rec.Code, rec.Body)
	}

	body := `{"sections":[{"id":"role","body":"You build {{project.name}}."},{"id":"task","body":"Fix it on {{date}}. Done when fixed — proof: go test"},{"id":"verify","body":"go test"},{"id":"report","body":"buckets"}],` +
		`"context":[{"kind":"page","path":"Notes/secret.md"},{"kind":"project","project":"demo"}],"project":"demo","target":"council"}`
	rec := do("POST", "/api/prompts/render", body, false)
	var out RenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 200 {
		t.Fatalf("render: %d %s", rec.Code, rec.Body)
	}
	if !out.Blocked || codes(out.Lint)["confidential"] != SevError || len(out.Sources) != 2 || !out.Sources[0].Confidential {
		t.Errorf("render: blocked %v lint %+v sources %+v", out.Blocked, out.Lint, out.Sources)
	}
	if !strings.HasPrefix(out.Text, "You build Demo.") || !strings.Contains(out.Text, "Fix it on 2026-03-04.") || out.TokensNote == "" {
		t.Errorf("text:\n%s", out.Text)
	}
	rec = do("POST", "/api/prompts/render", strings.Replace(body, `"target":"council"`, `"target":"copy"`, 1), false)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Blocked {
		t.Errorf("copy blocked: %+v", out.Lint)
	}
	rec = do("POST", "/api/prompts/render", strings.Replace(body, `"target":"council"`, `"target":"work"`, 1), false)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if strings.Contains(out.Text, "You build Demo.") || codes(out.Lint)["work-adds"] != SevInfo {
		t.Errorf("work target keeps the role: %s %+v", out.Text, out.Lint)
	}
	if rec := do("POST", "/api/prompts/render", `{"sections":[{"id":"bogus","body":"x"}],"target":"copy"}`, false); rec.Code != 400 {
		t.Errorf("bad section: %d", rec.Code)
	}
}
