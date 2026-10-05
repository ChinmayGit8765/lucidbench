package themes

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SystemPrompt is the fixed instruction every provider gets. It is versioned
// with the code in prompts/theme.md.
//
//go:embed prompts/theme.md
var SystemPrompt string

// GenerateRequest is the body of POST /api/themes/generate.
type GenerateRequest struct {
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Profile     string `json:"profile,omitempty"`
}

// Usage is what one generation cost, as far as the CLI reports it.
type Usage struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CacheRead    int     `json:"cache_read_tokens,omitempty"`
	CacheWrite   int     `json:"cache_write_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	DurationMS   int64   `json:"duration_ms"`
}

// Preview is a generated theme, validated and sanitised but not saved. Save
// it with POST /api/themes.
type Preview struct {
	Bundle
	Usage   Usage    `json:"usage"`
	Dropped []string `json:"dropped"`
}

// Errors returned by Generate. The HTTP layer maps each to a status.
var (
	ErrInContainer = errors.New("theme generation runs the provider's CLI on this machine, but this lucidd runs in a container: use the desktop app or a lucidd started on the host")
	ErrCLIMissing  = errors.New("CLI not found")
	ErrNotSignedIn = errors.New("CLI is not signed in")
	ErrTimeout     = errors.New("generation timed out")
	ErrBadOutput   = errors.New("the model did not return a usable theme")
	ErrBadRequest  = errors.New("bad request")
)

// Providers lists the CLIs a theme can be generated with, and how to sign in
// to each.
var Providers = map[string]string{
	"claude": "claude (then /login)",
	"codex":  "codex login",
	"grok":   "grok login",
}

// GenerateTimeout bounds one generation.
const GenerateTimeout = 120 * time.Second

// Generator runs a provider CLI on the host.
type Generator struct {
	InContainer func() bool
	LookPath    func(string) (string, error)
	// ProfileDir returns the config directory of a host account profile, for
	// CLAUDE_CONFIG_DIR or CODEX_HOME.
	ProfileDir func(provider, profile string) (string, error)
	Timeout    time.Duration
}

var profileRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// args returns the CLI arguments, the text sent on stdin, and for codex the
// file its answer is written to. Every CLI runs with no tools, no MCP
// servers, no hooks and no saved session, in an empty temporary directory.
func args(provider, dir, description string) (argv []string, stdin string, outFile string) {
	switch provider {
	case "claude":
		return []string{
			"-p", "--output-format", "json",
			"--model", "sonnet",
			"--system-prompt-file", filepath.Join(dir, "theme.md"),
			"--safe-mode", "--strict-mcp-config", "--tools", "",
			"--no-session-persistence",
		}, description, ""
	case "codex":
		out := filepath.Join(dir, "answer.txt")
		return []string{
			"exec", "--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--ignore-rules",
			"--sandbox", "read-only", "--color", "never", "-o", out, "-",
		}, SystemPrompt + "\n\nThe user's description:\n" + description, out
	case "grok":
		return []string{
			"-p", SystemPrompt + "\n\nThe user's description:\n" + description,
			"--output-format", "json", "--disable-web-search", "--no-subagents", "--max-turns", "1", "--cwd", dir,
		}, "", ""
	}
	return nil, "", ""
}

var authRE = regexp.MustCompile(`(?i)(not logged in|log ?in required|please (log|sign) ?in|/login|unauthori[sz]ed|authenticat|invalid api key|credentials)`)

// Generate asks the provider for a theme and returns a sanitised preview.
func (g *Generator) Generate(ctx context.Context, req GenerateRequest) (*Preview, error) {
	req.Description = strings.TrimSpace(req.Description)
	if n := len(req.Description); n < 3 || n > 500 {
		return nil, fmt.Errorf("%w: description must be 3-500 characters", ErrBadRequest)
	}
	login, ok := Providers[req.Provider]
	if !ok {
		return nil, fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
	}
	if req.Profile != "" && !profileRE.MatchString(req.Profile) {
		return nil, fmt.Errorf("%w: invalid profile name", ErrBadRequest)
	}
	if g.InContainer != nil && g.InContainer() {
		return nil, ErrInContainer
	}
	bin, err := g.LookPath(req.Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not on PATH; install it, sign in with `%s`, then try again", ErrCLIMissing, req.Provider, login)
	}
	env := os.Environ()
	if req.Profile != "" && req.Profile != "default" {
		if req.Provider == "grok" || g.ProfileDir == nil {
			return nil, fmt.Errorf("%w: %s has no selectable profiles", ErrBadRequest, req.Provider)
		}
		dir, err := g.ProfileDir(req.Provider, req.Profile)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
		}
		key := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}[req.Provider]
		env = append(env, key+"="+dir)
	}

	tmp, err := os.MkdirTemp("", "lucid-theme-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "theme.md"), []byte(SystemPrompt), 0o600); err != nil {
		return nil, err
	}
	argv, stdin, outFile := args(req.Provider, tmp, req.Description)

	timeout := g.Timeout
	if timeout == 0 {
		timeout = GenerateTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, argv...)
	cmd.Dir = tmp
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second
	start := time.Now()
	runErr := cmd.Run()
	usage := Usage{Provider: req.Provider, DurationMS: time.Since(start).Milliseconds()}
	if cctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%w after %s", ErrTimeout, timeout)
	}

	text, isErr := stdout.String(), false
	switch req.Provider {
	case "claude":
		text, isErr = parseClaude(stdout.Bytes(), &usage)
	case "codex":
		if b, err := os.ReadFile(outFile); err == nil {
			text = string(b)
		}
	case "grok":
		text = parseGeneric(stdout.Bytes(), &usage)
	}
	if runErr != nil || isErr {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(text)
		}
		if authRE.MatchString(detail) {
			return nil, fmt.Errorf("%w: sign in with `%s` in a terminal, then try again", ErrNotSignedIn, login)
		}
		if runErr != nil && detail == "" {
			detail = runErr.Error()
		}
		return nil, fmt.Errorf("%s failed: %s", req.Provider, excerpt(detail))
	}
	b, err := parseBundle(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %v (answer began: %q)", ErrBadOutput, err, excerpt(text))
	}
	p, err := prepare(b, req.Description)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
	}
	p.Usage = usage
	return p, nil
}

func excerpt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// parseClaude reads `claude -p --output-format json`: the answer is in
// "result", with cost and token counts alongside.
func parseClaude(out []byte, u *Usage) (string, bool) {
	var env struct {
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		Usage   struct {
			In         int `json:"input_tokens"`
			Out        int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		ModelUsage map[string]json.RawMessage `json:"modelUsage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &env); err != nil {
		return string(out), false
	}
	u.InputTokens, u.OutputTokens = env.Usage.In, env.Usage.Out
	u.CacheRead, u.CacheWrite, u.CostUSD = env.Usage.CacheRead, env.Usage.CacheWrite, env.Cost
	models := make([]string, 0, len(env.ModelUsage))
	for m := range env.ModelUsage {
		models = append(models, m)
	}
	sort.Strings(models)
	u.Model = strings.Join(models, ", ")
	return env.Result, env.IsError
}

// parseGeneric reads a JSON envelope whose answer sits in one of the usual
// fields, or returns the output as is.
func parseGeneric(out []byte, u *Usage) string {
	var env map[string]json.RawMessage
	if json.Unmarshal(bytes.TrimSpace(out), &env) != nil {
		return string(out)
	}
	for _, k := range []string{"result", "text", "content", "response", "output", "message"} {
		var s string
		if raw, ok := env[k]; ok && json.Unmarshal(raw, &s) == nil && s != "" {
			var usage struct {
				In  int `json:"input_tokens"`
				Out int `json:"output_tokens"`
			}
			if raw, ok := env["usage"]; ok {
				_ = json.Unmarshal(raw, &usage)
				u.InputTokens, u.OutputTokens = usage.In, usage.Out
			}
			return s
		}
	}
	return string(out)
}

// parseBundle finds the JSON object in a model answer, tolerating Markdown
// fences and stray prose around it.
func parseBundle(text string) (Bundle, error) {
	var b Bundle
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return b, errors.New("no JSON object in the answer")
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &b); err != nil {
		return b, fmt.Errorf("invalid JSON: %v", err)
	}
	return b, nil
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// prepare turns model output into a valid preview: at most 4 SVG assets,
// each sanitised; whatever else fails validation is dropped and reported;
// the id is derived from the name, never taken from the model.
func prepare(b Bundle, description string) (*Preview, error) {
	p := &Preview{Dropped: []string{}}
	keep := map[string]bool{}
	assets := map[string]string{}
	// Files the art uses come first, so the four kept are ones that show.
	referenced := map[string]bool{}
	for _, f := range b.Theme.Art.Files() {
		referenced[f] = true
	}
	names := make([]string, 0, len(b.Assets))
	for n := range b.Assets {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if referenced[names[i]] != referenced[names[j]] {
			return referenced[names[i]]
		}
		return names[i] < names[j]
	})
	for _, n := range names {
		if len(assets) == 4 || !ValidFile(n) || filepath.Ext(n) != ".svg" {
			p.Dropped = append(p.Dropped, "asset "+n)
			continue
		}
		clean, err := SanitizeSVG([]byte(b.Assets[n]))
		if err != nil {
			p.Dropped = append(p.Dropped, "asset "+n)
			continue
		}
		assets[n] = string(clean)
		keep[n] = true
	}
	t := b.Theme
	t.Version, t.BuiltIn = Version, false
	if t.Base != "light" {
		t.Base = "dark"
	}
	if t.Name = strings.TrimSpace(t.Name); t.Name == "" || len(t.Name) > 60 || controlRE.MatchString(t.Name) {
		t.Name = "Generated theme"
	}
	slug := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(t.Name), "-"), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "theme"
	}
	sum := sha256.Sum256([]byte(description + t.Name + time.Now().String()))
	t.ID = slug + "-" + hex.EncodeToString(sum[:2])
	p.Dropped = append(p.Dropped, t.Clean(keep)...)
	if t.Art != nil && len(t.Art.Files()) == 0 {
		t.Art = nil
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	// Keep only the assets the art uses.
	used := map[string]bool{}
	for _, f := range t.Art.Files() {
		used[f] = true
	}
	for n := range assets {
		if !used[n] {
			delete(assets, n)
		}
	}
	p.Theme, p.Assets = t, assets
	return p, nil
}
