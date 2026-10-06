package sections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// The test binary doubles as a provider CLI: copied onto PATH as claude and
// run with LUCID_FAKE_SECTION set to a folder, it answers with reply.txt from
// that folder and records its arguments and input there.
func TestMain(m *testing.M) {
	if dir := os.Getenv("LUCID_FAKE_SECTION"); dir != "" {
		in, _ := io.ReadAll(os.Stdin)
		_ = os.WriteFile(filepath.Join(dir, "args.txt"), []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "stdin.txt"), in, 0o600)
		reply, _ := os.ReadFile(filepath.Join(dir, "reply.txt"))
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": string(reply), "total_cost_usd": 0.0031,
			"usage": map[string]any{"input_tokens": 2400, "output_tokens": 180}, "modelUsage": map[string]any{"claude-haiku-fake": map[string]any{}}})
		fmt.Println(string(b))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func valid() Section {
	return Normalise(Section{
		ID: "prs", Title: "Open PRs", Source: Source{API: "/api/work/sessions"}, View: "list",
		Filter: []Filter{{Field: "pr_state", Op: "in", Value: []any{"draft", "open"}}},
		Fields: []Field{{Path: "title"}, {Path: "pr_url", Format: "link"}},
	})
}

func TestTemplatesAreValid(t *testing.T) {
	if len(Templates) < 4 || len(Templates) > 6 {
		t.Errorf("%d templates", len(Templates))
	}
	for _, tp := range Templates {
		if err := Validate(Normalise(tp)); err != nil {
			t.Errorf("%s: %v", tp.ID, err)
		}
		if FindAPI(tp.Source.API) == nil {
			t.Errorf("%s reads %s", tp.ID, tp.Source.API)
		}
	}
	if err := Validate(valid()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRefuses(t *testing.T) {
	for name, edit := range map[string]func(*Section){
		"api off the list":  func(s *Section) { s.Source.API = "/api/work/sessions/abc/raw" },
		"a write route":     func(s *Section) { s.Source.API = "/api/themes/generate" },
		"a URL as api":      func(s *Section) { s.Source.API = "https://example.com/api" },
		"protocol relative": func(s *Section) { s.Source.API = "//example.com/x" },
		"config route":      func(s *Section) { s.Source.API = "/api/config" },
		"query in api":      func(s *Section) { s.Source.API = "/api/work/sessions?x=1" },
		"unknown param":     func(s *Section) { s.Source.Params = map[string]string{"refresh": "1"} },
		"param out of range": func(s *Section) {
			s.Source.API, s.Source.Params, s.Rows = "/api/usage/summary", map[string]string{"days": "400"}, "providers"
		},
		"param not an id": func(s *Section) {
			s.Source.API, s.Source.Params, s.Rows = "/api/boards/{board}", map[string]string{"board": "../config"}, "cards"
		},
		"missing required param": func(s *Section) { s.Source.API, s.Rows = "/api/boards/{board}", "cards" },
		"script in title":        func(s *Section) { s.Title = "<script>alert(1)</script>" },
		"markup in label":        func(s *Section) { s.Fields[0].Label = "<b>x</b>" },
		"url in description":     func(s *Section) { s.Description = "see https://example.com" },
		"js scheme":              func(s *Section) { s.Fields[0].Label = "javascript:alert(1)" },
		"url in filter value":    func(s *Section) { s.Filter[0] = Filter{Field: "title", Op: "eq", Value: "http://x.example"} },
		"expression path":        func(s *Section) { s.Fields[0].Path = "title.constructor()" },
		"bracket path":           func(s *Section) { s.Fields[0].Path = "a['b']" },
		"dollar path":            func(s *Section) { s.Fields[0].Path = "$.title" },
		"bad rows":               func(s *Section) { s.Rows = "cards[0]" },
		"unknown view":           func(s *Section) { s.View = "chart" },
		"unknown format":         func(s *Section) { s.Fields[0].Format = "html" },
		"no fields":              func(s *Section) { s.Fields = nil },
		"too many fields": func(s *Section) {
			for i := 0; i < 9; i++ {
				s.Fields = append(s.Fields, Field{Path: "title"})
			}
		},
		"bad id":             func(s *Section) { s.ID = "../../etc" },
		"long title":         func(s *Section) { s.Title = strings.Repeat("x", 81) },
		"refresh too fast":   func(s *Section) { s.RefreshS = 1 },
		"limit too big":      func(s *Section) { s.Limit = 500 },
		"unknown op":         func(s *Section) { s.Filter[0].Op = "regex" },
		"in without list":    func(s *Section) { s.Filter[0].Value = "open" },
		"project token here": func(s *Section) { s.Filter[0] = Filter{Field: "project", Op: "eq", Value: ProjectToken} },
		"other placeholder":  func(s *Section) { s.Filter[0] = Filter{Field: "project", Op: "eq", Value: "{user}"} },
		"bad placement":      func(s *Section) { s.Placement = "sidebar" },
		"bars need two":      func(s *Section) { s.View, s.Fields = "bars", append(s.Fields, Field{Path: "id"}) },
		"agg off a stat":     func(s *Section) { s.Fields[0].Agg = "sum" },
		"filter on markdown": func(s *Section) { s.View = "markdown" },
		"control character":  func(s *Section) { s.Title = "a\x07b" },
	} {
		s := valid()
		s.Fields = append([]Field{}, s.Fields...)
		s.Filter = append([]Filter{}, s.Filter...)
		edit(&s)
		err := Validate(s)
		if err == nil || !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
	// The project token is fine on a project section.
	s := valid()
	s.Placement = PlaceProject
	s.Filter = []Filter{{Field: "project", Op: "eq", Value: ProjectToken}}
	if err := Validate(s); err != nil {
		t.Errorf("project section: %v", err)
	}
}

func TestParseRefusesUnknownKeys(t *testing.T) {
	for _, raw := range []string{
		`{"id":"x","title":"X","source":{"api":"/api/ideas"},"view":"list","fields":[{"path":"title"}],"script":"alert(1)"}`,
		`{"id":"x","title":"X","source":{"api":"/api/ideas","url":"https://x"},"view":"list","fields":[{"path":"title"}]}`,
		`{"id":"x","title":"X","source":{"api":"/api/ideas"},"view":"list","fields":[{"path":"title","html":"<b>"}]}`,
		`{"id":"x"} {"id":"y"}`,
		strings.Repeat(" ", MaxBytes+1),
	} {
		if _, err := Check([]byte(raw)); err == nil {
			t.Errorf("accepted %.80s", raw)
		}
	}
	if s, err := Check([]byte(`{"id":"x","title":"X","source":{"api":"/api/ideas"},"view":"list","fields":[{"path":"title"}]}`)); err != nil || s.RefreshS != DefaultRefresh || s.Placement != PlaceOverview || s.Limit != DefaultLimit {
		t.Errorf("defaults %+v %v", s, err)
	}
}

// The system prompt lists exactly the allowlisted routes.
func TestPromptListsTheAllowlist(t *testing.T) {
	if PromptVersion() != "v1" {
		t.Errorf("prompt version %q", PromptVersion())
	}
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^GET (/api/\S+)$`).FindAllStringSubmatch(SystemPrompt, -1) {
		listed[m[1]] = true
	}
	for _, a := range APIs {
		if !listed[a.Route] {
			t.Errorf("%s is allowed but not described in prompts/section.md", a.Route)
		}
		delete(listed, a.Route)
	}
	for r := range listed {
		t.Errorf("prompts/section.md describes %s, which is not allowed", r)
	}
	for _, a := range APIs {
		for _, p := range a.Params {
			if !strings.Contains(SystemPrompt, p.Name+" (") {
				t.Errorf("param %s of %s is not described", p.Name, a.Route)
			}
		}
	}
}

func TestStore(t *testing.T) {
	st := &Store{Dir: t.TempDir()}
	s := valid()
	if _, err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	tp, _ := Template("spend-week")
	time.Sleep(20 * time.Millisecond)
	if _, err := st.Save(tp); err != nil {
		t.Fatal(err)
	}
	// A hand-edited file that breaks the rules is reported, never shown.
	if err := os.WriteFile(filepath.Join(st.Dir, "evil.json"), []byte(`{"id":"evil","title":"x","source":{"api":"/api/config"},"view":"list","fields":[{"path":"title"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l := st.List()
	if len(l.Sections) != 2 || l.Sections[0].ID != "prs" || l.Sections[1].ID != "spend-week" || len(l.Broken) != 1 || l.Broken[0].File != "evil.json" {
		t.Fatalf("list %+v", l)
	}
	if id := st.FreeID("prs"); id != "prs-2" {
		t.Errorf("free id %s", id)
	}
	bad := valid()
	bad.Source.API = "/api/config"
	if _, err := st.Save(bad); err == nil {
		t.Error("saved a section off the allowlist")
	}
	if err := st.Delete("prs"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("prs"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete %v", err)
	}
	if err := st.Delete("../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id delete %v", err)
	}
}

// fakeClaude puts the test binary on PATH as claude, answering reply.
func fakeClaude(t *testing.T, reply string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin, state := t.TempDir(), t.TempDir()
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "reply.txt"), []byte(reply), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("LUCID_FAKE_SECTION", state)
	return state
}

func TestGenerateWithAFakeCLI(t *testing.T) {
	answer := "Here it is:\n```json\n" + `{"id": "Cards Due", "title": "Cards due soon", "source": {"api": "/api/boards/{board}", "params": {"board": "work"}},
	 "view": "table", "rows": "cards", "filter": [{"field": "due", "op": "within_days", "value": 7}],
	 "fields": [{"path": "title"}, {"path": "due", "format": "date"}], "refresh_s": 120}` + "\n```"
	state := fakeClaude(t, answer)
	st := &Store{Dir: t.TempDir()}
	runs := filepath.Join(t.TempDir(), "runs")
	g := &Generator{Store: st, RunsDir: runs}
	out, err := g.Generate(context.Background(), GenerateRequest{Description: "cards due this week on my work board", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Section.ID != "cards-due" || out.Section.View != "table" || out.Model != "haiku" || out.Prompt != "v1" || out.Usage.CostUSD != 0.0031 {
		t.Errorf("generated %+v", out)
	}
	args, _ := os.ReadFile(filepath.Join(state, "args.txt"))
	a := strings.ReplaceAll(string(args), "\n", " ")
	for _, want := range []string{"--model haiku", "--tools ", "--safe-mode", "--strict-mcp-config", "--system-prompt-file"} {
		if !strings.Contains(a, want) {
			t.Errorf("claude ran without %q: %s", want, a)
		}
	}
	stdin, _ := os.ReadFile(filepath.Join(state, "stdin.txt"))
	if !strings.Contains(string(stdin), "cards due this week") {
		t.Errorf("stdin %s", stdin)
	}
	if files, _ := filepath.Glob(filepath.Join(runs, "*.json")); len(files) != 1 {
		t.Errorf("%d usage records", len(files))
	}
	// Nothing was saved.
	if l := st.List(); len(l.Sections) != 0 {
		t.Errorf("generate saved %v", l.Sections)
	}
}

func TestGenerateRefusesWhatFailsValidation(t *testing.T) {
	fakeClaude(t, `{"id": "x", "title": "Config", "source": {"api": "/api/config"}, "view": "list", "fields": [{"path": "server.addr"}]}`)
	g := &Generator{Store: &Store{Dir: t.TempDir()}}
	if _, err := g.Generate(context.Background(), GenerateRequest{Description: "show my config", Provider: "claude"}); !errors.Is(err, ErrBadOutput) {
		t.Errorf("err %v", err)
	}
	fakeClaude(t, "I cannot help with that.")
	if _, err := g.Generate(context.Background(), GenerateRequest{Description: "show my config", Provider: "claude"}); !errors.Is(err, ErrBadOutput) {
		t.Errorf("no JSON: %v", err)
	}
	for _, req := range []GenerateRequest{
		{Description: "x"},
		{Description: "show it", Provider: "gemini"},
		{Description: "key " + "s" + "k-" + strings.Repeat("a", 32)},
		{Description: "show it", Placement: "sidebar"},
	} {
		if _, err := g.Generate(context.Background(), req); err == nil {
			t.Errorf("%+v accepted", req)
		}
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := g.Generate(context.Background(), GenerateRequest{Description: "show it"}); !errors.Is(err, agentexec.ErrCLIMissing) {
		t.Errorf("no CLI: %v", err)
	}
}

func TestRoutes(t *testing.T) {
	st := &Store{Dir: t.TempDir()}
	mux := http.NewServeMux()
	Register(mux, st, &Generator{Store: st})
	do := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	if w := do("POST", "/api/sections/templates/spend-week", "", false); w.Code != http.StatusForbidden {
		t.Errorf("add without confirm %d", w.Code)
	}
	if w := do("POST", "/api/sections/templates/spend-week", "", true); w.Code != 200 {
		t.Fatalf("add template %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/sections/templates/spend-week", "", true); w.Code != 200 || !strings.Contains(w.Body.String(), `"spend-week-2"`) {
		t.Errorf("second copy %d %s", w.Code, w.Body)
	}
	if w := do("POST", "/api/sections/templates/nope", "", true); w.Code != 404 {
		t.Errorf("unknown template %d", w.Code)
	}
	body, _ := json.Marshal(valid())
	if w := do("PUT", "/api/sections/prs", string(body), true); w.Code != 200 {
		t.Fatalf("put %d %s", w.Code, w.Body)
	}
	if w := do("PUT", "/api/sections/other", string(body), true); w.Code != 400 {
		t.Errorf("id mismatch %d", w.Code)
	}
	evil := strings.Replace(string(body), "/api/work/sessions", "/api/config", 1)
	if w := do("PUT", "/api/sections/prs", evil, true); w.Code != 400 || !strings.Contains(w.Body.String(), "source.api") {
		t.Errorf("evil put %d %s", w.Code, w.Body)
	}
	var l Listing
	if w := do("GET", "/api/sections", "", false); json.Unmarshal(w.Body.Bytes(), &l) != nil || len(l.Sections) != 3 {
		t.Errorf("list %s", w.Body)
	}
	var cr CheckResult
	if w := do("POST", "/api/sections/check", evil, false); json.Unmarshal(w.Body.Bytes(), &cr) != nil || cr.Valid || len(cr.Problems) == 0 {
		t.Errorf("check %s", w.Body)
	}
	var cat Catalog
	if w := do("GET", "/api/sections/catalog", "", false); json.Unmarshal(w.Body.Bytes(), &cat) != nil || len(cat.APIs) != len(APIs) || len(cat.Templates) != len(Templates) {
		t.Errorf("catalog %s", w.Body)
	}
	if w := do("DELETE", "/api/sections/prs", "", true); w.Code != 204 {
		t.Errorf("delete %d", w.Code)
	}
	if w := do("POST", "/api/sections/generate", `{"description": "x"}`, false); w.Code != http.StatusForbidden {
		t.Errorf("generate without confirm %d", w.Code)
	}
}
