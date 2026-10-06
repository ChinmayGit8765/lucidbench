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
	"slices"
	"strings"
	"unicode"

	"go.yaml.in/yaml/v3"
)

// Providers lists the supported provider names in display order.
var Providers = []string{"claude", "codex", "grok", "cursor"}

// Themes are the accepted values of ui.theme.
var Themes = []string{"dark", "light", "system"}

// Built-in defaults.
const (
	DefaultAddr        = "127.0.0.1:7420"
	DefaultClusterName = "lucidbench"
	DefaultAgentImage  = "lucidbench/agent:dev"
	DefaultTheme       = "dark"

	DefaultCIToken          SecretRef = "env:GITHUB_TOKEN"
	DefaultCIComposeProject           = ""
	DefaultCIImageMatch               = "github-runner"

	DefaultPowerCluster     = PowerOnDemand
	DefaultPowerClusterIdle = 15
	DefaultPowerRunners     = PowerAlways
	DefaultPowerRunnerIdle  = 10
	DefaultPowerPollSeconds = 60
	DefaultPowerStackMode   = PowerOnDemand
	MinPowerPollSeconds     = 10
	MaxPowerPollSeconds     = 3600
	MaxPowerIdleMinutes     = 24 * 60
)

// Power modes for the cluster, the runner containers and stacks.
const (
	// PowerAlways: Lucidbench never stops it.
	PowerAlways = "always"
	// PowerOnDemand: started when something needs it, stopped when idle.
	PowerOnDemand = "on-demand"
	// PowerOff: Lucidbench never starts or stops it on its own.
	PowerOff = "off"
)

// PowerModes are the accepted power modes, in display order.
var PowerModes = []string{PowerAlways, PowerOnDemand, PowerOff}

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

// CIGitHubConfig lists the GitHub repositories whose self-hosted runners and
// workflow runs Lucidbench shows, and the token used to read them.
type CIGitHubConfig struct {
	Repos []string  `json:"repos"`
	Token SecretRef `json:"token"`
}

// CIRunnersConfig selects the local runner containers: those in the compose
// project, or whose image contains ImageMatch. Empty disables that match.
type CIRunnersConfig struct {
	ComposeProject string `json:"compose_project"`
	ImageMatch     string `json:"image_match"`
}

// CIConfig configures the Runners & CI integration.
type CIConfig struct {
	GitHub  CIGitHubConfig  `json:"github"`
	Runners CIRunnersConfig `json:"runners"`
}

// DockerConfig configures the Containers page.
type DockerConfig struct {
	// AllowedProjects are extra compose projects whose containers the UI may
	// start, stop and restart (the lucidbench project and runner containers
	// are always allowed).
	AllowedProjects []string `json:"allowed_projects"`
}

// PowerStack is a compose project whose containers Lucidbench may start and
// stop as a group.
type PowerStack struct {
	Project string `json:"project"`
	Mode    string `json:"mode"`
}

// PowerConfig configures on-demand infrastructure: whether the kind cluster,
// the runner containers and listed compose stacks are started when needed
// and stopped when idle.
type PowerConfig struct {
	Cluster            string       `json:"cluster"`
	ClusterIdleMinutes int          `json:"cluster_idle_minutes"`
	Runners            string       `json:"runners"`
	RunnerIdleMinutes  int          `json:"runner_idle_minutes"`
	Stacks             []PowerStack `json:"stacks"`
	PollSeconds        int          `json:"poll_seconds"`
}

// Config is the effective configuration. It holds no secret values.
type Config struct {
	Server    ServerConfig              `json:"server"`
	Providers map[string]ProviderConfig `json:"providers"`
	Cluster   ClusterConfig             `json:"cluster"`
	Agent     AgentConfig               `json:"agent"`
	Vault     VaultConfig               `json:"vault"`
	UI        UIConfig                  `json:"ui"`
	CI        CIConfig                  `json:"ci"`
	Docker    DockerConfig              `json:"docker"`
	Power     PowerConfig               `json:"power"`

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
		CI: CIConfig{
			GitHub:  CIGitHubConfig{Repos: []string{}, Token: DefaultCIToken},
			Runners: CIRunnersConfig{ComposeProject: DefaultCIComposeProject, ImageMatch: DefaultCIImageMatch},
		},
		Docker: DockerConfig{AllowedProjects: []string{}},
		Power: PowerConfig{
			Cluster: DefaultPowerCluster, ClusterIdleMinutes: DefaultPowerClusterIdle,
			Runners: DefaultPowerRunners, RunnerIdleMinutes: DefaultPowerRunnerIdle,
			Stacks: []PowerStack{}, PollSeconds: DefaultPowerPollSeconds,
		},
		sources: map[string]string{},
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
	return append(ks, "cluster.name", "agent.image", "vault.path", "ui.theme",
		"ci.github.repos", "ci.github.token", "ci.runners.compose_project", "ci.runners.image_match", "docker.allowed_projects",
		"power.cluster", "power.cluster_idle_minutes", "power.runners", "power.runner_idle_minutes", "power.stacks", "power.poll_seconds")
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
		case "ci":
			err = d.ci(e)
		case "docker":
			err = d.section(e, func(k string, v *yaml.Node) (bool, error) {
				if k != "allowed_projects" {
					return false, nil
				}
				l, err := d.list(v, "docker.allowed_projects")
				c.Docker.AllowedProjects = l
				c.sources["docker.allowed_projects"] = "file"
				return true, err
			})
		case "power":
			err = d.power(e)
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

func (d *fileDecoder) ci(e entry) error {
	return d.section(e, func(k string, v *yaml.Node) (bool, error) {
		sub := entry{"ci." + k, v}
		switch k {
		case "github":
			return true, d.section(sub, func(k string, v *yaml.Node) (bool, error) {
				key := "ci.github." + k
				switch k {
				case "repos":
					l, err := d.list(v, key)
					d.c.CI.GitHub.Repos = l
					d.c.sources[key] = "file"
					return true, err
				case "token":
					s, err := d.str(v, key)
					if err != nil {
						return true, err
					}
					ref, err := ParseSecretRef(s)
					if err != nil {
						return true, d.errAt(v, key, err.Error())
					}
					d.c.CI.GitHub.Token = ref
					d.c.sources[key] = "file"
					return true, nil
				}
				return false, nil
			})
		case "runners":
			return true, d.section(sub, func(k string, v *yaml.Node) (bool, error) {
				switch k {
				case "compose_project":
					return d.stringField("ci.runners", k, &d.c.CI.Runners.ComposeProject)(k, v)
				case "image_match":
					return d.stringField("ci.runners", k, &d.c.CI.Runners.ImageMatch)(k, v)
				}
				return false, nil
			})
		}
		return false, nil
	})
}

func (d *fileDecoder) integer(n *yaml.Node, key string) (int, error) {
	var i int
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" || n.Decode(&i) != nil {
		return 0, d.errAt(n, key, "must be a whole number")
	}
	return i, nil
}

func (d *fileDecoder) power(e entry) error {
	p := &d.c.Power
	return d.section(e, func(k string, v *yaml.Node) (bool, error) {
		key := "power." + k
		var err error
		switch k {
		case "cluster":
			p.Cluster, err = d.str(v, key)
		case "runners":
			p.Runners, err = d.str(v, key)
		case "cluster_idle_minutes":
			p.ClusterIdleMinutes, err = d.integer(v, key)
		case "runner_idle_minutes":
			p.RunnerIdleMinutes, err = d.integer(v, key)
		case "poll_seconds":
			p.PollSeconds, err = d.integer(v, key)
		case "stacks":
			p.Stacks, err = d.stacks(v, key)
		default:
			return false, nil
		}
		d.c.sources[key] = "file"
		return true, err
	})
}

// stacks reads power.stacks, a list of {project, mode} mappings. A missing
// mode is on-demand.
func (d *fileDecoder) stacks(n *yaml.Node, key string) ([]PowerStack, error) {
	out := []PowerStack{}
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return out, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, d.errAt(n, key, "must be a list of {project, mode}")
	}
	for _, it := range n.Content {
		es, err := d.entries(it, key)
		if err != nil {
			return nil, err
		}
		s := PowerStack{Mode: DefaultPowerStackMode}
		for _, f := range es {
			switch f.key {
			case "project":
				s.Project, err = d.str(f.node, key+".project")
			case "mode":
				s.Mode, err = d.str(f.node, key+".mode")
			default:
				d.warnUnknown(f.node, key+"."+f.key)
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// ---- env layer ----

// envSpec maps a key to its environment variables, lowest precedence first.
var envSpecs = map[string][]string{
	"server.addr":  {"LUCID_ADDR", "LUCID_SERVER_ADDR"},
	"cluster.name": {"LUCID_CLUSTER_NAME"},
	"agent.image":  {"LUCID_AGENT_IMAGE"},
	"vault.path":   {"LUCID_VAULT_PATH"},
	"ui.theme":     {"LUCID_UI_THEME"},

	"ci.runners.compose_project": {"LUCID_CI_RUNNERS_COMPOSE_PROJECT"},
	"ci.runners.image_match":     {"LUCID_CI_RUNNERS_IMAGE_MATCH"},

	"power.cluster": {"LUCID_POWER_CLUSTER"},
	"power.runners": {"LUCID_POWER_RUNNERS"},
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

		"ci.runners.compose_project": &c.CI.Runners.ComposeProject,
		"ci.runners.image_match":     &c.CI.Runners.ImageMatch,

		"power.cluster": &c.Power.Cluster,
		"power.runners": &c.Power.Runners,
	}
	for key, names := range envSpecs {
		for _, n := range names {
			if v := getenv(n); v != "" {
				*dst[key] = v
				c.sources[key] = "env:" + n
			}
		}
	}
	if v := getenv("LUCID_CI_GITHUB_REPOS"); v != "" {
		// Comma or whitespace separated owner/name list.
		c.CI.GitHub.Repos = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		c.sources["ci.github.repos"] = "env:LUCID_CI_GITHUB_REPOS"
	}
	if v := getenv("LUCID_DOCKER_ALLOWED_PROJECTS"); v != "" {
		c.Docker.AllowedProjects = strings.FieldsFunc(v, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		c.sources["docker.allowed_projects"] = "env:LUCID_DOCKER_ALLOWED_PROJECTS"
	}
	if v := getenv("LUCID_CI_GITHUB_TOKEN"); v != "" {
		ref, err := ParseSecretRef(v)
		if err != nil {
			return fmt.Errorf("env LUCID_CI_GITHUB_TOKEN: ci.github.token: %w", err)
		}
		c.CI.GitHub.Token = ref
		c.sources["ci.github.token"] = "env:LUCID_CI_GITHUB_TOKEN"
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
	for _, r := range c.CI.GitHub.Repos {
		if !repoRE.MatchString(r) {
			return c.bad("ci.github.repos", fmt.Sprintf("%q must be owner/name", r))
		}
	}
	if strings.ContainsAny(c.CI.Runners.ComposeProject, " \t") {
		return c.bad("ci.runners.compose_project", "must not contain spaces")
	}
	for _, p := range c.Docker.AllowedProjects {
		if !composeProjectRE.MatchString(p) {
			return c.bad("docker.allowed_projects", fmt.Sprintf("%q must be a compose project name (lowercase letters, digits, dashes, underscores)", p))
		}
	}
	return c.validatePower()
}

func validMode(m string) bool { return slices.Contains(PowerModes, m) }

func (c *Config) validatePower() error {
	p := c.Power
	modes := strings.Join(PowerModes, ", ")
	if !validMode(p.Cluster) {
		return c.bad("power.cluster", "must be one of "+modes)
	}
	if !validMode(p.Runners) {
		return c.bad("power.runners", "must be one of "+modes)
	}
	if p.ClusterIdleMinutes < 1 || p.ClusterIdleMinutes > MaxPowerIdleMinutes {
		return c.bad("power.cluster_idle_minutes", fmt.Sprintf("must be between 1 and %d minutes", MaxPowerIdleMinutes))
	}
	if p.RunnerIdleMinutes < 1 || p.RunnerIdleMinutes > MaxPowerIdleMinutes {
		return c.bad("power.runner_idle_minutes", fmt.Sprintf("must be between 1 and %d minutes", MaxPowerIdleMinutes))
	}
	if p.PollSeconds < MinPowerPollSeconds || p.PollSeconds > MaxPowerPollSeconds {
		return c.bad("power.poll_seconds", fmt.Sprintf("must be between %d and %d seconds", MinPowerPollSeconds, MaxPowerPollSeconds))
	}
	seen := map[string]bool{}
	for _, s := range p.Stacks {
		if !composeProjectRE.MatchString(s.Project) {
			return c.bad("power.stacks", fmt.Sprintf("project %q must be a compose project name (lowercase letters, digits, dashes, underscores)", s.Project))
		}
		if seen[s.Project] {
			return c.bad("power.stacks", fmt.Sprintf("project %q is listed twice", s.Project))
		}
		seen[s.Project] = true
		if !validMode(s.Mode) {
			return c.bad("power.stacks", fmt.Sprintf("mode of %q must be one of %s", s.Project, modes))
		}
	}
	return nil
}

// StackMode returns the power mode of a compose project and whether it is
// listed in power.stacks.
func (c *Config) StackMode(project string) (string, bool) {
	for _, s := range c.Power.Stacks {
		if s.Project == project {
			return s.Mode, true
		}
	}
	return "", false
}

var composeProjectRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

var repoRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

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
	case "ci.github.repos":
		return "[" + strings.Join(c.CI.GitHub.Repos, ", ") + "]"
	case "ci.github.token":
		return c.CI.GitHub.Token.String()
	case "ci.runners.compose_project":
		return c.CI.Runners.ComposeProject
	case "ci.runners.image_match":
		return c.CI.Runners.ImageMatch
	case "docker.allowed_projects":
		return "[" + strings.Join(c.Docker.AllowedProjects, ", ") + "]"
	case "power.cluster":
		return c.Power.Cluster
	case "power.cluster_idle_minutes":
		return fmt.Sprint(c.Power.ClusterIdleMinutes)
	case "power.runners":
		return c.Power.Runners
	case "power.runner_idle_minutes":
		return fmt.Sprint(c.Power.RunnerIdleMinutes)
	case "power.poll_seconds":
		return fmt.Sprint(c.Power.PollSeconds)
	case "power.stacks":
		ss := []string{}
		for _, s := range c.Power.Stacks {
			ss = append(ss, s.Project+" ("+s.Mode+")")
		}
		return "[" + strings.Join(ss, ", ") + "]"
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
