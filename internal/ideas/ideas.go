// Package ideas follows one idea through the whole loop: the braindump the
// council turned into a brief, the card that brief became, the Work sessions
// that ran on the card and the pull requests they opened.
//
// Nothing is stored here. Every idea is derived on request from the council
// records, the boards and the Work sessions, joined on their links: a card's
// council:: key names the council session, and a Work session names its card.
// An idea from the council has the council session's id; a card made by hand
// has the id "card:<board>/<card id>".
//
// Pieces may be missing (a deleted page, a card removed from its board, a
// session folder that is gone): the idea is still shown, with what is left,
// and says what could not be read.
package ideas

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// Stages, in order: how far along the loop an idea has come.
const (
	StageBraindump = "braindump"
	StageBrief     = "brief"
	StageCard      = "card"
	StageWork      = "work"
	StagePR        = "pr"
	StageMerged    = "merged"
)

// Stages lists every stage in order.
var Stages = []string{StageBraindump, StageBrief, StageCard, StageWork, StagePR, StageMerged}

// CardPrefix starts the id of an idea that is a hand-made card.
const CardPrefix = "card:"

// ErrNotFound is an idea id that matches nothing.
var ErrNotFound = errors.New("not found")

// Summary is one row of the Ideas list.
type Summary struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Stage   string    `json:"stage"`
	Status  string    `json:"status"`
	Project string    `json:"project,omitempty"`
	Created time.Time `json:"created,omitzero"`
	Updated time.Time `json:"updated,omitzero"`
	// CostUSD sums the costs the CLIs reported, council and Work together.
	CostUSD float64 `json:"cost_usd"`
	// Council is the council session id; Card is "<board>/<card id>".
	Council  string `json:"council,omitempty"`
	Card     string `json:"card,omitempty"`
	Sessions int    `json:"sessions"`
	PR       string `json:"pr_url,omitempty"`
	PRState  string `json:"pr_state,omitempty"`
}

// Brief is the brief page.
type Brief struct {
	Path         string `json:"path"`
	Title        string `json:"title,omitempty"`
	Status       string `json:"status,omitempty"` // draft | approved, from its front matter
	Body         string `json:"body,omitempty"`   // Markdown
	Exists       bool   `json:"exists"`
	Confidential bool   `json:"confidential,omitempty"`
}

// Move is one known move of the card.
type Move struct {
	Time   time.Time `json:"time"`
	Column string    `json:"column"`
	By     string    `json:"by"` // what moved it: "council approval", "work session <id>", …
}

// CardView is the card, where it is now and how it got there.
type CardView struct {
	boards.Card
	Board      string `json:"board"`
	BoardTitle string `json:"board_title"`
	// History is rebuilt from the council and Work records: the board file
	// keeps no history, so moves made by hand are not in it. HistoryNote
	// says so.
	History     []Move `json:"history"`
	HistoryNote string `json:"history_note"`
}

// SessionView is a Work session without its bulk.
type SessionView struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	Status    string           `json:"status"`
	Provider  string           `json:"provider"`
	Model     string           `json:"model,omitempty"`
	Branch    string           `json:"branch"`
	Started   time.Time        `json:"started"`
	Ended     *time.Time       `json:"ended,omitempty"`
	Stat      string           `json:"diff_stat,omitempty"`
	Added     int              `json:"added"`
	Deleted   int              `json:"deleted"`
	Files     int              `json:"files"`
	Commits   []work.Commit    `json:"commits"`
	CostUSD   float64          `json:"cost_usd"`
	PR        string           `json:"pr_url,omitempty"`
	PRState   string           `json:"pr_state,omitempty"`
	PRChecks  *work.PRChecks   `json:"pr_checks,omitempty"`
	PRChecked time.Time        `json:"pr_checked,omitzero"`
	Usage     *agentexec.Usage `json:"usage,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// Event is one entry of the idea's timeline. An Untimed event happened after
// the one before it, but its time is not recorded.
type Event struct {
	Time    time.Time `json:"time"`
	Untimed bool      `json:"untimed,omitempty"`
	Kind    string    `json:"kind"` // council | critique | brief | approve | card | work | pr | merged | checks
	Title   string    `json:"title"`
	Detail  string    `json:"detail,omitempty"`
	// Provider is the model's CLI, for council steps and Work sessions.
	Provider string `json:"provider,omitempty"`
	// Link is the app page the event belongs to: "council/<id>",
	// "work/<id>", "memory/<path>", "boards/<board>" or a PR URL.
	Link string `json:"link,omitempty"`
}

// Cost is what one provider cost in one part of the loop.
type Cost struct {
	Provider     string  `json:"provider"`
	Part         string  `json:"part"` // council | work
	CostUSD      float64 `json:"cost_usd"`
	Calls        int     `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
}

// Idea is the whole structure of one idea.
type Idea struct {
	Summary
	Input    string           `json:"input,omitempty"` // the braindump
	Session  *council.Session `json:"council_session,omitempty"`
	Brief    *Brief           `json:"brief,omitempty"`
	CardView *CardView        `json:"card_view,omitempty"`
	Work     []SessionView    `json:"work"`
	// Links are the Memory pages that link to the brief.
	Links    []string `json:"links"`
	Timeline []Event  `json:"timeline"`
	Costs    []Cost   `json:"costs"`
	// Missing lists the pieces that could not be read.
	Missing []string `json:"missing"`
}

// Service derives ideas from the council, the boards and Work.
type Service struct {
	Council *council.Service
	Work    *work.Service
	Vault   memory.Opener
}

// cardRef is a card and the board it is on.
type cardRef struct {
	board, boardTitle string
	card              boards.Card
}

func (c cardRef) key() string { return c.board + "/" + c.card.ID }

// world is everything read once for one request.
type world struct {
	vault    *memory.Vault
	cards    []cardRef
	sessions []work.Session
	missing  []string
}

// load reads the boards and the Work sessions. Boards are only read, never
// created: a vault without a work board has no cards.
func (s *Service) load() *world {
	w := &world{}
	if s.Vault != nil {
		v, err := s.Vault()
		if err != nil {
			w.missing = append(w.missing, "the Memory vault cannot be opened: "+err.Error())
		} else {
			w.vault = v
			list, err := boards.List(v)
			if err != nil {
				w.missing = append(w.missing, "the boards cannot be read: "+err.Error())
			}
			for _, sum := range list {
				b, err := boards.Get(v, sum.ID)
				if err != nil {
					continue
				}
				for _, c := range b.Cards {
					w.cards = append(w.cards, cardRef{board: b.ID, boardTitle: b.Title, card: c})
				}
			}
		}
	}
	if s.Work != nil {
		w.sessions = s.Work.List()
	}
	return w
}

// sessionsFor are the Work sessions on a card, oldest first: those that name
// the card, and the one the card's work:: key names.
func (w *world) sessionsFor(c cardRef) []work.Session {
	var out []work.Session
	for _, se := range w.sessions {
		board := se.Board
		if board == "" {
			board = boards.DefaultBoard
		}
		if (se.Card == c.card.ID && board == c.board) || (c.card.Work != "" && c.card.Work == se.ID) {
			out = append(out, se)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// cardForCouncil finds the card a council session became.
func (w *world) cardForCouncil(sess *council.Session) *cardRef {
	for i := range w.cards {
		if w.cards[i].card.Council == sess.ID {
			return &w.cards[i]
		}
	}
	if sess.Card != nil {
		for i := range w.cards {
			if w.cards[i].board == boards.DefaultBoard && w.cards[i].card.ID == sess.Card.ID {
				return &w.cards[i]
			}
		}
	}
	return nil
}

// List returns every idea, newest first.
func (s *Service) List() ([]Summary, error) {
	w := s.load()
	out := []Summary{}
	linked := map[string]bool{}
	if s.Council != nil {
		list, err := s.Council.List()
		if err != nil {
			return nil, err
		}
		for _, sum := range list {
			sess, err := s.Council.Get(sum.ID)
			if err != nil {
				continue
			}
			c := w.cardForCouncil(sess)
			if c != nil {
				linked[c.key()] = true
			}
			out = append(out, summarise(sess, c, w.sessionsFor0(c)))
		}
	}
	for _, c := range w.cards {
		if linked[c.key()] {
			continue
		}
		out = append(out, summarise(nil, &c, w.sessionsFor(c)))
	}
	sort.SliceStable(out, func(i, j int) bool { return newest(out[i]).After(newest(out[j])) })
	return out, nil
}

func (w *world) sessionsFor0(c *cardRef) []work.Session {
	if c == nil {
		return nil
	}
	return w.sessionsFor(*c)
}

func newest(s Summary) time.Time {
	if s.Updated.After(s.Created) {
		return s.Updated
	}
	return s.Created
}

// stageOf is how far the idea has come.
func stageOf(sess *council.Session, c *cardRef, ses []work.Session) string {
	for _, se := range ses {
		if se.PRState == work.PRMerged {
			return StageMerged
		}
	}
	for _, se := range ses {
		if se.PR != "" {
			return StagePR
		}
	}
	switch {
	case len(ses) > 0:
		return StageWork
	case c != nil || (sess != nil && sess.Card != nil):
		return StageCard
	case sess != nil && sess.BriefPath != "":
		return StageBrief
	}
	return StageBraindump
}

// latestPR is the newest session's PR, preferring a merged one.
func latestPR(ses []work.Session) (string, string) {
	url, state := "", ""
	for _, se := range ses {
		if se.PR == "" {
			continue
		}
		if se.PRState == work.PRMerged {
			return se.PR, se.PRState
		}
		url, state = se.PR, se.PRState
	}
	return url, state
}

func statusOf(stage string, sess *council.Session, c *cardRef, ses []work.Session) string {
	var last *work.Session
	if len(ses) > 0 {
		last = &ses[len(ses)-1]
	}
	switch stage {
	case StageMerged:
		return "Merged"
	case StagePR:
		_, state := latestPR(ses)
		switch state {
		case work.PRDraft:
			return "Draft PR"
		case work.PROpen:
			return "PR open"
		case work.PRClosed:
			return "PR closed"
		}
		return "PR opened"
	case StageWork:
		switch last.Status {
		case work.StatusRunning:
			return "Agent working"
		case work.StatusDone:
			return "Diff to review"
		case work.StatusStopped:
			return "Session stopped"
		}
		return "Session failed"
	case StageCard:
		if c == nil {
			return "Card missing"
		}
		return "In " + c.card.Column
	case StageBrief:
		if sess.Status == council.StatusRunning {
			return "Council revising"
		}
		return "Waiting for approval"
	}
	if sess != nil && sess.Status == council.StatusFailed {
		return "Council failed"
	}
	return "Council running"
}

func summarise(sess *council.Session, c *cardRef, ses []work.Session) Summary {
	out := Summary{Sessions: len(ses)}
	if sess != nil {
		out.ID, out.Council, out.Project = sess.ID, sess.ID, sess.Project
		out.Title = sess.Title
		if out.Title == "" {
			out.Title = council.BriefTitle(sess.Brief)
		}
		if out.Title == "" {
			out.Title = excerpt(strings.Join(strings.Fields(sess.Input), " "), 80)
		}
		out.Created, out.Updated = sess.Created, sess.Updated
		for _, u := range sess.Usage {
			out.CostUSD += u.CostUSD
		}
	}
	if c != nil {
		out.Card = c.key()
		if sess == nil {
			out.ID = CardPrefix + c.key()
			out.Title, out.Project = c.card.Title, c.card.Project
		}
		if out.Project == "" {
			out.Project = c.card.Project
		}
	}
	for _, se := range ses {
		if se.Usage != nil {
			out.CostUSD += se.Usage.CostUSD
		}
		if out.Created.IsZero() || se.Started.Before(out.Created) {
			out.Created = se.Started
		}
		for _, t := range []time.Time{se.Started, deref(se.Ended), se.PRChecked} {
			if t.After(out.Updated) {
				out.Updated = t
			}
		}
	}
	out.PR, out.PRState = latestPR(ses)
	out.Stage = stageOf(sess, c, ses)
	out.Status = statusOf(out.Stage, sess, c, ses)
	return out
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func excerpt(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Get returns the whole structure of one idea.
func (s *Service) Get(id string) (*Idea, error) {
	w := s.load()
	var sess *council.Session
	var c *cardRef
	if key, ok := strings.CutPrefix(id, CardPrefix); ok {
		for i := range w.cards {
			if w.cards[i].key() == key {
				c = &w.cards[i]
			}
		}
		if c == nil {
			return nil, fmt.Errorf("%w: no card %s", ErrNotFound, key)
		}
		// A card the council made is that council's idea: a link that only
		// knows the card lands on the whole idea.
		if c.card.Council != "" && s.Council != nil {
			if got, err := s.Council.Get(c.card.Council); err == nil {
				sess = got
			}
		}
	} else {
		if s.Council == nil {
			return nil, fmt.Errorf("%w: no idea %q", ErrNotFound, id)
		}
		got, err := s.Council.Get(id)
		if err != nil {
			if errors.Is(err, council.ErrNotFound) {
				return nil, fmt.Errorf("%w: no idea %q", ErrNotFound, id)
			}
			return nil, err
		}
		sess = got
		c = w.cardForCouncil(sess)
		if c == nil && sess.Card != nil {
			w.missing = append(w.missing, fmt.Sprintf("the card %q this brief became is no longer on the work board", sess.Card.Title))
		}
	}
	ses := w.sessionsFor0(c)
	idea := &Idea{Summary: summarise(sess, c, ses), Session: sess, Work: []SessionView{}, Links: []string{}, Timeline: []Event{}, Costs: []Cost{}}
	if sess != nil {
		idea.Input = sess.Input
	}
	idea.Brief = w.brief(sess, c)
	if idea.Brief != nil && idea.Brief.Exists && w.vault != nil {
		if links, err := w.vault.Backlinks(idea.Brief.Path); err == nil {
			for _, l := range links {
				if !strings.HasPrefix(l, boards.Dir+"/") {
					idea.Links = append(idea.Links, l)
				}
			}
		}
	} else if idea.Brief != nil {
		w.missing = append(w.missing, "the brief page "+idea.Brief.Path+" is gone")
	}
	for _, se := range ses {
		idea.Work = append(idea.Work, view(se))
	}
	if c != nil {
		idea.CardView = cardView(sess, c, ses)
	}
	idea.Timeline = timeline(sess, c, ses, idea.Brief)
	idea.Costs = costs(sess, ses)
	idea.Missing = append([]string{}, w.missing...)
	return idea, nil
}

func (w *world) brief(sess *council.Session, c *cardRef) *Brief {
	p := ""
	if sess != nil {
		p = sess.BriefPath
	}
	if p == "" && c != nil {
		p = c.card.Memory
	}
	if p == "" {
		return nil
	}
	b := &Brief{Path: p}
	if sess != nil {
		b.Title, b.Body = sess.Title, sess.Brief
	}
	if w.vault == nil {
		return b
	}
	pg, err := w.vault.Read(p)
	if err != nil {
		return b
	}
	b.Exists, b.Title, b.Body, b.Confidential = true, pg.Title, pg.Body, pg.Confidential
	if st, ok := pg.Front["status"].(string); ok {
		b.Status = st
	}
	return b
}

func view(se work.Session) SessionView {
	v := SessionView{
		ID: se.ID, Title: se.Title, Status: se.Status, Provider: se.Provider, Branch: se.Branch,
		Started: se.Started, Ended: se.Ended, Commits: []work.Commit{}, PR: se.PR, PRState: se.PRState,
		PRChecks: se.PRChecks, PRChecked: se.PRChecked, Usage: se.Usage, Error: se.Error,
	}
	if se.Usage != nil {
		v.CostUSD, v.Model = se.Usage.CostUSD, se.Usage.Model
	}
	if d := se.Diff; d != nil {
		v.Stat, v.Added, v.Deleted, v.Files = d.Stat, d.Added, d.Deleted, len(d.Files)
		if d.Commits != nil {
			v.Commits = d.Commits
		}
	}
	return v
}

// approvedAt is when the council's approval put the card in Ready.
func approvedAt(sess *council.Session) time.Time {
	if sess == nil {
		return time.Time{}
	}
	for _, e := range sess.Log {
		if e.Kind == "done" && strings.HasPrefix(e.Text, "Approved") {
			return e.Time
		}
	}
	return time.Time{}
}

func cardView(sess *council.Session, c *cardRef, ses []work.Session) *CardView {
	v := &CardView{Card: c.card, Board: c.board, BoardTitle: c.boardTitle, History: []Move{}}
	if t := approvedAt(sess); !t.IsZero() {
		v.History = append(v.History, Move{Time: t, Column: "Ready", By: "council approval"})
	}
	for _, se := range ses {
		by := "work session " + se.ID
		v.History = append(v.History, Move{Time: se.Started, Column: work.ColumnInProgress, By: by})
		if se.Status == work.StatusDone && se.Ended != nil {
			v.History = append(v.History, Move{Time: *se.Ended, Column: work.ColumnReview, By: by})
		}
		if se.CardDone && !se.PRChecked.IsZero() {
			v.History = append(v.History, Move{Time: se.PRChecked, Column: work.ColumnDone, By: "the PR was seen merged"})
		}
	}
	sort.SliceStable(v.History, func(i, j int) bool { return v.History[i].Time.Before(v.History[j].Time) })
	if len(v.History) == 0 {
		v.HistoryNote = "The board does not record moves, and no council or Work record moved this card: only where it is now is known."
	} else {
		v.HistoryNote = "Rebuilt from the council and Work records; moves made by hand on the board are not recorded."
	}
	return v
}

func stepEvent(st *council.Step, round int) (Event, bool) {
	if st == nil || st.Running || st.Ended.IsZero() {
		return Event{}, false
	}
	e := Event{Time: st.Ended, Provider: st.Provider}
	switch st.Role {
	case council.RolePropose:
		e.Kind, e.Title = "council", "First draft of the brief"
	case council.RoleSynthesise:
		e.Kind, e.Title = "council", fmt.Sprintf("Brief revised after round %d", round)
	default:
		e.Kind = "critique"
		switch {
		case st.Skipped:
			e.Title = "Critic skipped"
			e.Detail = st.Error
		case st.Error != "":
			e.Title = "Critique failed"
			e.Detail = st.Error
		default:
			e.Title = fmt.Sprintf("Critique, round %d: %s", round, st.Verdict)
			e.Detail = fmt.Sprintf("%d %s", len(st.Points), map[bool]string{true: "point", false: "points"}[len(st.Points) == 1])
		}
	}
	return e, true
}

func timeline(sess *council.Session, c *cardRef, ses []work.Session, brief *Brief) []Event {
	out := []Event{}
	if sess != nil {
		link := "council/" + sess.ID
		out = append(out, Event{Time: sess.Created, Kind: "council", Title: "Braindump sent to the council", Link: link})
		for _, r := range sess.Rounds {
			steps := append([]*council.Step{r.Proposal}, r.Critiques...)
			steps = append(steps, r.Synthesis)
			for _, st := range steps {
				if e, ok := stepEvent(st, r.N); ok {
					e.Link = link
					out = append(out, e)
				}
			}
		}
		writes := 0
		for _, e := range sess.Log {
			if e.Kind != "done" {
				continue
			}
			title := "Brief written to Memory"
			if !strings.HasPrefix(e.Text, "Approved") {
				// Each Ask again writes the same page again.
				if writes++; writes > 1 {
					title = "Brief rewritten after Ask again"
				}
			}
			ev := Event{Time: e.Time, Kind: "brief", Title: title, Link: link}
			if brief != nil && brief.Path != "" {
				ev.Detail, ev.Link = brief.Path, "memory/"+brief.Path
			}
			if strings.HasPrefix(e.Text, "Approved") {
				ev = Event{Time: e.Time, Kind: "approve", Title: "Brief approved", Detail: e.Text, Link: link}
			}
			out = append(out, ev)
		}
		if sess.Status == council.StatusFailed {
			out = append(out, Event{Time: sess.Updated, Kind: "council", Title: "The council failed", Detail: sess.Error, Link: link})
		}
	}
	for _, se := range ses {
		link := "work/" + se.ID
		out = append(out, Event{Time: se.Started, Kind: "work", Title: "Work session started", Detail: se.Branch, Provider: se.Provider, Link: link})
		end := se.Started
		if se.Ended != nil {
			end = *se.Ended
			detail := se.Error
			if se.Diff != nil {
				detail = fmt.Sprintf("+%d −%d in %d %s, %d %s", se.Diff.Added, se.Diff.Deleted, len(se.Diff.Files),
					map[bool]string{true: "file", false: "files"}[len(se.Diff.Files) == 1], len(se.Diff.Commits),
					map[bool]string{true: "commit", false: "commits"}[len(se.Diff.Commits) == 1])
			}
			out = append(out, Event{Time: end, Kind: "work", Title: "Work session " + se.Status, Detail: detail, Provider: se.Provider, Link: link})
		}
		if se.PR != "" {
			// The time a PR was opened is not recorded: it follows the run.
			out = append(out, Event{Time: end, Untimed: true, Kind: "pr", Title: "Draft PR opened", Detail: se.PR, Link: se.PR})
		}
		if !se.PRChecked.IsZero() && se.PRState != "" {
			if se.PRState == work.PRMerged {
				out = append(out, Event{Time: se.PRChecked, Kind: "merged", Title: "PR seen merged", Detail: "first seen merged by Work's PR check", Link: se.PR})
			} else if ch := se.PRChecks; ch != nil {
				out = append(out, Event{Time: se.PRChecked, Kind: "checks", Title: "PR " + se.PRState + ", checks read",
					Detail: fmt.Sprintf("%d passing, %d failing, %d pending", ch.Passing, ch.Failing, ch.Pending), Link: se.PR})
			}
		}
	}
	if sess == nil && c != nil && len(out) == 0 {
		out = append(out, Event{Kind: "card", Untimed: true, Title: "Card made by hand in " + c.card.Column, Link: "boards/" + c.board})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

func costs(sess *council.Session, ses []work.Session) []Cost {
	by := map[[2]string]*Cost{}
	add := func(part string, u agentexec.Usage) {
		k := [2]string{u.Provider, part}
		c := by[k]
		if c == nil {
			c = &Cost{Provider: u.Provider, Part: part}
			by[k] = c
		}
		c.Calls++
		c.CostUSD += u.CostUSD
		c.InputTokens += u.InputTokens + u.CacheRead + u.CacheWrite
		c.OutputTokens += u.OutputTokens
	}
	if sess != nil {
		for _, u := range sess.Usage {
			add("council", u)
		}
	}
	for _, se := range ses {
		if se.Usage != nil {
			u := *se.Usage
			if u.Provider == "" {
				u.Provider = se.Provider
			}
			add("work", u)
		}
	}
	out := []Cost{}
	for _, c := range by {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		return out[i].Provider+out[i].Part < out[j].Provider+out[j].Part
	})
	return out
}
