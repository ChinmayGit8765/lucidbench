package ideas

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

var t0 = time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

type fixture struct {
	svc                *Service
	vault              *memory.Vault
	merged, hand, bare boards.Card
}

func writeJSON(t *testing.T, p string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// newFixture builds four council sessions (merged, waiting, failed early,
// and one whose card and page are gone), two hand-made cards and three Work
// sessions, one of them a free prompt with no card.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	v, err := memory.Open(filepath.Join(root, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{
		"Inbox/merged.md":  "# Ship the merged thing\n\n## Outcome\nShipped.\n",
		"Inbox/waiting.md": "# Waiting brief\n",
		"Notes/plan.md":    "See [[merged]] for the brief.\n",
	} {
		front := map[string]any{"type": "brief", "status": "approved"}
		if strings.HasPrefix(p, "Notes/") {
			front = nil
		}
		if err := v.Write(&memory.Page{Path: p, Front: front, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	add := func(c boards.Card) boards.Card {
		got, err := boards.AddCard(v, boards.DefaultBoard, c)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	f := &fixture{vault: v}
	f.merged = add(boards.Card{Title: "Ship the merged thing", Column: "Done", Project: "demo", Memory: "Inbox/merged.md", Council: "20260501-090000-aaaaaa"})
	f.hand = add(boards.Card{Title: "Hand-made fix", Column: "Review", Project: "demo"})
	f.bare = add(boards.Card{Title: "Someday", Column: "Inbox"})

	cdir := filepath.Join(root, "council")
	step := func(p, role string, end int, verdict string, points int) *council.Step {
		s := &council.Step{Provider: p, Role: role, Started: at(end - 1), Ended: at(end), Verdict: verdict}
		for i := 0; i < points; i++ {
			s.Points = append(s.Points, council.Point{Severity: "minor", Text: "x"})
		}
		return s
	}
	writeJSON(t, filepath.Join(cdir, "20260501-090000-aaaaaa.json"), council.Session{
		ID: "20260501-090000-aaaaaa", Input: "ship it", Project: "demo", Title: "Ship the merged thing", Status: council.StatusApproved,
		BriefPath: "Inbox/merged.md", Brief: "# Ship the merged thing\n", Card: &boards.Card{ID: f.merged.ID},
		Rounds: []council.Round{{N: 1, Proposal: step("claude", council.RolePropose, 1, "", 0),
			Critiques: []*council.Step{step("codex", council.RoleCritique, 2, "concerns", 2), step("grok", council.RoleCritique, 3, "ok", 0)},
			Synthesis: step("claude", council.RoleSynthesise, 4, "", 0)}},
		Log: []council.Event{
			{Time: at(5), Kind: "done", Text: "The brief is ready for approval"},
			{Time: at(10), Kind: "done", Text: "Approved: a card was added to Ready on the work board"},
		},
		Usage:   []agentexec.Usage{{Provider: "claude", CostUSD: 0.10, InputTokens: 100}, {Provider: "codex", CostUSD: 0.05}, {Provider: "claude", CostUSD: 0.02}},
		Created: at(0), Updated: at(10),
	})
	writeJSON(t, filepath.Join(cdir, "20260502-090000-bbbbbb.json"), council.Session{
		ID: "20260502-090000-bbbbbb", Input: "a waiting idea", Status: council.StatusDraft, BriefPath: "Inbox/waiting.md", Brief: "# Waiting brief\n",
		Created: at(60 * 24), Updated: at(60*24 + 5),
	})
	writeJSON(t, filepath.Join(cdir, "20260503-090000-cccccc.json"), council.Session{
		ID: "20260503-090000-cccccc", Input: "a   half formed\nthought", Status: council.StatusFailed, Error: "no provider",
		Created: at(2 * 60 * 24), Updated: at(2 * 60 * 24),
	})
	writeJSON(t, filepath.Join(cdir, "20260504-090000-dddddd.json"), council.Session{
		ID: "20260504-090000-dddddd", Input: "lost", Title: "Lost pieces", Status: council.StatusApproved,
		BriefPath: "Inbox/deleted.md", Card: &boards.Card{ID: "c-gone", Title: "Lost pieces"},
		Log: []council.Event{
			{Time: at(3*60*24 + 1), Kind: "done", Text: "The brief is ready for approval"},
			{Time: at(3*60*24 + 2), Kind: "done", Text: "The brief is ready for approval"},
		},
		Created: at(3 * 60 * 24), Updated: at(3 * 60 * 24),
	})

	wdir := filepath.Join(root, "work")
	end1, end2 := at(30), at(4*60*24+30)
	writeJSON(t, filepath.Join(wdir, "aaaaaaaa", "session.json"), work.Session{
		ID: "aaaaaaaa", Provider: "claude", Project: "demo", Title: "Ship the merged thing", Card: f.merged.ID, Board: "work",
		Status: work.StatusDone, Started: at(15), Ended: &end1, Branch: "lucid/aaaaaaaa-ship",
		Usage: &agentexec.Usage{Provider: "claude", Model: "sonnet", CostUSD: 1.50, OutputTokens: 900},
		Diff:  &work.Diff{Stat: " a.go | 2 +", Added: 2, Files: []work.FileChange{{Path: "a.go", Added: 2}}, Commits: []work.Commit{{SHA: "abc", Subject: "feat: ship"}}},
		PR:    "https://github.com/you/demo/pull/1", PRState: work.PRMerged, PRChecked: at(120), CardDone: true, Pushed: true,
	})
	writeJSON(t, filepath.Join(wdir, "bbbbbbbb", "session.json"), work.Session{
		ID: "bbbbbbbb", Provider: "codex", Project: "demo", Title: "Hand-made fix", Card: f.hand.ID, Board: "work",
		Status: work.StatusDone, Started: at(4 * 60 * 24), Ended: &end2, PR: "https://github.com/you/demo/pull/2", PRState: work.PRDraft,
		PRChecks: &work.PRChecks{Passing: 3}, PRChecked: at(4*60*24 + 40), Usage: &agentexec.Usage{CostUSD: 0.25},
	})
	writeJSON(t, filepath.Join(wdir, "cccccccc", "session.json"), work.Session{
		ID: "cccccccc", Provider: "claude", Project: "demo", Title: "A free prompt", Status: work.StatusRunning, Started: at(5 * 60 * 24),
	})

	f.svc = &Service{
		Council: council.New(cdir, nil, nil),
		Work:    work.New(wdir, nil, nil, nil),
		Vault:   func() (*memory.Vault, error) { return v, nil },
	}
	return f
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestListDerivesStagesAndCosts(t *testing.T) {
	f := newFixture(t)
	list, err := f.svc.List()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Summary{}
	for _, s := range list {
		byID[s.ID] = s
	}
	if len(list) != 6 {
		t.Fatalf("%d ideas: %+v", len(list), list)
	}
	handID := CardPrefix + "work/" + f.hand.ID
	bareID := CardPrefix + "work/" + f.bare.ID
	for id, want := range map[string]string{
		"20260501-090000-aaaaaa": StageMerged,
		"20260502-090000-bbbbbb": StageBrief,
		"20260503-090000-cccccc": StageBraindump,
		"20260504-090000-dddddd": StageCard,
		handID:                   StagePR,
		bareID:                   StageCard,
	} {
		if got := byID[id]; got.Stage != want {
			t.Errorf("%s: stage %q, want %q (%+v)", id, got.Stage, want, got)
		}
	}
	// The merged card is the council's idea, not a second hand-made one.
	if _, ok := byID[CardPrefix+"work/"+f.merged.ID]; ok {
		t.Error("a council card is listed twice")
	}
	m := byID["20260501-090000-aaaaaa"]
	if !near(m.CostUSD, 1.67) || m.Sessions != 1 || m.Status != "Merged" || m.PR == "" || m.Card != "work/"+f.merged.ID || m.Project != "demo" {
		t.Errorf("merged idea %+v", m)
	}
	if !m.Updated.Equal(at(120)) {
		t.Errorf("updated %v", m.Updated)
	}
	h := byID[handID]
	if h.Title != "Hand-made fix" || !near(h.CostUSD, 0.25) || h.Status != "Draft PR" || !h.Created.Equal(at(4*60*24)) || h.Council != "" {
		t.Errorf("hand-made idea %+v", h)
	}
	if b := byID["20260502-090000-bbbbbb"]; b.Status != "Waiting for approval" || b.Title != "Waiting brief" || b.CostUSD != 0 {
		t.Errorf("waiting idea %+v", b)
	}
	if c := byID["20260503-090000-cccccc"]; c.Title != "a half formed thought" || c.Status != "Council failed" {
		t.Errorf("braindump idea %+v", c)
	}
	if bare := byID[bareID]; bare.Status != "In Inbox" || !bare.Created.IsZero() {
		t.Errorf("bare card %+v", bare)
	}
	// Newest first; an idea with no time at all goes last.
	if list[0].ID != handID || list[len(list)-1].ID != bareID {
		t.Errorf("order: %s … %s", list[0].ID, list[len(list)-1].ID)
	}
}

func TestGetBuildsTheStructure(t *testing.T) {
	f := newFixture(t)
	idea, err := f.svc.Get("20260501-090000-aaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if idea.Session == nil || len(idea.Session.Rounds) != 1 || idea.Input != "ship it" {
		t.Errorf("council session %+v", idea.Session)
	}
	if b := idea.Brief; b == nil || !b.Exists || b.Status != "approved" || !strings.Contains(b.Body, "## Outcome") {
		t.Errorf("brief %+v", b)
	}
	if strings.Join(idea.Links, ",") != "Notes/plan.md" {
		t.Errorf("links %v", idea.Links)
	}
	cv := idea.CardView
	if cv == nil || cv.Column != "Done" || cv.Board != "work" {
		t.Fatalf("card %+v", cv)
	}
	var moves []string
	for _, m := range cv.History {
		moves = append(moves, m.Column)
	}
	if strings.Join(moves, ",") != "Ready,In progress,Review,Done" || !cv.History[0].Time.Equal(at(10)) || !strings.Contains(cv.HistoryNote, "by hand") {
		t.Errorf("history %v (%+v)", moves, cv)
	}
	if len(idea.Work) != 1 || idea.Work[0].Added != 2 || len(idea.Work[0].Commits) != 1 || idea.Work[0].Model != "sonnet" || idea.Work[0].PRState != "merged" {
		t.Errorf("work %+v", idea.Work)
	}
	var kinds []string
	for i, e := range idea.Timeline {
		kinds = append(kinds, e.Kind)
		if i > 0 && e.Time.Before(idea.Timeline[i-1].Time) {
			t.Errorf("timeline out of order at %d", i)
		}
	}
	if got := strings.Join(kinds, ","); got != "council,council,critique,critique,council,brief,approve,work,work,pr,merged" {
		t.Errorf("timeline %s", got)
	}
	for _, e := range idea.Timeline {
		if e.Kind == "pr" && (!e.Untimed || e.Link == "") {
			t.Errorf("the PR event claims a time: %+v", e)
		}
		if e.Kind == "brief" && e.Link != "memory/Inbox/merged.md" {
			t.Errorf("brief link %q", e.Link)
		}
	}
	want := map[string]float64{"claude/work": 1.50, "claude/council": 0.12, "codex/council": 0.05}
	if len(idea.Costs) != 3 || idea.Costs[0].Provider != "claude" || idea.Costs[0].Part != "work" {
		t.Errorf("costs %+v", idea.Costs)
	}
	for _, c := range idea.Costs {
		if !near(c.CostUSD, want[c.Provider+"/"+c.Part]) {
			t.Errorf("cost %+v", c)
		}
		if c.Provider == "claude" && c.Part == "council" && (c.Calls != 2 || c.InputTokens != 100) {
			t.Errorf("claude council calls %+v", c)
		}
	}
	if len(idea.Missing) != 0 {
		t.Errorf("missing %v", idea.Missing)
	}
}

func TestMissingPiecesAreTolerated(t *testing.T) {
	f := newFixture(t)
	idea, err := f.svc.Get("20260504-090000-dddddd")
	if err != nil {
		t.Fatal(err)
	}
	if idea.Stage != StageCard || idea.Status != "Card missing" || idea.CardView != nil || idea.Brief == nil || idea.Brief.Exists {
		t.Errorf("idea %+v", idea.Summary)
	}
	if len(idea.Missing) != 2 || !strings.Contains(strings.Join(idea.Missing, "|"), "no longer on the work board") || !strings.Contains(strings.Join(idea.Missing, "|"), "Inbox/deleted.md is gone") {
		t.Errorf("missing %v", idea.Missing)
	}

	var briefs []string
	for _, e := range idea.Timeline {
		if e.Kind == "brief" {
			briefs = append(briefs, e.Title)
		}
	}
	if strings.Join(briefs, "|") != "Brief written to Memory|Brief rewritten after Ask again" {
		t.Errorf("brief events %v", briefs)
	}

	// A hand-made card with no record that moved it.
	bare, err := f.svc.Get(CardPrefix + "work/" + f.bare.ID)
	if err != nil || bare.CardView == nil || len(bare.CardView.History) != 0 || !strings.Contains(bare.CardView.HistoryNote, "only where it is now") ||
		bare.Session != nil || bare.Brief != nil || len(bare.Timeline) != 1 || !bare.Timeline[0].Untimed {
		t.Errorf("bare card %+v %v", bare, err)
	}

	// A link that knows only the card lands on the council's idea.
	via, err := f.svc.Get(CardPrefix + "work/" + f.merged.ID)
	if err != nil || via.ID != "20260501-090000-aaaaaa" || via.Session == nil || via.Stage != StageMerged {
		t.Errorf("card of a council idea: %+v %v", via, err)
	}

	// No vault and no Work at all: the council ideas still come through.
	s := &Service{Council: f.svc.Council}
	list, err := s.List()
	if err != nil || len(list) != 4 {
		t.Fatalf("council only: %d %v", len(list), err)
	}
	if one, err := s.Get("20260501-090000-aaaaaa"); err != nil || one.Stage != StageCard || len(one.Work) != 0 {
		t.Errorf("council only: %+v %v", one, err)
	}
	if _, err := s.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	if _, err := f.svc.Get(CardPrefix + "work/c-none"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown card: %v", err)
	}
	if list, err := (&Service{}).List(); err != nil || len(list) != 0 {
		t.Errorf("nothing at all: %v %v", list, err)
	}

	// A fresh vault has no Boards folder: that is empty, not missing.
	fresh, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	empty := &Service{Council: f.svc.Council, Vault: func() (*memory.Vault, error) { return fresh, nil }}
	if one, err := empty.Get("20260503-090000-cccccc"); err != nil || len(one.Missing) != 0 {
		t.Errorf("fresh vault: missing %v, %v", one.Missing, err)
	}
	if _, err := os.Stat(filepath.Join(fresh.Root(), boards.Dir)); err == nil {
		t.Error("reading ideas created the Boards folder")
	}
}

func TestRoutes(t *testing.T) {
	f := newFixture(t)
	mux := http.NewServeMux()
	Register(mux, f.svc)
	get := func(p string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		return rec
	}
	if rec := get("/api/ideas"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"stage":"merged"`) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	// A card id holds a "/", escaped or not.
	for _, p := range []string{"/api/ideas/card:work/" + f.hand.ID, "/api/ideas/card:work%2F" + f.hand.ID} {
		rec := get(p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"stage":"pr"`) {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if rec := get("/api/ideas/20260501-090000-aaaaaa"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"timeline"`) {
		t.Errorf("one: %d", rec.Code)
	}
	if rec := get("/api/ideas/nope"); rec.Code != 404 {
		t.Errorf("unknown: %d", rec.Code)
	}
}
