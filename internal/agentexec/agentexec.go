// Package agentexec runs a provider CLI (claude, codex or grok) once, on the
// host, with the user's own signed-in account. Themes, Council and Work all
// use it. No credentials are read or stored here: the CLI finds its own.
//
// Two modes:
//
//   - ToolsNone: no tools, no MCP servers, no hooks, no saved session, in an
//     empty temporary directory. One answer comes back in Result.Text.
//   - ToolsEdit: the agent may edit files inside Request.Dir. The CLI streams
//     JSON, which is normalised into Events (text, tool, tool_result, diff,
//     error, done) as it arrives.
package agentexec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ToolMode says what the agent may do.
type ToolMode int

const (
	// ToolsNone gives the agent no tools: a plain question and answer.
	ToolsNone ToolMode = iota
	// ToolsEdit lets the agent read and edit files inside Request.Dir.
	ToolsEdit
)

// Harness values for Request.Harness.
const (
	// HarnessMine lets the CLI load the user's normal settings, hooks and
	// instructions.
	HarnessMine = "mine"
	// HarnessClean adds the CLI's no-settings, no-hooks flags. Grok has no
	// such flag, so for grok "clean" and "mine" run the same way.
	HarnessClean = "clean"
)

// Event kinds.
const (
	KindText       = "text"
	KindTool       = "tool"
	KindToolResult = "tool_result"
	KindDiff       = "diff"
	KindApproval   = "approval"
	KindError      = "error"
	KindDone       = "done"
)

// Request is one run.
type Request struct {
	Provider, Profile string // claude | codex | grok; profile "" = default
	SystemPrompt      string
	Prompt            string
	Dir               string // working directory; "" = a new empty temp dir
	Tools             ToolMode
	Model             string // optional provider model alias
	Harness           string // "mine" | "clean"; "" = clean. ToolsNone is always clean.
	Timeout           time.Duration
	OnEvent           func(Event) // optional streaming callback (Work)
}

// Usage is what one run cost, as far as the CLI reports it.
type Usage struct {
	Provider     string  `json:"provider"`
	Model        string  `json:"model,omitempty"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	CacheRead    int64   `json:"cache_read_tokens,omitempty"`
	CacheWrite   int64   `json:"cache_write_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	DurationMS   int64   `json:"duration_ms"`
}

// Event is one normalised step of a run. Raw is the CLI's own line, when it
// is small enough to keep.
type Event struct {
	Time  time.Time       `json:"time"`
	Kind  string          `json:"kind"`
	Title string          `json:"title,omitempty"`
	Body  string          `json:"body,omitempty"`
	Raw   json.RawMessage `json:"raw,omitempty"`
}

// Result is a finished run. On failure Run still returns the Result with
// whatever was seen before the error.
type Result struct {
	Text   string  `json:"text"`
	Usage  Usage   `json:"usage"`
	Events []Event `json:"events"`
}

// Errors returned by Run. Callers map each to a status.
var (
	ErrInContainer = errors.New("this runs the provider's CLI on this machine, but this lucidd runs in a container: use the desktop app or a lucidd started on the host")
	ErrCLIMissing  = errors.New("CLI not found")
	ErrNotSignedIn = errors.New("CLI is not signed in")
	ErrTimeout     = errors.New("run timed out")
	ErrBadRequest  = errors.New("bad request")
)

// Logins says how to sign in to each provider's CLI.
var Logins = map[string]string{
	"claude": "claude (then /login)",
	"codex":  "codex login",
	"grok":   "grok login",
}

// Default timeouts when Request.Timeout is zero.
const (
	TimeoutNone = 120 * time.Second
	TimeoutEdit = 30 * time.Minute
)

// Runner holds the host hooks. The zero value uses exec.LookPath, assumes
// no container and supports no profiles.
type Runner struct {
	InContainer func() bool
	LookPath    func(string) (string, error)
	// ProfileDir returns the config directory of a host account profile, for
	// CLAUDE_CONFIG_DIR or CODEX_HOME.
	ProfileDir func(provider, profile string) (string, error)
}

// Run runs one request with the default Runner.
func Run(ctx context.Context, r Request) (*Result, error) { return (&Runner{}).Run(ctx, r) }

var (
	profileRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	authRE    = regexp.MustCompile(`(?i)(not logged in|log ?in required|please (log|sign) ?in|/login|unauthori[sz]ed|authenticat|invalid api key|credentials)`)
)

// Run runs one request.
func (g *Runner) Run(ctx context.Context, r Request) (*Result, error) {
	login, ok := Logins[r.Provider]
	if !ok {
		return nil, fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
	}
	if r.Profile != "" && !profileRE.MatchString(r.Profile) {
		return nil, fmt.Errorf("%w: invalid profile name", ErrBadRequest)
	}
	if r.Tools != ToolsNone && r.Tools != ToolsEdit {
		return nil, fmt.Errorf("%w: unknown tool mode", ErrBadRequest)
	}
	switch r.Harness {
	case "", HarnessMine, HarnessClean:
	default:
		return nil, fmt.Errorf("%w: harness must be mine or clean", ErrBadRequest)
	}
	if r.Tools == ToolsEdit {
		if r.Dir == "" {
			return nil, fmt.Errorf("%w: a working directory is required to edit files", ErrBadRequest)
		}
		if st, err := os.Stat(r.Dir); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("%w: working directory %q does not exist", ErrBadRequest, r.Dir)
		}
	}
	if g.InContainer != nil && g.InContainer() {
		return nil, ErrInContainer
	}
	look := g.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(r.Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not on PATH; install it, sign in with `%s`, then try again", ErrCLIMissing, r.Provider, login)
	}
	env := os.Environ()
	if r.Profile != "" && r.Profile != "default" {
		if r.Provider == "grok" || g.ProfileDir == nil {
			return nil, fmt.Errorf("%w: %s has no selectable profiles", ErrBadRequest, r.Provider)
		}
		dir, err := g.ProfileDir(r.Provider, r.Profile)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadRequest, err)
		}
		key := map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}[r.Provider]
		env = append(env, key+"="+dir)
	}

	// aux holds files the CLI reads or writes that must not land in the
	// user's working directory. With no Dir it is also the working directory.
	aux, err := os.MkdirTemp("", "lucid-agent-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(aux)
	work := r.Dir
	if work == "" {
		work = aux
	}
	a, err := buildArgs(r, aux, work)
	if err != nil {
		return nil, err
	}

	timeout := r.Timeout
	if timeout == 0 {
		timeout = TimeoutNone
		if r.Tools == ToolsEdit {
			timeout = TimeoutEdit
		}
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, a.argv...)
	cmd.Dir = work
	cmd.Env = env
	cmd.Stdin = strings.NewReader(a.stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second

	res := &Result{}
	emit := func(e Event) {
		if e.Time.IsZero() {
			e.Time = time.Now()
		}
		res.Events = append(res.Events, e)
		if r.OnEvent != nil {
			r.OnEvent(e)
		}
	}
	var sp streamParser
	var lw *lineWriter
	if a.stream {
		sp = newStreamParser(r.Provider, emit)
		lw = &lineWriter{fn: sp.line}
		cmd.Stdout = lw
	} else {
		cmd.Stdout = &stdout
	}

	start := time.Now()
	runErr := cmd.Run()
	if lw != nil {
		lw.flush()
	}
	usage := Usage{Provider: r.Provider, Model: r.Model, DurationMS: time.Since(start).Milliseconds()}
	res.Usage = usage
	if cctx.Err() == context.DeadlineExceeded {
		emit(Event{Kind: KindError, Title: "timeout", Body: fmt.Sprintf("timed out after %s", timeout)})
		emit(Event{Kind: KindDone, Title: "failed"})
		return res, fmt.Errorf("%w after %s", ErrTimeout, timeout)
	}

	var text string
	var isErr bool
	var streamErrs []string
	switch {
	case a.stream:
		sp.finish()
		text, isErr, streamErrs = sp.result(&usage)
	case r.Provider == "claude":
		text, isErr = parseClaude(stdout.Bytes(), &usage)
	case r.Provider == "codex":
		text = stdout.String()
		if b, err := os.ReadFile(a.outFile); err == nil {
			text = string(b)
		}
		parseCodexUsage(stdout.Bytes(), &usage)
	case r.Provider == "grok":
		text = parseGeneric(stdout.Bytes(), &usage)
	}
	usage.DurationMS = time.Since(start).Milliseconds()
	res.Text, res.Usage = text, usage

	if runErr != nil || isErr {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(strings.Join(streamErrs, "; "))
		}
		if detail == "" {
			detail = strings.TrimSpace(text)
		}
		var err error
		if authRE.MatchString(detail) {
			err = fmt.Errorf("%w: sign in with `%s` in a terminal, then try again", ErrNotSignedIn, login)
		} else {
			if runErr != nil && detail == "" {
				detail = runErr.Error()
			}
			err = fmt.Errorf("%s failed: %s", r.Provider, Excerpt(detail))
		}
		emit(Event{Kind: KindError, Body: Excerpt(detail)})
		emit(Event{Kind: KindDone, Title: "failed"})
		return res, err
	}
	if !a.stream {
		emit(Event{Kind: KindText, Body: text})
	}
	emit(Event{Kind: KindDone, Title: "ok"})
	return res, nil
}

// Excerpt shortens s to 300 characters for an error message.
func Excerpt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// invocation is how to start a CLI for one request.
type invocation struct {
	argv    []string
	stdin   string
	outFile string // codex ToolsNone: the file its answer is written to
	stream  bool   // stdout is JSON lines to normalise
}

// buildArgs returns the CLI arguments for r. The flags come from each CLI's
// --help. ToolsNone is always the "clean" shape: no tools, no MCP servers, no
// hooks, no saved session.
func buildArgs(r Request, aux, work string) (invocation, error) {
	clean := r.Harness != HarnessMine || r.Tools == ToolsNone
	prompt := r.Prompt
	if r.SystemPrompt != "" && r.Provider != "claude" {
		prompt = r.SystemPrompt + "\n\nThe user's request:\n" + r.Prompt
	}
	var in invocation
	switch r.Provider {
	case "claude":
		in.argv = []string{"-p"}
		in.stdin = r.Prompt
		if r.Tools == ToolsNone {
			in.argv = append(in.argv, "--output-format", "json")
		} else {
			in.argv = append(in.argv, "--output-format", "stream-json", "--verbose", "--permission-mode", "acceptEdits")
			in.stream = true
		}
		if r.Model != "" {
			in.argv = append(in.argv, "--model", r.Model)
		}
		if r.SystemPrompt != "" {
			if r.Tools == ToolsNone {
				// Replaces the default prompt; a tool-less answer wants no
				// coding-agent instructions.
				f := filepath.Join(aux, "system.md")
				if err := os.WriteFile(f, []byte(r.SystemPrompt), 0o600); err != nil {
					return in, err
				}
				in.argv = append(in.argv, "--system-prompt-file", f)
			} else {
				// Adds to the default prompt, which carries the editing rules.
				in.argv = append(in.argv, "--append-system-prompt", r.SystemPrompt)
			}
		}
		if clean {
			in.argv = append(in.argv, "--safe-mode", "--strict-mcp-config")
		}
		if r.Tools == ToolsNone {
			in.argv = append(in.argv, "--tools", "")
		}
		in.argv = append(in.argv, "--no-session-persistence")
	case "codex":
		in.argv = []string{"exec", "--skip-git-repo-check", "--ephemeral"}
		if clean {
			in.argv = append(in.argv, "--ignore-user-config", "--ignore-rules")
		}
		if r.Model != "" {
			in.argv = append(in.argv, "-m", r.Model)
		}
		if r.Tools == ToolsNone {
			in.outFile = filepath.Join(aux, "answer.txt")
			// --json adds the usage line; the answer still comes from -o.
			in.argv = append(in.argv, "--sandbox", "read-only", "--color", "never", "--json", "-o", in.outFile, "-")
		} else {
			in.argv = append(in.argv, "--sandbox", "workspace-write", "--color", "never", "--json", "-")
			in.stream = true
		}
		in.stdin = prompt
	case "grok":
		in.argv = []string{"-p", prompt}
		if r.Tools == ToolsNone {
			in.argv = append(in.argv, "--output-format", "json", "--disable-web-search", "--no-subagents", "--max-turns", "1")
		} else {
			// A work brief can outgrow the command line, so it goes in a file.
			f := filepath.Join(aux, "prompt.md")
			if err := os.WriteFile(f, []byte(prompt), 0o600); err != nil {
				return in, err
			}
			in.argv = []string{"--prompt-file", f, "--output-format", "streaming-json", "--permission-mode", "acceptEdits", "--disable-web-search", "--no-subagents"}
			in.stream = true
		}
		if r.Model != "" {
			in.argv = append(in.argv, "-m", r.Model)
		}
		in.argv = append(in.argv, "--cwd", work)
	}
	return in, nil
}
