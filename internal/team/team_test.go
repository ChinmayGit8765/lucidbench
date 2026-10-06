package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/mcp"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

const full = `version: 1
roles:
  proposer: {provider: claude, model: sonnet, budget_usd: 0.5}
  critic:
    - {provider: codex, model: gpt-5-codex}
    - {provider: grok, model: grok-4}
  builder:
    provider: claude
    model: sonnet
    allowed_commands: [go, gofmt, git status]
    budget_usd: 2
  scout: {provider: claude, model: haiku, mcp_allow: [github]}
gates:
  approve_brief: human
  open_pr: human
  merge: human
`

func codes(ps []Problem) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Severity+":"+p.Code)
	}
	sort.Strings(out)
	return out
}

func TestParseReadsBothCriticShapes(t *testing.T) {
	tm, err := Parse([]byte(full))
	if err != nil {
		t.Fatal(err)
	}
	if ps := Validate(tm); len(ps) != 0 {
		t.Fatalf("problems: %+v", ps)
	}
	if len(tm.Roles.Critic) != 2 || tm.Roles.Critic[1].Provider != "grok" || tm.Roles.Builder.AllowedCommands[2] != "git status" || *tm.Roles.Builder.BudgetUSD != 2 {
		t.Fatalf("parsed %+v", tm.Roles)
	}
	one, err := Parse([]byte("version: 1\nroles:\n  critic: {provider: codex}\n"))
	if err != nil || len(one.Roles.Critic) != 1 || one.Roles.Critic[0].Provider != "codex" {
		t.Fatalf("one critic: %+v %v", one.Roles.Critic, err)
	}
	// JSON is YAML too.
	j, err := Parse([]byte(`{"version": 1, "roles": {"builder": {"provider": "codex"}}}`))
	if err != nil || j.Roles.Builder.Provider != "codex" {
		t.Fatalf("json: %+v %v", j, err)
	}
	// Written back, it reads the same.
	out, err := Marshal(Normalise(tm))
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(out)
	if err != nil || len(Validate(again)) != 0 || again.Roles.Scout.MCPAllow[0] != "github" || again.Gates.Merge != HumanGate {
		t.Fatalf("round trip: %v\n%s", err, out)
	}
}

func TestSchemaErrors(t *testing.T) {
	for _, c := range []struct {
		name, yaml, want string
	}{
		{"unknown key", "version: 1\nroles:\n  builder: {provider: claude, modle: sonnet}\n", "parse"},
		{"unknown role", "version: 1\nroles:\n  tester: {provider: claude}\n", "parse"},
		{"unknown top key", "version: 1\nteam: x\n", "parse"},
		{"string version", "version: \"one\"\n", "parse"},
		{"not a mapping", "- a\n- b\n", "parse"},
		{"empty", "", "parse"},
		{"version 2", "version: 2\n", "version"},
		{"unknown provider", "version: 1\nroles:\n  proposer: {provider: gemini}\n", "unknown-provider"},
		{"no provider", "version: 1\nroles:\n  proposer: {model: sonnet}\n", "provider"},
		{"auto merge", "version: 1\ngates: {merge: auto}\n", "gate"},
		{"auto brief", "version: 1\ngates: {approve_brief: agent}\n", "gate"},
		{"three critics", "version: 1\nroles:\n  critic: [{provider: codex}, {provider: grok}, {provider: claude}]\n", "critics"},
		{"negative budget", "version: 1\nroles:\n  builder: {provider: claude, budget_usd: -1}\n", "budget"},
		{"push allowed", "version: 1\nroles:\n  builder: {provider: claude, allowed_commands: [git push]}\n", "commands"},
		{"shell allowed", "version: 1\nroles:\n  builder: {provider: claude, allowed_commands: [bash]}\n", "commands"},
		{"bad model", "version: 1\nroles:\n  builder: {provider: claude, model: \"so net;\"}\n", "model"},
		{"grok profile", "version: 1\nroles:\n  scout: {provider: grok, profile: work}\n", "profile"},
		{"bad mcp", "version: 1\nroles:\n  scout: {provider: claude, mcp_allow: [\"https://x\"]}\n", "mcp"},
	} {
		tm, err := Parse([]byte(c.yaml))
		if c.want == "parse" {
			if err == nil {
				t.Errorf("%s: parsed %+v", c.name, tm)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		ps := Validate(tm)
		if !HasErrors(ps) || !slices.Contains(codes(ps), "error:"+c.want) {
			t.Errorf("%s: problems %v, want %s", c.name, codes(ps), c.want)
		}
	}
}

// The JSON Schema in docs/ is what external tools read; it must say what
// Validate enforces.
func TestSchemaFileMatchesCode(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "schemas", "lucid-team.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties struct {
			Version struct {
				Const int `json:"const"`
			} `json:"version"`
			Roles struct {
				Properties map[string]struct {
					OneOf []struct {
						MaxItems int `json:"maxItems"`
					} `json:"oneOf"`
				} `json:"properties"`
			} `json:"roles"`
			Gates struct {
				Properties map[string]struct {
					Const string `json:"const"`
				} `json:"properties"`
			} `json:"gates"`
		} `json:"properties"`
		Defs struct {
			Role struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"role"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Properties.Version.Const != Version {
		t.Errorf("schema version %d", s.Properties.Version.Const)
	}
	var roles []string
	for r := range s.Properties.Roles.Properties {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	want := slices.Clone(Roles)
	sort.Strings(want)
	if !slices.Equal(roles, want) {
		t.Errorf("schema roles %v, code %v", roles, want)
	}
	if c := s.Properties.Roles.Properties["critic"].OneOf; len(c) != 2 || c[1].MaxItems != MaxCritics {
		t.Errorf("critic oneOf %+v", c)
	}
	for _, g := range Gates {
		if s.Properties.Gates.Properties[g].Const != HumanGate {
			t.Errorf("gate %s not const human in the schema", g)
		}
	}
	if len(s.Properties.Gates.Properties) != len(Gates) {
		t.Errorf("schema gates %v", s.Properties.Gates.Properties)
	}
	var enum struct {
		Enum []string `json:"enum"`
	}
	_ = json.Unmarshal(s.Defs.Role.Properties["provider"], &enum)
	if !slices.Equal(enum.Enum, Providers) {
		t.Errorf("schema providers %v, code %v", enum.Enum, Providers)
	}
	for _, k := range []string{"provider", "profile", "model", "mcp_allow", "allowed_commands", "budget_usd"} {
		if _, ok := s.Defs.Role.Properties[k]; !ok {
			t.Errorf("schema role lacks %s", k)
		}
	}
}

// fixture is a project list with a checkout, one without, and a
// confidential one.
func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "code", "demo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	list := &projects.List{Configured: true, Projects: []projects.Project{
		{ID: "demo", Name: "Demo", Visibility: "private", LocalPath: repo},
		{ID: "nocode", Name: "No code", Visibility: "public", LocalPath: filepath.Join(root, "missing")},
		{ID: "vault", Name: "Vault", Visibility: "confidential"},
	}}
	return &Store{DataDir: filepath.Join(root, "data"), Projects: func() (*projects.List, error) { return list, nil }}, repo
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOrder(t *testing.T) {
	s, repo := fixture(t)
	r, err := s.Resolve("demo")
	if err != nil || r.Source != SourceBuiltin || r.Team.Roles.Proposer.Provider != "claude" || len(r.Team.Roles.Critic) != 2 {
		t.Fatalf("builtin: %+v %v", r, err)
	}
	if r.Targets[SourceRepo] == "" || r.Targets[SourceData] == "" {
		t.Errorf("targets %v", r.Targets)
	}

	s.Default = "version: 1\nroles:\n  proposer: {provider: grok}\n"
	if r, _ := s.Resolve("demo"); r.Source != SourceConfig || r.Team.Roles.Proposer.Provider != "grok" {
		t.Errorf("config: %+v", r)
	}

	write(t, filepath.Join(s.DataDir, "teams", "demo.yaml"), "version: 1\nroles:\n  proposer: {provider: codex}\n")
	if r, _ := s.Resolve("demo"); r.Source != SourceData || r.Team.Roles.Proposer.Provider != "codex" {
		t.Errorf("data: %+v", r)
	}

	repoFile := filepath.Join(repo, ".lucid", "team.yaml")
	write(t, repoFile, "version: 1\nroles:\n  proposer: {provider: claude, model: opus}\n")
	if r, _ := s.Resolve("demo"); r.Source != SourceRepo || r.Team.Roles.Proposer.Model != "opus" || !strings.HasSuffix(filepath.ToSlash(r.PathHint), ".lucid/team.yaml") {
		t.Errorf("repo: %+v", r)
	}

	// A broken repo file is skipped, said so, and the data dir's team is used.
	write(t, repoFile, "version: 1\ngates: {merge: auto}\n")
	r, _ = s.Resolve("demo")
	if r.Source != SourceData || len(r.Problems) != 1 || !strings.Contains(r.Problems[0].Message, "was skipped") {
		t.Errorf("broken repo file: %+v", r)
	}

	// A project whose local_path does not exist has only the data target.
	r, _ = s.Resolve("nocode")
	if _, ok := r.Targets[SourceRepo]; ok || r.Source != SourceConfig {
		t.Errorf("nocode: %+v", r)
	}
	if _, err := s.Resolve("nope"); err == nil {
		t.Error("unknown project resolved")
	}
	if _, err := s.Resolve("../x"); err == nil {
		t.Error("bad id resolved")
	}
}

func TestSave(t *testing.T) {
	s, repo := fixture(t)
	tm, _ := Parse([]byte(full))
	r, err := s.Save("demo", tm, SourceRepo)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != SourceRepo {
		t.Errorf("source %s", r.Source)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".lucid", "team.yaml"))
	if err != nil || !strings.Contains(string(data), "lucid-team.yaml version 1") || !strings.Contains(string(data), "merge: human") {
		t.Fatalf("repo file %s %v", data, err)
	}
	if _, err := s.Save("nocode", tm, SourceRepo); err == nil {
		t.Error("saved to a repo that does not exist")
	}
	if r, err := s.Save("nocode", tm, SourceData); err != nil || r.Source != SourceData {
		t.Errorf("data save: %+v %v", r, err)
	}
	bad := tm
	bad.Gates.Merge = "auto"
	if _, err := s.Save("demo", bad, SourceData); err == nil {
		t.Error("saved an auto merge gate")
	}
	if _, err := s.Save("demo", tm, "elsewhere"); err == nil {
		t.Error("saved to an unknown target")
	}
}

func TestCheckAgainstThisMachine(t *testing.T) {
	tm, _ := Parse([]byte(full))
	tm.Roles.Reviewer = &Role{Provider: "codex", Profile: "work", Model: "sonnet"}
	r := Reality{
		Accounts: []accounts.Profile{
			{Provider: "claude", Name: "default", Location: accounts.LocHost, Status: accounts.StatusLoggedIn},
			{Provider: "codex", Name: "default", Location: accounts.LocHost, Status: accounts.StatusExpired},
		},
		MCP: mcp.Matrix{Servers: []mcp.Row{{Name: "github", Providers: []string{"codex"}}}},
		Estimate: func(role, provider string) (float64, int) {
			if role == RoleBuilder {
				return 3.5, 4
			}
			return 0.1, 2
		},
	}
	ps, ests := Check(tm, r)
	got := strings.Join(codes(ps), " ")
	for _, want := range []string{"warning:sign-in", "warning:model", "warning:mcp", "warning:budget", "info:mcp-harness"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if HasErrors(ps) {
		t.Errorf("Check returned an error: %v", got)
	}
	msgs := ""
	for _, p := range ps {
		msgs += p.Message + "\n"
	}
	for _, want := range []string{"sign-in of profile default has expired", "grok is not set up", "no codex profile named work", `"sonnet" does not look like a codex model`, `"github" is configured, but not for claude`, "over the $2.00 budget"} {
		if !strings.Contains(msgs, want) {
			t.Errorf("no %q in:\n%s", want, msgs)
		}
	}
	over := 0
	for _, e := range ests {
		if e.Over {
			over++
		}
	}
	if over != 1 || len(ests) != 6 {
		t.Errorf("estimates %+v", ests)
	}

	// Confidential: the team is kept, and the user is told it is never used.
	ps, _ = Check(Default(), Reality{Confidential: true, Project: "Vault", Accounts: r.Accounts})
	if !slices.Contains(codes(ps), "warning:confidential") {
		t.Errorf("confidential not reported: %v", codes(ps))
	}
}

func TestEstimatorReadsRecords(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "council", "20260101-000000-aaaaaa.json"), `{"created":"2026-01-01T00:00:00Z","rounds":[
	 {"proposal":{"provider":"claude","role":"propose","usage":{"cost_usd":0.2}},
	  "critiques":[{"provider":"codex","role":"critique","usage":{"cost_usd":0.05}},{"provider":"grok","role":"critique"}],
	  "synthesis":{"provider":"claude","role":"synthesise","usage":{"cost_usd":0.1}}}]}`)
	write(t, filepath.Join(dir, "work", "sessions", "abcd1234", "session.json"), `{"provider":"claude","started":"2026-01-02T00:00:00Z","usage":{"cost_usd":1.5}}`)
	write(t, filepath.Join(dir, "work", "sessions", "abcd1235", "session.json"), `{"provider":"claude","started":"2026-01-03T00:00:00Z","usage":{"cost_usd":0.5}}`)
	est := Estimator(dir)
	if avg, n := est(RoleProposer, "claude"); n != 1 || avg < 0.299 || avg > 0.301 {
		t.Errorf("proposer %v %d", avg, n)
	}
	if avg, n := est(RoleCritic, "codex"); n != 1 || avg != 0.05 {
		t.Errorf("critic %v %d", avg, n)
	}
	if _, n := est(RoleCritic, "grok"); n != 0 {
		t.Errorf("a critique with no cost counted")
	}
	if avg, n := est(RoleBuilder, "claude"); n != 2 || avg != 1 {
		t.Errorf("builder %v %d", avg, n)
	}
}

func TestRoutes(t *testing.T) {
	s, repo := fixture(t)
	mux := http.NewServeMux()
	Register(mux, &API{Store: s,
		Accounts: func() []accounts.Profile {
			return []accounts.Profile{{Provider: "claude", Name: "default", Location: accounts.LocHost, Status: accounts.StatusLoggedIn}}
		},
		MCP: func() mcp.Matrix { return mcp.Matrix{} },
	})
	do := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	w := do("GET", "/api/projects/demo/team", "", false)
	var v View
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Source != SourceBuiltin {
		t.Fatalf("get %d %s", w.Code, w.Body)
	}
	body := `{"team": {"version": 1, "roles": {"builder": {"provider": "codex", "model": "gpt-5-codex"}}}, "target": "repo"}`
	if w := do("PUT", "/api/projects/demo/team", body, false); w.Code != http.StatusForbidden {
		t.Errorf("save without confirm: %d", w.Code)
	}
	w = do("PUT", "/api/projects/demo/team", body, true)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Source != SourceRepo || v.Team.Gates.OpenPR != HumanGate {
		t.Fatalf("save %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(repo, ".lucid", "team.yaml")); err != nil {
		t.Error(err)
	}
	if w := do("PUT", "/api/projects/demo/team", `{"team": {"version": 1, "gates": {"merge": "auto"}}, "target": "data"}`, true); w.Code != 400 {
		t.Errorf("auto gate saved: %d %s", w.Code, w.Body)
	}
	if w := do("PUT", "/api/projects/demo/team", `{"team": {"version": 1, "roles": {"builder": {"provider": "gemini"}}}, "target": "data"}`, true); w.Code != 400 {
		t.Errorf("unknown provider saved: %d", w.Code)
	}

	var vr ValidateResponse
	w = do("POST", "/api/team/validate", `{"yaml": "version: 1\nroles:\n  builder: {provider: grok}\n", "project": "vault"}`, false)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &vr) != nil || !vr.Valid || vr.Team == nil {
		t.Fatalf("validate %d %s", w.Code, w.Body)
	}
	if c := strings.Join(codes(vr.Problems), " "); !strings.Contains(c, "warning:confidential") || !strings.Contains(c, "warning:sign-in") {
		t.Errorf("validate problems %s", c)
	}
	w = do("POST", "/api/team/validate", `{"yaml": "version: 1\nroles: [1]\n"}`, false)
	if json.Unmarshal(w.Body.Bytes(), &vr) != nil || vr.Valid || vr.Team != nil {
		t.Errorf("bad yaml validated: %s", w.Body)
	}
	if w := do("GET", "/api/team/default", "", false); w.Code != 200 || !strings.Contains(w.Body.String(), `"builtin"`) {
		t.Errorf("default %d %s", w.Code, w.Body)
	}
}
