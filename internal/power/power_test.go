package power

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

var ctx = context.Background()

func TestRunnerStartsOnQueuedRun(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.gh.set(func(g *fakeGitHub) { g.queued["you/lib"] = []int64{42} })
	r.s.Tick(ctx)
	if got := r.d.acted(); !slices.Equal(got, []string{"start runner-lib"}) {
		t.Fatalf("acted %v", got)
	}
	es := r.activity(t)
	if len(es) != 1 || es[0].Kind != "runner" || es[0].Action != "start" || !es[0].Auto || !es[0].OK || !strings.Contains(es[0].Reason, "#42 queued in you/lib") {
		t.Fatalf("activity %+v", es)
	}
	// The run is picked up; nothing new is started on the next tick.
	r.s.Tick(ctx)
	if got := r.d.acted(); len(got) != 1 {
		t.Fatalf("second tick acted %v", got)
	}
}

func TestRunnerStopsAfterIdle(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.s.Tick(ctx) // first idle sighting
	r.clk.Add(9 * time.Minute)
	r.s.Tick(ctx)
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("stopped before the idle timeout: %v", got)
	}
	st := r.s.State(ctx)
	if it := st.Runners[0]; it.Name != "runner-app" || it.IdleSeconds == nil || *it.IdleSeconds != 540 || *it.StopsInSeconds != 60 {
		t.Fatalf("runner state %+v", it)
	}
	r.clk.Add(2 * time.Minute)
	r.s.Tick(ctx)
	if got := r.d.acted(); !slices.Equal(got, []string{"stop runner-app"}) {
		t.Fatalf("acted %v", got)
	}
	if es := r.activity(t); len(es) != 1 || es[0].Action != "stop" || !es[0].Auto || es[0].Reason != "idle for 11 min" {
		t.Fatalf("activity %+v", es)
	}
	if st := r.s.State(ctx); st.Runners[0].State != "sleeping" {
		t.Fatalf("stopped on-demand runner should be sleeping: %+v", st.Runners[0])
	}
}

func TestActiveRunsKeepRunnerAwake(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.gh.set(func(g *fakeGitHub) { g.progress["you/app"] = []int64{7} })
	for i := 0; i < 5; i++ {
		r.s.Tick(ctx)
		r.clk.Add(10 * time.Minute)
	}
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("a runner whose repo has a run in progress was stopped: %v", got)
	}
	// The run finishes: the idle clock starts then, not at the first tick.
	r.gh.set(func(g *fakeGitHub) { g.progress["you/app"] = nil })
	r.s.Tick(ctx)
	r.clk.Add(5 * time.Minute)
	r.s.Tick(ctx)
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("stopped 5 minutes after the run: %v", got)
	}
}

func TestBusyRunnerIsNeverStopped(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.gh.set(func(g *fakeGitHub) { g.busy["runner-app"] = true })
	for i := 0; i < 6; i++ {
		r.s.Tick(ctx)
		r.clk.Add(30 * time.Minute)
	}
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("busy runner stopped: %v", got)
	}
	if st := r.s.State(ctx); !st.Runners[0].Busy || st.Runners[0].IdleSeconds != nil {
		t.Fatalf("state %+v", st.Runners[0])
	}
	// Sleep everything idle and the Stop button refuse it too.
	out := r.s.SleepIdle(ctx)
	for _, o := range out {
		if o.Name == "runner-app" && o.Stopped {
			t.Fatalf("sleep stopped a busy runner: %+v", out)
		}
	}
	if err := r.s.StopRunner(ctx, "runner-app"); !errors.Is(err, ErrRunnerBusy) {
		t.Fatalf("StopRunner = %v", err)
	}
	if got := r.d.acted(); slices.Contains(got, "stop runner-app") {
		t.Fatalf("acted %v", got)
	}
}

func TestStaleNotBusyIsCheckedAgainBeforeStopping(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.s.GitHub.TTL = time.Hour // the runners list stays cached as not busy
	r.s.Tick(ctx)
	r.clk.Add(11 * time.Minute)
	r.gh.set(func(g *fakeGitHub) { g.busy["runner-app"] = true }) // picked up a job since
	r.s.Tick(ctx)
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("stopped a runner that had just become busy: %v", got)
	}
}

func TestNothingStopsWhileGitHubFails(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.gh.set(func(g *fakeGitHub) { g.fail = true })
	r.s.Tick(ctx)
	r.clk.Add(30 * time.Second) // inside the backoff
	n := r.gh.count()
	r.s.Tick(ctx)
	if r.gh.count() != n {
		t.Fatalf("asked GitHub during the backoff: %d -> %d", n, r.gh.count())
	}
	r.clk.Add(time.Hour)
	r.s.Tick(ctx)
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("acted while GitHub failed: %v", got)
	}
	if st := r.s.State(ctx); !strings.Contains(st.Runners[0].Detail, "GitHub") {
		t.Fatalf("detail %q", st.Runners[0].Detail)
	}
}

func TestETagSavesRequests(t *testing.T) {
	r := newRig(t, powerCfg("off", "on-demand"))
	r.s.Tick(ctx)
	r.s.Tick(ctx)
	r.gh.mu.Lock()
	hits := r.gh.etagHits
	r.gh.mu.Unlock()
	if hits < 4 { // queued and in_progress for two repos
		t.Fatalf("conditional hits %d", hits)
	}
}

func TestRunnerModesAlwaysAndOffAreHandsOff(t *testing.T) {
	for _, mode := range []string{"always", "off"} {
		r := newRig(t, powerCfg("off", mode))
		r.gh.set(func(g *fakeGitHub) { g.queued["you/lib"] = []int64{1} })
		for i := 0; i < 4; i++ {
			r.s.Tick(ctx)
			r.clk.Add(time.Hour)
		}
		if got := r.d.acted(); len(got) != 0 {
			t.Errorf("%s: acted %v", mode, got)
		}
		if n := r.gh.count(); n != 0 {
			t.Errorf("%s: %d GitHub requests", mode, n)
		}
		st := r.s.State(ctx)
		if st.Runners[1].State != "stopped" || st.Runners[1].Mode != mode {
			t.Errorf("%s: %+v", mode, st.Runners[1])
		}
	}
}

func TestClusterEnsureStartsAfterExited128(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	r.d.failNext["start"] = 1 // the first start after a Docker restart fails
	r.k.readyAfter = 3
	if err := r.s.EnsureCluster(ctx); err != nil {
		t.Fatal(err)
	}
	if got := r.d.acted(); !slices.Equal(got, []string{"start lucidbench-control-plane", "start lucidbench-control-plane"}) {
		t.Fatalf("acted %v", got)
	}
	if r.k.refreshes == 0 {
		t.Error("kubeconfig not refreshed after the start")
	}
	es := r.activity(t)
	if len(es) != 1 || !es[0].OK || es[0].Kind != "cluster" || es[0].Reason != "a job was submitted" || !strings.HasPrefix(es[0].Was, "Exited (128)") {
		t.Fatalf("activity %+v", es)
	}
	// Running and ready: a second job does not start anything.
	if err := r.s.EnsureCluster(ctx); err != nil || len(r.d.acted()) != 2 {
		t.Fatalf("second ensure: %v %v", err, r.d.acted())
	}
}

func TestClusterRestartedOnceWhenAPINeverComesUp(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	r.s.StartTimeout = 20 * time.Second
	r.k.readyAfter = 1000
	err := r.s.EnsureCluster(ctx)
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("err %v", err)
	}
	restarts := 0
	for _, a := range r.d.acted() {
		if strings.HasPrefix(a, "restart") {
			restarts++
		}
	}
	if restarts != 1 {
		t.Fatalf("restarts %d: %v", restarts, r.d.acted())
	}
	st := r.s.State(ctx)
	if st.Cluster.Error == "" || st.Cluster.ErrorAt == nil {
		t.Fatalf("failure not shown: %+v", st.Cluster)
	}
	if es := r.activity(t); len(es) != 1 || es[0].OK || !es[0].Restarted {
		t.Fatalf("activity %+v", es)
	}
}

func TestClusterIdleStop(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	if err := r.s.EnsureCluster(ctx); err != nil {
		t.Fatal(err)
	}
	r.k.set(1, 1) // the job runs
	r.clk.Add(20 * time.Minute)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "running" {
		t.Fatal("stopped while a pod runs")
	}
	r.k.set(0, 0) // the job finished
	r.clk.Add(14 * time.Minute)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "running" {
		t.Fatal("stopped before the idle timeout")
	}
	if st := r.s.State(ctx); st.Cluster.State != "running" || st.Cluster.StopsInSeconds == nil || *st.Cluster.StopsInSeconds != 60 {
		t.Fatalf("state %+v", st.Cluster)
	}
	// Reading the cluster is not activity; a submitted job is.
	r.clk.Add(30 * time.Second)
	r.s.Touch()
	r.clk.Add(time.Minute)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "running" {
		t.Fatal("stopped right after a job was submitted")
	}
	r.clk.Add(15 * time.Minute)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "exited" {
		t.Fatal("idle cluster not stopped")
	}
	es := r.activity(t)
	if es[0].Action != "stop" || !es[0].Auto || es[0].Reason != "idle for 16 min" {
		t.Fatalf("activity %+v", es[0])
	}
	if st := r.s.State(ctx); st.Cluster.State != "sleeping" {
		t.Fatalf("state %q", st.Cluster.State)
	}
}

func TestEnsureSurvivesACancelledRequest(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	r.k.readyAfter = 2
	cctx, cancel := context.WithCancel(ctx)
	cancel() // the browser went away before the node was ready
	if err := r.s.EnsureCluster(cctx); err != nil {
		t.Fatalf("a cancelled request aborted the start: %v", err)
	}
	if st := r.s.State(ctx); st.Cluster.State != "running" || st.Cluster.Error != "" {
		t.Fatalf("state %+v", st.Cluster)
	}
}

func TestClusterErrorClearsOnceHealthy(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	r.s.StartTimeout = 10 * time.Second
	r.k.readyAfter = 1000
	_ = r.s.EnsureCluster(ctx) // fails: never ready
	if r.s.State(ctx).Cluster.Error == "" {
		t.Fatal("no error after a failed start")
	}
	r.k.readyAfter = 0 // the node came up after all
	r.s.Tick(ctx)
	if e := r.s.State(ctx).Cluster.Error; e != "" {
		t.Fatalf("error stuck after the cluster became healthy: %q", e)
	}
}

func TestClusterBusyUnknownIsNotIdle(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	_ = r.s.EnsureCluster(ctx)
	r.k.busyErr = errors.New("api down")
	r.clk.Add(time.Hour)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "running" {
		t.Fatal("stopped a cluster whose pods could not be read")
	}
}

func TestClusterModes(t *testing.T) {
	// always: started for a job, never stopped when idle.
	r := newRig(t, powerCfg("always", "always"))
	if err := r.s.EnsureCluster(ctx); err != nil {
		t.Fatal(err)
	}
	r.clk.Add(10 * time.Hour)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "running" {
		t.Fatal("always: idle cluster stopped")
	}
	if out := r.s.SleepIdle(ctx); slices.ContainsFunc(out, func(o Outcome) bool { return o.Kind == "cluster" }) {
		t.Fatalf("always: sleep touched the cluster: %+v", out)
	}

	// off: a job does not start it, and a running one is never stopped.
	r = newRig(t, powerCfg("off", "always"))
	if err := r.s.EnsureCluster(ctx); !errors.Is(err, ErrClusterOff) {
		t.Fatalf("off: ensure = %v", err)
	}
	if got := r.d.acted(); len(got) != 0 {
		t.Fatalf("off: acted %v", got)
	}
	r.d.find("lucidbench-control-plane").state = "running"
	r.clk.Add(10 * time.Hour)
	r.s.Tick(ctx)
	if r.d.state("lucidbench-control-plane") != "running" {
		t.Fatal("off: cluster stopped")
	}
	if err := r.s.EnsureCluster(ctx); err != nil {
		t.Fatalf("off, running: %v", err)
	}
	if st := r.s.State(ctx); st.Cluster.Mode != "off" {
		t.Fatalf("mode %q", st.Cluster.Mode)
	}
}

func TestNoClusterIsReported(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "always"))
	r.d.boxes = r.d.boxes[1:]
	if err := r.s.EnsureCluster(ctx); !errors.Is(err, ErrNoCluster) {
		t.Fatalf("ensure = %v", err)
	}
	if st := r.s.State(ctx); st.Cluster.State != "missing" {
		t.Fatalf("state %q", st.Cluster.State)
	}
}

func TestSleepIdle(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "on-demand"))
	_ = r.s.EnsureCluster(ctx)
	out := r.s.SleepIdle(ctx)
	stopped := map[string]bool{}
	for _, o := range out {
		stopped[o.Kind+"/"+o.Name] = o.Stopped
	}
	want := map[string]bool{"cluster/lucidbench": true, "runner/runner-app": true, "stack/shop": true}
	for k, v := range want {
		if stopped[k] != v {
			t.Errorf("%s stopped=%v, outcomes %+v", k, stopped[k], out)
		}
	}
	if r.d.state("other-1") != "running" {
		t.Error("an unlisted project was stopped")
	}
	// A busy cluster is left running.
	r = newRig(t, powerCfg("on-demand", "always"))
	_ = r.s.EnsureCluster(ctx)
	r.k.set(1, 0)
	if out := r.s.SleepIdle(ctx); len(out) != 2 || out[0].Stopped || !strings.Contains(out[0].Reason, "busy") {
		t.Fatalf("outcomes %+v", out)
	}
}

func TestStateMemoryAndCounts(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "on-demand"))
	st := r.s.State(ctx)
	if st.Cluster.State != "sleeping" || st.Modes.Cluster != "on-demand" || st.PollSeconds != 60 {
		t.Fatalf("cluster %+v", st.Cluster)
	}
	if len(st.Runners) != 2 || st.Runners[0].Repo != "you/app" || st.Runners[1].State != "sleeping" {
		t.Fatalf("runners %+v", st.Runners)
	}
	if len(st.Stacks) != 1 || st.Stacks[0].State != "running" || st.Stacks[0].MemoryBytes != 1<<30 {
		t.Fatalf("stacks %+v", st.Stacks)
	}
	if st.Running != 2 || st.Sleeping != 2 {
		t.Fatalf("running %d sleeping %d", st.Running, st.Sleeping)
	}
	if !st.Memory.Known || st.Memory.ManagedBytes != 200<<20+1<<30 || st.Memory.DockerBytes != 200<<20+1<<30+10<<20 {
		t.Fatalf("memory %+v", st.Memory)
	}
	b, _ := json.Marshal(st)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "ACCESS_TOKEN") {
		t.Fatalf("state leaks container env: %s", b)
	}
}

func TestParseMemory(t *testing.T) {
	for in, want := range map[string]int64{
		"781MiB / 15GiB": 781 << 20, "1.5GiB / 15GiB": 3 << 29, "512KiB / 1GiB": 512 << 10,
		"12MB / 1GB": 12e6, "0B / 0B": 0, "": 0, "--": 0,
	} {
		if got := ParseMemory(in); got != want {
			t.Errorf("%q = %d, want %d", in, got, want)
		}
	}
}

func TestActivityLogTrims(t *testing.T) {
	r := newRig(t, powerCfg("off", "always"))
	big := strings.Repeat("x", 1500)
	for i := 0; i < 800; i++ {
		_ = r.logs.Append(Entry{Kind: "runner", Name: "r", Action: "start", Reason: big})
	}
	lines, _ := r.logs.lines()
	if len(lines) > KeepLines || len(lines) < 100 {
		t.Fatalf("%d lines after trimming", len(lines))
	}
	_ = r.logs.Append(Entry{Kind: "cluster", Name: "last", Action: "stop"})
	if es, _ := r.logs.Recent(1); es[0].Name != "last" {
		t.Fatalf("newest first: %+v", es)
	}
}

func TestHTTP(t *testing.T) {
	r := newRig(t, powerCfg("on-demand", "on-demand"))
	mux := http.NewServeMux()
	Register(mux, r.s)
	do := func(method, path string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := do("GET", "/api/power", false)
	var st State
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &st) != nil || st.Cluster.Name != "lucidbench" {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/power/stack/shop/stop", "/api/power/sleep", "/api/power/cluster/lucidbench/start"} {
		if rec := do("POST", p, false); rec.Code != http.StatusForbidden {
			t.Errorf("%s without confirm: %d", p, rec.Code)
		}
	}
	if len(r.d.acted()) != 0 {
		t.Fatalf("acted without confirm: %v", r.d.acted())
	}
	for path, code := range map[string]int{
		"/api/power/stack/shop/stop":         200,
		"/api/power/stack/other/stop":        404,
		"/api/power/runner/shop-db-1/stop":   404,
		"/api/power/runner/runner-lib/start": 200,
		"/api/power/cluster/nope/start":      404,
		"/api/power/disk/x/start":            404,
		"/api/power/stack/shop/restart":      400,
	} {
		if rec := do("POST", path, true); rec.Code != code {
			t.Errorf("POST %s = %d, want %d (%s)", path, rec.Code, code, rec.Body)
		}
	}
	if r.d.state("other-1") != "running" || r.d.state("shop-db-1") != "exited" || r.d.state("runner-lib") != "running" {
		t.Fatalf("states after actions: %v", r.d.acted())
	}

	rec = do("POST", "/api/power/cluster/lucidbench/start", true)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start cluster: %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.d.state("lucidbench-control-plane") != "running" || r.s.State(ctx).Cluster.State == "starting" {
		if time.Now().After(deadline) {
			t.Fatal("cluster start did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.k.set(2, 0)
	if rec := do("POST", "/api/power/cluster/lucidbench/stop", true); rec.Code != http.StatusConflict {
		t.Fatalf("stop busy cluster: %d", rec.Code)
	}
	r.k.set(0, 0)
	if rec := do("POST", "/api/power/cluster/lucidbench/stop", true); rec.Code != 200 {
		t.Fatalf("stop idle cluster: %d %s", rec.Code, rec.Body)
	}
	r.gh.set(func(g *fakeGitHub) { g.busy["runner-app"] = true })
	if rec := do("POST", "/api/power/runner/runner-app/stop", true); rec.Code != http.StatusConflict {
		t.Fatalf("stop busy runner: %d", rec.Code)
	}
	rec = do("POST", "/api/power/sleep", true)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"outcomes"`) {
		t.Fatalf("sleep: %d %s", rec.Code, rec.Body)
	}
	_ = config.PowerOff
}
