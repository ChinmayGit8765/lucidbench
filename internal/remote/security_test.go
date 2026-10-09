package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// A listed path with another method is 404 too, not 405: the remote does not
// say which routes exist.
func TestWrongMethodOnAllowedPathIs404(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	for _, route := range Routes {
		method, p, _ := strings.Cut(route, " ")
		p = strings.ReplaceAll(p, "{id}", "w1")
		for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
			if m == method {
				continue
			}
			if rec := r.do(m, p, tok, true, ""); rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: %d, want 404", m, p, rec.Code)
			}
		}
	}
}

func TestCIRepoOfConfidentialProjectRedacted(t *testing.T) {
	r := newRig(t)
	list := &projects.List{Projects: []projects.Project{{ID: "open", Visibility: "private", Repo: "you/open"}, {ID: "hush", Visibility: "confidential", Repo: "https://github.com/you/hush.git"}}}
	r.surface.Services.Projects = func() (*projects.List, error) { return list, nil }
	r.surface.Services.CI = func(context.Context) ci.Summary {
		return ci.Summary{Configured: true, FailingRepos: []string{"you/open", "You/Hush"}}
	}
	tok := r.pair()
	body := r.do("GET", "/r/api/overview", tok, false, "").Body.String()
	if strings.Contains(strings.ToLower(body), "hush") || !strings.Contains(body, "you/open") {
		t.Fatalf("overview: %s", body)
	}
}

// The phone sees the top three Next up picks, read only, with every
// confidential one redacted, even when the item itself was not marked.
func TestNextUpOnThePhoneIsRedacted(t *testing.T) {
	r := newRig(t)
	list := &projects.List{Projects: []projects.Project{{ID: "open", Visibility: "private"}, {ID: "hush", Visibility: "confidential"}}}
	r.surface.Services.Projects = func() (*projects.List, error) { return list, nil }
	r.surface.Services.NextUp = func(context.Context) []NextUpItem {
		return []NextUpItem{
			{Title: "Hush roadmap", Kind: "card", Project: "Hush", ProjectID: "hush", Score: 40, Why: "due today"},
			{Title: "Secret plan", Kind: "card", Score: 30, Confidential: true},
			{Title: "Write the docs", Kind: "card", Project: "Open", ProjectID: "open", Score: 20, Why: "due in 2 days"},
			{Title: "Fourth", Kind: "card", Score: 1},
		}
	}
	tok := r.pair()
	body := r.do("GET", "/r/api/overview", tok, false, "").Body.String()
	var ov Overview
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(body)
	if strings.Contains(low, "hush") || strings.Contains(low, "secret") || strings.Contains(body, "project_id") || strings.Contains(body, "Fourth") {
		t.Fatalf("overview: %s", body)
	}
	if len(ov.NextUp) != 3 || ov.NextUp[0].Title != Redacted || ov.NextUp[1].Title != Redacted || ov.NextUp[2].Title != "Write the docs" || ov.NextUp[2].Why == "" {
		t.Errorf("next up %+v", ov.NextUp)
	}
}

func TestRevokeEndsALiveTail(t *testing.T) {
	r := newRig(t) // w1 is running and never ends by itself
	tok := r.pair()
	srv := httptest.NewServer(r.handler)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/r/api/work/sessions/w1/events", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("tail: %v %v", err, res)
	}
	defer res.Body.Close()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, res.Body)
		close(done)
	}()
	list, _ := r.surface.Devices.List()
	if _, err := r.surface.Devices.Revoke(list[0].ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream outlived the revocation")
	}
}
