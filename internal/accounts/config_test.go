package accounts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

func providersOf(ps []Profile) map[string]int {
	m := map[string]int{}
	for _, p := range ps {
		m[p.Provider]++
	}
	return m
}

func TestDetectSkipsDisabledProviders(t *testing.T) {
	r := Roots{Home: t.TempDir(), Getenv: func(k string) string {
		if k == "XAI_API_KEY" {
			return "set"
		}
		return ""
	}, Disabled: map[string]bool{"grok": true, "cursor": true}}
	got := providersOf(Detect(r))
	if got["grok"] != 0 || got["cursor"] != 0 {
		t.Errorf("disabled providers detected: %v", got)
	}
	if got["claude"] == 0 || got["codex"] == 0 {
		t.Errorf("enabled providers missing: %v", got)
	}
}

func TestDetectExtraDirs(t *testing.T) {
	extra := filepath.Join(t.TempDir(), "second")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	r := Roots{Home: t.TempDir(), ExtraDirs: map[string][]string{"codex": {extra}}}
	if n := providersOf(Detect(r))["codex"]; n != 2 {
		t.Errorf("codex profiles = %d, want 2", n)
	}
}

func TestFromConfigDisabled(t *testing.T) {
	c := config.Default()
	pc := c.Providers["grok"]
	pc.Enabled = false
	c.Providers["grok"] = pc
	r := FromConfig(c)
	if r.On("grok") || !r.On("claude") {
		t.Errorf("Disabled = %v", r.Disabled)
	}
}
