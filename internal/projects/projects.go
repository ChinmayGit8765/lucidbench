// Package projects reads the user's project portfolio: what they build, what
// kind of thing each project is (a product, a portfolio piece, a private tool
// that builds other projects, an experiment or coursework) and what each one
// needs from the others.
//
// The file is the user's own: <data dir>/projects.yaml, or $LUCID_PROJECTS.
// It is never part of the repository; projects.example.yaml shows the shape.
package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Accepted values, in display order.
var (
	Categories   = []string{"product", "portfolio", "tool", "experiment", "coursework"}
	Types        = []string{"game", "web-app", "desktop-app", "cli", "library", "service", "ml-research", "site", "video-system"}
	Statuses     = []string{"idea", "active", "paused", "frozen", "shipped", "archived"}
	Visibilities = []string{"public", "private", "confidential"}
	NeedStatuses = []string{"todo", "doing", "done", "blocked"}
)

// FileName is the projects file name inside the data dir.
const FileName = "projects.yaml"

var idRE = regexp.MustCompile(`^[a-z0-9-]+$`)

// Need is something a project needs before it can move on, optionally
// supplied by another project.
type Need struct {
	What   string `json:"what" yaml:"what"`
	From   string `json:"from,omitempty" yaml:"from"`
	Status string `json:"status" yaml:"status"`
}

// Progress counts done needs.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Project is one entry, plus the links derived from every other entry.
type Project struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	Category   string   `json:"category" yaml:"category"`
	Type       string   `json:"type" yaml:"type"`
	Status     string   `json:"status" yaml:"status"`
	Visibility string   `json:"visibility" yaml:"visibility"`
	Repo       string   `json:"repo,omitempty" yaml:"repo"`
	Linear     string   `json:"linear,omitempty" yaml:"linear"`
	Summary    string   `json:"summary,omitempty" yaml:"summary"`
	BuildsInto []string `json:"builds_into" yaml:"builds_into"`
	Needs      []Need   `json:"needs" yaml:"needs"`

	// Derived: projects that list this one in builds_into, projects whose
	// needs name this one in from, and how many of this project's needs are
	// done.
	BuiltBy  []string `json:"built_by" yaml:"-"`
	NeededBy []string `json:"needed_by" yaml:"-"`
	Progress Progress `json:"progress" yaml:"-"`
}

// List is the loaded file. A missing file is not an error: Configured is
// false and Projects is empty. Errors are validation problems; the projects
// that could be read are still returned.
type List struct {
	Configured bool      `json:"configured"`
	PathHint   string    `json:"path_hint"`
	Projects   []Project `json:"projects"`
	Errors     []string  `json:"errors"`
}

// Path returns the projects file: $LUCID_PROJECTS, else <data dir>/projects.yaml.
func Path() (string, error) {
	if p := os.Getenv("LUCID_PROJECTS"); p != "" {
		return p, nil
	}
	d, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, FileName), nil
}

// Load reads the projects file from Path.
func Load() (*List, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	return LoadFrom(p, home), nil
}

// HomeHint replaces a leading home directory in path with "~", so a path can
// be shown without the user's name in it.
func HomeHint(path, home string) string {
	if home == "" {
		return path
	}
	h := filepath.Clean(home)
	p := filepath.Clean(path)
	if p == h {
		return "~"
	}
	if strings.HasPrefix(strings.ToLower(p), strings.ToLower(h+string(filepath.Separator))) {
		return "~" + string(filepath.Separator) + p[len(h)+1:]
	}
	return path
}

// LoadFrom reads and validates the file at path. home is only used for the
// path hint.
func LoadFrom(path, home string) *List {
	l := &List{PathHint: HomeHint(path, home), Projects: []Project{}, Errors: []string{}}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return l
	case err != nil:
		l.Configured = true
		l.Errors = append(l.Errors, fmt.Sprintf("%s: cannot read the file: %v", l.PathHint, errors.Unwrap(err)))
		return l
	}
	l.Configured = true
	ps, errs := Parse(data)
	l.Projects = ps
	l.Errors = append(l.Errors, errs...)
	return l
}

type file struct {
	Version  *int      `yaml:"version"`
	Projects []Project `yaml:"projects"`
}

// Parse decodes and validates a projects file and derives the links. It
// returns every project it could read and one message per problem; each
// message names the project id and the field.
func Parse(data []byte) ([]Project, []string) {
	var f file
	var errs []string
	if err := yaml.Unmarshal(data, &f); err != nil {
		// A type error (a list where a string belongs) still decodes the
		// rest of the file; a syntax error does not.
		var te *yaml.TypeError
		if !errors.As(err, &te) {
			return []Project{}, []string{"invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}
		}
		errs = append(errs, te.Errors...)
	}
	bad := func(id, field, msg string) {
		errs = append(errs, fmt.Sprintf("project %q: %s: %s", id, field, msg))
	}
	if f.Version != nil && *f.Version != 1 {
		errs = append(errs, fmt.Sprintf("version: must be 1, got %d", *f.Version))
	}

	out := []Project{}
	seen := map[string]bool{}
	for i, p := range f.Projects {
		if p.ID == "" {
			errs = append(errs, fmt.Sprintf("project #%d: id: required", i+1))
			continue
		}
		if !idRE.MatchString(p.ID) {
			bad(p.ID, "id", "use lowercase letters, digits and dashes")
		}
		if seen[p.ID] {
			bad(p.ID, "id", "duplicate id; only the first entry is used")
			continue
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Name) == "" {
			bad(p.ID, "name", "required")
		}
		enum := func(field, v string, allowed []string, required bool) {
			switch {
			case v == "" && required:
				bad(p.ID, field, "required (one of "+strings.Join(allowed, ", ")+")")
			case v != "" && !slices.Contains(allowed, v):
				bad(p.ID, field, fmt.Sprintf("unknown value %q (want one of %s)", v, strings.Join(allowed, ", ")))
			}
		}
		enum("category", p.Category, Categories, true)
		enum("type", p.Type, Types, false)
		enum("status", p.Status, Statuses, true)
		enum("visibility", p.Visibility, Visibilities, true)
		if p.BuildsInto == nil {
			p.BuildsInto = []string{}
		}
		if p.Needs == nil {
			p.Needs = []Need{}
		}
		for j := range p.Needs {
			n := &p.Needs[j]
			if n.Status == "" {
				n.Status = "todo"
			}
			if strings.TrimSpace(n.What) == "" {
				bad(p.ID, fmt.Sprintf("needs[%d].what", j), "required")
			}
			if !slices.Contains(NeedStatuses, n.Status) {
				bad(p.ID, fmt.Sprintf("needs[%d].status", j),
					fmt.Sprintf("unknown value %q (want one of %s)", n.Status, strings.Join(NeedStatuses, ", ")))
			}
		}
		p.BuiltBy, p.NeededBy = []string{}, []string{}
		out = append(out, p)
	}

	// References must name known projects.
	idx := map[string]int{}
	for i, p := range out {
		idx[p.ID] = i
	}
	for i := range out {
		p := &out[i]
		for _, t := range p.BuildsInto {
			switch _, ok := idx[t]; {
			case !ok:
				bad(p.ID, "builds_into", fmt.Sprintf("unknown project id %q", t))
			case t == p.ID:
				bad(p.ID, "builds_into", "a project cannot build into itself")
			}
		}
		for j, n := range p.Needs {
			if _, ok := idx[n.From]; n.From != "" && !ok {
				bad(p.ID, fmt.Sprintf("needs[%d].from", j), fmt.Sprintf("unknown project id %q", n.From))
			}
		}
	}
	derive(out, idx)
	return out, errs
}

// derive fills BuiltBy, NeededBy and Progress. Unknown ids are ignored.
func derive(ps []Project, idx map[string]int) {
	add := func(list []string, id string) []string {
		if slices.Contains(list, id) {
			return list
		}
		return append(list, id)
	}
	for _, p := range ps {
		for _, t := range p.BuildsInto {
			if i, ok := idx[t]; ok && t != p.ID {
				ps[i].BuiltBy = add(ps[i].BuiltBy, p.ID)
			}
		}
		for _, n := range p.Needs {
			if i, ok := idx[n.From]; ok && n.From != p.ID {
				ps[i].NeededBy = add(ps[i].NeededBy, p.ID)
			}
		}
	}
	for i := range ps {
		pr := Progress{Total: len(ps[i].Needs)}
		for _, n := range ps[i].Needs {
			if n.Status == "done" {
				pr.Done++
			}
		}
		ps[i].Progress = pr
	}
}

// Find returns the project with the given id.
func (l *List) Find(id string) *Project {
	for i := range l.Projects {
		if l.Projects[i].ID == id {
			return &l.Projects[i]
		}
	}
	return nil
}
