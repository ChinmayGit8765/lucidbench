package projects

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/assess"
)

// commented is a projects.yaml the way a person keeps one: comments, blank
// lines, odd indentation, flow lists and a trailing comment.
const commented = `# My projects. Do not reformat me.
version: 1

projects:
    # the game
  - id: demo-game
    name: Demo game     # the working title
    category: experiment
    type: game
    status: active
    visibility: private
    builds_into: [ ]


  - {id: demo-site, name: Demo site, category: portfolio, type: site, status: idea, visibility: public}
# end of file
`

var gameAnswers = map[string]string{
	"engine": "godot", "stage": "prototype", "audience": "just-me", "multiplayer": "false", "saves": "true",
}

type fixture struct {
	api  *AssessAPI
	mux  *http.ServeMux
	file string
	dir  string
}

func newFixture(t *testing.T, yamlText string) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{file: filepath.Join(root, FileName), dir: filepath.Join(root, AssessDirName)}
	if err := os.WriteFile(f.file, []byte(yamlText), 0o600); err != nil {
		t.Fatal(err)
	}
	f.api = &AssessAPI{
		Load: func() (*List, error) {
			l := LoadFrom(f.file, "")
			l.MergeAssessments(f.dir)
			return l, nil
		},
		Dir: func() (string, error) { return f.dir, nil },
		Now: func() time.Time { return time.Date(2026, 10, 6, 9, 30, 0, 0, time.UTC) },
	}
	f.mux = http.NewServeMux()
	RegisterAssessment(f.mux, f.api)
	return f
}

func (f *fixture) do(method, path string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	if method != http.MethodGet {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	w := httptest.NewRecorder()
	f.mux.ServeHTTP(w, req)
	return w
}

func TestConfirmLeavesProjectsYamlUntouched(t *testing.T) {
	f := newFixture(t, commented)
	before, _ := os.ReadFile(f.file)

	w := f.do("PUT", "/api/projects/demo-game/assessment", map[string]any{"kind": "game", "answers": gameAnswers})
	if w.Code != 200 {
		t.Fatalf("PUT = %d %s", w.Code, w.Body)
	}
	var v AssessmentView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Assessment == nil || v.Source != "app" || !v.Assessment.AssessedAt.Equal(f.api.Now()) {
		t.Fatalf("view = %+v", v)
	}
	if v.Suggestion == nil || !slices.Contains(v.Suggestion.CheckCommands, "godot") || v.Suggestion.Risk.Level != "medium" {
		t.Errorf("suggestion = %+v", v.Suggestion)
	}

	after, _ := os.ReadFile(f.file)
	if !bytes.Equal(before, after) {
		t.Fatalf("projects.yaml changed:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "demo-game.yaml")); err != nil {
		t.Fatalf("side file: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(f.dir, ".assess-*")); len(matches) != 0 {
		t.Errorf("temp files left behind: %v", matches)
	}

	// The next load merges it, and the list carries the chip fields.
	l, _ := f.api.load()
	p := l.Find("demo-game")
	if p.Assessment == nil || p.Assessment.Kind != "game" || p.Risk != "medium" || p.AssessmentFrom != "app" {
		t.Errorf("merged project = %+v", p)
	}
	if other := l.Find("demo-site"); other.Assessment != nil || other.Risk != "" {
		t.Errorf("other project picked up an assessment: %+v", other)
	}
	if len(l.Errors) != 0 {
		t.Errorf("errors = %v", l.Errors)
	}

	// GET reads it back, with the kind preselected from the type for the other.
	w = f.do("GET", "/api/projects/demo-site/assessment", nil)
	var v2 AssessmentView
	_ = json.Unmarshal(w.Body.Bytes(), &v2)
	if v2.Assessment != nil || v2.SuggestedKind != "site" {
		t.Errorf("unassessed view = %+v", v2)
	}
}

func TestConfirmValidation(t *testing.T) {
	f := newFixture(t, commented)
	for name, c := range map[string]struct {
		path string
		body map[string]any
		want int
	}{
		"unknown project": {"/api/projects/nope/assessment", map[string]any{"kind": "game", "answers": gameAnswers}, 404},
		"unknown kind":    {"/api/projects/demo-game/assessment", map[string]any{"kind": "spaceship", "answers": gameAnswers}, 400},
		"bad option":      {"/api/projects/demo-game/assessment", map[string]any{"kind": "game", "answers": map[string]string{"engine": "cryengine"}}, 400},
		"unknown field":   {"/api/projects/demo-game/assessment", map[string]any{"kind": "game", "answers": gameAnswers, "surprise": 1}, 400},
	} {
		if w := f.do("PUT", c.path, c.body); w.Code != c.want {
			t.Errorf("%s: %d %s, want %d", name, w.Code, w.Body, c.want)
		}
	}
	if _, err := os.Stat(f.dir); err == nil {
		t.Errorf("a rejected PUT created %s", f.dir)
	}
	// The client's assessed_at is ignored.
	w := f.do("PUT", "/api/projects/demo-game/assessment", map[string]any{"kind": "game", "answers": gameAnswers, "assessed_at": "1999-01-01T00:00:00Z"})
	var v AssessmentView
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if w.Code != 200 || v.Assessment.AssessedAt.Year() != 2026 {
		t.Errorf("PUT with assessed_at = %d %+v", w.Code, v.Assessment)
	}
	// No confirm header, no write.
	req := httptest.NewRequest("PUT", "/api/projects/demo-game/assessment", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Errorf("PUT without X-Lucid-Confirm = %d", rec.Code)
	}
}

func TestPreviewSavesNothing(t *testing.T) {
	f := newFixture(t, commented)
	w := f.do("POST", "/api/projects/demo-game/assessment/preview", map[string]any{"kind": "game", "answers": gameAnswers})
	var s assess.Suggestion
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || w.Code != 200 || !slices.Contains(s.CheckCommands, "godot") {
		t.Fatalf("preview = %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(f.dir); err == nil {
		t.Error("preview wrote the assessments folder")
	}
	if w := f.do("POST", "/api/projects/demo-game/assessment/preview", map[string]any{"kind": "game", "answers": map[string]string{}}); w.Code != 400 {
		t.Errorf("incomplete preview = %d", w.Code)
	}
}

func TestKindsRoute(t *testing.T) {
	f := newFixture(t, commented)
	w := f.do("GET", "/api/assess/kinds", nil)
	var ks []assess.Kind
	if err := json.Unmarshal(w.Body.Bytes(), &ks); err != nil || len(ks) != 9 || len(ks[0].Questions) < 5 {
		t.Fatalf("kinds = %d %v", len(ks), err)
	}
	if strings.Contains(w.Body.String(), "criteria") {
		t.Error("the rules should not be sent to the browser")
	}
}

func TestInlineAssessmentValidatedAndOverridden(t *testing.T) {
	f := newFixture(t, `version: 1
projects:
  - id: a
    name: A
    category: product
    type: web-app
    status: active
    visibility: public
    assessment:
      kind: web-app
      answers: {users: nobody-yet, accounts: "true", payments: "false", data: "false", tests: none, deploy: by-hand}
      assessed_at: 2026-09-01T00:00:00Z
  - id: b
    name: B
    category: product
    type: web-app
    status: active
    visibility: public
    assessment:
      kind: web-app
      answers: {users: everyone}
`)
	l, _ := f.api.load()
	a, b := l.Find("a"), l.Find("b")
	if a.Assessment == nil || a.AssessmentFrom != "projects.yaml" || a.Risk != "medium" {
		t.Errorf("a = %+v", a)
	}
	if b.Assessment != nil || len(l.Errors) != 1 || !strings.Contains(l.Errors[0], `project "b": assessment`) {
		t.Errorf("b = %+v, errors %v", b.Assessment, l.Errors)
	}
	// Confirming in the app wins over the inline block.
	answers := map[string]string{"users": "paying-customers", "accounts": "true", "payments": "true", "data": "true", "tests": "thorough", "deploy": "ci-pipeline"}
	if w := f.do("PUT", "/api/projects/a/assessment", map[string]any{"kind": "web-app", "answers": answers}); w.Code != 200 {
		t.Fatalf("PUT = %d %s", w.Code, w.Body)
	}
	l, _ = f.api.load()
	if a := l.Find("a"); a.AssessmentFrom != "app" || a.Risk != "high" {
		t.Errorf("a after confirm = %+v", a)
	}
}

func TestCorruptSideFileIsReportedNotFatal(t *testing.T) {
	f := newFixture(t, commented)
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "demo-game.yaml"), []byte("kind: [unclosed"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, _ := f.api.load()
	if len(l.Projects) != 2 || l.Find("demo-game").Assessment != nil || len(l.Errors) != 1 {
		t.Errorf("projects %d errors %v", len(l.Projects), l.Errors)
	}
}

func TestSuggestRefusesConfidentialAndMissingPath(t *testing.T) {
	root := t.TempDir()
	f := newFixture(t, `version: 1
projects:
  - {id: secret, name: Secret, category: product, type: game, status: active, visibility: confidential, local_path: `+jsonPath(root)+`}
  - {id: nopath, name: No path, category: product, type: game, status: active, visibility: private}
`)
	// A runner that would fail the test if it were ever reached.
	f.api.Runner = nil
	t.Setenv("PATH", "")
	if w := f.do("POST", "/api/projects/secret/assessment/suggest", map[string]any{"kind": "game"}); w.Code != 403 {
		t.Errorf("confidential = %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", "/api/projects/nopath/assessment/suggest", map[string]any{"kind": "game"}); w.Code != 400 {
		t.Errorf("no local_path = %d %s", w.Code, w.Body)
	}
	v := f.do("GET", "/api/projects/secret/assessment", nil)
	if strings.Contains(v.Body.String(), `"can_suggest":true`) {
		t.Errorf("confidential project offers suggestions: %s", v.Body)
	}
}

// jsonPath quotes a path for a YAML flow value on any platform.
func jsonPath(p string) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func TestRepoListing(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"go.mod", "README.md", ".env", "server.pem", "cmd/main.go", "node_modules/x/y.js", ".git/config", "a/b/c/deep.txt", "a/b/ok.txt"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte("x"), 0o600)
	}
	got := repoListing(root)
	for _, want := range []string{"go.mod", "README.md", "cmd/", "cmd/main.go", "a/b/"} {
		if !slices.Contains(got, want) {
			t.Errorf("listing missing %q: %v", want, got)
		}
	}
	for _, bad := range []string{".env", "server.pem", "node_modules/", ".git/", "a/b/c/deep.txt"} {
		if slices.Contains(got, bad) {
			t.Errorf("listing has %q: %v", bad, got)
		}
	}
}

func TestParseSuggestionKeepsOnlyValidAnswers(t *testing.T) {
	k, _ := assess.Get("game")
	got, err := parseSuggestion("Sure! ```json\n{\"engine\": \"Godot\", \"stage\": \"beta\", \"multiplayer\": \"yes\", \"saves\": true, \"platforms\": \"windows\", \"junk\": 1}\n```", k)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"engine": "godot", "multiplayer": "true", "saves": "true", "platforms": "windows"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, err := parseSuggestion("no json here", k); err == nil {
		t.Error("accepted a reply without JSON")
	}
}
