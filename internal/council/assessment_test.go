package council

import (
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

func assessedProjects() (*projects.List, error) {
	l, _ := testProjects()
	l.Projects = append(l.Projects, projects.Project{
		ID: "demo-game", Name: "Demo game", Category: "experiment", Type: "game", Status: "active", Visibility: "private",
		Assessment: &assess.Assessment{Kind: "game", Answers: map[string]string{
			"engine": "godot", "stage": "prototype", "audience": "just-me", "multiplayer": "true", "saves": "false",
		}},
	})
	return l, nil
}

func TestProposePromptCarriesAssessmentCriteria(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Add a lobby"), brief("Add a lobby")},
		"codex":  {ok},
		"grok":   {ok},
	})
	s, _ := newService(t)
	s.Projects = assessedProjects
	start(t, s, StartRequest{Input: "Players need a lobby before a match.", Project: "demo-game"})

	first := calls(t, script, "claude")[0]
	for _, want := range []string{"Done criteria suggested for this kind of project", "Two clients play one session without desync", "proof:"} {
		if !strings.Contains(first, want) {
			t.Errorf("propose prompt lacks %q:\n%s", want, first)
		}
	}
	// Only the proposer's first call carries them.
	for _, c := range calls(t, script, "codex") {
		if strings.Contains(c, "Done criteria suggested") {
			t.Errorf("a critique prompt carries the suggested criteria:\n%s", c)
		}
	}
}

func TestProposePromptWithoutAssessmentIsUnchanged(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Clean up"), brief("Clean up")},
		"codex":  {ok},
		"grok":   {ok},
	})
	s, _ := newService(t)
	s.Projects = assessedProjects
	start(t, s, StartRequest{Input: dump, Project: "demo-app"})
	if first := calls(t, script, "claude")[0]; strings.Contains(first, "Done criteria suggested") {
		t.Errorf("a project without an assessment got criteria:\n%s", first)
	}
	if got := proposePrompt("x", "", nil); got != "The braindump:\nx" {
		t.Errorf("proposePrompt = %q", got)
	}
}
