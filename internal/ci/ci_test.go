package ci

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	fixtureToken       = "FIXTURE-GH-TOKEN-91c2"
	fixtureAccessToken = "FIXTURE-ACCESS-TOKEN-7f3a"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var ciRunners = Filter{ComposeProject: "ci-runners", ImageMatch: "github-runner"}

// fakeDocker answers ps and inspect from fixtures and records other calls.
type fakeDocker struct {
	t     *testing.T
	mu    sync.Mutex
	calls [][]string
}

func (f *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, args)
	f.mu.Unlock()
	switch args[0] {
	case "ps":
		return fixture(f.t, "docker_ps.jsonl"), nil
	case "inspect":
		return fixture(f.t, "docker_inspect.json"), nil
	case "start", "stop", "restart":
		return []byte(args[1] + "\n"), nil
	}
	return nil, errors.New("unexpected docker call")
}

func (f *fakeDocker) actions() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if Actions[c[0]] {
			out = append(out, c)
		}
	}
	return out
}

func TestParseDockerPSFilters(t *testing.T) {
	cs, err := parsePS(fixture(t, "docker_ps.jsonl"), ciRunners)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	want := []string{"ci-runners-runner", "ci-runners-runner-other", "lone-runner"}
	if !slices.Equal(names, want) {
		t.Fatalf("names %v, want %v", names, want)
	}
	if cs[0].Project != "ci-runners" || cs[0].Service != "runner" || cs[0].State != "running" || cs[0].Status != "Up 2 days" {
		t.Errorf("first container %+v", cs[0])
	}
	if cs[1].Up() || !cs[0].Up() {
		t.Error("Up() wrong")
	}
	// Only the image match.
	cs, _ = parsePS(fixture(t, "docker_ps.jsonl"), Filter{ImageMatch: "github-runner-custom"})
	if len(cs) != 1 || cs[0].Name != "lone-runner" {
		t.Errorf("image-only filter: %+v", cs)
	}
	// An empty filter matches nothing.
	cs, _ = ListContainers(context.Background(), (&fakeDocker{t: t}).run, Filter{})
	if len(cs) != 0 {
		t.Errorf("empty filter matched %d", len(cs))
	}
}

func TestListContainersKeepsOnlyRepoURLAndRunnerName(t *testing.T) {
	cs, err := ListContainers(context.Background(), (&fakeDocker{t: t}).run, ciRunners)
	if err != nil {
		t.Fatal(err)
	}
	c := cs[0]
	if c.RepoURL != "https://github.com/you/your-repo" || c.RunnerName != "home-runner" ||
		c.StartedAt != "2026-10-03T04:53:30.717058271Z" {
		t.Errorf("inspect fields %+v", c)
	}
	if cs[2].StartedAt != "" {
		t.Errorf("zero start time kept: %q", cs[2].StartedAt)
	}
	for _, c := range cs {
		if strings.Contains(c.Name+c.Image+c.RepoURL+c.RunnerName+c.Status, fixtureAccessToken) {
			t.Fatalf("env secret leaked into %+v", c)
		}
	}
}

func TestContainerActionAllowlist(t *testing.T) {
	ctx := context.Background()
	fd := &fakeDocker{t: t}
	if err := ContainerAction(ctx, fd.run, ciRunners, "ci-runners-runner", "restart"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, action string }{
		{"app-db-1", "stop"},              // exists, but not a runner
		{"no-such-container", "stop"},     // unknown
		{"--help", "stop"},                // flag-shaped
		{"ci-runners-runner;rm", "stop"},  // shell-shaped
		{"ci-runners-runner", "rm"},       // action not allowed
		{"ci-runners-runner", "kill"},     // action not allowed
		{"../ci-runners-runner", "start"}, // path-shaped
		{"CI-RUNNER-X", "restart"},        // not an exact match
	} {
		err := ContainerAction(ctx, fd.run, ciRunners, tc.name, tc.action)
		if !errors.Is(err, ErrNotRunner) && !errors.Is(err, ErrBadAction) {
			t.Errorf("%s %s: err = %v, want refusal", tc.action, tc.name, err)
		}
	}
	got := fd.actions()
	if len(got) != 1 || !slices.Equal(got[0], []string{"restart", "ci-runners-runner"}) {
		t.Fatalf("docker actions run: %v", got)
	}
}

// fakeGitHub serves the runners and runs fixtures for every repo and checks
// the Authorization header.
func fakeGitHub(t *testing.T, hits *int) *httptest.Server {
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*hits++
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/you/missing/"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		case strings.HasSuffix(r.URL.Path, "/actions/runners"):
			_, _ = w.Write(fixture(t, "runners.json"))
		case strings.HasSuffix(r.URL.Path, "/actions/runs"):
			_, _ = w.Write(fixture(t, "runs.json"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rerun-failed-jobs"):
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestService(t *testing.T, repos ...string) (*Service, *fakeDocker, *int) {
	hits := new(int)
	srv := fakeGitHub(t, hits)
	t.Cleanup(srv.Close)
	fd := &fakeDocker{t: t}
	return &Service{
		Repos:  repos,
		Filter: ciRunners,
		GitHub: &GitHub{BaseURL: srv.URL, Tokens: &TokenSource{
			Ref:    "env:TEST_GH",
			Getenv: func(k string) string { return map[string]string{"TEST_GH": fixtureToken}[k] },
		}},
		Docker: fd.run,
		Now:    func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) },
	}, fd, hits
}

func TestParseRunnersAndRuns(t *testing.T) {
	rs, err := parseRunners("you/your-repo", fixture(t, "runners.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Name != "home-runner" || !rs[0].Busy || rs[0].Status != "online" ||
		!slices.Equal(rs[0].Labels, []string{"self-hosted", "Linux", "ci-runners"}) || rs[1].Status != "offline" {
		t.Fatalf("runners %+v", rs)
	}
	runs, err := parseRuns("you/your-repo", fixture(t, "runs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 4 || runs[0].Conclusion != "" || runs[0].Status != "in_progress" || runs[1].Conclusion != "failure" ||
		runs[1].ID != 1003 || runs[1].Branch != "main" || runs[1].HTMLURL == "" || runs[3].Actor != "" || runs[0].Actor != "you" {
		t.Fatalf("runs %+v", runs)
	}
}

func TestSummaryMaths(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	runs := []Run{
		{Repo: "you/a", Status: "completed", Conclusion: "success", CreatedAt: "2026-10-05T11:00:00Z"},
		{Repo: "you/a", Status: "completed", Conclusion: "failure", CreatedAt: "2026-10-05T10:00:00Z"},
		{Repo: "you/a", Status: "completed", Conclusion: "success", CreatedAt: "2026-10-05T09:00:00Z"},
		{Repo: "you/a", Status: "completed", Conclusion: "cancelled", CreatedAt: "2026-10-05T08:00:00Z"},
		{Repo: "you/b", Status: "in_progress", CreatedAt: "2026-10-05T11:50:00Z"},
		{Repo: "you/b", Status: "completed", Conclusion: "timed_out", CreatedAt: "2026-10-05T07:00:00Z"},
		{Repo: "you/c", Status: "completed", Conclusion: "failure", CreatedAt: "2026-10-01T07:00:00Z"}, // old
	}
	runners := []Runner{{Status: "online", Busy: true}, {Status: "online"}, {Status: "offline", Busy: true}}
	cs := []Container{{State: "running"}, {State: "exited"}, {State: "running"}}
	s := summarize(now, []string{"you/a", "you/b", "you/c"}, runners, cs, runs)

	if s.Runners != (RunnerCounts{Total: 3, Online: 2, Busy: 1, Offline: 1}) {
		t.Errorf("runners %+v", s.Runners)
	}
	if s.Containers != (ContainerCounts{Total: 3, Up: 2, Down: 1}) {
		t.Errorf("containers %+v", s.Containers)
	}
	r := s.Runs24h
	if r.Total != 6 || r.InProgress != 1 || r.ByConclusion["success"] != 2 || r.ByConclusion["failure"] != 1 ||
		r.ByConclusion["cancelled"] != 1 || r.ByConclusion["timed_out"] != 1 {
		t.Errorf("runs_24h %+v", r)
	}
	// 2 successes, 2 failures (failure + timed_out); cancelled ignored.
	if r.PassRate == nil || *r.PassRate != 0.5 {
		t.Errorf("pass rate %v", r.PassRate)
	}
	// you/a's latest completed run succeeded; you/b's latest completed run
	// timed out (the in-progress one does not count); you/c's latest failed
	// even though it is older than 24h.
	if !slices.Equal(s.FailingRepos, []string{"you/b", "you/c"}) {
		t.Errorf("failing %v", s.FailingRepos)
	}
	if !s.Configured {
		t.Error("configured false")
	}
	empty := summarize(now, nil, nil, nil, nil)
	if empty.Runs24h.PassRate != nil || empty.Configured || empty.FailingRepos == nil {
		t.Errorf("empty summary %+v", empty)
	}
}

func TestServiceSummaryAndCache(t *testing.T) {
	s, _, hits := newTestService(t, "you/your-repo", "you/missing")
	sum := s.Summary(context.Background())
	if sum.TokenSource != "env" || sum.Runners.Total != 2 || sum.Runners.Busy != 1 || sum.Containers.Up != 2 ||
		sum.Runs24h.Total != 3 || !slices.Equal(sum.FailingRepos, []string{"you/your-repo"}) {
		t.Fatalf("summary %+v", sum)
	}
	if len(sum.Errors) != 2 || sum.Errors[0].Source != "you/missing" || !strings.Contains(sum.Errors[0].Message, "404") {
		t.Fatalf("errors %+v", sum.Errors)
	}
	first := *hits
	s.Summary(context.Background())
	if *hits != first {
		t.Errorf("cache missed: %d then %d requests", first, *hits)
	}
	rr := s.Runners(context.Background())
	if rr.Runners[0].Container != "ci-runners-runner" || rr.Runners[1].Container != "" {
		t.Errorf("container link %+v", rr.Runners)
	}
}

func TestNoTokenIsReportedNotSent(t *testing.T) {
	s, _, hits := newTestService(t, "you/your-repo")
	s.GitHub.Tokens = &TokenSource{Ref: "env:UNSET", Getenv: func(string) string { return "" }}
	sum := s.Summary(context.Background())
	if sum.TokenSource != "none" || *hits != 0 || len(sum.Errors) != 1 || sum.Errors[0].Source != "github" {
		t.Fatalf("summary %+v hits %d", sum, *hits)
	}
	// The gh fallback is used when the variable is empty.
	s.GitHub.Tokens = &TokenSource{Ref: "env:UNSET", Getenv: func(string) string { return "" },
		GH: func(context.Context) (string, error) { return fixtureToken + "\n", nil }}
	if src := s.Summary(context.Background()).TokenSource; src != "gh" {
		t.Fatalf("token source %q", src)
	}
}

func TestHTTPNeverReturnsTokens(t *testing.T) {
	s, _, _ := newTestService(t, "you/your-repo", "you/missing")
	mux := http.NewServeMux()
	Register(mux, s)
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/ci/summary", nil),
		httptest.NewRequest(http.MethodGet, "/api/ci/runners", nil),
		httptest.NewRequest(http.MethodGet, "/api/ci/runs", nil),
		httptest.NewRequest(http.MethodGet, "/api/ci/runs?repo=you/your-repo", nil),
		confirm(httptest.NewRequest(http.MethodPost, "/api/ci/runs/you%2Fyour-repo/1003/rerun", nil)),
		confirm(httptest.NewRequest(http.MethodPost, "/api/ci/runs/you%2Fmissing/1/rerun", nil)),
		confirm(httptest.NewRequest(http.MethodPost, "/api/ci/containers/ci-runners-runner/restart", nil)),
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		body := rec.Body.String()
		if rec.Code >= 500 && rec.Code != http.StatusBadGateway {
			t.Errorf("%s %s: %d %s", req.Method, req.URL, rec.Code, body)
		}
		for _, secret := range []string{fixtureToken, fixtureAccessToken, "Bearer"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s %s: response contains %q: %s", req.Method, req.URL, secret, body)
			}
		}
	}
}

func confirm(r *http.Request) *http.Request {
	r.Header.Set(ConfirmHeader, "yes")
	return r
}

func TestHTTPActions(t *testing.T) {
	s, fd, _ := newTestService(t, "you/your-repo")
	mux := http.NewServeMux()
	Register(mux, s)
	do := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		return rec
	}
	// The confirm header is required.
	if rec := do(httptest.NewRequest(http.MethodPost, "/api/ci/containers/ci-runners-runner/stop", nil)); rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), ConfirmHeader) {
		t.Fatalf("no header: %d %s", rec.Code, rec.Body.String())
	}
	if len(fd.actions()) != 0 {
		t.Fatal("acted without confirmation")
	}
	if rec := do(confirm(httptest.NewRequest(http.MethodPost, "/api/ci/containers/app-db-1/stop", nil))); rec.Code != http.StatusForbidden {
		t.Errorf("non-runner: %d", rec.Code)
	}
	if rec := do(confirm(httptest.NewRequest(http.MethodPost, "/api/ci/containers/ci-runners-runner/remove", nil))); rec.Code != http.StatusBadRequest {
		t.Errorf("bad action: %d", rec.Code)
	}
	if rec := do(confirm(httptest.NewRequest(http.MethodPost, "/api/ci/containers/ci-runners-runner/stop", nil))); rec.Code != http.StatusOK {
		t.Errorf("stop: %d %s", rec.Code, rec.Body.String())
	}
	if got := fd.actions(); len(got) != 1 || !slices.Equal(got[0], []string{"stop", "ci-runners-runner"}) {
		t.Errorf("actions %v", got)
	}
	// GET is not an action.
	if rec := do(httptest.NewRequest(http.MethodGet, "/api/ci/containers/ci-runners-runner/stop", nil)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET action: %d", rec.Code)
	}

	// Re-run: the URL-encoded repo reaches the handler decoded.
	if rec := do(confirm(httptest.NewRequest(http.MethodPost, "/api/ci/runs/you%2Fyour-repo/1003/rerun", nil))); rec.Code != http.StatusAccepted ||
		!strings.Contains(rec.Body.String(), `"repo":"you/your-repo"`) {
		t.Errorf("rerun: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(confirm(httptest.NewRequest(http.MethodPost, "/api/ci/runs/you%2Fother/1/rerun", nil))); rec.Code != http.StatusNotFound {
		t.Errorf("rerun unknown repo: %d", rec.Code)
	}
	if rec := do(confirm(httptest.NewRequest(http.MethodPost, "/api/ci/runs/you%2Fyour-repo/abc/rerun", nil))); rec.Code != http.StatusBadRequest {
		t.Errorf("rerun bad id: %d", rec.Code)
	}
	if rec := do(httptest.NewRequest(http.MethodPost, "/api/ci/runs/you%2Fyour-repo/1003/rerun", nil)); rec.Code != http.StatusForbidden {
		t.Errorf("rerun without header: %d", rec.Code)
	}
	if rec := do(httptest.NewRequest(http.MethodGet, "/api/ci/runs?repo=you/other", nil)); rec.Code != http.StatusNotFound {
		t.Errorf("runs unknown repo: %d", rec.Code)
	}
}
