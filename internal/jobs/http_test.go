package jobs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func withRunner(t *testing.T, r *Runner) {
	t.Helper()
	old := connect
	t.Cleanup(func() { connect = old })
	connect = func() (*Runner, error) { return r, nil }
}

func TestListAlwaysReturnsJSONArray(t *testing.T) {
	withRunner(t, NewRunner(fake.NewSimpleClientset()))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		Handler(rec, httptest.NewRequest(method, "/api/jobs", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (HEAD used to 405 and look like an empty body)", method, rec.Code)
		}
		if method == http.MethodGet && strings.TrimSpace(rec.Body.String()) != "[]" {
			t.Errorf("empty list body = %q, want []", rec.Body.String())
		}
	}
}

func TestPostHelloSubmitsJob(t *testing.T) {
	r := NewRunner(fake.NewSimpleClientset())
	withRunner(t, r)
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/hello", nil))
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"name":"hello-`) {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
	list, err := r.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, err %v", list, err)
	}
}

func TestPostElsewhereRejected(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodPost, "/api/jobs", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestPostHelloNoCluster503(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/hello", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

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
