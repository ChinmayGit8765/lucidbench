package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/remote"
)

// Wiring the phone remote into the handler binds nothing, and without it
// the desktop API has no /api/remote routes.
func TestRemoteWiringBindsNothing(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("LUCID_DATA_DIR", data)
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/remote", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/api/remote without a manager: %d", rec.Code)
	}
	m := remote.NewManager(filepath.Join(data, "remote"))
	binds := 0
	m.Listen = func(network, addr string) (net.Listener, error) {
		binds++
		return net.Listen(network, "127.0.0.1:0")
	}
	t.Cleanup(m.Close)
	h := NewWith(config.Default(), Deps{Remote: m})
	if binds != 0 {
		t.Fatal("NewWith bound the remote")
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/remote", nil))
	var st remote.Status
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &st) != nil || st.Listening || st.Settings.Enabled {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	// Turning it on needs the confirm header.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/remote/settings", strings.NewReader(`{"enabled":true,"mode":"lan","address":"127.0.0.1","port":7499}`)))
	if rec.Code != http.StatusForbidden || binds != 0 {
		t.Fatalf("enable without confirm: %d, %d binds", rec.Code, binds)
	}
	// The remote's own routes are not on the desktop handler's /r/api.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/r/api/overview", nil))
	if strings.Contains(rec.Body.String(), "attention") {
		t.Fatal("the desktop handler serves the remote API")
	}
	if err := m.Start(); err != nil || binds != 0 {
		t.Fatalf("Start with no settings: %v, %d binds", err, binds)
	}
}
