package prompts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Context source kinds.
const (
	KindProject   = "project"
	KindPage      = "page"
	KindRules     = "rules"
	KindRepo      = "repo"
	KindCard      = "card"
	KindDecisions = "decisions"
)

// Source formats.
const (
	FormatText = "text"
	FormatCode = "code" // shown in a code fence
)

// Ref names one context source. Project is used by the project, rules,
// repo and decisions kinds; Path by page; Board and ID by card.
type Ref struct {
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
	Path    string `json:"path,omitempty"`
	Board   string `json:"board,omitempty"`
	ID      string `json:"id,omitempty"`
}

// Source is a resolved context source: its text, its size, and whether it is
// confidential (it may then never reach a provider).
type Source struct {
	Ref
	Label        string   `json:"label"`
	Text         string   `json:"text"`
	Format       string   `json:"format"`
	Chars        int      `json:"chars"`
	Tokens       int      `json:"tokens"`
	Confidential bool     `json:"confidential"`
	Truncated    bool     `json:"truncated,omitempty"`
	Files        []string `json:"files,omitempty"` // rules: the files read
}

// Errors from Resolve, each mapped to an HTTP status.
var (
	ErrBadRef   = errors.New("bad context source")
	ErrNotFound = errors.New("not found")
)

// Context limits, so one source cannot swamp a prompt.
const (
	MaxPageChars  = 24000
	MaxRulesChars = 16000 // per file
	repoDepth     = 2
	repoMaxLines  = 200
	repoMaxWalk   = 50000
	decisionsMax  = 6
)

// RulesFiles are the files offered as "project rules", in this order.
var RulesFiles = []string{"docs/BETA-CONTRACTS.md", "CONTRIBUTING.md", "AGENTS.md", "CLAUDE.md"}

// repoSkip are folders a repo map leaves out: git's own, dependencies, build
// output (dist, and target for Cargo) and agent settings.
var repoSkip = map[string]bool{".git": true, "node_modules": true, "dist": true, ".claude": true, "target": true}

// Resolver reads context sources.
type Resolver struct {
	Projects func() (*projects.List, error)
	Vault    memory.Opener
	Council  *council.Service
	Home     string // shown as "~"
}

func (r *Resolver) project(id string) (*projects.Project, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: choose a project", ErrBadRef)
	}
	if r.Projects == nil {
		return nil, fmt.Errorf("%w: no projects file", ErrNotFound)
	}
	l, err := r.Projects()
	if err != nil {
		return nil, err
	}
	p := l.Find(id)
	if p == nil {
		return nil, fmt.Errorf("%w: no project %q in projects.yaml", ErrNotFound, id)
	}
	return p, nil
}

func (r *Resolver) vault() (*memory.Vault, error) {
	if r.Vault == nil {
		return nil, fmt.Errorf("%w: there is no Memory vault", ErrNotFound)
	}
	return r.Vault()
}

// Resolve reads one source.
func (r *Resolver) Resolve(ref Ref) (Source, error) {
	var s Source
	var err error
	switch ref.Kind {
	case KindProject:
		s, err = r.projectSource(ref)
	case KindPage:
		s, err = r.pageSource(ref)
	case KindRules:
		s, err = r.rulesSource(ref)
	case KindRepo:
		s, err = r.repoSource(ref)
	case KindCard:
		s, err = r.cardSource(ref)
	case KindDecisions:
		s, err = r.decisionsSource(ref)
	default:
		return s, fmt.Errorf("%w: kind must be project, page, rules, repo, card or decisions", ErrBadRef)
	}
	if err != nil {
		return s, err
	}
	s.Ref = ref
	if s.Format == "" {
		s.Format = FormatText
	}
	s.Text = strings.TrimSpace(s.Text)
	s.Chars = utf8.RuneCountInString(s.Text)
	s.Tokens = EstimateTokens(s.Text)
	return s, nil
}

// Vars returns the template variables for a project; nil when id is "" or
// unknown.
func (r *Resolver) Vars(id string) map[string]string {
	if id == "" {
		return nil
	}
	p, err := r.project(id)
	if err != nil {
		return nil
	}
	return ProjectVars(*p, r.Home)
}

// ProjectVars are the template variables a project fills.
func ProjectVars(p projects.Project, home string) map[string]string {
	v := map[string]string{
		"project.id":      p.ID,
		"project.name":    p.Name,
		"project.repo":    p.Repo,
		"project.summary": oneLine(p.Summary),
	}
	if p.LocalPath != "" {
		v["project.local_path"] = projects.HomeHint(p.LocalPath, home)
	}
	if p.Assessment != nil {
		sug := assess.Suggest(*p.Assessment)
		v["project.kind"] = p.Assessment.Kind
		v["project.risk"] = sug.Risk.Level
		v["project.criteria"] = bullets(sug.DoneCriteria)
		v["project.checks"] = bullets(sug.CheckCommands)
	}
	return v
}

func bullets(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "- " + strings.Join(items, "\n- ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (r *Resolver) projectSource(ref Ref) (Source, error) {
	p, err := r.project(ref.Project)
	if err != nil {
		return Source{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s (id %s)\n", p.Name, p.ID)
	fmt.Fprintf(&b, "Category: %s · type: %s · status: %s · visibility: %s\n", p.Category, p.Type, p.Status, p.Visibility)
	if p.Repo != "" {
		fmt.Fprintf(&b, "Repository: %s\n", p.Repo)
	}
	if p.LocalPath != "" {
		fmt.Fprintf(&b, "Local checkout: %s\n", projects.HomeHint(p.LocalPath, r.Home))
	}
	if p.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", oneLine(p.Summary))
	}
	if len(p.BuildsInto) > 0 {
		fmt.Fprintf(&b, "Builds into: %s\n", strings.Join(p.BuildsInto, ", "))
	}
	if len(p.Needs) > 0 {
		b.WriteString("Needs:\n")
		for _, n := range p.Needs {
			from := ""
			if n.From != "" {
				from = " (from " + n.From + ")"
			}
			fmt.Fprintf(&b, "- [%s] %s%s\n", n.Status, n.What, from)
		}
	}
	if p.Assessment != nil {
		sug := assess.Suggest(*p.Assessment)
		fmt.Fprintf(&b, "\nAssessment: kind %s, risk %s\n", p.Assessment.Kind, sug.Risk.Level)
		for _, why := range sug.Risk.Reasons {
			fmt.Fprintf(&b, "- risk: %s\n", why)
		}
		if len(sug.DoneCriteria) > 0 {
			b.WriteString("Done criteria for this kind of project:\n" + bullets(sug.DoneCriteria) + "\n")
		}
		if len(sug.CheckCommands) > 0 {
			b.WriteString("Checks to run:\n" + bullets(sug.CheckCommands) + "\n")
		}
	}
	return Source{Label: "Project: " + p.Name, Text: b.String(), Confidential: p.Visibility == "confidential"}, nil
}

func (r *Resolver) pageSource(ref Ref) (Source, error) {
	if ref.Path == "" {
		return Source{}, fmt.Errorf("%w: choose a Memory page", ErrBadRef)
	}
	v, err := r.vault()
	if err != nil {
		return Source{}, err
	}
	pg, err := v.Read(ref.Path)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			return Source{}, fmt.Errorf("%w: no page %s", ErrNotFound, ref.Path)
		}
		if errors.Is(err, memory.ErrBadPath) {
			return Source{}, fmt.Errorf("%w: %v", ErrBadRef, err)
		}
		return Source{}, err
	}
	text, cut := clip(pg.Body, MaxPageChars)
	return Source{Label: "Page: " + pg.Title, Text: text, Truncated: cut, Confidential: pg.Confidential}, nil
}

func (r *Resolver) checkout(id string) (*projects.Project, error) {
	p, err := r.project(id)
	if err != nil {
		return nil, err
	}
	if p.LocalPath == "" {
		return nil, fmt.Errorf("%w: %s has no local_path in projects.yaml", ErrNotFound, p.Name)
	}
	if st, err := os.Stat(p.LocalPath); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: %s's local_path is not a folder", ErrNotFound, p.Name)
	}
	return p, nil
}

func (r *Resolver) rulesSource(ref Ref) (Source, error) {
	p, err := r.checkout(ref.Project)
	if err != nil {
		return Source{}, err
	}
	var b strings.Builder
	s := Source{Label: "Project rules: " + p.Name, Confidential: p.Visibility == "confidential", Files: []string{}}
	for _, f := range RulesFiles {
		data, err := os.ReadFile(filepath.Join(p.LocalPath, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		text, cut := clip(string(data), MaxRulesChars)
		s.Truncated = s.Truncated || cut
		s.Files = append(s.Files, f)
		fmt.Fprintf(&b, "#### %s\n\n%s\n\n", f, strings.TrimSpace(text))
	}
	if len(s.Files) == 0 {
		return Source{}, fmt.Errorf("%w: %s has none of %s", ErrNotFound, p.Name, strings.Join(RulesFiles, ", "))
	}
	s.Text = b.String()
	return s, nil
}

// secretFile reports whether a file name looks like a credential; a repo map
// leaves such names out.
func secretFile(n string) bool {
	l := strings.ToLower(n)
	return strings.HasPrefix(l, ".env") || strings.HasSuffix(l, ".pem") || strings.HasSuffix(l, ".key") ||
		strings.HasPrefix(l, "id_rsa") || strings.HasPrefix(l, "id_ed25519")
}

func (r *Resolver) repoSource(ref Ref) (Source, error) {
	p, err := r.checkout(ref.Project)
	if err != nil {
		return Source{}, err
	}
	text, cut := RepoMap(p.LocalPath)
	return Source{Label: "Repo map: " + p.Name, Text: text, Truncated: cut, Format: FormatCode, Confidential: p.Visibility == "confidential"}, nil
}

// RepoMap is the tree of root two levels deep, each folder with the number
// of files below it, leaving out .git, node_modules, dist, target and .claude.
func RepoMap(root string) (string, bool) {
	var lines []string
	cut := false
	var walk func(dir, indent string, depth int)
	walk = func(dir, indent string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		for _, e := range entries {
			name := e.Name()
			if repoSkip[name] || secretFile(name) {
				continue
			}
			if len(lines) >= repoMaxLines {
				cut = true
				return
			}
			if e.IsDir() {
				n, more := countFiles(filepath.Join(dir, name))
				count := fmt.Sprintf("%d %s", n, plural(n, "file"))
				if more {
					count = fmt.Sprintf("%d+ files", n)
				}
				lines = append(lines, fmt.Sprintf("%s%s/  (%s)", indent, name, count))
				if depth < repoDepth {
					walk(filepath.Join(dir, name), indent+"  ", depth+1)
				}
				continue
			}
			lines = append(lines, indent+name)
		}
	}
	walk(root, "", 1)
	total, more := countFiles(root)
	head := fmt.Sprintf("%d files in all", total)
	if more {
		head = fmt.Sprintf("more than %d files in all", total)
	}
	head += " (.git, node_modules, dist, target and .claude left out)"
	return head + "\n" + strings.Join(lines, "\n"), cut
}

// countFiles counts the files below dir, skipping the folders a repo map
// leaves out, and stops at repoMaxWalk.
func countFiles(dir string) (int, bool) {
	n := 0
	more := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && repoSkip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		n++
		if n >= repoMaxWalk {
			more = true
			return filepath.SkipAll
		}
		return nil
	})
	return n, more
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func (r *Resolver) cardSource(ref Ref) (Source, error) {
	board := ref.Board
	if board == "" {
		board = boards.DefaultBoard
	}
	if ref.ID == "" {
		return Source{}, fmt.Errorf("%w: choose a card", ErrBadRef)
	}
	v, err := r.vault()
	if err != nil {
		return Source{}, err
	}
	b, err := boards.Get(v, board)
	if err != nil {
		return Source{}, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	var c *boards.Card
	for i := range b.Cards {
		if b.Cards[i].ID == ref.ID {
			c = &b.Cards[i]
		}
	}
	if c == nil {
		return Source{}, fmt.Errorf("%w: no card %q on board %s", ErrNotFound, ref.ID, board)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Card: %s\nBoard: %s, column %s\n", c.Title, b.Title, c.Column)
	s := Source{Label: "Card: " + c.Title}
	if c.Project != "" {
		fmt.Fprintf(&sb, "Project: %s\n", c.Project)
		if p, err := r.project(c.Project); err == nil && p.Visibility == "confidential" {
			s.Confidential = true
		}
	}
	if len(c.Labels) > 0 {
		fmt.Fprintf(&sb, "Labels: %s\n", strings.Join(c.Labels, ", "))
	}
	if c.Due != "" {
		fmt.Fprintf(&sb, "Due: %s\n", c.Due)
	}
	if c.Memory != "" {
		pg, err := v.Read(c.Memory)
		if err == nil {
			s.Confidential = s.Confidential || pg.Confidential
			text, cut := clip(pg.Body, MaxPageChars)
			s.Truncated = cut
			fmt.Fprintf(&sb, "\nIts brief (%s):\n\n%s\n", c.Memory, strings.TrimSpace(text))
		} else {
			fmt.Fprintf(&sb, "Brief: %s (cannot be read)\n", c.Memory)
		}
	}
	s.Text = sb.String()
	return s, nil
}

var sectionRE = regexp.MustCompile(`(?m)^## +(.+?)\s*$`)

// BriefSection returns the body of a "## name" section of a brief, or "".
func BriefSection(brief, name string) string {
	locs := sectionRE.FindAllStringSubmatchIndex(brief, -1)
	for i, m := range locs {
		if !strings.EqualFold(strings.TrimSpace(brief[m[2]:m[3]]), name) {
			continue
		}
		end := len(brief)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		return strings.TrimSpace(brief[m[1]:end])
	}
	return ""
}

func (r *Resolver) decisionsSource(ref Ref) (Source, error) {
	p, err := r.project(ref.Project)
	if err != nil {
		return Source{}, err
	}
	if r.Council == nil {
		return Source{}, fmt.Errorf("%w: the council is not available", ErrNotFound)
	}
	list, err := r.Council.List()
	if err != nil {
		return Source{}, err
	}
	var b strings.Builder
	n := 0
	for _, sum := range list {
		if sum.Project != p.ID || (sum.Status != council.StatusApproved && sum.Status != council.StatusDraft) {
			continue
		}
		if n == decisionsMax {
			break
		}
		n++
		sess, err := r.Council.Get(sum.ID)
		if err != nil {
			continue
		}
		state := "approved"
		if sess.Status == council.StatusDraft {
			state = "draft, not approved yet"
		} else if sess.ApprovedWithBlockers {
			state = "approved with a blocker still open"
		}
		title := sess.Title
		if title == "" {
			title = council.BriefTitle(sess.Brief)
		}
		fmt.Fprintf(&b, "- %s · %s (%s)", sess.Created.Format("2006-01-02"), title, state)
		if sess.BriefPath != "" {
			fmt.Fprintf(&b, ", brief %s", sess.BriefPath)
		}
		b.WriteString("\n")
		if o := BriefSection(sess.Brief, "Outcome"); o != "" {
			fmt.Fprintf(&b, "  Outcome: %s\n", oneLine(o))
		}
	}
	if n == 0 {
		return Source{}, fmt.Errorf("%w: the council has no brief for %s yet", ErrNotFound, p.Name)
	}
	return Source{Label: "Council decisions: " + p.Name, Text: b.String(), Confidential: p.Visibility == "confidential"}, nil
}

// clip cuts s to n characters on a line boundary, with a note.
func clip(s string, n int) (string, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	r := []rune(s)[:n]
	out := string(r)
	if i := strings.LastIndex(out, "\n"); i > n/2 {
		out = out[:i]
	}
	return out + "\n\n[… cut: the rest is longer than Studio sends]", true
}
