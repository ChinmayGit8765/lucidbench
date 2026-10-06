package runner

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/runner/sweeptest"
)

func sweep(t *testing.T, data string) SweepResult {
	t.Helper()
	res, err := Sweep(data, SweepGrace)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSweepRemovesStaleRun(t *testing.T) {
	data := t.TempDir()
	dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "claude-host.lock", sweeptest.Old)
	// A lock left by the same crash names the run but its holder is dead.
	sweeptest.Lock(t, data, "claude-host.lock", fmt.Sprintf("%d\n", sweeptest.DeadPID))
	res := sweep(t, data)
	if len(res.Removed) != 1 || res.Removed[0] != sweeptest.ID || len(res.Kept)+len(res.Failed) != 0 {
		t.Fatalf("result = %+v", res)
	}
	sweeptest.AssertGone(t, dir)
	if lines := res.Lines(); lines[len(lines)-1] != "removed 1, kept 0, failed 0" {
		t.Errorf("summary = %q", lines[len(lines)-1])
	}
	if _, err := os.Stat(filepath.Join(data, "locks", "claude-host.lock")); err != nil {
		t.Errorf("sweep touched locks/: %v", err)
	}
	if res := sweep(t, data); len(res.Removed)+len(res.Kept)+len(res.Failed) != 0 {
		t.Errorf("second sweep = %+v, want nothing to do", res)
	}
}

func TestSweepKeeps(t *testing.T) {
	for _, c := range sweeptest.KeepCases() {
		t.Run(c.Name, func(t *testing.T) {
			data := t.TempDir()
			dir := c.Seed(t, data)
			res := sweep(t, data)
			if len(res.Removed) != 0 || len(res.Kept) != 1 {
				t.Fatalf("result = %+v", res)
			}
			t.Logf("kept: %s", res.Kept[0].Reason)
			sweeptest.AssertKept(t, dir)
		})
	}
}

func TestSweepKeepsWhenPidCheckFails(t *testing.T) {
	old := pidState
	pidState = func(int) (bool, error) { return false, errors.New("boom") }
	defer func() { pidState = old }()
	data := t.TempDir()
	dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
	res := sweep(t, data)
	if len(res.Kept) != 1 || !strings.Contains(res.Kept[0].Reason, "boom") {
		t.Fatalf("result = %+v", res)
	}
	sweeptest.AssertKept(t, dir)
}

func TestSweepKeepsWhenLockPidCheckFails(t *testing.T) {
	old := pidState
	pidState = func(pid int) (bool, error) {
		if pid == 4242 {
			return false, errors.New("boom")
		}
		return false, nil
	}
	defer func() { pidState = old }()
	data := t.TempDir()
	dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
	sweeptest.Lock(t, data, "grok-host.lock", "4242\n")
	if res := sweep(t, data); len(res.Kept) != 1 {
		t.Fatalf("result = %+v", res)
	}
	sweeptest.AssertKept(t, dir)
}

func TestSweepIgnoresForeignEntries(t *testing.T) {
	data := t.TempDir()
	sweeptest.Write(t, filepath.Join(data, "runs", "notes.txt"), "x")
	sweeptest.Write(t, filepath.Join(data, "runs", "NOT-A-RUN-ID", "f"), "x")
	if res := sweep(t, data); len(res.Kept) != 2 || len(res.Removed) != 0 {
		t.Fatalf("result = %+v", res)
	}
}

func TestSweepWithoutRunsDir(t *testing.T) {
	res, err := Sweep(t.TempDir(), SweepGrace)
	if err != nil || len(res.Removed)+len(res.Kept)+len(res.Failed) != 0 {
		t.Fatalf("Sweep = %+v, %v", res, err)
	}
}

func TestSweepRefusesLinkedRunsDir(t *testing.T) {
	data := t.TempDir()
	outside, snap := sweeptest.Outside(t)
	sweeptest.Run(t, outside, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
	want := snap()
	sweeptest.LinkDir(t, filepath.Join(outside, "runs"), filepath.Join(data, "runs"))
	t.Cleanup(func() { os.Remove(filepath.Join(data, "runs")) })
	if _, err := Sweep(data, SweepGrace); err == nil {
		t.Error("sweep went through a linked runs/")
	}
	if got := snap(); !maps.Equal(got, want) {
		t.Errorf("outside tree changed: %v", got)
	}
}

// D3: links inside a stale run are removed, never followed.
func TestSweepNeverFollowsLinks(t *testing.T) {
	plant := map[string]func(t *testing.T, outside, runDir string){
		"junction": func(t *testing.T, outside, runDir string) {
			sweeptest.LinkDir(t, outside, filepath.Join(runDir, "work", "escape"))
		},
		"nested symlink": func(t *testing.T, outside, runDir string) {
			sweeptest.Symlink(t, outside, filepath.Join(runDir, "home", ".claude", "deep", "escape"))
		},
		"file symlink": func(t *testing.T, outside, runDir string) {
			sweeptest.Symlink(t, filepath.Join(outside, "keep.txt"), filepath.Join(runDir, "home", "f"))
		},
	}
	for name, p := range plant {
		t.Run(name, func(t *testing.T) {
			data := t.TempDir()
			outside, snap := sweeptest.Outside(t)
			want := snap()
			dir := sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
			if err := os.MkdirAll(filepath.Join(dir, "home", ".claude", "deep"), 0o700); err != nil {
				t.Fatal(err)
			}
			p(t, outside, dir)
			sweeptest.Age(t, dir, sweeptest.Old)
			res := sweep(t, data)
			if got := snap(); !maps.Equal(got, want) {
				t.Fatalf("outside tree changed: %v", got)
			}
			if len(res.Removed) != 1 {
				t.Fatalf("result = %+v", res)
			}
			sweeptest.AssertGone(t, dir)
		})
	}
}

// D4: a swap after the root is open cannot redirect the delete.
func TestSweepSwapCannotRedirect(t *testing.T) {
	cases := map[string]func(t *testing.T, data, outside, id string){
		"run folder becomes a junction": func(t *testing.T, data, outside, id string) {
			runDir := filepath.Join(data, "runs", id)
			if err := os.Rename(runDir, filepath.Join(data, "moved")); err != nil {
				t.Fatal(err)
			}
			sweeptest.LinkDir(t, outside, runDir)
			t.Cleanup(func() { os.Remove(runDir) })
		},
		"runs dir becomes a junction": func(t *testing.T, data, outside, id string) {
			runs := filepath.Join(data, "runs")
			if err := os.Rename(runs, filepath.Join(data, "runs-old")); err != nil {
				t.Logf("swap refused while the root is open: %v", err)
				return
			}
			sweeptest.LinkDir(t, outside, runs)
			t.Cleanup(func() { os.Remove(runs) })
			t.Log("runs/ swapped for a junction")
		},
	}
	for name, swap := range cases {
		t.Run(name, func(t *testing.T) {
			data := t.TempDir()
			outside, snap := sweeptest.Outside(t)
			// The outside tree is shaped like both a run folder and a runs/
			// dir, so a redirected delete would find what it expects.
			dead := fmt.Sprintf("%d\n", sweeptest.DeadPID)
			for _, d := range []string{outside, filepath.Join(outside, sweeptest.ID)} {
				sweeptest.Write(t, filepath.Join(d, "owner"), dead)
				sweeptest.Write(t, sweeptest.AuthFile(d), "secret")
			}
			want := snap()
			sweeptest.Run(t, data, sweeptest.ID, sweeptest.DeadPID, "", sweeptest.Old)
			sweepBeforeRemove = func(id string) { swap(t, data, outside, id) }
			defer func() { sweepBeforeRemove = nil }()
			res, err := Sweep(data, SweepGrace)
			t.Logf("result = %+v, err = %v", res, err)
			if got := snap(); !maps.Equal(got, want) {
				t.Fatalf("outside tree changed:\n got %v\nwant %v", got, want)
			}
		})
	}
}

// D6: runs started while sweeps loop are never deleted from under the run,
// even with no grace period at all.
func TestSweepNeverDeletesLiveRun(t *testing.T) {
	data, home := t.TempDir(), t.TempDir()
	sweeptest.Write(t, filepath.Join(home, ".codex", "auth.json"), `{"t":1}`)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var removed []string
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			res, err := Sweep(data, 0)
			if err != nil {
				continue
			}
			mu.Lock()
			removed = append(removed, res.Removed...)
			mu.Unlock()
			time.Sleep(time.Millisecond)
		}
	}()
	o := Options{Provider: "codex", Profile: HostProfile, Prompt: "hi", Home: home, DataDir: data,
		LockPath: filepath.Join(data, "locks", "codex-host.lock")}
	for i := 0; i < 25; i++ {
		// On Windows a sweep reading the lock can make Release fail; the lock
		// then names this (live) test process. Clear it so the next run starts.
		if err := os.Remove(o.LockPath); err == nil {
			t.Logf("run %d: cleared a lock the previous release left behind", i)
		}
		_, _, err := Run(o, func([]string) int {
			entries, err := os.ReadDir(filepath.Join(data, "runs"))
			if err != nil {
				t.Errorf("run %d: %v", i, err)
				return 1
			}
			var auth string
			for _, e := range entries {
				p := filepath.Join(data, "runs", e.Name(), "home", ".codex", "auth.json")
				if _, err := os.Stat(p); err == nil {
					auth = p
				}
			}
			if auth == "" {
				t.Errorf("run %d: no staged auth file", i)
				return 1
			}
			for j := 0; j < 10; j++ {
				if _, err := os.Stat(auth); err != nil {
					t.Errorf("run %d: live run's auth file vanished: %v", i, err)
					return 1
				}
				time.Sleep(time.Millisecond)
			}
			return 0
		})
		if err != nil {
			t.Errorf("run %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
	if len(removed) != 0 {
		t.Errorf("sweeps removed live runs: %v", removed)
	}
}
