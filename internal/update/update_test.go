package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseAndCompare(t *testing.T) {
	for _, bad := range []string{"dev", "", "1.2", "1.2.3.4", "01.2.3", "1.2.3-", "1.2.3-beta..1", "1.x.3", "1.2.3-b@d"} {
		if _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	if v, ok := Parse("v0.3.0-beta.5+build.7"); !ok || v.String() != "0.3.0-beta.5" {
		t.Fatalf("Parse with v and build = %v %v", v, ok)
	}
	// Each version is older than the next.
	order := []string{"0.2.9", "0.3.0-alpha", "0.3.0-alpha.1", "0.3.0-beta", "0.3.0-beta.2", "0.3.0-beta.4", "0.3.0-beta.10", "0.3.0-rc.1", "0.3.0", "0.3.1", "0.10.0", "1.0.0"}
	for i := range order {
		for j := range order {
			a, _ := Parse(order[i])
			b, _ := Parse(order[j])
			want := sign(i - j)
			if got := Compare(a, b); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	// Numeric identifiers sort before alphanumeric ones.
	a, _ := Parse("1.0.0-1")
	b, _ := Parse("1.0.0-beta")
	if Compare(a, b) != -1 {
		t.Error("1.0.0-1 should be older than 1.0.0-beta")
	}
}

// fakeGitHub serves a releases list and counts the requests.
type fakeGitHub struct {
	srv  *httptest.Server
	hits atomic.Int32
	rels []Release
	ua   atomic.Value
}

func newFake(t *testing.T, rels []Release) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{rels: rels}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.ua.Store(r.Header.Get("User-Agent"))
		if r.URL.Path != "/repos/owner/app/releases" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(f.rels)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func rel(tag string, pre bool) Release {
	r := Release{Tag: tag, Prerelease: pre, HTMLURL: "https://example.invalid/releases/" + tag, Body: "## " + tag + "\n\n- One thing\n- Another"}
	r.Assets = append(r.Assets, struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{"Lucidbench_" + tag + "_x64-setup.exe", "https://example.invalid/dl/" + tag + ".exe"})
	return r
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSvc(t *testing.T, f *fakeGitHub, current string, c *clock) *Service {
	t.Helper()
	return &Service{Enabled: true, Repo: "owner/app", Current: current, BaseURL: f.srv.URL, DataDir: t.TempDir(), Now: c.now}
}

func TestCheckFindsNewerPrerelease(t *testing.T) {
	draft := rel("v0.4.0", false)
	draft.Draft = true
	f := newFake(t, []Release{rel("v0.3.0-beta.4", true), rel("v0.3.0-beta.10", true), rel("v0.2.0", false), draft, rel("nightly", true)})
	c := &clock{time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	s := newSvc(t, f, "0.3.0-beta.4", c)
	st := s.Check(context.Background(), false)
	if !st.Available || st.Latest != "0.3.0-beta.10" || !st.Prerelease || st.Offline {
		t.Fatalf("status %+v", st)
	}
	if st.URL != "https://example.invalid/releases/v0.3.0-beta.10" || st.DownloadURL != "https://example.invalid/dl/v0.3.0-beta.10.exe" {
		t.Errorf("links %q %q", st.URL, st.DownloadURL)
	}
	if st.NotesExcerpt != "- One thing\n- Another" {
		t.Errorf("excerpt %q", st.NotesExcerpt)
	}
	if ua, _ := f.ua.Load().(string); ua != "lucidbench/0.3.0-beta.4" {
		t.Errorf("user agent %q", ua)
	}
	if _, err := os.Stat(filepath.Join(s.DataDir, FileName)); err != nil {
		t.Errorf("no cache file: %v", err)
	}

	// The same version is not an update.
	s2 := newSvc(t, f, "0.3.0-beta.10", c)
	if st := s2.Check(context.Background(), false); st.Available || st.Latest != "0.3.0-beta.10" {
		t.Errorf("same version: %+v", st)
	}
}

func TestStableBuildsIgnorePrereleases(t *testing.T) {
	f := newFake(t, []Release{rel("v0.3.0-beta.5", true), rel("v0.2.1", false), rel("v0.2.0", false)})
	c := &clock{time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	st := newSvc(t, f, "v0.2.0", c).Check(context.Background(), false)
	if !st.Available || st.Latest != "0.2.1" || st.Prerelease {
		t.Fatalf("stable channel: %+v", st)
	}
}

func TestCacheWindow(t *testing.T) {
	f := newFake(t, []Release{rel("v1.1.0", false)})
	c := &clock{time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	s := newSvc(t, f, "1.0.0", c)
	ctx := context.Background()
	s.Check(ctx, false)
	c.t = c.t.Add(5*time.Hour + 59*time.Minute)
	if st := s.Check(ctx, false); !st.Available || f.hits.Load() != 1 {
		t.Fatalf("within the window: hits %d, status %+v", f.hits.Load(), st)
	}
	// A new release appears; the cached answer stands until the window ends.
	f.rels = append(f.rels, rel("v1.2.0", false))
	if st := s.Check(ctx, false); st.Latest != "1.1.0" {
		t.Errorf("cached latest %q", st.Latest)
	}
	c.t = c.t.Add(time.Minute)
	if st := s.Check(ctx, false); st.Latest != "1.2.0" || f.hits.Load() != 2 {
		t.Errorf("after six hours: hits %d latest %q", f.hits.Load(), st.Latest)
	}
	// A restart reads the cache file instead of asking again.
	s2 := &Service{Enabled: true, Repo: "owner/app", Current: "1.0.0", BaseURL: f.srv.URL, DataDir: s.DataDir, Now: c.now}
	if st := s2.Check(ctx, false); st.Latest != "1.2.0" || f.hits.Load() != 2 {
		t.Errorf("after restart: hits %d latest %q", f.hits.Load(), st.Latest)
	}
	// A manual check asks again, but at most once a minute.
	s.Check(ctx, true)
	if f.hits.Load() != 2 {
		t.Errorf("manual check inside a minute asked GitHub: %d", f.hits.Load())
	}
	c.t = c.t.Add(time.Minute)
	s.Check(ctx, true)
	if f.hits.Load() != 3 {
		t.Errorf("manual check after a minute: hits %d", f.hits.Load())
	}
}

func TestOptOutAndDevBuildNeverAsk(t *testing.T) {
	f := newFake(t, []Release{rel("v1.1.0", false)})
	c := &clock{time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	off := newSvc(t, f, "1.0.0", c)
	off.Enabled = false
	if st := off.Check(context.Background(), true); st.Enabled || st.Available || !strings.Contains(st.Note, "update_check: false") {
		t.Errorf("opted out: %+v", st)
	}
	dev := &Service{Enabled: true, Repo: "owner/app", Current: "dev", DataDir: t.TempDir(), Now: c.now,
		Client: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
			t.Error("a dev build asked GitHub")
			return nil, http.ErrHandlerTimeout
		})}}
	if st := dev.Check(context.Background(), true); st.Available || !strings.Contains(st.Note, "development build") {
		t.Errorf("dev build: %+v", st)
	}
	if f.hits.Load() != 0 {
		t.Errorf("hits %d", f.hits.Load())
	}
	// Against a test feed a dev build counts as older than any release.
	feed := newSvc(t, f, "dev", c)
	if st := feed.Check(context.Background(), false); !st.Available || st.Latest != "1.1.0" {
		t.Errorf("dev against a feed: %+v", st)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOfflineKeepsLastAnswerQuietly(t *testing.T) {
	f := newFake(t, []Release{rel("v1.1.0", false)})
	c := &clock{time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	s := newSvc(t, f, "1.0.0", c)
	ctx := context.Background()
	s.Check(ctx, false)
	f.srv.Close() // offline from here on
	c.t = c.t.Add(7 * time.Hour)
	st := s.Check(ctx, false)
	if !st.Offline || !st.Available || st.Latest != "1.1.0" || !strings.Contains(st.Note, "Could not reach GitHub") {
		t.Fatalf("offline: %+v", st)
	}
	// The failed attempt counts: no retry storm within the window.
	var calls atomic.Int32
	s.Client = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, http.ErrHandlerTimeout
	})}
	c.t = c.t.Add(time.Hour)
	for range 5 {
		s.Check(ctx, false)
	}
	if calls.Load() != 0 {
		t.Errorf("retried %d times inside the window", calls.Load())
	}

	// Offline on the very first check: no latest, no error status.
	first := &Service{Enabled: true, Repo: "owner/app", Current: "1.0.0", BaseURL: "http://127.0.0.1:1", DataDir: t.TempDir(), Now: c.now}
	st = first.Check(ctx, false)
	if !st.Offline || st.Available || st.Latest != "" {
		t.Errorf("first check offline: %+v", st)
	}
}

func TestRoutes(t *testing.T) {
	f := newFake(t, []Release{rel("v1.1.0", false)})
	c := &clock{time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)}
	mux := http.NewServeMux()
	Register(mux, newSvc(t, f, "1.0.0", c))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/update", nil))
	var st Status
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &st) != nil || st.Current != "1.0.0" || st.Latest != "1.1.0" || !st.Available {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/update/check", nil))
	if rec.Code == 200 {
		t.Errorf("POST without confirm: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/update/check", nil)
	req.Header.Set("X-Lucid-Confirm", "yes")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("POST confirmed: %d", rec.Code)
	}
}

func TestExcerptCuts(t *testing.T) {
	long := "## 1.0.0\n\n" + strings.Repeat("word ", 200)
	ex := Excerpt(long)
	if !strings.HasSuffix(ex, "…") || len([]rune(ex)) > MaxExcerpt+1 || strings.Contains(ex, "#") {
		t.Errorf("excerpt %q", ex)
	}
}
