package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
)

func TestHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" || got.Version != version.Version {
		t.Fatalf("unexpected body %+v", got)
	}
}

func TestHealthRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestCIActionsNeedConfirmHeader(t *testing.T) {
	for _, p := range []string{"/api/ci/containers/x/stop", "/api/ci/runs/you%2Fyour-repo/1/rerun"} {
		rec := httptest.NewRecorder()
		New().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", p, rec.Code)
		}
	}
}

func TestProjectsAndMCPRoutes(t *testing.T) {
	t.Setenv("LUCID_PROJECTS", filepath.Join(t.TempDir(), "projects.yaml"))
	t.Setenv("LUCID_HOST_HOME", t.TempDir())
	for _, p := range []string{"/api/projects", "/api/mcp"} {
		rec := httptest.NewRecorder()
		New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", p, rec.Code)
		}
		var body map[string]any
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestMemoryAndBoardsRoutes(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("LUCID_DATA_DIR", data)
	h := New()
	// Building the handler creates nothing; the first request does.
	if _, err := os.Stat(data); err == nil {
		t.Fatal("New created the data dir")
	}
	for _, p := range []string{"/api/memory/tree", "/api/boards", "/api/boards/work", "/api/memory/search?q=x"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d %s", p, rec.Code, rec.Body)
		}
	}
	for method, p := range map[string]string{http.MethodPut: "/api/memory/page?path=a.md", http.MethodPost: "/api/boards/work/cards"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, p, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without confirm: status %d", method, p, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(data, "memory", "Boards", "work.md")); err != nil {
		t.Errorf("default board not created in the data dir: %v", err)
	}
}

func TestWorkRoutes(t *testing.T) {
	t.Setenv("LUCID_DATA_DIR", filepath.Join(t.TempDir(), "data"))
	h := New()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/work/sessions", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/work/sessions", strings.NewReader(`{}`)))
	if rec.Code != http.StatusForbidden {
		t.Errorf("start without confirm: %d", rec.Code)
	}
}

func TestNextUpRoutes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LUCID_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("PATH", root) // no provider CLI
	h := New()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nextup", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/nextup/rank", "/api/nextup/snooze", "/api/nextup/dismiss", "/api/nextup/restore"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, p, strings.NewReader(`{}`)))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without confirm: %d", p, rec.Code)
		}
	}
}

func TestPromptsRoutes(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("LUCID_DATA_DIR", data)
	t.Setenv("LUCID_PROJECTS", filepath.Join(t.TempDir(), "projects.yaml"))
	h := New()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/prompts/templates", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"builder"`) {
		t.Errorf("templates: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/prompts/render", strings.NewReader(`{"sections":[{"id":"task","body":"x"}],"target":"copy"}`)))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tokens_note"`) {
		t.Errorf("render: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/prompts/improve", strings.NewReader(`{"text":"x"}`)))
	if rec.Code != http.StatusForbidden {
		t.Errorf("improve without confirm: %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(data, "prompts")); err == nil {
		t.Error("reading templates created the prompts folder")
	}
}

func TestUnknownAPIIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestFailingOnDefault(t *testing.T) {
	runs := []ci.Run{
		{Repo: "you/a", Branch: "main", Status: "completed", Conclusion: "failure", CreatedAt: "2026-10-09T10:00:00Z", Name: "ci", RunNumber: 7, HTMLURL: "https://example.invalid/a/7"},
		{Repo: "you/a", Branch: "main", Status: "completed", Conclusion: "success", CreatedAt: "2026-10-09T09:00:00Z"},
		{Repo: "you/b", Branch: "main", Status: "completed", Conclusion: "success", CreatedAt: "2026-10-09T10:00:00Z"},
		{Repo: "you/b", Branch: "main", Status: "completed", Conclusion: "failure", CreatedAt: "2026-10-09T08:00:00Z"},
		{Repo: "you/c", Branch: "feature", Status: "completed", Conclusion: "failure", CreatedAt: "2026-10-09T10:00:00Z"},
		{Repo: "you/d", Branch: "master", Status: "in_progress", CreatedAt: "2026-10-09T11:00:00Z"},
		{Repo: "you/d", Branch: "master", Status: "completed", Conclusion: "timed_out", CreatedAt: "2026-10-09T10:00:00Z"},
	}
	got := failingOnDefault(runs)
	if len(got) != 2 || got[0].Repo != "you/a" || got[0].RunNumber != 7 || got[0].URL == "" || got[1].Repo != "you/d" || got[1].Conclusion != "timed_out" {
		t.Errorf("failing %+v", got)
	}
}
