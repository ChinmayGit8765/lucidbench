package ci

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Container is a local self-hosted runner container. Of the container's
// environment only the repository URL and runner name are kept; every other
// variable (the registration token among them) is dropped while parsing.
type Container struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	State      string `json:"state"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	Project    string `json:"compose_project,omitempty"`
	Service    string `json:"compose_service,omitempty"`
	RepoURL    string `json:"repo_url,omitempty"`
	RunnerName string `json:"runner_name,omitempty"`
}

// Up reports whether the container is running.
func (c Container) Up() bool { return c.State == "running" }

// Filter selects runner containers: those in the compose project, or whose
// image contains ImageMatch. An empty field disables that match, so an empty
// filter matches nothing.
type Filter struct {
	ComposeProject string
	ImageMatch     string
}

// Matches reports whether a container with this compose project and image is
// a runner.
func (f Filter) Matches(project, image string) bool {
	return (f.ComposeProject != "" && project == f.ComposeProject) ||
		(f.ImageMatch != "" && strings.Contains(image, f.ImageMatch))
}

// DockerFunc runs the docker CLI with args and returns its standard output.
type DockerFunc func(ctx context.Context, args ...string) ([]byte, error)

// ExecDocker runs the real docker CLI.
func ExecDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("docker %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("docker %s: %w", args[0], err)
	}
	return out, nil
}

// psLine is one line of `docker ps --format '{{json .}}'`.
type psLine struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Labels string `json:"Labels"`
}

// parseLabels parses docker's "k=v,k=v" label string. Values that contain
// commas are split, which only loses labels this package does not read.
func parseLabels(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// parsePS parses `docker ps -a --no-trunc --format '{{json .}}'` output (one
// JSON object per line) and keeps the containers the filter matches.
func parsePS(out []byte, f Filter) ([]Container, error) {
	var cs []Container
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var p psLine
		if err := json.Unmarshal(line, &p); err != nil {
			return nil, fmt.Errorf("parse docker ps output: %w", err)
		}
		labels := parseLabels(p.Labels)
		project := labels["com.docker.compose.project"]
		if !f.Matches(project, p.Image) {
			continue
		}
		cs = append(cs, Container{
			ID:      p.ID,
			Name:    strings.TrimPrefix(strings.Split(p.Names, ",")[0], "/"),
			Image:   p.Image,
			State:   p.State,
			Status:  p.Status,
			Project: project,
			Service: labels["com.docker.compose.service"],
		})
	}
	return cs, sc.Err()
}

// inspected is the part of `docker inspect` this package reads.
type inspected struct {
	ID    string `json:"Id"`
	State struct {
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
	Config struct {
		Env []string `json:"Env"`
	} `json:"Config"`
}

// keptEnv are the only environment variables ever read from a container.
var keptEnv = map[string]bool{"REPO_URL": true, "RUNNER_NAME": true}

// applyInspect copies the start time, repository URL and runner name from
// `docker inspect` output onto the matching containers. All other
// environment values are discarded here and never stored.
func applyInspect(cs []Container, out []byte) error {
	var docs []inspected
	if err := json.Unmarshal(out, &docs); err != nil {
		return fmt.Errorf("parse docker inspect output: %w", err)
	}
	for _, d := range docs {
		for i := range cs {
			if cs[i].ID == "" || !strings.HasPrefix(d.ID, cs[i].ID) {
				continue
			}
			if !strings.HasPrefix(d.State.StartedAt, "0001-") {
				cs[i].StartedAt = d.State.StartedAt
			}
			for _, kv := range d.Config.Env {
				k, v, _ := strings.Cut(kv, "=")
				if !keptEnv[k] {
					continue
				}
				if k == "REPO_URL" {
					cs[i].RepoURL = v
				} else {
					cs[i].RunnerName = v
				}
			}
		}
	}
	return nil
}

// ListContainers returns the runner containers the filter matches, running
// or not.
func ListContainers(ctx context.Context, docker DockerFunc, f Filter) ([]Container, error) {
	if f.ComposeProject == "" && f.ImageMatch == "" {
		return []Container{}, nil
	}
	out, err := docker(ctx, "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	cs, err := parsePS(out, f)
	if err != nil || len(cs) == 0 {
		if cs == nil {
			cs = []Container{}
		}
		return cs, err
	}
	args := []string{"inspect"}
	for _, c := range cs {
		args = append(args, c.ID)
	}
	out, err = docker(ctx, args...)
	if err != nil {
		// The list is still useful without start times and repo URLs.
		return cs, nil
	}
	return cs, applyInspect(cs, out)
}

// Actions are the container actions the API allows.
var Actions = map[string]bool{"start": true, "stop": true, "restart": true}

// ErrNotRunner is returned for a container the runner filter does not match.
var ErrNotRunner = errors.New("not a runner container")

// ErrBadAction is returned for an action other than start, stop or restart.
var ErrBadAction = errors.New("action must be start, stop or restart")

var containerNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// ContainerAction starts, stops or restarts a runner container by name. The
// name must belong to a container the filter matches right now; anything else
// is refused before docker is asked to act.
func ContainerAction(ctx context.Context, docker DockerFunc, f Filter, name, action string) error {
	if !Actions[action] {
		return ErrBadAction
	}
	if !containerNameRE.MatchString(name) {
		return ErrNotRunner
	}
	cs, err := ListContainers(ctx, docker, f)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.Name == name {
			_, err := docker(ctx, action, name)
			return err
		}
	}
	return ErrNotRunner
}
