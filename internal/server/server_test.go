package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestUnknownAPIIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}
