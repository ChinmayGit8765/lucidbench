package work

import (
	"slices"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Every check command a kind can suggest must pass CleanAllowed, or Work
// would silently drop it.
func TestAssessmentCommandsAreAllowable(t *testing.T) {
	for _, k := range assess.Kinds() {
		for i, r := range k.Rules {
			clean, refused := CleanAllowed(r.Commands)
			if len(refused) > 0 || len(clean) != len(r.Commands) {
				t.Errorf("%s rule %d: refused %v", k.ID, i+1, refused)
			}
		}
	}
}

func TestProjectAllowedAddsAssessmentChecks(t *testing.T) {
	a := &assess.Assessment{Kind: "cli-library", Answers: map[string]string{
		"shape": "library", "language": "go", "consumers": "just-me", "stable-api": "false", "published": "false", "docs": "readme",
	}}
	bare := t.TempDir()

	// No work block: the stack defaults (none here) plus the assessment's.
	got := projectAllowed(projects.Project{LocalPath: bare, Assessment: a})
	for _, w := range []string{"go test", "go vet", "gofmt", "git status"} {
		if !slices.Contains(got, w) {
			t.Errorf("%q missing from %v", w, got)
		}
	}
	// A work block replaces the stack part, not the assessment's checks.
	got = projectAllowed(projects.Project{LocalPath: bare, Assessment: a, Work: &projects.WorkConfig{AllowedCommands: []string{"make"}}})
	if !slices.Contains(got, "make") || !slices.Contains(got, "go test") {
		t.Errorf("work block + assessment: %v", got)
	}
	// Without an assessment nothing is added.
	if got := projectAllowed(projects.Project{LocalPath: bare}); slices.Contains(got, "go test") {
		t.Errorf("unassessed project got %v", got)
	}
}
