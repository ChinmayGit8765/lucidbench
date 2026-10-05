package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// hostFixture is a host ~/.claude with auth plus things that must not leak.
func hostFixture(t *testing.T) string {
	host := filepath.Join(t.TempDir(), ".claude")
	write(t, filepath.Join(host, ".credentials.json"), `{"tok":"a"}`)
	write(t, filepath.Join(host, "settings.json"), `{"hooks":{}}`)
	write(t, filepath.Join(host, "CLAUDE.md"), "rules")
	write(t, filepath.Join(host, "hooks", "h.js"), "x")
	return host
}

func TestStageCopiesOnlyAuth(t *testing.T) {
	data, host := t.TempDir(), hostFixture(t)
	st, err := Stage(data, "claude", host)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Finish()
	if !strings.HasPrefix(st.ConfigDir, filepath.Join(data, "runs")) || filepath.Base(st.ConfigDir) != ".claude" {
		t.Errorf("unexpected config dir %s", st.ConfigDir)
	}
	var got []string
	filepath.WalkDir(st.ConfigDir, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			rel, _ := filepath.Rel(st.ConfigDir, p)
			got = append(got, rel)
		}
		return nil
	})
	if len(got) != 1 || got[0] != ".credentials.json" {
		t.Errorf("staged files = %v, want only .credentials.json", got)
	}
	if entries, _ := os.ReadDir(st.WorkDir); len(entries) != 0 {
		t.Errorf("work dir not empty: %v", entries)
	}
}

func TestStageRequiresAuth(t *testing.T) {
	data, host := t.TempDir(), t.TempDir()
	write(t, filepath.Join(host, "settings.json"), "{}")
	if _, err := Stage(data, "claude", host); err == nil {
		t.Fatal("expected error without credentials")
	}
	if entries, _ := os.ReadDir(filepath.Join(data, "runs")); len(entries) != 0 {
		t.Errorf("failed stage left %d run dirs", len(entries))
	}
}

func TestFinishUnchangedLeavesHostAloneAndCleansUp(t *testing.T) {
	data, host := t.TempDir(), hostFixture(t)
	st, _ := Stage(data, "claude", host)
	cred := filepath.Join(host, ".credentials.json")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(cred, old, old)
	refreshed, err := st.Finish()
	if err != nil || len(refreshed) != 0 {
		t.Fatalf("Finish = %v, %v", refreshed, err)
	}
	fi, _ := os.Stat(cred)
	if !fi.ModTime().Equal(old) {
		t.Error("unchanged auth file was rewritten on the host")
	}
	if _, err := os.Stat(st.RunDir); !os.IsNotExist(err) {
		t.Error("run dir not deleted")
	}
	entries, _ := os.ReadDir(host)
	if len(entries) != 4 {
		t.Errorf("host dir gained or lost files: %v", entries)
	}
}

func TestFinishCopiesRefreshedAuthBack(t *testing.T) {
	data, host := t.TempDir(), hostFixture(t)
	st, _ := Stage(data, "claude", host)
	write(t, filepath.Join(st.ConfigDir, ".credentials.json"), `{"tok":"b"}`)
	write(t, filepath.Join(st.ConfigDir, "junk.json"), "x") // CLI scratch files never go back
	refreshed, err := st.Finish()
	if err != nil || len(refreshed) != 1 {
		t.Fatalf("Finish = %v, %v", refreshed, err)
	}
	if got := read(t, filepath.Join(host, ".credentials.json")); got != `{"tok":"b"}` {
		t.Errorf("host auth = %q", got)
	}
	entries, _ := os.ReadDir(host)
	if len(entries) != 4 {
		t.Errorf("host dir has stray files (temp not cleaned?): %v", entries)
	}
	if _, err := os.Stat(st.RunDir); !os.IsNotExist(err) {
		t.Error("run dir not deleted")
	}
}

func TestFinishKeepsNewerHostLogin(t *testing.T) {
	data, host := t.TempDir(), hostFixture(t)
	st, _ := Stage(data, "claude", host)
	write(t, filepath.Join(st.ConfigDir, ".credentials.json"), `{"tok":"b"}`)
	write(t, filepath.Join(host, ".credentials.json"), `{"tok":"host-new"}`)
	refreshed, err := st.Finish()
	if err == nil || len(refreshed) != 0 {
		t.Fatalf("Finish = %v, %v; want a discard error", refreshed, err)
	}
	if got := read(t, filepath.Join(host, ".credentials.json")); got != `{"tok":"host-new"}` {
		t.Errorf("host login overwritten: %q", got)
	}
}

func TestRunHoldsLockAcrossRunAndCleansUp(t *testing.T) {
	data, home := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".codex", "auth.json"), `{"t":1}`)
	write(t, filepath.Join(home, ".codex", "config.toml"), "mcp")
	o := Options{Provider: "codex", Profile: HostProfile, Prompt: "hi", Home: home, DataDir: data,
		LockPath: LockPathIn(data, "codex", HostProfile)}
	var runDir string
	code, refreshed, err := Run(o, func(args []string) int {
		if _, err := Acquire(o.LockPath); err == nil || !strings.Contains(err.Error(), "in use") {
			t.Errorf("lock not held during run: %v", err)
		}
		joined := strings.Join(args, " ")
		if strings.Contains(joined, filepath.ToSlash(filepath.Join(home, ".codex"))) {
			t.Errorf("host config dir mounted: %v", args)
		}
		entries, _ := os.ReadDir(filepath.Join(data, "runs"))
		if len(entries) != 1 {
			t.Fatalf("want one run dir during run, got %v", entries)
		}
		runDir = filepath.Join(data, "runs", entries[0].Name())
		// the CLI refreshes its token
		write(t, filepath.Join(runDir, "home", ".codex", "auth.json"), `{"t":2}`)
		return 7
	})
	if err != nil || code != 7 || len(refreshed) != 1 {
		t.Fatalf("Run = %d, %v, %v", code, refreshed, err)
	}
	if got := read(t, filepath.Join(home, ".codex", "auth.json")); got != `{"t":2}` {
		t.Errorf("host auth = %q", got)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Error("run dir not deleted")
	}
	l, err := Acquire(o.LockPath)
	if err != nil {
		t.Fatalf("lock not released after run: %v", err)
	}
	l.Release()
}

func TestRunVolumeProfileSkipsStaging(t *testing.T) {
	data := t.TempDir()
	o := Options{Provider: "claude", Profile: "work", Prompt: "hi", Home: t.TempDir(), DataDir: data,
		LockPath: LockPathIn(data, "claude", "work")}
	called := false
	code, _, err := Run(o, func(args []string) int { called = true; return 0 })
	if err != nil || code != 0 || !called {
		t.Fatalf("Run = %d, %v, called=%v", code, err, called)
	}
	if _, err := os.Stat(filepath.Join(data, "runs")); !os.IsNotExist(err) {
		t.Error("volume profile created a runs dir")
	}
}
