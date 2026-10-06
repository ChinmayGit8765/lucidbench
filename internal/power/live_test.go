package power

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// TestLiveThrowawayRunner drives a throwaway container through the real
// docker CLI against the fake GitHub: a queued run starts it, and ten idle
// minutes on the fake clock stop it. It runs only with LUCID_POWER_LIVE=1,
// creates and removes a container named lucid-test-runner in compose
// project lucid-test, and matches nothing else.
func TestLiveThrowawayRunner(t *testing.T) {
	if os.Getenv("LUCID_POWER_LIVE") != "1" {
		t.Skip("set LUCID_POWER_LIVE=1 to run against the local docker engine")
	}
	const name = "lucid-test-runner"
	dockerCLI := func(args ...string) string {
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	state := func() string { return dockerCLI("inspect", "-f", "{{.State.Status}}", name) }
	_ = exec.Command("docker", "rm", "-f", name).Run()
	dockerCLI("run", "-d", "--name", name, "--label", "com.docker.compose.project=lucid-test",
		"-e", "REPO_URL=https://github.com/you/throwaway", "-e", "RUNNER_NAME="+name, "alpine", "sleep", "3600")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	dockerCLI("stop", "-t", "1", name)
	if s := state(); s != "exited" {
		t.Fatalf("setup: %s", s)
	}

	gh := newFakeGitHub(t)
	gh.runners["you/throwaway"] = []string{name}
	gh.queued["you/throwaway"] = []int64{99}
	clk := newClock()
	s := &Supervisor{
		Cfg:         config.PowerConfig{Cluster: "off", ClusterIdleMinutes: 15, Runners: "on-demand", RunnerIdleMinutes: 10, Stacks: []config.PowerStack{}, PollSeconds: 60},
		ClusterName: "lucid-test-no-cluster",
		Docker:      docker.Exec,
		Cluster:     &fakeKube{},
		Runners:     ci.Filter{ComposeProject: "lucid-test"},
		GitHub: &ci.GitHub{BaseURL: gh.srv.URL, TTL: time.Nanosecond, Tokens: &ci.TokenSource{
			Ref: "env:T", Getenv: func(string) string { return testToken },
		}},
		Log:  &ActivityLog{Path: filepath.Join(t.TempDir(), "activity.jsonl")},
		Now:  clk.Now,
		Wait: clk.Wait,
	}
	ctx := context.Background()

	s.Tick(ctx)
	if st := state(); st != "running" {
		t.Fatalf("queued run did not start the runner: %s", st)
	}
	t.Logf("queued run #99 -> %s started", name)

	gh.set(func(g *fakeGitHub) { g.queued["you/throwaway"] = nil })
	s.Tick(ctx) // first idle sighting
	clk.Add(11 * time.Minute)
	s.Tick(ctx)
	if st := state(); st != "exited" {
		t.Fatalf("idle runner not stopped: %s", st)
	}
	es, _ := s.Log.Recent(10)
	for _, e := range es {
		t.Logf("activity: %s %s %s auto=%v ok=%v (%s) %.1fs", e.Kind, e.Name, e.Action, e.Auto, e.OK, e.Reason, e.Seconds)
	}
}
