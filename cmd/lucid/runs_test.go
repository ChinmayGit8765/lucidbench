package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/runner/sweeptest"
)

// TestMain lets a test run the real CLI: with LUCID_TEST_MAIN set, the test
// binary is lucid itself.
func TestMain(m *testing.M) {
	if os.Getenv("LUCID_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// lucid runs the CLI with args against dataDir and returns its output and
// exit code.
func lucid(t *testing.T, dataDir string, args ...string) (string, int) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "LUCID_TEST_MAIN=1", "LUCID_DATA_DIR="+dataDir,
		"LUCID_CONFIG="+filepath.Join(home, "config.yaml"), "HOME="+home, "USERPROFILE="+home)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return string(out), ee.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(out), 0
}

// D1 through the CLI.
func TestRunsSweepRemovesStaleRun(t *testing.T) {
	data := t.TempDir()
	dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
	out, code := lucid(t, data, "runs", "sweep")
	if code != 0 || !strings.Contains(out, "removed "+sweeptest.ID) || !strings.Contains(out, "removed 1, kept 0, failed 0") {
		t.Fatalf("lucid runs sweep = %d:\n%s", code, out)
	}
	sweeptest.AssertGone(t, dir)
}

// D2 through the CLI.
func TestRunsSweepKeeps(t *testing.T) {
	for _, c := range sweeptest.KeepCases() {
		t.Run(c.Name, func(t *testing.T) {
			data := t.TempDir()
			dir := c.Seed(t, data)
			out, code := lucid(t, data, "runs", "sweep")
			if code != 0 || !strings.Contains(out, "kept "+sweeptest.ID+": ") || !strings.Contains(out, "removed 0, kept 1, failed 0") {
				t.Fatalf("lucid runs sweep = %d:\n%s", code, out)
			}
			sweeptest.AssertKept(t, dir)
		})
	}
}

func TestRunsUsage(t *testing.T) {
	if out, code := lucid(t, t.TempDir(), "runs"); code != 2 || !strings.Contains(out, "usage: lucid runs sweep") {
		t.Fatalf("lucid runs = %d:\n%s", code, out)
	}
}
