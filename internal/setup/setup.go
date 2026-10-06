// Package setup backs the first-run setup in the UI: whether setup is
// needed, a folder browser, a scan for git repositories under a folder, and
// adding the ones the user picks to projects.yaml.
//
// projects.yaml is the user's own file. Setup only ever appends entries to
// it: the file is backed up first, the new text is added after the old
// bytes, and the result is parsed again to prove that every existing entry
// is unchanged before it is written. When that proof fails (the file is not
// in a shape that can be appended to), nothing is written and the caller
// gets the entries as a snippet to paste. config.yaml is never written.
package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/prefs"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Defaults for imported projects. The user edits the file to change them.
const (
	DefaultCategory   = "experiment"
	DefaultStatus     = "active"
	DefaultVisibility = "private"
)

// MaxRepos is the most repositories one scan returns.
const MaxRepos = 200

// MaxDirs is the most folders one listing returns.
const MaxDirs = 500

// Errors the HTTP layer maps to status codes.
var (
	ErrBadRequest = errors.New("bad request")
	// ErrNotAppendable means the projects file exists but appending to it
	// cannot be proven safe; the snippet is returned instead.
	ErrNotAppendable = errors.New("projects.yaml cannot be appended to safely")
)

// reqError is a problem with the request; it matches ErrBadRequest.
type reqError struct{ msg string }

func (e *reqError) Error() string        { return e.msg }
func (e *reqError) Is(target error) bool { return target == ErrBadRequest }

func bad(format string, args ...any) error { return &reqError{fmt.Sprintf(format, args...)} }

// Service answers the setup routes.
type Service struct {
	// DataDir holds ui.json.
	DataDir string
	// ProjectsPath returns the projects file (projects.Path).
	ProjectsPath func() (string, error)
	// Home is the user's home folder, for the default listing and hints.
	Home string
	// Now is time.Now; tests set it.
	Now func() time.Time
}

// Status says whether first-run setup should open by itself.
type Status struct {
	// Needed is true when neither ui.json nor projects.yaml exists yet.
	Needed bool `json:"needed"`
	// UI is true when ui.json exists.
	UI bool `json:"ui"`
	// Projects is true when projects.yaml exists.
	Projects bool `json:"projects"`
	// ProjectsHint is the projects file with the home folder as ~.
	ProjectsHint string `json:"projects_hint"`
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Status reports what exists.
func (s *Service) Status() Status {
	st := Status{UI: exists(filepath.Join(s.DataDir, prefs.FileName))}
	if p, err := s.ProjectsPath(); err == nil {
		st.Projects = exists(p)
		st.ProjectsHint = projects.HomeHint(p, s.Home)
	}
	st.Needed = !st.UI && !st.Projects
	return st
}

// Dir is one folder in a listing.
type Dir struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Git is true when the folder is a git repository.
	Git bool `json:"git"`
}

// Listing is the body of GET /api/setup/dirs.
type Listing struct {
	Path   string `json:"path"`
	Parent string `json:"parent,omitempty"`
	Dirs   []Dir  `json:"dirs"`
	// Truncated is set when the folder had more than MaxDirs folders.
	Truncated bool `json:"truncated,omitempty"`
}

func isRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// skipDir reports folders that never hold a project of their own.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "$") {
		return true
	}
	switch strings.ToLower(name) {
	case "node_modules", "vendor", "target", "dist", "build", "appdata", "library", "system volume information":
		return true
	}
	return false
}

// List returns the folders inside path (home when empty). Only names are
// read, never file contents.
func (s *Service) List(path string) (Listing, error) {
	if strings.TrimSpace(path) == "" {
		path = s.Home
	}
	if !filepath.IsAbs(path) {
		return Listing{}, bad("the folder must be an absolute path")
	}
	path = filepath.Clean(path)
	ents, err := os.ReadDir(path)
	if err != nil {
		return Listing{}, bad("cannot open the folder: %v", errors.Unwrap(err))
	}
	l := Listing{Path: path, Dirs: []Dir{}}
	if parent := filepath.Dir(path); parent != path {
		l.Parent = parent
	}
	for _, e := range ents {
		if !e.IsDir() || skipDir(e.Name()) {
			continue
		}
		if len(l.Dirs) == MaxDirs {
			l.Truncated = true
			break
		}
		p := filepath.Join(path, e.Name())
		l.Dirs = append(l.Dirs, Dir{Name: e.Name(), Path: p, Git: isRepo(p)})
	}
	sort.Slice(l.Dirs, func(a, b int) bool { return strings.ToLower(l.Dirs[a].Name) < strings.ToLower(l.Dirs[b].Name) })
	return l, nil
}

// Candidate is one repository found by a scan, as the entry it would add.
type Candidate struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
	// Type is the guessed project type, or "" when no file gave it away.
	Type string `json:"type"`
	// Why names the files the guess came from.
	Why string `json:"why,omitempty"`
	// Existing is the id of the projects.yaml entry already at this path
	// or with this id; such a repository cannot be added again.
	Existing string `json:"existing,omitempty"`
}

// ScanResult is the body of POST /api/setup/scan.
type ScanResult struct {
	Root      string      `json:"root"`
	Repos     []Candidate `json:"repos"`
	Truncated bool        `json:"truncated,omitempty"`
}

var (
	nonID = regexp.MustCompile(`[^a-z0-9]+`)
	idRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

// Slug turns a folder name into a project id.
func Slug(name string) string {
	s := strings.Trim(nonID.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "project"
	}
	return s
}

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// Scan finds git repositories in root and up to two levels below it.
func (s *Service) Scan(root string) (ScanResult, error) {
	if !filepath.IsAbs(root) {
		return ScanResult{}, bad("the folder must be an absolute path")
	}
	root = filepath.Clean(root)
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return ScanResult{}, bad("%s is not a folder", root)
	}
	res := ScanResult{Root: root, Repos: []Candidate{}}
	var found []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if len(found) >= MaxRepos {
			res.Truncated = true
			return
		}
		if isRepo(dir) {
			found = append(found, dir)
			return // a repository's own subfolders are part of it
		}
		if depth == 2 {
			return
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range ents {
			if e.IsDir() && !skipDir(e.Name()) {
				walk(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	walk(root, 0)

	existing := s.existing()
	taken := map[string]bool{}
	for _, p := range existing {
		taken[p.ID] = true
	}
	for _, dir := range found {
		name := filepath.Base(dir)
		c := Candidate{Name: name, LocalPath: dir}
		c.Type, c.Why = GuessType(dir)
		for _, p := range existing {
			if p.LocalPath != "" && sameDir(p.LocalPath, dir) {
				c.Existing, c.ID = p.ID, p.ID
			}
		}
		if c.Existing == "" {
			id := Slug(name)
			for n := 2; taken[id]; n++ {
				id = fmt.Sprintf("%s-%d", Slug(name), n)
			}
			c.ID = id
			taken[id] = true
		}
		res.Repos = append(res.Repos, c)
	}
	return res, nil
}

func (s *Service) existing() []projects.Project {
	p, err := s.ProjectsPath()
	if err != nil {
		return nil
	}
	return projects.LoadFrom(p, s.Home).Projects
}

// GuessType guesses a project type from the files at the top of dir, and
// says which files it looked at.
func GuessType(dir string) (string, string) {
	has := func(names ...string) string {
		for _, n := range names {
			if matches, _ := filepath.Glob(filepath.Join(dir, n)); len(matches) > 0 {
				return filepath.Base(matches[0])
			}
		}
		return ""
	}
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(b) > 1<<20 {
			return ""
		}
		return string(b)
	}
	if f := has("project.godot", "*.uproject", "ProjectSettings/ProjectVersion.txt"); f != "" {
		return "game", f
	}
	if f := has("src-tauri", "wails.json"); f != "" {
		return "desktop-app", f
	}
	pkg := read("package.json")
	if strings.Contains(pkg, `"electron"`) {
		return "desktop-app", "package.json (electron)"
	}
	if f := has("astro.config.*", "hugo.toml", "_config.yml", "docusaurus.config.*"); f != "" {
		return "site", f
	}
	for _, dep := range []string{`"next"`, `"vite"`, `"react"`, `"vue"`, `"svelte"`, `"@angular/core"`, `"nuxt"`} {
		if strings.Contains(pkg, dep) {
			return "web-app", "package.json (" + strings.Trim(dep, `"`) + ")"
		}
	}
	if f := has("*.ipynb", "notebooks"); f != "" {
		return "ml-research", f
	}
	if read("go.mod") != "" {
		if has("Dockerfile") != "" {
			return "service", "go.mod + Dockerfile"
		}
		if has("cmd", "main.go") != "" {
			return "cli", "go.mod + main"
		}
		return "library", "go.mod"
	}
	if cargo := read("Cargo.toml"); cargo != "" {
		if has("src/main.rs") != "" || strings.Contains(cargo, "[[bin]]") {
			return "cli", "Cargo.toml + src/main.rs"
		}
		return "library", "Cargo.toml"
	}
	if f := has("Dockerfile", "docker-compose.yml", "compose.yaml"); f != "" {
		return "service", f
	}
	if pkg != "" {
		return "library", "package.json"
	}
	if f := has("pyproject.toml", "setup.py"); f != "" {
		return "library", f
	}
	return "", ""
}

// Entry is one project to add: the fields the user can change in setup.
type Entry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
	Type      string `json:"type,omitempty"`
}

// AddResult is the body of POST /api/setup/projects.
type AddResult struct {
	// Added are the ids written.
	Added []string `json:"added"`
	// Backup is the backup file's name, beside projects.yaml; empty when the
	// file did not exist before.
	Backup string `json:"backup,omitempty"`
	// Created is true when projects.yaml was created.
	Created bool `json:"created,omitempty"`
	// Snippet is the YAML that was (or, on refusal, would be) appended.
	Snippet string `json:"snippet"`
	// PathHint is the projects file with the home folder as ~.
	PathHint string `json:"path_hint"`
}

// quote writes a YAML double-quoted scalar.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

// Snippet renders entries as items of the projects: list.
func Snippet(entries []Entry) string {
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "  - id: %s\n", e.ID)
		fmt.Fprintf(&b, "    name: %s\n", quote(e.Name))
		fmt.Fprintf(&b, "    category: %s\n", DefaultCategory)
		if e.Type != "" {
			fmt.Fprintf(&b, "    type: %s\n", e.Type)
		}
		fmt.Fprintf(&b, "    status: %s\n", DefaultStatus)
		fmt.Fprintf(&b, "    visibility: %s\n", DefaultVisibility)
		fmt.Fprintf(&b, "    local_path: %s\n", quote(e.LocalPath))
	}
	return b.String()
}

const header = `# Lucidbench projects. This file is yours; Lucidbench's first-run setup
# created it and only ever appends to it. See projects.example.yaml in the
# repository for every field.

version: 1
projects:
`

func (s *Service) check(entries []Entry) error {
	if len(entries) == 0 {
		return bad("pick at least one repository")
	}
	if len(entries) > MaxRepos {
		return bad("at most %d projects at once", MaxRepos)
	}
	ids := map[string]bool{}
	for _, p := range s.existing() {
		ids[p.ID] = true
	}
	for _, e := range entries {
		switch {
		case !idRE.MatchString(e.ID):
			return bad("id %q: use lowercase letters, digits and dashes", e.ID)
		case ids[e.ID]:
			return bad("id %q is already used", e.ID)
		case strings.TrimSpace(e.Name) == "" || len(e.Name) > 80:
			return bad("%s: the name must be 1-80 characters", e.ID)
		case !filepath.IsAbs(e.LocalPath):
			return bad("%s: local_path must be an absolute path", e.ID)
		case e.Type != "" && !contains(projects.Types, e.Type):
			return bad("%s: unknown type %q", e.ID, e.Type)
		}
		if !isRepo(e.LocalPath) {
			return bad("%s: %s is not a git repository", e.ID, e.LocalPath)
		}
		ids[e.ID] = true
	}
	return nil
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// Preview returns what Add would append, without writing.
func (s *Service) Preview(entries []Entry) (AddResult, error) {
	if err := s.check(entries); err != nil {
		return AddResult{}, err
	}
	p, err := s.ProjectsPath()
	if err != nil {
		return AddResult{}, err
	}
	return AddResult{Added: []string{}, Snippet: Snippet(entries), PathHint: projects.HomeHint(p, s.Home), Created: !exists(p)}, nil
}

// Add appends entries to projects.yaml, creating it when it does not exist.
// An existing file is backed up first and never rewritten: the new bytes are
// the old bytes plus the entries, and the result must parse with every old
// entry unchanged and no new problem, or nothing is written and
// ErrNotAppendable comes back with the snippet.
func (s *Service) Add(entries []Entry) (AddResult, error) {
	res, err := s.Preview(entries)
	if err != nil {
		return res, err
	}
	path, err := s.ProjectsPath()
	if err != nil {
		return res, err
	}
	for _, e := range entries {
		res.Added = append(res.Added, e.ID)
	}
	old, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		res.Created = true
		if err := prefs.WriteAtomic(path, []byte(header+res.Snippet)); err != nil {
			return res, err
		}
		return res, nil
	case err != nil:
		return res, err
	}
	next := append([]byte{}, old...)
	if len(next) > 0 && next[len(next)-1] != '\n' {
		next = append(next, '\n')
	}
	next = append(next, res.Snippet...)
	if err := provenAppend(old, next, entries); err != nil {
		res.Added = []string{}
		return res, fmt.Errorf("%w: %v", ErrNotAppendable, err)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	backup := path + ".bak-" + now().Format("20060102-150405")
	if err := os.WriteFile(backup, old, 0o600); err != nil {
		res.Added = []string{}
		return res, fmt.Errorf("cannot back up projects.yaml: %w", err)
	}
	res.Backup = filepath.Base(backup)
	if err := prefs.WriteAtomic(path, next); err != nil {
		res.Added = []string{}
		return res, err
	}
	return res, nil
}

// provenAppend checks that next keeps every project of old exactly as it was,
// adds exactly entries after them, and reports no problem old did not have.
func provenAppend(old, next []byte, entries []Entry) error {
	before, errsBefore := projects.Parse(old)
	after, errsAfter := projects.Parse(next)
	if len(after) != len(before)+len(entries) {
		return fmt.Errorf("the file would list %d projects instead of %d (the projects: list is probably not the last thing in the file)", len(after), len(before)+len(entries))
	}
	strip := func(p projects.Project) projects.Project {
		p.BuiltBy, p.NeededBy, p.Progress = nil, nil, projects.Progress{}
		return p
	}
	for i := range before {
		if !reflect.DeepEqual(strip(before[i]), strip(after[i])) {
			return fmt.Errorf("existing project %q would change", before[i].ID)
		}
	}
	for i, e := range entries {
		got := after[len(before)+i]
		if got.ID != e.ID || got.LocalPath != e.LocalPath || got.Name != e.Name || got.Type != e.Type {
			return fmt.Errorf("new project %q did not read back as written", e.ID)
		}
	}
	had := map[string]bool{}
	for _, e := range errsBefore {
		had[e] = true
	}
	for _, e := range errsAfter {
		if !had[e] {
			return fmt.Errorf("the file would gain a problem: %s", e)
		}
	}
	return nil
}
