package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/runner"
	"github.com/ChinmayGit8765/lucidbench/internal/runner/sweeptest"
)

// TestMain lets a test run the real daemon: with LUCID_TEST_MAIN set, the
// test binary is lucidd itself.
func TestMain(m *testing.M) {
	if os.Getenv("LUCID_TEST_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startLucidd starts lucidd on a free port against dataDir, waits until it
// listens, stops it, and returns what it logged up to then.
func startLucidd(t *testing.T, dataDir string) string {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "LUCID_TEST_MAIN=1", "LUCID_DATA_DIR="+dataDir,
		"LUCID_CONFIG="+filepath.Join(home, "config.yaml"), "LUCID_ADDR=127.0.0.1:0",
		"LUCID_SERVER_ADDR=127.0.0.1:0", "HOME="+home, "USERPROFILE="+home)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	var log strings.Builder
	timeout := time.After(30 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("lucidd exited before listening:\n%s", log.String())
			}
			log.WriteString(l + "\n")
			if strings.Contains(l, "listening on") {
				go func() {
					for range lines {
					}
				}()
				return log.String()
			}
		case <-timeout:
			t.Fatalf("lucidd did not start:\n%s", log.String())
		}
	}
}

// D1 at daemon startup.
func TestStartupSweepRemovesStaleRun(t *testing.T) {
	data := t.TempDir()
	dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
	out := startLucidd(t, data)
	if !strings.Contains(out, "runs sweep: removed "+sweeptest.ID) || !strings.Contains(out, "runs sweep: removed 1, kept 0, failed 0") {
		t.Fatalf("lucidd log:\n%s", out)
	}
	sweeptest.AssertGone(t, dir)
}

// D2 at daemon startup.
func TestStartupSweepKeeps(t *testing.T) {
	for _, c := range sweeptest.KeepCases() {
		t.Run(c.Name, func(t *testing.T) {
			data := t.TempDir()
			dir := c.Seed(t, data)
			out := startLucidd(t, data)
			if !strings.Contains(out, "runs sweep: kept "+sweeptest.ID+": ") || !strings.Contains(out, "runs sweep: removed 0, kept 1, failed 0") {
				t.Fatalf("lucidd log:\n%s", out)
			}
			sweeptest.AssertKept(t, dir)
		})
	}
}

// D5: a file held open blocks the delete on Windows; lucidd still starts,
// logs the failure, and a later sweep finishes the job.
func TestStartupSweepRetriesHeldFolder(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("an open file only blocks deletion on Windows")
	}
	data := t.TempDir()
	dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
	held, err := os.Open(sweeptest.AuthFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	out := startLucidd(t, data)
	held.Close()
	if !strings.Contains(out, "runs sweep: failed "+sweeptest.ID) || !strings.Contains(out, "runs sweep: removed 0, kept 0, failed 1") {
		t.Fatalf("lucidd log:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "owner")); err != nil {
		t.Fatalf("failed delete lost the owner record: %v", err)
	}

	res, err := runner.Sweep(data, runner.SweepGrace)
	if err != nil || len(res.Removed) != 1 || len(res.Failed) != 0 {
		t.Fatalf("retry sweep = %+v, %v", res, err)
	}
	sweeptest.AssertGone(t, dir)

	res, err = runner.Sweep(data, runner.SweepGrace)
	if err != nil || len(res.Removed)+len(res.Failed) != 0 {
		t.Fatalf("third sweep = %+v, %v", res, err)
	}
}
