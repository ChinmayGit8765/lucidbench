package config

import (
	"strings"
	"testing"
)

func TestPowerDefaults(t *testing.T) {
	p := Default().Power
	if p.Cluster != "on-demand" || p.ClusterIdleMinutes != 15 || p.Runners != "always" ||
		p.RunnerIdleMinutes != 10 || p.PollSeconds != 60 || p.Stacks == nil || len(p.Stacks) != 0 {
		t.Fatalf("defaults %+v", p)
	}
}

func TestPowerFileAndEnv(t *testing.T) {
	p := write(t, `power:
  cluster: off
  cluster_idle_minutes: 2
  runners: on-demand
  runner_idle_minutes: 5
  poll_seconds: 15
  stacks:
    - project: shop
      mode: always
    - project: my-db
`)
	c, warns, err := LoadFrom(p, env(map[string]string{"LUCID_POWER_CLUSTER": "on-demand"}))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	w := c.Power
	if w.Cluster != "on-demand" || c.Source("power.cluster") != "env:LUCID_POWER_CLUSTER" {
		t.Errorf("cluster %q from %q", w.Cluster, c.Source("power.cluster"))
	}
	if w.ClusterIdleMinutes != 2 || w.Runners != "on-demand" || w.RunnerIdleMinutes != 5 || w.PollSeconds != 15 {
		t.Errorf("power %+v", w)
	}
	if len(w.Stacks) != 2 || w.Stacks[0] != (PowerStack{"shop", "always"}) || w.Stacks[1] != (PowerStack{"my-db", "on-demand"}) {
		t.Errorf("stacks %+v", w.Stacks)
	}
	if m, ok := c.StackMode("my-db"); !ok || m != "on-demand" {
		t.Errorf("StackMode %q %v", m, ok)
	}
	if _, ok := c.StackMode("other"); ok {
		t.Error("unlisted project has a mode")
	}
	if v := c.value("power.stacks"); v != "[shop (always), my-db (on-demand)]" {
		t.Errorf("value %q", v)
	}
}

func TestPowerInvalid(t *testing.T) {
	for body, want := range map[string]string{
		"power:\n  cluster: sometimes\n":                          "power.cluster: must be one of always, on-demand, off",
		"power:\n  runners: 3\n":                                  "power.runners: must be one of",
		"power:\n  cluster_idle_minutes: 0\n":                     "power.cluster_idle_minutes: must be between 1",
		"power:\n  runner_idle_minutes: ten\n":                    "power.runner_idle_minutes: must be a whole number",
		"power:\n  poll_seconds: 5\n":                             "power.poll_seconds: must be between 10 and 3600",
		"power:\n  stacks: shop\n":                                "power.stacks: must be a list",
		"power:\n  stacks:\n    - project: Shop\n":                `power.stacks: project "Shop" must be a compose project name`,
		"power:\n  stacks:\n    - project: a\n    - project: a\n": `project "a" is listed twice`,
		"power:\n  stacks:\n    - project: a\n      mode: on\n":   `mode of "a" must be one of`,
	} {
		_, _, err := LoadFrom(write(t, body), env(nil))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", body, err, want)
		}
	}
}
