package jobs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func withEnsure(t *testing.T, fn func(context.Context) error) {
	t.Helper()
	old := EnsureCluster
	t.Cleanup(func() { EnsureCluster = old })
	EnsureCluster = fn
}

func TestPostHelloEnsuresClusterFirst(t *testing.T) {
	var order []string
	withRunner(t, NewRunner(fake.NewSimpleClientset()))
	old := connect
	connect = func() (*Runner, error) { order = append(order, "connect"); return old() }
	withEnsure(t, func(context.Context) error { order = append(order, "ensure"); return nil })
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/hello", nil))
	if rec.Code != http.StatusCreated || strings.Join(order, ",") != "ensure,connect" {
		t.Fatalf("status %d, order %v", rec.Code, order)
	}
}

func TestPostHelloEnsureFailureIs503(t *testing.T) {
	withRunner(t, NewRunner(fake.NewSimpleClientset()))
	withEnsure(t, func(context.Context) error { return errors.New("the cluster is stopped and power.cluster is off") })
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodPost, "/api/jobs/hello", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "power.cluster is off") {
		t.Fatalf("status %d body %q", rec.Code, rec.Body.String())
	}
}
