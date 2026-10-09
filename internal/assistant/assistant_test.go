package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// The test binary doubles as the claude CLI: started with
// LUCID_FAKE_ASSISTANT set to a folder, it records its arguments, its input
// and the system prompt file there, and answers with reply.txt. In Work's
// streaming mode it reports a finished run.
func TestMain(m *testing.M) {
	if dir := os.Getenv("LUCID_FAKE_ASSISTANT"); dir != "" {
		in, _ := io.ReadAll(os.Stdin)
		args := os.Args[1:]
		_ = os.WriteFile(filepath.Join(dir, "args.txt"), []byte(strings.Join(args, "\n")), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "stdin.txt"), in, 0o600)
		for i, a := range args {
			if a == "--system-prompt-file" && i+1 < len(args) {
				b, _ := os.ReadFile(args[i+1])
				_ = os.WriteFile(filepath.Join(dir, "system.txt"), b, 0o600)
			}
		}
		for _, a := range args {
			if a == "stream-json" {
				fmt.Println(`{"type":"system","subtype":"init","session_id":"fake-session"}`)
				fmt.Println(`{"type":"result","is_error":false,"result":"Done.","total_cost_usd":0.01,"session_id":"fake-session","usage":{"input_tokens":1,"output_tokens":1}}`)
				os.Exit(0)
			}
		}
		reply, _ := os.ReadFile(filepath.Join(dir, "reply.txt"))
		b, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": string(reply), "total_cost_usd": 0.0042,
			"usage": map[string]any{"input_tokens": 900, "output_tokens": 60}, "modelUsage": map[string]any{"claude-haiku-fake": map[string]any{}}})
		fmt.Println(string(b))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fake is the fake claude: its binary path and the folder it records into.
type fake struct{ bin, state string }

func newFake(t *testing.T) *fake {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	dir, state := t.TempDir(), t.TempDir()
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUCID_FAKE_ASSISTANT", state)
	return &fake{bin: bin, state: state}
}

func (f *fake) reply(t *testing.T, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.state, "reply.txt"), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

// seen is everything the fake was given: arguments, stdin and the system
// prompt file. It clears the record, so a later call that never ran shows
// as empty.
func (f *fake) seen(t *testing.T) string {
	t.Helper()
	var all []string
	for _, n := range []string{"args.txt", "stdin.txt", "system.txt"} {
		b, _ := os.ReadFile(filepath.Join(f.state, n))
		all = append(all, string(b))
		_ = os.Remove(filepath.Join(f.state, n))
	}
	return strings.Join(all, "\n")
}

func (f *fake) runner() *agentexec.Runner {
	return &agentexec.Runner{LookPath: func(name string) (string, error) {
		if name == "claude" {
			return f.bin, nil
		}
		return "", exec.ErrNotFound
	}}
}

const projectsYAML = `version: 1
projects:
  - id: demo
    name: Demo app
    category: product
    status: active
    visibility: private
  - id: hush
    name: Moonshot Vault
    category: product
    status: active
    visibility: confidential
`

// env is a service over a temporary data dir, vault and projects file.
type env struct {
	svc   *Service
	vault *memory.Vault
	data  string
	list  *projects.List
}

func newEnv(t *testing.T, f *fake, yaml string) *env {
	t.Helper()
	data := t.TempDir()
	v, err := memory.Open(filepath.Join(data, "memory"))
	if err != nil {
		t.Fatal(err)
	}
	ps, errs := projects.Parse([]byte(yaml))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	e := &env{vault: v, data: data, list: &projects.List{Configured: true, Projects: ps}}
	e.svc = &Service{
		Dir: filepath.Join(data, "assistant"), BotsDir: filepath.Join(data, "bots"),
		Projects: func() (*projects.List, error) { return e.list, nil },
		Vault:    func() (*memory.Vault, error) { return v, nil },
	}
	if f != nil {
		e.svc.Runner = f.runner()
	}
	return e
}

func act(kind string, args any) Action {
	b, _ := json.Marshal(args)
	return Action{Action: kind, Args: b}
}

func world(t *testing.T, e *env) *World {
	t.Helper()
	w, err := e.svc.World()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestValidateAllowlist(t *testing.T) {
	e := newEnv(t, nil, projectsYAML)
	w := world(t, e)
	if p := Validate(act("delete_everything", map[string]any{}), w, nil); p.Valid || len(p.Requests) != 0 {
		t.Errorf("unknown kind accepted: %+v", p)
	}
	if p := Validate(act(ActStartWork, map[string]any{"project": "demo", "prompt": "x"}), w, []string{ActCreateCard}); p.Valid || !strings.Contains(strings.Join(p.Problems, " "), "may not propose") {
		t.Errorf("a kind outside the bot's list: %+v", p)
	}
	// Unknown argument keys are refused, so nothing rides along unchecked.
	if p := Validate(act(ActCreateCard, map[string]any{"title": "x", "column": "Inbox", "url": "https://example.com"}), w, nil); p.Valid {
		t.Errorf("unknown key accepted: %+v", p)
	}
	if p := Validate(act(ActCreateCard, map[string]any{"title": "Write the README", "project": "demo"}), w, nil); !p.Valid || p.Args["column"] != "Inbox" {
		t.Errorf("good card: %+v", p)
	}
	for _, k := range Kinds() {
		if p := Validate(Action{Action: k, Args: json.RawMessage(`{}`)}, w, nil); p.Valid {
			t.Errorf("%s with no arguments is valid", k)
		}
	}
}

func TestValidateUnknownProjects(t *testing.T) {
	e := newEnv(t, nil, projectsYAML)
	w := world(t, e)
	for _, a := range []Action{
		act(ActCreateCard, map[string]any{"project": "nope", "title": "x"}),
		act(ActCreateIdea, map[string]any{"project": "nope", "title": "x"}),
		act(ActStartCouncil, map[string]any{"project": "nope", "braindump": "x"}),
		act(ActStartWork, map[string]any{"project": "nope", "prompt": "x"}),
		act(ActAddNeeds, map[string]any{"project": "nope", "needs": []string{"x"}}),
		act(ActAddNeeds, map[string]any{"project": "demo", "needs": []any{map[string]any{"what": "x", "from": "nope"}}}),
		act(ActLinkBuildsInto, map[string]any{"project": "demo", "target": "nope"}),
		act(ActLinkBuildsInto, map[string]any{"project": "demo", "target": "demo"}),
		act(ActCreateProject, map[string]any{"id": "demo", "name": "Again", "kind": "product"}),
		act(ActCreateProject, map[string]any{"id": "Bad ID", "name": "x", "kind": "product"}),
		act(ActCreateProject, map[string]any{"id": "ok", "name": "x", "kind": "spaceship"}),
		act(ActCreateProject, map[string]any{"id": "ok", "name": "x", "kind": "tool", "local_path": "relative/path"}),
		act(ActCreateCard, map[string]any{"title": "x", "column": "Someday"}),
		act(ActStartWork, map[string]any{"project": "demo", "prompt": "x", "provider": "gemini"}),
	} {
		if p := Validate(a, w, nil); p.Valid || len(p.Problems) == 0 {
			t.Errorf("%s %s accepted", a.Action, a.Args)
		}
	}
	// Spending on a confidential project is refused before it is shown.
	for _, a := range []Action{
		act(ActStartWork, map[string]any{"project": "hush", "prompt": "x"}),
		act(ActStartCouncil, map[string]any{"project": "hush", "braindump": "x"}),
	} {
		if p := Validate(a, w, nil); p.Valid || !strings.Contains(strings.Join(p.Problems, " "), "confidential") {
			t.Errorf("%s on a confidential project: %+v", a.Action, p)
		}
	}
	// demo has no local_path: Work has no checkout.
	if p := Validate(act(ActStartWork, map[string]any{"project": "demo", "prompt": "x"}), w, nil); p.Valid {
		t.Errorf("work without a checkout: %+v", p)
	}
}

// The UI sends a proposal's args back to check before Apply, so a valid
// proposal's args must check again as they are.
func TestProposalArgsCheckAgain(t *testing.T) {
	e := newEnv(t, nil, projectsYAML+"  - id: tool\n    name: Tooling\n    category: tool\n    status: active\n    visibility: private\n    local_path: "+quote(t.TempDir())+"\n")
	w := world(t, e)
	for _, a := range []Action{
		act(ActCreateCard, map[string]any{"project": "demo", "title": "x", "body": "y", "column": "ready"}),
		act(ActCreateIdea, map[string]any{"title": "x", "body": "y"}),
		act(ActCreatePage, map[string]any{"path": "Notes/x", "markdown": "y"}),
		act(ActStartCouncil, map[string]any{"braindump": "x", "project": "demo"}),
		act(ActStartWork, map[string]any{"project": "tool", "prompt": "x"}),
		act(ActCreateProject, map[string]any{"id": "new-one", "name": "New", "kind": "tool"}),
		act(ActAddNeeds, map[string]any{"project": "demo", "needs": []any{"x", map[string]any{"what": "y", "from": "tool"}}}),
		act(ActLinkBuildsInto, map[string]any{"project": "tool", "target": "demo"}),
	} {
		p := Validate(a, w, nil)
		if !p.Valid {
			t.Errorf("%s: %v", a.Action, p.Problems)
			continue
		}
		again := Validate(act(p.Action, p.Args), w, nil)
		if !again.Valid {
			t.Errorf("%s args %v do not check again: %v", a.Action, p.Args, again.Problems)
		}
	}
}

func TestPagePathSafety(t *testing.T) {
	for _, bad := range []string{"", "../x", "Notes/../../x", "/etc/passwd", `C:\Windows\x`, "C:/x", "a//b", "./a", ".trash/x", "Notes/.hidden", "Boards/work", "boards/work.md", "a/b\\c", strings.Repeat("a", MaxPagePath+1)} {
		if p, err := PagePath(bad); err == nil {
			t.Errorf("%q accepted as %q", bad, p)
		}
	}
	for in, want := range map[string]string{"Notes/plan": "Notes/plan.md", "Inbox/idea.md": "Inbox/idea.md", "readme": "readme.md"} {
		if got, err := PagePath(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	e := newEnv(t, nil, projectsYAML)
	if err := e.vault.Write(&memory.Page{Path: "Notes/plan.md", Body: "mine"}); err != nil {
		t.Fatal(err)
	}
	w := world(t, e)
	if p := Validate(act(ActCreatePage, map[string]any{"path": "Notes/plan", "markdown": "theirs"}), w, nil); p.Valid {
		t.Errorf("overwrites an existing page: %+v", p)
	}
	if p := Validate(act(ActCreatePage, map[string]any{"path": "../../outside", "markdown": "x"}), w, nil); p.Valid {
		t.Errorf("traversal accepted: %+v", p)
	}
}

func TestSizeCaps(t *testing.T) {
	e := newEnv(t, nil, projectsYAML)
	w := world(t, e)
	long := func(n int) string { return strings.Repeat("x", n) }
	needs := make([]string, MaxNeeds+1)
	for i := range needs {
		needs[i] = "a need"
	}
	for _, a := range []Action{
		act(ActCreateCard, map[string]any{"title": long(MaxTitle + 1)}),
		act(ActCreateCard, map[string]any{"title": "x", "body": long(MaxBody + 1)}),
		act(ActCreateIdea, map[string]any{"title": "x", "body": long(MaxBody + 1)}),
		act(ActCreatePage, map[string]any{"path": "Notes/x", "markdown": long(MaxPage + 1)}),
		act(ActStartCouncil, map[string]any{"braindump": long(MaxPrompt + 1)}),
		act(ActAddNeeds, map[string]any{"project": "demo", "needs": needs}),
		act(ActAddNeeds, map[string]any{"project": "demo", "needs": []string{long(MaxNeed + 1)}}),
		act(ActCreateProject, map[string]any{"id": "ok", "name": long(MaxName + 1), "kind": "tool"}),
	} {
		if p := Validate(a, w, nil); p.Valid {
			t.Errorf("%s over its cap accepted", a.Action)
		}
	}
	var many []string
	for range MaxActions + 3 {
		many = append(many, `{"action":"create_card","args":{"title":"x"}}`)
	}
	_, acts, err := ParseActions("Sure.\n```lucid-actions\n{\"actions\": [" + strings.Join(many, ",") + "]}\n```")
	if err == nil || len(acts) != MaxActions {
		t.Errorf("%d actions, %v", len(acts), err)
	}
	if _, _, err := ParseActions("```lucid-actions\n{\"actions\": [], \"run\": \"rm -rf\"}\n```"); err == nil {
		t.Error("unknown top-level key accepted")
	}
	prose, acts, err := ParseActions("No actions here.")
	if err != nil || acts != nil || prose != "No actions here." {
		t.Errorf("%q %v %v", prose, acts, err)
	}
}

func TestTurnProposesAndRedactsConfidential(t *testing.T) {
	f := newFake(t)
	e := newEnv(t, f, projectsYAML)
	f.reply(t, "I'll put it on the board.\n\n```lucid-actions\n"+`{"actions": [{"action": "create_card", "args": {"project": "demo", "title": "Write the README", "body": "Cover install and usage.", "column": "Inbox"}}, {"action": "start_work", "args": {"project": "hush", "prompt": "do it"}}]}`+"\n```")
	res, err := e.svc.Turn(context.Background(), TurnRequest{Message: "make a card to write the README for project demo"})
	if err != nil {
		t.Fatal(err)
	}
	seen := f.seen(t)
	if strings.Contains(seen, "Moonshot Vault") {
		t.Error("the confidential project's name reached the CLI")
	}
	for _, want := range []string{"hush · confidential", "demo · Demo app", "--tools", "--model\nhaiku", "lucid-actions", "Inbox, Ready"} {
		if !strings.Contains(seen, want) {
			t.Errorf("the CLI was not given %q", want)
		}
	}
	c := res.Conversation
	if len(c.Messages) != 2 || c.Messages[1].Text != "I'll put it on the board." {
		t.Fatalf("messages %+v", c.Messages)
	}
	ps := c.Messages[1].Proposals
	if len(ps) != 2 || !ps[0].Valid || ps[0].Status != StatusPending || len(ps[0].Requests) != 2 {
		t.Fatalf("proposals %+v", ps)
	}
	if ps[1].Valid {
		t.Errorf("work on a confidential project proposed as valid: %+v", ps[1])
	}
	// Nothing was applied: no card, no page.
	if _, err := e.vault.Read("Boards/work.md"); err == nil {
		t.Error("the turn created the work board")
	}
	if _, err := e.vault.Read(ps[0].Page); err == nil {
		t.Error("the turn wrote the card's page")
	}
	if files, _ := filepath.Glob(filepath.Join(e.data, "assistant", "runs", "*.json")); len(files) != 1 {
		t.Errorf("%d usage records", len(files))
	}
	if c.Messages[1].Usage == nil || c.Messages[1].Usage.CostUSD != 0.0042 {
		t.Errorf("usage %+v", c.Messages[1].Usage)
	}

	// A second turn resends the conversation.
	f.reply(t, "Done thinking.")
	if _, err := e.svc.Turn(context.Background(), TurnRequest{Conversation: c.ID, Message: "and a second one?"}); err != nil {
		t.Fatal(err)
	}
	seen = f.seen(t)
	if !strings.Contains(seen, "make a card to write the README") || !strings.Contains(seen, "proposed create_card") {
		t.Errorf("the transcript was not resent: %s", seen)
	}
	got, err := e.svc.Conversation(c.ID)
	if err != nil || len(got.Messages) != 4 {
		t.Fatalf("reload %v %+v", err, got)
	}

	// Recording Apply or Skip changes the proposal's status only.
	if _, err := e.svc.Record(c.ID, Outcome{Message: 1, Proposal: 0, Status: StatusApplied}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Record(c.ID, Outcome{Message: 1, Proposal: 0, Status: "pending"}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("bad status %v", err)
	}
	got, _ = e.svc.Conversation(c.ID)
	if got.Messages[1].Proposals[0].Status != StatusApplied {
		t.Errorf("outcome not kept: %+v", got.Messages[1].Proposals[0])
	}
}

func TestTurnRefusesConfidentialSubjects(t *testing.T) {
	f := newFake(t)
	e := newEnv(t, f, projectsYAML)
	f.reply(t, "ok")
	if err := e.vault.Write(&memory.Page{Path: "Secret/plan.md", Front: map[string]any{"confidential": true}, Body: "the plan"}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []TurnRequest{
		{Message: "what next?", Project: "hush"},
		{Message: "what should Moonshot Vault do next?"},
		{Message: "summarise [[plan]] for me"},
		{Message: "what is on this page?", Page: "Secret/plan.md"},
	} {
		_, err := e.svc.Turn(context.Background(), req)
		if !errors.Is(err, ErrConfidential) {
			t.Errorf("%+v: %v", req, err)
		}
		if seen := f.seen(t); strings.TrimSpace(seen) != "" {
			t.Errorf("%+v reached the CLI: %s", req, seen)
		}
	}
	// A word that merely contains the name is not the name.
	if _, err := e.svc.Turn(context.Background(), TurnRequest{Message: "a moonshot vaulting pole"}); err != nil {
		t.Errorf("false match: %v", err)
	}
}

func TestBotTurnUsesPersonaAndAllowList(t *testing.T) {
	f := newFake(t)
	e := newEnv(t, f, projectsYAML)
	b, err := e.svc.SaveBot(Bot{ID: "scribe", Name: "Scribe", Provider: "claude", Model: "sonnet", Persona: "You keep notes tidy.", AllowedActions: []string{ActCreatePage}})
	if err != nil {
		t.Fatal(err)
	}
	f.reply(t, "```lucid-actions\n"+`{"actions": [{"action": "create_card", "args": {"title": "x"}}, {"action": "create_page", "args": {"path": "Notes/tidy", "markdown": "# Tidy"}}]}`+"\n```")
	res, err := e.svc.Turn(context.Background(), TurnRequest{Bot: b.ID, Message: "tidy up"})
	if err != nil {
		t.Fatal(err)
	}
	seen := f.seen(t)
	if !strings.Contains(seen, "You keep notes tidy.") || !strings.Contains(seen, "--model\nsonnet") || !strings.Contains(seen, "only these actions: create_page") {
		t.Errorf("bot not applied: %s", seen)
	}
	ps := res.Conversation.Messages[1].Proposals
	if ps[0].Valid || !ps[1].Valid {
		t.Errorf("allow list: %+v", ps)
	}
	if res.Conversation.Bot != "scribe" {
		t.Errorf("conversation bot %q", res.Conversation.Bot)
	}
}

func TestBotStore(t *testing.T) {
	e := newEnv(t, nil, projectsYAML)
	for _, b := range []Bot{
		{ID: "../x", Name: "x", Provider: "claude"},
		{ID: "x", Name: "", Provider: "claude"},
		{ID: "x", Name: "x", Provider: "gemini"},
		{ID: "x", Name: "x", Provider: "claude", AllowedActions: []string{"rm"}},
		{ID: "x", Name: "x", Provider: "claude", Avatar: "sprite:dancing"},
		{ID: "x", Name: "x", Provider: "claude", Avatar: "<img src=x>"},
		{ID: "x", Name: "x", Provider: "claude", Persona: strings.Repeat("p", MaxPersona+1)},
	} {
		if _, err := e.svc.SaveBot(b); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v saved: %v", b, err)
		}
	}
	b, err := e.svc.SaveBot(Bot{ID: "helper", Name: "Helper", Provider: "grok", Persona: "Be brief.", Avatar: "sprite:thinking"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.AllowedActions) != len(Catalog) {
		t.Errorf("a new bot may propose %v", b.AllowedActions)
	}
	if _, err := e.svc.SaveBot(Bot{ID: "quiet", Name: "Quiet", Provider: "codex", AllowedActions: []string{}}); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Bot("quiet")
	if err != nil || len(got.AllowedActions) != 0 {
		t.Errorf("an empty allow list came back as %v (%v)", got, err)
	}
	if l := e.svc.Bots(); len(l) != 2 || l[0].ID != "helper" {
		t.Errorf("bots %+v", l)
	}
	if id := e.svc.FreeBotID("Helper"); id != "helper-2" {
		t.Errorf("free id %q", id)
	}
	if err := e.svc.DeleteBot("helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Bot("helper"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted bot: %v", err)
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBraindumpParse(t *testing.T) {
	f := newFake(t)
	e := newEnv(t, f, projectsYAML)
	e.svc.Existing = func() []Existing {
		return []Existing{{Kind: "card", Title: "Write the README for demo", Ref: "work/c1"}}
	}
	dump := "ok so  the demo app needs a README badly. also maybe a tool that renames my photos?? and the login button is broken on mobile"
	f.reply(t, `{"items": [
	 {"quote": "the demo app needs a README badly", "restatement": "Write the README", "type": "chore", "project": "demo", "next": "card"},
	 {"quote": "a tool that renames my photos", "restatement": "Build a photo renaming tool", "type": "idea", "project": "photos", "next": "council"},
	 {"quote": "the login button is broken on mobile", "restatement": "Fix the login button on mobile", "type": "defect", "project": "demo", "next": "now"}
	]}`)
	res, err := e.svc.Braindump(context.Background(), BraindumpRequest{Text: dump})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 3 {
		t.Fatalf("items %+v", res.Items)
	}
	a, b, c := res.Items[0], res.Items[1], res.Items[2]
	if !a.QuoteFound || a.Similar == nil || a.Similar.Ref != "work/c1" || a.Project != "demo" {
		t.Errorf("first %+v", a)
	}
	if b.Project != "" || !b.NewProject || b.Similar != nil || b.Next != "council" {
		t.Errorf("second %+v", b)
	}
	if c.Type != "idea" || c.Next != "park" {
		t.Errorf("third falls back: %+v", c)
	}
	if seen := f.seen(t); strings.Contains(seen, "Moonshot Vault") || !strings.Contains(seen, "renames my photos") {
		t.Errorf("sent %s", seen)
	}
	if _, err := e.svc.Braindump(context.Background(), BraindumpRequest{Text: "Moonshot Vault needs a logo"}); !errors.Is(err, ErrConfidential) {
		t.Errorf("confidential name: %v", err)
	}
	f.reply(t, "I would rather not.")
	if _, err := e.svc.Braindump(context.Background(), BraindumpRequest{Text: dump}); !errors.Is(err, ErrBadOutput) {
		t.Errorf("no JSON: %v", err)
	}
}

func TestSimilarity(t *testing.T) {
	for _, c := range []struct {
		a, b string
		like bool
	}{
		{"Write the README", "Write README for demo", true},
		{"Fix the login button", "Fix login button on mobile", true},
		{"Fix the login button", "Write the README", false},
		{"README", "Write the README for the demo project", false},
	} {
		if got := Similarity(c.a, c.b) >= Similar; got != c.like {
			t.Errorf("%q ~ %q = %v", c.a, c.b, Similarity(c.a, c.b))
		}
	}
}
