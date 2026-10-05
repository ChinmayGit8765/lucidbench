package themes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as a fake provider CLI: copied onto PATH as
// claude, codex or grok and run with LUCID_FAKE_CLI_MODE set, it records its
// arguments and stdin and prints a canned answer.
func TestMain(m *testing.M) {
	if mode := os.Getenv("LUCID_FAKE_CLI_MODE"); mode != "" {
		os.Exit(fakeCLI(mode))
	}
	os.Exit(m.Run())
}

const cannedBundle = "Here you go:\n```json\n" + `{"theme":{"id":"../../evil","name":"Ember Ki","version":1,"base":"dark",
"description":"Warm sparks on charcoal.",
"tokens":{"--background":"oklch(0.16 0.02 40)","--brand":"oklch(0.75 0.17 50)","--brand-2":"url(https://x.example/a.png)","--made-up":"#fff"},
"art":{"headerImage":"banner.svg","sidebarMascot":"emblem.svg","spriteBoard":[{"file":"orb.svg","caption":"Charge"},{"file":"spark.svg","caption":"Burst"},{"file":"nope.svg"}]},
"labels":{"usage_meter":"Power level","sidebar":"x"}},
"assets":{"banner.svg":"<svg viewBox=\"0 0 1200 200\"><rect width=\"1200\" height=\"200\" fill=\"#f90\"/></svg>",
"emblem.svg":"<svg viewBox=\"0 0 10 10\" onload=\"alert(1)\"><script>alert(1)</script><circle r=\"4\" cx=\"5\" cy=\"5\"/></svg>",
"orb.svg":"<svg viewBox=\"0 0 10 10\"><circle r=\"4\" cx=\"5\" cy=\"5\"/></svg>",
"spark.svg":"<svg viewBox=\"0 0 10 10\"><path d=\"M0 0L10 10\"/></svg>",
"extra.svg":"<svg viewBox=\"0 0 1 1\"/>"}}` + "\n```\n"

func fakeCLI(mode string) int {
	in, _ := io.ReadAll(os.Stdin)
	if log := os.Getenv("LUCID_FAKE_CLI_LOG"); log != "" {
		rec, _ := json.Marshal(map[string]any{"args": os.Args[1:], "stdin": string(in), "env_claude": os.Getenv("CLAUDE_CONFIG_DIR")})
		_ = os.WriteFile(log, rec, 0o600)
	}
	switch mode {
	case "claude-ok":
		env, _ := json.Marshal(map[string]any{
			"type": "result", "is_error": false, "result": cannedBundle, "total_cost_usd": 0.0421,
			"usage":      map[string]int{"input_tokens": 1200, "output_tokens": 2400, "cache_read_input_tokens": 0},
			"modelUsage": map[string]any{"claude-sonnet-x": map[string]int{}},
		})
		fmt.Println(string(env))
	case "claude-auth":
		fmt.Println(`{"type":"result","is_error":true,"result":"Not logged in · Please run /login"}`)
		return 1
	case "codex-ok":
		for i, a := range os.Args {
			if a == "-o" && i+1 < len(os.Args) {
				_ = os.WriteFile(os.Args[i+1], []byte(cannedBundle), 0o600)
			}
		}
		fmt.Println("tokens used: 1234")
	case "grok-ok":
		env, _ := json.Marshal(map[string]any{"result": cannedBundle})
		fmt.Println(string(env))
	case "garbage":
		fmt.Println(`{"type":"result","is_error":false,"result":"I cannot help with that."}`)
	case "slow":
		time.Sleep(10 * time.Second)
	}
	return 0
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

func gen() *Generator {
	return &Generator{
		InContainer: func() bool { return false },
		LookPath:    exec.LookPath,
		ProfileDir: func(provider, profile string) (string, error) {
			if profile == "work" {
				return filepath.Join(os.TempDir(), "claude-work"), nil
			}
			return "", errors.New("no such profile")
		},
		Timeout: 30 * time.Second,
	}
}

func TestGenerateWithClaude(t *testing.T) {
	log := installFake(t, "claude", "claude-ok")
	p, err := gen().Generate(context.Background(), GenerateRequest{Description: "warm sparks", Provider: "claude", Profile: "work"})
	if err != nil {
		t.Fatal(err)
	}
	var call struct {
		Args      []string `json:"args"`
		Stdin     string   `json:"stdin"`
		EnvClaude string   `json:"env_claude"`
	}
	b, _ := os.ReadFile(log)
	_ = json.Unmarshal(b, &call)
	joined := strings.Join(call.Args, " ")
	for _, flag := range []string{"-p", "--output-format json", "--safe-mode", "--strict-mcp-config", "--no-session-persistence", "--system-prompt-file"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("claude args lack %s: %v", flag, call.Args)
		}
	}
	if i := indexOf(call.Args, "--tools"); i < 0 || call.Args[i+1] != "" {
		t.Errorf("tools not disabled: %v", call.Args)
	}
	if call.Stdin != "warm sparks" || !strings.HasSuffix(call.EnvClaude, "claude-work") {
		t.Errorf("stdin %q, CLAUDE_CONFIG_DIR %q", call.Stdin, call.EnvClaude)
	}

	th := p.Theme
	if !strings.HasPrefix(th.ID, "ember-ki-") || strings.Contains(th.ID, ".") {
		t.Errorf("id = %q, want derived from the name", th.ID)
	}
	if th.Tokens["--brand"] == "" || th.Tokens["--brand-2"] != "" || th.Tokens["--made-up"] != "" {
		t.Errorf("tokens = %v", th.Tokens)
	}
	if th.Labels["usage_meter"] != "Power level" || th.Labels["sidebar"] != "" {
		t.Errorf("labels = %v", th.Labels)
	}
	if len(p.Assets) > 4 || strings.Contains(p.Assets["emblem.svg"], "script") || strings.Contains(p.Assets["emblem.svg"], "onload") {
		t.Errorf("assets = %v", p.Assets)
	}
	if len(th.Art.SpriteBoard) != 2 {
		t.Errorf("sprites = %+v", th.Art.SpriteBoard)
	}
	if p.Usage.CostUSD != 0.0421 || p.Usage.InputTokens != 1200 || p.Usage.OutputTokens != 2400 || p.Usage.Model != "claude-sonnet-x" {
		t.Errorf("usage = %+v", p.Usage)
	}
	if len(p.Dropped) == 0 {
		t.Error("nothing reported as dropped")
	}
	// The preview saves as is.
	if _, err := (&Store{Dir: t.TempDir()}).Save(p.Bundle); err != nil {
		t.Errorf("preview does not save: %v", err)
	}
}

func indexOf(xs []string, s string) int {
	for i, x := range xs {
		if x == s && i+1 < len(xs) {
			return i
		}
	}
	return -1
}

func TestGenerateWithCodexAndGrok(t *testing.T) {
	for _, prov := range []string{"codex", "grok"} {
		t.Run(prov, func(t *testing.T) {
			installFake(t, prov, prov+"-ok")
			p, err := gen().Generate(context.Background(), GenerateRequest{Description: "warm sparks", Provider: prov})
			if err != nil {
				t.Fatal(err)
			}
			if p.Theme.Name != "Ember Ki" {
				t.Errorf("theme = %+v", p.Theme)
			}
		})
	}
}

func TestGenerateErrors(t *testing.T) {
	ctx := context.Background()
	req := GenerateRequest{Description: "warm sparks", Provider: "claude"}

	installFake(t, "claude", "claude-auth")
	if _, err := gen().Generate(ctx, req); !errors.Is(err, ErrNotSignedIn) {
		t.Errorf("signed out: %v", err)
	}
	installFake(t, "claude", "garbage")
	if _, err := gen().Generate(ctx, req); !errors.Is(err, ErrBadOutput) {
		t.Errorf("garbage: %v", err)
	}
	installFake(t, "claude", "slow")
	g := gen()
	g.Timeout = 500 * time.Millisecond
	if _, err := g.Generate(ctx, req); !errors.Is(err, ErrTimeout) {
		t.Errorf("slow: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := gen().Generate(ctx, req); !errors.Is(err, ErrCLIMissing) {
		t.Errorf("missing: %v", err)
	}
	g = gen()
	g.InContainer = func() bool { return true }
	if _, err := g.Generate(ctx, req); !errors.Is(err, ErrInContainer) {
		t.Errorf("container: %v", err)
	}
	for _, bad := range []GenerateRequest{
		{Description: "x", Provider: "claude"},
		{Description: "warm", Provider: "cursor"},
		{Description: "warm", Provider: "claude", Profile: "../x"},
	} {
		if _, err := gen().Generate(ctx, bad); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestGenerateHTTP(t *testing.T) {
	g := gen()
	g.InContainer = func() bool { return true }
	mux := http.NewServeMux()
	RegisterGenerate(mux, g)
	body := `{"description":"warm sparks","provider":"claude"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/themes/generate", strings.NewReader(body)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no confirm = %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/themes/generate", strings.NewReader(body))
	req.Header.Set("X-Lucid-Confirm", "yes")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "desktop app") {
		t.Errorf("in container = %d %s", rec.Code, rec.Body)
	}
}

func TestPromptNamesNoFranchise(t *testing.T) {
	if !strings.Contains(SystemPrompt, "ORIGINAL") || !strings.Contains(SystemPrompt, "Never draw, trace or name existing characters") {
		t.Error("the prompt lost its originality rule")
	}
}
