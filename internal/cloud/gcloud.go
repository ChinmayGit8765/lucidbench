package cloud

import (
	"context"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

type gcloudConfig struct {
	Core struct {
		Account string `json:"account"`
		Project string `json:"project"`
	} `json:"core"`
}

type gcloudProject struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	LifecycleState string `json:"lifecycleState"`
}

type gcloudCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	LastTransitionTime string `json:"lastTransitionTime"`
	Message            string `json:"message"`
}

type gcloudService struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		URL                       string            `json:"url"`
		LatestReadyRevisionName   string            `json:"latestReadyRevisionName"`
		LatestCreatedRevisionName string            `json:"latestCreatedRevisionName"`
		Conditions                []gcloudCondition `json:"conditions"`
	} `json:"status"`
}

func gcloudConsole(path string, project string) string {
	u := "https://console.cloud.google.com/" + path
	if project != "" {
		u += "?project=" + url.QueryEscape(project)
	}
	return u
}

// cloudRunStatus reads the Ready condition: True is ready, False failed,
// anything else (Unknown, or no condition yet) is a rollout in progress.
func cloudRunStatus(conds []gcloudCondition) (status, updated string) {
	for _, c := range conds {
		if c.Type != "Ready" {
			continue
		}
		switch c.Status {
		case "True":
			return StatusReady, c.LastTransitionTime
		case "False":
			return StatusFailed, c.LastTransitionTime
		}
		return StatusBuilding, c.LastTransitionTime
	}
	return StatusUnknown, ""
}

func gcloudServices(ctx context.Context, c *caller, project string) ([]Resource, error) {
	var raw []gcloudService
	if err := c.json(ctx, &raw, "run", "services", "list", "--project", project, "--quiet", "--format=json"); err != nil {
		return nil, err
	}
	out := make([]Resource, 0, len(raw))
	for _, s := range raw {
		region := s.Metadata.Labels["cloud.googleapis.com/location"]
		status, updated := cloudRunStatus(s.Status.Conditions)
		if updated == "" {
			updated = s.Metadata.CreationTimestamp
		}
		rev := s.Status.LatestReadyRevisionName
		if rev == "" {
			rev = s.Status.LatestCreatedRevisionName
		}
		r := Resource{
			Kind: KindService, Name: s.Metadata.Name, Region: region, Project: project,
			Status: status, URL: s.Status.URL, Updated: updated,
			Console: gcloudConsole("run/detail/"+url.PathEscape(region)+"/"+url.PathEscape(s.Metadata.Name)+"/metrics", project),
		}
		if rev != "" {
			r.Detail = "revision " + rev
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// fetchGcloud reads the active account and project, the projects the account
// can see, and Cloud Run services across regions for the active project and
// for any other project a deploy entry names.
func fetchGcloud(ctx context.Context, c *caller, targets []projects.Deploy) Summary {
	var sum Summary
	var cfg gcloudConfig
	if err := c.json(ctx, &cfg, "config", "list", "--format=json"); err != nil {
		return failed(sum, err)
	}
	if cfg.Core.Account == "" {
		sum.State, sum.Message = StateNotSignedIn, "not signed in"
		return sum
	}
	sum.State, sum.Account, sum.Scope = StateConnected, cfg.Core.Account, cfg.Core.Project

	var list []gcloudProject
	pj := Section{ID: "projects", Title: "Projects", Resources: []Resource{}}
	if err := c.json(ctx, &list, "projects", "list", "--limit=100", "--format=json"); err != nil {
		if ce, ok := err.(*cliError); ok && ce.SignedOut {
			return failed(Summary{}, err)
		}
		pj = sectionError(pj, err)
	} else {
		for _, p := range list {
			pj.Resources = append(pj.Resources, Resource{
				Kind: KindGCPProject, Name: firstOf(p.Name, p.ProjectID), Project: p.ProjectID,
				Detail: strings.ToLower(p.LifecycleState), Console: gcloudConsole("home/dashboard", p.ProjectID),
			})
		}
	}

	// Services of the active project, then of any other project a deploy
	// entry asks for.
	var secs []Section
	wanted := []string{}
	if cfg.Core.Project != "" {
		wanted = append(wanted, cfg.Core.Project)
	}
	for _, t := range targets {
		if t.Project != "" && !slices.Contains(wanted, t.Project) {
			wanted = append(wanted, t.Project)
		}
	}
	if len(wanted) == 0 {
		secs = append(secs, Section{ID: "run", Title: "Cloud Run", Resources: []Resource{}, Note: "No active project. Run gcloud config set project <id>."})
	}
	for _, p := range wanted {
		sec := Section{ID: "run:" + p, Title: "Cloud Run · " + p, Resources: []Resource{}}
		rs, err := gcloudServices(ctx, c, p)
		if err != nil {
			sec = sectionError(sec, err)
		} else {
			sec.Resources = rs
			if len(rs) == 0 {
				sec.Note = "No Cloud Run services in this project."
			}
		}
		secs = append(secs, sec)
	}
	sum.Sections = append(secs, pj)
	return sum
}

func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
