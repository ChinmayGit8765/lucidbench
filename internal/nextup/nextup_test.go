package nextup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/linear"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/trello"
	"github.com/ChinmayGit8765/lucidbench/internal/usage"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// The test binary doubles as the claude CLI: copied onto PATH and run with
// LUCID_FAKE_RANK set to a folder, it records its arguments and input there
// and answers with reply.txt, or, when that file says REVERSE, with the
// candidates it was given in reverse order.
func TestMain(m *testing.M) {
	if dir := os.Getenv("LUCID_FAKE_RANK"); dir != "" {
		in, _ := io.ReadAll(os.Stdin)
		_ = os.WriteFile(filepath.Join(dir, "args.txt"), []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "stdin.txt"), in, 0o600)
		reply, _ := os.ReadFile(filepath.Join(dir, "reply.txt"))
		answer := string(reply)
		if answer == "REVERSE" {
			var input struct {
				Candidates []struct {
					ID string `json:"id"`
				} `json:"candidates"`
			}
			s := string(in)
			_ = json.Unmarshal([]byte(s[strings.Index(s, "{"):]), &input)
			var out []map[string]string
			for i := len(input.Candidates) - 1; i >= 0; i-- {
				out = append(out, map[string]string{"id": input.Candidates[i].ID, "reason": "reversed by the fake", "suggested_action": "start_work"})
			}
			b, _ := json.Marshal(map[string]any{"ranking": out})
			answer = string(b)
		}
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": answer, "total_cost_usd": 0.0012,
			"usage": map[string]any{"input_tokens": 900, "output_tokens": 120}, "modelUsage": map[string]any{"claude-haiku-fake": map[string]any{}}})
		fmt.Println(string(b))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// now is the fixtures' clock: a Friday.
var now = time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)

const projectsYAML = `version: 1
projects:
  - id: app
    name: App
    category: product
    type: web-app
    status: active
    visibility: public
    repo: you/app
    local_path: /src/app
    needs:
      - what: an API client
        from: lib
        status: todo
  - id: lib
    name: Lib
    category: tool
    type: library
    status: active
    visibility: public
    local_path: /src/lib
    builds_into: [app]
  - id: secret
    name: Hush Project
    category: product
    type: cli
    status: active
    visibility: confidential
    local_path: /src/secret
  - id: old
    name: Old
    category: experiment
    type: cli
    status: archived
    visibility: private
    needs:
      - what: a rewrite
        status: todo
`

func projectList(t *testing.T) *projects.List {
	t.Helper()
	// local_path must be absolute on this OS.
	base := filepath.ToSlash(t.TempDir())
	ps, errs := projects.Parse([]byte(strings.ReplaceAll(projectsYAML, "/src/", base+"/")))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return &projects.List{Configured: true, Projects: ps}
}

func ended(d time.Duration) *time.Time {
	t := now.Add(-d)
	return &t
}

// fixture is one of everything, read from plain values.
func fixture(t *testing.T) Sources {
	l := projectList(t)
	return Sources{
		Projects: func() (*projects.List, error) { return l, nil },
		Boards: func() ([]boards.Board, error) {
			return []boards.Board{{ID: "work", Title: "Work", Columns: boards.DefaultColumns, Cards: []boards.Card{
				{ID: "c1", Title: "Write the docs", Column: "Ready", Project: "app"},
				{ID: "c2", Title: "Refactor the parser", Column: "In progress", Project: "lib", Labels: []string{"small"}},
				{ID: "c3", Title: "Hush plan", Column: "Ready", Project: "secret", Memory: "Inbox/hush.md"},
				{ID: "c4", Title: "Shipped thing", Column: "Done", Project: "app", Done: true},
				{ID: "c5", Title: "In review", Column: "Review", Project: "app"},
				{ID: "c6", Title: "Overdue fix", Column: "Ready", Project: "app", Due: "2026-10-05"},
				{ID: "c7", Title: "Waiting on vendor", Column: "Inbox", Project: "lib", Labels: []string{"blocked"}},
				{ID: "c8", Title: "Linked issue", Column: "Ready", Project: "app", Linear: "ENG-7"},
				{ID: "c9", Title: "Being worked on", Column: "In progress", Project: "app"},
				{ID: "c10", Title: "Private page", Column: "Ready", Project: "app", Memory: "Private/plan.md"},
			}}}, nil
		},
		Briefs: func() ([]Brief, error) {
			return []Brief{
				{ID: "b1", Title: "Ready brief", Status: "draft", BriefPath: "Inbox/b1.md", Project: "app", Updated: now.Add(-time.Hour)},
				{ID: "b2", Title: "Approved, no card", Status: "approved", BriefPath: "Inbox/b2.md", Project: "lib"},
				{ID: "b3", Input: "Make the build report run nightly", Status: "failed", Project: "app"},
				{ID: "b4", Title: "Still running", Status: "running"},
				{ID: "b5", Title: "Approved with its card", Status: "approved", BriefPath: "Inbox/b5.md", Card: "c1"},
				{ID: "b6", Title: "Blocked brief", Status: "draft", BriefPath: "Inbox/b6.md", Blockers: 2},
			}, nil
		},
		Work: func() []work.Session {
			return []work.Session{
				{ID: "s1", Title: "Add the API client", Status: work.StatusWaiting, Project: "lib", Provider: "claude", Started: now.Add(-3 * time.Hour), Ended: ended(2 * time.Hour)},
				{ID: "s2", Title: "Broken run", Status: work.StatusFailed, Project: "app", Provider: "codex", Error: "exit status 1", Started: now.Add(-5 * 24 * time.Hour), Ended: ended(5 * 24 * time.Hour)},
				{ID: "s3", Title: "Failing PR", Status: work.StatusDone, Project: "app", PR: "https://github.com/you/app/pull/3", PRState: work.PROpen, PRChecks: &work.PRChecks{Failing: 1, Passing: 2}, Started: now.Add(-24 * time.Hour), Ended: ended(20 * time.Hour)},
				{ID: "s4", Title: "Green PR", Status: work.StatusDone, Project: "lib", PR: "https://github.com/you/lib/pull/4", PRState: work.PROpen, PRChecks: &work.PRChecks{Passing: 3}, Started: now.Add(-24 * time.Hour), Ended: ended(20 * time.Hour)},
				{ID: "s5", Title: "Merged PR", Status: work.StatusDone, Project: "app", PR: "https://github.com/you/app/pull/5", PRState: work.PRMerged, Started: now.Add(-24 * time.Hour)},
				{ID: "s6", Title: "Busy", Status: work.StatusRunning, Project: "app", Card: "c9", Board: "work", Started: now.Add(-time.Minute)},
			}
		},
		CI: func(context.Context) ([]FailingRun, error) {
			return []FailingRun{{Repo: "you/app", Workflow: "ci", Title: "fix: tidy", Branch: "main", Conclusion: "failure", RunNumber: 41, URL: "https://github.com/you/app/actions/runs/41", At: now.Add(-2 * time.Hour)}}, nil
		},
		Linear: func(context.Context) ([]linear.Issue, error) {
			return []linear.Issue{
				{Identifier: "ENG-7", Title: "Already on a card", URL: "https://linear.app/x/issue/ENG-7", Priority: 2},
				{Identifier: "ENG-9", Title: "Urgent bug", URL: "https://linear.app/x/issue/ENG-9", Priority: 1, PriorityLabel: "Urgent", TeamKey: "ENG", State: linear.State{Name: "Todo"}, UpdatedAt: now.Add(-24 * time.Hour).Format(time.RFC3339)},
			}, nil
		},
		Trello: func(context.Context) ([]trello.Card, error) {
			return []trello.Card{{ID: "t1", Name: "Book the venue", URL: "https://trello.com/c/t1", Due: "2026-10-10T12:00:00.000Z"}}, nil
		},
		Builder: func(project string) (*work.Builder, error) {
			if project == "app" {
				return &work.Builder{Provider: "codex", Model: "gpt-fake"}, nil
			}
			return nil, nil
		},
		PageConfidential: func(p string) bool { return strings.HasPrefix(p, "Private/") || p == "Inbox/hush.md" },
		LookPath: func(name string) (string, error) {
			if name == "claude" {
				return "/bin/claude", nil
			}
			return "", errors.New("not found")
		},
	}
}

func service(t *testing.T, src Sources) *Service {
	t.Helper()
	return &Service{Sources: src, Store: &Store{Dir: t.TempDir()}, Now: func() time.Time { return now }}
}

func byID(cs []Candidate) map[string]Candidate {
	m := map[string]Candidate{}
	for _, c := range cs {
		m[c.ID] = c
	}
	return m
}

func ids(cs []Candidate) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.ID)
	}
	return out
}

func TestCollectEverySource(t *testing.T) {
	v := service(t, fixture(t)).List(context.Background())
	if len(v.Errors) != 0 {
		t.Fatalf("errors %+v", v.Errors)
	}
	got := byID(v.Items)
	want := map[string]string{
		"card:work/c1": "boards", "card:work/c2": "boards", "card:work/c3": "boards", "card:work/c6": "boards", "card:work/c7": "boards",
		"card:work/c8": "boards", "card:work/c10": "boards",
		"brief:b1": "council", "brief:b2": "council", "brief:b3": "council", "brief:b6": "council",
		"work:s1": "work", "work:s2": "work", "pr:s3": "github", "pr:s4": "github",
		"ci:you/app": "github", "linear:ENG-9": "linear", "trello:t1": "trello", "need:app/0": "projects",
	}
	for id, src := range want {
		c, ok := got[id]
		if !ok {
			t.Errorf("missing %s", id)
			continue
		}
		if c.Source != src {
			t.Errorf("%s source %q, want %q", id, c.Source, src)
		}
		if c.Link.Route == "" && c.Link.URL == "" {
			t.Errorf("%s has no source link", id)
		}
		if c.Action.Kind == "" || c.Action.Label == "" || c.Action.Explain == "" {
			t.Errorf("%s has no action: %+v", id, c.Action)
		}
	}
	// Done, Review, a card a running session covers, a running council, an
	// approved brief with its card, a merged PR, a Linear issue already on a
	// card and an archived project's need are all left out.
	for _, id := range []string{"card:work/c4", "card:work/c5", "card:work/c9", "brief:b4", "brief:b5", "pr:s5", "linear:ENG-7", "need:old/0", "work:s6"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s should not be listed", id)
		}
	}
	if len(v.Items) != len(want) {
		t.Errorf("%d items: %v", len(v.Items), ids(v.Items))
	}
	// Confidential: the project, or the card's Memory page.
	for id, conf := range map[string]bool{"card:work/c3": true, "card:work/c10": true, "card:work/c1": false, "need:app/0": false} {
		if got[id].Confidential != conf {
			t.Errorf("%s confidential = %v", id, got[id].Confidential)
		}
	}
	if c := got["ci:you/app"]; c.Project != "app" || c.Link.URL == "" || !strings.Contains(c.Context, "run #41") {
		t.Errorf("ci %+v", c)
	}
	if c := got["need:app/0"]; c.Project != "lib" || !strings.Contains(c.Title, "App needs an API client from Lib") {
		t.Errorf("need %+v", c)
	}
}

func TestSourceErrorsAreReportedAndBounded(t *testing.T) {
	src := fixture(t)
	src.CI = func(ctx context.Context) ([]FailingRun, error) {
		<-ctx.Done() // a source that hangs
		return nil, ctx.Err()
	}
	src.Linear = func(context.Context) ([]linear.Issue, error) { return nil, errors.New("401 from Linear") }
	s := service(t, src)
	s.Timeout = 300 * time.Millisecond
	start := time.Now()
	v := s.List(context.Background())
	if time.Since(start) > 3*time.Second {
		t.Errorf("listing took %v", time.Since(start))
	}
	var got []string
	for _, e := range v.Errors {
		got = append(got, e.Source)
	}
	if !slices.Contains(got, "ci") || !slices.Contains(got, "linear") {
		t.Errorf("errors %+v", v.Errors)
	}
	if _, ok := byID(v.Items)["card:work/c1"]; !ok {
		t.Error("the other sources should still list")
	}
}

func TestScoringIsDeterministicAndExplained(t *testing.T) {
	a := service(t, fixture(t)).List(context.Background())
	b := service(t, fixture(t)).List(context.Background())
	if !slices.Equal(ids(a.Items), ids(b.Items)) {
		t.Fatalf("two listings differ:\n%v\n%v", ids(a.Items), ids(b.Items))
	}
	for i, c := range a.Items {
		sum := 0
		for _, p := range c.Parts {
			sum += p.Points
			if p.Why == "" || p.Factor == "" {
				t.Errorf("%s has an unexplained part %+v", c.ID, p)
			}
		}
		if sum != c.Score {
			t.Errorf("%s score %d, parts sum to %d", c.ID, c.Score, sum)
		}
		if i > 0 {
			prev := a.Items[i-1]
			if prev.Score < c.Score || (prev.Score == c.Score && prev.ID > c.ID) {
				t.Errorf("%s (%d) is above %s (%d)", prev.ID, prev.Score, c.ID, c.Score)
			}
		}
	}
	got := byID(a.Items)
	// CI failing on main beats every card.
	for _, c := range a.Items {
		if c.Kind == KindCard && c.Score >= got["ci:you/app"].Score {
			t.Errorf("%s (%d) is not below failing CI (%d)", c.ID, c.Score, got["ci:you/app"].Score)
		}
	}
	if !hasPart(got["card:work/c6"], "urgency", "overdue by 4 days") {
		t.Errorf("overdue parts %+v", got["card:work/c6"].Parts)
	}
	if !hasPart(got["card:work/c7"], "blocked", "") || got["card:work/c7"].Score >= got["card:work/c2"].Score {
		t.Errorf("blocked card %+v vs %+v", got["card:work/c7"], got["card:work/c2"])
	}
	if !hasPart(got["card:work/c2"], "effort", "labelled small") || !hasPart(got["card:work/c2"], "urgency", "already in progress") {
		t.Errorf("doing card parts %+v", got["card:work/c2"].Parts)
	}
	if !hasPart(got["brief:b1"], "urgency", "ready") || got["brief:b1"].Score <= got["brief:b6"].Score {
		t.Errorf("a ready brief should beat one with blockers: %+v %+v", got["brief:b1"].Parts, got["brief:b6"].Parts)
	}
}

func hasPart(c Candidate, factor, why string) bool {
	for _, p := range c.Parts {
		if p.Factor == factor && strings.Contains(p.Why, why) {
			return true
		}
	}
	return false
}

func TestUnblockingBeatsStaleness(t *testing.T) {
	l := projectList(t)
	src := Sources{
		Projects: func() (*projects.List, error) { return l, nil },
		Work: func() []work.Session {
			return []work.Session{
				// A month-old failure on app, which unblocks nobody.
				{ID: "old", Title: "Old failure", Status: work.StatusFailed, Project: "app", Started: now.Add(-31 * 24 * time.Hour), Ended: ended(30 * 24 * time.Hour)},
				// A fresh failure on lib, which App needs.
				{ID: "new", Title: "New failure", Status: work.StatusFailed, Project: "lib", Started: now.Add(-time.Hour), Ended: ended(time.Hour)},
			}
		},
	}
	v := service(t, src).List(context.Background())
	got := byID(v.Items)
	stale, unblock := got["work:old"], got["work:new"]
	if !hasPart(stale, "staleness", "untouched for 30 days") || !hasPart(unblock, "unblocking", "unblocks App") {
		t.Fatalf("parts: %+v / %+v", stale.Parts, unblock.Parts)
	}
	if v.Items[0].ID != "work:new" || unblock.Score <= stale.Score {
		t.Errorf("unblocking (%d) should beat staleness (%d): %v", unblock.Score, stale.Score, ids(v.Items))
	}
}

func pct(f float64) *float64 { return &f }

func TestUsageHeadroomDemotesSpendingWork(t *testing.T) {
	src := fixture(t)
	resets := now.Add(2 * time.Hour)
	src.Usage = func() *usage.Summary {
		return &usage.Summary{Providers: []usage.Provider{{ID: "codex", Windows: []usage.Window{{Name: "primary", Label: "5-hour window", UsedPercent: pct(95), ResetsAt: &resets}}}}}
	}
	with := byID(service(t, src).List(context.Background()).Items)
	without := byID(service(t, fixture(t)).List(context.Background()).Items)
	// app's builder is codex: its cards start codex, and lose 30.
	c := with["card:work/c1"]
	if !hasPart(c, "headroom", "codex's 5-hour window is at 95 %") || c.Score != without["card:work/c1"].Score-30 {
		t.Errorf("c1 %d vs %d: %+v", c.Score, without["card:work/c1"].Score, c.Parts)
	}
	// lib has no builder: claude, which has room. A link-only item never spends.
	if hasPart(with["card:work/c2"], "headroom", "") || hasPart(with["pr:s3"], "headroom", "") {
		t.Errorf("only codex work should be demoted: %+v %+v", with["card:work/c2"].Parts, with["pr:s3"].Parts)
	}
	// A window that has already reset counts for nothing.
	past := now.Add(-time.Minute)
	src.Usage = func() *usage.Summary {
		return &usage.Summary{Providers: []usage.Provider{{ID: "codex", Windows: []usage.Window{{Name: "primary", UsedPercent: pct(99), ResetsAt: &past}}}}}
	}
	if hasPart(byID(service(t, src).List(context.Background()).Items)["card:work/c1"], "headroom", "") {
		t.Error("a reset window demoted work")
	}
}

func TestFocusAndPinnedProjects(t *testing.T) {
	s := service(t, fixture(t))
	if _, err := s.Store.SaveSettings(Settings{FocusProject: "lib", Pinned: []string{"app", "app"}}); err != nil {
		t.Fatal(err)
	}
	got := byID(s.List(context.Background()).Items)
	if !hasPart(got["card:work/c2"], "focus", "your focus project") || !hasPart(got["card:work/c1"], "focus", "a pinned project") {
		t.Errorf("focus parts %+v / %+v", got["card:work/c2"].Parts, got["card:work/c1"].Parts)
	}
	if st := s.Store.Load(); !slices.Equal(st.Settings.Pinned, []string{"app"}) {
		t.Errorf("pinned %v", st.Settings.Pinned)
	}
	for _, bad := range []Settings{{FocusProject: "Not An Id"}, {ScheduleHours: -1}, {ScheduleHours: 1000}, {Provider: "gpt"}, {Pinned: []string{"../x"}}} {
		if _, err := s.Store.SaveSettings(bad); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v saved: %v", bad, err)
		}
	}
}

func TestSnoozeAndDismissHideAndFeedBack(t *testing.T) {
	s := service(t, fixture(t))
	ctx := context.Background()
	before := byID(s.List(ctx).Items)
	if _, err := s.Store.Snooze("card:work/c1", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Dismiss("card:work/c6", "not-important", "app", KindCard, now); err != nil {
		t.Fatal(err)
	}
	v := s.List(ctx)
	got := byID(v.Items)
	if _, ok := got["card:work/c1"]; ok {
		t.Error("a snoozed item is still listed")
	}
	if _, ok := got["card:work/c6"]; ok {
		t.Error("a set-aside item is still listed")
	}
	if len(v.Hidden) != 2 {
		t.Errorf("hidden %+v", v.Hidden)
	}
	// Setting an item aside counts against its project's other work.
	if c := got["card:work/c8"]; !hasPart(c, "set_aside", "1 item") || c.Score != before["card:work/c8"].Score-5 {
		t.Errorf("c8 %d vs %d: %+v", c.Score, before["card:work/c8"].Score, c.Parts)
	}
	// A snooze ends by itself.
	s.Now = func() time.Time { return now.Add(25 * time.Hour) }
	if _, ok := byID(s.List(ctx).Items)["card:work/c1"]; !ok {
		t.Error("the snooze did not end")
	}
	s.Now = func() time.Time { return now }
	if err := s.Store.Restore("card:work/c6"); err != nil {
		t.Fatal(err)
	}
	if _, ok := byID(s.List(ctx).Items)["card:work/c6"]; !ok {
		t.Error("restore did not bring it back")
	}
	for _, err := range []error{
		func() error { _, err := s.Store.Snooze("card:work/c1", 3, now); return err }(),
		func() error { _, err := s.Store.Snooze("../../etc", 1, now); return err }(),
		s.Store.Dismiss("card:work/c1", "because", "", "", now),
	} {
		if !errors.Is(err, ErrBadRequest) {
			t.Errorf("want a bad request, got %v", err)
		}
	}
}

func TestActionsCreateTheRightRequests(t *testing.T) {
	got := byID(service(t, fixture(t)).List(context.Background()).Items)
	check := func(id, kind string, spends bool) Action {
		t.Helper()
		a := got[id].Action
		if a.Kind != kind || a.Spends != spends {
			t.Errorf("%s action %s (spends %v), want %s (%v)", id, a.Kind, a.Spends, kind, spends)
		}
		return a
	}
	// A card on app: Work with the card linked and app's builder.
	if a := check("card:work/c1", ActStartWork, true); a.Work == nil || *a.Work != (WorkRequest{Card: "c1", Project: "app", Provider: "codex", Model: "gpt-fake"}) {
		t.Errorf("c1 work %+v", a.Work)
	}
	// lib has no builder: the first installed CLI.
	if a := check("card:work/c2", ActStartWork, true); a.Work == nil || a.Work.Provider != "claude" || a.Work.Model != "" || a.Work.Card != "c2" {
		t.Errorf("c2 work %+v", a.Work)
	}
	// A confidential card only opens.
	if a := check("card:work/c3", ActOpenCard, false); a.Work != nil || !strings.Contains(a.Note, "confidential") || a.Route != "/boards/work/c3" {
		t.Errorf("c3 %+v", a)
	}
	// Fix CI: Work on the repo's project, the run in the prompt.
	if a := check("ci:you/app", ActFixCI, true); a.Work == nil || a.Work.Project != "app" || a.Work.Card != "" ||
		!strings.Contains(a.Work.Prompt, "https://github.com/you/app/actions/runs/41") || !strings.Contains(a.Work.Prompt, "run #41") {
		t.Errorf("ci work %+v", a.Work)
	}
	if a := check("work:s1", ActResumeWork, false); a.Route != "/work/s1" {
		t.Errorf("s1 %+v", a)
	}
	if a := check("work:s2", ActOpenSession, false); a.Route != "/work/s2" {
		t.Errorf("s2 %+v", a)
	}
	if a := check("pr:s3", ActOpenPR, false); a.URL != "https://github.com/you/app/pull/3" {
		t.Errorf("s3 %+v", a)
	}
	if a := check("brief:b3", ActRunCouncil, true); a.Council == nil || a.Council.Input != "Make the build report run nightly" || a.Council.Project != "app" {
		t.Errorf("b3 %+v", a.Council)
	}
	if a := check("brief:b1", ActOpenBrief, false); a.Route != "/council/b1" {
		t.Errorf("b1 %+v", a)
	}
	if a := check("linear:ENG-9", ActOpenLink, false); a.URL == "" {
		t.Errorf("linear %+v", a)
	}
	// A need supplied by lib is Work on lib, its prompt naming App.
	if a := check("need:app/0", ActStartWork, true); a.Work == nil || a.Work.Project != "lib" || !strings.Contains(a.Work.Prompt, "App needs an API client from Lib") {
		t.Errorf("need %+v", a.Work)
	}
	// Without any CLI, nothing that spends is offered.
	src := fixture(t)
	src.Builder = nil
	src.LookPath = func(string) (string, error) { return "", errors.New("none") }
	if a := byID(service(t, src).List(context.Background()).Items)["card:work/c1"].Action; a.Kind != ActOpenCard || !strings.Contains(a.Note, "No provider CLI") {
		t.Errorf("no CLI: %+v", a)
	}
}

// fakeClaude puts the test binary on PATH as claude and returns its folder.
func fakeClaude(t *testing.T, reply string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	bin, state := t.TempDir(), t.TempDir()
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "reply.txt"), []byte(reply), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("LUCID_FAKE_RANK", state)
	return state
}

func TestAgentInputRedactsConfidentialItems(t *testing.T) {
	state := fakeClaude(t, "REVERSE")
	s := service(t, fixture(t))
	runs := filepath.Join(t.TempDir(), "runs")
	s.Ranker = &Ranker{RunsDir: runs, Now: func() time.Time { return now }}
	v, r, err := s.Rank(context.Background(), RankRequest{})
	if err != nil {
		t.Fatal(err)
	}
	stdin, _ := os.ReadFile(filepath.Join(state, "stdin.txt"))
	in := string(stdin)
	for _, secret := range []string{"Hush", "hush", "secret", "Private page", "Private/plan.md", "card:work", "Inbox/"} {
		if strings.Contains(in, secret) {
			t.Errorf("the agent's input holds %q:\n%s", secret, in)
		}
	}
	if !strings.Contains(in, "Write the docs") {
		t.Errorf("a public title is missing:\n%s", in)
	}
	// Confidential items are exactly {id, score}.
	var parsed struct {
		Candidates []map[string]any `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(in[strings.Index(in, "{"):]), &parsed); err != nil {
		t.Fatal(err)
	}
	private := 0
	for _, c := range parsed.Candidates {
		if _, ok := c["title"]; !ok {
			private++
			if len(c) != 2 || c["id"] == nil || c["score"] == nil {
				t.Errorf("a private candidate carries %v", c)
			}
		}
	}
	if private != 2 {
		t.Errorf("%d private candidates, want 2", private)
	}
	args, _ := os.ReadFile(filepath.Join(state, "args.txt"))
	a := strings.ReplaceAll(string(args), "\n", " ")
	for _, want := range []string{"--model haiku", "--tools ", "--system-prompt-file"} {
		if !strings.Contains(a, want) {
			t.Errorf("claude ran without %q: %s", want, a)
		}
	}
	// The fake reversed the list, so the lowest scored item is now first.
	if r.Provider != "claude" || r.Model != "haiku" || r.PromptVersion != "v2" || r.Usage.CostUSD != 0.0012 || v.Ranked == nil || v.Ranked.Ranked != len(v.Items) {
		t.Errorf("ranking %+v / %+v", r, v.Ranked)
	}
	plain := service(t, fixture(t)).List(context.Background()).Items
	if v.Items[0].ID != plain[len(plain)-1].ID || v.Items[len(v.Items)-1].ID != plain[0].ID {
		t.Errorf("not reversed: %v", ids(v.Items))
	}
	for _, c := range v.Items {
		if c.Agent == nil || (c.Confidential && c.Agent.Reason != privateRankNotes) {
			t.Errorf("%s agent %+v", c.ID, c.Agent)
		}
	}
	// The ranking is kept, and its cost recorded for the Usage page.
	if again := s.List(context.Background()); again.Items[0].ID != v.Items[0].ID || again.Ranked == nil {
		t.Errorf("the ranking was not kept: %v", ids(again.Items))
	}
	if files, _ := filepath.Glob(filepath.Join(runs, "*.json")); len(files) != 1 {
		t.Errorf("%d usage records", len(files))
	}
}

func TestParseRankingValidates(t *testing.T) {
	cs := []Candidate{
		{ID: "card:work/a", Title: "A", Action: Action{Kind: ActStartWork, Work: &WorkRequest{Project: "app"}}},
		{ID: "card:work/b", Title: "B", Confidential: true, Action: Action{Kind: ActStartWork, Work: &WorkRequest{Project: "secret"}}},
		{ID: "pr:c", Title: "C", Action: Action{Kind: ActOpenPR}},
	}
	alias := map[string]int{"c1": 0, "c2": 1, "c3": 2}
	long := strings.Repeat("x", 5000)
	answer := "Sure:\n```json\n" + `{"ranking": [
	  {"id": "c9", "reason": "made up"},
	  {"id": "card:work/a", "reason": "a real id is not an alias"},
	  {"id": "c3", "reason": "first", "suggested_action": "rm -rf", "suggested_prompt": "not for a PR"},
	  {"id": "c2", "reason": "the private one", "suggested_prompt": "leak it"},
	  {"id": "c3", "reason": "twice"},
	  {"id": "c1", "reason": "` + long + `", "suggested_action": "start_work", "suggested_prompt": "` + long + `"}
	]}` + "\n```"
	got, err := ParseRanking(answer, cs, alias)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].ID != "pr:c" || got[1].ID != "card:work/b" || got[2].ID != "card:work/a" {
		t.Fatalf("ranking %+v", got)
	}
	if got[0].SuggestedAction != "" || got[0].SuggestedPrompt != "" {
		t.Errorf("an unknown action or a prompt for a link was kept: %+v", got[0])
	}
	if got[1].SuggestedPrompt != "" || got[1].Reason != privateRankNotes {
		t.Errorf("the private item kept the agent's words: %+v", got[1])
	}
	if n := len([]rune(got[2].Reason)); n != MaxReason {
		t.Errorf("reason is %d runes", n)
	}
	if n := len([]rune(got[2].SuggestedPrompt)); n != MaxPrompt || got[2].SuggestedAction != ActStartWork {
		t.Errorf("prompt is %d runes, action %q", n, got[2].SuggestedAction)
	}
	// Aliases in a reason become titles; a private one stays private.
	got, err = ParseRanking(`{"ranking": [{"id": "c1", "reason": "Do it before c3 and c2, unlike c12."}]}`, cs, alias)
	if err != nil || got[0].Reason != "Do it before “C” and a private item, unlike c12." {
		t.Errorf("unaliased %q (%v)", got[0].Reason, err)
	}
	// A bare list is fine; nothing known is an error.
	if got, err := ParseRanking(`[{"id":"c1"}]`, cs, alias); err != nil || len(got) != 1 {
		t.Errorf("bare list: %v %v", got, err)
	}
	for _, bad := range []string{"no json here", `{"ranking": [{"id": "zz"}]}`, `{"ranking": "c1"}`} {
		if _, err := ParseRanking(bad, cs, alias); !errors.Is(err, ErrBadOutput) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestAgentPromptBecomesTheWorkPrompt(t *testing.T) {
	items := []Candidate{
		{ID: "x", Score: 1, Action: Action{Kind: ActStartWork, Work: &WorkRequest{Project: "app", Card: "c1"}}},
		{ID: "y", Score: 9, Confidential: true, Action: Action{Kind: ActStartWork, Work: &WorkRequest{Project: "secret"}}},
	}
	orig := items[0].Action.Work
	n := ApplyRanking(items, []RankedItem{{ID: "gone"}, {ID: "x", Reason: "r", SuggestedPrompt: "Do the docs."}, {ID: "y", SuggestedPrompt: "nope"}})
	if n != 2 || items[0].ID != "x" || items[0].Action.Work.Prompt != "Do the docs." || items[0].Agent.Rank != 2 {
		t.Errorf("applied %d: %+v", n, items[0])
	}
	if items[1].Action.Work.Prompt != "" {
		t.Error("a confidential item took a prompt")
	}
	if orig.Prompt != "" {
		t.Error("the shared request was changed in place")
	}
}

func TestHTTP(t *testing.T) {
	s := service(t, fixture(t))
	mux := http.NewServeMux()
	Register(mux, s)
	do := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := do(http.MethodGet, "/api/nextup", "", false)
	var v View
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil || len(v.Items) == 0 || v.Items[0].Parts == nil {
		t.Fatalf("GET %d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/nextup/rank", "/api/nextup/snooze", "/api/nextup/dismiss", "/api/nextup/restore"} {
		if rec := do(http.MethodPost, p, `{}`, false); rec.Code != http.StatusForbidden {
			t.Errorf("%s without confirm: %d", p, rec.Code)
		}
	}
	if rec := do(http.MethodPost, "/api/nextup/snooze", `{"id":"card:work/c1","days":7}`, true); rec.Code != 200 {
		t.Errorf("snooze %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/api/nextup/snooze", `{"id":"card:work/c1","days":2}`, true); rec.Code != 400 {
		t.Errorf("snooze 2 days %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/nextup/dismiss", `{"id":"card:work/c6","reason":"blocked"}`, true); rec.Code != 200 {
		t.Errorf("dismiss %d %s", rec.Code, rec.Body)
	}
	if d := s.Store.Load().Dismissed["card:work/c6"]; d.Project != "app" || d.Kind != KindCard {
		t.Errorf("dismissal %+v", d)
	}
	if rec := do(http.MethodPost, "/api/nextup/dismiss", `{"id":"card:work/c6","reason":"blocked","project":"x"}`, true); rec.Code != 400 {
		t.Errorf("unknown field %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/nextup/restore", `{"id":"card:work/c6"}`, true); rec.Code != 204 {
		t.Errorf("restore %d", rec.Code)
	}
	if rec := do(http.MethodPut, "/api/nextup/settings", `{"focus_project":"lib","pinned":[],"schedule_hours":4}`, true); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"schedule_hours":4`) {
		t.Errorf("settings %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodPost, "/api/nextup/rank", `{}`, true); rec.Code != http.StatusInternalServerError {
		t.Errorf("rank without a ranker %d", rec.Code)
	}
}

func TestRankWithoutACLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := service(t, fixture(t))
	s.Ranker = &Ranker{Runner: &agentexec.Runner{}}
	if _, _, err := s.Rank(context.Background(), RankRequest{}); !errors.Is(err, agentexec.ErrCLIMissing) {
		t.Errorf("err %v", err)
	}
}

func TestTopNRedactsForThePhone(t *testing.T) {
	s := service(t, fixture(t))
	// Put a confidential card on top.
	if _, err := s.Store.SaveSettings(Settings{FocusProject: "secret", Pinned: []string{}}); err != nil {
		t.Fatal(err)
	}
	src := s.Sources
	src.CI, src.Work, src.Linear, src.Trello, src.Briefs = nil, nil, nil, nil, nil
	s.Sources = src
	top := s.TopN(context.Background(), 3)
	if len(top) != 3 {
		t.Fatalf("top %+v", top)
	}
	b, _ := json.Marshal(top)
	for _, secret := range []string{"Hush", "secret", "Private page"} {
		if strings.Contains(string(b), secret) {
			t.Errorf("the phone view holds %q: %s", secret, b)
		}
	}
	if !strings.Contains(string(b), PrivateTitle) {
		t.Errorf("no redacted item: %s", b)
	}
}
