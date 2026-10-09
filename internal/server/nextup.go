package server

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/linear"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/nextup"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/team"
	"github.com/ChinmayGit8765/lucidbench/internal/trello"
	"github.com/ChinmayGit8765/lucidbench/internal/usage"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// nextupDeps are the services Next up reads from.
type nextupDeps struct {
	cfg     *config.Config
	data    string
	vault   memory.Opener
	council *council.Service
	work    *work.Service
	ci      *ci.Service
	linear  *linear.Service
	trello  *trello.Service
	usage   *usage.Service
	teams   *team.Store
}

// newNextUp builds the Next up service over the daemon's other services.
func newNextUp(d nextupDeps) *nextup.Service {
	src := nextup.Sources{
		Projects: projects.Load,
		Boards: func() ([]boards.Board, error) {
			v, err := d.vault()
			if err != nil {
				return nil, err
			}
			list, err := boards.List(v)
			if err != nil {
				return nil, err
			}
			out := make([]boards.Board, 0, len(list))
			for _, b := range list {
				if full, err := boards.Get(v, b.ID); err == nil {
					out = append(out, *full)
				}
			}
			return out, nil
		},
		Briefs: func() ([]nextup.Brief, error) { return councilBriefs(d.council) },
		Work:   d.work.List,
		CI: func(ctx context.Context) ([]nextup.FailingRun, error) {
			if len(d.cfg.CI.GitHub.Repos) == 0 {
				return nil, nil
			}
			res, err := d.ci.Runs(ctx, "")
			if err != nil {
				return nil, err
			}
			return failingOnDefault(res.Runs), nil
		},
		Usage: func() *usage.Summary { return d.usage.Summary(1) },
		Builder: func(project string) (*work.Builder, error) {
			return d.teams.WorkBuilder(d.data)(project)
		},
		PageConfidential: func(path string) bool {
			v, err := d.vault()
			if err != nil {
				return true
			}
			c, err := v.IsConfidential(path)
			return err != nil || c
		},
		LookPath: exec.LookPath,
	}
	if d.linear != nil && d.linear.Status().Configured {
		src.Linear = func(ctx context.Context) ([]linear.Issue, error) {
			l, err := d.linear.Issues(ctx, linear.Filter{Me: true})
			if err != nil {
				return nil, err
			}
			return l.Issues, nil
		}
	}
	if d.trello != nil && d.trello.Status().Configured {
		src.Trello = d.trello.MyCards
	}
	return &nextup.Service{
		Sources: src,
		Store:   &nextup.Store{Dir: filepath.Join(d.data, "nextup")},
		Ranker: &nextup.Ranker{
			Runner: &agentexec.Runner{InContainer: cluster.InContainer, LookPath: exec.LookPath,
				ProfileDir: func(provider, profile string) (string, error) { return hostProfileDir(d.cfg, provider, profile) }},
			RunsDir: filepath.Join(d.data, "nextup", "runs"),
			Scout: func() (string, string, string) {
				t, _, _ := d.teams.DefaultTeam()
				if s := t.Roles.Scout; s != nil {
					return s.Provider, s.Model, s.Profile
				}
				return "", "", ""
			},
		},
	}
}

// councilBriefs reads the council sessions that may need the user: drafts
// (with their open blockers), approvals and failures.
func councilBriefs(c *council.Service) ([]nextup.Brief, error) {
	list, err := c.List()
	if err != nil {
		return nil, err
	}
	out := []nextup.Brief{}
	for _, s := range list {
		b := nextup.Brief{ID: s.ID, Title: s.Title, Input: s.Input, Project: s.Project, Status: s.Status, BriefPath: s.BriefPath, Card: s.Card, Updated: s.Updated}
		switch s.Status {
		case council.StatusDraft, council.StatusFailed:
			if full, err := c.Get(s.ID); err == nil {
				b.Input = full.Input
				b.Blockers = len(council.LatestBlockers(full))
			}
		case council.StatusApproved:
		default:
			continue
		}
		out = append(out, b)
	}
	return out, nil
}

// failingOnDefault is, per repository, the latest completed run on main or
// master when it failed.
func failingOnDefault(runs []ci.Run) []nextup.FailingRun {
	latest := map[string]ci.Run{}
	var order []string
	for _, r := range runs {
		if r.Status != "completed" || (r.Branch != "main" && r.Branch != "master") {
			continue
		}
		cur, ok := latest[r.Repo]
		if !ok {
			order = append(order, r.Repo)
		}
		if !ok || r.CreatedAt > cur.CreatedAt {
			latest[r.Repo] = r
		}
	}
	out := []nextup.FailingRun{}
	for _, repo := range order {
		r := latest[repo]
		switch r.Conclusion {
		case "failure", "timed_out", "startup_failure":
		default:
			continue
		}
		at, _ := time.Parse(time.RFC3339, r.CreatedAt)
		out = append(out, nextup.FailingRun{Repo: r.Repo, Workflow: r.Name, Title: r.Title, Branch: r.Branch, Conclusion: r.Conclusion, RunNumber: r.RunNumber, URL: r.HTMLURL, At: at})
	}
	return out
}
