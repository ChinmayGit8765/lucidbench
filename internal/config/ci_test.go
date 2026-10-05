package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCIDefaults(t *testing.T) {
	c := Default()
	if c.CI.GitHub.Token != "env:GITHUB_TOKEN" || len(c.CI.GitHub.Repos) != 0 ||
		c.CI.Runners.ComposeProject != "" || c.CI.Runners.ImageMatch != "github-runner" {
		t.Fatalf("ci defaults %+v", c.CI)
	}
}

func TestCIFileThenEnv(t *testing.T) {
	p := write(t, "ci:\n  github:\n    repos: [you/one, you/two]\n    token: env:MY_GH\n  runners:\n    compose_project: ci\n    image_match: runner\n")
	c, warns, err := LoadFrom(p, env(nil))
	if err != nil || len(warns) != 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if !reflect.DeepEqual(c.CI.GitHub.Repos, []string{"you/one", "you/two"}) || c.CI.GitHub.Token != "env:MY_GH" ||
		c.CI.Runners.ComposeProject != "ci" || c.CI.Runners.ImageMatch != "runner" || c.Source("ci.github.token") != "file" {
		t.Fatalf("file values %+v", c.CI)
	}
	c, _, err = LoadFrom(p, env(map[string]string{
		"LUCID_CI_GITHUB_REPOS":            "you/three, you/four",
		"LUCID_CI_GITHUB_TOKEN":            "env:OTHER",
		"LUCID_CI_RUNNERS_COMPOSE_PROJECT": "x",
		"LUCID_CI_RUNNERS_IMAGE_MATCH":     "y",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.CI.GitHub.Repos, []string{"you/three", "you/four"}) || c.CI.GitHub.Token != "env:OTHER" ||
		c.CI.Runners.ComposeProject != "x" || c.CI.Runners.ImageMatch != "y" ||
		c.Source("ci.github.repos") != "env:LUCID_CI_GITHUB_REPOS" {
		t.Fatalf("env values %+v", c.CI)
	}
}

func TestCIRejectsLiteralTokenWithoutEcho(t *testing.T) {
	const lit = "literal-token-value-12345"
	p := write(t, "ci:\n  github:\n    token: "+lit+"\n")
	_, _, err := LoadFrom(p, env(nil))
	if err == nil || !strings.Contains(err.Error(), "ci.github.token") || strings.Contains(err.Error(), lit) {
		t.Fatalf("err = %v", err)
	}
	_, _, err = LoadFrom(filepath.Join(t.TempDir(), "x.yaml"), env(map[string]string{"LUCID_CI_GITHUB_TOKEN": lit}))
	if err == nil || !strings.Contains(err.Error(), "LUCID_CI_GITHUB_TOKEN") || strings.Contains(err.Error(), lit) {
		t.Fatalf("env err = %v", err)
	}
}

func TestCIRejectsBadRepoAndWarnsUnknown(t *testing.T) {
	p := write(t, "ci:\n  github:\n    repos: [not-a-repo]\n")
	_, _, err := LoadFrom(p, env(nil))
	if err == nil || !strings.Contains(err.Error(), "ci.github.repos") {
		t.Fatalf("err = %v", err)
	}
	p = write(t, "ci:\n  bogus: 1\n  github:\n    nope: 2\n")
	_, warns, err := LoadFrom(p, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if j := strings.Join(warns, "\n"); !strings.Contains(j, `"ci.bogus"`) || !strings.Contains(j, `"ci.github.nope"`) {
		t.Errorf("warnings: %s", j)
	}
}
