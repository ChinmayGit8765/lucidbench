package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestUnknownAPIIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}
