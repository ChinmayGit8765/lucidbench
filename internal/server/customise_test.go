package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sections and teams are wired, read-only routes answer without the confirm
// header, and nothing is written by reading.
func TestSectionAndTeamRoutes(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	projects := filepath.Join(t.TempDir(), "projects.yaml")
	if err := os.WriteFile(projects, []byte("version: 1\nprojects:\n  - id: demo\n    name: Demo\n    category: product\n    type: cli\n    status: active\n    visibility: private\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUCID_DATA_DIR", data)
	t.Setenv("LUCID_PROJECTS", projects)
	h := New()
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	if rec := get("/api/sections"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"sections":[]`) {
		t.Errorf("sections: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/api/sections/catalog"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"prs-waiting"`) {
		t.Errorf("catalog: %d", rec.Code)
	}
	if rec := get("/api/projects/demo/team"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"source":"builtin"`) {
		t.Errorf("team: %d %s", rec.Code, rec.Body)
	}
	if rec := get("/api/projects/nope/team"); rec.Code != 404 {
		t.Errorf("unknown project team: %d", rec.Code)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sections/templates/spend-week", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("add without confirm: %d", rec.Code)
	}
	for _, d := range []string{"sections", "teams"} {
		if _, err := os.Stat(filepath.Join(data, d)); err == nil {
			t.Errorf("reading created %s", d)
		}
	}
}
