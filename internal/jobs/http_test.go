package jobs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerNoCluster503(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	for _, path := range []string{"/api/jobs", "/api/jobs/x/logs"} {
		rec := httptest.NewRecorder()
		Handler(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "lucid cluster up") {
			t.Errorf("%s: body = %q", path, rec.Body.String())
		}
	}
}
