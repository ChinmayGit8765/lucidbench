package assistant

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The action catalog: the only things the assistant may propose. Each one,
// when the user applies it, becomes requests to Lucidbench's own routes; the
// assistant itself never writes anything.
const (
	ActCreateCard     = "create_card"
	ActCreateProject  = "create_project"
	ActCreateIdea     = "create_idea"
	ActCreatePage     = "create_page"
	ActStartCouncil   = "start_council"
	ActStartWork      = "start_work"
	ActAddNeeds       = "add_needs"
	ActLinkBuildsInto = "link_builds_into"
)

// Catalog lists every action kind, in the order the prompt names them.
var Catalog = []CatalogEntry{
	{ActCreateCard, "Add a card to the work board", "project, title, body, column", false},
	{ActCreateProject, "Add a project to projects.yaml", "id, name, kind, local_path?", false},
	{ActCreateIdea, "Note an idea: a page in Memory's Inbox and a card in the Inbox column", "title, body, project?", false},
	{ActCreatePage, "Write a new Memory page", "path, markdown", false},
	{ActStartCouncil, "Start a council on a braindump (spends on your accounts)", "braindump, project?", true},
	{ActStartWork, "Start a Work session (spends on your account)", "project, prompt, provider?, model?", true},
	{ActAddNeeds, "Add needs to a project in projects.yaml", "project, needs[]", false},
	{ActLinkBuildsInto, "Say a project builds into another, in projects.yaml", "project, target", false},
}

// CatalogEntry describes one action kind.
type CatalogEntry struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Args   string `json:"args"`
	Spends bool   `json:"spends"`
}

// Kinds returns every action kind.
func Kinds() []string {
	out := make([]string, len(Catalog))
	for i, c := range Catalog {
		out[i] = c.Kind
	}
	return out
}

func known(kind string) bool { return slices.Contains(Kinds(), kind) }

// Size caps on what an action may carry.
const (
	MaxActions   = 10
	MaxTitle     = 200
	MaxBody      = 8000
	MaxPage      = 20000
	MaxPagePath  = 160
	MaxPrompt    = 20000
	MaxNeeds     = 10
	MaxNeed      = 200
	MaxName      = 80
	MaxProjectID = 40
)

// Action is one proposal as the model wrote it: a kind and its arguments.
type Action struct {
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args"`
}

// Request is one call to a Lucidbench route that applying an action makes.
// The web UI sends them in order, with the confirm header.
type Request struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   any    `json:"body,omitempty"`
}

// Proposal is a validated action, ready to show as a card.
type Proposal struct {
	Action string         `json:"action"`
	Args   map[string]any `json:"args"`
	// Summary is one line for the card's title.
	Summary string `json:"summary"`
	// Page is the Memory page a card or idea will link, when it has one.
	Page string `json:"page,omitempty"`
	Valid   bool   `json:"valid"`
	// Problems say why an action cannot be applied.
	Problems []string `json:"problems"`
	// Spends is true for council and Work: the UI asks first.
	Spends bool `json:"spends,omitempty"`
	// Requests are what Apply sends. Empty for a snippet-only action.
	Requests []Request `json:"requests,omitempty"`
	// Snippet is YAML for the user to paste into projects.yaml, which
	// Lucidbench never rewrites.
	Snippet string `json:"snippet,omitempty"`
	// Status is pending, applied, skipped or failed; Note says more.
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// Outcome values for Proposal.Status.
const (
	StatusPending = "pending"
	StatusApplied = "applied"
	StatusSkipped = "skipped"
	StatusFailed  = "failed"
)

// World is what validation checks actions against. Every field is read
// fresh for each check.
type World struct {
	Projects []projects.Project
	// Columns are the work board's columns.
	Columns []string
	// PageExists reports whether a vault page is there; nil means no vault.
	PageExists func(p string) bool
}

func (w *World) project(id string) *projects.Project {
	for i := range w.Projects {
		if w.Projects[i].ID == id {
			return &w.Projects[i]
		}
	}
	return nil
}

func (w *World) columns() []string {
	if len(w.Columns) > 0 {
		return w.Columns
	}
	return boards.DefaultColumns
}

// Argument shapes, one per kind. Unknown keys are refused.
type (
	cardArgs struct {
		Project string `json:"project"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		Column  string `json:"column"`
	}
	projectArgs struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		LocalPath string `json:"local_path"`
	}
	ideaArgs struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Project string `json:"project"`
	}
	pageArgs struct {
		Path     string `json:"path"`
		Markdown string `json:"markdown"`
	}
	councilArgs struct {
		Braindump string `json:"braindump"`
		Project   string `json:"project"`
	}
	workArgs struct {
		Project  string `json:"project"`
		Prompt   string `json:"prompt"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	needsArgs struct {
		Project string `json:"project"`
		Needs   []need `json:"needs"`
	}
	linkArgs struct {
		Project string `json:"project"`
		Target  string `json:"target"`
	}
)

// need is {what, from?}, or a plain string for what.
type need struct {
	What string `json:"what"`
	From string `json:"from,omitempty"`
}

func (n *need) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		n.What = s
		return nil
	}
	type plain need
	var p plain
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return err
	}
	*n = need(p)
	return nil
}

var (
	projectIDRE = regexp.MustCompile(`^[a-z0-9-]+$`)
	modelRE     = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	slugRE      = regexp.MustCompile(`[^a-z0-9]+`)
)

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// Slug turns a title into a file name part.
func Slug(s string) string {
	s = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		s = "untitled"
	}
	return s
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// checker collects problems for one action.
type checker struct{ problems []string }

func (c *checker) bad(format string, a ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, a...))
}

func (c *checker) text(field, v string, min, max int) {
	switch n := len(v); {
	case n < min:
		c.bad("%s is required", field)
	case n > max:
		c.bad("%s is longer than %d characters", field, max)
	}
}

// existing checks that id names a project; empty is fine when optional.
func (c *checker) existing(w *World, field, id string, optional bool) *projects.Project {
	if id == "" {
		if !optional {
			c.bad("%s is required", field)
		}
		return nil
	}
	p := w.project(id)
	if p == nil {
		c.bad("%s: there is no project %q in projects.yaml", field, id)
	}
	return p
}

// PagePath checks a vault-relative page path and adds ".md" when missing.
// It refuses absolute paths, "..", backslashes, hidden folders and the
// Boards folder, which only the board code writes.
func PagePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	switch {
	case p == "":
		return "", errors.New("the page path is required")
	case len(p) > MaxPagePath:
		return "", fmt.Errorf("the page path is longer than %d characters", MaxPagePath)
	case strings.ContainsAny(p, "\\:*?\"<>|\x00"):
		return "", errors.New("the page path may only use / between folders, and no \\ : * ? \" < > |")
	case strings.HasPrefix(p, "/"):
		return "", errors.New("the page path must be relative to the vault")
	}
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "" || seg == "." || seg == "..":
			return "", errors.New("the page path has an empty, . or .. part")
		case strings.HasPrefix(seg, "."):
			return "", errors.New("the page path may not name a hidden folder or file")
		}
	}
	if path.Clean(p) != p {
		return "", errors.New("the page path is not clean")
	}
	if strings.EqualFold(strings.SplitN(p, "/", 2)[0], boards.Dir) {
		return "", errors.New("pages under Boards/ are boards; add a card instead")
	}
	if !strings.EqualFold(path.Ext(p), ".md") {
		p += ".md"
	}
	return p, nil
}

// freePage returns dir/<slug>.md, or with -2, -3… when that page exists.
func freePage(w *World, dir, title string) string {
	base := dir + "/" + Slug(title)
	p := base + ".md"
	for i := 2; w.PageExists != nil && w.PageExists(p) && i < 100; i++ {
		p = fmt.Sprintf("%s-%d.md", base, i)
	}
	return p
}

func pageRequest(p, title string, front map[string]any, body string) Request {
	return Request{Method: "PUT", Path: "/api/memory/page?path=" + url.QueryEscape(p), Body: map[string]any{"title": title, "front": front, "body": body}}
}

func cardRequest(c map[string]any) Request {
	return Request{Method: "POST", Path: "/api/boards/" + boards.DefaultBoard + "/cards", Body: c}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Validate checks one action against the catalog and the world and returns
// it as a proposal. allowed limits the kinds (a bot's allowed actions); nil
// allows the whole catalog. A proposal that is not valid carries problems
// and no requests.
func Validate(a Action, w *World, allowed []string) Proposal {
	p := Proposal{Action: a.Action, Args: map[string]any{}, Problems: []string{}, Status: StatusPending}
	if !known(a.Action) {
		p.Summary = "Unknown action"
		p.Problems = append(p.Problems, fmt.Sprintf("%q is not an action Lucidbench knows (it knows %s)", a.Action, strings.Join(Kinds(), ", ")))
		return p
	}
	if allowed != nil && !slices.Contains(allowed, a.Action) {
		p.Summary = a.Action
		p.Problems = append(p.Problems, fmt.Sprintf("this bot may not propose %s", a.Action))
		return p
	}
	c := &checker{}
	switch a.Action {
	case ActCreateCard:
		var x cardArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit create_card: %v", err)
			break
		}
		x.Title, x.Project, x.Column, x.Body = oneLine(x.Title), strings.TrimSpace(x.Project), oneLine(x.Column), strings.TrimSpace(x.Body)
		c.text("title", x.Title, 1, MaxTitle)
		c.text("body", x.Body, 0, MaxBody)
		c.existing(w, "project", x.Project, true)
		if x.Column == "" {
			x.Column = w.columns()[0]
		} else if i := slices.IndexFunc(w.columns(), func(s string) bool { return strings.EqualFold(s, x.Column) }); i >= 0 {
			x.Column = w.columns()[i]
		} else {
			c.bad("column %q is not on the work board (it has %s)", x.Column, strings.Join(w.columns(), ", "))
		}
		p.Summary = "Add card “" + x.Title + "”"
		p.Args = map[string]any{"project": x.Project, "title": x.Title, "body": x.Body, "column": x.Column}
		if len(c.problems) > 0 {
			break
		}
		card := map[string]any{"title": x.Title, "column": x.Column}
		if x.Project != "" {
			card["project"] = x.Project
		}
		if x.Body != "" {
			// A card has no body; it links a page that holds it.
			pg := freePage(w, "Inbox", x.Title)
			front := map[string]any{"type": "card"}
			if x.Project != "" {
				front["project"] = x.Project
			}
			p.Requests = append(p.Requests, pageRequest(pg, x.Title, front, x.Body+"\n"))
			card["memory"] = pg
			p.Page = pg
		}
		p.Requests = append(p.Requests, cardRequest(card))
	case ActCreateIdea:
		var x ideaArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit create_idea: %v", err)
			break
		}
		x.Title, x.Project, x.Body = oneLine(x.Title), strings.TrimSpace(x.Project), strings.TrimSpace(x.Body)
		c.text("title", x.Title, 1, MaxTitle)
		c.text("body", x.Body, 0, MaxBody)
		c.existing(w, "project", x.Project, true)
		p.Summary = "Note idea “" + x.Title + "”"
		p.Args = map[string]any{"title": x.Title, "body": x.Body, "project": x.Project}
		if len(c.problems) > 0 {
			break
		}
		pg := freePage(w, "Inbox", x.Title)
		front := map[string]any{"type": "idea"}
		card := map[string]any{"title": x.Title, "column": w.columns()[0], "memory": pg, "labels": []string{"idea"}}
		if x.Project != "" {
			front["project"] = x.Project
			card["project"] = x.Project
		}
		body := x.Body
		if body == "" {
			body = x.Title
		}
		p.Page = pg
		p.Requests = []Request{pageRequest(pg, x.Title, front, body+"\n"), cardRequest(card)}
	case ActCreatePage:
		var x pageArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit create_page: %v", err)
			break
		}
		pg, err := PagePath(x.Path)
		if err != nil {
			c.bad("%v", err)
			pg = strings.TrimSpace(x.Path)
		}
		c.text("markdown", strings.TrimSpace(x.Markdown), 1, MaxPage)
		if err == nil && w.PageExists != nil && w.PageExists(pg) {
			c.bad("%s already exists; Lucidbench never overwrites a page from a proposal", pg)
		}
		if err == nil && w.PageExists == nil {
			c.bad("there is no Memory vault to write to")
		}
		p.Summary = "Write page " + pg
		p.Args = map[string]any{"path": pg, "markdown": x.Markdown}
		if len(c.problems) == 0 {
			title := strings.TrimSuffix(path.Base(pg), path.Ext(pg))
			p.Requests = []Request{pageRequest(pg, title, map[string]any{}, x.Markdown)}
		}
	case ActStartCouncil:
		var x councilArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit start_council: %v", err)
			break
		}
		x.Braindump, x.Project = strings.TrimSpace(x.Braindump), strings.TrimSpace(x.Project)
		c.text("braindump", x.Braindump, 1, MaxPrompt)
		if pr := c.existing(w, "project", x.Project, true); pr != nil && pr.Visibility == "confidential" {
			c.bad("%s is confidential; the council never sends it to a provider", x.Project)
		}
		p.Spends = true
		p.Summary = "Start a council on “" + firstLine(x.Braindump, 80) + "”"
		p.Args = map[string]any{"braindump": x.Braindump, "project": x.Project}
		if len(c.problems) == 0 {
			body := map[string]any{"input": x.Braindump}
			if x.Project != "" {
				body["project"] = x.Project
			}
			p.Requests = []Request{{Method: "POST", Path: "/api/council/sessions", Body: body}}
		}
	case ActStartWork:
		var x workArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit start_work: %v", err)
			break
		}
		x.Project, x.Prompt, x.Provider, x.Model = strings.TrimSpace(x.Project), strings.TrimSpace(x.Prompt), strings.TrimSpace(x.Provider), strings.TrimSpace(x.Model)
		c.text("prompt", x.Prompt, 1, MaxPrompt)
		if pr := c.existing(w, "project", x.Project, false); pr != nil {
			if pr.Visibility == "confidential" {
				c.bad("%s is confidential; Work never sends it to a provider", x.Project)
			} else if pr.LocalPath == "" {
				c.bad("%s has no local_path in projects.yaml, so Work has no checkout to use", x.Project)
			}
		}
		if x.Provider != "" && !slices.Contains(Providers, x.Provider) {
			c.bad("provider must be claude, codex or grok")
		}
		if x.Model != "" && !modelRE.MatchString(x.Model) {
			c.bad("model %q is not a model name", x.Model)
		}
		p.Spends = true
		p.Summary = "Start work on " + x.Project + ": “" + firstLine(x.Prompt, 80) + "”"
		p.Args = map[string]any{"project": x.Project, "prompt": x.Prompt, "provider": x.Provider, "model": x.Model}
		if len(c.problems) == 0 {
			body := map[string]any{"project": x.Project, "prompt": x.Prompt, "provider": x.Provider}
			if x.Model != "" {
				body["model"] = x.Model
			}
			p.Requests = []Request{{Method: "POST", Path: "/api/work/sessions", Body: body}}
		}
	case ActCreateProject:
		var x projectArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit create_project: %v", err)
			break
		}
		x.ID, x.Name, x.Kind, x.LocalPath = strings.TrimSpace(x.ID), oneLine(x.Name), strings.TrimSpace(x.Kind), strings.TrimSpace(x.LocalPath)
		switch {
		case x.ID == "" || len(x.ID) > MaxProjectID || !projectIDRE.MatchString(x.ID):
			c.bad("id must be 1-%d lowercase letters, digits and dashes", MaxProjectID)
		case w.project(x.ID) != nil:
			c.bad("there is already a project %q", x.ID)
		}
		c.text("name", x.Name, 1, MaxName)
		if x.Kind == "" {
			x.Kind = "experiment"
		}
		if !slices.Contains(projects.Categories, x.Kind) {
			c.bad("kind must be one of %s", strings.Join(projects.Categories, ", "))
		}
		if x.LocalPath != "" && (!isAbs(x.LocalPath) || strings.Contains(x.LocalPath, "..")) {
			c.bad("local_path must be an absolute path with no ..")
		}
		p.Summary = "Add project " + x.ID
		p.Args = map[string]any{"id": x.ID, "name": x.Name, "kind": x.Kind, "local_path": x.LocalPath}
		if len(c.problems) > 0 {
			break
		}
		p.Snippet = projectSnippet(x)
		if x.LocalPath != "" {
			// Setup's append: backed up, proven by parsing, never a rewrite.
			p.Requests = []Request{{Method: "POST", Path: "/api/setup/projects", Body: map[string]any{
				"entries": []map[string]any{{"id": x.ID, "name": x.Name, "local_path": x.LocalPath, "category": x.Kind}},
			}}}
		}
	case ActAddNeeds:
		var x needsArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit add_needs: %v", err)
			break
		}
		x.Project = strings.TrimSpace(x.Project)
		c.existing(w, "project", x.Project, false)
		if len(x.Needs) == 0 || len(x.Needs) > MaxNeeds {
			c.bad("needs must list 1 to %d items", MaxNeeds)
		}
		var ns []map[string]any
		for i := range x.Needs {
			n := &x.Needs[i]
			n.What, n.From = oneLine(n.What), strings.TrimSpace(n.From)
			c.text(fmt.Sprintf("needs[%d].what", i), n.What, 1, MaxNeed)
			c.existing(w, fmt.Sprintf("needs[%d].from", i), n.From, true)
			m := map[string]any{"what": n.What}
			if n.From != "" {
				m["from"] = n.From
			}
			ns = append(ns, m)
		}
		p.Summary = fmt.Sprintf("Add %d %s to %s", len(x.Needs), map[bool]string{true: "need", false: "needs"}[len(x.Needs) == 1], x.Project)
		p.Args = map[string]any{"project": x.Project, "needs": ns}
		if len(c.problems) == 0 {
			var b strings.Builder
			fmt.Fprintf(&b, "# projects.yaml, under the project with id: %s\n    needs:\n", x.Project)
			for _, n := range x.Needs {
				fmt.Fprintf(&b, "      - what: %s\n", quote(n.What))
				if n.From != "" {
					fmt.Fprintf(&b, "        from: %s\n", n.From)
				}
				b.WriteString("        status: todo\n")
			}
			p.Snippet = b.String()
		}
	case ActLinkBuildsInto:
		var x linkArgs
		if err := decodeArgs(a.Args, &x); err != nil {
			c.bad("the arguments do not fit link_builds_into: %v", err)
			break
		}
		x.Project, x.Target = strings.TrimSpace(x.Project), strings.TrimSpace(x.Target)
		pr := c.existing(w, "project", x.Project, false)
		c.existing(w, "target", x.Target, false)
		switch {
		case x.Project != "" && x.Project == x.Target:
			c.bad("a project cannot build into itself")
		case pr != nil && slices.Contains(pr.BuildsInto, x.Target):
			c.bad("%s already builds into %s", x.Project, x.Target)
		}
		p.Summary = x.Project + " builds into " + x.Target
		p.Args = map[string]any{"project": x.Project, "target": x.Target}
		if len(c.problems) == 0 {
			p.Snippet = fmt.Sprintf("# projects.yaml, under the project with id: %s\n    builds_into: [%s]\n", x.Project, strings.Join(append(append([]string{}, pr.BuildsInto...), x.Target), ", "))
		}
	}
	p.Problems = append(p.Problems, c.problems...)
	p.Valid = len(p.Problems) == 0
	if !p.Valid {
		p.Requests, p.Snippet = nil, ""
	}
	return p
}

// isAbs accepts an absolute path on any OS: projects.yaml may name a
// Windows checkout while the daemon's tests run elsewhere.
func isAbs(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

func projectSnippet(x projectArgs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  - id: %s\n    name: %s\n    category: %s\n    status: active\n    visibility: private\n", x.ID, quote(x.Name), x.Kind)
	if x.LocalPath != "" {
		fmt.Fprintf(&b, "    local_path: %s\n", quote(x.LocalPath))
	}
	return b.String()
}

func firstLine(s string, n int) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	l = oneLine(l)
	if len([]rune(l)) > n {
		l = string([]rune(l)[:n]) + "…"
	}
	return l
}

// ParseActions finds the actions block in a model answer: a fenced block
// tagged lucid-actions holding {"actions": [...]}. It returns the answer's
// prose without the block. A block that is not valid JSON is an error the
// caller shows; no block means no actions.
func ParseActions(answer string) (string, []Action, error) {
	const open = "```lucid-actions"
	i := strings.Index(answer, open)
	if i < 0 {
		return strings.TrimSpace(answer), nil, nil
	}
	rest := answer[i+len(open):]
	j := strings.Index(rest, "```")
	if j < 0 {
		return strings.TrimSpace(answer[:i]), nil, errors.New("the actions block is not closed")
	}
	block := rest[:j]
	prose := strings.TrimSpace(answer[:i] + "\n" + rest[j+3:])
	if len(block) > 64<<10 {
		return prose, nil, errors.New("the actions block is larger than 64 KB")
	}
	var doc struct {
		Actions []Action `json:"actions"`
	}
	dec := json.NewDecoder(strings.NewReader(block))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return prose, nil, fmt.Errorf("the actions block is not valid JSON: %v", err)
	}
	if len(doc.Actions) > MaxActions {
		return prose, doc.Actions[:MaxActions], fmt.Errorf("the answer proposed %d actions; only the first %d are shown", len(doc.Actions), MaxActions)
	}
	return prose, doc.Actions, nil
}
