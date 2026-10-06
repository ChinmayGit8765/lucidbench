package power

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// Item is one managed thing in GET /api/power.
type Item struct {
	Kind string `json:"kind"` // cluster | runner | stack
	Name string `json:"name"`
	Mode string `json:"mode"`
	// State is running, starting, stopping, sleeping (stopped, on-demand),
	// stopped (stopped, always or off), partial (a stack with some
	// containers up) or missing.
	State      string   `json:"state"`
	Repo       string   `json:"repo,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	Busy       bool     `json:"busy"`
	Detail     string   `json:"detail,omitempty"`
	Containers []string `json:"containers"`
	// IdleSeconds is how long it has been idle; StopsInSeconds how long until
	// an on-demand stop. Both are absent when busy or unknown.
	IdleSeconds    *int64     `json:"idle_seconds,omitempty"`
	StopsInSeconds *int64     `json:"stops_in_seconds,omitempty"`
	MemoryBytes    int64      `json:"memory_bytes"`
	Error          string     `json:"error,omitempty"`
	ErrorAt        *time.Time `json:"error_at,omitempty"`
}

// Memory is the memory in use by running containers, from docker stats.
type Memory struct {
	ManagedBytes int64 `json:"managed_bytes"`
	DockerBytes  int64 `json:"docker_bytes"`
	Known        bool  `json:"known"`
}

// State is the body of GET /api/power.
type State struct {
	Modes struct {
		Cluster            string `json:"cluster"`
		ClusterIdleMinutes int    `json:"cluster_idle_minutes"`
		Runners            string `json:"runners"`
		RunnerIdleMinutes  int    `json:"runner_idle_minutes"`
	} `json:"modes"`
	PollSeconds int        `json:"poll_seconds"`
	CheckedAt   *time.Time `json:"checked_at,omitempty"`
	Cluster     Item       `json:"cluster"`
	Runners     []Item     `json:"runners"`
	Stacks      []Item     `json:"stacks"`
	Running     int        `json:"running"`
	Sleeping    int        `json:"sleeping"`
	Memory      Memory     `json:"memory"`
	Activity    []Entry    `json:"activity"`
	Errors      []string   `json:"errors"`
}

// ParseMemory reads the used part of docker's MemUsage ("781MiB / 15GiB").
func ParseMemory(s string) int64 {
	used, _, _ := strings.Cut(s, "/")
	used = strings.TrimSpace(used)
	i := strings.IndexFunc(used, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return 0
	}
	n, err := strconv.ParseFloat(used[:i], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{
		"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40,
		"kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
	}[strings.TrimSpace(used[i:])]
	return int64(n * mult)
}

func secs(d time.Duration) *int64 {
	v := int64(d.Seconds())
	if v < 0 {
		v = 0
	}
	return &v
}

func stoppedState(mode string) string {
	if mode == config.PowerOnDemand {
		return "sleeping"
	}
	return "stopped"
}

func errAt(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// StatsTTL is how long one `docker stats` sample is reused; sampling takes
// a couple of seconds.
const StatsTTL = 15 * time.Second

// memory returns the memory in use per running container name. The first
// call samples and waits; later calls answer from the last sample at once
// and take a new one in the background when it is older than StatsTTL.
func (s *Supervisor) memory(ctx context.Context) (map[string]int64, bool) {
	s.mu.Lock()
	have, stale := !s.statsAt.IsZero(), time.Since(s.statsAt) >= StatsTTL
	m := s.stats
	if have && stale && !s.statsBusy {
		s.statsBusy = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			s.sampleMemory(ctx)
		}()
	}
	s.mu.Unlock()
	if have {
		return m, true
	}
	return s.sampleMemory(ctx)
}

func (s *Supervisor) sampleMemory(ctx context.Context) (map[string]int64, bool) {
	stats, err := docker.Stats(ctx, s.Docker)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statsBusy = false
	if err != nil {
		return map[string]int64{}, false
	}
	m := map[string]int64{}
	for _, x := range stats {
		m[x.Name] = ParseMemory(x.MemUsage)
	}
	s.stats, s.statsAt = m, time.Now()
	return m, true
}

// State reads docker once and reports every managed thing.
func (s *Supervisor) State(ctx context.Context) State {
	s.mu.Lock()
	s.begin()
	cl := s.cl
	ticked := s.ticked
	s.mu.Unlock()
	now := s.now()

	st := State{PollSeconds: int(s.poll().Seconds()), Runners: []Item{}, Stacks: []Item{}, Activity: []Entry{}, Errors: []string{}}
	st.Modes.Cluster, st.Modes.Runners = s.Cfg.Cluster, s.Cfg.Runners
	st.Modes.ClusterIdleMinutes, st.Modes.RunnerIdleMinutes = s.Cfg.ClusterIdleMinutes, s.Cfg.RunnerIdleMinutes
	if !ticked.IsZero() {
		st.CheckedAt = &ticked
	}
	// Three independent docker reads, side by side: every container (one
	// ps), the runner containers (ps and inspect, for REPO_URL) and memory.
	var (
		all   []docker.Container
		err   error
		rcs   []ci.Container
		rErr  error
		mem   map[string]int64
		known bool
		wg    sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		var out []byte
		if out, err = s.Docker(ctx, "ps", "-a", "--no-trunc", "--format", "{{json .}}"); err == nil {
			all, err = docker.ParsePS(out)
		}
	}()
	go func() { defer wg.Done(); rcs, rErr = ci.ListContainers(ctx, s.Docker, s.Runners) }()
	go func() { defer wg.Done(); mem, known = s.memory(ctx) }()
	wg.Wait()
	if err != nil {
		st.Errors = append(st.Errors, "docker: "+err.Error())
	}
	st.Memory.Known = known
	for _, b := range mem {
		st.Memory.DockerBytes += b
	}

	// cluster
	c := Item{Kind: "cluster", Name: s.ClusterName, Mode: s.Cfg.Cluster, Containers: []string{}}
	var nodes []docker.Container
	for _, x := range all {
		if x.KindCluster == s.ClusterName {
			nodes = append(nodes, x)
			c.Containers = append(c.Containers, x.Name)
			c.MemoryBytes += mem[x.Name]
		}
	}
	switch {
	case err != nil:
		c.State = "unknown"
	case cl.starting:
		c.State = "starting"
	case cl.stopping:
		c.State = "stopping"
	case len(nodes) == 0:
		c.State = "missing"
	case nodesUp(nodes):
		c.State = "running"
	default:
		c.State = stoppedState(c.Mode)
	}
	if c.State == "running" && cl.busyKnown {
		c.Busy = cl.pods+cl.jobs > 0
		if c.Busy {
			c.Detail = fmt.Sprintf("%d pod(s) running, %d job(s) active", cl.pods, cl.jobs)
		} else {
			idle := now.Sub(cl.active)
			c.IdleSeconds = secs(idle)
			c.Detail = "idle"
			if c.Mode == config.PowerOnDemand {
				c.StopsInSeconds = secs(s.clusterIdle() - idle)
			}
		}
	}
	if cl.err != "" {
		c.Error, c.ErrorAt = cl.err, errAt(cl.errAt)
	}
	st.Cluster = c

	// runners
	if rErr == nil {
		idleFor := time.Duration(s.Cfg.RunnerIdleMinutes) * time.Minute
		s.mu.Lock()
		for _, x := range rcs {
			it := Item{Kind: "runner", Name: x.Name, Mode: s.Cfg.Runners, Repo: x.Repo(), Containers: []string{x.Name}, MemoryBytes: mem[x.Name]}
			if x.Up() {
				it.State = "running"
			} else {
				it.State = stoppedState(it.Mode)
			}
			if r, ok := s.runners[x.Name]; ok {
				it.Busy, it.Labels = r.busy && x.Up(), r.labels
				if r.err != "" {
					it.Error, it.ErrorAt = r.err, errAt(r.errAt)
				}
				if x.Up() && !r.busy && !r.idleSince.IsZero() {
					it.IdleSeconds = secs(now.Sub(r.idleSince))
					if it.Mode == config.PowerOnDemand {
						it.StopsInSeconds = secs(idleFor - now.Sub(r.idleSince))
					}
				}
			}
			if it.Repo == "" && it.Mode == config.PowerOnDemand {
				it.Detail = "REPO_URL names no repository, so it is not started or stopped on demand"
			} else if rs, ok := s.repos[it.Repo]; ok && rs.err != "" {
				it.Detail = "GitHub: " + rs.err
			} else if it.Busy {
				it.Detail = "running a job"
			}
			st.Runners = append(st.Runners, it)
		}
		s.mu.Unlock()
	}

	// stacks
	for _, cfgStack := range s.Cfg.Stacks {
		it := Item{Kind: "stack", Name: cfgStack.Project, Mode: cfgStack.Mode, Containers: []string{}}
		up := 0
		for _, x := range all {
			if x.Project == cfgStack.Project && x.KindCluster == "" {
				it.Containers = append(it.Containers, x.Name)
				it.MemoryBytes += mem[x.Name]
				if x.State == "running" {
					up++
				}
			}
		}
		switch {
		case len(it.Containers) == 0:
			it.State = "missing"
		case up == len(it.Containers):
			it.State = "running"
		case up > 0:
			it.State = "partial"
			it.Detail = fmt.Sprintf("%d of %d containers running", up, len(it.Containers))
		default:
			it.State = stoppedState(it.Mode)
		}
		st.Stacks = append(st.Stacks, it)
	}

	for _, it := range append(append([]Item{st.Cluster}, st.Runners...), st.Stacks...) {
		switch it.State {
		case "running", "starting", "stopping", "partial":
			st.Running++
			st.Memory.ManagedBytes += it.MemoryBytes
		case "sleeping", "stopped":
			st.Sleeping++
		}
	}
	if s.Log != nil {
		if es, err := s.Log.Recent(30); err == nil {
			st.Activity = es
		}
	}
	return st
}
