package power

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

const testToken = "FIXTURE-POWER-TOKEN-5d1e"

// clock is a fake clock that only moves when told to.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// Wait advances the clock instead of sleeping; like a real wait it fails
// once ctx is done.
func (c *clock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.Add(d)
	return nil
}

type box struct {
	name, image, state, status, labels string
	env                                []string
	mem                                string
}

// fakeDocker is a docker CLI over an in-memory list of containers.
type fakeDocker struct {
	mu       sync.Mutex
	boxes    []*box
	calls    [][]string
	failNext map[string]int // action -> number of failures left
}

func (f *fakeDocker) find(name string) *box {
	for _, b := range f.boxes {
		if b.name == name {
			return b
		}
	}
	return nil
}

func (f *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	switch args[0] {
	case "ps":
		var sb strings.Builder
		for _, b := range f.boxes {
			line, _ := json.Marshal(map[string]string{"ID": "id-" + b.name, "Names": b.name, "Image": b.image, "State": b.state, "Status": b.status, "Labels": b.labels})
			sb.Write(line)
			sb.WriteByte('\n')
		}
		return []byte(sb.String()), nil
	case "inspect":
		var docs []map[string]any
		for _, id := range args[1:] {
			if b := f.find(strings.TrimPrefix(id, "id-")); b != nil {
				docs = append(docs, map[string]any{"Id": "id-" + b.name + "-full", "State": map[string]string{"StartedAt": "2026-10-06T08:00:00Z"}, "Config": map[string]any{"Env": b.env}})
			}
		}
		return json.Marshal(docs)
	case "stats":
		var sb strings.Builder
		for _, b := range f.boxes {
			if b.state == "running" {
				line, _ := json.Marshal(map[string]string{"ID": "id-" + b.name, "Name": b.name, "MemUsage": b.mem})
				sb.Write(line)
				sb.WriteByte('\n')
			}
		}
		return []byte(sb.String()), nil
	case "start", "stop", "restart":
		if f.failNext[args[0]] > 0 {
			f.failNext[args[0]]--
			return nil, fmt.Errorf("docker %s: Error response from daemon: failed to start", args[0])
		}
		b := f.find(args[len(args)-1])
		if b == nil {
			return nil, errors.New("no such container")
		}
		if args[0] == "stop" {
			b.state, b.status = "exited", "Exited (0) 1 second ago"
		} else {
			b.state, b.status = "running", "Up 1 second"
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected docker %v", args)
}

// acted returns the start, stop and restart calls as "action name".
func (f *fakeDocker) acted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if c[0] == "start" || c[0] == "stop" || c[0] == "restart" {
			out = append(out, strings.Join(c, " "))
		}
	}
	return out
}

func (f *fakeDocker) state(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.find(name).state
}

// fakeKube is the cluster API.
type fakeKube struct {
	mu         sync.Mutex
	readyAfter int // Ready fails this many more times
	refreshes  int
	pods, jobs int
	busyErr    error
	isUp       func() bool // Ready also requires this
}

func (k *fakeKube) Refresh() error {
	k.mu.Lock()
	k.refreshes++
	k.mu.Unlock()
	return nil
}

func (k *fakeKube) Ready(context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.isUp != nil && !k.isUp() {
		return errors.New("connection refused")
	}
	if k.readyAfter > 0 {
		k.readyAfter--
		return errors.New("node not ready yet")
	}
	return nil
}

func (k *fakeKube) Busy(context.Context) (int, int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.pods, k.jobs, k.busyErr
}

func (k *fakeKube) set(pods, jobs int) {
	k.mu.Lock()
	k.pods, k.jobs = pods, jobs
	k.mu.Unlock()
}

// fakeGitHub serves queued and in-progress runs and runners per repo, with
// ETags, and counts requests.
type fakeGitHub struct {
	mu       sync.Mutex
	queued   map[string][]int64 // repo -> run ids
	progress map[string][]int64
	busy     map[string]bool // runner name -> busy
	runners  map[string][]string
	fail     bool
	requests int
	etagHits int
	srv      *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	g := &fakeGitHub{queued: map[string][]int64{}, progress: map[string][]int64{}, busy: map[string]bool{}, runners: map[string][]string{}}
	g.srv = httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests++
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if g.fail {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/repos/"), "/")
	if len(parts) < 4 {
		http.NotFound(w, r)
		return
	}
	repo := parts[0] + "/" + parts[1]
	var body []byte
	switch parts[3] {
	case "runs":
		ids := g.queued[repo]
		if r.URL.Query().Get("status") == "in_progress" {
			ids = g.progress[repo]
		}
		runs := []map[string]any{}
		for _, id := range ids {
			runs = append(runs, map[string]any{"id": id, "run_number": id, "name": "CI", "status": r.URL.Query().Get("status")})
		}
		body, _ = json.Marshal(map[string]any{"workflow_runs": runs})
	case "runners":
		rs := []map[string]any{}
		for _, n := range g.runners[repo] {
			rs = append(rs, map[string]any{"id": 1, "name": n, "status": "online", "busy": g.busy[n], "labels": []map[string]string{{"name": "self-hosted"}}})
		}
		body, _ = json.Marshal(map[string]any{"runners": rs})
	default:
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf(`"%x"`, len(body)*31+int(body[len(body)/2]))
	if r.Header.Get("If-None-Match") == etag {
		g.etagHits++
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("ETag", etag)
	_, _ = w.Write(body)
}

func (g *fakeGitHub) set(fn func(g *fakeGitHub)) {
	g.mu.Lock()
	fn(g)
	g.mu.Unlock()
}

func (g *fakeGitHub) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.requests
}

const runnerLabels = "com.docker.compose.project=ci-runners,com.docker.compose.service=runner"

type rig struct {
	s    *Supervisor
	d    *fakeDocker
	k    *fakeKube
	gh   *fakeGitHub
	clk  *clock
	logs *ActivityLog
}

// newRig builds a supervisor with a stopped kind node, one running runner
// for you/app and one stopped runner for you/lib.
func newRig(t *testing.T, cfg config.PowerConfig) *rig {
	t.Helper()
	d := &fakeDocker{failNext: map[string]int{}, boxes: []*box{
		{name: "lucidbench-control-plane", image: "kindest/node:v1", state: "exited", status: "Exited (128) 2 hours ago", labels: "io.x-k8s.kind.cluster=lucidbench", mem: "700MiB / 15GiB"},
		{name: "runner-app", image: "example/github-runner", state: "running", status: "Up 1 hour", labels: runnerLabels, env: []string{"REPO_URL=https://github.com/you/app", "RUNNER_NAME=runner-app", "ACCESS_TOKEN=secret"}, mem: "200MiB / 15GiB"},
		{name: "runner-lib", image: "example/github-runner", state: "exited", status: "Exited (0) 1 hour ago", labels: runnerLabels, env: []string{"REPO_URL=https://github.com/you/lib", "RUNNER_NAME=runner-lib"}},
		{name: "shop-db-1", image: "postgres:17", state: "running", status: "Up 1 day", labels: "com.docker.compose.project=shop", mem: "1GiB / 15GiB"},
		{name: "other-1", image: "busybox", state: "running", status: "Up 1 day", labels: "com.docker.compose.project=other", mem: "10MiB / 15GiB"},
	}}
	k := &fakeKube{}
	k.isUp = func() bool { return d.state("lucidbench-control-plane") == "running" }
	gh := newFakeGitHub(t)
	gh.runners["you/app"] = []string{"runner-app"}
	gh.runners["you/lib"] = []string{"runner-lib"}
	clk := newClock()
	logs := &ActivityLog{Path: filepath.Join(t.TempDir(), "power", "activity.jsonl")}
	s := &Supervisor{
		Cfg:         cfg,
		ClusterName: "lucidbench",
		Docker:      d.run,
		Cluster:     k,
		Runners:     ci.Filter{ComposeProject: "ci-runners"},
		GitHub: &ci.GitHub{BaseURL: gh.srv.URL, TTL: time.Nanosecond, Tokens: &ci.TokenSource{
			Ref: "env:T", Getenv: func(string) string { return testToken },
		}},
		Log:  logs,
		Now:  clk.Now,
		Wait: clk.Wait,
	}
	return &rig{s: s, d: d, k: k, gh: gh, clk: clk, logs: logs}
}

func powerCfg(cluster, runners string) config.PowerConfig {
	return config.PowerConfig{
		Cluster: cluster, ClusterIdleMinutes: 15,
		Runners: runners, RunnerIdleMinutes: 10,
		Stacks:      []config.PowerStack{{Project: "shop", Mode: "on-demand"}},
		PollSeconds: 60,
	}
}

func (r *rig) activity(t *testing.T) []Entry {
	t.Helper()
	es, err := r.logs.Recent(100)
	if err != nil {
		t.Fatal(err)
	}
	return es
}
