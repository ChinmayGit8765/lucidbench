package config

import (
	"strings"
	"testing"
)

func TestTeamKeptAsYAML(t *testing.T) {
	p := write(t, `team:
  version: 1
  roles:
    proposer: {provider: claude, model: sonnet}
    critic: [{provider: codex}]
`)
	c, warns, err := LoadFrom(p, env(nil))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if !strings.Contains(c.Team, "version: 1") || !strings.Contains(c.Team, "provider: codex") || c.Source("team") != "file" || c.value("team") != "(set)" {
		t.Errorf("team %q from %s", c.Team, c.Source("team"))
	}
	if Default().Team != "" || Default().value("team") != "" {
		t.Error("a default team is set")
	}
	if _, _, err := LoadFrom(write(t, "team: claude\n"), env(nil)); err == nil || !strings.Contains(err.Error(), "team: must be a mapping") {
		t.Errorf("scalar team: %v", err)
	}
}
