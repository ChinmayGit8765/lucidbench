package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// The connector routes are wired, report status without calling out, need the
// confirm header to change anything, and never echo a credential.
func TestIntegrationRoutes(t *testing.T) {
	const secret = "SENTINEL-credential-value-0123456789"
	dir := t.TempDir()
	t.Setenv("LUCID_CONFIG", filepath.Join(dir, "config.yaml"))
	t.Setenv("LUCID_DATA_DIR", dir)
	t.Setenv("LUCID_VAULT_PATH", filepath.Join(dir, "vault"))
	t.Setenv("LUCID_HOST_HOME", t.TempDir())
	t.Setenv("LINEAR_API_KEY", secret)
	t.Setenv("TRELLO_API_KEY", secret)
	t.Setenv("TRELLO_TOKEN", secret)
	h := New()
	for _, p := range []string{"/api/linear/status", "/api/trello/status", "/api/config", "/api/host/tools"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), secret) {
			t.Errorf("%s: status %d, secret in body: %v", p, rec.Code, strings.Contains(rec.Body.String(), secret))
		}
		if p == "/api/linear/status" && !strings.Contains(rec.Body.String(), `"configured":true`) {
			t.Errorf("linear status = %s", rec.Body)
		}
	}
	for _, c := range [][2]string{
		{http.MethodPost, "/api/linear/issues"}, {http.MethodPost, "/api/linear/promote"}, {http.MethodPost, "/api/linear/link"},
		{http.MethodPost, "/api/trello/cards"}, {http.MethodPut, "/api/trello/cards/abc"}, {http.MethodPost, "/api/trello/link"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c[0], c[1], strings.NewReader("{}")))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without confirm = %d", c[0], c[1], rec.Code)
		}
	}
}
