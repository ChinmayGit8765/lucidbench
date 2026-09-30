// Package accounts detects which AI provider accounts are available to
// Lucidbench: host config directories, Docker profile volumes and API keys
// present in the environment.
//
// Detection is by PRESENCE only. Token values are never read into results,
// logged, printed or copied. The only file content inspected is a non-secret
// expiry timestamp, and secret fields are discarded by the JSON decoder.
//
// Fair use: profiles are separate accounts you own and sign in to yourself.
// Lucidbench never auto-rotates between profiles to extend or evade
// provider usage limits.
package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Location values for Profile.Location.
const (
	LocHost   = "host"
	LocVolume = "volume"
	LocEnv    = "env"
)

// Status values for Profile.Status.
const (
	StatusLoggedIn = "logged_in"
	StatusExpired  = "expired"
	StatusMissing  = "missing"
	StatusUnknown  = "unknown"
)

// Profile is one detected (or missing) account profile. It never carries
// credential material.
type Profile struct {
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	Location  string `json:"location"`
	ConfigDir string `json:"config_dir,omitempty"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
}

// Roots says where to look. Home is the (host) home directory; inside Docker
// it is the read-only host mount named by LUCID_HOST_HOME.
type Roots struct {
	Home            string
	ExtraClaudeDirs []string
	CodexHome       string // overrides Home/.codex when set
	Getenv          func(string) string
	Now             func() time.Time

	// Disabled providers are skipped by detection; ExtraDirs holds additional
	// config directories for codex and grok (Claude uses ExtraClaudeDirs).
	Disabled  map[string]bool
	ExtraDirs map[string][]string
}

// On reports whether a provider is enabled.
func (r Roots) On(provider string) bool { return !r.Disabled[provider] }

// FromConfig is FromEnv plus the user's provider settings.
func FromConfig(c *config.Config) Roots {
	r := FromEnv()
	r.Disabled = map[string]bool{}
	r.ExtraDirs = map[string][]string{}
	for _, p := range config.Providers {
		if !c.ProviderEnabled(p) {
			r.Disabled[p] = true
		}
		r.ExtraDirs[p] = c.ExtraDirs(p)
	}
	// The config already folds in LUCID_CLAUDE_DIRS; CLAUDE_CONFIG_DIR stays.
	dirs := append([]string(nil), c.ExtraDirs("claude")...)
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	r.ExtraClaudeDirs = dirs
	return r
}

// FromEnv builds Roots from the process environment.
func FromEnv() Roots {
	r := Roots{Getenv: os.Getenv, Now: time.Now}
	r.Home = os.Getenv("LUCID_HOST_HOME")
	if r.Home == "" {
		r.Home, _ = os.UserHomeDir()
	}
	r.CodexHome = os.Getenv("CODEX_HOME")
	r.ExtraClaudeDirs = filepath.SplitList(os.Getenv("LUCID_CLAUDE_DIRS"))
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		r.ExtraClaudeDirs = append(r.ExtraClaudeDirs, d)
	}
	return r
}

// Detect returns host and env profiles. Volume profiles are added by the
// caller (see ListVolumes) so detection stays free of subprocesses.
func Detect(r Roots) []Profile {
	if r.Getenv == nil {
		r.Getenv = os.Getenv
	}
	if r.Now == nil {
		r.Now = time.Now
	}
	var out []Profile

	if r.On("claude") {
		defClaude := filepath.Join(r.Home, ".claude")
		out = append(out, claudeProfile(r, "default", defClaude))
		seen := map[string]bool{filepath.Clean(defClaude): true}
		for _, d := range r.ExtraClaudeDirs {
			if d == "" || seen[filepath.Clean(d)] {
				continue
			}
			seen[filepath.Clean(d)] = true
			out = append(out, claudeProfile(r, filepath.Base(d), d))
		}
	}

	if r.On("codex") {
		codexDir := r.CodexHome
		if codexDir == "" {
			codexDir = filepath.Join(r.Home, ".codex")
		}
		out = append(out, fileProfile(r, "codex", "default", codexDir, "auth.json"))
		for _, d := range r.ExtraDirs["codex"] {
			out = append(out, fileProfile(r, "codex", filepath.Base(d), d, "auth.json"))
		}
	}
	// Grok has no known home override; extra Grok accounts live in volumes
	// or in configured extra_dirs.
	if r.On("grok") {
		out = append(out, fileProfile(r, "grok", "default", filepath.Join(r.Home, ".grok"), "auth.json"))
		for _, d := range r.ExtraDirs["grok"] {
			out = append(out, fileProfile(r, "grok", filepath.Base(d), d, "auth.json"))
		}
	}
	if r.On("cursor") {
		out = append(out, cursorProfile(filepath.Join(r.Home, ".cursor")))
	}

	for _, k := range []struct{ env, provider string }{
		{"ANTHROPIC_API_KEY", "claude"},
		{"OPENAI_API_KEY", "codex"},
		{"XAI_API_KEY", "grok"},
		{"CURSOR_API_KEY", "cursor"},
	} {
		if r.On(k.provider) && r.Getenv(k.env) != "" {
			out = append(out, Profile{Provider: k.provider, Name: k.env, Location: LocEnv,
				Status: StatusLoggedIn, Detail: "API key set in environment"})
		}
	}
	return out
}

func exists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

func missing(provider, name, dir string) Profile {
	return Profile{Provider: provider, Name: name, Location: LocHost, ConfigDir: dir,
		Status: StatusMissing, Detail: "no credentials found"}
}

// expiry holds only non-secret fields; every other key (tokens) is dropped by
// the decoder. RefreshToken is decoded to raw bytes solely to test presence
// and is not retained.
type expiry struct {
	OAuth *struct {
		ExpiresAt    int64           `json:"expiresAt"`
		RefreshToken json.RawMessage `json:"refreshToken"`
	} `json:"claudeAiOauth"`
	ExpiresAt int64 `json:"expiresAt"`
}

func claudeProfile(r Roots, name, dir string) Profile {
	file := filepath.Join(dir, ".credentials.json")
	if !exists(file) {
		return missing("claude", name, dir)
	}
	p := Profile{Provider: "claude", Name: name, Location: LocHost, ConfigDir: dir, Status: StatusLoggedIn}
	data, err := os.ReadFile(file)
	if err != nil {
		p.Status, p.Detail = StatusUnknown, "credentials file unreadable"
		return p
	}
	var e expiry
	_ = json.Unmarshal(data, &e)
	for i := range data { // do not leave secrets lingering in the buffer
		data[i] = 0
	}
	var ms int64
	hasRefresh := false
	if e.OAuth != nil {
		ms = e.OAuth.ExpiresAt
		hasRefresh = len(e.OAuth.RefreshToken) > 2
	} else {
		ms = e.ExpiresAt
	}
	if ms > 0 {
		at := time.UnixMilli(ms)
		switch {
		case at.After(r.Now()):
			p.Detail = "access token valid until " + at.UTC().Format(time.RFC3339)
		case hasRefresh:
			p.Detail = "access token lapsed; refreshes on next use"
		default:
			p.Status = StatusExpired
			p.Detail = "token expired " + at.UTC().Format(time.RFC3339)
		}
	}
	return p
}

func fileProfile(r Roots, provider, name, dir, file string) Profile {
	if !exists(filepath.Join(dir, file)) {
		return missing(provider, name, dir)
	}
	return Profile{Provider: provider, Name: name, Location: LocHost, ConfigDir: dir, Status: StatusLoggedIn}
}

// cursorProfile is detection-only: status is unknown unless the config has an
// obvious auth marker key. Only key names are inspected, never values.
func cursorProfile(dir string) Profile {
	file := filepath.Join(dir, "cli-config.json")
	if !exists(file) {
		return missing("cursor", "default", dir)
	}
	p := Profile{Provider: "cursor", Name: "default", Location: LocHost, ConfigDir: dir,
		Status: StatusUnknown, Detail: "detection only"}
	data, err := os.ReadFile(file)
	if err != nil {
		return p
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(data, &keys)
	for i := range data {
		data[i] = 0
	}
	for k := range keys {
		if strings.EqualFold(k, "authInfo") {
			p.Status, p.Detail = StatusLoggedIn, "auth marker present"
		}
	}
	return p
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// Table renders profiles as a plain-text table.
func Table(ps []Profile) string {
	w := [4]int{len("PROVIDER"), len("NAME"), len("LOCATION"), len("STATUS")}
	for _, p := range ps {
		for i, s := range []string{p.Provider, p.Name, p.Location, p.Status} {
			if len(s) > w[i] {
				w[i] = len(s)
			}
		}
	}
	var b strings.Builder
	row := func(a, n, l, s string) {
		b.WriteString(pad(a, w[0]) + "  " + pad(n, w[1]) + "  " + pad(l, w[2]) + "  " + s + "\n")
	}
	row("PROVIDER", "NAME", "LOCATION", "STATUS")
	for _, p := range ps {
		row(p.Provider, p.Name, p.Location, p.Status)
	}
	return b.String()
}
