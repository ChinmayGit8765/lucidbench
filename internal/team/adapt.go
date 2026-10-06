package team

import (
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// own resolves a project's team and returns it, with its source, only when
// the project (or the config) has one: the built-in default changes nothing,
// so Council and Work keep their own defaults and the request's choices.
func (s *Store) own(project string) (*Team, string, error) {
	r, err := s.Resolve(project)
	if err != nil || r.Source == SourceBuiltin {
		return nil, "", err
	}
	return &r.Team, r.Source, nil
}

// CouncilRoles is council.Service.Team: the team's proposer and critics.
func (s *Store) CouncilRoles(project string) (*council.TeamRoles, error) {
	t, source, err := s.own(project)
	if t == nil || err != nil {
		return nil, err
	}
	out := &council.TeamRoles{Source: source}
	if p := t.Roles.Proposer; p != nil {
		out.Proposer = &council.Seat{Provider: p.Provider, Model: p.Model, Profile: p.Profile}
	}
	for _, c := range t.Roles.Critic {
		out.Critics = append(out.Critics, council.Seat{Provider: c.Provider, Model: c.Model, Profile: c.Profile})
	}
	if out.Proposer == nil && len(out.Critics) == 0 {
		return nil, nil
	}
	return out, nil
}

// WorkBuilder returns work.Service.Team: the team's builder, with its budget
// and what recent builder runs on its provider cost (read from dataDir).
func (s *Store) WorkBuilder(dataDir string) func(project string) (*work.Builder, error) {
	return func(project string) (*work.Builder, error) {
		t, source, err := s.own(project)
		if t == nil || err != nil || t.Roles.Builder == nil {
			return nil, err
		}
		b := t.Roles.Builder
		out := &work.Builder{Provider: b.Provider, Model: b.Model, Profile: b.Profile, AllowedCommands: b.AllowedCommands,
			Source: source, BudgetUSD: b.BudgetUSD}
		if dataDir != "" {
			out.AvgUSD, out.Runs = Estimator(dataDir)(RoleBuilder, b.Provider)
		}
		return out, nil
	}
}
