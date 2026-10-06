package cloud

import (
	"context"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// vercelWhoami is the part of `vercel whoami --format json` that is read;
// its email is never decoded.
type vercelWhoami struct {
	Username string `json:"username"`
	Team     *struct {
		Slug string `json:"slug"`
	} `json:"team"`
}

type vercelProjects struct {
	Projects []struct {
		Name                string `json:"name"`
		LatestProductionURL string `json:"latestProductionUrl"`
		UpdatedAt           int64  `json:"updatedAt"`
	} `json:"projects"`
}

type vercelDeployments struct {
	Deployments []struct {
		URL       string `json:"url"`
		Name      string `json:"name"`
		State     string `json:"state"`
		Target    string `json:"target"`
		CreatedAt int64  `json:"createdAt"`
		Meta      struct {
			Ref string `json:"githubCommitRef"`
		} `json:"meta"`
	} `json:"deployments"`
}

// vercelStatus maps a deployment state. Blocked deploys never ran, which
// is a failure from the owner's side.
func vercelStatus(state string) string {
	switch strings.ToUpper(state) {
	case "READY":
		return StatusReady
	case "ERROR", "BLOCKED":
		return StatusFailed
	case "BUILDING", "QUEUED", "INITIALIZING", "DEPLOYING":
		return StatusBuilding
	}
	return StatusUnknown
}

func httpsURL(host string) string {
	if host == "" || strings.HasPrefix(host, "http") {
		return host
	}
	return "https://" + host
}

func msTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// fetchVercel reads who is signed in, the projects and the recent
// deployments across them. A project's status is its newest production
// deployment, or its newest deployment when it has no production one.
func fetchVercel(ctx context.Context, c *caller, _ []projects.Deploy) Summary {
	var sum Summary
	var who vercelWhoami
	if err := c.json(ctx, &who, "whoami", "--format", "json"); err != nil {
		return failed(sum, err)
	}
	if who.Username == "" {
		sum.State, sum.Message = StateNotSignedIn, "not signed in"
		return sum
	}
	sum.State, sum.Account = StateConnected, who.Username
	team := ""
	if who.Team != nil {
		team = who.Team.Slug
		sum.Scope = "team " + team
	}
	console := func(project string) string {
		if team == "" {
			return "https://vercel.com/dashboard"
		}
		u := "https://vercel.com/" + url.PathEscape(team)
		if project != "" {
			u += "/" + url.PathEscape(project)
		}
		return u
	}

	var deps vercelDeployments
	depsErr := c.json(ctx, &deps, "ls", "--all", "--limit", "50", "--format", "json")
	d := deps.Deployments
	sort.SliceStable(d, func(i, j int) bool { return d[i].CreatedAt > d[j].CreatedAt })

	pj := Section{ID: "projects", Title: "Projects", Resources: []Resource{}}
	var pl vercelProjects
	if err := c.json(ctx, &pl, "project", "ls", "--format", "json"); err != nil {
		pj = sectionError(pj, err)
	} else {
		for _, p := range pl.Projects {
			r := Resource{Kind: KindProject, Name: p.Name, URL: httpsURL(p.LatestProductionURL), Console: console(p.Name), Updated: msTime(p.UpdatedAt)}
			if depsErr == nil {
				best := -1
				for i, x := range d {
					if x.Name != p.Name {
						continue
					}
					if x.Target == "production" {
						best = i
						break
					}
					if best < 0 {
						best = i
					}
				}
				if best >= 0 {
					x := d[best]
					r.Status, r.Updated = vercelStatus(x.State), msTime(x.CreatedAt)
					r.Detail = strings.Join(nonEmpty(strings.ToLower(x.State), x.Target, x.Meta.Ref), " · ")
				}
			}
			pj.Resources = append(pj.Resources, r)
		}
		if len(pj.Resources) == 0 {
			pj.Note = "No projects in this scope."
		}
	}

	dp := Section{ID: "deployments", Title: "Recent deployments", Resources: []Resource{}}
	if depsErr != nil {
		dp = sectionError(dp, depsErr)
	}
	for i, x := range d {
		if i == 20 {
			break
		}
		dp.Resources = append(dp.Resources, Resource{
			Kind: KindDeployment, Name: x.Name, Status: vercelStatus(x.State), Updated: msTime(x.CreatedAt),
			Detail: strings.Join(nonEmpty(strings.ToLower(x.State), x.Target, x.Meta.Ref), " · "),
			URL:    httpsURL(x.URL), Console: console(x.Name),
		})
	}
	sum.Sections = []Section{pj, dp}
	return sum
}
