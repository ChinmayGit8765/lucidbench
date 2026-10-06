package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo makes a folder that looks like a git checkout, with files.
func repo(t *testing.T, dir string, files ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(files); i += 2 {
		write(t, filepath.Join(dir, files[i]), files[i+1])
	}
}

func newService(t *testing.T) (*Service, string, string) {
	t.Helper()
	data := t.TempDir()
	path := filepath.Join(data, "projects.yaml")
	s := &Service{
		DataDir:      data,
		ProjectsPath: func() (string, error) { return path, nil },
		Home:         t.TempDir(),
		Now:          func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
	return s, data, path
}

func TestStatus(t *testing.T) {
	s, data, path := newService(t)
	if st := s.Status(); !st.Needed || st.UI || st.Projects {
		t.Fatalf("fresh = %+v", st)
	}
	write(t, filepath.Join(data, "ui.json"), "{}")
	if st := s.Status(); st.Needed || !st.UI {
		t.Fatalf("with ui.json = %+v", st)
	}
	os.Remove(filepath.Join(data, "ui.json"))
	write(t, path, "version: 1\nprojects: []\n")
	if st := s.Status(); st.Needed || !st.Projects {
		t.Fatalf("with projects.yaml = %+v", st)
	}
}

func TestScanAndGuess(t *testing.T) {
	s, _, _ := newService(t)
	root := t.TempDir()
	repo(t, filepath.Join(root, "api"), "go.mod", "module x\n", "Dockerfile", "FROM scratch\n")
	repo(t, filepath.Join(root, "group", "Web App"), "package.json", `{"devDependencies":{"vite":"1"}}`)
	repo(t, filepath.Join(root, "group", "game"), "project.godot", "")
	repo(t, filepath.Join(root, "group", "plain"))
	repo(t, filepath.Join(root, "a", "b", "too-deep"))
	repo(t, filepath.Join(root, "node_modules", "dep"))
	repo(t, filepath.Join(root, ".hidden"))
	// A repository's own subfolders are not scanned.
	repo(t, filepath.Join(root, "api", "inner"))

	res, err := s.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Candidate{}
	for _, c := range res.Repos {
		got[c.Name] = c
	}
	if len(res.Repos) != 4 {
		t.Fatalf("repos = %+v", res.Repos)
	}
	want := map[string][2]string{"api": {"api", "service"}, "Web App": {"web-app", "web-app"}, "game": {"game", "game"}, "plain": {"plain", ""}}
	for name, w := range want {
		c, ok := got[name]
		if !ok || c.ID != w[0] || c.Type != w[1] {
			t.Errorf("%s = %+v, want id %s type %q", name, c, w[0], w[1])
		}
	}
	if _, err := s.Scan("relative/path"); !errors.Is(err, ErrBadRequest) {
		t.Errorf("relative root = %v", err)
	}
}

func TestAddCreatesThenAppends(t *testing.T) {
	s, _, path := newService(t)
	root := t.TempDir()
	repo(t, filepath.Join(root, "one"), "go.mod", "module one\n", "main.go", "package main\n")
	repo(t, filepath.Join(root, "two"))

	res, err := s.Add([]Entry{{ID: "one", Name: `One "quoted"`, LocalPath: filepath.Join(root, "one"), Type: "cli"}})
	if err != nil || !res.Created || res.Backup != "" {
		t.Fatalf("create = %+v, %v", res, err)
	}
	l := projects.LoadFrom(path, "")
	if len(l.Projects) != 1 || l.Projects[0].Name != `One "quoted"` || l.Projects[0].Visibility != DefaultVisibility || len(l.Errors) != 0 {
		t.Fatalf("after create = %+v", l)
	}

	// Rescanning knows the first is already listed.
	sc, _ := s.Scan(root)
	for _, c := range sc.Repos {
		if c.Name == "one" && c.Existing != "one" {
			t.Errorf("existing not detected: %+v", c)
		}
	}

	// A hand edit with a comment is kept byte for byte.
	before, _ := os.ReadFile(path)
	before = append(before, []byte("    # my note\n")...)
	write(t, path, string(before))
	res, err = s.Add([]Entry{{ID: "two", Name: "two", LocalPath: filepath.Join(root, "two")}})
	if err != nil || res.Backup == "" {
		t.Fatalf("append = %+v, %v", res, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.HasPrefix(after, before) {
		t.Errorf("existing bytes changed:\n%s", after)
	}
	bak, _ := os.ReadFile(filepath.Join(filepath.Dir(path), res.Backup))
	if !bytes.Equal(bak, before) {
		t.Error("backup is not the previous file")
	}
	if l := projects.LoadFrom(path, ""); len(l.Projects) != 2 || len(l.Errors) != 0 {
		t.Fatalf("after append = %+v", l)
	}

	// Duplicate ids, missing repos and bad types are refused.
	for _, e := range []Entry{
		{ID: "one", Name: "x", LocalPath: filepath.Join(root, "two")},
		{ID: "nope", Name: "x", LocalPath: filepath.Join(root, "missing")},
		{ID: "bad-type", Name: "x", LocalPath: filepath.Join(root, "two"), Type: "spaceship"},
		{ID: "Bad ID", Name: "x", LocalPath: filepath.Join(root, "two")},
	} {
		if _, err := s.Add([]Entry{e}); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v = %v", e, err)
		}
	}
}

func TestAddRefusesWhenTheListIsNotLast(t *testing.T) {
	s, _, path := newService(t)
	root := t.TempDir()
	repo(t, filepath.Join(root, "two"))
	orig := "projects:\n  - id: keep\n    name: Keep\n    category: tool\n    status: active\n    visibility: private\nversion: 1\n"
	write(t, path, orig)
	res, err := s.Add([]Entry{{ID: "two", Name: "two", LocalPath: filepath.Join(root, "two")}})
	if !errors.Is(err, ErrNotAppendable) || !strings.Contains(res.Snippet, "id: two") {
		t.Fatalf("= %+v, %v", res, err)
	}
	if b, _ := os.ReadFile(path); string(b) != orig {
		t.Error("file changed on refusal")
	}
	ents, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range ents {
		if strings.Contains(e.Name(), ".bak-") {
			t.Error("backup written on refusal")
		}
	}
}

func TestRoutes(t *testing.T) {
	s, _, _ := newService(t)
	root := t.TempDir()
	repo(t, filepath.Join(root, "one"))
	mux := http.NewServeMux()
	Register(mux, s)
	do := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("GET", "/api/setup", "", false); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"needed":true`) {
		t.Errorf("status = %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/api/setup/dirs?path="+root, "", false); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"git":true`) {
		t.Errorf("dirs = %d %s", rec.Code, rec.Body)
	}
	body, _ := json.Marshal(map[string]string{"root": root})
	if rec := do("POST", "/api/setup/scan", string(body), false); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":"one"`) {
		t.Errorf("scan = %d %s", rec.Code, rec.Body)
	}
	entries, _ := json.Marshal(map[string]any{"entries": []Entry{{ID: "one", Name: "One", LocalPath: filepath.Join(root, "one")}}})
	if rec := do("POST", "/api/setup/projects/preview", string(entries), false); rec.Code != 200 || !strings.Contains(rec.Body.String(), "id: one") {
		t.Errorf("preview = %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/setup/projects", string(entries), false); rec.Code == 200 {
		t.Error("add without confirm accepted")
	}
	if rec := do("POST", "/api/setup/projects", string(entries), true); rec.Code != 200 {
		t.Errorf("add = %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/setup/scan", `{"root":"x","extra":1}`, false); rec.Code != 400 {
		t.Errorf("unknown field = %d", rec.Code)
	}
}
