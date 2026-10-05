package agentexec

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
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake provider CLI: copied onto PATH as
// claude, codex or grok and run with LUCID_FAKE_CLI_MODE set, it records its
// arguments and stdin and prints canned output.
func TestMain(m *testing.M) {
	if mode := os.Getenv("LUCID_FAKE_CLI_MODE"); mode != "" {
		os.Exit(fakeCLI(mode))
	}
	os.Exit(m.Run())
}

func jsonLine(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

type m = map[string]any

func fakeCLI(mode string) int {
	in, _ := io.ReadAll(os.Stdin)
	if log := os.Getenv("LUCID_FAKE_CLI_LOG"); log != "" {
		wd, _ := os.Getwd()
		rec, _ := json.Marshal(m{"args": os.Args[1:], "stdin": string(in), "cwd": wd, "claude_dir": os.Getenv("CLAUDE_CONFIG_DIR")})
		_ = os.WriteFile(log, rec, 0o600)
	}
	out := func(v any) { fmt.Println(jsonLine(v)) }
	switch mode {
	case "claude-json":
		out(m{"type": "result", "is_error": false, "result": "ok", "total_cost_usd": 0.01,
			"usage":      m{"input_tokens": 5, "output_tokens": 7, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 2},
			"modelUsage": m{"claude-x": m{}}})
	case "claude-auth":
		out(m{"type": "result", "is_error": true, "result": "Not logged in · Please run /login"})
		return 1
	case "claude-stream":
		out(m{"type": "system", "subtype": "init"})
		out(m{"type": "assistant", "message": m{"content": []any{
			m{"type": "text", "text": "Reading the file."},
			m{"type": "tool_use", "id": "t1", "name": "Read", "input": m{"file_path": "a.txt"}}}}})
		out(m{"type": "rate_limit_event"})
		out(m{"type": "user", "message": m{"content": []any{
			m{"type": "tool_result", "tool_use_id": "t1", "content": "1\thello"}}}})
		out(m{"type": "assistant", "message": m{"content": []any{
			m{"type": "tool_use", "id": "t2", "name": "Edit", "input": m{"file_path": "a.txt", "old_string": "hello", "new_string": "goodbye"}}}}})
		// A tool result larger than a default scanner buffer.
		out(m{"type": "user", "message": m{"content": []any{
			m{"type": "tool_result", "tool_use_id": "t2", "is_error": true, "content": []any{m{"type": "text", "text": strings.Repeat("x", 200<<10)}}}}}})
		out(m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": "done"}}}})
		out(m{"type": "result", "is_error": false, "result": "done", "total_cost_usd": 0.04,
			"usage":      m{"input_tokens": 6, "output_tokens": 274, "cache_read_input_tokens": 61114, "cache_creation_input_tokens": 6540},
			"modelUsage": m{"claude-x": m{}}})
	case "codex-json":
		for i, a := range os.Args {
			if a == "-o" && i+1 < len(os.Args) {
				_ = os.WriteFile(os.Args[i+1], []byte("ok"), 0o600)
			}
		}
		out(m{"type": "thread.started", "thread_id": "x"})
		out(m{"type": "turn.completed", "usage": m{"input_tokens": 100, "cached_input_tokens": 40, "output_tokens": 9}})
	case "codex-auth":
		fmt.Fprintln(os.Stderr, "Not logged in. Run codex login")
		return 1
	case "codex-stream":
		out(m{"type": "thread.started", "thread_id": "x"})
		out(m{"type": "item.completed", "item": m{"id": "i0", "type": "error", "message": "clamping hook timeout"}})
		out(m{"type": "turn.started"})
		out(m{"type": "item.completed", "item": m{"id": "i1", "type": "agent_message", "text": "I will edit it."}})
		out(m{"type": "item.started", "item": m{"id": "i2", "type": "command_execution", "command": "cat a.txt", "status": "in_progress"}})
		out(m{"type": "item.completed", "item": m{"id": "i2", "type": "command_execution", "command": "cat a.txt", "aggregated_output": "hello\n", "exit_code": 0, "status": "completed"}})
		out(m{"type": "item.completed", "item": m{"id": "i3", "type": "file_change", "status": "completed", "changes": []any{m{"path": "a.txt", "kind": "update"}}}})
		out(m{"type": "item.completed", "item": m{"id": "i4", "type": "agent_message", "text": "done"}})
		out(m{"type": "turn.completed", "usage": m{"input_tokens": 100, "cached_input_tokens": 40, "output_tokens": 9}})
	case "grok-json":
		out(m{"result": "ok", "usage": m{"input_tokens": 11, "output_tokens": 2}})
	case "grok-stream":
		out(m{"type": "available_commands", "tools": []string{"write"}})
		out(m{"type": "thought", "data": "hmm"})
		for _, s := range []string{"I'll", " edit", " it."} {
			out(m{"type": "text", "data": s})
		}
		out(m{"type": "tool_call", "toolCallId": "c1", "title": "write", "toolName": "write", "rawInput": m{"file_path": "a.txt", "content": "goodbye\n"}})
		out(m{"type": "tool_call_update", "toolCallId": "c1", "status": nil, "content": []any{m{"type": "diff", "path": "a.txt", "oldText": "", "newText": "goodbye\n"}}})
		out(m{"type": "tool_call_update", "toolCallId": "c1", "status": "completed",
			"content":   []any{m{"type": "diff", "path": "a.txt", "oldText": "hello\n", "newText": "goodbye\n"}},
			"rawOutput": m{"EditsApplied": m{"tool_output_for_prompt_concise": "Wrote file."}}})
		for _, s := range []string{"do", "ne"} {
			out(m{"type": "text", "data": s})
		}
		out(m{"type": "end", "stopReason": "end_turn", "total_cost_usd": 0.05,
			"usage":      m{"input_tokens": 35476, "output_tokens": 736, "cache_read_input_tokens": 147200},
			"modelUsage": m{"grok-x": m{}}})
	case "slow":
		time.Sleep(10 * time.Second)
	case "tree":
		// Streams one message with usage, starts a grandchild that beats into
		// its own file, then beats into its own file too, forever.
		out(m{"type": "assistant", "message": m{"id": "m1", "content": []any{m{"type": "text", "text": "working"}},
			"usage": m{"input_tokens": 12, "output_tokens": 34, "cache_read_input_tokens": 5, "cache_creation_input_tokens": 6}}})
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "LUCID_FAKE_CLI_MODE=beat", "LUCID_BEAT_FILE="+os.Getenv("LUCID_BEAT_CHILD"))
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			return 2
		}
		beat(os.Getenv("LUCID_BEAT_PARENT"))
	case "beat":
		beat(os.Getenv("LUCID_BEAT_FILE"))
	}
	return 0
}

// beat rewrites file with a growing counter every 50ms, forever.
func beat(file string) {
	for i := 1; ; i++ {
		_ = os.WriteFile(file, []byte(strconv.Itoa(i)), 0o600)
		time.Sleep(50 * time.Millisecond)
	}
}

// installFake puts the test binary on a fresh PATH as name.
func installFake(t *testing.T, name, mode string) (log string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		dst += ".exe"
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("LUCID_FAKE_CLI_MODE", mode)
	log = filepath.Join(t.TempDir(), "call.json")
	t.Setenv("LUCID_FAKE_CLI_LOG", log)
	return log
}

type call struct {
	Args      []string `json:"args"`
	Stdin     string   `json:"stdin"`
	Cwd       string   `json:"cwd"`
	ClaudeDir string   `json:"claude_dir"`
}

func readCall(t *testing.T, log string) call {
	t.Helper()
	var c call
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func has(args []string, flag string, val ...string) bool {
	for i, a := range args {
		if a != flag {
			continue
		}
		if len(val) == 0 {
			return true
		}
		if i+1 < len(args) && args[i+1] == val[0] {
			return true
		}
	}
	return false
}

func kinds(evs []Event) string {
	ks := make([]string, len(evs))
	for i, e := range evs {
		ks[i] = e.Kind
	}
	return strings.Join(ks, ",")
}

func TestToolsNoneFlags(t *testing.T) {
	ctx := context.Background()
	t.Run("claude", func(t *testing.T) {
		log := installFake(t, "claude", "claude-json")
		g := &Runner{ProfileDir: func(string, string) (string, error) { return filepath.Join(os.TempDir(), "claude-work"), nil }}
		res, err := g.Run(ctx, Request{Provider: "claude", Profile: "work", SystemPrompt: "be brief", Prompt: "hi", Model: "sonnet"})
		if err != nil {
			t.Fatal(err)
		}
		c := readCall(t, log)
		for _, f := range []string{"-p", "--safe-mode", "--strict-mcp-config", "--no-session-persistence", "--system-prompt-file"} {
			if !has(c.Args, f) {
				t.Errorf("args lack %s: %v", f, c.Args)
			}
		}
		if !has(c.Args, "--output-format", "json") || !has(c.Args, "--model", "sonnet") || !has(c.Args, "--tools", "") {
			t.Errorf("args = %v", c.Args)
		}
		if c.Stdin != "hi" || !strings.HasSuffix(c.ClaudeDir, "claude-work") {
			t.Errorf("stdin %q, CLAUDE_CONFIG_DIR %q", c.Stdin, c.ClaudeDir)
		}
		u := res.Usage
		if res.Text != "ok" || u.InputTokens != 5 || u.OutputTokens != 7 || u.CacheRead != 3 || u.CacheWrite != 2 || u.CostUSD != 0.01 || u.Model != "claude-x" {
			t.Errorf("result = %+v", res)
		}
		if kinds(res.Events) != "text,done" {
			t.Errorf("events = %s", kinds(res.Events))
		}
	})
	t.Run("codex", func(t *testing.T) {
		log := installFake(t, "codex", "codex-json")
		res, err := Run(ctx, Request{Provider: "codex", SystemPrompt: "sys", Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		c := readCall(t, log)
		for _, f := range []string{"--ignore-user-config", "--ignore-rules", "--ephemeral", "--skip-git-repo-check"} {
			if !has(c.Args, f) {
				t.Errorf("args lack %s: %v", f, c.Args)
			}
		}
		if !has(c.Args, "--sandbox", "read-only") || !strings.HasPrefix(c.Stdin, "sys\n\n") || !strings.HasSuffix(c.Stdin, "hi") {
			t.Errorf("args %v stdin %q", c.Args, c.Stdin)
		}
		if res.Text != "ok" || res.Usage.InputTokens != 100 || res.Usage.CacheRead != 40 || res.Usage.OutputTokens != 9 {
			t.Errorf("result = %+v", res)
		}
	})
	t.Run("grok", func(t *testing.T) {
		log := installFake(t, "grok", "grok-json")
		res, err := Run(ctx, Request{Provider: "grok", Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		c := readCall(t, log)
		if !has(c.Args, "-p", "hi") || !has(c.Args, "--max-turns", "1") || !has(c.Args, "--no-subagents") || !has(c.Args, "--output-format", "json") {
			t.Errorf("args = %v", c.Args)
		}
		if res.Text != "ok" || res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 2 {
			t.Errorf("result = %+v", res)
		}
	})
}

func TestToolsEditStreams(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	want, err := filepath.EvalSymlinks(work)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("claude", func(t *testing.T) {
		log := installFake(t, "claude", "claude-stream")
		var got []Event
		res, err := Run(ctx, Request{Provider: "claude", SystemPrompt: "rules", Prompt: "edit", Dir: work, Tools: ToolsEdit, Harness: HarnessClean,
			OnEvent: func(e Event) { got = append(got, e) }})
		if err != nil {
			t.Fatal(err)
		}
		c := readCall(t, log)
		if !has(c.Args, "--output-format", "stream-json") || !has(c.Args, "--verbose") || !has(c.Args, "--permission-mode", "acceptEdits") ||
			!has(c.Args, "--safe-mode") || !has(c.Args, "--append-system-prompt", "rules") || has(c.Args, "--tools") || has(c.Args, "--system-prompt-file") {
			t.Errorf("args = %v", c.Args)
		}
		if gc, _ := filepath.EvalSymlinks(c.Cwd); gc != want || c.Stdin != "edit" {
			t.Errorf("cwd %q (want %q), stdin %q", c.Cwd, want, c.Stdin)
		}
		if k := kinds(res.Events); k != "text,tool,tool_result,tool,diff,tool_result,text,done" {
			t.Errorf("events = %s", k)
		}
		if len(got) != len(res.Events) {
			t.Errorf("OnEvent saw %d of %d events", len(got), len(res.Events))
		}
		if d := res.Events[4]; d.Title != "a.txt" || d.Body != "- hello\n+ goodbye\n" {
			t.Errorf("diff = %+v", d)
		}
		if r := res.Events[5]; r.Title != "Edit (error)" || len(r.Body) < 1000 || len(r.Raw) != 0 {
			t.Errorf("big tool_result: title %q body %d raw %d", r.Title, len(r.Body), len(r.Raw))
		}
		u := res.Usage
		if res.Text != "done" || u.CostUSD != 0.04 || u.OutputTokens != 274 || u.CacheRead != 61114 || u.Model != "claude-x" || u.Provider != "claude" {
			t.Errorf("result = %q %+v", res.Text, u)
		}
		// Nothing but the agent's own files may land in the working dir.
		if ents, _ := os.ReadDir(work); len(ents) != 0 {
			t.Errorf("working dir polluted: %v", ents)
		}
	})
	t.Run("claude mine", func(t *testing.T) {
		log := installFake(t, "claude", "claude-stream")
		if _, err := Run(ctx, Request{Provider: "claude", Prompt: "edit", Dir: work, Tools: ToolsEdit, Harness: HarnessMine}); err != nil {
			t.Fatal(err)
		}
		if c := readCall(t, log); has(c.Args, "--safe-mode") || has(c.Args, "--strict-mcp-config") {
			t.Errorf("mine must not isolate: %v", c.Args)
		}
	})
	t.Run("codex", func(t *testing.T) {
		log := installFake(t, "codex", "codex-stream")
		res, err := Run(ctx, Request{Provider: "codex", Prompt: "edit", Dir: work, Tools: ToolsEdit})
		if err != nil {
			t.Fatal(err)
		}
		c := readCall(t, log)
		if !has(c.Args, "--json") || !has(c.Args, "--sandbox", "workspace-write") || !has(c.Args, "--ignore-user-config") || !has(c.Args, "--skip-git-repo-check") {
			t.Errorf("args = %v", c.Args)
		}
		if k := kinds(res.Events); k != "error,text,tool,tool_result,diff,text,done" {
			t.Errorf("events = %s", k)
		}
		if res.Text != "done" || res.Usage.InputTokens != 100 || res.Usage.CacheRead != 40 || res.Usage.OutputTokens != 9 {
			t.Errorf("result = %q %+v", res.Text, res.Usage)
		}
		if res.Events[0].Title != "warning" || res.Events[3].Body != "hello\n" || res.Events[4].Title != "a.txt" {
			t.Errorf("events = %+v", res.Events)
		}
	})
	t.Run("codex mine", func(t *testing.T) {
		log := installFake(t, "codex", "codex-stream")
		if _, err := Run(ctx, Request{Provider: "codex", Prompt: "edit", Dir: work, Tools: ToolsEdit, Harness: HarnessMine}); err != nil {
			t.Fatal(err)
		}
		if c := readCall(t, log); has(c.Args, "--ignore-user-config") || has(c.Args, "--ignore-rules") {
			t.Errorf("mine must load the user's config: %v", c.Args)
		}
	})
	t.Run("grok", func(t *testing.T) {
		log := installFake(t, "grok", "grok-stream")
		res, err := Run(ctx, Request{Provider: "grok", Prompt: "edit", Dir: work, Tools: ToolsEdit})
		if err != nil {
			t.Fatal(err)
		}
		c := readCall(t, log)
		if !has(c.Args, "--output-format", "streaming-json") || !has(c.Args, "--permission-mode", "acceptEdits") || !has(c.Args, "--prompt-file") || has(c.Args, "--max-turns") {
			t.Errorf("args = %v", c.Args)
		}
		if k := kinds(res.Events); k != "text,tool,diff,tool_result,text,done" {
			t.Errorf("events = %s", k)
		}
		if res.Events[0].Body != "I'll edit it." || res.Events[3].Body != "Wrote file." || res.Events[2].Body != "- hello\n+ goodbye\n" {
			t.Errorf("events = %+v", res.Events)
		}
		u := res.Usage
		if res.Text != "done" || u.InputTokens != 35476 || u.OutputTokens != 736 || u.CacheRead != 147200 || u.CostUSD != 0.05 || u.Model != "grok-x" {
			t.Errorf("result = %q %+v", res.Text, u)
		}
	})
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	req := Request{Provider: "claude", Prompt: "hi"}

	installFake(t, "claude", "claude-auth")
	if _, err := Run(ctx, req); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("signed out: %v", err)
	}
	installFake(t, "codex", "codex-auth")
	if _, err := Run(ctx, Request{Provider: "codex", Prompt: "hi"}); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("codex signed out: %v", err)
	}
	installFake(t, "claude", "slow")
	r := req
	r.Timeout = 500 * time.Millisecond
	res, err := Run(ctx, r)
	if !errors.Is(err, ErrTimeout) || res == nil || kinds(res.Events) != "error,done" {
		t.Errorf("slow: %v %+v", err, res)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Run(ctx, req); !errors.Is(err, ErrCLIMissing) {
		t.Errorf("missing: %v", err)
	}
	g := &Runner{InContainer: func() bool { return true }}
	if _, err := g.Run(ctx, req); !errors.Is(err, ErrInContainer) || !strings.Contains(err.Error(), "desktop app") {
		t.Errorf("container: %v", err)
	}
	present := &Runner{LookPath: func(s string) (string, error) { return s, nil }}
	for _, bad := range []Request{
		{Provider: "cursor", Prompt: "x"},
		{Provider: "claude", Profile: "../x"},
		{Provider: "claude", Profile: "work"}, // no ProfileDir
		{Provider: "grok", Profile: "work"},
		{Provider: "claude", Harness: "weird"},
		{Provider: "claude", Tools: ToolsEdit},                                          // no Dir
		{Provider: "claude", Tools: ToolsEdit, Dir: filepath.Join(t.TempDir(), "gone")}, // missing Dir
		{Provider: "claude", Tools: 9},
	} {
		if _, err := present.Run(ctx, bad); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestLineWriterKeepsHugeLines(t *testing.T) {
	var got []int
	w := &lineWriter{fn: func(b []byte) { got = append(got, len(b)) }}
	big := strings.Repeat("a", 3<<20)
	// Split across writes, plus a last line with no newline.
	_, _ = w.Write([]byte(big[:1<<20]))
	_, _ = w.Write([]byte(big[1<<20:] + "\nshort\ntail"))
	w.flush()
	if len(got) != 3 || got[0] != 3<<20 || got[1] != 5 || got[2] != 4 {
		t.Errorf("lines = %v", got)
	}
}

// TestLive runs each installed provider CLI once with a tiny prompt. It costs
// a few cents, so it only runs with LUCID_AGENTEXEC_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("LUCID_AGENTEXEC_LIVE") != "1" {
		t.Skip("set LUCID_AGENTEXEC_LIVE=1 to run the provider CLIs")
	}
	for _, p := range []string{"claude", "codex", "grok"} {
		t.Run(p, func(t *testing.T) {
			if _, err := exec.LookPath(p); err != nil {
				t.Skipf("%s is not installed", p)
			}
			res, err := Run(context.Background(), Request{Provider: p, Prompt: "Reply with exactly: ok", Timeout: 3 * time.Minute})
			if errors.Is(err, ErrNotSignedIn) {
				t.Skipf("%s is not signed in", p)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s text=%q usage=%+v", p, strings.TrimSpace(res.Text), res.Usage)
			if !strings.Contains(strings.ToLower(res.Text), "ok") {
				t.Errorf("answer = %q", res.Text)
			}
		})
	}
}

// TestLiveEdit has each installed provider CLI change one file in a temp
// directory and logs the normalised events. It needs LUCID_AGENTEXEC_LIVE=1
// and LUCID_AGENTEXEC_LIVE_EDIT=1.
func TestLiveEdit(t *testing.T) {
	if os.Getenv("LUCID_AGENTEXEC_LIVE") != "1" || os.Getenv("LUCID_AGENTEXEC_LIVE_EDIT") != "1" {
		t.Skip("set LUCID_AGENTEXEC_LIVE=1 and LUCID_AGENTEXEC_LIVE_EDIT=1 to run the provider CLIs")
	}
	for _, p := range []string{"claude", "codex", "grok"} {
		t.Run(p, func(t *testing.T) {
			if _, err := exec.LookPath(p); err != nil {
				t.Skipf("%s is not installed", p)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "a.txt")
			if err := os.WriteFile(file, []byte("hello\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			res, err := Run(context.Background(), Request{Provider: p, Tools: ToolsEdit, Dir: dir, Timeout: 3 * time.Minute,
				Prompt: "Edit a.txt so it contains only the word goodbye. Then reply with: done"})
			if res != nil {
				t.Logf("%s events=%s usage=%+v", p, kinds(res.Events), res.Usage)
			}
			if errors.Is(err, ErrNotSignedIn) {
				t.Skipf("%s is not signed in", p)
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(file)
			if p == "codex" && !strings.Contains(string(b), "goodbye") {
				// Codex's workspace-write sandbox is not available on every host.
				t.Skip("codex's sandbox refused the write")
			}
			if !strings.Contains(string(b), "goodbye") {
				t.Errorf("a.txt = %q", b)
			}
		})
	}
}

// beats reads a heartbeat file's counter; 0 when it is not there yet.
func beats(file string) int {
	b, _ := os.ReadFile(file)
	n, _ := strconv.Atoi(string(b))
	return n
}

// Cancelling a run must end the CLI and every process it started, and keep the
// usage already streamed.
func TestCancelKillsTree(t *testing.T) {
	installFake(t, "claude", "tree")
	dir := t.TempDir()
	parent, child := filepath.Join(dir, "parent.beat"), filepath.Join(dir, "child.beat")
	t.Setenv("LUCID_BEAT_PARENT", parent)
	t.Setenv("LUCID_BEAT_CHILD", child)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type out struct {
		res *Result
		err error
	}
	done := make(chan out, 1)
	go func() {
		res, err := Run(ctx, Request{Provider: "claude", Prompt: "go", Dir: t.TempDir(), Tools: ToolsEdit})
		done <- out{res, err}
	}()
	// A fresh executable can take a while to start on a busy machine.
	deadline := time.Now().Add(60 * time.Second)
	for beats(parent) < 3 || beats(child) < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("the fake CLI and its child never started beating (parent %d, child %d)", beats(parent), beats(child))
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	var o out
	select {
	case o = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s of cancel")
	}
	if o.err == nil {
		t.Fatal("a cancelled run returned no error")
	}
	if u := o.res.Usage; u.InputTokens != 12 || u.OutputTokens != 34 || u.CacheRead != 5 || u.CacheWrite != 6 {
		t.Errorf("partial usage not kept: %+v", u)
	}
	p0, c0 := beats(parent), beats(child)
	time.Sleep(1500 * time.Millisecond)
	if p1 := beats(parent); p1 != p0 {
		t.Errorf("the direct child still ran after cancel: %d -> %d", p0, p1)
	}
	if c1 := beats(child); c1 != c0 {
		t.Errorf("the grandchild still ran after cancel: %d -> %d", c0, c1)
	}
}
