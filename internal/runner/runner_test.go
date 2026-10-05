package runner

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDockerPath(t *testing.T) {
	cases := map[string]string{
		`C:\Users\me\.claude`: "/c/Users/me/.claude",
		`d:/data/x`:           "/d/data/x",
		"/home/me/.codex":     "/home/me/.codex",
	}
	for in, want := range cases {
		if got := DockerPath(in); got != want {
			t.Errorf("DockerPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArgsHost(t *testing.T) {
	st := &Staged{ConfigDir: `C:\data\runs\x\cfg\.claude`, WorkDir: `C:\data\runs\x\work`}
	got, err := Args("claude", HostProfile, "hi", st)
	if err != nil {
		t.Fatal(err)
	}
	if got[4] != "/c/data/runs/x/cfg/.claude:/root/.claude" || got[6] != "/c/data/runs/x/work:/work" {
		t.Errorf("unexpected mounts %q %q", got[4], got[6])
	}
	if got[0] != "run" || got[1] != "--rm" || got[2] != "-i" || got[3] != "-v" || got[5] != "-v" || got[7] != Image {
		t.Errorf("unexpected args %v", got)
	}
	want, _ := Command("claude", "hi")
	if !reflect.DeepEqual(got[8:], want) {
		t.Errorf("unexpected command %v", got[8:])
	}
	if _, err := Args("claude", HostProfile, "hi", nil); err == nil {
		t.Error("host profile without staging must fail")
	}
}

func TestArgsVolume(t *testing.T) {
	got, err := Args("codex", "work", "hi", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[4] != "lucidbench-codex-work:/root/.codex" {
		t.Errorf("unexpected mount %q", got[4])
	}
	if !strings.Contains(strings.Join(got, " "), "--ignore-user-config") {
		t.Errorf("volume profiles must get the clean flags: %v", got)
	}
	if _, err := Args("codex", "../x", "hi", nil); err == nil {
		t.Error("expected invalid profile error")
	}
	if _, err := Args("nope", "work", "hi", nil); err == nil {
		t.Error("expected unknown provider error")
	}
}

func TestCommand(t *testing.T) {
	want := map[string][]string{
		"claude": {"claude", "--safe-mode", "--strict-mcp-config", "--setting-sources", "", "--no-session-persistence", "-p", "p"},
		"codex":  {"codex", "exec", "--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--ignore-rules", "p"},
		"grok":   {"grok", "--no-subagents", "-p", "p"},
	}
	for prov, w := range want {
		got, err := Command(prov, "p")
		if err != nil || !reflect.DeepEqual(got, w) {
			t.Errorf("Command(%s) = %v, %v; want %v", prov, got, err, w)
		}
	}
}

func TestLockAcquireContendRelease(t *testing.T) {
	path := LockPathIn(t.TempDir(), "claude", "host")
	l, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(path); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("expected contention error, got %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(path)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	l2.Release()
}

func TestLockStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "claude-host.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := pidAlive
	pidAlive = func(int) bool { return false }
	defer func() { pidAlive = old }()
	l, err := Acquire(path)
	if err != nil {
		t.Fatalf("stale lock should be replaced: %v", err)
	}
	l.Release()
}
