package team

import (
	"path/filepath"
	"testing"
)

func TestAdaptersForCouncilAndWork(t *testing.T) {
	s, _ := fixture(t)
	// Nothing configured: Council and Work keep their own defaults.
	if r, err := s.CouncilRoles("demo"); r != nil || err != nil {
		t.Errorf("builtin council roles %+v %v", r, err)
	}
	if b, err := s.WorkBuilder("")("demo"); b != nil || err != nil {
		t.Errorf("builtin builder %+v %v", b, err)
	}
	write(t, filepath.Join(s.DataDir, "teams", "demo.yaml"), full)
	r, err := s.CouncilRoles("demo")
	if err != nil || r.Source != SourceData || r.Proposer.Provider != "claude" || r.Proposer.Model != "sonnet" || len(r.Critics) != 2 || r.Critics[1].Model != "grok-4" {
		t.Fatalf("council roles %+v %v", r, err)
	}
	b, err := s.WorkBuilder(s.DataDir)("demo")
	if err != nil || b.Provider != "claude" || b.Model != "sonnet" || len(b.AllowedCommands) != 3 || *b.BudgetUSD != 2 || b.Source != SourceData {
		t.Fatalf("builder %+v %v", b, err)
	}
	// A team without a builder leaves Work alone.
	write(t, filepath.Join(s.DataDir, "teams", "demo.yaml"), "version: 1\nroles:\n  proposer: {provider: codex}\n")
	if b, _ := s.WorkBuilder("")("demo"); b != nil {
		t.Errorf("builder from a team without one: %+v", b)
	}
	if _, err := s.CouncilRoles("missing"); err == nil {
		t.Error("unknown project")
	}
}
