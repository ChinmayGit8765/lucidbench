package work

import (
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Work's prompt is the builder template: the safety rules first, then the
// task, then the verify and report sections.
func TestPromptHasVerifyAndReport(t *testing.T) {
	card := &task{card: &boards.Card{ID: "c-1"}, title: "Say hello", body: "# Say hello\n\n## Done criteria\n- [ ] D1 — README says hello — proof: cat README.md",
		project: projects.Project{ID: "demo", Assessment: &assess.Assessment{Kind: "cli-library", Answers: map[string]string{"language": "go"}}}}
	got := prompt(card, "Keep it short.", []string{"go", "git status"})
	order := []string{
		Preamble,
		"- You may run only these commands without asking: `go`, `git status`. Anything else is refused",
		"# Task: Say hello",
		"## Notes from the user\n\nKeep it short.",
		"## Verify\n\n- Before you finish, check every done criterion",
		"- The checks this project needs: `go test`, `go vet`, `gofmt`.",
		"## Report\n\nEnd with a short report in three parts:",
		"3. Not done:",
	}
	at := 0
	for _, want := range order {
		i := strings.Index(got[at:], want)
		if i < 0 {
			t.Fatalf("prompt lacks %q after offset %d:\n%s", want, at, got)
		}
		at += i + len(want)
	}
	if !strings.HasPrefix(got, Preamble) {
		t.Errorf("the prompt does not open with the preamble:\n%s", got)
	}
	for _, rule := range []string{"Work only inside this worktree", "Do not push", "Commit your changes"} {
		if !strings.Contains(Preamble, rule) {
			t.Errorf("the preamble lost %q:\n%s", rule, Preamble)
		}
	}

	// A free prompt keeps the "# Task" heading the session view looks for.
	free := prompt(&task{}, "Add a line.", nil)
	if !strings.Contains(free, "\n# Task\n\nAdd a line.\n") || strings.Contains(free, "You may run only") {
		t.Errorf("free prompt:\n%s", free)
	}

	// A prompt composed in Prompt Studio with its own Verify and Report keeps
	// them, and gets no second copy.
	// Its own constraints join Work's under one heading, after the safety rules.
	composed := "## Constraints\n\n- Do not touch go.mod.\n\n## Context\n\nA CLI.\n\n# Task\n\nAdd a flag.\n\n## Verify\n\nRun go test.\n\n## Report\n\nOne line."
	got = prompt(&task{}, composed, []string{"go"})
	if strings.Count(got, "## Verify") != 1 || strings.Count(got, "## Report") != 1 || !strings.Contains(got, "Run go test.") ||
		strings.Contains(got, "# Task\n\n## Context") || !strings.HasPrefix(got, Preamble) || strings.Count(got, "## Constraints") != 1 ||
		!strings.Contains(got, Preamble+"\n- Do not touch go.mod.\n- You may run only these commands without asking: `go`.") {
		t.Errorf("composed prompt:\n%s", got)
	}
	if title := firstLine(taskPart(composed), 80); title != "Add a flag." {
		t.Errorf("title of a composed prompt: %q", title)
	}
	if title := firstLine(taskPart("Fix the build"), 80); title != "Fix the build" {
		t.Errorf("title of a plain prompt: %q", title)
	}
}
