package work

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

// The project's team builder supplies the provider, model and commands a
// session starts with when the request leaves them out.
func TestTeamBuilderDefaults(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	claudeLog, _ := installFakes(t, "edit")
	f.svc.Team = func(project string) (*Builder, error) {
		return &Builder{Provider: "claude", Model: "opus", AllowedCommands: []string{"make"}, Source: "repo"}, nil
	}
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Add a README line saying hello."})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	if se.Provider != "claude" || se.Model != "opus" || se.Team != "repo" {
		t.Errorf("session provider %s model %s team %q", se.Provider, se.Model, se.Team)
	}
	// The team's commands replace the stack part, like work.allowed_commands.
	if !slices.Contains(se.AllowedCommands, "make") || slices.Contains(se.AllowedCommands, "go") || !slices.Contains(se.AllowedCommands, "git status") {
		t.Errorf("allowed %v", se.AllowedCommands)
	}
	var rec struct {
		Args []string `json:"args"`
	}
	data, _ := os.ReadFile(claudeLog)
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if a := strings.Join(rec.Args, " "); !strings.Contains(a, "--model opus") || !strings.Contains(a, "Bash(make:*)") {
		t.Errorf("claude ran with %s", a)
	}

	// The New Session form sees the same defaults.
	cmds, b, err := f.svc.Defaults("demo")
	if err != nil || b == nil || b.Model != "opus" || !slices.Contains(cmds, "make") {
		t.Errorf("defaults %v %+v %v", cmds, b, err)
	}
}

// A provider chosen in the request wins; the team's model then applies only
// when the providers match.
func TestRequestBeatsTeamBuilder(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	claudeLog, _ := installFakes(t, "edit")
	f.svc.Team = func(project string) (*Builder, error) {
		return &Builder{Provider: "codex", Model: "gpt-5-codex", Source: "data"}, nil
	}
	se, err := f.svc.Start(StartRequest{Project: "demo", Provider: "claude", Prompt: "Add a README line saying hello."})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	if se.Provider != "claude" || se.Model != "" {
		t.Errorf("provider %s model %q", se.Provider, se.Model)
	}
	data, _ := os.ReadFile(claudeLog)
	if strings.Contains(string(data), "--model") {
		t.Errorf("claude got the codex model: %s", data)
	}
	// No team and no provider: the request is refused.
	f.svc.Team = nil
	if _, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "x"}); !errors.Is(err, ErrBadRequest) {
		t.Errorf("no provider: %v", err)
	}
}

// A confidential project is refused whatever its team says, before any CLI
// starts.
func TestTeamNeverOverridesConfidential(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	claudeLog, _ := installFakes(t, "edit")
	f.svc.Team = func(project string) (*Builder, error) {
		return &Builder{Provider: "claude", Model: "opus", Source: "repo"}, nil
	}
	if _, err := f.svc.Start(StartRequest{Project: "secret", Prompt: "x"}); !errors.Is(err, ErrRefused) {
		t.Fatalf("confidential: %v", err)
	}
	if _, err := os.Stat(claudeLog); err == nil {
		t.Error("claude ran for a confidential project")
	}
}
