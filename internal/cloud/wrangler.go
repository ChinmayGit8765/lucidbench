package cloud

import (
	"context"
	"net/url"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// wranglerWhoami is the part of `wrangler whoami --json` that is read. Its
// email and token permissions are never decoded.
type wranglerWhoami struct {
	LoggedIn bool `json:"loggedIn"`
	Accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"accounts"`
}

type wranglerPages struct {
	Name     string `json:"Project Name"`
	Domains  string `json:"Project Domains"`
	Modified string `json:"Last Modified"`
}

type wranglerPagesDeploy struct {
	Environment string `json:"Environment"`
	Branch      string `json:"Branch"`
	Source      string `json:"Source"`
	Deployment  string `json:"Deployment"`
	Status      string `json:"Status"`
	Build       string `json:"Build"`
}

// pagesStatus reads the Status column of `pages deployment list`, which is
// "6 days ago" for a deployment that succeeded and the stage otherwise.
func pagesStatus(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "fail") || strings.Contains(l, "error") || strings.Contains(l, "cancel"):
		return StatusFailed
	case strings.Contains(l, "progress") || strings.Contains(l, "build") || strings.Contains(l, "queue") || strings.Contains(l, "initial") || strings.Contains(l, "deploying"):
		return StatusBuilding
	case strings.Contains(l, "ago") || strings.Contains(l, "just now") || strings.Contains(l, "success") || strings.Contains(l, "active"):
		return StatusReady
	}
	return StatusUnknown
}

func dashURL(account, path string) string {
	if account == "" {
		return "https://dash.cloudflare.com/"
	}
	return "https://dash.cloudflare.com/" + url.PathEscape(account) + "/" + path
}

// fetchWrangler reads the signed-in account, the Pages projects and, because
// wrangler cannot list Workers at all, the deployments of the Workers that
// projects.yaml names. For a Pages project named there it also reads the
// latest deployment, the only place Cloudflare reports a failure.
func fetchWrangler(ctx context.Context, c *caller, targets []projects.Deploy) Summary {
	var sum Summary
	var who wranglerWhoami
	if err := c.json(ctx, &who, "whoami", "--json"); err != nil {
		return failed(sum, err)
	}
	if !who.LoggedIn || len(who.Accounts) == 0 {
		sum.State, sum.Message = StateNotSignedIn, "not signed in"
		return sum
	}
	acct := who.Accounts[0]
	// Cloudflare names a personal account after its email.
	sum.State, sum.Account = StateConnected, emailRE.ReplaceAllString(acct.Name, "<email>")
	if len(who.Accounts) > 1 {
		sum.Scope = "account 1 of " + itoa(len(who.Accounts))
	}

	wanted := map[string]bool{}
	for _, t := range targets {
		wanted[t.Service] = true
	}

	pages := Section{ID: "pages", Title: "Pages", Resources: []Resource{}}
	pageNames := map[string]bool{}
	var list []wranglerPages
	if err := c.json(ctx, &list, "pages", "project", "list", "--json"); err != nil {
		pages = sectionError(pages, err)
	} else {
		for _, p := range list {
			pageNames[p.Name] = true
			r := Resource{
				Kind: KindPages, Name: p.Name, Status: StatusUnknown, UpdatedText: p.Modified,
				Console: dashURL(acct.ID, "pages/view/"+url.PathEscape(p.Name)),
			}
			if d := strings.TrimSpace(strings.Split(p.Domains, ",")[0]); d != "" {
				r.URL = "https://" + d
			}
			if wanted[p.Name] {
				var deps []wranglerPagesDeploy
				if err := c.json(ctx, &deps, "pages", "deployment", "list", "--project-name", p.Name, "--json"); err != nil {
					r.Detail = "latest deployment unavailable: " + errText(err)
				} else if len(deps) > 0 {
					d := deps[0]
					r.Status, r.UpdatedText = pagesStatus(d.Status), d.Status
					r.Detail = strings.Join(nonEmpty(strings.ToLower(d.Environment), d.Branch, d.Source), " · ")
					if d.Build != "" {
						r.Console = d.Build
					}
				}
			} else {
				r.Detail = "status needs a deploy entry in projects.yaml"
			}
			pages.Resources = append(pages.Resources, r)
		}
		if len(pages.Resources) == 0 {
			pages.Note = "No Pages projects on this account."
		}
	}

	workers := Section{ID: "workers", Title: "Workers", Resources: []Resource{},
		Note: "Wrangler cannot list Workers, so only those named in a projects.yaml deploy entry are shown."}
	seen := map[string]bool{}
	for _, t := range targets {
		if pageNames[t.Service] || seen[t.Service] {
			continue
		}
		seen[t.Service] = true
		r := Resource{Kind: KindWorker, Name: t.Service, Status: StatusUnknown,
			Console: dashURL(acct.ID, "workers/services/view/"+url.PathEscape(t.Service)+"/production")}
		var raw any
		if err := c.json(ctx, &raw, "deployments", "list", "--name", t.Service, "--json"); err != nil {
			r.Detail = errText(err)
			if strings.Contains(r.Detail, "does not exist") {
				r.Detail = "not found on this account"
			}
		} else if latest, n := latestDeployment(raw); n > 0 {
			r.Status, r.Updated = StatusReady, latest
			r.Detail = itoa(n) + " recent " + plural(n, "deployment")
		} else {
			r.Detail = "no deployments"
		}
		workers.Resources = append(workers.Resources, r)
	}
	sum.Sections = []Section{pages, workers}
	return sum
}

// latestDeployment finds the newest created_on among the deployments in
// whatever shape `wrangler deployments list --json` prints (a list, or an
// object holding one).
func latestDeployment(raw any) (latest string, n int) {
	var items []any
	switch v := raw.(type) {
	case []any:
		items = v
	case map[string]any:
		for _, k := range []string{"deployments", "result", "items"} {
			if l, ok := v[k].([]any); ok {
				items = l
				break
			}
		}
	}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		n++
		if t, _ := m["created_on"].(string); t > latest {
			latest = t
		}
	}
	return latest, n
}

func nonEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
