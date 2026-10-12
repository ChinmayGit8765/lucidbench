package config

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const commented = `# My Lucidbench config.
server:
  addr: "127.0.0.1:7420" # keep the default port

# Infrastructure: start things only when needed.
power:
  cluster: "always" # the kind node
  runner_idle_minutes: 15
  stacks:
    - project: "shop"
      mode: "on-demand"

some_future_key: keep me # unknown here, but the file is the user's
`

func noEnv(string) string { return "" }

func writeFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Saving changes only the keys asked for: comments, order, the stacks list
// and a key Lucidbench does not know all stay, and the old file is backed up.
func TestSaveEditKeepsCommentsAndUnknownKeys(t *testing.T) {
	p := writeFile(t, commented)
	running, _, err := LoadFrom(p, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	e, err := SaveEdit(p, map[string]any{"power.cluster": "on-demand", "power.cluster_idle_minutes": float64(45), "update_check": false}, running, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	s := string(got)
	for _, want := range []string{
		"# My Lucidbench config.",
		`addr: "127.0.0.1:7420" # keep the default port`,
		"# Infrastructure: start things only when needed.",
		`cluster: "on-demand" # the kind node`,
		"cluster_idle_minutes: 45",
		"runner_idle_minutes: 15",
		`- project: "shop"`,
		"some_future_key: keep me # unknown here, but the file is the user's",
		"update_check: false",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, `"always"`) {
		t.Errorf("old value still there:\n%s", s)
	}
	// Only the touched lines changed.
	var removed, added []string
	for _, l := range e.Diff {
		switch l.Op {
		case "-":
			removed = append(removed, strings.TrimSpace(l.Text))
		case "+":
			added = append(added, strings.TrimSpace(l.Text))
		}
	}
	if len(removed) != 1 || removed[0] != `cluster: "always" # the kind node` {
		t.Errorf("removed %q", removed)
	}
	if len(added) != 3 {
		t.Errorf("added %q", added)
	}
	// The backup is the old file, byte for byte.
	if e.Backup == "" {
		t.Fatal("no backup")
	}
	if b, _ := os.ReadFile(e.Backup); string(b) != commented {
		t.Errorf("backup %q", b)
	}
	// It loads, and says what waits for a restart.
	again, _, err := LoadFrom(p, noEnv)
	if err != nil || again.Power.Cluster != PowerOnDemand || again.Power.ClusterIdleMinutes != 45 || again.UpdateCheck || len(again.Power.Stacks) != 1 {
		t.Fatalf("reload: %+v %v", again, err)
	}
	if strings.Join(e.Restart, ",") != "power.cluster,power.cluster_idle_minutes,update_check" {
		t.Errorf("restart %v", e.Restart)
	}
	// Saved again with the same values: nothing to write, no new backup.
	e2, err := SaveEdit(p, map[string]any{"power.cluster": "on-demand"}, running, noEnv)
	if err != nil || e2.Changed || e2.Backup != "" {
		t.Errorf("unchanged save: %+v %v", e2, err)
	}
}

// No file yet: the save creates it with only the keys set.
func TestSaveEditCreatesTheFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.yaml")
	e, err := SaveEdit(p, map[string]any{"power.runners": "off"}, Default(), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "power:\n  runners: \"off\"\n" || !e.Created || e.Backup != "" {
		t.Errorf("created %q %+v", got, e)
	}
}

// A bad value, a key the UI may not write, a literal secret and a file that
// is not YAML are all refused, and the file is left as it was.
func TestSaveEditRefuses(t *testing.T) {
	p := writeFile(t, commented)
	for name, set := range map[string]map[string]any{
		"mode":    {"power.cluster": "sometimes"},
		"int":     {"power.poll_seconds": 2.5},
		"invalid": {"power.poll_seconds": float64(1)},
		"key":     {"server.addr": "0.0.0.0:80"},
		"secret":  {"integrations.linear.token": "lin_api_abc123"},
		"empty":   {},
	} {
		_, err := SaveEdit(p, set, nil, noEnv)
		if err == nil {
			t.Errorf("%s: saved", name)
		}
		if name == "secret" && strings.Contains(err.Error(), "lin_api") {
			t.Errorf("the error echoes the secret: %v", err)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != commented {
		t.Errorf("file changed:\n%s", b)
	}
	if m, _ := filepath.Glob(p + ".bak-*"); len(m) != 0 {
		t.Errorf("backups %v", m)
	}
	broken := writeFile(t, "power: [\n")
	if _, err := SaveEdit(broken, map[string]any{"power.cluster": "off"}, nil, noEnv); err == nil {
		t.Error("broken YAML was rewritten")
	}
}

// A secret is saved as its env: reference, and an environment variable that
// sets a key is reported, since the file's value does not count while set.
func TestSaveEditSecretRefAndOverride(t *testing.T) {
	p := writeFile(t, commented)
	env := func(k string) string {
		if k == "LUCID_POWER_RUNNERS" {
			return "always"
		}
		return ""
	}
	e, err := SaveEdit(p, map[string]any{"integrations.linear.token": "env:MY_LINEAR_KEY", "power.runners": "off"}, nil, env)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "token: \"env:MY_LINEAR_KEY\"") {
		t.Errorf("file:\n%s", got)
	}
	if strings.Join(e.Overridden, ",") != "power.runners" {
		t.Errorf("overridden %v", e.Overridden)
	}
}

// Every line the save did not touch is byte for byte what it was, blank
// lines included; a flow-style block falls back to re-encoding the tree.
func TestSaveEditTouchesOnlyItsLines(t *testing.T) {
	p := writeFile(t, commented)
	if _, err := SaveEdit(p, map[string]any{"power.runner_idle_minutes": float64(30)}, nil, noEnv); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if want := strings.Replace(commented, "runner_idle_minutes: 15", "runner_idle_minutes: 30", 1); string(got) != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	flow := writeFile(t, "# flow\npower: {cluster: \"off\"}\n")
	if _, err := SaveEdit(flow, map[string]any{"power.cluster": "always"}, nil, noEnv); err != nil {
		t.Fatal(err)
	}
	if c, _, err := LoadFrom(flow, noEnv); err != nil || c.Power.Cluster != PowerAlways {
		t.Errorf("flow: %v %v", c, err)
	}
}

func TestPruneBackups(t *testing.T) {
	p := writeFile(t, "power:\n  cluster: \"off\"\n")
	for i, m := range []string{"always", "on-demand", "off", "always", "on-demand", "off", "always"} {
		if _, err := SaveEdit(p, map[string]any{"power.cluster": m}, nil, noEnv); err != nil {
			t.Fatal(i, err)
		}
	}
	if m, _ := filepath.Glob(p + ".bak-*"); len(m) != KeepBackups {
		t.Errorf("%d backups: %v", len(m), m)
	}
}

func TestWriteRoutes(t *testing.T) {
	p := writeFile(t, commented)
	running, _, err := LoadFrom(p, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterWrite(mux, running, noEnv)
	do := func(method, path, body string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	body := `{"set":{"power.cluster":"off"}}`
	// The preview writes nothing.
	if rec := do("POST", "/api/config/preview", body, false); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"op":"+"`) {
		t.Fatalf("preview %d %s", rec.Code, rec.Body)
	}
	if b, _ := os.ReadFile(p); string(b) != commented {
		t.Error("the preview wrote the file")
	}
	if rec := do("POST", "/api/config/save", body, false); rec.Code != http.StatusForbidden {
		t.Errorf("save without confirm: %d", rec.Code)
	}
	if rec := do("POST", "/api/config/save", `{"set":{"power.cluster":"off"},"x":1}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", rec.Code)
	}
	rec := do("POST", "/api/config/save", body, true)
	if rec.Code != 200 {
		t.Fatalf("save %d %s", rec.Code, rec.Body)
	}
	// The file view has the saved value, pending a restart.
	rec = do("GET", "/api/config/file", "", false)
	var v FileView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Values["power.cluster"] != "off" || v.Sources["power.cluster"] != "file" || strings.Join(v.Pending, ",") != "power.cluster" || v.Values["power.runner_idle_minutes"] != float64(15) {
		t.Errorf("file view %+v", v)
	}
}
