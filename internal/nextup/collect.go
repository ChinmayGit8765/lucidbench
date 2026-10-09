package nextup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/linear"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/trello"
	"github.com/ChinmayGit8765/lucidbench/internal/usage"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// Brief is a council session as Next up reads it.
type Brief struct {
	ID        string
	Title     string
	Input     string
	Project   string
	Status    string // running | draft | approved | failed
	BriefPath string
	Card      string // the card the approval added, "" when none
	Blockers  int    // blockers in the latest critiques
	Updated   time.Time
}

// FailingRun is the latest failed workflow run on a repository's default branch.
type FailingRun struct {
	Repo       string
	Workflow   string
	Title      string
	Branch     string
	Conclusion string
	RunNumber  int
	URL        string
	At         time.Time
}

// Sources are the readers Next up collects from. A nil source is skipped;
// Linear and Trello are nil when they are not connected.
type Sources struct {
	Projects func() (*projects.List, error)
	Boards   func() ([]boards.Board, error)
	Briefs   func() ([]Brief, error)
	Work     func() []work.Session
	CI       func(ctx context.Context) ([]FailingRun, error)
	Linear   func(ctx context.Context) ([]linear.Issue, error)
	Trello   func(ctx context.Context) ([]trello.Card, error)
	Usage    func() *usage.Summary
	// Builder is a project's team builder (nil without a team).
	Builder func(project string) (*work.Builder, error)
	// PageConfidential reports whether a Memory page is confidential; a page
	// that cannot be checked should count as confidential.
	PageConfidential func(path string) bool
	// LookPath finds an installed CLI when a project has no builder.
	LookPath func(string) (string, error)
}

// world is what one listing read.
type world struct {
	src      *Sources
	list     *projects.List
	listErr  error
	boards   []boards.Board
	boardsOK bool
	briefs   []Brief
	sessions []work.Session
	failing  []FailingRun
	issues   []linear.Issue
	tcards   []trello.Card
	usage    *usage.Summary
	errs     []SourceError
}

type result[T any] struct {
	v   T
	err error
}

// async runs f in the background and returns where its answer will arrive.
func async[T any](ctx context.Context, f func(context.Context) (T, error)) <-chan result[T] {
	ch := make(chan result[T], 1)
	go func() {
		v, err := f(ctx)
		ch <- result[T]{v, err}
	}()
	return ch
}

// wait takes an answer, or a timeout error once ctx is done.
func wait[T any](ctx context.Context, ch <-chan result[T]) (T, error) {
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		return zero, errors.New("did not answer in time")
	}
}

// gather reads every source at once, each bounded by ctx.
func (s Sources) gather(ctx context.Context) *world {
	w := &world{src: &s}
	fail := func(source string, err error) {
		if err != nil {
			w.errs = append(w.errs, SourceError{Source: source, Error: excerpt(err.Error(), 200)})
		}
	}
	var (
		pCh = async(ctx, func(context.Context) (*projects.List, error) {
			if s.Projects == nil {
				return nil, errors.New("no project list")
			}
			return s.Projects()
		})
		bCh  <-chan result[[]boards.Board]
		cCh  <-chan result[[]Brief]
		wCh  <-chan result[[]work.Session]
		ciCh <-chan result[[]FailingRun]
		lCh  <-chan result[[]linear.Issue]
		tCh  <-chan result[[]trello.Card]
		uCh  <-chan result[*usage.Summary]
	)
	if s.Boards != nil {
		bCh = async(ctx, func(context.Context) ([]boards.Board, error) { return s.Boards() })
	}
	if s.Briefs != nil {
		cCh = async(ctx, func(context.Context) ([]Brief, error) { return s.Briefs() })
	}
	if s.Work != nil {
		wCh = async(ctx, func(context.Context) ([]work.Session, error) { return s.Work(), nil })
	}
	if s.CI != nil {
		ciCh = async(ctx, s.CI)
	}
	if s.Linear != nil {
		lCh = async(ctx, s.Linear)
	}
	if s.Trello != nil {
		tCh = async(ctx, s.Trello)
	}
	if s.Usage != nil {
		uCh = async(ctx, func(context.Context) (*usage.Summary, error) { return s.Usage(), nil })
	}
	w.list, w.listErr = wait(ctx, pCh)
	if w.listErr == nil && w.list == nil {
		w.listErr = errors.New("no project list")
	}
	if s.Projects != nil {
		fail("projects", w.listErr)
	}
	if bCh != nil {
		var err error
		w.boards, err = wait(ctx, bCh)
		w.boardsOK = err == nil
		fail("boards", err)
	}
	if cCh != nil {
		var err error
		w.briefs, err = wait(ctx, cCh)
		fail("council", err)
	}
	if wCh != nil {
		var err error
		w.sessions, err = wait(ctx, wCh)
		fail("work", err)
	}
	if ciCh != nil {
		var err error
		w.failing, err = wait(ctx, ciCh)
		fail("ci", err)
	}
	if lCh != nil {
		var err error
		w.issues, err = wait(ctx, lCh)
		fail("linear", err)
	}
	if tCh != nil {
		var err error
		w.tcards, err = wait(ctx, tCh)
		fail("trello", err)
	}
	if uCh != nil {
		w.usage, _ = wait(ctx, uCh) // headroom is a hint; without it nothing is demoted
	}
	return w
}

// project returns a project by id, or nil.
func (w *world) project(id string) *projects.Project {
	if w.list == nil || id == "" {
		return nil
	}
	return w.list.Find(id)
}

func (w *world) projectName(id string) string {
	if p := w.project(id); p != nil && p.Name != "" {
		return p.Name
	}
	return id
}

// confidential reports whether a project is confidential. A project that
// cannot be checked (no list, or not in it) counts as confidential.
func (w *world) confidential(id string) bool {
	if id == "" {
		return false
	}
	if w.listErr != nil || w.list == nil {
		return true
	}
	p := w.list.Find(id)
	return p == nil || p.Visibility == "confidential"
}

func (w *world) pageConfidential(path string) bool {
	if path == "" {
		return false
	}
	if w.src.PageConfidential == nil {
		return true
	}
	return w.src.PageConfidential(path)
}

// unblocks lists the other projects with an unmet need supplied by id.
func (w *world) unblocks(id string) []string {
	if w.list == nil || id == "" {
		return nil
	}
	var out []string
	for _, q := range w.list.Projects {
		if q.ID == id {
			continue
		}
		for _, n := range q.Needs {
			if n.From == id && n.Status != "done" {
				out = append(out, q.Name)
				break
			}
		}
	}
	return out
}

// feeds lists the live projects id builds into.
func (w *world) feeds(id string) []string {
	p := w.project(id)
	if p == nil {
		return nil
	}
	var out []string
	for _, t := range p.BuildsInto {
		if q := w.project(t); q != nil && live(q.Status) {
			out = append(out, q.Name)
		}
	}
	return out
}

// live reports whether a project status is still being worked on.
func live(status string) bool {
	switch status {
	case "archived", "shipped", "frozen":
		return false
	}
	return true
}

// Providers is the order CLIs are tried in when a project has no builder.
var Providers = []string{"claude", "codex", "grok"}

// workAction is "start a Work session" on a project, or the fallback when
// Work cannot run there.
func (w *world) workAction(kind, project, card, prompt string, fallback Action) (Action, string) {
	p := w.project(project)
	switch {
	case project == "":
		fallback.Note = "No project: link it to one to start an agent from here."
		return fallback, ""
	case p == nil:
		fallback.Note = fmt.Sprintf("There is no project %q in projects.yaml.", project)
		return fallback, ""
	case w.confidential(project):
		fallback.Note = p.Name + " is confidential, so no agent may work on it."
		return fallback, ""
	case p.LocalPath == "":
		fallback.Note = p.Name + " has no local_path, so Work cannot make a worktree."
		return fallback, ""
	}
	req := &WorkRequest{Card: card, Project: project, Prompt: prompt}
	if w.src.Builder != nil {
		if b, err := w.src.Builder(project); err == nil && b != nil {
			req.Provider, req.Model, req.Profile = b.Provider, b.Model, b.Profile
		}
	}
	if req.Provider == "" {
		look := w.src.LookPath
		if look == nil {
			look = exec.LookPath
		}
		for _, pr := range Providers {
			if _, err := look(pr); err == nil {
				req.Provider = pr
				break
			}
		}
	}
	if req.Provider == "" {
		fallback.Note = "No provider CLI is installed (claude, codex or grok)."
		return fallback, ""
	}
	label := "Start work"
	if kind == ActFixCI {
		label = "Fix CI"
	}
	who := req.Provider
	if req.Model != "" {
		who += " (" + req.Model + ")"
	}
	return Action{
		Kind: kind, Label: label, Spends: true, Work: req,
		Explain: fmt.Sprintf("Starts %s in a new worktree of %s, on your own account. You confirm first; nothing is pushed until you open a PR.", who, p.Name),
	}, req.Provider
}

// boardRoute is a card's place in the app.
func boardRoute(board, card string) string {
	if card == "" {
		return "/boards/" + board
	}
	return "/boards/" + board + "/" + card
}

// column classes.
var (
	todoColumns  = []string{"inbox", "ready", "to do", "todo", "backlog", "next", "up next"}
	doingColumns = []string{"in progress", "doing", "wip", "in-progress"}
)

func columnClass(col string) string {
	c := strings.ToLower(strings.TrimSpace(col))
	for _, x := range doingColumns {
		if c == x {
			return "doing"
		}
	}
	for _, x := range todoColumns {
		if c == x {
			return "todo"
		}
	}
	return ""
}

func hasLabel(labels []string, names ...string) bool {
	for _, l := range labels {
		l = strings.ToLower(strings.TrimSpace(l))
		for _, n := range names {
			if l == n {
				return true
			}
		}
	}
	return false
}

func workRef(board, card string) string {
	if board == boards.DefaultBoard {
		return card
	}
	return board + "/" + card
}

// collect turns what was read into candidates, unscored.
func (w *world) collect(now time.Time) []Candidate {
	var out []Candidate
	// Sessions that are live cover their card.
	busyCards := map[string]bool{}
	for _, se := range w.sessions {
		if se.Status == work.StatusRunning || se.Status == work.StatusWaiting {
			if se.Card != "" {
				b := se.Board
				if b == "" {
					b = boards.DefaultBoard
				}
				busyCards[b+"/"+se.Card] = true
			}
			busyCards["work:"+se.ID] = true
		}
	}
	linked := map[string]bool{} // Linear identifiers and Trello ids already on a board
	cardIDs := map[string]bool{}
	for _, b := range w.boards {
		for _, c := range b.Cards {
			cardIDs[c.ID] = true
			cardIDs[b.ID+"/"+c.ID] = true
			if c.Linear != "" {
				linked["linear:"+strings.ToUpper(c.Linear)] = true
			}
			if c.Trello != "" {
				linked["trello:"+c.Trello] = true
			}
		}
	}

	// Board cards in a to-do or doing column.
	for _, b := range w.boards {
		for _, c := range b.Cards {
			class := columnClass(c.Column)
			if c.Done || class == "" || busyCards[b.ID+"/"+c.ID] || (c.Work != "" && busyCards["work:"+c.Work]) {
				continue
			}
			cand := Candidate{
				ID: "card:" + b.ID + "/" + c.ID, Kind: KindCard, Source: "boards", Title: c.Title,
				Project: c.Project, ProjectName: w.projectName(c.Project), Due: c.Due, Labels: c.Labels,
				Confidential: w.confidential(c.Project) || w.pageConfidential(c.Memory),
				Link:         Link{Kind: "card", Label: "Card on " + b.Title, Route: boardRoute(b.ID, c.ID)},
			}
			cand.Context = c.Column + " · " + b.Title + " board"
			if len(c.Labels) > 0 {
				cand.Context += " · " + strings.Join(c.Labels, ", ")
			}
			cand.sig.doing = class == "doing"
			cand.sig.blocked = hasLabel(c.Labels, "blocked")
			cand.sig.unblocks, cand.sig.feeds = w.unblocks(c.Project), w.feeds(c.Project)
			open := Action{Kind: ActOpenCard, Label: "Open card", Route: cand.Link.Route, Explain: "Opens the card on its board."}
			cand.Action, cand.sig.provider = w.workAction(ActStartWork, c.Project, workRef(b.ID, c.ID), "", open)
			if cand.Action.Kind == ActStartWork {
				cand.Action.Explain += " The agent gets the card's brief as its task."
			}
			out = append(out, cand)
		}
	}

	// Council briefs.
	for _, br := range w.briefs {
		title := br.Title
		if title == "" {
			title = excerpt(br.Input, 80)
		}
		cand := Candidate{
			ID: "brief:" + br.ID, Kind: KindBrief, Source: "council", Project: br.Project, ProjectName: w.projectName(br.Project),
			Updated:      br.Updated,
			Confidential: w.confidential(br.Project) || w.pageConfidential(br.BriefPath),
			Link:         Link{Kind: "council", Label: "Council session", Route: "/council/" + br.ID},
		}
		cand.sig.unblocks, cand.sig.feeds = w.unblocks(br.Project), w.feeds(br.Project)
		open := Action{Kind: ActOpenBrief, Label: "Review brief", Route: cand.Link.Route, Explain: "Opens the brief in Council, where you approve it or ask again."}
		switch {
		case br.Status == "draft" && br.BriefPath != "":
			cand.Title = "Approve the brief: " + title
			cand.sig.briefDraft, cand.sig.briefReady = true, br.Blockers == 0
			if br.Blockers == 0 {
				cand.Context = "Draft brief with no open blockers: ready to approve"
			} else {
				cand.Context = fmt.Sprintf("Draft brief with %d open blocker(s)", br.Blockers)
			}
			cand.Action = open
		case br.Status == "approved" && (br.Card == "" || (w.boardsOK && !cardIDs[br.Card] && !cardIDs[boards.DefaultBoard+"/"+br.Card])):
			cand.Title = "Approved brief without a card: " + title
			cand.Context = "The brief was approved but no card is on a board for it"
			cand.sig.approvedNoCard = true
			cand.Action = open
		case br.Status == "failed" && br.BriefPath == "":
			cand.Title = "Run Council again: " + title
			cand.Context = "The council did not finish a brief for this braindump"
			cand.sig.councilFailed = true
			cand.Action = open
			if !cand.Confidential && strings.TrimSpace(br.Input) != "" {
				cand.Action = Action{Kind: ActRunCouncil, Label: "Run Council", Spends: true, Council: &CouncilRequest{Input: br.Input, Project: br.Project},
					Explain: "Runs the council again on the same braindump, on your own accounts. You confirm first."}
				cand.sig.provider = "claude"
			}
		default:
			continue
		}
		out = append(out, cand)
	}

	// Work sessions and their pull requests.
	for _, se := range w.sessions {
		conf := w.confidential(se.Project) || w.pageConfidential(se.Brief)
		updated := se.Started
		if se.Ended != nil {
			updated = *se.Ended
		}
		route := "/work/" + se.ID
		base := Candidate{
			Kind: KindSession, Source: "work", Project: se.Project, ProjectName: w.projectName(se.Project), Confidential: conf, Updated: updated,
			Link: Link{Kind: "work", Label: "Work session", Route: route},
		}
		base.sig.unblocks, base.sig.feeds = w.unblocks(se.Project), w.feeds(se.Project)
		switch se.Status {
		case work.StatusWaiting:
			c := base
			c.ID, c.Title = "work:"+se.ID, "Reply to the agent: "+se.Title
			c.Context = se.Provider + " finished a turn and waits for your follow-up"
			c.sig.waiting = true
			c.Action = Action{Kind: ActResumeWork, Label: "Reply", Route: route, Explain: "Opens the session; your reply runs the next turn in the same worktree once you send it."}
			out = append(out, c)
		case work.StatusFailed:
			c := base
			c.ID, c.Title = "work:"+se.ID, "Session failed: "+se.Title
			c.Context = excerpt(se.Error, 160)
			if c.Context == "" {
				c.Context = se.Provider + " stopped with an error"
			}
			c.sig.failed = true
			c.Action = Action{Kind: ActOpenSession, Label: "Review", Route: route, Explain: "Opens the session to read what went wrong."}
			out = append(out, c)
		}
		if se.PR == "" || se.PRState == work.PRMerged || se.PRState == work.PRClosed {
			continue
		}
		c := base
		c.ID, c.Kind, c.Source = "pr:"+se.ID, KindPR, "github"
		c.Link = Link{Kind: "pr", Label: "Pull request", URL: se.PR}
		c.Action = Action{Kind: ActOpenPR, Label: "Open PR", URL: se.PR, Explain: "Opens the pull request on GitHub."}
		switch {
		case se.PRChecks != nil && se.PRChecks.Failing > 0:
			c.Title, c.sig.prFailing = "PR checks failing: "+se.Title, true
			c.Context = fmt.Sprintf("%d failing, %d passing, %d pending", se.PRChecks.Failing, se.PRChecks.Passing, se.PRChecks.Pending)
		case se.PRState == work.PRDraft:
			c.Title, c.sig.prDraft = "Draft PR: "+se.Title, true
			c.Context = "A draft: mark it ready when the diff is right"
		default:
			c.Title, c.sig.prReview = "PR waiting on review: "+se.Title, true
			c.Context = "Open and waiting for a review"
		}
		out = append(out, c)
	}

	// CI failing on a default branch.
	for _, r := range w.failing {
		proj := w.projectForRepo(r.Repo)
		cand := Candidate{
			ID: "ci:" + r.Repo, Kind: KindCI, Source: "github", Title: "CI failing on " + r.Branch + " in " + r.Repo,
			Project: proj, ProjectName: w.projectName(proj), Updated: r.At,
			Confidential: w.confidential(proj) || w.repoConfidential(r.Repo),
			Link:         Link{Kind: "ci", Label: "Workflow run", URL: r.URL},
		}
		cand.Context = fmt.Sprintf("%s run #%d: %s", r.Workflow, r.RunNumber, r.Conclusion)
		if r.Title != "" {
			cand.Context += " · " + excerpt(r.Title, 80)
		}
		cand.sig.ciFailing = true
		cand.sig.unblocks, cand.sig.feeds = w.unblocks(proj), w.feeds(proj)
		open := Action{Kind: ActOpenLink, Label: "Open run", URL: r.URL, Explain: "Opens the failing run on GitHub."}
		cand.Action, cand.sig.provider = w.workAction(ActFixCI, proj, "", FixCIPrompt(r), open)
		out = append(out, cand)
	}

	// Linear issues assigned to the user.
	for _, is := range w.issues {
		if linked["linear:"+strings.ToUpper(is.Identifier)] {
			continue
		}
		cand := Candidate{
			ID: "linear:" + is.Identifier, Kind: KindLinear, Source: "linear", Title: is.Identifier + " " + is.Title,
			Link:   Link{Kind: "linear", Label: "Linear issue", URL: is.URL},
			Action: Action{Kind: ActOpenLink, Label: "Open in Linear", URL: is.URL, Explain: "Opens the issue in Linear."},
		}
		parts := []string{is.State.Name, is.TeamKey}
		if is.PriorityLabel != "" {
			parts = append(parts, is.PriorityLabel)
		}
		cand.Context = strings.Join(nonEmpty(parts), " · ")
		cand.sig.linearPriority = is.Priority
		if t, err := time.Parse(time.RFC3339, is.UpdatedAt); err == nil {
			cand.Updated = t
		}
		for _, l := range is.Labels {
			cand.Labels = append(cand.Labels, l.Name)
		}
		cand.sig.blocked = hasLabel(cand.Labels, "blocked")
		out = append(out, cand)
	}

	// Trello cards assigned to the user.
	for _, tc := range w.tcards {
		if linked["trello:"+tc.ID] {
			continue
		}
		cand := Candidate{
			ID: "trello:" + tc.ID, Kind: KindTrello, Source: "trello", Title: tc.Name, Due: tc.Due,
			Link:   Link{Kind: "trello", Label: "Trello card", URL: tc.URL},
			Action: Action{Kind: ActOpenLink, Label: "Open in Trello", URL: tc.URL, Explain: "Opens the card in Trello."},
		}
		for _, l := range tc.Labels {
			if l.Name != "" {
				cand.Labels = append(cand.Labels, l.Name)
			}
		}
		cand.Context = "Assigned to you on Trello"
		cand.sig.blocked = hasLabel(cand.Labels, "blocked")
		out = append(out, cand)
	}

	// Project needs that are not done.
	if w.list != nil {
		for _, p := range w.list.Projects {
			if !live(p.Status) {
				continue
			}
			for i, n := range p.Needs {
				if n.Status == "done" {
					continue
				}
				owner := p.ID
				title := p.Name + " needs " + n.What
				if n.From != "" && w.project(n.From) != nil {
					owner = n.From
					title += " from " + w.projectName(n.From)
				}
				cand := Candidate{
					ID: fmt.Sprintf("need:%s/%d", p.ID, i), Kind: KindNeed, Source: "projects", Title: title,
					Project: owner, ProjectName: w.projectName(owner),
					Confidential: w.confidential(p.ID) || w.confidential(owner),
					Context:      "Need is " + n.Status,
					Link:         Link{Kind: "project", Label: "Project", Route: "/projects/" + p.ID},
				}
				if owner != p.ID {
					cand.sig.unblocks = []string{p.Name}
				}
				cand.sig.needStatus = n.Status
				cand.sig.blocked = n.Status == "blocked"
				cand.sig.doing = n.Status == "doing"
				cand.sig.feeds = w.feeds(owner)
				open := Action{Kind: ActOpenProject, Label: "Open project", Route: cand.Link.Route, Explain: "Opens the project and its needs."}
				prompt := NeedPrompt(p.Name, n, w.projectName(owner), owner != p.ID)
				cand.Action, cand.sig.provider = w.workAction(ActStartWork, owner, "", prompt, open)
				out = append(out, cand)
			}
		}
	}
	return out
}

func nonEmpty(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// normRepo makes owner/name, a GitHub URL and a .git suffix compare equal.
func normRepo(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "github.com/")
	return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
}

// projectForRepo is the project whose repo is r, or "".
func (w *world) projectForRepo(r string) string {
	if w.list == nil {
		return ""
	}
	want := normRepo(r)
	for _, p := range w.list.Projects {
		if p.Repo != "" && normRepo(p.Repo) == want {
			return p.ID
		}
	}
	return ""
}

// repoConfidential reports whether a confidential project owns repo r.
// Without a project list every repo counts as confidential.
func (w *world) repoConfidential(r string) bool {
	if w.listErr != nil || w.list == nil {
		return true
	}
	want := normRepo(r)
	for _, p := range w.list.Projects {
		if p.Visibility == "confidential" && p.Repo != "" && normRepo(p.Repo) == want {
			return true
		}
	}
	return false
}

// FixCIPrompt is the task a "Fix CI" session starts with. GitHub's log is
// not fetched, so the prompt says what is known and asks the agent to
// reproduce the failure.
func FixCIPrompt(r FailingRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CI is failing on %s in %s.\n\n", r.Branch, r.Repo)
	fmt.Fprintf(&b, "- Workflow: %s, run #%d, conclusion %s", r.Workflow, r.RunNumber, r.Conclusion)
	if !r.At.IsZero() {
		fmt.Fprintf(&b, ", %s", r.At.UTC().Format("2006-01-02 15:04 UTC"))
	}
	b.WriteString("\n")
	if r.Title != "" {
		fmt.Fprintf(&b, "- Commit: %s\n", oneLine(r.Title))
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "- Run: %s\n", r.URL)
	}
	b.WriteString("\nThe run's log is not included. Reproduce the failure locally with the same checks the workflow runs, find the cause, fix it, and run the checks again before you commit.")
	return b.String()
}

// NeedPrompt is the task for working on a project need.
func NeedPrompt(project string, n projects.Need, owner string, supplied bool) string {
	if supplied {
		return fmt.Sprintf("%s needs %s from %s, and it is %s.\n\nDo the part that belongs in this repository (%s) so %s can move on. Say what %s still has to do on its side.", project, n.What, owner, n.Status, owner, project, project)
	}
	return fmt.Sprintf("%s needs %s, and it is %s.\n\nWork on it in this repository. Say what is left when you stop.", project, n.What, n.Status)
}
