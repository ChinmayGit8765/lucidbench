package picture

// "From live state": a Mermaid flowchart of what Lucidbench already knows
// about a project. No AI and no network: the project's deploy entries, its
// compose project's containers, the databases tied to it, its CI runner
// containers and its builds_into links. The same inputs give the same text.

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/databases"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Inputs is everything Generate draws. Names maps a project id to its name,
// for the builds_into and built_by links. The other lists are the machine's
// whole inventory: Generate picks what belongs to the project.
type Inputs struct {
	Project    projects.Project
	Names      map[string]string
	Containers []docker.Container
	Databases  []databases.Discovered
	Runners    []ci.Container
}

// composeNames are the compose project names that belong to p: its id and
// the name of its folder, the default name docker compose gives a project.
func composeNames(p projects.Project) []string {
	names := []string{strings.ToLower(p.ID)}
	if p.LocalPath != "" {
		if b := strings.ToLower(filepath.Base(filepath.Clean(p.LocalPath))); b != "" && b != "." && b != string(filepath.Separator) && b != names[0] {
			names = append(names, b)
		}
	}
	return names
}

func inCompose(p projects.Project, project string) bool {
	if project == "" {
		return false
	}
	for _, n := range composeNames(p) {
		if strings.EqualFold(n, project) {
			return true
		}
	}
	return false
}

// Select narrows the machine's inventory to what belongs to the project:
//   - containers of a compose project named like the project id or its folder;
//   - databases in such a compose project, or published on a port that one of
//     those containers also publishes;
//   - runner containers whose repository is the project's repo.
func Select(p projects.Project, cs []docker.Container, dbs []databases.Discovered, runners []ci.Container) Inputs {
	in := Inputs{Project: p}
	for _, c := range cs {
		if inCompose(p, c.Project) {
			in.Containers = append(in.Containers, c)
		}
	}
	for _, d := range dbs {
		linked := inCompose(p, d.Compose)
		if !linked && d.Port != 0 {
			needle := fmt.Sprintf(":%d->", d.Port)
			for _, c := range in.Containers {
				if strings.Contains(c.Ports, needle) {
					linked = true
					break
				}
			}
		}
		if linked {
			in.Databases = append(in.Databases, d)
		}
	}
	if p.Repo != "" {
		for _, r := range runners {
			if strings.EqualFold(r.Repo(), p.Repo) {
				in.Runners = append(in.Runners, r)
			}
		}
	}
	return in
}

// label makes text safe inside a quoted Mermaid label.
func label(parts ...string) string {
	var kept []string
	for _, s := range parts {
		if s = strings.Join(strings.Fields(s), " "); s != "" {
			kept = append(kept, s)
		}
	}
	r := strings.NewReplacer(`"`, "#quot;", "<", "#lt;", ">", "#gt;", "&", "#amp;", "`", "#96;")
	return r.Replace(strings.Join(kept, " · "))
}

// Generate returns the flowchart. A project with nothing else known still
// gets its own node and a comment saying so.
func Generate(in Inputs) string {
	p := in.Project
	var b strings.Builder
	line := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	line("%%%% Generated from live state by Lucidbench. Edit freely.")
	line("flowchart LR")
	line("  p_main[\"%s\"]", label(firstNonEmpty(p.Name, p.ID), p.Type, p.Status))
	if p.Repo != "" {
		line("  repo[(\"%s\")]", label(p.Repo))
		line("  p_main --- repo")
	}
	empty := p.Repo == ""

	if len(p.Deploy) > 0 {
		empty = false
		ds := append([]projects.Deploy(nil), p.Deploy...)
		sort.SliceStable(ds, func(i, j int) bool {
			return deployKey(ds[i]) < deployKey(ds[j])
		})
		line("  subgraph deploys [\"Deploys\"]")
		for i, d := range ds {
			line("    d_%d[\"%s\"]", i+1, label(d.Provider, d.Service, d.Region))
		}
		line("  end")
		for i := range ds {
			line("  p_main --> d_%d", i+1)
		}
	}

	dbNames := map[string]bool{}
	for _, d := range in.Databases {
		dbNames[d.Name] = true
	}
	cs := append([]docker.Container(nil), in.Containers...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Name < cs[j].Name })
	var svc []docker.Container
	for _, c := range cs {
		if !dbNames[c.Name] {
			svc = append(svc, c)
		}
	}
	if len(svc) > 0 {
		empty = false
		line("  subgraph compose [\"Containers\"]")
		for i, c := range svc {
			line("    c_%d[\"%s\"]", i+1, label(firstNonEmpty(c.Service, c.Name), c.Image, c.State))
		}
		line("  end")
		for i := range svc {
			line("  p_main --> c_%d", i+1)
		}
	}

	dbs := append([]databases.Discovered(nil), in.Databases...)
	sort.SliceStable(dbs, func(i, j int) bool { return dbs[i].Name < dbs[j].Name })
	if len(dbs) > 0 {
		empty = false
		line("  subgraph data [\"Databases\"]")
		for i, d := range dbs {
			port := ""
			if d.Port != 0 {
				port = fmt.Sprintf(":%d", d.Port)
			}
			line("    db_%d[(\"%s\")]", i+1, label(d.Name, d.Engine+port))
		}
		line("  end")
		for i := range dbs {
			line("  p_main -.-> db_%d", i+1)
		}
	}

	rs := append([]ci.Container(nil), in.Runners...)
	sort.SliceStable(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	if len(rs) > 0 {
		empty = false
		line("  subgraph ci [\"CI runners\"]")
		for i, r := range rs {
			line("    r_%d[\"%s\"]", i+1, label(firstNonEmpty(r.RunnerName, r.Name), r.State))
		}
		line("  end")
		for i := range rs {
			if p.Repo != "" {
				line("  repo --> r_%d", i+1)
			} else {
				line("  p_main --> r_%d", i+1)
			}
		}
	}

	into := linked(p.BuildsInto, in.Names)
	by := linked(p.BuiltBy, in.Names)
	for i, l := range into {
		empty = false
		line("  o_%d[\"%s\"]", i+1, label(l))
		line("  p_main -->|builds into| o_%d", i+1)
	}
	for i, l := range by {
		empty = false
		line("  u_%d[\"%s\"]", i+1, label(l))
		line("  u_%d -->|builds into| p_main", i+1)
	}
	if empty {
		line("  %%%% Nothing else is known about this project yet: no deploys, containers,")
		line("  %%%% databases, runners or builds_into links.")
	}
	return b.String()
}

func deployKey(d projects.Deploy) string { return d.Provider + "\x00" + d.Service + "\x00" + d.Region }

// linked returns the display names of project ids, sorted.
func linked(ids []string, names map[string]string) []string {
	var out []string
	for _, id := range ids {
		out = append(out, firstNonEmpty(names[id], id))
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
