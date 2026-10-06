// Package team reads and checks an AI-team spec, lucid-team.yaml version 1:
// which provider (and model, profile, MCP servers, commands and budget) fills
// each role of the loop, and which gates stay with a human.
//
// A project's team lives in its checkout at .lucid/team.yaml when the project
// has a local_path, else in <DataDir>/teams/<project>.yaml. The `team:` key of
// config.yaml is the default for a project with neither. Council takes its
// proposer and critics from the team, and Work its builder.
//
// Checking has two layers. Validate is the schema: a version other than 1, an
// unknown provider, a gate set to anything but human or a malformed value is
// an error, and a team with an error is never saved or used. Check compares a
// valid team with this machine (signed-in accounts, the MCP matrix, model
// names, recent costs) and only warns.
package team

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// Version is the only spec version this package reads.
const Version = 1

// Role names, in display order.
const (
	RoleProposer = "proposer"
	RoleCritic   = "critic"
	RoleBuilder  = "builder"
	RoleReviewer = "reviewer"
	RoleScout    = "scout"
)

// Roles lists every role in display order.
var Roles = []string{RoleProposer, RoleCritic, RoleBuilder, RoleReviewer, RoleScout}

// Providers are the CLIs a role may name.
var Providers = []string{"claude", "codex", "grok"}

// Gates are the decisions that stay with a human; HumanGate is the only value
// version 1 accepts for each.
var Gates = []string{"approve_brief", "open_pr", "merge"}

// HumanGate is the value of every gate in version 1.
const HumanGate = "human"

// Limits on one spec.
const (
	MaxBytes     = 32 << 10
	MaxCritics   = 2
	MaxMCP       = 20
	MaxCommands  = 40
	MaxBudgetUSD = 10000
)

// Severity of a Problem.
const (
	SevError   = "error"
	SevWarning = "warning"
	SevInfo    = "info"
)

// Role is one seat on the team.
type Role struct {
	Provider string `json:"provider" yaml:"provider"`
	Profile  string `json:"profile,omitempty" yaml:"profile,omitempty"`
	Model    string `json:"model,omitempty" yaml:"model,omitempty"`
	// MCPAllow names the MCP servers the role may use. Version 1 records and
	// checks them against the MCP matrix; the clean harness loads no MCP
	// server at all, so they take effect only with harness "mine".
	MCPAllow []string `json:"mcp_allow,omitempty" yaml:"mcp_allow,omitempty"`
	// AllowedCommands replaces a builder's default allowed commands.
	AllowedCommands []string `json:"allowed_commands,omitempty" yaml:"allowed_commands,omitempty"`
	// BudgetUSD is what one run of the role should cost at most. Version 1
	// warns when recent runs cost more; it never stops a run.
	BudgetUSD *float64 `json:"budget_usd,omitempty" yaml:"budget_usd,omitempty"`
}

// Seats is the critic role: one role, or a list of up to MaxCritics. It reads
// either shape and always writes a list.
type Seats []Role

// UnmarshalJSON reads one role object or a list of them.
func (s *Seats) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '{' {
		var r Role
		if err := strictJSON(b, &r); err != nil {
			return err
		}
		*s = Seats{r}
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return errors.New("critic must be a role or a list of roles")
	}
	out := Seats{}
	for _, it := range raw {
		var r Role
		if err := strictJSON(it, &r); err != nil {
			return err
		}
		out = append(out, r)
	}
	*s = out
	return nil
}

// RoleSet holds the five roles; a missing role means Lucidbench's default.
type RoleSet struct {
	Proposer *Role `json:"proposer,omitempty" yaml:"proposer,omitempty"`
	Critic   Seats `json:"critic,omitempty" yaml:"critic,omitempty"`
	Builder  *Role `json:"builder,omitempty" yaml:"builder,omitempty"`
	Reviewer *Role `json:"reviewer,omitempty" yaml:"reviewer,omitempty"`
	Scout    *Role `json:"scout,omitempty" yaml:"scout,omitempty"`
}

// GateSet says who decides at each gate. Empty means human.
type GateSet struct {
	ApproveBrief string `json:"approve_brief,omitempty" yaml:"approve_brief,omitempty"`
	OpenPR       string `json:"open_pr,omitempty" yaml:"open_pr,omitempty"`
	Merge        string `json:"merge,omitempty" yaml:"merge,omitempty"`
}

// Team is one lucid-team.yaml.
type Team struct {
	Version int     `json:"version" yaml:"version"`
	Roles   RoleSet `json:"roles" yaml:"roles"`
	Gates   GateSet `json:"gates" yaml:"gates"`
}

// Problem is one finding about a team. Role is "critic 2" for the second
// critic, "" for the team as a whole.
type Problem struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Role     string `json:"role,omitempty"`
	Message  string `json:"message"`
}

// Seat is one filled role with its label ("critic 2").
type Seat struct {
	Name  string
	Label string
	Role  Role
}

// Seats lists the filled roles in display order.
func (t *Team) Seats() []Seat {
	var out []Seat
	add := func(name string, r *Role) {
		if r != nil {
			out = append(out, Seat{Name: name, Label: name, Role: *r})
		}
	}
	add(RoleProposer, t.Roles.Proposer)
	for i, r := range t.Roles.Critic {
		label := RoleCritic
		if len(t.Roles.Critic) > 1 {
			label = fmt.Sprintf("critic %d", i+1)
		}
		out = append(out, Seat{Name: RoleCritic, Label: label, Role: r})
	}
	add(RoleBuilder, t.Roles.Builder)
	add(RoleReviewer, t.Roles.Reviewer)
	add(RoleScout, t.Roles.Scout)
	return out
}

// Default is the team Lucidbench uses when nothing is configured: the
// council's and Work's own defaults, written as a spec.
func Default() Team {
	return Team{
		Version: Version,
		Roles: RoleSet{
			Proposer: &Role{Provider: "claude"},
			Critic:   Seats{{Provider: "codex"}, {Provider: "grok"}},
			Builder:  &Role{Provider: "claude"},
		},
		Gates: GateSet{ApproveBrief: HumanGate, OpenPR: HumanGate, Merge: HumanGate},
	}
}

func strictJSON(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return cleanJSONErr(err)
	}
	return nil
}

// cleanJSONErr names the YAML-side problem rather than Go types.
func cleanJSONErr(err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		field := te.Field
		if field == "" {
			field = "a value"
		}
		return fmt.Errorf("%s must be a %s", field, jsonKind(te.Type.Kind().String()))
	}
	return errors.New(strings.TrimPrefix(err.Error(), "json: "))
}

func jsonKind(k string) string {
	switch k {
	case "string":
		return "string"
	case "int", "int64", "float64":
		return "number"
	case "slice":
		return "list"
	case "ptr", "struct", "map":
		return "mapping"
	}
	return k
}

// Parse reads a lucid-team.yaml (YAML, or JSON, which is YAML too). Unknown
// keys are errors, so a typo never silently means "use the default".
func Parse(data []byte) (Team, error) {
	var t Team
	if len(data) > MaxBytes {
		return t, fmt.Errorf("the team file is larger than %d KB", MaxBytes>>10)
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return t, fmt.Errorf("invalid YAML: %v", err)
	}
	if doc == nil {
		return t, errors.New("the team file is empty")
	}
	if _, ok := doc.(map[string]any); !ok {
		return t, errors.New("the team file must be a mapping with version, roles and gates")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return t, fmt.Errorf("the team file holds a value JSON cannot carry: %v", err)
	}
	return t, strictJSON(b, &t)
}

// ParseJSON reads a team sent by the UI.
func ParseJSON(data []byte) (Team, error) {
	var t Team
	return t, strictJSON(data, &t)
}

// Marshal writes t as lucid-team.yaml, with a header saying what it is.
func Marshal(t Team) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# lucid-team.yaml version 1: who fills each role of the Lucidbench loop.\n")
	buf.WriteString("# Schema: docs/schemas/lucid-team.schema.json in the Lucidbench repository.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(t); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	profileRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	modelRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,79}$`)
	mcpRE     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._ -]{0,63}$`)
)

func isProvider(p string) bool {
	for _, x := range Providers {
		if x == p {
			return true
		}
	}
	return false
}

// Validate checks t against the schema. Every problem it returns is an error.
func Validate(t Team) []Problem {
	var out []Problem
	bad := func(code, role, format string, a ...any) {
		out = append(out, Problem{Severity: SevError, Code: code, Role: role, Message: fmt.Sprintf(format, a...)})
	}
	if t.Version != Version {
		bad("version", "", "version must be 1 (this Lucidbench reads lucid-team.yaml version 1 only)")
	}
	if len(t.Roles.Critic) > MaxCritics {
		bad("critics", RoleCritic, "at most %d critics; the council's proposer is the third voice", MaxCritics)
	}
	for _, s := range t.Seats() {
		r := s.Role
		switch {
		case r.Provider == "":
			bad("provider", s.Label, "%s has no provider; use claude, codex or grok", s.Label)
		case !isProvider(r.Provider):
			bad("unknown-provider", s.Label, "%s: unknown provider %q; use claude, codex or grok", s.Label, r.Provider)
		}
		if r.Profile != "" && !profileRE.MatchString(r.Profile) {
			bad("profile", s.Label, "%s: profile %q is not a profile name (letters, digits, . _ -)", s.Label, r.Profile)
		}
		if r.Profile != "" && r.Profile != "default" && r.Provider == "grok" {
			bad("profile", s.Label, "%s: grok has no selectable profiles", s.Label)
		}
		if r.Model != "" && !modelRE.MatchString(r.Model) {
			bad("model", s.Label, "%s: model %q is not a model name", s.Label, r.Model)
		}
		if len(r.MCPAllow) > MaxMCP {
			bad("mcp", s.Label, "%s: at most %d MCP servers", s.Label, MaxMCP)
		}
		for _, m := range r.MCPAllow {
			if !mcpRE.MatchString(m) {
				bad("mcp", s.Label, "%s: %q is not an MCP server name", s.Label, m)
			}
		}
		if len(r.AllowedCommands) > MaxCommands {
			bad("commands", s.Label, "%s: at most %d allowed commands", s.Label, MaxCommands)
		}
		if _, refused := work.CleanAllowed(r.AllowedCommands); len(refused) > 0 {
			bad("commands", s.Label, "%s: these commands can never be allowed: %s", s.Label, strings.Join(refused, ", "))
		}
		if r.BudgetUSD != nil && (*r.BudgetUSD < 0 || *r.BudgetUSD > MaxBudgetUSD) {
			bad("budget", s.Label, "%s: budget_usd must be between 0 and %d", s.Label, MaxBudgetUSD)
		}
	}
	for _, g := range []struct{ name, v string }{
		{"approve_brief", t.Gates.ApproveBrief}, {"open_pr", t.Gates.OpenPR}, {"merge", t.Gates.Merge},
	} {
		if g.v != "" && g.v != HumanGate {
			bad("gate", "", "gates.%s must be human: version 1 never lets an agent %s on its own", g.name, gateVerb(g.name))
		}
	}
	return out
}

func gateVerb(g string) string {
	switch g {
	case "approve_brief":
		return "approve a brief"
	case "open_pr":
		return "open a PR"
	}
	return "merge"
}

// Normalise fills the gates with human and trims the lists, so a saved file
// says what applies.
func Normalise(t Team) Team {
	if t.Gates.ApproveBrief == "" {
		t.Gates.ApproveBrief = HumanGate
	}
	if t.Gates.OpenPR == "" {
		t.Gates.OpenPR = HumanGate
	}
	if t.Gates.Merge == "" {
		t.Gates.Merge = HumanGate
	}
	trim := func(r *Role) {
		if r == nil {
			return
		}
		r.Provider, r.Profile, r.Model = strings.TrimSpace(r.Provider), strings.TrimSpace(r.Profile), strings.TrimSpace(r.Model)
		if r.Profile == "default" {
			r.Profile = ""
		}
		r.MCPAllow = cleanList(r.MCPAllow)
		if r.AllowedCommands != nil {
			r.AllowedCommands, _ = work.CleanAllowed(r.AllowedCommands)
		}
	}
	for _, p := range []*Role{t.Roles.Proposer, t.Roles.Builder, t.Roles.Reviewer, t.Roles.Scout} {
		trim(p)
	}
	for i := range t.Roles.Critic {
		trim(&t.Roles.Critic[i])
	}
	return t
}

func cleanList(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// HasErrors reports whether ps holds an error.
func HasErrors(ps []Problem) bool {
	for _, p := range ps {
		if p.Severity == SevError {
			return true
		}
	}
	return false
}
