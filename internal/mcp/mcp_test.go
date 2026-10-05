package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The fixtures under testdata/home hold fake secrets in env, headers, args,
// commands, URL query strings, URL user info and project paths. Every one
// carries the FAKE-…-SECRET marker.
var secretRE = regexp.MustCompile(`FAKE-[A-Z]+(-[A-Z]+)*-SECRET-[0-9a-f]{4}`)

func fixtureRoots(t *testing.T) Roots {
	t.Helper()
	home, err := filepath.Abs(filepath.Join("testdata", "home"))
	if err != nil {
		t.Fatal(err)
	}
	return Roots{Home: home, Getenv: func(string) string { return "" }}
}

func TestFixturesContainSecrets(t *testing.T) {
	// Guard against a fixture edit that silently removes what we test for.
	n := 0
	_ = filepath.Walk(filepath.Join("testdata", "home"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			n += len(secretRE.FindAll(b, -1))
		}
		return nil
	})
	if n < 15 {
		t.Fatalf("fixtures hold %d secret markers, want at least 15", n)
	}
}

func TestNoSecretsLeave(t *testing.T) {
	m := Read(fixtureRoots(t))
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{"json": string(b), "table": Table(m)}

	rec := httptest.NewRecorder()
	HandlerFor(func() Roots { return fixtureRoots(t) }).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mcp", nil))
	outputs["http"] = rec.Body.String()

	for name, out := range outputs {
		if s := secretRE.FindString(out); s != "" {
			t.Errorf("%s output leaks %q", name, s)
		}
		for _, bad := range []string{"SECRET", "Bearer", "?token", "?key", "/opt/", "pg-mcp", "--root", "fakeuser", "D:", "/srv/"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s output contains %q", name, bad)
			}
		}
	}
}

func find(c Client, name string) *Server {
	for i := range c.Servers {
		if c.Servers[i].Name == name {
			return &c.Servers[i]
		}
	}
	return nil
}

func TestClients(t *testing.T) {
	m := Read(fixtureRoots(t))
	if len(m.Clients) != 4 {
		t.Fatalf("got %d clients, want 4", len(m.Clients))
	}
	byP := map[string]Client{}
	for _, c := range m.Clients {
		byP[c.Provider] = c
		if !c.ConfigFound || c.Error != "" {
			t.Errorf("%s: found=%v error=%q", c.Provider, c.ConfigFound, c.Error)
		}
	}
	claude := byP["claude"]
	want := []Server{
		{Name: "filesystem", Transport: Stdio, Scope: "user"},
		{Name: "linear", Transport: SSE, Host: "mcp.linear.example", Scope: "user"},
		{Name: "notion", Transport: HTTP, Host: "mcp.notion.example", Scope: "user"},
		{Name: "sentry", Transport: HTTP, Host: "mcp.sentry.example", Scope: "user"},
		{Name: "postgres", Transport: Stdio, Scope: "project:shop"},
	}
	if !slices.Equal(claude.Servers, want) {
		t.Errorf("claude servers =\n%+v\nwant\n%+v", claude.Servers, want)
	}
	if claude.ConfigHint != "~/.claude.json" {
		t.Errorf("claude hint = %q", claude.ConfigHint)
	}
	if s := find(byP["codex"], "linear"); s == nil || s.Transport != HTTP || s.Host != "mcp.linear.example" {
		t.Errorf("codex linear = %+v", s)
	}
	if s := find(byP["codex"], "filesystem"); s == nil || s.Transport != Stdio || s.Host != "" {
		t.Errorf("codex filesystem = %+v", s)
	}
	if g := byP["grok"]; !slices.Equal(g.Inherits, []string{"claude"}) || len(g.Servers) != 1 {
		t.Errorf("grok = %+v", g)
	}
	if s := find(byP["cursor"], "Notion"); s == nil || s.Transport != HTTP {
		t.Errorf("cursor Notion = %+v", s)
	}
}

func TestMatrix(t *testing.T) {
	m := Read(fixtureRoots(t))
	rows := map[string]Row{}
	for _, r := range m.Servers {
		rows[strings.ToLower(r.Name)] = r
	}
	if r := rows["notion"]; !slices.Equal(r.Providers, []string{"claude", "cursor"}) || !slices.Equal(r.Inherited, []string{"grok"}) {
		t.Errorf("notion row = %+v", r)
	}
	if r := rows["linear"]; !slices.Equal(r.Providers, []string{"claude", "codex"}) || !slices.Equal(r.Transports, []string{SSE, HTTP}) {
		t.Errorf("linear row = %+v", r)
	}
	// Project-scoped servers are not inherited.
	if r := rows["postgres"]; len(r.Inherited) != 0 {
		t.Errorf("postgres row = %+v", r)
	}
	if !strings.Contains(Table(m), "~") {
		t.Error("table does not mark inherited cells")
	}
}

func TestDisabledAndMissing(t *testing.T) {
	r := fixtureRoots(t)
	r.Disabled = map[string]bool{"cursor": true, "grok": true}
	m := Read(r)
	for _, c := range m.Clients {
		if c.Provider == "cursor" || c.Provider == "grok" {
			t.Errorf("disabled provider %s listed", c.Provider)
		}
	}
	empty := Read(Roots{Home: t.TempDir()})
	if len(empty.Clients) != 4 || len(empty.Servers) != 0 {
		t.Fatalf("empty home: %+v", empty)
	}
	for _, c := range empty.Clients {
		if c.ConfigFound || c.Servers == nil {
			t.Errorf("%s: %+v", c.Provider, c)
		}
	}
}

func TestParseErrorDoesNotQuoteContent(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"mcpServers": {"x": {"env": {"K": "FAKE-ENV-SECRET-0000"}}` // truncated JSON
	if err := os.WriteFile(filepath.Join(home, ".cursor", "mcp.json"), []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	m := Read(Roots{Home: home, Disabled: map[string]bool{"claude": true, "codex": true, "grok": true}})
	if len(m.Clients) != 1 || m.Clients[0].Error != "could not parse mcp.json" {
		t.Fatalf("got %+v", m.Clients)
	}
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "SECRET") {
		t.Error("parse error leaks content")
	}
}

func TestFolder(t *testing.T) {
	for in, want := range map[string]string{
		`D:\work\shop`: "shop", "/srv/blog/": "blog", "C:/a/b": "b", "/": "(root)", "solo": "solo",
	} {
		if got := folder(in); got != want {
			t.Errorf("folder(%q) = %q, want %q", in, got, want)
		}
	}
}
