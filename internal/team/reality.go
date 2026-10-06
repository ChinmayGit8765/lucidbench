package team

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/mcp"
)

// Reality is what this machine has, for Check.
type Reality struct {
	// Accounts are the detected profiles (GET /api/accounts).
	Accounts []accounts.Profile
	// MCP is the MCP matrix (GET /api/mcp).
	MCP mcp.Matrix
	// Confidential is set when the team belongs to a confidential project.
	Confidential bool
	// Project names the project in messages.
	Project string
	// Estimate returns what one run of a role on a provider has cost lately,
	// and how many runs that is from; nil skips the budget check.
	Estimate func(role, provider string) (avgUSD float64, runs int)
}

// modelPatterns says which model names are plausible for each provider: the
// CLIs' own aliases and the families their makers publish. A name outside
// them is a warning, never an error: a new model should not need a release.
var modelPatterns = map[string]*regexp.Regexp{
	"claude": regexp.MustCompile(`(?i)^(haiku|sonnet|opus|default|opusplan|claude-[a-z0-9.-]+)(\[1m\])?$`),
	"codex":  regexp.MustCompile(`(?i)^(gpt-[a-z0-9.-]+|o[0-9][a-z0-9.-]*|codex-[a-z0-9.-]+)$`),
	"grok":   regexp.MustCompile(`(?i)^grok-[a-z0-9.-]+$`),
}

// Estimate is one seat's recent cost against its budget.
type Estimate struct {
	Role      string   `json:"role"`
	Provider  string   `json:"provider"`
	AvgUSD    float64  `json:"avg_usd"`
	Runs      int      `json:"runs"`
	BudgetUSD *float64 `json:"budget_usd,omitempty"`
	Over      bool     `json:"over"`
}

// Check compares a team with this machine. Everything it finds is a warning
// or a note: the team is still saved and used.
func Check(t Team, r Reality) ([]Problem, []Estimate) {
	var out []Problem
	ests := []Estimate{}
	warn := func(code, role, format string, a ...any) {
		out = append(out, Problem{Severity: SevWarning, Code: code, Role: role, Message: fmt.Sprintf(format, a...)})
	}
	if r.Confidential {
		name := r.Project
		if name == "" {
			name = "This project"
		}
		warn("confidential", "", "%s is confidential: Council and Work never send it to a provider, so this team is kept but never used", name)
	}
	for _, s := range t.Seats() {
		role := s.Role
		if !isProvider(role.Provider) {
			continue // Validate reports it
		}
		if msg := signIn(r.Accounts, role.Provider, role.Profile); msg != "" {
			warn("sign-in", s.Label, "%s: %s", s.Label, msg)
		}
		if role.Model != "" {
			if re := modelPatterns[role.Provider]; re != nil && !re.MatchString(role.Model) {
				warn("model", s.Label, "%s: %q does not look like a %s model; the CLI will refuse it if it does not know it", s.Label, role.Model, role.Provider)
			}
		}
		for _, name := range role.MCPAllow {
			if msg := mcpFor(r.MCP, role.Provider, name); msg != "" {
				warn("mcp", s.Label, "%s: %s", s.Label, msg)
			}
		}
		if len(role.MCPAllow) > 0 {
			out = append(out, Problem{Severity: SevInfo, Code: "mcp-harness", Role: s.Label,
				Message: s.Label + ": MCP servers load only with your own harness; the clean harness Council uses loads none"})
		}
		if r.Estimate != nil {
			avg, n := r.Estimate(s.Name, role.Provider)
			e := Estimate{Role: s.Label, Provider: role.Provider, AvgUSD: avg, Runs: n, BudgetUSD: role.BudgetUSD}
			if n > 0 && role.BudgetUSD != nil && avg > *role.BudgetUSD {
				e.Over = true
				warn("budget", s.Label, "%s: recent %s runs cost $%.2f on average (%d %s), over the $%.2f budget", s.Label, role.Provider, avg, n, plural(n, "run"), *role.BudgetUSD)
			}
			ests = append(ests, e)
		}
	}
	return out, ests
}

func plural(n int, w string) string {
	if n == 1 {
		return w
	}
	return w + "s"
}

// signIn says what is wrong with the provider's sign-in, or "".
func signIn(ps []accounts.Profile, provider, profile string) string {
	if profile == "" {
		profile = "default"
	}
	var host []accounts.Profile
	for _, p := range ps {
		if p.Provider == provider && p.Location == accounts.LocHost {
			host = append(host, p)
		}
	}
	if len(host) == 0 {
		return provider + " is not set up on this machine (or is disabled in the config)"
	}
	for _, p := range host {
		if p.Name != profile {
			continue
		}
		switch p.Status {
		case accounts.StatusLoggedIn:
			return ""
		case accounts.StatusExpired:
			return fmt.Sprintf("the %s sign-in of profile %s has expired", provider, profile)
		case accounts.StatusUnknown:
			return ""
		}
		return fmt.Sprintf("%s profile %s is not signed in", provider, profile)
	}
	return fmt.Sprintf("there is no %s profile named %s", provider, profile)
}

// mcpFor says why provider cannot reach the MCP server name, or "".
func mcpFor(m mcp.Matrix, provider, name string) string {
	for _, row := range m.Servers {
		if !strings.EqualFold(row.Name, name) {
			continue
		}
		for _, p := range append(append([]string{}, row.Providers...), row.Inherited...) {
			if p == provider {
				return ""
			}
		}
		return fmt.Sprintf("MCP server %q is configured, but not for %s", name, provider)
	}
	return fmt.Sprintf("no MCP server named %q is configured on this machine", name)
}
