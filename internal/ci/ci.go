// Package ci shows self-hosted GitHub Actions runners: the local runner
// containers (through the docker CLI) and, for each configured repository,
// its registered runners and recent workflow runs (through the GitHub REST
// API). It can start, stop and restart runner containers and re-run failed
// jobs.
//
// The GitHub token is resolved per request and only ever placed in the
// Authorization header; nothing in this package logs or returns it.
package ci

import (
	"context"
	"errors"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Service answers the Runners & CI questions for one configuration.
type Service struct {
	Repos  []string
	Filter Filter
	GitHub *GitHub
	Docker DockerFunc
	Now    func() time.Time
}

// New builds a Service from the user config, using the real docker CLI, the
// GitHub API and the `gh auth token` fallback.
func New(c config.CIConfig) *Service {
	return &Service{
		Repos:  slices.Clone(c.GitHub.Repos),
		Filter: Filter{ComposeProject: c.Runners.ComposeProject, ImageMatch: c.Runners.ImageMatch},
		GitHub: &GitHub{Tokens: &TokenSource{Ref: c.GitHub.Token, Getenv: os.Getenv, GH: GHAuthToken}},
		Docker: ExecDocker,
		Now:    time.Now,
	}
}

// SourceError is a failure reading one source ("docker" or a repository).
// One failing source never blanks the others.
type SourceError struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}

// snapshot is everything read for one request.
type snapshot struct {
	containers []Container
	runners    []Runner
	runs       []Run
	errors     []SourceError
	token      string // token source, never the value
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) read(ctx context.Context, wantRunners, wantRuns, wantContainers bool) snapshot {
	var (
		snap snapshot
		mu   sync.Mutex
		wg   sync.WaitGroup
	)
	snap.containers, snap.runners, snap.runs, snap.errors = []Container{}, []Runner{}, []Run{}, []SourceError{}
	fail := func(src string, err error) {
		mu.Lock()
		snap.errors = append(snap.errors, SourceError{Source: src, Message: err.Error()})
		mu.Unlock()
	}
	if wantContainers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cs, err := ListContainers(ctx, s.Docker, s.Filter)
			if err != nil {
				fail("docker", err)
				return
			}
			mu.Lock()
			snap.containers = cs
			mu.Unlock()
		}()
	}
	snap.token = s.GitHub.Tokens.Source(ctx)
	if snap.token == "none" && len(s.Repos) > 0 && (wantRunners || wantRuns) {
		fail("github", ErrNoToken)
	} else if snap.token != "none" {
		for _, repo := range s.Repos {
			if wantRunners {
				wg.Add(1)
				go func() {
					defer wg.Done()
					rs, err := s.GitHub.Runners(ctx, repo)
					if err != nil {
						fail(repo, err)
						return
					}
					mu.Lock()
					snap.runners = append(snap.runners, rs...)
					mu.Unlock()
				}()
			}
			if wantRuns {
				wg.Add(1)
				go func() {
					defer wg.Done()
					rs, err := s.GitHub.Runs(ctx, repo)
					if err != nil {
						fail(repo, err)
						return
					}
					mu.Lock()
					snap.runs = append(snap.runs, rs...)
					mu.Unlock()
				}()
			}
		}
	}
	wg.Wait()
	linkContainers(snap.runners, snap.containers)
	sort.SliceStable(snap.runners, func(i, j int) bool {
		if snap.runners[i].Repo != snap.runners[j].Repo {
			return repoIndex(s.Repos, snap.runners[i].Repo) < repoIndex(s.Repos, snap.runners[j].Repo)
		}
		return snap.runners[i].Name < snap.runners[j].Name
	})
	sort.SliceStable(snap.runs, func(i, j int) bool { return snap.runs[i].CreatedAt > snap.runs[j].CreatedAt })
	sort.SliceStable(snap.errors, func(i, j int) bool { return snap.errors[i].Source < snap.errors[j].Source })
	return snap
}

func repoIndex(repos []string, r string) int {
	if i := slices.Index(repos, r); i >= 0 {
		return i
	}
	return len(repos)
}

// linkContainers records on each runner the local container that runs it:
// same runner name, and a REPO_URL that ends in the runner's repository.
func linkContainers(rs []Runner, cs []Container) {
	for i := range rs {
		for _, c := range cs {
			if c.RunnerName == "" || !strings.EqualFold(c.RunnerName, rs[i].Name) {
				continue
			}
			u := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(c.RepoURL, "/"), ".git"))
			if u == "" || strings.HasSuffix(u, "/"+strings.ToLower(rs[i].Repo)) {
				rs[i].Container = c.Name
				break
			}
		}
	}
}

// RunnerCounts counts registered runners. Online includes busy runners.
type RunnerCounts struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Busy    int `json:"busy"`
	Offline int `json:"offline"`
}

// ContainerCounts counts local runner containers.
type ContainerCounts struct {
	Total int `json:"total"`
	Up    int `json:"up"`
	Down  int `json:"down"`
}

// RunCounts counts the workflow runs created in the last 24 hours.
type RunCounts struct {
	Total        int            `json:"total"`
	InProgress   int            `json:"in_progress"`
	ByConclusion map[string]int `json:"by_conclusion"`
	// PassRate is success / (success + failed) over completed runs, where
	// failed is failure, timed_out or startup_failure. Cancelled, skipped and
	// neutral runs do not count. Nil when no run qualifies.
	PassRate *float64 `json:"pass_rate"`
}

// Summary is the body of GET /api/ci/summary.
type Summary struct {
	Configured   bool            `json:"configured"`
	Repos        []string        `json:"repos"`
	TokenSource  string          `json:"token_source"`
	Runners      RunnerCounts    `json:"runners"`
	Containers   ContainerCounts `json:"containers"`
	Runs24h      RunCounts       `json:"runs_24h"`
	FailingRepos []string        `json:"failing_repos"`
	Errors       []SourceError   `json:"errors"`
	GeneratedAt  string          `json:"generated_at"`
}

// failed reports whether a conclusion counts as a failure.
func failed(conclusion string) bool {
	switch conclusion {
	case "failure", "timed_out", "startup_failure":
		return true
	}
	return false
}

// summarize does the summary maths over already-read data.
func summarize(now time.Time, repos []string, runners []Runner, cs []Container, runs []Run) Summary {
	sum := Summary{
		Configured:   len(repos) > 0,
		Repos:        append([]string{}, repos...),
		FailingRepos: []string{},
		Runs24h:      RunCounts{ByConclusion: map[string]int{}},
		GeneratedAt:  now.UTC().Format(time.RFC3339),
	}
	for _, r := range runners {
		sum.Runners.Total++
		if r.Status == "online" {
			sum.Runners.Online++
			if r.Busy {
				sum.Runners.Busy++
			}
		} else {
			sum.Runners.Offline++
		}
	}
	for _, c := range cs {
		sum.Containers.Total++
		if c.Up() {
			sum.Containers.Up++
		} else {
			sum.Containers.Down++
		}
	}
	pass, fail := 0, 0
	cutoff := now.Add(-24 * time.Hour)
	for _, r := range runs {
		t, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil || t.Before(cutoff) {
			continue
		}
		sum.Runs24h.Total++
		if r.Status != "completed" {
			sum.Runs24h.InProgress++
			continue
		}
		c := r.Conclusion
		if c == "" {
			c = "unknown"
		}
		sum.Runs24h.ByConclusion[c]++
		switch {
		case c == "success":
			pass++
		case failed(c):
			fail++
		}
	}
	if pass+fail > 0 {
		rate := float64(pass) / float64(pass+fail)
		sum.Runs24h.PassRate = &rate
	}
	// A repository is failing when its most recent completed run failed.
	latest := map[string]Run{}
	for _, r := range runs {
		if r.Status != "completed" {
			continue
		}
		if cur, ok := latest[r.Repo]; !ok || r.CreatedAt > cur.CreatedAt {
			latest[r.Repo] = r
		}
	}
	for _, repo := range repos {
		if r, ok := latest[repo]; ok && failed(r.Conclusion) {
			sum.FailingRepos = append(sum.FailingRepos, repo)
		}
	}
	return sum
}

// Summary reads every source and returns the counts.
func (s *Service) Summary(ctx context.Context) Summary {
	snap := s.read(ctx, true, true, true)
	sum := summarize(s.now(), s.Repos, snap.runners, snap.containers, snap.runs)
	sum.TokenSource = snap.token
	sum.Errors = snap.errors
	return sum
}

// RunnersResponse is the body of GET /api/ci/runners.
type RunnersResponse struct {
	Repos       []string      `json:"repos"`
	TokenSource string        `json:"token_source"`
	Runners     []Runner      `json:"runners"`
	Containers  []Container   `json:"containers"`
	Errors      []SourceError `json:"errors"`
}

// Runners lists registered runners and local runner containers.
func (s *Service) Runners(ctx context.Context) RunnersResponse {
	snap := s.read(ctx, true, false, true)
	return RunnersResponse{
		Repos: append([]string{}, s.Repos...), TokenSource: snap.token,
		Runners: snap.runners, Containers: snap.containers, Errors: snap.errors,
	}
}

// RunsResponse is the body of GET /api/ci/runs.
type RunsResponse struct {
	Repos       []string      `json:"repos"`
	TokenSource string        `json:"token_source"`
	Runs        []Run         `json:"runs"`
	Errors      []SourceError `json:"errors"`
}

// ErrUnknownRepo is returned for a repository not in ci.github.repos.
var ErrUnknownRepo = errors.New("repository is not in ci.github.repos")

// Runs lists recent workflow runs, newest first, across all configured
// repositories or only repo when it is not empty.
func (s *Service) Runs(ctx context.Context, repo string) (RunsResponse, error) {
	sub := s
	if repo != "" {
		if !slices.Contains(s.Repos, repo) {
			return RunsResponse{}, ErrUnknownRepo
		}
		cp := *s
		cp.Repos = []string{repo}
		sub = &cp
	}
	snap := sub.read(ctx, false, true, false)
	return RunsResponse{
		Repos: append([]string{}, s.Repos...), TokenSource: snap.token, Runs: snap.runs, Errors: snap.errors,
	}, nil
}

// ContainerAction starts, stops or restarts a runner container.
func (s *Service) ContainerAction(ctx context.Context, name, action string) error {
	return ContainerAction(ctx, s.Docker, s.Filter, name, action)
}

// Rerun re-runs the failed jobs of a run in a configured repository.
func (s *Service) Rerun(ctx context.Context, repo string, id int64) error {
	if !slices.Contains(s.Repos, repo) {
		return ErrUnknownRepo
	}
	return s.GitHub.RerunFailed(ctx, repo, id)
}
