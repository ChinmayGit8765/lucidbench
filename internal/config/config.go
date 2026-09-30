// Package config is the user configuration layer. Everything specific to a
// user or machine lives in the user's config and data directories or in the
// environment, never in the repository.
//
// Load order: built-in defaults, then the config file, then LUCID_*
// environment variables. Secrets are never stored as values: a secret field
// only accepts a reference such as env:NAME (see SecretRef).
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Providers lists the supported provider names in display order.
var Providers = []string{"claude", "codex", "grok", "cursor"}

// Themes are the accepted values of ui.theme.
var Themes = []string{"dark", "light", "system"}

// Built-in defaults.
const (
	DefaultAddr        = ":7420"
	DefaultClusterName = "lucidbench"
	DefaultAgentImage  = "lucidbench/agent:dev"
	DefaultTheme       = "dark"
)

// ServerConfig configures the daemon listener.
type ServerConfig struct {
	Addr string `json:"addr"`
}

// ProviderConfig configures one AI provider.
type ProviderConfig struct {
	Enabled   bool     `json:"enabled"`
	ExtraDirs []string `json:"extra_dirs"`
}

// ClusterConfig configures the local kind cluster.
type ClusterConfig struct {
	Name string `json:"name"`
}

// AgentConfig configures the agent container image.
type AgentConfig struct {
	Image string `json:"image"`
}

// VaultConfig points at the user's notes vault. Empty means not configured.
type VaultConfig struct {
	Path string `json:"path"`
}

// UIConfig configures the web UI.
type UIConfig struct {
	Theme string `json:"theme"`
}

// Config is the effective configuration. It holds no secret values.
type Config struct {
	Server    ServerConfig              `json:"server"`
	Providers map[string]ProviderConfig `json:"providers"`
	Cluster   ClusterConfig             `json:"cluster"`
	Agent     AgentConfig               `json:"agent"`
	Vault     VaultConfig               `json:"vault"`
	UI        UIConfig                  `json:"ui"`

	// File is the config file path that was consulted; FileFound says whether
	// it existed.
	File      string `json:"-"`
	FileFound bool   `json:"-"`

	sources map[string]string
}

// Default returns the built-in defaults.
func Default() *Config {
	c := &Config{
		Server:    ServerConfig{Addr: DefaultAddr},
		Providers: map[string]ProviderConfig{},
		Cluster:   ClusterConfig{Name: DefaultClusterName},
		Agent:     AgentConfig{Image: DefaultAgentImage},
		UI:        UIConfig{Theme: DefaultTheme},
		sources:   map[string]string{},
	}
	for _, p := range Providers {
		c.Providers[p] = ProviderConfig{Enabled: true, ExtraDirs: []string{}}
	}
	for _, k := range Keys() {
		c.sources[k] = "default"
	}
	return c
}

// Keys returns every config key in display order.
func Keys() []string {
	ks := []string{"server.addr"}
	for _, p := range Providers {
		ks = append(ks, "providers."+p+".enabled", "providers."+p+".extra_dirs")
	}
	return append(ks, "cluster.name", "agent.image", "vault.path", "ui.theme")
}

// Source reports where a key's effective value came from: "default", "file"
// or "env:NAME".
func (c *Config) Source(key string) string {
	if s, ok := c.sources[key]; ok {
		return s
	}
	return "default"
}

// ProviderEnabled reports whether a provider is enabled. Unknown names are
// treated as enabled so a newer provider is not silently hidden.
func (c *Config) ProviderEnabled(name string) bool {
	p, ok := c.Providers[name]
	return !ok || p.Enabled
}

// ExtraDirs returns the extra config directories for a provider.
func (c *Config) ExtraDirs(name string) []string { return c.Providers[name].ExtraDirs }

// ConfigPath returns the config file location: $LUCID_CONFIG, else
// <user config dir>/lucidbench/config.yaml.
func ConfigPath() (string, error) { return configPath(os.Getenv, os.UserConfigDir) }

func configPath(getenv func(string) string, userConfigDir func() (string, error)) (string, error) {
	if p := getenv("LUCID_CONFIG"); p != "" {
		return p, nil
	}
	d, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(d, "lucidbench", "config.yaml"), nil
}

// DataDir returns where Lucidbench keeps its own state (locks, kubeconfig):
// $LUCID_DATA_DIR, else <user config dir>/lucidbench.
func DataDir() (string, error) { return dataDir(os.Getenv, os.UserConfigDir) }

func dataDir(getenv func(string) string, userConfigDir func() (string, error)) (string, error) {
	if p := getenv("LUCID_DATA_DIR"); p != "" {
		return p, nil
	}
	d, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(d, "lucidbench"), nil
}

// Load reads defaults, the config file and the process environment. The
// returned warnings (unknown keys) are not fatal. A missing file is not an
// error. An invalid value is an error naming the file (or env var) and key.
func Load() (*Config, []string, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, nil, err
	}
	return LoadFrom(path, os.Getenv)
}

// LoadFrom is Load with an explicit file path and environment lookup.
func LoadFrom(path string, getenv func(string) string) (*Config, []string, error) {
	c := Default()
	c.File = path
	var warns []string
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		c.FileFound = true
		w, err := c.applyFile(path, data)
		warns = append(warns, w...)
		if err != nil {
			return nil, warns, err
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := c.applyEnv(getenv); err != nil {
		return nil, warns, err
	}
	c.expandHome()
	if err := c.validate(); err != nil {
		return nil, warns, err
	}
	return c, warns, nil
}

// ---- file layer ----

type entry struct {
	key  string
	node *yaml.Node
}

type fileDecoder struct {
	c     *Config
	path  string
	warns []string
}

func (d *fileDecoder) errAt(n *yaml.Node, key, msg string) error {
	return fmt.Errorf("config %s:%d: %s: %s", d.path, n.Line, key, msg)
}

func (d *fileDecoder) entries(n *yaml.Node, path string) ([]entry, error) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, d.errAt(n, path, "must be a mapping")
	}
	var out []entry
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, entry{n.Content[i].Value, n.Content[i+1]})
	}
	return out, nil
}

func (d *fileDecoder) str(n *yaml.Node, key string) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", d.errAt(n, key, "must be a string")
	}
	if n.Tag == "!!null" {
		return "", nil
	}
	return n.Value, nil
}

func (d *fileDecoder) boolean(n *yaml.Node, key string) (bool, error) {
	var b bool
	if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" || n.Decode(&b) != nil {
		return false, d.errAt(n, key, "must be true or false")
	}
	return b, nil
}

func (d *fileDecoder) list(n *yaml.Node, key string) ([]string, error) {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return []string{}, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, d.errAt(n, key, "must be a list of strings")
	}
	out := []string{}
	for _, it := range n.Content {
		if it.Kind != yaml.ScalarNode || it.Tag == "!!null" {
			return nil, d.errAt(it, key, "must be a list of strings")
		}
		out = append(out, it.Value)
	}
	return out, nil
}

func (d *fileDecoder) warnUnknown(n *yaml.Node, key string) {
	d.warns = append(d.warns, fmt.Sprintf("config %s:%d: unknown key %q ignored", d.path, n.Line, key))
}

func (c *Config) applyFile(path string, data []byte) ([]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("config %s: invalid YAML: %w", path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, nil
	}
	d := &fileDecoder{c: c, path: path}
	top, err := d.entries(doc.Content[0], "(top level)")
	if err != nil {
		return d.warns, err
	}
	for _, e := range top {
		var err error
		switch e.key {
		case "server":
			err = d.section(e, func(k string, v *yaml.Node) (bool, error) {
				if k != "addr" {
					return false, nil
				}
				s, err := d.str(v, "server.addr")
				c.Server.Addr = s
				c.sources["server.addr"] = "file"
				return true, err
			})
		case "providers":
			err = d.providers(e)
		case "cluster":
			err = d.section(e, d.stringField("cluster", "name", &c.Cluster.Name))
		case "agent":
			err = d.section(e, d.stringField("agent", "image", &c.Agent.Image))
		case "vault":
			err = d.section(e, d.stringField("vault", "path", &c.Vault.Path))
		case "ui":
			err = d.section(e, d.stringField("ui", "theme", &c.UI.Theme))
		default:
			d.warnUnknown(e.node, e.key)
		}
		if err != nil {
			return d.warns, err
		}
	}
	return d.warns, nil
}

func (d *fileDecoder) stringField(section, name string, dst *string) func(string, *yaml.Node) (bool, error) {
	return func(k string, v *yaml.Node) (bool, error) {
		if k != name {
			return false, nil
		}
		s, err := d.str(v, section+"."+name)
		*dst = s
		d.c.sources[section+"."+name] = "file"
		return true, err
	}
}

func (d *fileDecoder) section(e entry, field func(string, *yaml.Node) (bool, error)) error {
	es, err := d.entries(e.node, e.key)
	if err != nil {
		return err
	}
	for _, f := range es {
		known, err := field(f.key, f.node)
		if err != nil {
			return err
		}
		if !known {
			d.warnUnknown(f.node, e.key+"."+f.key)
		}
	}
	return nil
}

func (d *fileDecoder) providers(e entry) error {
	ps, err := d.entries(e.node, "providers")
	if err != nil {
		return err
	}
	for _, p := range ps {
		if _, ok := d.c.Providers[p.key]; !ok {
			d.warnUnknown(p.node, "providers."+p.key)
			continue
		}
		name := p.key
		err := d.section(p, func(k string, v *yaml.Node) (bool, error) {
			key := "providers." + name + "." + k
			pc := d.c.Providers[name]
			switch k {
			case "enabled":
				b, err := d.boolean(v, key)
				pc.Enabled = b
				d.c.Providers[name] = pc
				d.c.sources[key] = "file"
				return true, err
			case "extra_dirs":
				l, err := d.list(v, key)
				pc.ExtraDirs = l
				d.c.Providers[name] = pc
				d.c.sources[key] = "file"
				return true, err
			}
			return false, nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ---- env layer ----

// envSpec maps a key to its environment variables, lowest precedence first.
var envSpecs = map[string][]string{
	"server.addr":  {"LUCID_ADDR", "LUCID_SERVER_ADDR"},
	"cluster.name": {"LUCID_CLUSTER_NAME"},
	"agent.image":  {"LUCID_AGENT_IMAGE"},
	"vault.path":   {"LUCID_VAULT_PATH"},
	"ui.theme":     {"LUCID_UI_THEME"},
}

func providerEnv(p, field string) string {
	return "LUCID_PROVIDERS_" + strings.ToUpper(p) + "_" + strings.ToUpper(field)
}

func (c *Config) applyEnv(getenv func(string) string) error {
	dst := map[string]*string{
		"server.addr":  &c.Server.Addr,
		"cluster.name": &c.Cluster.Name,
		"agent.image":  &c.Agent.Image,
		"vault.path":   &c.Vault.Path,
		"ui.theme":     &c.UI.Theme,
	}
	for key, names := range envSpecs {
		for _, n := range names {
			if v := getenv(n); v != "" {
				*dst[key] = v
				c.sources[key] = "env:" + n
			}
		}
	}
	for _, p := range Providers {
		pc := c.Providers[p]
		if n := providerEnv(p, "enabled"); getenv(n) != "" {
			b, ok := parseBool(getenv(n))
			if !ok {
				return fmt.Errorf("env %s: providers.%s.enabled: must be true or false", n, p)
			}
			pc.Enabled = b
			c.sources["providers."+p+".enabled"] = "env:" + n
		}
		if n := providerEnv(p, "extra_dirs"); getenv(n) != "" {
			pc.ExtraDirs = splitList(getenv(n))
			c.sources["providers."+p+".extra_dirs"] = "env:" + n
		}
		c.Providers[p] = pc
	}
	// Legacy: LUCID_CLAUDE_DIRS extends the Claude directory list.
	if v := getenv("LUCID_CLAUDE_DIRS"); v != "" {
		pc := c.Providers["claude"]
		pc.ExtraDirs = appendUnique(pc.ExtraDirs, splitList(v)...)
		c.Providers["claude"] = pc
		if c.sources["providers.claude.extra_dirs"] == "default" {
			c.sources["providers.claude.extra_dirs"] = "env:LUCID_CLAUDE_DIRS"
		}
	}
	return nil
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}
	return false, false
}

func splitList(s string) []string {
	var out []string
	for _, p := range filepath.SplitList(s) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func appendUnique(dst []string, add ...string) []string {
	for _, a := range add {
		found := false
		for _, d := range dst {
			if d == a {
				found = true
			}
		}
		if !found {
			dst = append(dst, a)
		}
	}
	return dst
}

// ---- post-processing ----

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}

func (c *Config) expandHome() {
	c.Vault.Path = expandHome(c.Vault.Path)
	for name, pc := range c.Providers {
		for i, d := range pc.ExtraDirs {
			pc.ExtraDirs[i] = expandHome(d)
		}
		c.Providers[name] = pc
	}
}

var clusterNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,48}[a-z0-9])?$`)

func (c *Config) origin(key string) string {
	s := c.Source(key)
	switch {
	case s == "file":
		return "config " + c.File
	case strings.HasPrefix(s, "env:"):
		return "env " + strings.TrimPrefix(s, "env:")
	}
	return "default"
}

func (c *Config) bad(key, msg string) error {
	return fmt.Errorf("%s: %s: %s", c.origin(key), key, msg)
}

func (c *Config) validate() error {
	if _, port, err := net.SplitHostPort(c.Server.Addr); err != nil || port == "" {
		return c.bad("server.addr", `must be host:port, for example ":7420" or "127.0.0.1:7420"`)
	}
	if !clusterNameRE.MatchString(c.Cluster.Name) {
		return c.bad("cluster.name", "must be lowercase letters, digits and dashes, 1-50 characters")
	}
	if c.Agent.Image == "" || strings.ContainsAny(c.Agent.Image, " \t") {
		return c.bad("agent.image", "must be a non-empty image reference without spaces")
	}
	okTheme := false
	for _, t := range Themes {
		okTheme = okTheme || c.UI.Theme == t
	}
	if !okTheme {
		return c.bad("ui.theme", "must be one of "+strings.Join(Themes, ", "))
	}
	for _, p := range Providers {
		for _, d := range c.Providers[p].ExtraDirs {
			if strings.TrimSpace(d) == "" {
				return c.bad("providers."+p+".extra_dirs", "entries must not be empty")
			}
		}
	}
	return nil
}

// Rows returns every key with its effective value and source, for display.
// List values are joined with the OS path list separator. No secret values
// exist in Config, so nothing here needs redacting.
func (c *Config) Rows() [][3]string {
	var rows [][3]string
	for _, k := range Keys() {
		rows = append(rows, [3]string{k, c.value(k), c.Source(k)})
	}
	return rows
}

func (c *Config) value(key string) string {
	switch key {
	case "server.addr":
		return c.Server.Addr
	case "cluster.name":
		return c.Cluster.Name
	case "agent.image":
		return c.Agent.Image
	case "vault.path":
		return c.Vault.Path
	case "ui.theme":
		return c.UI.Theme
	}
	parts := strings.Split(key, ".")
	if len(parts) == 3 && parts[0] == "providers" {
		pc := c.Providers[parts[1]]
		if parts[2] == "enabled" {
			return fmt.Sprint(pc.Enabled)
		}
		return "[" + strings.Join(pc.ExtraDirs, ", ") + "]"
	}
	return ""
}
