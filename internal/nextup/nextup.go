// Package nextup works out what to work on next. It gathers candidates from
// everything Lucidbench already knows (board cards, council briefs, Work
// sessions and their pull requests, failing CI, assigned Linear issues and
// Trello cards, and project needs), scores each one with a deterministic,
// explained formula, and gives it one primary action.
//
// Listing is free: it reads local records and the cached CI, Linear and
// Trello answers, and never runs a provider. Asking an agent to rank the list
// is a separate, opt-in call (see rank.go) that spends on the user's own
// account; confidential items reach it as an id and a score only.
//
// Nothing here starts work. An action is a request the UI sends after the
// user confirms it.
package nextup

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// Kinds of candidate.
const (
	KindCard    = "card"    // a board card in a to-do or doing column
	KindBrief   = "brief"   // a council brief: waiting for approval, approved without a card, or a council that failed
	KindSession = "session" // a Work session waiting for a reply, or failed
	KindPR      = "pr"      // a pull request a Work session opened
	KindCI      = "ci"      // CI failing on a repository's default branch
	KindLinear  = "linear"  // a Linear issue assigned to the user
	KindTrello  = "trello"  // a Trello card assigned to the user
	KindNeed    = "need"    // a project need that is not done
)

// Action kinds. Only start_work, fix_ci and run_council spend; each of them
// goes through the UI's confirm dialog. The others open a page or a link.
const (
	ActStartWork   = "start_work"
	ActFixCI       = "fix_ci"
	ActRunCouncil  = "run_council"
	ActResumeWork  = "resume_work"
	ActOpenSession = "open_session"
	ActOpenPR      = "open_pr"
	ActOpenCard    = "open_card"
	ActOpenBrief   = "open_brief"
	ActOpenLink    = "open_link"
	ActOpenProject = "open_project"
)

// ActionKinds lists every action kind, for validating an agent's suggestion.
var ActionKinds = []string{ActStartWork, ActFixCI, ActRunCouncil, ActResumeWork, ActOpenSession, ActOpenPR, ActOpenCard, ActOpenBrief, ActOpenLink, ActOpenProject}

// Link is where a candidate comes from: an in-app route or an outside URL.
type Link struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Route string `json:"route,omitempty"`
	URL   string `json:"url,omitempty"`
}

// Part is one term of a score: its factor, its points and why.
type Part struct {
	Factor string `json:"factor"` // urgency | unblocking | staleness | focus | effort | blocked | headroom | set_aside
	Points int    `json:"points"`
	Why    string `json:"why"`
}

// WorkRequest is the body the UI posts to /api/work/sessions once the user
// confirms. Provider and model come from the project's team builder, else
// the first installed CLI.
type WorkRequest struct {
	Card     string `json:"card,omitempty"`
	Project  string `json:"project"`
	Prompt   string `json:"prompt,omitempty"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Profile  string `json:"profile,omitempty"`
}

// CouncilRequest is the body the UI posts to /api/council/sessions.
type CouncilRequest struct {
	Input   string `json:"input"`
	Project string `json:"project,omitempty"`
}

// Action is a candidate's one primary action.
type Action struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Spends is true when the action runs an agent on the user's account.
	Spends bool `json:"spends"`
	// Explain says what happens if the user takes the action.
	Explain string `json:"explain"`
	// Route is an in-app path to open; URL an outside link.
	Route   string          `json:"route,omitempty"`
	URL     string          `json:"url,omitempty"`
	Work    *WorkRequest    `json:"work,omitempty"`
	Council *CouncilRequest `json:"council,omitempty"`
	// Note says why a spending action was not offered (a confidential project,
	// no local_path).
	Note string `json:"note,omitempty"`
}

// AgentPick is what the ranking agent said about a candidate.
type AgentPick struct {
	Rank            int    `json:"rank"`
	Reason          string `json:"reason,omitempty"`
	SuggestedAction string `json:"suggested_action,omitempty"`
	SuggestedPrompt string `json:"suggested_prompt,omitempty"`
}

// Candidate is one piece of work that could be next.
type Candidate struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Source       string     `json:"source"` // boards | council | work | github | linear | trello | projects
	Title        string     `json:"title"`
	Context      string     `json:"context,omitempty"`
	Project      string     `json:"project,omitempty"`
	ProjectName  string     `json:"project_name,omitempty"`
	Confidential bool       `json:"confidential"`
	Due          string     `json:"due,omitempty"`
	Labels       []string   `json:"labels,omitempty"`
	Updated      time.Time  `json:"updated,omitzero"`
	Link         Link       `json:"link"`
	Score        int        `json:"score"`
	Parts        []Part     `json:"parts"`
	Action       Action     `json:"action"`
	Agent        *AgentPick `json:"agent,omitempty"`

	sig signals
}

// signals are the facts the score is computed from, set by the collector.
type signals struct {
	ciFailing, prFailing, prReview, prDraft bool
	waiting, failed                         bool
	briefDraft, briefReady, approvedNoCard  bool
	councilFailed                           bool
	doing, blocked                          bool
	linearPriority                          int
	needStatus                              string
	unblocks                                []string // project names this work unblocks (needs)
	feeds                                   []string // project names it builds into
	provider                                string   // the provider a spending action would use
}

// Hidden is a snoozed or set-aside candidate, so it can be brought back.
type Hidden struct {
	ID     string    `json:"id"`
	Title  string    `json:"title"`
	Kind   string    `json:"kind"`
	Until  time.Time `json:"until,omitzero"`
	Reason string    `json:"reason,omitempty"`
}

// SourceError is a source that could not be read; the rest still show.
type SourceError struct {
	Source string `json:"source"`
	Error  string `json:"error"`
}

// RankInfo describes the last agent ranking still applied.
type RankInfo struct {
	At            time.Time `json:"at"`
	Provider      string    `json:"provider"`
	Model         string    `json:"model,omitempty"`
	PromptVersion string    `json:"prompt_version"`
	CostUSD       float64   `json:"cost_usd"`
	Ranked        int       `json:"ranked"`
}

// View is the body of GET /api/nextup.
type View struct {
	Items       []Candidate   `json:"items"`
	Hidden      []Hidden      `json:"hidden"`
	Errors      []SourceError `json:"errors"`
	Settings    Settings      `json:"settings"`
	Ranked      *RankInfo     `json:"ranked,omitempty"`
	GeneratedAt time.Time     `json:"generated_at"`
}

// Errors the HTTP layer maps to statuses.
var (
	ErrBadRequest = errors.New("bad request")
	ErrNotFound   = errors.New("not found")
)

// Service lists, ranks and remembers.
type Service struct {
	Sources Sources
	Store   *Store
	Ranker  *Ranker
	Now     func() time.Time
	// Timeout bounds every source of one listing; 0 means DefaultTimeout.
	Timeout time.Duration
}

// DefaultTimeout is how long a listing waits for its slowest source.
const DefaultTimeout = 6 * time.Second

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// candidates collects and scores every candidate, hidden ones included.
func (s *Service) candidates(ctx context.Context, st State) ([]Candidate, []SourceError, *world) {
	t := s.Timeout
	if t <= 0 {
		t = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, t)
	defer cancel()
	now := s.now()
	w := s.Sources.gather(ctx)
	cs := w.collect(now)
	ScoreAll(cs, Context{Now: now, Settings: st.Settings, Dismissed: st.Dismissed, Usage: w.usage})
	return cs, w.errs, w
}

// List returns the visible candidates, best first, with the hidden ones.
func (s *Service) List(ctx context.Context) View {
	st := s.Store.Load()
	cs, errs, _ := s.candidates(ctx, st)
	return s.view(cs, errs, st)
}

func (s *Service) view(cs []Candidate, errs []SourceError, st State) View {
	now := s.now()
	v := View{Items: []Candidate{}, Hidden: []Hidden{}, Errors: errs, Settings: st.Settings, GeneratedAt: now.UTC()}
	if v.Errors == nil {
		v.Errors = []SourceError{}
	}
	for _, c := range cs {
		if until, ok := st.Snoozed[c.ID]; ok && until.After(now) {
			v.Hidden = append(v.Hidden, Hidden{ID: c.ID, Title: c.Title, Kind: c.Kind, Until: until})
			continue
		}
		if d, ok := st.Dismissed[c.ID]; ok {
			v.Hidden = append(v.Hidden, Hidden{ID: c.ID, Title: c.Title, Kind: c.Kind, Reason: d.Reason})
			continue
		}
		v.Items = append(v.Items, c)
	}
	if r := st.LastRank; r != nil {
		applied := ApplyRanking(v.Items, r.Items)
		if applied > 0 {
			v.Ranked = &RankInfo{At: r.At, Provider: r.Provider, Model: r.Model, PromptVersion: r.PromptVersion, CostUSD: r.Usage.CostUSD, Ranked: applied}
		}
	}
	return v
}

// ApplyRanking orders items by an agent's ranking: the ranked ids that are
// still listed first, in the agent's order, then the rest by score. It
// returns how many ranked items were found.
func ApplyRanking(items []Candidate, ranked []RankedItem) int {
	pos := map[string]int{}
	for i, r := range ranked {
		if _, dup := pos[r.ID]; !dup {
			pos[r.ID] = i
		}
	}
	found := 0
	for i := range items {
		if p, ok := pos[items[i].ID]; ok {
			r := ranked[p]
			items[i].Agent = &AgentPick{Rank: p + 1, Reason: r.Reason, SuggestedAction: r.SuggestedAction, SuggestedPrompt: r.SuggestedPrompt}
			if r.SuggestedPrompt != "" && !items[i].Confidential && items[i].Action.Work != nil {
				w := *items[i].Action.Work
				w.Prompt = r.SuggestedPrompt
				items[i].Action.Work = &w
			}
			found++
		}
	}
	sort.SliceStable(items, func(a, b int) bool {
		pa, oka := pos[items[a].ID]
		pb, okb := pos[items[b].ID]
		switch {
		case oka && okb:
			return pa < pb
		case oka != okb:
			return oka
		}
		return less(items[a], items[b])
	})
	return found
}

// less is the deterministic order: score, then id.
func less(a, b Candidate) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.ID < b.ID
}

// Find returns one candidate by id, hidden or not.
func (s *Service) Find(ctx context.Context, id string) (*Candidate, error) {
	st := s.Store.Load()
	cs, _, _ := s.candidates(ctx, st)
	for i := range cs {
		if cs[i].ID == id {
			return &cs[i], nil
		}
	}
	return nil, ErrNotFound
}

// oneLine collapses whitespace.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// excerpt cuts s to n runes on one line.
func excerpt(s string, n int) string {
	s = oneLine(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
