package power

import (
	"context"
	"errors"
	"fmt"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// ErrUnknownStack is returned for a compose project not in power.stacks.
var ErrUnknownStack = errors.New("this compose project is not listed in power.stacks")

func (s *Supervisor) stackMode(project string) (string, bool) {
	for _, st := range s.Cfg.Stacks {
		if st.Project == project {
			return st.Mode, true
		}
	}
	return "", false
}

// StackPolicy allows start and stop on the containers of listed stacks.
func (s *Supervisor) StackPolicy() docker.Policy {
	ps := []string{}
	for _, st := range s.Cfg.Stacks {
		ps = append(ps, st.Project)
	}
	return docker.Policy{Projects: ps}
}

// StackAction starts or stops every container of a listed compose project.
func (s *Supervisor) StackAction(ctx context.Context, project, action string, auto bool, reason string) ([]string, error) {
	if _, ok := s.stackMode(project); !ok {
		return nil, ErrUnknownStack
	}
	began := s.now()
	done, err := docker.ActProject(ctx, s.Docker, s.StackPolicy(), project, action)
	e := Entry{Kind: "stack", Name: project, Action: action, Auto: auto, Reason: reason, OK: err == nil, Seconds: s.now().Sub(began).Seconds()}
	if err != nil {
		e.Error = err.Error()
	}
	if err != nil || len(done) > 0 {
		s.record(e)
	}
	return done, err
}

// Outcome is what "Sleep everything idle" did with one thing.
type Outcome struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Stopped bool   `json:"stopped"`
	Reason  string `json:"reason"`
}

// SleepIdle stops every on-demand thing that is idle right now, without
// waiting for its idle timeout: the cluster when no pod runs and no job is
// active, runners that GitHub reports not busy with no queued or running
// run, and running on-demand stacks. Things in always or off mode are left
// alone, and so is anything whose idleness cannot be checked.
func (s *Supervisor) SleepIdle(ctx context.Context) []Outcome {
	const why = "sleep everything idle"
	out := []Outcome{}
	if s.Cfg.Cluster == config.PowerOnDemand {
		if ns, err := s.nodes(ctx); err == nil && len(ns) > 0 && nodesUp(ns) {
			o := Outcome{Kind: "cluster", Name: s.ClusterName}
			switch err := s.StopCluster(ctx, false, why); {
			case err == nil:
				o.Stopped, o.Reason = true, "stopped"
			case errors.Is(err, ErrClusterBusy):
				o.Reason = "busy: pods running or jobs active"
			default:
				o.Reason = err.Error()
			}
			out = append(out, o)
		}
	}
	if s.Cfg.Runners == config.PowerOnDemand && s.GitHub != nil {
		if cs, err := ci.ListContainers(ctx, s.Docker, s.Runners); err == nil {
			for _, c := range cs {
				if !c.Up() {
					continue
				}
				o := Outcome{Kind: "runner", Name: c.Name}
				repo := c.Repo()
				q, p, ok := []ci.Run(nil), []ci.Run(nil), false
				if repo != "" {
					q, p, ok = s.activeRuns(ctx, repo)
				}
				switch {
				case repo == "":
					o.Reason = "no repository in REPO_URL"
				case !ok:
					o.Reason = "GitHub could not be read"
				case len(q)+len(p) > 0:
					o.Reason = fmt.Sprintf("%s has %d queued or running run(s)", repo, len(q)+len(p))
				default:
					switch err := s.stopIdleRunner(ctx, repo, c, false, why); {
					case err == nil:
						o.Stopped, o.Reason = true, "stopped"
					case errors.Is(err, ErrRunnerBusy):
						o.Reason = "busy: running a job"
					default:
						o.Reason = err.Error()
					}
				}
				out = append(out, o)
			}
		}
	}
	for _, st := range s.Cfg.Stacks {
		if st.Mode != config.PowerOnDemand {
			continue
		}
		done, err := s.StackAction(ctx, st.Project, "stop", false, why)
		switch {
		case errors.Is(err, docker.ErrNotFound):
		case err != nil:
			out = append(out, Outcome{Kind: "stack", Name: st.Project, Reason: err.Error()})
		case len(done) > 0:
			out = append(out, Outcome{Kind: "stack", Name: st.Project, Stopped: true, Reason: fmt.Sprintf("stopped %d container(s)", len(done))})
		}
	}
	return out
}
