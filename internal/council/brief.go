package council

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// Sections are the brief's headings, in order.
var Sections = []string{"Problem", "Outcome", "Done criteria", "Risks", "First steps", "Open questions"}

// InboxDir is the vault folder briefs are written to.
const InboxDir = "Inbox"

func projectBlock(project string) string {
	if project == "" {
		return ""
	}
	return "\n\nThe project this is for: " + project
}

func notesBlock(notes string) string {
	if notes == "" {
		return ""
	}
	return "\n\nNotes from the user (binding):\n" + notes
}

func proposePrompt(input, project string) string {
	return "The braindump:\n" + input + projectBlock(project)
}

func critiquePrompt(input, project, draft, notes string, self bool) string {
	var b strings.Builder
	if self {
		b.WriteString("You wrote this draft yourself. No other critic is available, so be as strict as an outside reviewer would be.\n\n")
	}
	b.WriteString("The braindump:\n" + input + projectBlock(project))
	b.WriteString("\n\nThe draft brief:\n" + draft)
	b.WriteString(notesBlock(notes))
	return b.String()
}

func synthesisePrompt(input, project, draft string, critiques []*Step, notes string) string {
	var b strings.Builder
	b.WriteString("The braindump:\n" + input + projectBlock(project))
	b.WriteString("\n\nYour current brief:\n" + draft)
	b.WriteString("\n\nThe critics' points:")
	any := false
	for _, st := range critiques {
		if st.Error != "" || st.Verdict == "" {
			continue
		}
		any = true
		who := st.Provider
		if st.Role == RoleSelfCritique {
			who += " (your own critique)"
		}
		fmt.Fprintf(&b, "\n\n%s, verdict %s:", who, st.Verdict)
		if len(st.Points) == 0 {
			b.WriteString("\n- nothing to change")
		}
		for _, p := range st.Points {
			fmt.Fprintf(&b, "\n- [%s] %s", p.Severity, p.Text)
		}
	}
	if !any {
		b.WriteString("\n(none)")
	}
	b.WriteString(notesBlock(notes))
	return b.String()
}

var fenceRE = regexp.MustCompile("(?s)^```[a-zA-Z]*\\s*\n(.*?)\n?```\\s*$")

// cleanBrief removes a code fence around the whole answer and anything
// before the title.
func cleanBrief(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if m := fenceRE.FindStringSubmatch(text); m != nil {
		text = strings.TrimSpace(m[1])
	}
	if !strings.HasPrefix(text, "# ") {
		if i := strings.Index(text, "\n# "); i >= 0 {
			text = strings.TrimSpace(text[i+1:])
		}
	}
	// A fence opened before the title leaves its closing line behind.
	if strings.HasSuffix(text, "```") && strings.Count(text, "```")%2 == 1 {
		text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	}
	return text
}

// BriefTitle is the brief's "# " heading, or "".
func BriefTitle(brief string) string {
	for _, l := range strings.Split(brief, "\n") {
		if strings.HasPrefix(l, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(l, "# "))
		}
	}
	return ""
}

var (
	headingRE   = regexp.MustCompile(`(?m)^## +(.+?)\s*$`)
	criterionRE = regexp.MustCompile(`^\s*[-*] `)
)

// CheckBrief lists where a brief departs from the format: a missing or
// misplaced section, or a done criterion without a proof.
func CheckBrief(brief string) []string {
	var out []string
	if BriefTitle(brief) == "" {
		out = append(out, "the brief has no title")
	}
	found := map[string]int{}
	locs := headingRE.FindAllStringSubmatchIndex(brief, -1)
	for i, m := range locs {
		found[strings.ToLower(brief[m[2]:m[3]])] = i
	}
	last := -1
	for _, sec := range Sections {
		i, ok := found[strings.ToLower(sec)]
		if !ok {
			out = append(out, "missing section: "+sec)
			continue
		}
		if i < last {
			out = append(out, "section out of order: "+sec)
		}
		last = i
	}
	if i, ok := found["done criteria"]; ok {
		start := locs[i][1]
		end := len(brief)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		n := 0
		for _, l := range strings.Split(brief[start:end], "\n") {
			if !criterionRE.MatchString(l) {
				continue
			}
			n++
			if !strings.Contains(strings.ToLower(l), "proof:") {
				out = append(out, "a done criterion has no proof: "+excerpt(strings.TrimSpace(l), 80))
			}
		}
		if n == 0 {
			out = append(out, "Done criteria lists no criteria")
		}
	}
	return out
}

func excerpt(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseCritique reads a critic's JSON answer, tolerating a code fence or
// text around the object.
func parseCritique(text string) (string, []Point, error) {
	t := strings.TrimSpace(text)
	i, j := strings.Index(t, "{"), strings.LastIndex(t, "}")
	if i < 0 || j <= i {
		return "", nil, errors.New("no JSON object in the answer")
	}
	var raw struct {
		Verdict string  `json:"verdict"`
		Points  []Point `json:"points"`
	}
	if err := json.Unmarshal([]byte(t[i:j+1]), &raw); err != nil {
		return "", nil, err
	}
	points := []Point{}
	worst := ""
	for _, p := range raw.Points {
		p.Text = strings.TrimSpace(p.Text)
		if p.Text == "" {
			continue
		}
		switch sev := strings.ToLower(strings.TrimSpace(p.Severity)); sev {
		case SeverityBlocker, SeverityMajor, SeverityMinor:
			p.Severity = sev
		case "critical":
			p.Severity = SeverityBlocker
		default:
			p.Severity = SeverityMinor
		}
		if p.Severity == SeverityBlocker {
			worst = VerdictBlocker
		} else if worst == "" {
			worst = VerdictConcerns
		}
		points = append(points, p)
	}
	verdict := strings.ToLower(strings.TrimSpace(raw.Verdict))
	switch verdict {
	case VerdictOK, VerdictConcerns, VerdictBlocker:
	default:
		if worst == "" {
			return "", nil, fmt.Errorf("unknown verdict %q", raw.Verdict)
		}
		verdict = worst
	}
	// A blocker point makes the verdict a blocker whatever the label said.
	if worst == VerdictBlocker {
		verdict = VerdictBlocker
	}
	return verdict, points, nil
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slug is a file-name-safe form of a title.
func Slug(title string) string {
	s := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		s = "brief"
	}
	return s
}

// writeBrief saves the brief as a draft page and finishes the session. A
// session that already has a page keeps it.
func (s *Service) writeBrief(sess *Session, onUpdate func(Session), brief string) {
	s.change(sess, onUpdate, func() {
		sess.Stage = StageWrite
		s.logLocked(sess, "stage", 0, "", "Writing the brief to Memory")
	})
	title := BriefTitle(brief)
	if title == "" {
		title = excerpt(oneLine(sess.Input), 60)
	}
	p, err := s.saveBrief(sess, title, brief)
	if err != nil {
		s.fail(sess, onUpdate, "could not write the brief: "+err.Error())
		return
	}
	s.change(sess, onUpdate, func() {
		sess.Brief, sess.BriefPath, sess.Title = brief, p, title
		sess.Warnings = CheckBrief(brief)
		if sess.Warnings == nil {
			sess.Warnings = []string{}
		}
		sess.Status, sess.Stage = StatusDraft, StageDone
		sess.Thinking = []string{}
		s.logLocked(sess, "done", 0, "", "The brief is ready for approval")
	})
}

func (s *Service) saveBrief(sess *Session, title, brief string) (string, error) {
	if s.Vault == nil {
		return "", errors.New("no Memory vault")
	}
	v, err := s.Vault()
	if err != nil {
		return "", err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	front := map[string]any{"type": "brief", "status": StatusDraft, "project": sess.Project, "council": sess.ID}
	if title != "" {
		// The file name is a dated slug; the page shows this instead.
		front["title"] = title
	}
	p := sess.BriefPath
	if p != "" {
		// Asked again: keep the page and any keys the user added to it.
		if pg, err := v.Read(p); err == nil {
			for k, val := range pg.Front {
				if _, ours := front[k]; !ours {
					front[k] = val
				}
			}
		}
	} else {
		base := path.Join(InboxDir, s.now().Format("2006-01-02")+"-"+Slug(title))
		p = base + ".md"
		for i := 2; ; i++ {
			if _, err := v.Read(p); errors.Is(err, memory.ErrNotFound) {
				break
			} else if err != nil {
				return "", err
			}
			p = fmt.Sprintf("%s-%d.md", base, i)
		}
	}
	if err := v.Write(&memory.Page{Path: p, Front: front, Body: brief + "\n"}); err != nil {
		return "", err
	}
	return p, nil
}

// Approve marks the brief approved and adds a card for it to the Ready
// column of the work board. project, when set, replaces the session's
// project. Approving twice returns the same card.
func (s *Service) Approve(id, project string) (*boards.Card, error) {
	sess, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	switch {
	case sess.Status == StatusRunning:
		return nil, ErrBusy
	case sess.Status == StatusApproved && sess.Card != nil:
		return sess.Card, nil
	case sess.Status != StatusDraft || sess.BriefPath == "":
		return nil, fmt.Errorf("%w: this session has no draft brief to approve", ErrBadRequest)
	}
	project = strings.TrimSpace(project)
	if project != "" {
		l, err := s.loadProjects()
		if err != nil {
			return nil, err
		}
		if l.Find(project) == nil {
			return nil, fmt.Errorf("%w: no project %q in projects.yaml", ErrBadRequest, project)
		}
	} else {
		project = sess.Project
	}
	if s.Vault == nil {
		return nil, errors.New("no Memory vault")
	}
	v, err := s.Vault()
	if err != nil {
		return nil, err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	pg, err := v.Read(sess.BriefPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the brief at %s: %w", sess.BriefPath, err)
	}
	before := pg.Front["status"]
	if pg.Front == nil {
		pg.Front = map[string]any{}
	}
	pg.Front["status"] = StatusApproved
	pg.Front["project"] = project
	pg.Title = ""
	if err := v.Write(pg); err != nil {
		return nil, err
	}
	title := sess.Title
	if title == "" {
		title = BriefTitle(sess.Brief)
	}
	card, err := boards.AddCard(v, boards.DefaultBoard, boards.Card{
		Title: title, Column: "Ready", Project: project, Memory: sess.BriefPath, Council: sess.ID,
	})
	if err != nil {
		// Put the page back so it still reads as a draft.
		pg.Front["status"] = before
		_ = v.Write(pg)
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if live := s.live[id]; live != nil {
		return nil, ErrBusy
	}
	cur, err := s.readLocked(id)
	if err != nil {
		return nil, err
	}
	cur.Status, cur.Project, cur.Card = StatusApproved, project, &card
	cur.Updated = s.now()
	cur.Log = append(cur.Log, Event{Time: cur.Updated, Kind: "done", Text: "Approved: a card was added to Ready on the work board"})
	if err := s.persistLocked(cur); err != nil {
		return nil, err
	}
	s.publishLocked(clone(cur))
	return &card, nil
}

// checkConfidential refuses a confidential project, and any [[wikilink]] in
// texts that names a confidential Memory page or folder.
func (s *Service) checkConfidential(project string, texts ...string) error {
	if project != "" {
		l, err := s.loadProjects()
		if err != nil {
			return fmt.Errorf("cannot read projects.yaml to check the project: %w", err)
		}
		p := l.Find(project)
		if p == nil {
			return fmt.Errorf("%w: no project %q in projects.yaml", ErrBadRequest, project)
		}
		if p.Visibility == "confidential" {
			return fmt.Errorf("%w: %s is a confidential project; the council never sends it to a provider", ErrConfidential, p.Name)
		}
	}
	var links []string
	for _, t := range texts {
		links = append(links, memory.Links(t)...)
	}
	if len(links) == 0 {
		return nil
	}
	if s.Vault == nil {
		return fmt.Errorf("%w: the braindump links Memory pages, but there is no vault to check them against", ErrConfidential)
	}
	v, err := s.Vault()
	if err != nil {
		return fmt.Errorf("%w: the braindump links Memory pages, but the vault cannot be opened to check them: %v", ErrConfidential, err)
	}
	for _, l := range links {
		for _, p := range resolveLink(v, l) {
			if conf, err := v.IsConfidential(p); err == nil && conf {
				return fmt.Errorf("%w: [[%s]] is a confidential Memory page; the council never sends it to a provider", ErrConfidential, l)
			}
		}
	}
	return nil
}

// resolveLink returns the vault paths a [[link]] can mean: the page at that
// path, a folder at that path, and every page with that name, as Obsidian
// links by note name.
func resolveLink(v *memory.Vault, link string) []string {
	var out []string
	if _, err := v.Read(link + ".md"); err == nil {
		out = append(out, link+".md")
	}
	if ents, err := v.List(link); err == nil && ents != nil {
		out = append(out, link)
	}
	name := strings.ToLower(path.Base(link))
	root := v.Root()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		base := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
		if strings.ToLower(base) != name {
			return nil
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}
