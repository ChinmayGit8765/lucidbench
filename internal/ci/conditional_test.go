package ci

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunsByStatusUsesETag(t *testing.T) {
	var gotStatus, gotETag []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotStatus = append(gotStatus, r.URL.Query().Get("status"))
		gotETag = append(gotETag, r.Header.Get("If-None-Match"))
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":7,"run_number":3,"name":"CI","status":"queued","created_at":"2026-10-05T11:00:00Z"}]}`))
	}))
	defer srv.Close()
	g := &GitHub{BaseURL: srv.URL, Tokens: &TokenSource{Ref: "env:T", Getenv: func(string) string { return fixtureToken }}}
	runs, tag, err := g.RunsByStatus(context.Background(), "you/repo", "queued", "")
	if err != nil || tag != `"v1"` || len(runs) != 1 || runs[0].ID != 7 || runs[0].Repo != "you/repo" {
		t.Fatalf("runs=%+v tag=%q err=%v", runs, tag, err)
	}
	_, tag, err = g.RunsByStatus(context.Background(), "you/repo", "queued", tag)
	if !errors.Is(err, ErrNotModified) || tag != `"v1"` {
		t.Fatalf("second call: tag=%q err=%v", tag, err)
	}
	if gotStatus[0] != "queued" || gotETag[0] != "" || gotETag[1] != `"v1"` {
		t.Fatalf("status=%v etag=%v", gotStatus, gotETag)
	}
}

func TestRetryAtFromHeaders(t *testing.T) {
	now := time.Unix(1000, 0)
	h := http.Header{}
	h.Set("Retry-After", "30")
	if got := retryAt(h, now); !got.Equal(now.Add(30 * time.Second)) {
		t.Errorf("retry-after: %v", got)
	}
	h = http.Header{}
	h.Set("X-RateLimit-Remaining", "0")
	h.Set("X-RateLimit-Reset", "5000")
	if got := retryAt(h, now); !got.Equal(time.Unix(5000, 0)) {
		t.Errorf("reset: %v", got)
	}
	h.Set("X-RateLimit-Remaining", "12")
	if got := retryAt(h, now); !got.IsZero() {
		t.Errorf("remaining requests should not delay: %v", got)
	}
}

func TestRateLimitErrorCarriesRetryAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow down"}`))
	}))
	defer srv.Close()
	g := &GitHub{BaseURL: srv.URL, Tokens: &TokenSource{Ref: "env:T", Getenv: func(string) string { return fixtureToken }}}
	_, _, err := g.RunsByStatus(context.Background(), "you/repo", "in_progress", "")
	var api *APIError
	if !errors.As(err, &api) || api.Status != 429 || api.RetryAt.IsZero() {
		t.Fatalf("err=%v", err)
	}
}

func TestContainerRepo(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/you/repo":      "you/repo",
		"https://github.com/you/repo.git/": "you/repo",
		"github.com/you/repo":              "you/repo",
		"https://github.com/your-org":      "",
		"https://github.com/you/repo/tree": "",
		"":                                 "",
	} {
		if got := (Container{RepoURL: url}).Repo(); got != want {
			t.Errorf("%q: %q, want %q", url, got, want)
		}
	}
}
