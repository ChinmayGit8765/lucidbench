package council

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// args returns the command line a provider was started with on call n.
func args(t *testing.T, script, provider string, n int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(script, provider, fmt.Sprintf("args-%d.txt", n)))
	if err != nil {
		t.Fatalf("%s call %d: %v", provider, n, err)
	}
	return strings.ReplaceAll(string(b), "\n", " ")
}

func teamOf(proposer Seat, critics ...Seat) func(string) (*TeamRoles, error) {
	return func(project string) (*TeamRoles, error) {
		if project == "demo-app" || project == "secret-app" {
			return &TeamRoles{Proposer: &proposer, Critics: critics, Source: "repo"}, nil
		}
		return nil, nil
	}
}

// A project's team picks the proposer, the critics and their models.
func TestTeamPicksRolesAndModels(t *testing.T) {
	script := fakes(t, map[string][]string{
		"codex": {brief("Clean up stale run folders")},
		"grok":  {ok},
	})
	s, _ := newService(t)
	s.Team = teamOf(Seat{Provider: "codex", Model: "gpt-5-codex"}, Seat{Provider: "grok", Model: "grok-4"})
	sess := start(t, s, StartRequest{Input: dump, Project: "demo-app"})
	if sess.Proposer != "codex" || len(sess.Critics) != 1 || sess.Critics[0] != "grok" || sess.Team != "repo" {
		t.Fatalf("proposer %s critics %v team %q", sess.Proposer, sess.Critics, sess.Team)
	}
	if a := args(t, script, "codex", 1); !strings.Contains(a, "-m gpt-5-codex") {
		t.Errorf("codex ran without the team's model: %s", a)
	}
	if a := args(t, script, "grok", 1); !strings.Contains(a, "-m grok-4") {
		t.Errorf("grok ran without the team's model: %s", a)
	}
	if p := sess.Rounds[0].Proposal; p.Model != "gpt-5-codex" {
		t.Errorf("proposal step model %q", p.Model)
	}
	if c := sess.Rounds[0].Critiques[0]; c.Model != "grok-4" {
		t.Errorf("critique step model %q", c.Model)
	}
}

// Choices in the request win over the team; a provider the team does not
// seat in that place runs with the CLI's default model.
func TestRequestOverridesTeam(t *testing.T) {
	script := fakes(t, map[string][]string{
		"claude": {brief("Clean up stale run folders")},
		"grok":   {ok},
	})
	s, _ := newService(t)
	s.Team = teamOf(Seat{Provider: "codex", Model: "gpt-5-codex"}, Seat{Provider: "grok", Model: "grok-4"})
	sess := start(t, s, StartRequest{Input: dump, Project: "demo-app", Proposer: "claude", Critics: []string{"grok"}})
	if sess.Proposer != "claude" {
		t.Fatalf("proposer %s", sess.Proposer)
	}
	if a := args(t, script, "claude", 1); strings.Contains(a, "--model") {
		t.Errorf("claude got a model it was not given: %s", a)
	}
	if a := args(t, script, "grok", 1); !strings.Contains(a, "-m grok-4") {
		t.Errorf("grok, still the team's critic, lost its model: %s", a)
	}
}

// Without a project there is no team: the defaults run as before.
func TestNoProjectNoTeam(t *testing.T) {
	fakes(t, map[string][]string{"claude": {brief("x")}, "codex": {ok}, "grok": {ok}})
	s, _ := newService(t)
	s.Team = teamOf(Seat{Provider: "codex", Model: "gpt-5-codex"})
	sess := start(t, s, StartRequest{Input: dump})
	if sess.Proposer != DefaultProposer || sess.Team != "" || len(sess.Models) != 0 {
		t.Errorf("proposer %s team %q models %v", sess.Proposer, sess.Team, sess.Models)
	}
}

// The confidential rule wins over any team: nothing runs.
func TestTeamNeverOverridesConfidential(t *testing.T) {
	script := fakes(t, map[string][]string{"claude": {brief("x")}, "codex": {ok}, "grok": {ok}})
	s, _ := newService(t)
	s.Team = teamOf(Seat{Provider: "codex"}, Seat{Provider: "grok"})
	if _, err := s.Start(context.Background(), StartRequest{Input: dump, Project: "secret-app"}, nil); !errors.Is(err, ErrConfidential) {
		t.Fatalf("err = %v", err)
	}
	for _, p := range []string{"claude", "codex", "grok"} {
		if n := len(calls(t, script, p)); n != 0 {
			t.Errorf("%s was called %d times", p, n)
		}
	}
}
