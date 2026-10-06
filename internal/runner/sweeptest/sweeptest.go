// Package sweeptest seeds run folders and locks for the stale-run sweep
// tests, shared by the runner package and the lucid and lucidd entry points.
package sweeptest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// DeadPID is a PID no running process has: far above Linux's pid_max, and
// OpenProcess rejects it on Windows.
const DeadPID = 1 << 30

// ID is the run ID the seeds use.
const ID = "0123456789abcdef"

// Old is an age well past the sweep's grace period.
const Old = time.Hour

// Run seeds <dataDir>/runs/<id> as Stage does: an auth file, an empty work
// dir and an owner record for pid naming lockName, all aged by age. It
// returns the run dir.
func Run(t testing.TB, dataDir, id string, pid int, lockName string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(dataDir, "runs", id)
	Write(t, AuthFile(dir), `{"tok":"secret"}`)
	if err := os.MkdirAll(filepath.Join(dir, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	owner := fmt.Sprintf("%d\n", pid)
	if lockName != "" {
		owner += lockName + "\n"
	}
	Write(t, filepath.Join(dir, "owner"), owner)
	Age(t, dir, age)
	return dir
}

// AuthFile is the seeded auth file inside a run dir.
func AuthFile(runDir string) string {
	return filepath.Join(runDir, "home", ".claude", ".credentials.json")
}

// Age sets the mtime of a run dir and its owner record to age ago.
func Age(t testing.TB, runDir string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age)
	for _, p := range []string{filepath.Join(runDir, "owner"), runDir} {
		if err := os.Chtimes(p, at, at); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

// Lock writes <dataDir>/locks/<name> with content.
func Lock(t testing.TB, dataDir, name, content string) {
	t.Helper()
	Write(t, filepath.Join(dataDir, "locks", name), content)
}

// Write creates path and its parent dirs.
func Write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Case is a run folder the sweep must keep. Seed returns its run dir.
type Case struct {
	Name string
	Seed func(t testing.TB, dataDir string) string
}

// KeepCases are the failed, live and uncertain conditions that each keep an
// otherwise stale run folder.
func KeepCases() []Case {
	return []Case{
		{"held lock referencing the run", func(t testing.TB, d string) string {
			dir := Run(t, d, ID, DeadPID, "claude-host.lock", Old)
			Lock(t, d, "claude-host.lock", fmt.Sprintf("%d\n", os.Getpid()))
			return dir
		}},
		{"live owner pid", func(t testing.TB, d string) string {
			return Run(t, d, ID, os.Getpid(), "", Old)
		}},
		{"inside grace period", func(t testing.TB, d string) string {
			return Run(t, d, ID, DeadPID, "", time.Minute)
		}},
		{"unreadable lock", func(t testing.TB, d string) string {
			dir := Run(t, d, ID, DeadPID, "", Old)
			if err := os.MkdirAll(filepath.Join(d, "locks", "codex-host.lock"), 0o755); err != nil {
				t.Fatal(err)
			}
			return dir
		}},
		{"lock without a pid", func(t testing.TB, d string) string {
			dir := Run(t, d, ID, DeadPID, "", Old)
			Lock(t, d, "codex-host.lock", "")
			return dir
		}},
		{"unreadable owner record", func(t testing.TB, d string) string {
			dir := Run(t, d, ID, DeadPID, "", Old)
			owner := filepath.Join(dir, "owner")
			if err := os.Remove(owner); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(owner, 0o700); err != nil {
				t.Fatal(err)
			}
			Age(t, dir, Old)
			return dir
		}},
		{"owner record without a pid", func(t testing.TB, d string) string {
			dir := Run(t, d, ID, DeadPID, "", Old)
			Write(t, filepath.Join(dir, "owner"), "garbage\n")
			Age(t, dir, Old)
			return dir
		}},
		{"missing owner record", func(t testing.TB, d string) string {
			dir := Run(t, d, ID, DeadPID, "", Old)
			if err := os.Remove(filepath.Join(dir, "owner")); err != nil {
				t.Fatal(err)
			}
			Age(t, dir, Old)
			return dir
		}},
	}
}

// AssertKept fails t unless runDir and its auth file still exist.
func AssertKept(t testing.TB, runDir string) {
	t.Helper()
	if _, err := os.Stat(AuthFile(runDir)); err != nil {
		t.Errorf("run folder or its auth file was deleted: %v", err)
	}
}

// AssertGone fails t unless runDir is gone.
func AssertGone(t testing.TB, runDir string) {
	t.Helper()
	if _, err := os.Lstat(runDir); !os.IsNotExist(err) {
		t.Errorf("stale run folder %s still exists (%v)", runDir, err)
	}
}

// Outside creates a folder tree outside runs/ for link tests and returns it
// with a snapshot function listing every file and its content.
func Outside(t testing.TB) (string, func() map[string]string) {
	t.Helper()
	dir := t.TempDir()
	Write(t, filepath.Join(dir, "keep.txt"), "outside")
	Write(t, filepath.Join(dir, "sub", "deep.txt"), "deeper")
	return dir, func() map[string]string {
		got := map[string]string{}
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				b, _ := os.ReadFile(p)
				rel, _ := filepath.Rel(dir, p)
				got[rel] = string(b)
			}
			return nil
		})
		return got
	}
}

// LinkDir makes link point at the directory target: a junction on Windows
// (no privilege needed), a symlink elsewhere.
func LinkDir(t testing.TB, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v: %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// Symlink makes a symlink, skipping the test where the OS does not allow it
// (Windows without Developer Mode).
func Symlink(t testing.TB, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("cannot create symlinks here: %v", err)
		}
		t.Fatal(err)
	}
}
