package power

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// MaxBackoff caps the wait after GitHub errors for one repository.
const MaxBackoff = 15 * time.Minute

type runnerState struct {
	idleSince time.Time // when the runner was first seen idle; zero while busy or unknown
	busy      bool
	known     bool // GitHub knows a runner by this name
	labels    []string
	err       string
	errAt     time.Time
}

type repoState struct {
	etag     map[string]string
	runs     map[string][]ci.Run
	until    time.Time // no GitHub request before this
	failures int
	err      string
	readAt   time.Time
}

// runStatuses are the run states that mean a repository needs its runner.
var runStatuses = []string{"queued", "in_progress"}

// ErrRunnerBusy is returned when asked to stop a runner that runs a job.
var ErrRunnerBusy = errors.New("the runner is running a job")

func (s *Supervisor) runner(name string) *runnerState {
	r, ok := s.runners[name]
	if !ok {
		r = &runnerState{}
		s.runners[name] = r
	}
	return r
}

func (s *Supervisor) repo(name string) *repoState {
	r, ok := s.repos[name]
	if !ok {
		r = &repoState{etag: map[string]string{}, runs: map[string][]ci.Run{}}
		s.repos[name] = r
	}
	return r
}

// activeRuns reads the repository's queued and in-progress runs with
// conditional requests. ok is false when they are unknown (GitHub failed or
// is being backed off), and then nothing may be decided from them.
func (s *Supervisor) activeRuns(ctx context.Context, repo string) (queued, inProgress []ci.Run, ok bool) {
	s.mu.Lock()
	rs := s.repo(repo)
	wait := s.now().Before(rs.until)
	etags := map[string]string{}
	for k, v := range rs.etag {
		etags[k] = v
	}
	s.mu.Unlock()
	if wait {
		return nil, nil, false
	}
	got := map[string][]ci.Run{}
	for _, st := range runStatuses {
		runs, tag, err := s.GitHub.RunsByStatus(ctx, repo, st, etags[st])
		switch {
		case errors.Is(err, ci.ErrNotModified):
			continue
		case err != nil:
			s.mu.Lock()
			rs.failures++
			back := s.poll() << min(rs.failures, 6)
			if back > MaxBackoff {
				back = MaxBackoff
			}
			rs.until = s.now().Add(back)
			var api *ci.APIError
			if errors.As(err, &api) && api.RetryAt.After(rs.until) {
				rs.until = api.RetryAt
			}
			rs.err = err.Error()
			s.mu.Unlock()
			return nil, nil, false
		}
		got[st] = runs
		etags[st] = tag
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for st, runs := range got {
		rs.runs[st] = runs
		rs.etag[st] = etags[st]
	}
	rs.failures, rs.err, rs.until, rs.readAt = 0, "", time.Time{}, s.now()
	return rs.runs["queued"], rs.runs["in_progress"], true
}

// findRunner matches a container to the GitHub runner it registers: by
// RUNNER_NAME, else by container name.
func findRunner(rs []ci.Runner, c ci.Container) (ci.Runner, bool) {
	name := c.RunnerName
	if name == "" {
		name = c.Name
	}
	for _, r := range rs {
		if strings.EqualFold(r.Name, name) {
			return r, true
		}
	}
	return ci.Runner{}, false
}

func (s *Supervisor) runnerFailed(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.runner(name)
	r.err, r.errAt = err.Error(), s.now()
}

// tickRunners starts the stopped runners of repositories with a queued run
// and stops runners that have been idle long enough. It does nothing, and
// asks GitHub nothing, unless power.runners is on-demand.
func (s *Supervisor) tickRunners(ctx context.Context) {
	if s.Cfg.Runners != config.PowerOnDemand || s.GitHub == nil {
		return
	}
	cs, err := ci.ListContainers(ctx, s.Docker, s.Runners)
	if err != nil {
		return
	}
	byRepo := map[string][]ci.Container{}
	for _, c := range cs {
		if repo := c.Repo(); repo != "" {
			byRepo[repo] = append(byRepo[repo], c)
		}
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	for _, repo := range repos {
		s.tickRepo(ctx, repo, byRepo[repo])
	}
}

func (s *Supervisor) tickRepo(ctx context.Context, repo string, cs []ci.Container) {
	queued, inProgress, ok := s.activeRuns(ctx, repo)
	if !ok {
		return
	}
	now := s.now()
	if len(queued) > 0 {
		for _, c := range cs {
			if c.Up() {
				continue
			}
			reason := fmt.Sprintf("%d queued run(s) in %s", len(queued), repo)
			if r := queued[0]; r.Name != "" {
				reason = fmt.Sprintf("%s #%d queued in %s", r.Name, r.RunNumber, repo)
			}
			if err := s.runnerAction(ctx, c.Name, "start", true, reason); err == nil {
				s.mu.Lock()
				s.runner(c.Name).idleSince = time.Time{}
				s.mu.Unlock()
			}
		}
	}
	var running []ci.Container
	for _, c := range cs {
		if c.Up() {
			running = append(running, c)
		}
	}
	if len(running) == 0 {
		return
	}
	ghRunners, err := s.GitHub.Runners(ctx, repo)
	if err != nil {
		for _, c := range running {
			s.runnerFailed(c.Name, err)
		}
		return
	}
	idleFor := time.Duration(s.Cfg.RunnerIdleMinutes) * time.Minute
	active := len(queued) + len(inProgress)
	for _, c := range running {
		r, found := findRunner(ghRunners, c)
		s.mu.Lock()
		st := s.runner(c.Name)
		st.known, st.busy, st.labels, st.err = found, found && r.Busy, r.Labels, ""
		if st.busy || active > 0 {
			st.idleSince = time.Time{}
			s.mu.Unlock()
			continue
		}
		if st.idleSince.IsZero() {
			st.idleSince = now
		}
		idle := now.Sub(st.idleSince)
		s.mu.Unlock()
		if idle < idleFor {
			continue
		}
		reason := fmt.Sprintf("idle for %d min", int(idle.Minutes()))
		_ = s.stopIdleRunner(ctx, repo, c, true, reason)
	}
}

// stopIdleRunner stops a runner after asking GitHub again, without the
// cache, that it is not busy.
func (s *Supervisor) stopIdleRunner(ctx context.Context, repo string, c ci.Container, auto bool, reason string) error {
	fresh, err := s.GitHub.RunnersNow(ctx, repo)
	if err != nil {
		s.runnerFailed(c.Name, err)
		return fmt.Errorf("could not check the runner is idle: %w", err)
	}
	if r, ok := findRunner(fresh, c); ok && r.Busy {
		s.mu.Lock()
		st := s.runner(c.Name)
		st.busy, st.idleSince = true, time.Time{}
		s.mu.Unlock()
		return ErrRunnerBusy
	}
	return s.runnerAction(ctx, c.Name, "stop", auto, reason)
}

// runnerAction starts or stops a runner container and records it.
func (s *Supervisor) runnerAction(ctx context.Context, name, action string, auto bool, reason string) error {
	began := s.now()
	err := ci.ContainerAction(ctx, s.Docker, s.Runners, name, action)
	e := Entry{Kind: "runner", Name: name, Action: action, Auto: auto, Reason: reason, OK: err == nil, Seconds: s.now().Sub(began).Seconds()}
	if err != nil {
		e.Error = err.Error()
		s.runnerFailed(name, err)
	} else {
		s.mu.Lock()
		st := s.runner(name)
		st.err, st.errAt, st.idleSince = "", time.Time{}, time.Time{}
		s.mu.Unlock()
	}
	s.record(e)
	return err
}

// StopRunner stops a runner by hand. It is refused while GitHub says the
// runner is busy; when GitHub cannot be read the stop goes ahead, as the
// Runners & CI page already allows.
func (s *Supervisor) StopRunner(ctx context.Context, name string) error {
	cs, err := ci.ListContainers(ctx, s.Docker, s.Runners)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.Name != name {
			continue
		}
		if repo := c.Repo(); repo != "" && s.GitHub != nil {
			if fresh, err := s.GitHub.RunnersNow(ctx, repo); err == nil {
				if r, ok := findRunner(fresh, c); ok && r.Busy {
					return ErrRunnerBusy
				}
			}
		}
		return s.runnerAction(ctx, name, "stop", false, "stopped from Lucidbench")
	}
	return ci.ErrNotRunner
}
