package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsWhenNoFile(t *testing.T) {
	c, warns, err := LoadFrom(filepath.Join(t.TempDir(), "missing.yaml"), env(nil))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if c.FileFound || c.Server.Addr != ":7420" || c.Cluster.Name != "lucidbench" ||
		c.Agent.Image != "lucidbench/agent:dev" || c.UI.Theme != "dark" || c.Vault.Path != "" {
		t.Fatalf("unexpected defaults %+v", c)
	}
	for _, p := range Providers {
		if !c.ProviderEnabled(p) {
			t.Errorf("%s disabled by default", p)
		}
	}
}

func TestFileThenEnvPrecedence(t *testing.T) {
	p := write(t, "server:\n  addr: \":9000\"\nui:\n  theme: light\nproviders:\n  grok:\n    enabled: false\n")
	c, warns, err := LoadFrom(p, env(map[string]string{"LUCID_UI_THEME": "system"}))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if c.Server.Addr != ":9000" || c.Source("server.addr") != "file" {
		t.Errorf("addr %q from %q", c.Server.Addr, c.Source("server.addr"))
	}
	if c.UI.Theme != "system" || c.Source("ui.theme") != "env:LUCID_UI_THEME" {
		t.Errorf("theme %q from %q", c.UI.Theme, c.Source("ui.theme"))
	}
	if c.ProviderEnabled("grok") || !c.ProviderEnabled("claude") {
		t.Errorf("provider flags wrong: %+v", c.Providers)
	}
}

func TestLegacyEnv(t *testing.T) {
	sep := string(os.PathListSeparator)
	c, _, err := LoadFrom(filepath.Join(t.TempDir(), "x.yaml"), env(map[string]string{
		"LUCID_ADDR":        ":1234",
		"LUCID_CLAUDE_DIRS": "/a" + sep + "/b",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Addr != ":1234" {
		t.Errorf("LUCID_ADDR ignored: %q", c.Server.Addr)
	}
	if !reflect.DeepEqual(c.ExtraDirs("claude"), []string{"/a", "/b"}) {
		t.Errorf("extra dirs %v", c.ExtraDirs("claude"))
	}
}

func TestLegacyClaudeDirsExtendsFile(t *testing.T) {
	p := write(t, "providers:\n  claude:\n    extra_dirs: [/a]\n")
	c, _, err := LoadFrom(p, env(map[string]string{"LUCID_CLAUDE_DIRS": "/a" + string(os.PathListSeparator) + "/c"}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.ExtraDirs("claude"), []string{"/a", "/c"}) {
		t.Errorf("extra dirs %v", c.ExtraDirs("claude"))
	}
}

func TestUnknownKeysWarn(t *testing.T) {
	p := write(t, "bogus: 1\nserver:\n  addr: \":1\"\n  nope: x\nproviders:\n  gemini:\n    enabled: true\n")
	c, warns, err := LoadFrom(p, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Addr != ":1" {
		t.Errorf("known key lost")
	}
	joined := strings.Join(warns, "\n")
	for _, want := range []string{`"bogus"`, `"server.nope"`, `"providers.gemini"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("no warning for %s in:\n%s", want, joined)
		}
	}
}

func TestInvalidValuesNameFileAndKey(t *testing.T) {
	for name, tc := range map[string]struct{ body, key string }{
		"theme":   {"ui:\n  theme: neon\n", "ui.theme"},
		"addr":    {"server:\n  addr: nonsense\n", "server.addr"},
		"cluster": {"cluster:\n  name: Bad_Name\n", "cluster.name"},
		"enabled": {"providers:\n  claude:\n    enabled: maybe\n", "providers.claude.enabled"},
		"list":    {"providers:\n  codex:\n    extra_dirs: nope\n", "providers.codex.extra_dirs"},
		"image":   {"agent:\n  image: \"a b\"\n", "agent.image"},
		"shape":   {"server: [1]\n", "server"},
	} {
		t.Run(name, func(t *testing.T) {
			p := write(t, tc.body)
			_, _, err := LoadFrom(p, env(nil))
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), p) || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("error should name file and key %s: %v", tc.key, err)
			}
		})
	}
}

func TestInvalidEnvNamesVariable(t *testing.T) {
	_, _, err := LoadFrom(filepath.Join(t.TempDir(), "x.yaml"), env(map[string]string{"LUCID_UI_THEME": "neon"}))
	if err == nil || !strings.Contains(err.Error(), "LUCID_UI_THEME") || !strings.Contains(err.Error(), "ui.theme") {
		t.Fatalf("err = %v", err)
	}
	_, _, err = LoadFrom(filepath.Join(t.TempDir(), "x.yaml"), env(map[string]string{"LUCID_PROVIDERS_GROK_ENABLED": "perhaps"}))
	if err == nil || !strings.Contains(err.Error(), "LUCID_PROVIDERS_GROK_ENABLED") {
		t.Fatalf("err = %v", err)
	}
}

func TestPaths(t *testing.T) {
	ucd := func() (string, error) { return filepath.Join("base", "cfg"), nil }
	p, _ := configPath(env(nil), ucd)
	if p != filepath.Join("base", "cfg", "lucidbench", "config.yaml") {
		t.Errorf("config path %q", p)
	}
	p, _ = configPath(env(map[string]string{"LUCID_CONFIG": "/x/c.yaml"}), ucd)
	if p != "/x/c.yaml" {
		t.Errorf("config path override %q", p)
	}
	d, _ := dataDir(env(nil), ucd)
	if d != filepath.Join("base", "cfg", "lucidbench") {
		t.Errorf("data dir %q", d)
	}
	d, _ = dataDir(env(map[string]string{"LUCID_DATA_DIR": "/data"}), ucd)
	if d != "/data" {
		t.Errorf("data dir override %q", d)
	}
}

func TestTemplateMatchesDefaultsAndExample(t *testing.T) {
	c, warns, err := LoadFrom(write(t, Template), env(nil))
	if err != nil || len(warns) != 0 {
		t.Fatalf("template does not load cleanly: err=%v warns=%v", err, warns)
	}
	d := Default()
	c.File, c.FileFound, c.sources = "", false, nil
	d.File, d.sources = "", nil
	if !reflect.DeepEqual(c, d) {
		t.Errorf("template values differ from defaults:\n%+v\n%+v", c, d)
	}
	ex, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Skip("config.example.yaml not found")
	}
	if strings.ReplaceAll(string(ex), "\r\n", "\n") != Template {
		t.Error("config.example.yaml differs from config.Template; keep them identical")
	}
}

func TestHandler(t *testing.T) {
	c := Default()
	rec := httptest.NewRecorder()
	Handler(c, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"theme":"dark"`) || !strings.Contains(rec.Body.String(), `"sources"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	Handler(c, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/config", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d", rec.Code)
	}
}
