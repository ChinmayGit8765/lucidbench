package cluster

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerRejectsPost(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(rec, httptest.NewRequest(http.MethodPost, "/api/cluster", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestInContainer(t *testing.T) {
	t.Setenv("LUCID_IN_CONTAINER", "1")
	if !InContainer() {
		t.Fatal("want true")
	}
	t.Setenv("LUCID_IN_CONTAINER", "")
	if InContainer() {
		t.Fatal("want false")
	}
}
