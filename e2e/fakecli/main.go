// Command fakecli stands in for every outside CLI the end-to-end tests could
// reach: claude, codex, grok, gh and docker. The e2e harness builds it once
// and copies it onto a PATH of its own under each of those names; it acts on
// the name it was started as. It never talks to a network or a provider.
//
//   - claude: in Work (stream-json) it writes a file and commits it in its
//     working directory, reporting a session id; a follow-up (--resume with
//     that id, or a prompt carrying Work's summary of the earlier turns)
//     adds a line and commits again. In the council it answers with a
//     brief, or with an "ok" critique when the council-critique prompt is in
//     its input; asked for a section (Settings › Sections), it answers with
//     one.
//   - codex, grok: critics; they always answer "ok".
//   - gh: `pr create` prints a PR URL; `pr view` reports the state written
//     in $LUCID_E2E_STATE/pr-state (OPEN unless a test wrote MERGED).
//   - docker: there is no engine.
//
// Every call is appended to $LUCID_E2E_STATE/calls.log.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type m = map[string]any

func out(v any) {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
}

func main() {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	args := os.Args[1:]
	logCall(name, args)
	if len(args) > 0 && (args[0] == "--version" || args[0] == "version") && name != "docker" {
		fmt.Printf("%s 0.0.0-fake\n", name)
		return
	}
	switch name {
	case "claude":
		os.Exit(claude(args))
	case "codex":
		os.Exit(codex(args))
	case "grok":
		out(m{"result": okCritique, "usage": m{"input_tokens": 70, "output_tokens": 10}})
	case "gh":
		os.Exit(gh(args))
	case "docker":
		fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon (fake docker for tests)")
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "fakecli: unknown name %q\n", name)
		os.Exit(2)
	}
}

func stateDir() string { return os.Getenv("LUCID_E2E_STATE") }

func logCall(name string, args []string) {
	dir := stateDir()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "calls.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", name, strings.Join(args, " "))
}

const okCritique = `{"verdict": "ok", "points": [{"severity": "minor", "text": "Say which build command each project uses when it has several."}]}`

const brief = `# Report which side projects still build

## Problem
Side projects drift: a dependency moves and a project stops building, and nobody notices until it is needed.

## Outcome
One command prints, for every project in the folder, whether its own build or tests pass and how long that took.

## Done criteria
- [ ] D1 — Running the script lists every project with pass or fail — proof: run it on a folder of three projects and read the output
- [ ] D2 — A failing project is reported without stopping the others — proof: break one project and run it again
- [ ] D3 — Nothing in any repository changes — proof: git status is clean in each after a run

## Risks
- A build that hangs blocks the rest; give each a timeout.
- Some builds write files; run them in a temporary copy.

## First steps
1. List the projects in the folder.
2. Pick each project's build command from its files.
3. Run them one by one and print a line each.

## Open questions
- Which folder holds the projects?
`

// section is what "Describe a section" gets back: the briefs waiting for
// approval, as a list.
const section = `{"id": "briefs-waiting", "title": "Briefs waiting for me", "description": "Council briefs to approve, newest first.",
 "source": {"api": "/api/council/sessions"}, "view": "list", "filter": [{"field": "status", "op": "eq", "value": "draft"}],
 "sort": {"field": "updated", "desc": true}, "fields": [{"path": "title"}, {"path": "project", "format": "badge"}, {"path": "updated", "format": "relative"}]}`

// claude answers the council (no tools, one JSON result) or does Work's
// edit-and-commit run (stream-json).
func claude(args []string) int {
	in, _ := io.ReadAll(os.Stdin)
	if has(args, "stream-json") {
		if resumes(args) || strings.Contains(string(in), "## Follow-up from the user") {
			return workAgain(string(in), resumes(args))
		}
		return work()
	}
	text := string(in) + " " + strings.Join(args, " ")
	for i, a := range args {
		if a == "--system-prompt-file" && i+1 < len(args) {
			b, _ := os.ReadFile(args[i+1])
			text += string(b)
		}
	}
	answer := brief
	switch {
	case strings.Contains(text, "council-critique"):
		answer = okCritique
	case strings.Contains(text, "You design one dashboard section"):
		answer = section
	}
	out(m{"type": "result", "is_error": false, "result": answer, "total_cost_usd": 0.02,
		"usage": m{"input_tokens": 100, "output_tokens": 50}, "modelUsage": m{"claude-fake": m{}}})
	return 0
}

func has(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}

// sessionID is the claude session every Work run reports and a follow-up resumes.
const sessionID = "0e2e0e2e-1111-4222-8333-444455556666"

// resumes reports whether the arguments resume the fake's session.
func resumes(args []string) bool {
	for i, a := range args {
		if a == "--resume" && i+1 < len(args) && args[i+1] == sessionID {
			return true
		}
	}
	return false
}

// git runs git in the working directory, its errors on stderr.
func git(a ...string) error {
	cmd := exec.Command("git", a...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// workAgain is a follow-up turn: it adds the follow-up to NOTES.md and
// commits it, saying how it was reached.
func workAgain(prompt string, resumed bool) int {
	out(m{"type": "system", "subtype": "init", "session_id": sessionID})
	how := "from Work's summary of the earlier turns"
	if resumed {
		how = "resumed with --resume"
	}
	ask := strings.TrimSpace(prompt)
	if i := strings.LastIndex(ask, "## Follow-up from the user"); i >= 0 {
		ask = strings.TrimSpace(ask[i+len("## Follow-up from the user"):])
	}
	out(m{"type": "assistant", "message": m{"content": []any{
		m{"type": "text", "text": "Picking up where I left off (" + how + ")."},
		m{"type": "tool_use", "id": "t3", "name": "Edit", "input": m{"file_path": "NOTES.md", "old_string": "Every project builds.\n", "new_string": "Every project builds.\n\n- " + firstLine(ask) + "\n"}}}}})
	time.Sleep(1500 * time.Millisecond)
	b, err := os.ReadFile("NOTES.md")
	if err != nil {
		b = []byte("# Build report\n")
	}
	if err := os.WriteFile("NOTES.md", append(b, []byte("\n- "+firstLine(ask)+"\n")...), 0o644); err != nil {
		return 1
	}
	out(m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t3", "content": "File edited"}}}})
	if err := git("add", "NOTES.md"); err != nil {
		return 1
	}
	if err := git("commit", "-q", "-m", "docs: answer the follow-up"); err != nil {
		return 1
	}
	out(m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": "Done: the follow-up is in NOTES.md and committed."}}}})
	out(m{"type": "result", "is_error": false, "result": "Done again.", "total_cost_usd": 0.03, "session_id": sessionID,
		"usage": m{"input_tokens": 4, "output_tokens": 40}, "modelUsage": m{"claude-fake": m{}}})
	return 0
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return l
}

// work writes NOTES.md, commits it and reports, the way an agent would.
func work() int {
	out(m{"type": "system", "subtype": "init", "session_id": sessionID})
	out(m{"type": "assistant", "message": m{"content": []any{
		m{"type": "text", "text": "I'll add the notes file and commit it."},
		m{"type": "tool_use", "id": "t1", "name": "Write", "input": m{"file_path": "NOTES.md", "content": "# Build report\n"}}}}})
	time.Sleep(1500 * time.Millisecond) // long enough for the UI to show it running
	if err := os.WriteFile("NOTES.md", []byte("# Build report\n\nEvery project builds.\n"), 0o644); err != nil {
		return 1
	}
	out(m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t1", "content": "File written"}}}})
	out(m{"type": "assistant", "message": m{"content": []any{
		m{"type": "tool_use", "id": "t2", "name": "Bash", "input": m{"command": "git add NOTES.md && git commit -m 'docs: add the build report'"}}}}})
	for _, a := range [][]string{{"add", "NOTES.md"}, {"commit", "-q", "-m", "docs: add the build report"}} {
		cmd := exec.Command("git", a...)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "fake claude: git", a[0], err)
			return 1
		}
	}
	out(m{"type": "user", "message": m{"content": []any{m{"type": "tool_result", "tool_use_id": "t2", "content": "[lucid] docs: add the build report"}}}})
	out(m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": "Done: NOTES.md is committed."}}}})
	out(m{"type": "result", "is_error": false, "result": "Done.", "total_cost_usd": 0.04,
		"usage": m{"input_tokens": 6, "output_tokens": 70}, "modelUsage": m{"claude-fake": m{}}})
	return 0
}

// codex answers a critique into the -o file.
func codex(args []string) int {
	_, _ = io.ReadAll(os.Stdin)
	for i, a := range args {
		if a == "-o" && i+1 < len(args) {
			_ = os.WriteFile(args[i+1], []byte(okCritique), 0o600)
		}
	}
	out(m{"type": "turn.completed", "usage": m{"input_tokens": 80, "cached_input_tokens": 0, "output_tokens": 20}})
	return 0
}

func gh(args []string) int {
	if len(args) >= 2 && args[0] == "pr" && args[1] == "create" {
		fmt.Println("Creating draft pull request\n\nhttps://github.com/example/demo/pull/7")
		return 0
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "view" {
		state := "OPEN"
		if b, err := os.ReadFile(filepath.Join(stateDir(), "pr-state")); err == nil {
			state = strings.TrimSpace(string(b))
		}
		if state == "MERGED" {
			out(m{"state": "MERGED", "isDraft": false, "mergedAt": time.Now().UTC().Format(time.RFC3339), "statusCheckRollup": []any{}})
		} else {
			out(m{"state": state, "isDraft": true, "mergedAt": nil, "statusCheckRollup": []any{m{"__typename": "CheckRun", "status": "COMPLETED", "conclusion": "SUCCESS"}}})
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "fake gh: not supported in tests:", strings.Join(args, " "))
	return 1
}
