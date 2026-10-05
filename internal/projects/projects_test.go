package projects

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTemplateParsesCleanAndMatchesExample(t *testing.T) {
	ps, errs := Parse([]byte(Template))
	if len(errs) != 0 {
		t.Fatalf("template has errors: %v", errs)
	}
	if len(ps) != 3 {
		t.Fatalf("got %d projects, want 3", len(ps))
	}
	ex, err := os.ReadFile(filepath.Join("..", "..", "projects.example.yaml"))
	if err != nil {
		t.Skip("projects.example.yaml not found")
	}
	if strings.ReplaceAll(string(ex), "\r\n", "\n") != Template {
		t.Error("projects.example.yaml differs from projects.Template; keep them identical")
	}
}

func TestDerivedLinks(t *testing.T) {
	ps, _ := Parse([]byte(Template))
	byID := map[string]Project{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	if got := byID["my-app"].BuiltBy; !slices.Equal(got, []string{"build-tools"}) {
		t.Errorf("my-app built_by = %v", got)
	}
	if got := byID["build-tools"].NeededBy; !slices.Equal(got, []string{"my-app"}) {
		t.Errorf("build-tools needed_by = %v", got)
	}
	if got := byID["my-app"].NeededBy; !slices.Equal(got, []string{"my-portfolio-site"}) {
		t.Errorf("my-app needed_by = %v", got)
	}
	if got := byID["my-app"].Progress; got != (Progress{Done: 0, Total: 2}) {
		t.Errorf("my-app progress = %+v", got)
	}
	if got := byID["build-tools"].Needs; got == nil || len(got) != 0 {
		t.Errorf("needs should be an empty list, got %#v", got)
	}
}

func TestValidationNamesIDAndField(t *testing.T) {
	src := `version: 1
projects:
  - { id: a, name: A, category: product, status: active, visibility: public, builds_into: [ghost] }
  - { id: a, name: A2, category: product, status: active, visibility: public }
  - { id: b, name: B, category: gadget, type: spaceship, status: active, visibility: secret,
      needs: [ { what: x, from: nobody, status: maybe } ] }
  - { id: c, category: tool, status: active, visibility: private }
`
	ps, errs := Parse([]byte(src))
	if len(ps) != 3 {
		t.Errorf("got %d projects, want 3 (duplicate dropped)", len(ps))
	}
	all := strings.Join(errs, "\n")
	for _, want := range []string{
		`project "a": builds_into: unknown project id "ghost"`,
		`project "a": id: duplicate id`,
		`project "b": category: unknown value "gadget"`,
		`project "b": type: unknown value "spaceship"`,
		`project "b": visibility: unknown value "secret"`,
		`project "b": needs[0].from: unknown project id "nobody"`,
		`project "b": needs[0].status: unknown value "maybe"`,
		`project "c": name: required`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing error %q in:\n%s", want, all)
		}
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	home := t.TempDir()
	l := LoadFrom(filepath.Join(home, "lucidbench", FileName), home)
	if l.Configured || len(l.Errors) != 0 || l.Projects == nil || len(l.Projects) != 0 {
		t.Errorf("got %+v", l)
	}
	if want := filepath.Join("~", "lucidbench", FileName); l.PathHint != want {
		t.Errorf("path hint = %q, want %q", l.PathHint, want)
	}
}

func TestInvalidYAML(t *testing.T) {
	l := LoadFrom(write(t, "projects: [\n"), "")
	if !l.Configured || len(l.Errors) != 1 || !strings.HasPrefix(l.Errors[0], "invalid YAML") {
		t.Errorf("got %+v", l)
	}
}

func TestTableGroupsByCategory(t *testing.T) {
	ps, _ := Parse([]byte(Template))
	out := Table(ps)
	i, j, k := strings.Index(out, "Products (1)"), strings.Index(out, "Portfolio (1)"), strings.Index(out, "Tools (build other projects) (1)")
	if i < 0 || j < i || k < j {
		t.Errorf("unexpected grouping:\n%s", out)
	}
	if !strings.Contains(Detail(ps[0]), "(from build-tools)") {
		t.Errorf("detail lacks need source:\n%s", Detail(ps[0]))
	}
}

func TestHandler(t *testing.T) {
	path := write(t, Template)
	h := HandlerFor(func() *List { return LoadFrom(path, filepath.Dir(path)) })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"configured", "path_hint", "projects", "errors"} {
		if _, ok := got[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
	if got["path_hint"] != filepath.Join("~", FileName) {
		t.Errorf("path_hint = %v", got["path_hint"])
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/projects", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d", rec.Code)
	}
}

func TestLocalPath(t *testing.T) {
	// An absolute path that does not exist loads fine: existence is checked
	// when the path is used, not when the file is read.
	abs, err := filepath.Abs(filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatal(err)
	}
	src := "version: 1\nprojects:\n" +
		"  - { id: a, name: A, category: product, status: active, visibility: public, local_path: '" + abs + "' }\n" +
		"  - { id: b, name: B, category: product, status: active, visibility: public, local_path: relative/dir }\n" +
		"  - { id: c, name: C, category: product, status: active, visibility: public }\n"
	ps, errs := Parse([]byte(src))
	if len(ps) != 3 || ps[0].LocalPath != abs || ps[2].LocalPath != "" {
		t.Fatalf("projects = %+v", ps)
	}
	if len(errs) != 1 || !strings.Contains(errs[0], `project "b": local_path: must be an absolute path`) {
		t.Errorf("errors = %v", errs)
	}
	if !strings.Contains(Detail(ps[0]), abs) {
		t.Errorf("detail lacks the local path:\n%s", Detail(ps[0]))
	}

	rec := httptest.NewRecorder()
	HandlerFor(func() *List { return &List{Projects: ps} }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var out struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Projects[0]["local_path"] != abs {
		t.Errorf("API local_path = %v", out.Projects[0]["local_path"])
	}
	if _, ok := out.Projects[2]["local_path"]; ok {
		t.Error("an unset local_path should be left out of the API output")
	}
}
