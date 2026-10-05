// Package mcp reports which MCP servers each AI client (Claude Code, Codex,
// Grok, Cursor) is configured to reach, as a matrix.
//
// Only three facts leave a definition: the server name, its transport and,
// for remote servers, the URL host. Every other field (env, headers, args,
// commands, tokens, full URLs and their query strings) is dropped by the
// decoder and never held, logged or returned. Claude Code project scopes are
// reported by folder name only, never by full path.
package mcp

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Transports.
const (
	Stdio = "stdio"
	HTTP  = "http"
	SSE   = "sse"
)

// Server is one configured MCP server, reduced to non-secret facts.
type Server struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Host      string `json:"host,omitempty"`
	// Scope is "user", or "project:<folder>" for a Claude Code project.
	Scope string `json:"scope"`
}

// Client is one AI client's MCP configuration.
type Client struct {
	Provider    string   `json:"provider"`
	ConfigFound bool     `json:"config_found"`
	ConfigHint  string   `json:"config_hint"`
	Servers     []Server `json:"servers"`
	// Inherits lists providers whose user-scope servers this client also
	// loads through its own compatibility setting (Grok can load Claude Code
	// and Cursor servers).
	Inherits []string `json:"inherits,omitempty"`
	// Error is set when a config file exists but cannot be parsed. It never
	// quotes file content.
	Error string `json:"error,omitempty"`
}

// Row is one matrix row: a server name and the providers configured for it.
type Row struct {
	Name       string   `json:"name"`
	Providers  []string `json:"providers"`
	Inherited  []string `json:"inherited"`
	Transports []string `json:"transports"`
	Hosts      []string `json:"hosts"`
}

// Matrix is the body of GET /api/mcp.
type Matrix struct {
	Clients []Client `json:"clients"`
	Servers []Row    `json:"servers"`
}

// def is the only shape decoded from any config file: every other key is
// discarded by the JSON and TOML decoders.
type def struct {
	Type      string `json:"type" toml:"type"`
	Transport string `json:"transport" toml:"transport"`
	URL       string `json:"url" toml:"url"`
	ServerURL string `json:"serverUrl" toml:"-"`
	Command   string `json:"command" toml:"command"`
}

// server reduces a definition. The command and the URL are read only to
// classify the transport and take the host.
func (d def) server(name, scope string) Server {
	s := Server{Name: name, Scope: scope}
	u := d.URL
	if u == "" {
		u = d.ServerURL
	}
	t := strings.ToLower(d.Type)
	if t == "" {
		t = strings.ToLower(d.Transport)
	}
	switch {
	case strings.Contains(t, "sse"):
		s.Transport = SSE
	case strings.Contains(t, "http"):
		s.Transport = HTTP
	case t == Stdio:
		s.Transport = Stdio
	case u != "":
		s.Transport = HTTP
	default:
		s.Transport = Stdio
	}
	if u != "" && s.Transport != Stdio {
		if p, err := url.Parse(u); err == nil {
			s.Host = p.Hostname()
		}
	}
	return s
}

// Roots says where to look; it reuses the accounts roots so LUCID_HOST_HOME,
// CODEX_HOME and disabled providers apply the same way.
type Roots = accounts.Roots

// FromConfig builds Roots from the user config and the environment.
func FromConfig(c *config.Config) Roots { return accounts.FromConfig(c) }

// Read collects every enabled client's servers and the matrix.
func Read(r Roots) Matrix {
	m := Matrix{Clients: []Client{}, Servers: []Row{}}
	for _, p := range config.Providers {
		if !r.On(p) {
			continue
		}
		var c Client
		switch p {
		case "claude":
			c = readClaude(r.Home)
		case "codex":
			dir := r.CodexHome
			if dir == "" {
				dir = filepath.Join(r.Home, ".codex")
			}
			c = readTOML("codex", filepath.Join(dir, "config.toml"), r.Home)
		case "grok":
			c = readTOML("grok", filepath.Join(r.Home, ".grok", "config.toml"), r.Home)
		case "cursor":
			c = readCursor(r.Home)
		}
		sort.SliceStable(c.Servers, func(i, j int) bool {
			if (c.Servers[i].Scope == "user") != (c.Servers[j].Scope == "user") {
				return c.Servers[i].Scope == "user"
			}
			return strings.ToLower(c.Servers[i].Name) < strings.ToLower(c.Servers[j].Name)
		})
		m.Clients = append(m.Clients, c)
	}
	m.Servers = matrix(m.Clients)
	return m
}

func hint(path, home string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(filepath.Join("~", rel))
		}
	}
	return filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
}

func newClient(provider, path, home string) Client {
	return Client{Provider: provider, ConfigHint: hint(path, home), Servers: []Server{}}
}

// readFile reads a config file; found is false when it does not exist.
func readFile(path string) (data []byte, found bool, err error) {
	data, err = os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return data, true, err
}

func parseError(path string) string {
	return "could not parse " + filepath.Base(path)
}

// folder returns the last element of a project path written with either
// slash style, so a Windows key reads the same on any OS.
func folder(p string) string {
	p = strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return "(root)"
	}
	return p
}

func sortedNames[T any](m map[string]T) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func readClaude(home string) Client {
	path := filepath.Join(home, ".claude.json")
	c := newClient("claude", path, home)
	var doc struct {
		MCPServers map[string]def `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]def `json:"mcpServers"`
		} `json:"projects"`
	}
	data, found, err := readFile(path)
	c.ConfigFound = found
	switch {
	case err != nil:
		c.Error = "could not read " + filepath.Base(path)
	case found && json.Unmarshal(data, &doc) != nil:
		c.Error = parseError(path)
	case found:
		for _, n := range sortedNames(doc.MCPServers) {
			c.Servers = append(c.Servers, doc.MCPServers[n].server(n, "user"))
		}
		for _, p := range sortedNames(doc.Projects) {
			ms := doc.Projects[p].MCPServers
			for _, n := range sortedNames(ms) {
				c.Servers = append(c.Servers, ms[n].server(n, "project:"+folder(p)))
			}
		}
	}

	// ~/.claude/settings.json may also declare servers.
	spath := filepath.Join(home, ".claude", "settings.json")
	var settings struct {
		MCPServers map[string]def `json:"mcpServers"`
	}
	if sdata, sfound, err := readFile(spath); sfound && err == nil {
		if json.Unmarshal(sdata, &settings) == nil && len(settings.MCPServers) > 0 {
			c.ConfigFound = true
			for _, n := range sortedNames(settings.MCPServers) {
				if !hasServer(c.Servers, n, "user") {
					c.Servers = append(c.Servers, settings.MCPServers[n].server(n, "user"))
				}
			}
		}
	}
	return c
}

func hasServer(ss []Server, name, scope string) bool {
	for _, s := range ss {
		if s.Name == name && s.Scope == scope {
			return true
		}
	}
	return false
}

func readCursor(home string) Client {
	path := filepath.Join(home, ".cursor", "mcp.json")
	c := newClient("cursor", path, home)
	var doc struct {
		MCPServers map[string]def `json:"mcpServers"`
	}
	data, found, err := readFile(path)
	c.ConfigFound = found
	switch {
	case err != nil:
		c.Error = "could not read " + filepath.Base(path)
	case found && json.Unmarshal(data, &doc) != nil:
		c.Error = parseError(path)
	case found:
		for _, n := range sortedNames(doc.MCPServers) {
			c.Servers = append(c.Servers, doc.MCPServers[n].server(n, "user"))
		}
	}
	return c
}

// readTOML reads [mcp_servers.<name>] tables (Codex and Grok share the
// shape) and Grok's [compat.<client>] mcps switches.
func readTOML(provider, path, home string) Client {
	c := newClient(provider, path, home)
	var doc struct {
		MCPServers map[string]def `toml:"mcp_servers"`
		Compat     map[string]struct {
			MCPs bool `toml:"mcps"`
		} `toml:"compat"`
	}
	data, found, err := readFile(path)
	c.ConfigFound = found
	switch {
	case err != nil:
		c.Error = "could not read " + filepath.Base(path)
	case found:
		if _, err := toml.Decode(string(data), &doc); err != nil {
			c.Error = parseError(path)
			break
		}
		for _, n := range sortedNames(doc.MCPServers) {
			c.Servers = append(c.Servers, doc.MCPServers[n].server(n, "user"))
		}
		if provider == "grok" {
			for _, k := range sortedNames(doc.Compat) {
				if doc.Compat[k].MCPs && (k == "claude" || k == "cursor") {
					c.Inherits = append(c.Inherits, k)
				}
			}
		}
	}
	return c
}

// matrix groups servers by name (case-insensitive) across clients.
func matrix(clients []Client) []Row {
	rows := map[string]*Row{}
	var order []string
	add := func(list []string, v string) []string {
		for _, x := range list {
			if x == v {
				return list
			}
		}
		return append(list, v)
	}
	row := func(name string) *Row {
		k := strings.ToLower(name)
		r, ok := rows[k]
		if !ok {
			r = &Row{Name: name, Providers: []string{}, Inherited: []string{}, Transports: []string{}, Hosts: []string{}}
			rows[k] = r
			order = append(order, k)
		}
		return r
	}
	byProvider := map[string]Client{}
	for _, c := range clients {
		byProvider[c.Provider] = c
		for _, s := range c.Servers {
			r := row(s.Name)
			r.Providers = add(r.Providers, c.Provider)
			r.Transports = add(r.Transports, s.Transport)
			if s.Host != "" {
				r.Hosts = add(r.Hosts, s.Host)
			}
		}
	}
	for _, c := range clients {
		for _, from := range c.Inherits {
			for _, s := range byProvider[from].Servers {
				if s.Scope != "user" {
					continue
				}
				r := row(s.Name)
				if !contains(r.Providers, c.Provider) {
					r.Inherited = add(r.Inherited, c.Provider)
				}
			}
		}
	}
	out := make([]Row, 0, len(order))
	for _, k := range order {
		r := rows[k]
		r.Providers = ordered(r.Providers)
		r.Inherited = ordered(r.Inherited)
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := len(out[i].Providers)+len(out[i].Inherited), len(out[j].Providers)+len(out[j].Inherited)
		if a != b {
			return a > b
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ordered sorts provider names into config.Providers order.
func ordered(ps []string) []string {
	out := []string{}
	for _, p := range config.Providers {
		if contains(ps, p) {
			out = append(out, p)
		}
	}
	return out
}
