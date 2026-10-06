// Package power is on-demand infrastructure. It starts the local kind
// cluster, the self-hosted runner containers and listed compose stacks when
// something needs them, and stops them once they have been idle, so they do
// not have to run all the time.
//
// What counts as need, and as idle:
//
//   - Cluster: a job submitted through the API (see jobs.EnsureCluster) or
//     the Start button wakes it. Reading the Kubernetes page never wakes it
//     and never counts as activity, so an open page does not keep it awake.
//     It is idle when the lucidbench namespace has no running or pending pod,
//     no job is active, and no job has been submitted for
//     power.cluster_idle_minutes.
//   - Runners: a queued workflow run in a runner's repository (its REPO_URL)
//     wakes the stopped runner. A runner is idle when GitHub reports it not
//     busy and its repository has no queued or in-progress run. A busy runner
//     is never stopped, and nothing is stopped while GitHub cannot be read.
//   - Stacks: there is no signal; they start and stop from the UI, and "Sleep
//     everything idle" stops the on-demand ones.
//
// Modes: always (Lucidbench never stops it; a job still starts a stopped
// cluster), on-demand, and off (never started or stopped on its own). Only
// on-demand runners cause GitHub requests. The Start and Stop buttons work
// in every mode. Every action is appended to <DataDir>/power/activity.jsonl.
package power

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// ClusterAPI is what the supervisor needs from the cluster's API server.
type ClusterAPI interface {
	// Refresh writes the kubeconfig again after the node started.
	Refresh() error
	// Ready returns nil when the API answers and every node is Ready.
	Ready(ctx context.Context) error
	// Busy counts running or pending pods in the lucidbench namespace and
	// active jobs.
	Busy(ctx context.Context) (pods, jobs int, err error)
}

// DefaultStartTimeout is how long a cluster start may take before it fails.
const DefaultStartTimeout = 120 * time.Second

// Supervisor applies the power config. Build it with New, or fill the
// fields directly in tests; Run drives it.
type Supervisor struct {
	Cfg          config.PowerConfig
	ClusterName  string
	Docker       docker.Func
	Cluster      ClusterAPI
	Runners      ci.Filter
	GitHub       *ci.GitHub
	Log          *ActivityLog
	Now          func() time.Time
	Wait         func(ctx context.Context, d time.Duration) error
	StartTimeout time.Duration

	clusterMu sync.Mutex // one cluster start or stop at a time

	mu        sync.Mutex // guards the fields below
	begun     bool
	cl        clusterState
	runners   map[string]*runnerState // by container name
	repos     map[string]*repoState
	ticked    time.Time
	stats     map[string]int64 // memory per container name
	statsAt   time.Time
	statsBusy bool
}

type clusterState struct {
	starting  bool
	stopping  bool
	active    time.Time // last activity: a job submitted, a pod running, a start
	pods      int
	jobs      int
	busyKnown bool
	err       string
	errAt     time.Time
}

// New builds a supervisor for the user config, with the real docker CLI,
// the cluster API and the GitHub API.
func New(cfg *config.Config, dataDir string) *Supervisor {
	return &Supervisor{
		Cfg:         cfg.Power,
		ClusterName: cfg.Cluster.Name,
		Docker:      docker.Exec,
		Cluster:     Kube{},
		Runners:     ci.Filter{ComposeProject: cfg.CI.Runners.ComposeProject, ImageMatch: cfg.CI.Runners.ImageMatch},
		GitHub:      &ci.GitHub{Tokens: &ci.TokenSource{Ref: cfg.CI.GitHub.Token, Getenv: os.Getenv, GH: ci.GHAuthToken}},
		Log:         &ActivityLog{Path: filepath.Join(dataDir, "power", "activity.jsonl")},
	}
}

func (s *Supervisor) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Supervisor) wait(ctx context.Context, d time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *Supervisor) poll() time.Duration {
	if s.Cfg.PollSeconds < config.MinPowerPollSeconds {
		return config.DefaultPowerPollSeconds * time.Second
	}
	return time.Duration(s.Cfg.PollSeconds) * time.Second
}

// begin sets up state on first use. The cluster counts as active from the
// moment the supervisor starts, so a daemon restart never stops it at once.
// The caller holds s.mu.
func (s *Supervisor) begin() {
	if s.begun {
		return
	}
	s.begun = true
	s.cl.active = s.now()
	s.runners = map[string]*runnerState{}
	s.repos = map[string]*repoState{}
}

func (s *Supervisor) record(e Entry) {
	if e.At.IsZero() {
		e.At = s.now()
	}
	if s.Log != nil {
		_ = s.Log.Append(e)
	}
}

// Run checks the cluster and the runners every poll_seconds until ctx ends.
func (s *Supervisor) Run(ctx context.Context) {
	t := time.NewTicker(s.poll())
	defer t.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Tick checks everything once: it stops an idle on-demand cluster, and
// starts or stops on-demand runners.
func (s *Supervisor) Tick(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	s.mu.Lock()
	s.begin()
	s.ticked = s.now()
	s.mu.Unlock()
	s.tickCluster(ctx)
	s.tickRunners(ctx)
	// Warm the memory sample so GET /api/power never waits for docker stats.
	s.memory(ctx)
}

// ---- cluster ----

// Errors from the cluster actions.
var (
	ErrNoCluster   = errors.New("no local cluster: run `lucid cluster up`")
	ErrClusterOff  = errors.New("the cluster is stopped and power.cluster is off: start it from the Kubernetes page")
	ErrClusterBusy = errors.New("the cluster has running pods or active jobs")
)

// nodes returns the kind node containers of the cluster.
func (s *Supervisor) nodes(ctx context.Context) ([]docker.Container, error) {
	out, err := s.Docker(ctx, "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	cs, err := docker.ParsePS(out)
	if err != nil {
		return nil, err
	}
	var ns []docker.Container
	for _, c := range cs {
		if c.KindCluster == s.ClusterName {
			ns = append(ns, c)
		}
	}
	return ns, nil
}

// nodesUp reports whether there are nodes and all of them run.
func nodesUp(ns []docker.Container) bool {
	for _, n := range ns {
		if n.State != "running" {
			return false
		}
	}
	return len(ns) > 0
}

// Touch records cluster activity, which postpones an idle stop.
func (s *Supervisor) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begin()
	s.cl.active = s.now()
}

// EnsureCluster is called before a job is submitted. In always and
// on-demand mode it starts a stopped cluster and waits until it is ready; in
// off mode a stopped cluster is an error.
func (s *Supervisor) EnsureCluster(ctx context.Context) error {
	s.Touch()
	if s.Cfg.Cluster == config.PowerOff {
		ns, err := s.nodes(ctx)
		switch {
		case err != nil:
			return err
		case len(ns) == 0:
			return ErrNoCluster
		case !nodesUp(ns):
			return ErrClusterOff
		}
		return nil
	}
	return s.StartCluster(ctx, true, "a job was submitted")
}

// MarkStarting shows the cluster as starting before an asynchronous start
// has taken the lock.
func (s *Supervisor) MarkStarting() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.begin()
	s.cl.starting = true
}

func (s *Supervisor) clusterFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cl.err, s.cl.errAt = err.Error(), s.now()
}

// StartCluster starts the stopped kind node and waits until the API answers
// and every node is Ready. A node that will not start is tried three times
// (after a Docker restart a node can sit in "Exited (128)" and fail its
// first start), and a node that started but whose API is not ready halfway
// through the timeout is restarted once.
func (s *Supervisor) StartCluster(ctx context.Context, auto bool, reason string) (err error) {
	s.clusterMu.Lock()
	defer s.clusterMu.Unlock()
	s.mu.Lock()
	s.begin()
	s.cl.starting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cl.starting = false
		s.mu.Unlock()
	}()

	ns, err := s.nodes(ctx)
	if err != nil {
		return err
	}
	if len(ns) == 0 {
		return ErrNoCluster
	}
	if nodesUp(ns) {
		if s.Cluster.Ready(ctx) == nil {
			s.Touch()
			return nil
		}
	}
	began := s.now()
	timeout := s.StartTimeout
	if timeout == 0 {
		timeout = DefaultStartTimeout
	}
	e := Entry{Kind: "cluster", Name: s.ClusterName, Action: "start", Auto: auto, Reason: reason}
	fail := func(err error) error {
		e.Error = err.Error()
		e.Seconds = s.now().Sub(began).Seconds()
		s.record(e)
		s.clusterFailed(err)
		return err
	}

	started := false
	for _, n := range ns {
		if n.State == "running" {
			continue
		}
		if e.Was == "" {
			e.Was = n.Status
		}
		var startErr error
		for attempt := 1; attempt <= 3; attempt++ {
			if _, startErr = s.Docker(ctx, "start", n.Name); startErr == nil {
				break
			}
			if err := s.wait(ctx, 2*time.Second); err != nil {
				return fail(err)
			}
		}
		if startErr != nil {
			return fail(fmt.Errorf("start node %s: %w", n.Name, startErr))
		}
		started = true
	}

	restarted := false
	for {
		_ = s.Cluster.Refresh()
		lastErr := s.Cluster.Ready(ctx)
		if lastErr == nil {
			break
		}
		elapsed := s.now().Sub(began)
		if elapsed >= timeout {
			return fail(fmt.Errorf("the cluster did not become ready in %s: %w", timeout, lastErr))
		}
		if started && !restarted && elapsed >= timeout/2 {
			restarted = true
			e.Restarted = true
			for _, n := range ns {
				if _, err := s.Docker(ctx, "restart", n.Name); err != nil {
					return fail(fmt.Errorf("restart node %s: %w", n.Name, err))
				}
			}
		}
		if err := s.wait(ctx, 2*time.Second); err != nil {
			return fail(err)
		}
	}
	if started {
		e.OK = true
		e.Seconds = s.now().Sub(began).Seconds()
		s.record(e)
	}
	s.mu.Lock()
	s.cl.err, s.cl.errAt = "", time.Time{}
	s.cl.active = s.now()
	s.mu.Unlock()
	return nil
}

// StopCluster stops the kind node unless pods run or jobs are active. An
// automatic stop also checks again that the cluster is still idle.
func (s *Supervisor) StopCluster(ctx context.Context, auto bool, reason string) error {
	s.clusterMu.Lock()
	defer s.clusterMu.Unlock()
	ns, err := s.nodes(ctx)
	if err != nil {
		return err
	}
	if len(ns) == 0 {
		return ErrNoCluster
	}
	running := []string{}
	for _, n := range ns {
		if n.State == "running" {
			running = append(running, n.Name)
		}
	}
	if len(running) == 0 {
		return nil
	}
	pods, jobs, err := s.Cluster.Busy(ctx)
	if err != nil {
		return fmt.Errorf("could not check the cluster is idle: %w", err)
	}
	if pods+jobs > 0 {
		s.mu.Lock()
		s.cl.active = s.now()
		s.mu.Unlock()
		return ErrClusterBusy
	}
	if auto {
		s.mu.Lock()
		idle := s.now().Sub(s.cl.active)
		s.mu.Unlock()
		if idle < s.clusterIdle() {
			return nil
		}
	}
	s.mu.Lock()
	s.cl.stopping = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cl.stopping = false
		s.mu.Unlock()
	}()
	began := s.now()
	e := Entry{Kind: "cluster", Name: s.ClusterName, Action: "stop", Auto: auto, Reason: reason}
	for _, n := range running {
		if _, err := s.Docker(ctx, "stop", n); err != nil {
			e.Error = err.Error()
			s.record(e)
			s.clusterFailed(err)
			return err
		}
	}
	e.OK = true
	e.Seconds = s.now().Sub(began).Seconds()
	s.record(e)
	return nil
}

func (s *Supervisor) clusterIdle() time.Duration {
	return time.Duration(s.Cfg.ClusterIdleMinutes) * time.Minute
}

// tickCluster tracks cluster activity and stops an idle on-demand cluster.
func (s *Supervisor) tickCluster(ctx context.Context) {
	ns, err := s.nodes(ctx)
	if err != nil || !nodesUp(ns) {
		return
	}
	s.mu.Lock()
	busyNow := s.cl.starting || s.cl.stopping
	s.mu.Unlock()
	if busyNow {
		return
	}
	pods, jobs, err := s.Cluster.Busy(ctx)
	s.mu.Lock()
	s.cl.busyKnown = err == nil
	s.cl.pods, s.cl.jobs = pods, jobs
	if err == nil && pods+jobs > 0 {
		s.cl.active = s.now()
	}
	idle := s.now().Sub(s.cl.active)
	s.mu.Unlock()
	if err != nil || s.Cfg.Cluster != config.PowerOnDemand || idle < s.clusterIdle() {
		return
	}
	reason := fmt.Sprintf("idle for %d min", int(idle.Minutes()))
	if err := s.StopCluster(ctx, true, reason); err != nil && !errors.Is(err, ErrClusterBusy) {
		s.clusterFailed(err)
	}
}
