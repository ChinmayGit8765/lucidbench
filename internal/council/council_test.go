package council

import (
	"bufio"
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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The test binary doubles as the provider CLIs: copied onto PATH as claude,
// codex and grok and run with LUCID_FAKE_COUNCIL set, it answers each call
// with the next scripted reply for its provider and records what it was
// asked. Each provider has its own queue, so critics running in parallel
// cannot take each other's replies.
func TestMain(m *testing.M) {
	if dir := os.Getenv("LUCID_FAKE_COUNCIL"); dir != "" {
		os.Exit(fakeCLI(dir))
	}
	code := m.Run()
	if master != "" {
		os.RemoveAll(master)
	}
	os.Exit(code)
}

// master holds one copy of the test binary. Each test links it onto its own
// PATH: a fresh copy per test is slow on hosts that scan new executables.
var master string

func masterCopy(t *testing.T) string {
	t.Helper()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	if master == "" {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		dir, err := os.MkdirTemp("", "lucid-council-fake-")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fake"+exe), data, 0o755); err != nil {
			t.Fatal(err)
		}
		master = dir
	}
	return filepath.Join(master, "fake"+exe)
}

// authReply makes the fake answer like a CLI that is not signed in.
const authReply = "!auth"

// failReply makes the fake fail the way a broken call does.
const failReply = "!fail"

func fakeCLI(dir string) int {
	provider := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	in, _ := io.ReadAll(os.Stdin)
	pdir := filepath.Join(dir, provider)
	n := 1
	if b, err := os.ReadFile(filepath.Join(pdir, "count")); err == nil {
		n, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		n++
	}
	_ = os.WriteFile(filepath.Join(pdir, "count"), []byte(strconv.Itoa(n)), 0o600)
	prompt := string(in)
	if provider == "grok" {
		for i, a := range os.Args {
			if a == "-p" && i+1 < len(os.Args) {
				prompt = os.Args[i+1]
			}
		}
	}
	_ = os.WriteFile(filepath.Join(pdir, fmt.Sprintf("call-%d.txt", n)), []byte(prompt), 0o600)

	reply, err := os.ReadFile(filepath.Join(pdir, fmt.Sprintf("%d.txt", n)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fake %s: no reply %d queued\n", provider, n)
		return 3
	}
	text := string(reply)
	out := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}
	type m = map[string]any
	if text == failReply {
		fmt.Fprintln(os.Stderr, "upstream error 500")
		return 2
	}
	if text == authReply {
		if provider == "claude" {
			out(m{"type": "result", "is_error": true, "result": "Not logged in · Please run /login"})
		} else {
			fmt.Fprintln(os.Stderr, "Not logged in. Please log in first.")
		}
		return 1
	}
	switch provider {
	case "claude":
		out(m{"type": "result", "is_error": false, "result": text, "total_cost_usd": 0.02,
			"usage": m{"input_tokens": 100, "output_tokens": 50}, "modelUsage": m{"claude-test": m{}}})
	case "codex":
		for i, a := range os.Args {
			if a == "-o" && i+1 < len(os.Args) {
				_ = os.WriteFile(os.Args[i+1], []byte(text), 0o600)
			}
		}
		out(m{"type": "turn.completed", "usage": m{"input_tokens": 80, "cached_input_tokens": 0, "output_tokens": 20}})
	case "grok":
		out(m{"result": text, "usage": m{"input_tokens": 70, "output_tokens": 10}})
	}
	return 0
}

// fakes installs the named providers on a fresh PATH, each with its queue of
// replies, and returns the script dir.
func fakes(t *testing.T, replies map[string][]string) string {
	t.Helper()
	src := masterCopy(t)
	bin, script := t.TempDir(), t.TempDir()
	for p, rs := range replies {
		dst := filepath.Join(bin, p)
		if runtime.GOOS == "windows" {
			dst += ".exe"
		}
		if err := os.Link(src, dst); err != nil {
			data, rerr := os.ReadFile(src)
			if rerr != nil {
				t.Fatal(rerr)
			}
			if err := os.WriteFile(dst, data, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		pdir := filepath.Join(script, p)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, r := range rs {
			if err := os.WriteFile(filepath.Join(pdir, fmt.Sprintf("%d.txt", i+1)), []byte(r), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("LUCID_FAKE_COUNCIL", script)
	return script
}

// calls returns the prompts a provider received, in order.
func calls(t *testing.T, script, provider string) []string {
	t.Helper()
	var out []string
	for i := 1; ; i++ {
		b, err := os.ReadFile(filepath.Join(script, provider, fmt.Sprintf("call-%d.txt", i)))
		if err != nil {
			return out
		}
		out = append(out, string(b))
	}
}

func brief(title string) string {
	return "# " + title + `

## Problem
Stale folders pile up.

## Outcome
They are cleaned up safely.

## Done criteria
- [ ] D1 — old folders are removed — proof: go test ./internal/x
- [ ] D2 — live folders stay — proof: a test that holds one open

## Risks
- Deleting a live folder.

## First steps
1. Find where folders are made.

## Open questions
- How old is stale?
`
}

func critique(verdict string, points ...Point) string {
	b, _ := json.Marshal(map[string]any{"verdict": verdict, "points": points})
	return string(b)
}

var (
	ok      = critique(VerdictOK)
	concern = critique(VerdictConcerns, Point{SeverityMajor, "Say how old is stale."})
	block   = critique(VerdictBlocker, Point{SeverityBlocker, "This could delete a running session's folder."})
)

func testProjects() (*projects.List, error) {
	return &projects.List{Configured: true, Projects: []projects.Project{
		{ID: "demo-app", Name: "Demo app", Category: "product", Type: "cli", Status: "active", Visibility: "public", Summary: "A demo."},
		{ID: "secret-app", Name: "Secret app", Category: "product", Type: "cli", Status: "active", Visibility: "confidential"},
	}}, nil
}

func newService(t *testing.T) (*Service, *memory.Vault) {
	t.Helper()
	v, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 3, 4, 10, 0, 0, 0, time.Local)
	return &Service{
		Dir:      filepath.Join(t.TempDir(), "council"),
		Vault:    func() (*memory.Vault, error) { return v, nil },
		Projects: testProjects,
		Timeout:  time.Minute,
		Now:      func() time.Time { return day },
	}, v
}

const dump = "Stale run folders are left behind when a run crashes; clean them up safely."

func start(t *testing.T, s *Service, in StartRequest) *Session {
	t.Helper()
	sess, err := s.Start(context.Background(), in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != StatusDraft {
		t.Fatalf("status = %s, error %q, log %+v", sess.Status, sess.Error, sess.Log)
	}
	return sess
}

func TestEarlyStop(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Clean up stale run folders"), brief("Clean up stale run folders safely")},
		"codex":  {concern},
		"grok":   {ok},
	})
	s, v := newService(t)
	var updates int
	sess, err := s.Start(context.Background(), StartRequest{Input: dump, Project: "demo-app"}, func(Session) { updates++ })
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != StatusDraft || len(sess.Rounds) != 1 || !sess.StoppedEarly || sess.Mode != ModeCouncil {
		t.Fatalf("session = status %s rounds %d early %v mode %s err %q", sess.Status, len(sess.Rounds), sess.StoppedEarly, sess.Mode, sess.Error)
	}
	r := sess.Rounds[0]
	if r.Proposal == nil || r.Proposal.Provider != "claude" || r.Synthesis == nil || len(r.Critiques) != 2 {
		t.Fatalf("round = %+v", r)
	}
	verdicts := map[string]string{}
	for _, c := range r.Critiques {
		verdicts[c.Provider] = c.Verdict
	}
	if verdicts["codex"] != VerdictConcerns || verdicts["grok"] != VerdictOK {
		t.Errorf("verdicts = %v", verdicts)
	}
	if len(sess.Usage) != 4 || updates == 0 {
		t.Errorf("usage entries %d, updates %d", len(sess.Usage), updates)
	}
	if got := calls(t, script, "claude"); len(got) != 2 || !strings.Contains(got[1], "Say how old is stale.") || !strings.Contains(got[0], "Demo app") {
		t.Errorf("claude prompts = %q", got)
	}

	// The brief is a draft page in the Inbox with the contract's front matter.
	if sess.BriefPath != "Inbox/2026-03-04-clean-up-stale-run-folders-safely.md" || sess.Title != "Clean up stale run folders safely" {
		t.Fatalf("brief path %q title %q", sess.BriefPath, sess.Title)
	}
	pg, err := v.Read(sess.BriefPath)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Front["type"] != "brief" || pg.Front["status"] != "draft" || pg.Front["project"] != "demo-app" || pg.Front["council"] != sess.ID {
		t.Errorf("front = %v", pg.Front)
	}
	if len(sess.Warnings) != 0 {
		t.Errorf("warnings = %v", sess.Warnings)
	}
}

func TestTwoRoundCap(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Draft"), brief("Revision one"), brief("Revision two")},
		"codex":  {block, block},
		"grok":   {ok, concern},
	})
	s, _ := newService(t)
	sess := start(t, s, StartRequest{Input: dump})
	if len(sess.Rounds) != 2 || sess.StoppedEarly || sess.Title != "Revision two" {
		t.Fatalf("rounds %d early %v title %q", len(sess.Rounds), sess.StoppedEarly, sess.Title)
	}
	if n := len(calls(t, script, "claude")); n != 3 {
		t.Errorf("claude calls = %d, want 3 (propose + 2 syntheses)", n)
	}
	var round2 bool
	for _, e := range sess.Log {
		if e.Kind == "round" && e.Round == 2 {
			round2 = true
		}
	}
	if !round2 {
		t.Error("no round 2 event in the log")
	}
}

func TestOneRoundRequested(t *testing.T) {
	fakes(t, map[string][]string{
		"claude": {brief("Draft"), brief("Revision")},
		"codex":  {block},
		"grok":   {ok},
	})
	s, _ := newService(t)
	sess := start(t, s, StartRequest{Input: dump, Rounds: 1})
	if len(sess.Rounds) != 1 || sess.StoppedEarly {
		t.Fatalf("rounds %d early %v", len(sess.Rounds), sess.StoppedEarly)
	}
}

func TestAllContentKeepsDraft(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Draft")},
		"codex":  {ok},
		"grok":   {ok},
	})
	s, _ := newService(t)
	sess := start(t, s, StartRequest{Input: dump})
	if sess.Rounds[0].Synthesis != nil || !sess.StoppedEarly || sess.Title != "Draft" {
		t.Fatalf("synthesis %+v early %v title %q", sess.Rounds[0].Synthesis, sess.StoppedEarly, sess.Title)
	}
	if n := len(calls(t, script, "claude")); n != 1 {
		t.Errorf("claude calls = %d", n)
	}
}

func TestCriticMissingOrSignedOut(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		fakes(t, map[string][]string{
			"claude": {brief("Draft"), brief("Revision")},
			"codex":  {concern},
		})
		s, _ := newService(t)
		sess := start(t, s, StartRequest{Input: dump})
		if sess.Mode != ModeCouncil || len(sess.Rounds[0].Critiques) != 1 || !hasNote(sess, "grok is not installed") {
			t.Fatalf("mode %s critiques %d notes %v", sess.Mode, len(sess.Rounds[0].Critiques), sess.Notes)
		}
	})
	t.Run("not signed in", func(t *testing.T) {
		fakes(t, map[string][]string{
			"claude": {brief("Draft"), brief("Revision"), brief("Revision 2")},
			"codex":  {block, ok},
			"grok":   {authReply},
		})
		s, _ := newService(t)
		sess := start(t, s, StartRequest{Input: dump})
		if !hasNote(sess, "grok is not signed in") || len(sess.Rounds) != 2 {
			t.Fatalf("notes %v rounds %d", sess.Notes, len(sess.Rounds))
		}
		// A signed-out critic is not asked again in round 2.
		if c := sess.Rounds[1].Critiques; len(c) != 1 || c[0].Provider != "codex" {
			t.Errorf("round 2 critiques = %+v", c)
		}
		if g := sess.Rounds[0].Critiques; !g[1].Skipped && !g[0].Skipped {
			t.Errorf("no critique marked skipped: %+v", g)
		}
	})
	t.Run("critic call fails", func(t *testing.T) {
		fakes(t, map[string][]string{
			"claude": {brief("Draft"), brief("Revision")},
			"codex":  {concern},
			"grok":   {failReply},
		})
		s, _ := newService(t)
		sess := start(t, s, StartRequest{Input: dump})
		// The round goes on with the critique it has, and says what it lost.
		if !hasNote(sess, "grok's critique in round 1 failed") || len(sess.Rounds) != 1 || sess.Rounds[0].Synthesis == nil {
			t.Fatalf("notes %v rounds %d", sess.Notes, len(sess.Rounds))
		}
	})
	t.Run("proposer signed out", func(t *testing.T) {
		fakes(t, map[string][]string{
			"claude": {authReply},
			"codex":  {brief("Draft"), brief("Revision")},
			"grok":   {concern},
		})
		s, _ := newService(t)
		sess := start(t, s, StartRequest{Input: dump})
		if sess.Proposer != "codex" || len(sess.Critics) != 1 || sess.Critics[0] != "grok" || !hasNote(sess, "codex proposes instead") {
			t.Fatalf("proposer %s critics %v notes %v", sess.Proposer, sess.Critics, sess.Notes)
		}
	})
}

func hasNote(s *Session, sub string) bool {
	for _, n := range s.Notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

func TestSingleProvider(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Draft"), concern, brief("Revision")},
	})
	s, _ := newService(t)
	sess := start(t, s, StartRequest{Input: dump})
	if sess.Mode != ModeSelfCritique || !hasNote(sess, "critiqued its own draft") {
		t.Fatalf("mode %s notes %v", sess.Mode, sess.Notes)
	}
	c := sess.Rounds[0].Critiques
	last := c[len(c)-1]
	if last.Provider != "claude" || last.Role != RoleSelfCritique || last.Verdict != VerdictConcerns {
		t.Fatalf("critiques = %+v", c)
	}
	if got := calls(t, script, "claude"); len(got) != 3 || !strings.Contains(got[1], "You wrote this draft yourself") {
		t.Errorf("claude prompts = %q", got)
	}

	// Asking for no critics at all is self-critique from the start.
	fakes(t, map[string][]string{"claude": {brief("Draft"), ok}})
	s2, _ := newService(t)
	sess = start(t, s2, StartRequest{Input: dump, Critics: []string{}})
	if sess.Mode != ModeSelfCritique || sess.Rounds[0].Critiques[0].Role != RoleSelfCritique {
		t.Fatalf("mode %s critiques %+v", sess.Mode, sess.Rounds[0].Critiques)
	}
}

func TestNoProviders(t *testing.T) {
	fakes(t, map[string][]string{})
	s, _ := newService(t)
	sess, err := s.Start(context.Background(), StartRequest{Input: dump}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != StatusFailed || !strings.Contains(sess.Error, "No provider") {
		t.Fatalf("status %s error %q", sess.Status, sess.Error)
	}
}

func TestConfidentialRefusal(t *testing.T) {
	script := fakes(t, map[string][]string{"claude": {brief("x")}, "codex": {ok}, "grok": {ok}})
	s, v := newService(t)
	for _, p := range []memory.Page{
		{Path: "Private/_folder.md", Front: map[string]any{"confidential": true}},
		{Path: "Private/plan.md", Body: "secret"},
		{Path: "Notes/flagged.md", Front: map[string]any{"confidential": true}, Body: "secret"},
		{Path: "Notes/open.md", Body: "fine"},
	} {
		if err := v.Write(&p); err != nil {
			t.Fatal(err)
		}
	}
	for _, in := range []StartRequest{
		{Input: dump, Project: "secret-app"},
		{Input: dump + " See [[plan]]."},
		{Input: dump + " See [[Private/plan|the plan]]."},
		{Input: dump + " See [[Notes/flagged]]."},
	} {
		if _, err := s.Start(context.Background(), in, nil); !errors.Is(err, ErrConfidential) {
			t.Errorf("%+v: err = %v", in, err)
		}
	}
	for _, p := range []string{"claude", "codex", "grok"} {
		if n := len(calls(t, script, p)); n != 0 {
			t.Errorf("%s was called %d times", p, n)
		}
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Errorf("a refused session was stored: %+v", list)
	}
	if _, err := s.Start(context.Background(), StartRequest{Input: dump, Project: "nope"}, nil); !errors.Is(err, ErrBadRequest) {
		t.Errorf("unknown project: %v", err)
	}
	// A link to an ordinary page is fine.
	if err := s.checkConfidential("demo-app", "see [[open]] and [[missing page]]"); err != nil {
		t.Errorf("open page refused: %v", err)
	}
}

func TestApproveAndAskAgain(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Clean up stale run folders"), brief("Clean up stale run folders"), brief("Clean up stale run folders v2")},
		"codex":  {concern, concern},
		"grok":   {ok, ok},
	})
	s, v := newService(t)
	sess := start(t, s, StartRequest{Input: dump})

	// Ask again with notes: one more round, the notes reach every model.
	again, err := s.AskAgain(context.Background(), sess.ID, "Only folders older than a day.", nil)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != StatusDraft || len(again.Rounds) != 2 || again.Rounds[1].Notes == "" || again.BriefPath != sess.BriefPath {
		t.Fatalf("again = status %s rounds %d path %q", again.Status, len(again.Rounds), again.BriefPath)
	}
	for _, p := range []string{"claude", "codex", "grok"} {
		got := calls(t, script, p)
		if !strings.Contains(got[len(got)-1], "Only folders older than a day.") {
			t.Errorf("%s did not get the notes", p)
		}
	}
	pg, _ := v.Read(sess.BriefPath)
	if !strings.Contains(pg.Body, "v2") {
		t.Errorf("brief not rewritten: %q", pg.Body)
	}

	card, err := s.Approve(sess.ID, "demo-app")
	if err != nil {
		t.Fatal(err)
	}
	if card.Column != "Ready" || card.Memory != sess.BriefPath || card.Council != sess.ID || card.Project != "demo-app" || card.ID == "" {
		t.Fatalf("card = %+v", card)
	}
	pg, _ = v.Read(sess.BriefPath)
	if pg.Front["status"] != "approved" || pg.Front["project"] != "demo-app" {
		t.Errorf("front = %v", pg.Front)
	}
	b, err := boards.Get(v, boards.DefaultBoard)
	if err != nil || len(b.Cards) != 1 || b.Cards[0].Council != sess.ID {
		t.Fatalf("board = %+v, %v", b, err)
	}
	// Approving again is harmless: same card, no second one.
	if c2, err := s.Approve(sess.ID, ""); err != nil || c2.ID != card.ID {
		t.Errorf("second approve = %+v, %v", c2, err)
	}
	if b, _ := boards.Get(v, boards.DefaultBoard); len(b.Cards) != 1 {
		t.Errorf("cards = %d", len(b.Cards))
	}
	if _, err := s.AskAgain(context.Background(), sess.ID, "more", nil); !errors.Is(err, ErrApproved) {
		t.Errorf("ask again after approve: %v", err)
	}
}

func TestPersistence(t *testing.T) {
	fakes(t, map[string][]string{"claude": {brief("Draft")}, "codex": {ok}, "grok": {ok}})
	s, _ := newService(t)
	sess := start(t, s, StartRequest{Input: dump})
	if _, err := os.Stat(filepath.Join(s.Dir, sess.ID+".json")); err != nil {
		t.Fatal(err)
	}
	// A fresh service (a restarted daemon) reads the same record.
	s2 := &Service{Dir: s.Dir, Vault: s.Vault, Projects: testProjects}
	got, err := s2.Get(sess.ID)
	if err != nil || got.Brief != sess.Brief || got.Status != StatusDraft || len(got.Usage) != len(sess.Usage) {
		t.Fatalf("reloaded = %+v, %v", got, err)
	}
	// A record left running by a daemon that stopped reads as failed.
	stale := clone(got)
	stale.ID, stale.Status, stale.Created = "20260101-000000-abcdef", StatusRunning, stale.Created.Add(time.Hour)
	if err := s2.persistLocked(stale); err != nil {
		t.Fatal(err)
	}
	list, err := s2.List()
	if err != nil || len(list) != 2 || list[0].ID != stale.ID || list[0].Status != StatusFailed || list[1].CostUSD == 0 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if _, err := s2.Get("../escape"); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad id: %v", err)
	}
}

func TestParseCritique(t *testing.T) {
	for _, c := range []struct {
		in, verdict string
		points      int
		err         bool
	}{
		{`{"verdict":"ok","points":[]}`, VerdictOK, 0, false},
		{"```json\n{\"verdict\":\"Concerns\",\"points\":[{\"severity\":\"MAJOR\",\"text\":\"a\"}]}\n```", VerdictConcerns, 1, false},
		{`Here you go: {"verdict":"concerns","points":[{"severity":"blocker","text":"a"}]}`, VerdictBlocker, 1, false},
		{`{"points":[{"severity":"weird","text":"a"}]}`, VerdictConcerns, 1, false},
		{`no json`, "", 0, true},
		{`{"verdict":"maybe"}`, "", 0, true},
	} {
		v, p, err := parseCritique(c.in)
		if (err != nil) != c.err || v != c.verdict || len(p) != c.points {
			t.Errorf("%q = %q %d %v", c.in, v, len(p), err)
		}
	}
}

func TestCheckBrief(t *testing.T) {
	if w := CheckBrief(brief("T")); len(w) != 0 {
		t.Errorf("good brief: %v", w)
	}
	bad := strings.Replace(brief("T"), " — proof: go test ./internal/x", "", 1)
	bad = strings.Replace(bad, "## Risks\n- Deleting a live folder.\n\n", "", 1)
	w := CheckBrief(bad)
	if len(w) != 2 || !strings.Contains(strings.Join(w, ";"), "missing section: Risks") || !strings.Contains(strings.Join(w, ";"), "no proof") {
		t.Errorf("bad brief: %v", w)
	}
	if got := cleanBrief("Sure!\n```markdown\n# T\n\n## Problem\nx\n```"); !strings.HasPrefix(got, "# T") || strings.Contains(got, "```") {
		t.Errorf("cleanBrief = %q", got)
	}
}

func TestHTTP(t *testing.T) {
	fakes(t, map[string][]string{"claude": {brief("Draft")}, "codex": {ok}, "grok": {ok}})
	s, _ := newService(t)
	mux := http.NewServeMux()
	Register(mux, s)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	post := func(path, body string, confirm bool) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if res := post("/api/council/sessions", `{"input":"x"}`, false); res.StatusCode != http.StatusForbidden {
		t.Errorf("unconfirmed start = %d", res.StatusCode)
	}
	if res := post("/api/council/sessions", `{"input":"x","project":"secret-app"}`, true); res.StatusCode != http.StatusForbidden {
		t.Errorf("confidential start = %d", res.StatusCode)
	}
	if res := post("/api/council/sessions", `{"input":""}`, true); res.StatusCode != http.StatusBadRequest {
		t.Errorf("empty start = %d", res.StatusCode)
	}
	res := post("/api/council/sessions", `{"input":"`+dump+`"}`, true)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start = %d", res.StatusCode)
	}
	var sess Session
	_ = json.NewDecoder(res.Body).Decode(&sess)
	res.Body.Close()

	// The event stream carries snapshots until the run ends.
	ev, err := http.Get(srv.URL + "/api/council/sessions/" + sess.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	if ct := ev.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content type %q", ct)
	}
	var last Session
	var ended bool
	sc := bufio.NewScanner(ev.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	event := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && event == "session":
			_ = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &last)
		case strings.HasPrefix(line, "data: ") && event == "end":
			ended = true
		}
		if ended {
			break
		}
	}
	ev.Body.Close()
	if !ended || last.Status != StatusDraft {
		t.Fatalf("stream ended %v, last status %q", ended, last.Status)
	}

	if res := post("/api/council/sessions/"+sess.ID+"/approve", "", false); res.StatusCode != http.StatusForbidden {
		t.Errorf("unconfirmed approve = %d", res.StatusCode)
	}
	res = post("/api/council/sessions/"+sess.ID+"/approve", `{"project":"demo-app"}`, true)
	var card boards.Card
	_ = json.NewDecoder(res.Body).Decode(&card)
	if res.StatusCode != http.StatusOK || card.Column != "Ready" {
		t.Fatalf("approve = %d %+v", res.StatusCode, card)
	}
	lres, err := http.Get(srv.URL + "/api/council/sessions")
	if err != nil {
		t.Fatal(err)
	}
	var list []Summary
	_ = json.NewDecoder(lres.Body).Decode(&list)
	if len(list) != 1 || list[0].Status != StatusApproved || list[0].Card != card.ID {
		t.Errorf("list = %+v", list)
	}
	if r, _ := http.Get(srv.URL + "/api/council/sessions/nope"); r.StatusCode != http.StatusNotFound {
		t.Errorf("missing = %d", r.StatusCode)
	}
}
