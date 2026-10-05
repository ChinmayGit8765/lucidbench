package projects

import (
	"fmt"
	"strings"
)

// categoryTitles are the section headings, in display order.
var categoryTitles = map[string]string{
	"product":    "Products",
	"portfolio":  "Portfolio",
	"tool":       "Tools (build other projects)",
	"experiment": "Experiments",
	"coursework": "Coursework",
}

// Table renders the projects grouped by category, in category order. Projects
// with an unknown category are listed last under "Other".
func Table(ps []Project) string {
	w := [5]int{len("ID"), len("TYPE"), len("STATUS"), len("VISIBILITY"), len("NEEDS")}
	for _, p := range ps {
		for i, s := range []string{p.ID, p.Type, p.Status, p.Visibility, needs(p)} {
			w[i] = max(w[i], len(s))
		}
	}
	var b strings.Builder
	row := func(cells ...string) {
		b.WriteString("  ")
		for i, c := range cells[:5] {
			b.WriteString(fmt.Sprintf("%-*s  ", w[i], c))
		}
		b.WriteString(cells[5] + "\n")
	}
	groups := append(append([]string{}, Categories...), "")
	first := true
	for _, cat := range groups {
		var in []Project
		for _, p := range ps {
			known := false
			for _, c := range Categories {
				known = known || p.Category == c
			}
			if (cat == "" && !known) || (cat != "" && p.Category == cat) {
				in = append(in, p)
			}
		}
		if len(in) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		title := categoryTitles[cat]
		if cat == "" {
			title = "Other"
		}
		fmt.Fprintf(&b, "%s (%d)\n", strings.ToUpper(title[:1])+title[1:], len(in))
		row("ID", "TYPE", "STATUS", "VISIBILITY", "NEEDS", "NAME")
		for _, p := range in {
			row(p.ID, dash(p.Type), dash(p.Status), dash(p.Visibility), needs(p), p.Name)
		}
	}
	return b.String()
}

func needs(p Project) string {
	if p.Progress.Total == 0 {
		return "-"
	}
	s := fmt.Sprintf("%d/%d", p.Progress.Done, p.Progress.Total)
	if n := countBlocked(p); n > 0 {
		s += fmt.Sprintf(" (%d blocked)", n)
	}
	return s
}

func countBlocked(p Project) int {
	n := 0
	for _, x := range p.Needs {
		if x.Status == "blocked" {
			n++
		}
	}
	return n
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Detail renders one project with its links and needs.
func Detail(p Project) string {
	var b strings.Builder
	kv := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%-12s %s\n", k+":", v)
		}
	}
	fmt.Fprintf(&b, "%s (%s)\n\n", p.Name, p.ID)
	kv("category", p.Category)
	kv("type", p.Type)
	kv("status", p.Status)
	kv("visibility", p.Visibility)
	if p.Visibility == "confidential" {
		kv("", "never sent to AI providers")
	}
	kv("repo", p.Repo)
	kv("local path", p.LocalPath)
	kv("linear", p.Linear)
	kv("summary", p.Summary)
	kv("builds into", strings.Join(p.BuildsInto, ", "))
	kv("built by", strings.Join(p.BuiltBy, ", "))
	kv("needed by", strings.Join(p.NeededBy, ", "))
	if len(p.Needs) > 0 {
		fmt.Fprintf(&b, "\nneeds (%d/%d done):\n", p.Progress.Done, p.Progress.Total)
		for _, n := range p.Needs {
			mark := map[string]string{"done": "[x]", "doing": "[~]", "blocked": "[!]"}[n.Status]
			if mark == "" {
				mark = "[ ]"
			}
			line := fmt.Sprintf("  %s %s", mark, n.What)
			if n.From != "" {
				line += "  (from " + n.From + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}
