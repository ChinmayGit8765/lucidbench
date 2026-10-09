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
	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// zone is a fixed zone ahead of UTC, so a late-evening UTC event lands on the
// next local day.
var zone = time.FixedZone("test", 10*3600)

// now is 2026-03-03 12:00 local.
var now = time.Date(2026, 3, 3, 12, 0, 0, 0, zone)

const secret = "SECRET-PROMPT-TEXT-do-not-leak"

type env struct {
	home, data string
	svc        *Service
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{home: t.TempDir(), data: t.TempDir()}
	e.svc = &Service{
		Roots:   func() accounts.Roots { return accounts.Roots{Home: e.home} },
		DataDir: e.data,
		Loc:     zone,
		Now:     func() time.Time { return now },
	}
	return e
}

func write(t *testing.T, p string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, p string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		t.Fatal(err)
	}
}

func claudeMsg(id, ts string, in, out, cr, cw int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"cwd":"/work/%s","message":{"id":%q,"model":"model-a","content":[{"type":"text","text":%q}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d}}}`,
		ts, secret, id, secret, in, out, cr, cw)
}

func provider(t *testing.T, s *Summary, id string) Provider {
	t.Helper()
	for _, p := range s.Providers {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no provider %s", id)
	return Provider{}
}

func day(p Provider, date string) Day {
	for _, d := range p.Daily {
		if d.Date == date {
			return d
		}
	}
	return Day{}
}

func TestClaudeDedupeBucketsAndProjects(t *testing.T) {
	e := newEnv(t)
	proj := filepath.Join(e.home, ".claude", "projects", encodeHome(e.home)+"-myapp")
	home := filepath.Join(e.home, ".claude", "projects", encodeHome(e.home))
	write(t, filepath.Join(proj, "a.jsonl"),
		// 20:00 UTC on Mar 1 is 06:00 on Mar 2 in the test zone.
		claudeMsg("m1", "2026-03-01T20:00:00Z", 10, 1, 100, 5),
		// The same message logged again while it streams: the larger one wins.
		claudeMsg("m1", "2026-03-01T20:00:01Z", 10, 7, 100, 5),
		// 10:00 UTC on Mar 1 is 20:00 on Mar 1 locally.
		claudeMsg("m2", "2026-03-01T10:00:00Z", 1, 2, 3, 4),
		`{"type":"user","timestamp":"2026-03-01T10:00:00Z","message":{"content":"`+secret+`"}}`,
		`not json at all`,
		// Older than the range asked for below.
		claudeMsg("old", "2025-01-01T00:00:00Z", 999, 999, 999, 999),
	)
	// A resumed session repeats m1 in another file, and sub-agent logs sit deeper.
	write(t, filepath.Join(proj, "sess", "subagents", "b.jsonl"),
		claudeMsg("m1", "2026-03-01T20:00:00Z", 10, 7, 100, 5),
		claudeMsg("m3", "2026-03-03T01:00:00Z", 5, 5, 5, 5),
	)
	write(t, filepath.Join(home, "c.jsonl"), claudeMsg("m4", "2026-03-03T01:00:00Z", 1, 1, 1, 1))

	s := e.svc.Summary(7)
	c := provider(t, s, "claude")
	if c.Status != "ok" || c.Sources != 3 {
		t.Fatalf("status %q sources %d", c.Status, c.Sources)
	}
	if d := day(c, "2026-03-02"); d.Input != 10 || d.Output != 7 || d.CacheRead != 100 || d.CacheWrite != 5 || d.Total != 122 {
		t.Errorf("Mar 2 (m1 once, largest copy) = %+v", d)
	}
	if d := day(c, "2026-03-01"); d.Total != 10 {
		t.Errorf("Mar 1 (m2) = %+v", d)
	}
	if d := day(c, "2026-03-03"); d.Total != 20+4 {
		t.Errorf("Mar 3 (m3, m4) = %+v", d)
	}
	if c.Totals.Total != 122+10+24 {
		t.Errorf("total %d", c.Totals.Total)
	}
	if len(c.Daily) != 7 || c.Daily[6].Date != "2026-03-03" || c.Daily[0].Date != "2026-02-25" {
		t.Errorf("daily range %v..%v (%d)", c.Daily[0].Date, c.Daily[len(c.Daily)-1].Date, len(c.Daily))
	}
	names := map[string]int64{}
	for _, b := range c.Projects {
		names[b.Name] = b.Total
	}
	if names["myapp"] != 122+10+20 || names["~"] != 4 || len(names) != 2 {
		t.Errorf("projects %v", names)
	}
	if len(c.Models) != 1 || c.Models[0].Name != "model-a" {
		t.Errorf("models %+v", c.Models)
	}
	if c.Totals.CostUSD != 0 {
		t.Errorf("claude cost must not be invented: %v", c.Totals.CostUSD)
	}
	if c.WindowNote == "" || len(c.Windows) != 0 {
		t.Errorf("claude window should be unknown: %+v %q", c.Windows, c.WindowNote)
	}
}

func TestNoMessageTextOrPathsInOutput(t *testing.T) {
	e := newEnv(t)
	proj := filepath.Join(e.home, ".claude", "projects", encodeHome(e.home)+"-myapp")
	write(t, filepath.Join(proj, "a.jsonl"), claudeMsg("m1", "2026-03-02T01:00:00Z", 1, 1, 1, 1))
	write(t, filepath.Join(e.home, ".codex", "sessions", "2026", "03", "02", "rollout-x.jsonl"),
		`{"timestamp":"2026-03-02T01:00:00Z","type":"session_meta","payload":{"cwd":"/work/`+secret+`/proj"}}`,
		`{"timestamp":"2026-03-02T01:00:01Z","type":"response_item","payload":{"type":"message","content":"`+secret+`"}}`,
		`{"timestamp":"2026-03-02T01:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}},"rate_limits":null}}`,
	)
	b, err := json.Marshal(e.svc.Summary(30))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, "do-not-leak") {
		t.Errorf("message text or cwd path leaked into the output: %s", out)
	}
	if strings.Contains(out, filepath.ToSlash(e.home)) || strings.Contains(out, e.home) {
		t.Errorf("home directory leaked into the output")
	}
	for _, w := range []string{"myapp", "proj"} {
		if !strings.Contains(out, w) {
			t.Errorf("folder name %q missing", w)
		}
	}
	// Cache file must not hold message text either.
	raw, _ := os.ReadFile(filepath.Join(e.data, "usage-cache.gob"))
	if strings.Contains(string(raw), "do-not-leak") {
		t.Errorf("message text leaked into the cache")
	}
}

func codexTok(ts string, in, cached, out, total int, rl string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"total_tokens":%d}},"rate_limits":%s}}`,
		ts, in, cached, out, total, rl)
}

func TestCodexTotalsAndRateLimits(t *testing.T) {
	e := newEnv(t)
	p1 := filepath.Join(e.home, ".codex", "sessions", "2026", "03", "02", "rollout-a.jsonl")
	write(t, p1,
		`{"timestamp":"2026-03-02T01:00:00Z","type":"session_meta","payload":{"cwd":"C:\\work\\widget"}}`,
		`{"timestamp":"2026-03-02T01:00:00Z","type":"turn_context","payload":{"model":"gpt-x"}}`,
		// null info and null rate limits: nothing to count, nothing to learn.
		`{"timestamp":"2026-03-02T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":null}}`,
		codexTok("2026-03-02T01:00:02Z", 100, 40, 10, 110, `{"primary":{"used_percent":12.5,"window_minutes":300,"resets_at":1772500000},"secondary":{"used_percent":3,"window_minutes":10080,"resets_at":1773000000}}`),
		// A repeated event adds nothing.
		codexTok("2026-03-02T01:00:03Z", 100, 40, 10, 110, `null`),
		// 20:00 UTC Mar 2 is Mar 3 locally. Its rate_limits has a null secondary.
		codexTok("2026-03-02T20:00:00Z", 160, 60, 30, 190, `{"primary":{"used_percent":20,"window_minutes":300,"resets_at":1772400000},"secondary":null}`),
		// A later event without rate limits must not erase what is known.
		codexTok("2026-03-02T21:00:00Z", 160, 60, 30, 190, `null`),
	)
	// Another session with no windows and a restarted total.
	write(t, filepath.Join(e.home, ".codex", "sessions", "2026", "03", "02", "rollout-b.jsonl"),
		// A session started in the home directory is "~", not the user's name.
		fmt.Sprintf(`{"timestamp":"2026-03-02T01:00:00Z","type":"session_meta","payload":{"cwd":%q}}`, strings.ToUpper(e.home)),
		codexTok("2026-03-02T02:00:00Z", 8, 0, 2, 10, `null`),
		codexTok("2026-03-02T03:00:00Z", 3, 0, 1, 4, `null`),
	)
	s := e.svc.Summary(7)
	c := provider(t, s, "codex")
	if c.Status != "ok" || c.Sources != 2 {
		t.Fatalf("status %q sources %d", c.Status, c.Sources)
	}
	// Mar 2 local: session a first event (in 60, cached 40, out 10) + b (8+3 in, 2+1 out).
	if d := day(c, "2026-03-02"); d.Input != 60+8+3 || d.CacheRead != 40 || d.Output != 10+2+1 {
		t.Errorf("Mar 2 = %+v", d)
	}
	// Mar 3 local: a's growth (in 60-20=40, cached 20, out 20).
	if d := day(c, "2026-03-03"); d.Input != 40 || d.CacheRead != 20 || d.Output != 20 {
		t.Errorf("Mar 3 = %+v", d)
	}
	if len(c.Windows) != 1 {
		t.Fatalf("windows %+v", c.Windows)
	}
	w := c.Windows[0]
	if w.Name != "primary" || w.Label != "5h" || w.UsedPercent == nil || *w.UsedPercent != 20 || w.ResetsAt == nil || w.ResetsAt.Unix() != 1772400000 {
		t.Errorf("latest window = %+v", w)
	}
	if !w.Stale {
		t.Errorf("a window whose reset is in the past must be marked stale")
	}
	proj := map[string]bool{}
	for _, b := range c.Projects {
		proj[b.Name] = true
	}
	if len(proj) != 2 || !proj["widget"] || !proj["~"] {
		t.Errorf("projects %+v", c.Projects)
	}
	if c.Totals.CostUSD != 0 {
		t.Errorf("codex cost must not be invented")
	}
}

func TestCodexWindowsWithNullFieldsAreUnknown(t *testing.T) {
	e := newEnv(t)
	write(t, filepath.Join(e.home, ".codex", "sessions", "rollout-n.jsonl"),
		codexTok("2026-03-02T01:00:02Z", 1, 0, 1, 2, `{"primary":{"used_percent":null,"window_minutes":null,"resets_at":null},"secondary":null}`),
	)
	c := provider(t, e.svc.Summary(7), "codex")
	if len(c.Windows) != 1 || c.Windows[0].UsedPercent != nil || c.Windows[0].ResetsAt != nil || c.Windows[0].WindowMinutes != nil || c.Windows[0].Label != "primary" {
		t.Errorf("null window fields should stay unknown: %+v", c.Windows)
	}
	b, _ := json.Marshal(c.Windows)
	if !strings.Contains(string(b), `"used_percent":null`) {
		t.Errorf("unknown must serialise as null: %s", b)
	}
}

func TestCacheIsReusedAndGrowsIncrementally(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(e.home, ".claude", "projects", "proj-x", "a.jsonl")
	write(t, p, claudeMsg("m1", "2026-03-02T01:00:00Z", 1, 1, 1, 1))
	if got := provider(t, e.svc.Summary(7), "claude").Totals.Total; got != 4 {
		t.Fatalf("first read %d", got)
	}
	if _, err := os.Stat(filepath.Join(e.data, "usage-cache.gob")); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	info, _ := os.Stat(p)

	// Same size and mtime, different content: only the cache can answer.
	same := strings.Replace(claudeMsg("m1", "2026-03-02T01:00:00Z", 1, 1, 1, 1), `"input_tokens":1`, `"input_tokens":9`, 1)
	write(t, p, same)
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	fresh := &Service{Roots: e.svc.Roots, DataDir: e.data, Loc: zone, Now: e.svc.Now} // a restart: cache loaded from disk
	if got := provider(t, fresh.Summary(7), "claude").Totals.Total; got != 4 {
		t.Errorf("unchanged file should come from the cache, got %d", got)
	}

	// Back to the real content, so only growth differs from the cache.
	write(t, p, claudeMsg("m1", "2026-03-02T01:00:00Z", 1, 1, 1, 1))
	if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	// Growth is read from the old offset: m1 is not counted twice.
	appendTo(t, p, claudeMsg("m2", "2026-03-02T02:00:00Z", 2, 2, 2, 2))
	if got := provider(t, fresh.Summary(7), "claude").Totals.Total; got != 4+8 {
		t.Errorf("after append total = %d, want 12", got)
	}

	// A rewritten file (new head) is read again from the start.
	write(t, p, claudeMsg("z9", "2026-03-02T05:00:00Z", 5, 5, 5, 5), claudeMsg("z8", "2026-03-02T06:00:00Z", 1, 0, 0, 0))
	if got := provider(t, fresh.Summary(7), "claude").Totals.Total; got != 21 {
		t.Errorf("after rewrite total = %d, want 21", got)
	}

	// A deleted file drops out.
	os.Remove(p)
	if c := provider(t, fresh.Summary(7), "claude"); c.Totals.Total != 0 || c.Status != "missing" {
		t.Errorf("after delete: %+v", c)
	}
}

func TestPartialLastLineIsReadLater(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(e.home, ".claude", "projects", "proj-x", "a.jsonl")
	full := claudeMsg("m2", "2026-03-02T02:00:00Z", 2, 2, 2, 2)
	write(t, p, claudeMsg("m1", "2026-03-02T01:00:00Z", 1, 1, 1, 1))
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(full[:len(full)/2]) // half written
	f.Close()
	if got := provider(t, e.svc.Summary(7), "claude").Totals.Total; got != 4 {
		t.Fatalf("partial line counted: %d", got)
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(full[len(full)/2:] + "\n")
	f.Close()
	if got := provider(t, e.svc.Summary(7), "claude").Totals.Total; got != 12 {
		t.Errorf("completed line total = %d, want 12", got)
	}
}

func TestOwnRuns(t *testing.T) {
	e := newEnv(t)
	write(t, filepath.Join(e.data, "council", "c1.json"),
		`{"id":"c1","started":"2026-03-02T01:00:00Z","usage":[{"provider":"claude","model":"m","input_tokens":10,"output_tokens":5,"cost_usd":0.25,"duration_ms":1},{"provider":"codex","input_tokens":3,"output_tokens":1,"duration_ms":1}]}`)
	write(t, filepath.Join(e.data, "work", "sessions", "w1", "session.json"),
		`{"id":"w1","started":"2026-03-03T01:00:00Z","usage":{"provider":"claude","input_tokens":7,"output_tokens":3,"cache_read_tokens":100,"cost_usd":0.5,"duration_ms":2}}`)
	write(t, filepath.Join(e.data, "work", "sessions", "w2", "session.json"), `{"id":"w2","status":"running"}`)
	write(t, filepath.Join(e.data, "council", "bad.json"), `{nope`)
	write(t, filepath.Join(e.data, "prompts", "runs", "r1.json"),
		`{"created":"2026-03-03T02:00:00Z","usage":{"provider":"claude","model":"haiku","input_tokens":4,"output_tokens":2,"cost_usd":0.125,"duration_ms":1}}`)
	write(t, filepath.Join(e.data, "sections", "runs", "s1.json"),
		`{"kind":"section","created":"2026-03-02T02:00:00Z","usage":{"provider":"claude","model":"haiku","input_tokens":2,"output_tokens":1,"cost_usd":0.125,"duration_ms":1}}`)
	write(t, filepath.Join(e.data, "nextup", "runs", "n1.json"),
		`{"kind":"rank","created":"2026-03-02T03:00:00Z","usage":{"provider":"claude","model":"haiku","input_tokens":8,"output_tokens":4,"duration_ms":1}}`)

	o := e.svc.Summary(7).Lucidbench
	if o.Runs != 6 || o.Totals.Input != 34 || o.Totals.Output != 16 || o.Totals.CacheRead != 100 {
		t.Errorf("own totals %+v runs %d", o.Totals, o.Runs)
	}
	if o.Totals.CostUSD != 1 {
		t.Errorf("cost %v", o.Totals.CostUSD)
	}
	if d := day(Provider{Daily: o.Daily}, "2026-03-03"); d.Input != 11 || d.CostUSD != 0.625 { // the work run and the studio run
		t.Errorf("Mar 3 %+v", d)
	}
	src := map[string]int64{}
	for _, b := range o.Sources {
		src[b.Name] = b.Total
	}
	if src["council"] != 19 || src["work"] != 110 || src["studio"] != 6 || src["sections"] != 3 || src["nextup"] != 12 {
		t.Errorf("sources %v", src)
	}
}

func TestOwnRunsCountTheAssistant(t *testing.T) {
	e := newEnv(t)
	write(t, filepath.Join(e.data, "assistant", "runs", "a1.json"),
		`{"kind":"chat","conversation":"c","created":"2026-03-03T03:00:00Z","usage":{"provider":"claude","model":"haiku","input_tokens":5,"output_tokens":2,"cost_usd":0.25,"duration_ms":1}}`)
	write(t, filepath.Join(e.data, "assistant", "c.jsonl"), `{"type":"meta"}`)
	o := e.svc.Summary(7).Lucidbench
	if o.Runs != 1 || o.Totals.CostUSD != 0.25 {
		t.Errorf("runs %d cost %v", o.Runs, o.Totals.CostUSD)
	}
	if len(o.Sources) != 1 || o.Sources[0].Name != "assistant" || o.Sources[0].Total != 7 {
		t.Errorf("sources %+v", o.Sources)
	}
}

// TestOwnRunsFromRealRecords writes records the way Council and Work write
// them (their own structs, marshalled), so a renamed field in either package
// fails here instead of leaving the Lucidbench row silently empty.
func TestOwnRunsFromRealRecords(t *testing.T) {
	e := newEnv(t)
	created := time.Date(2026, 3, 2, 9, 0, 0, 0, zone)
	cs := council.Session{
		ID: "c-real", Status: council.StatusApproved, Created: created, Updated: created.Add(30 * time.Hour),
		Usage: []agentexec.Usage{
			{Provider: "claude", Model: "m", InputTokens: 10, OutputTokens: 5, CacheRead: 1, CacheWrite: 2, CostUSD: 0.25, DurationMS: 1},
			{Provider: "codex", InputTokens: 3, OutputTokens: 1, DurationMS: 1},
		},
	}
	b, err := json.Marshal(cs)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.data, "council", cs.ID+".json"), string(b))

	ended := created.Add(26 * time.Hour)
	ws := work.Session{
		ID: "w-real", Provider: "claude", Status: "done", Started: created.Add(25 * time.Hour), Ended: &ended,
		Usage: &agentexec.Usage{Provider: "claude", InputTokens: 7, OutputTokens: 3, CacheRead: 100, CostUSD: 0.5, DurationMS: 2, Note: "n"},
	}
	if b, err = json.Marshal(ws); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.data, "work", "sessions", ws.ID, "session.json"), string(b))
	// A session still running has reported no usage yet: it is not a run.
	running, _ := json.Marshal(work.Session{ID: "w-run", Status: "running", Started: created})
	write(t, filepath.Join(e.data, "work", "sessions", "w-run", "session.json"), string(running))

	o := e.svc.Summary(7).Lucidbench
	if o.Runs != 3 || o.Totals.Input != 20 || o.Totals.Output != 9 || o.Totals.CacheRead != 101 || o.Totals.CacheWrite != 2 {
		t.Errorf("own totals %+v runs %d", o.Totals, o.Runs)
	}
	if o.Totals.CostUSD != 0.75 {
		t.Errorf("cost %v", o.Totals.CostUSD)
	}
	// Council runs count on the day the session was created, work on the day it started.
	if d := day(Provider{Daily: o.Daily}, "2026-03-02"); d.Input != 13 || d.CostUSD != 0.25 {
		t.Errorf("Mar 2 %+v", d)
	}
	if d := day(Provider{Daily: o.Daily}, "2026-03-03"); d.Input != 7 || d.CostUSD != 0.5 {
		t.Errorf("Mar 3 %+v", d)
	}
}

func TestEmptyMachineAndUnavailableProviders(t *testing.T) {
	e := newEnv(t)
	s := e.svc.Summary(30)
	if len(s.Providers) != 4 {
		t.Fatalf("providers %d", len(s.Providers))
	}
	for _, id := range []string{"grok", "cursor"} {
		p := provider(t, s, id)
		if p.Status != "unavailable" || !strings.Contains(p.Note, "Not available yet") || p.Totals.Total != 0 || len(p.Daily) != 30 {
			t.Errorf("%s: %+v", id, p)
		}
	}
	if p := provider(t, s, "claude"); p.Status != "missing" {
		t.Errorf("claude %q", p.Status)
	}
	if s.Lucidbench.Runs != 0 || len(s.Lucidbench.Daily) != 30 {
		t.Errorf("own %+v", s.Lucidbench)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "null") {
		t.Errorf("empty slices must serialise as [] not null: %s", b)
	}
}

func TestDisabledProviderIsSkipped(t *testing.T) {
	e := newEnv(t)
	write(t, filepath.Join(e.home, ".claude", "projects", "p", "a.jsonl"), claudeMsg("m1", "2026-03-02T01:00:00Z", 1, 1, 1, 1))
	e.svc.Roots = func() accounts.Roots {
		return accounts.Roots{Home: e.home, Disabled: map[string]bool{"claude": true}}
	}
	if c := provider(t, e.svc.Summary(7), "claude"); c.Status != "disabled" || c.Totals.Total != 0 {
		t.Errorf("%+v", c)
	}
}

func TestExtraClaudeDirAndProjectName(t *testing.T) {
	e := newEnv(t)
	extra := t.TempDir()
	write(t, filepath.Join(extra, "projects", "proj-y", "a.jsonl"), claudeMsg("e1", "2026-03-02T01:00:00Z", 1, 1, 1, 1))
	e.svc.Roots = func() accounts.Roots {
		return accounts.Roots{Home: e.home, ExtraClaudeDirs: []string{extra, extra}}
	}
	c := provider(t, e.svc.Summary(7), "claude")
	if c.Totals.Total != 4 || c.Sources != 1 || c.Projects[0].Name != "proj-y" {
		t.Errorf("%+v", c)
	}
	if got := projectName("D--work-me-code-app", `D:\work\me`); got != "code-app" {
		t.Errorf("projectName %q", got)
	}
	if got := projectName("-srv-me", "/srv/me"); got != "~" {
		t.Errorf("projectName %q", got)
	}
}

func TestHandler(t *testing.T) {
	e := newEnv(t)
	mux := http.NewServeMux()
	Register(mux, e.svc)
	get := func(q string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/usage/summary"+q, nil))
		return w
	}
	w := get("?days=30")
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	var s Summary
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || s.Days != 30 || s.GeneratedAt.IsZero() {
		t.Errorf("body %v %+v", err, s)
	}
	if get("").Code != 200 {
		t.Errorf("default days")
	}
	for _, q := range []string{"?days=0", "?days=x", "?days=500"} {
		if c := get(q).Code; c != 400 {
			t.Errorf("%s -> %d", q, c)
		}
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/usage/summary", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST -> %d", w.Code)
	}
}
