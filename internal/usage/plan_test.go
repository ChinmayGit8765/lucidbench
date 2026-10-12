package usage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
)

// at is now plus d, as a sample timestamp.
func at(d time.Duration) int64 { return now.Add(d).Unix() }

func tok(d time.Duration, n int64) sample { return sample{TS: at(d), In: n, Account: "default"} }

func i64(n int64) *int64       { return &n }
func f64(f float64) *float64   { return &f }
func ptime(t time.Time) *int64 { u := t.Unix(); return &u }

func TestCodexPaceBurnRateAndRunOut(t *testing.T) {
	// A 5-hour window that resets at 15:00 local; it started at 10:00.
	w := &cxWin{Used: f64(40), Minutes: i64(300), Resets: ptime(now.Add(3 * time.Hour))}
	observed := now.Add(-10 * time.Minute)
	ss := []sample{
		tok(-110*time.Minute, 20000),
		tok(-30*time.Minute, 20000),
		// Spent after the CLI last reported the window.
		tok(-5*time.Minute, 20000),
		// Before the window started: not counted in it.
		tok(-3*time.Hour, 99999),
		// Cache reads never count.
		{TS: at(-time.Minute), CR: 500000, Account: "default"},
	}

	// 40 % was 40k tokens, so the window holds about 100k; 60k are used.
	p := codexPace("default", "5h", w, observed, ss, now, 1, zone)
	if p.CapacityTokens != 100000 || p.UsedTokens != 60000 || p.UsedPercent == nil || *p.UsedPercent != 60 {
		t.Fatalf("capacity %d used %d pct %v", p.CapacityTokens, p.UsedTokens, p.UsedPercent)
	}
	// The last hour burnt 40k: the 40k left last one more hour.
	if p.BurnPerHour != 40000 || p.Verdict != "runs_out" || p.RunsOutAt == nil || !p.RunsOutAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("burn %v verdict %s runs out %v", p.BurnPerHour, p.Verdict, p.RunsOutAt)
	}
	if want := "At this pace codex hits its 5h limit at 13:00, 2 h before it resets."; p.Summary != want {
		t.Errorf("summary %q, want %q", p.Summary, want)
	}

	// Over the last two hours the pace is 30k an hour: 80 minutes left.
	p = codexPace("default", "5h", w, observed, ss, now, 2, zone)
	if p.RunsOutAt == nil || !p.RunsOutAt.Equal(now.Add(80*time.Minute)) {
		t.Fatalf("2h pace runs out at %v", p.RunsOutAt)
	}
	if want := "At this pace codex hits its 5h limit at 13:20, 1 h 40 min before it resets."; p.Summary != want {
		t.Errorf("summary %q", p.Summary)
	}

	// Averaged over a day the pace is slow, and the window lasts.
	p = codexPace("default", "5h", w, observed, ss, now, 24, zone)
	if p.Verdict != "lasts" || p.RunsOutAt != nil || !strings.Contains(p.Summary, "lasts until it resets at 15:00") {
		t.Errorf("slow pace: %s %q", p.Verdict, p.Summary)
	}

	// Nothing in the last 15 minutes: idle.
	p = codexPace("default", "5h", w, observed, ss[:2], now, 0.25, zone)
	if p.Verdict != "idle" || p.BurnPerHour != 0 {
		t.Errorf("idle: %s %v", p.Verdict, p.BurnPerHour)
	}

	// An unknown share gives no capacity and no projection, only the pace.
	p = codexPace("default", "5h", &cxWin{Minutes: i64(300), Resets: w.Resets}, observed, ss, now, 1, zone)
	if p.CapacityTokens != 0 || p.Verdict != "unknown" || !strings.Contains(p.Summary, "40k tokens an hour") {
		t.Errorf("unknown share: %+v", p)
	}

	// A window that has reset since it was seen has nothing to project.
	if p := codexPace("default", "5h", &cxWin{Used: f64(90), Minutes: i64(300), Resets: ptime(now.Add(-time.Minute))}, observed, ss, now, 1, zone); p != nil {
		t.Errorf("stale window projected: %+v", p)
	}
}

func TestClaudePaceFromBlocks(t *testing.T) {
	ss := []sample{
		// Yesterday's block: 100k, the busiest lately.
		tok(-26*time.Hour, 60000),
		tok(-25*time.Hour, 40000),
		// A smaller block the day before.
		tok(-50*time.Hour, 30000),
		// The current block started at 00:30 UTC, so it runs 00:00 to 05:00
		// UTC: 10:00 to 15:00 local.
		tok(-90*time.Minute, 30000),
		tok(-30*time.Minute, 20000),
	}
	p := claudePace("default", ss, now, 2, zone)
	if p == nil || p.Basis != "estimated" || p.CapacityTokens != 100000 || p.UsedTokens != 50000 {
		t.Fatalf("pace %+v", p)
	}
	if p.ResetsAt == nil || p.ResetsAt.In(zone).Format("15:04") != "15:00" {
		t.Fatalf("resets at %v", p.ResetsAt)
	}
	// 50k left at 25k an hour: two hours, at 14:00.
	if want := "At this pace claude hits its 5h limit at 14:00, 1 h before it resets."; p.Summary != want {
		t.Errorf("summary %q, want %q", p.Summary, want)
	}

	// No block running: idle, nothing to project.
	p = claudePace("default", ss[:3], now, 2, zone)
	if p.Verdict != "idle" || p.StartsAt != nil {
		t.Errorf("no block: %+v", p)
	}
	// A block busier than any before has no estimated limit.
	p = claudePace("work", append(ss[:3:3], tok(-time.Hour, 150000)), now, 2, zone)
	if p.CapacityTokens != 0 || p.Verdict != "unknown" || !strings.Contains(p.Summary, "busier than any") || !strings.Contains(p.Summary, "(work)") {
		t.Errorf("busiest block: %+v", p)
	}
}

func TestPacesFromLogsPerAccount(t *testing.T) {
	e := newEnv(t)
	second := filepath.Join(e.home, "claude-work")
	e.svc.Roots = func() accounts.Roots { return accounts.Roots{Home: e.home, ExtraClaudeDirs: []string{second}} }
	// Default account: a current block only.
	write(t, filepath.Join(e.home, ".claude", "projects", "p", "a.jsonl"),
		claudeMsg("d1", now.Add(-time.Hour).UTC().Format(time.RFC3339), 1000, 0, 0, 0))
	// The second account: another block, deduplicated separately.
	write(t, filepath.Join(second, "projects", "p", "b.jsonl"),
		claudeMsg("w1", now.Add(-2*time.Hour).UTC().Format(time.RFC3339), 2000, 0, 0, 0))
	// Codex reports its windows.
	resets := now.Add(2 * time.Hour).Unix()
	write(t, filepath.Join(e.home, ".codex", "sessions", "2026", "03", "03", "rollout-a.jsonl"),
		codexTok(now.Add(-time.Hour).UTC().Format(time.RFC3339), 10000, 0, 0, 10000,
			fmt.Sprintf(`{"primary":{"used_percent":50,"window_minutes":300,"resets_at":%d},"secondary":{"used_percent":5,"window_minutes":10080,"resets_at":%d}}`, resets, resets+5*86400)))
	v, err := e.svc.Planner(2)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Pace{}
	for _, p := range v.Paces {
		got[p.Provider+"/"+p.Account+"/"+p.Window] = p
	}
	if p := got["claude/default/5h"]; p.UsedTokens != 1000 || p.Basis != "estimated" {
		t.Errorf("claude default %+v", p)
	}
	if p := got["claude/claude-work/5h"]; p.UsedTokens != 2000 {
		t.Errorf("claude second account %+v", p)
	}
	if p := got["codex/default/5h"]; p.CapacityTokens != 20000 || p.Basis != "reported" || p.UsedTokens != 10000 {
		t.Errorf("codex 5h %+v", p)
	}
	if p := got["codex/default/weekly"]; p.CapacityTokens != 200000 {
		t.Errorf("codex weekly %+v", p)
	}
	if len(v.Items) != 0 || v.Windows == nil {
		t.Errorf("empty plan: %+v", v)
	}
}

// writeRecords writes the fixture records the estimates are read from.
func writeRecords(t *testing.T, data string) {
	t.Helper()
	u := func(provider, model string, in, out int, cost float64) string {
		return fmt.Sprintf(`{"provider":%q,"model":%q,"input_tokens":%d,"output_tokens":%d,"cache_read_tokens":999999,"cost_usd":%v,"duration_ms":1}`, provider, model, in, out, cost)
	}
	// Three councils: claude proposes, codex critiques.
	for i, n := range []int{1000, 2000, 9000} {
		write(t, filepath.Join(data, "council", fmt.Sprintf("c%d.json", i)),
			fmt.Sprintf(`{"created":"2026-03-0%dT01:00:00Z","usage":[%s,%s,%s]}`, i+1, u("claude", "opus", n, 0, 0.5), u("codex", "gpt-x", 300, 0, 0.25), u("claude", "opus", n, 0, 0.5)))
	}
	// Work: one session, three turns on claude sonnet, one on haiku.
	write(t, filepath.Join(data, "work", "sessions", "w1", "session.json"),
		fmt.Sprintf(`{"started":"2026-03-02T01:00:00Z","usage":[%s,%s,%s,%s]}`,
			u("claude", "sonnet", 10000, 1000, 1.0), u("claude", "sonnet", 20000, 2000, 2.0), u("claude", "sonnet", 40000, 4000, 4.0), u("claude", "haiku", 500, 0, 0.01)))
	// Ranking: two calls on claude haiku.
	write(t, filepath.Join(data, "nextup", "runs", "n1.json"), `{"created":"2026-03-02T03:00:00Z","usage":`+u("claude", "haiku", 300, 100, 0)+`}`)
	write(t, filepath.Join(data, "nextup", "runs", "n2.json"), `{"created":"2026-03-02T04:00:00Z","usage":`+u("claude", "haiku", 500, 100, 0)+`}`)
}

func TestEstimatesFromRecords(t *testing.T) {
	e := newEnv(t)
	writeRecords(t, e.data)
	runs := planRuns(readOwn(e.data))
	if n := len(runs[PlanCouncil]); n != 3 {
		t.Fatalf("councils %d", n)
	}
	cases := []struct {
		kind, provider, model string
		tokens                int64
		cost                  float64
		runs                  int
		basis                 string
	}{
		// A council is one run: claude's share is its two steps, median 4000.
		{PlanCouncil, "claude", "", 4000, 1.25, 3, "provider"},
		{PlanCouncil, "codex", "", 300, 1.25, 3, "provider"},
		// Grok never sat on one: the whole council stands in for it.
		{PlanCouncil, "grok", "", 4300, 1.25, 3, "kind"},
		// A Work turn: median of the sonnet turns, cache reads left out.
		{PlanWork, "claude", "sonnet", 22000, 2.0, 3, "model"},
		{PlanWork, "claude", "", 16500, 1.5, 4, "provider"},
		{PlanWork, "claude", "opus", 16500, 1.5, 4, "provider"},
		{PlanRank, "claude", "haiku", 500, 0, 2, "model"},
		{PlanSection, "claude", "", 0, 0, 0, "none"},
	}
	for _, c := range cases {
		got := estimate(runs, c.kind, c.provider, c.model)
		if got.Tokens != c.tokens || got.CostUSD != c.cost || got.Runs != c.runs || got.Basis != c.basis {
			t.Errorf("%s on %s/%s = %+v, want %d tokens $%v %d runs %s", c.kind, c.provider, c.model, got, c.tokens, c.cost, c.runs, c.basis)
		}
	}
}

// testPaces gives claude 10k tokens left before 15:00 and codex 70k.
func testPaces() []Pace {
	reset := now.Add(3 * time.Hour)
	return []Pace{
		{Provider: "claude", Account: "default", Window: "5h", CapacityTokens: 100000, UsedTokens: 90000, ResetsAt: &reset},
		{Provider: "codex", Account: "default", Window: "5h", CapacityTokens: 100000, UsedTokens: 30000, ResetsAt: &reset},
		// A second account never counts for planning on the default one.
		{Provider: "codex", Account: "other", Window: "5h", CapacityTokens: 100000, UsedTokens: 99999, ResetsAt: &reset},
	}
}

func fixedRuns(kind, provider string, tokens ...int64) map[string][]planRun {
	out := map[string][]planRun{}
	for i, n := range tokens {
		out[kind] = append(out[kind], planRun{At: now.Add(-time.Duration(i) * time.Hour), Tokens: map[string]int64{provider: n}, Models: map[string]bool{provider + "/": true}, Cost: 1.2})
	}
	return out
}

func TestPlanFitsBeforeReset(t *testing.T) {
	runs := fixedRuns(PlanWork, "codex", 30000)
	items := []PlanItem{
		{ID: "a", Title: "First", Kind: PlanWork, Provider: "codex"},
		{ID: "b", Title: "Second", Kind: PlanWork, Provider: "codex"},
		{ID: "c", Title: "Third", Kind: PlanWork, Provider: "codex"},
		{ID: "d", Title: "Describe", Kind: PlanSection, Provider: "codex"},
	}
	got, windows := Plan(PlanInput{Items: items, Paces: testPaces(), Runs: runs, Now: now, Loc: zone})
	verdicts := []string{}
	for _, it := range got {
		verdicts = append(verdicts, it.Verdict)
	}
	if strings.Join(verdicts, ",") != "fits,fits,after_reset,unknown" {
		t.Fatalf("verdicts %v", verdicts)
	}
	if got[0].Fit != "Fits before codex's 5h limit resets at 15:00." {
		t.Errorf("fit %q", got[0].Fit)
	}
	if !strings.Contains(got[2].Fit, "start it after then") || !strings.Contains(got[3].Fit, "No recent runs") {
		t.Errorf("fits %q / %q", got[2].Fit, got[3].Fit)
	}
	if got[0].Suggested || got[0].Use != "codex" {
		t.Errorf("a pinned provider is not a suggestion: %+v", got[0])
	}
	if len(windows) != 1 || windows[0].Fit != 2 || windows[0].Items != 3 || windows[0].Remaining != 70000 || windows[0].Planned != 60000 {
		t.Fatalf("windows %+v", windows)
	}
	if want := "Before codex's 5h limit resets at 15:00: 2 of 3 planned items fit, using about 60k of the 70k tokens left."; windows[0].Summary != want {
		t.Errorf("window summary %q", windows[0].Summary)
	}

	// More than a whole window: split it up.
	big, _ := Plan(PlanInput{Items: items[:1], Paces: testPaces(), Runs: fixedRuns(PlanWork, "codex", 150000), Now: now, Loc: zone})
	if big[0].Verdict != "too_big" {
		t.Errorf("too big: %+v", big[0])
	}
	// No window known for grok: unknown, not a guess.
	g, _ := Plan(PlanInput{Items: []PlanItem{{ID: "g", Title: "G", Kind: PlanWork, Provider: "grok"}}, Paces: testPaces(), Runs: fixedRuns(PlanWork, "grok", 10), Now: now, Loc: zone})
	if g[0].Verdict != "unknown" || !strings.Contains(g[0].Fit, "grok reports no limit window") {
		t.Errorf("grok: %+v", g[0])
	}
}

func TestPlanSuggestsProviderFromHeadroomAndTeam(t *testing.T) {
	runs := map[string][]planRun{}
	for _, p := range []string{"claude", "codex"} {
		for k, v := range fixedRuns(PlanWork, p, 30000) {
			runs[k] = append(runs[k], v...)
		}
	}
	budget := 1.0
	team := func(project string) []TeamSeat {
		if project == "solo" {
			return nil
		}
		return []TeamSeat{{Role: "proposer", Provider: "claude"}, {Role: "builder", Provider: "claude", Model: "", BudgetUSD: &budget}}
	}
	items := []PlanItem{{ID: "w", Title: "Build it", Kind: PlanWork, Project: "demo"}}

	// The builder is claude, but claude has 10k left: codex has room.
	got, _ := Plan(PlanInput{Items: items, Paces: testPaces(), Runs: runs, Team: team, Now: now, Loc: zone})
	w := got[0]
	if w.Use != "codex" || !w.Suggested || w.Verdict != "fits" || !strings.Contains(w.Reason, "claude's 5h limit has about 10k tokens left, not enough; codex has room") {
		t.Fatalf("suggestion %+v", w)
	}
	// The builder's budget is claude's, so codex's cost does not trip it.
	if len(w.Warnings) != 0 {
		t.Errorf("warnings %v", w.Warnings)
	}

	// With room on claude the team's builder is kept, and its budget warns.
	roomy := testPaces()
	roomy[0].UsedTokens = 0
	got, _ = Plan(PlanInput{Items: items, Paces: roomy, Runs: runs, Team: team, Now: now, Loc: zone})
	w = got[0]
	if w.Use != "claude" || w.Reason != "your team's builder" || w.Verdict != "fits" {
		t.Fatalf("team kept: %+v", w)
	}
	if len(w.Warnings) != 1 || w.Warnings[0] != "A Work turn on claude has cost about $1.20 lately, over the builder's $1.00 budget." {
		t.Errorf("budget warning %v", w.Warnings)
	}

	// Two items: the first takes codex's room, so the second waits.
	two := []PlanItem{{ID: "a", Title: "A", Kind: PlanWork, Project: "demo"}, {ID: "b", Title: "B", Kind: PlanWork, Project: "demo"}, {ID: "c", Title: "C", Kind: PlanWork, Project: "demo"}}
	got, _ = Plan(PlanInput{Items: two, Paces: testPaces(), Runs: runs, Team: team, Available: []string{"claude", "codex"}, Now: now, Loc: zone})
	if got[0].Use != "codex" || got[1].Use != "codex" || got[2].Verdict != "after_reset" || got[2].Reason != "no provider has room before its reset" {
		t.Errorf("sequence: %+v / %+v / %+v", got[0], got[1], got[2])
	}

	// Only signed-in providers are suggested.
	got, _ = Plan(PlanInput{Items: items, Paces: testPaces(), Runs: runs, Team: team, Available: []string{"claude"}, Now: now, Loc: zone})
	if got[0].Use != "claude" || got[0].Verdict != "after_reset" {
		t.Errorf("only claude: %+v", got[0])
	}

	// A council warns against the sum of its seats' budgets, when all are set.
	cb := 0.5
	councilTeam := func(string) []TeamSeat {
		return []TeamSeat{{Role: "proposer", Provider: "codex", BudgetUSD: &cb}, {Role: "critic", Provider: "codex", BudgetUSD: &cb}}
	}
	got, _ = Plan(PlanInput{Items: []PlanItem{{ID: "k", Title: "Brief", Kind: PlanCouncil}}, Paces: testPaces(), Runs: fixedRuns(PlanCouncil, "codex", 1000), Team: councilTeam, Now: now, Loc: zone})
	if got[0].Use != "codex" || len(got[0].Warnings) != 1 || !strings.Contains(got[0].Warnings[0], "over the $1.00 its seats' budgets add up to") {
		t.Errorf("council budget: %+v", got[0])
	}
}

func TestPlanRoutes(t *testing.T) {
	e := newEnv(t)
	writeRecords(t, e.data)
	mux := http.NewServeMux()
	Register(mux, e.svc)
	put := func(body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/usage/plan", strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := put(`{"items":[]}`, false); rec.Code == 200 {
		t.Errorf("unconfirmed PUT: %d", rec.Code)
	}
	for _, bad := range []string{
		`{"items":[{"title":"x","kind":"spaceship"}]}`,
		`{"items":[{"title":" ","kind":"work"}]}`,
		`{"items":[{"title":"x","kind":"work","provider":"cursor"}]}`,
		`{"items":[{"id":"a","title":"x","kind":"work"},{"id":"a","title":"y","kind":"work"}]}`,
		`{"items":[],"extra":1}`,
	} {
		if rec := put(bad, true); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	rec := put(`{"items":[{"title":"Ship the changelog","kind":"work","provider":"claude","model":"sonnet"},{"title":"Rank","kind":"rank","source":"nextup","nextup_id":"card:x"}]}`, true)
	if rec.Code != 200 {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(e.data, "usage", "plan.json"))
	if err != nil || !strings.Contains(string(b), "Ship the changelog") {
		t.Fatalf("plan.json: %v %s", err, b)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/usage/plan?hours=3", nil))
	var v PlanView
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &v) != nil {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	if v.Hours != 3 || len(v.Items) != 2 || v.Items[0].ID == "" || v.Items[0].Estimate.Tokens != 22000 || v.Items[1].Source != "nextup" || v.Updated == nil {
		t.Errorf("view %+v", v)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/usage/plan?hours=99", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("hours=99: %d", rec.Code)
	}
}
