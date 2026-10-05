// Package docker reads the local Docker engine through the docker CLI:
// containers grouped by compose project, live stats, logs, images and
// volumes. It can start, stop and restart a container only when a Policy
// allows it.
//
// Labels, mounts and environment values are never returned: they can carry
// host paths and secrets. Of the labels, only the compose project and service
// and the kind cluster name are read.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Func runs the docker CLI with args and returns its standard output.
type Func func(ctx context.Context, args ...string) ([]byte, error)

// Exec runs the real docker CLI.
func Exec(ctx context.Context, args ...string) ([]byte, error) {
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

// ParseLabels parses docker's "k=v,k=v" label string. Values that contain
// commas are split, which only loses labels this package does not read.
func ParseLabels(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// Label keys read from containers.
const (
	LabelProject     = "com.docker.compose.project"
	LabelService     = "com.docker.compose.service"
	LabelKindCluster = "io.x-k8s.kind.cluster"
)

// Container is one container, running or not.
type Container struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Image       string   `json:"image"`
	State       string   `json:"state"`
	Status      string   `json:"status"`
	Ports       string   `json:"ports,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	StartedAt   string   `json:"started_at,omitempty"`
	Project     string   `json:"compose_project,omitempty"`
	Service     string   `json:"compose_service,omitempty"`
	KindCluster string   `json:"kind_cluster,omitempty"`
	Actions     []string `json:"actions"`
}

// Group is the containers of one compose project. Containers outside compose
// share the group with an empty project; kind nodes share one per cluster.
type Group struct {
	Project    string      `json:"project"`
	Kind       string      `json:"kind"` // compose | kind | standalone
	Containers []Container `json:"containers"`
}

type psLine struct {
	ID        string `json:"ID"`
	Names     string `json:"Names"`
	Image     string `json:"Image"`
	State     string `json:"State"`
	Status    string `json:"Status"`
	Ports     string `json:"Ports"`
	CreatedAt string `json:"CreatedAt"`
	Labels    string `json:"Labels"`
}

// jsonLines decodes one JSON object per line into fn.
func jsonLines(out []byte, fn func([]byte) error) error {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(line); err != nil {
			return err
		}
	}
	return sc.Err()
}

// ParsePS parses `docker ps -a --no-trunc --format '{{json .}}'`.
func ParsePS(out []byte) ([]Container, error) {
	cs := []Container{}
	err := jsonLines(out, func(line []byte) error {
		var p psLine
		if err := json.Unmarshal(line, &p); err != nil {
			return fmt.Errorf("parse docker ps output: %w", err)
		}
		l := ParseLabels(p.Labels)
		cs = append(cs, Container{
			ID:          p.ID,
			Name:        strings.TrimPrefix(strings.Split(p.Names, ",")[0], "/"),
			Image:       p.Image,
			State:       p.State,
			Status:      p.Status,
			Ports:       p.Ports,
			CreatedAt:   p.CreatedAt,
			Project:     l[LabelProject],
			Service:     l[LabelService],
			KindCluster: l[LabelKindCluster],
			Actions:     []string{},
		})
		return nil
	})
	return cs, err
}

// inspected is the only part of `docker inspect` this package reads.
type inspected struct {
	ID    string `json:"Id"`
	State struct {
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
}

func applyStarted(cs []Container, out []byte) error {
	var docs []inspected
	if err := json.Unmarshal(out, &docs); err != nil {
		return fmt.Errorf("parse docker inspect output: %w", err)
	}
	for _, d := range docs {
		for i := range cs {
			if cs[i].ID != "" && strings.HasPrefix(d.ID, cs[i].ID) && !strings.HasPrefix(d.State.StartedAt, "0001-") {
				cs[i].StartedAt = d.State.StartedAt
			}
		}
	}
	return nil
}

// Policy decides which containers the API may act on.
type Policy struct {
	// Projects are compose projects whose containers may be started,
	// stopped and restarted.
	Projects []string
	// IsRunner reports whether a container is a CI runner (the Runners & CI
	// filter); runners may be started, stopped and restarted.
	IsRunner func(project, image string) bool
}

// Allowed returns the actions the policy allows on c, in display order.
func (p Policy) Allowed(c Container) []string {
	if c.KindCluster != "" {
		return []string{"restart"}
	}
	ok := c.Project != "" && slices.Contains(p.Projects, c.Project)
	ok = ok || (p.IsRunner != nil && p.IsRunner(c.Project, c.Image))
	if !ok {
		return []string{}
	}
	if c.State == "running" {
		return []string{"stop", "restart"}
	}
	return []string{"start"}
}

// List returns every container with its start time and allowed actions.
func List(ctx context.Context, docker Func, p Policy) ([]Container, error) {
	out, err := docker(ctx, "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	cs, err := ParsePS(out)
	if err != nil {
		return nil, err
	}
	// Start times matter only for running containers; inspecting just those
	// keeps the call fast on machines with many stopped ones.
	args := []string{"inspect"}
	for _, c := range cs {
		if c.State == "running" {
			args = append(args, c.ID)
		}
	}
	if len(args) > 1 {
		// Start times are a nicety; the list stands without them.
		if out, err := docker(ctx, args...); err == nil {
			_ = applyStarted(cs, out)
		}
	}
	for i := range cs {
		cs[i].Actions = p.Allowed(cs[i])
	}
	return cs, nil
}

// GroupContainers groups containers by compose project (kind nodes by
// cluster), compose projects first, alphabetically; containers by name.
func GroupContainers(cs []Container) []Group {
	idx := map[string]int{}
	gs := []Group{}
	for _, c := range cs {
		kind, key := "standalone", ""
		switch {
		case c.KindCluster != "":
			kind, key = "kind", c.KindCluster
		case c.Project != "":
			kind, key = "compose", c.Project
		}
		k := kind + "/" + key
		i, ok := idx[k]
		if !ok {
			i = len(gs)
			idx[k] = i
			gs = append(gs, Group{Project: key, Kind: kind, Containers: []Container{}})
		}
		gs[i].Containers = append(gs[i].Containers, c)
	}
	rank := map[string]int{"compose": 0, "kind": 1, "standalone": 2}
	sort.SliceStable(gs, func(a, b int) bool {
		if rank[gs[a].Kind] != rank[gs[b].Kind] {
			return rank[gs[a].Kind] < rank[gs[b].Kind]
		}
		return gs[a].Project < gs[b].Project
	})
	for _, g := range gs {
		sort.SliceStable(g.Containers, func(a, b int) bool { return g.Containers[a].Name < g.Containers[b].Name })
	}
	return gs
}

// Stat is one line of `docker stats --no-stream`.
type Stat struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	CPUPerc  string `json:"cpu"`
	MemUsage string `json:"mem_usage"`
	MemPerc  string `json:"mem"`
	NetIO    string `json:"net_io"`
	BlockIO  string `json:"block_io"`
	PIDs     string `json:"pids"`
}

// Stats samples resource use of the running containers once.
func Stats(ctx context.Context, docker Func) ([]Stat, error) {
	out, err := docker(ctx, "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	ss := []Stat{}
	err = jsonLines(out, func(line []byte) error {
		var s struct {
			ID, Name, CPUPerc, MemUsage, MemPerc, NetIO, BlockIO, PIDs string
		}
		if err := json.Unmarshal(line, &s); err != nil {
			return fmt.Errorf("parse docker stats output: %w", err)
		}
		ss = append(ss, Stat{s.ID, s.Name, s.CPUPerc, s.MemUsage, s.MemPerc, s.NetIO, s.BlockIO, s.PIDs})
		return nil
	})
	return ss, err
}

// Image is one local image.
type Image struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	CreatedAt  string `json:"created_at"`
	Created    string `json:"created_since"`
}

// Images lists local images.
func Images(ctx context.Context, docker Func) ([]Image, error) {
	out, err := docker(ctx, "images", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	is := []Image{}
	err = jsonLines(out, func(line []byte) error {
		var i struct{ ID, Repository, Tag, Size, CreatedAt, CreatedSince string }
		if err := json.Unmarshal(line, &i); err != nil {
			return fmt.Errorf("parse docker images output: %w", err)
		}
		is = append(is, Image{i.ID, i.Repository, i.Tag, i.Size, i.CreatedAt, i.CreatedSince})
		return nil
	})
	return is, err
}

// Volume is one volume. Mount points and labels are not returned.
type Volume struct {
	Name      string `json:"name"`
	Driver    string `json:"driver"`
	Scope     string `json:"scope"`
	Project   string `json:"compose_project,omitempty"`
	Anonymous bool   `json:"anonymous"`
}

// Volumes lists volumes.
func Volumes(ctx context.Context, docker Func) ([]Volume, error) {
	out, err := docker(ctx, "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	vs := []Volume{}
	err = jsonLines(out, func(line []byte) error {
		var v struct{ Name, Driver, Scope, Labels string }
		if err := json.Unmarshal(line, &v); err != nil {
			return fmt.Errorf("parse docker volume output: %w", err)
		}
		l := ParseLabels(v.Labels)
		_, anon := l["com.docker.volume.anonymous"]
		vs = append(vs, Volume{Name: v.Name, Driver: v.Driver, Scope: v.Scope, Project: l[LabelProject], Anonymous: anon})
		return nil
	})
	return vs, err
}

// NameRE matches a valid container name.
var NameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Errors returned by Act.
var (
	ErrBadAction = errors.New("action must be start, stop or restart")
	ErrRefused   = errors.New("this container is not one Lucidbench may control")
	ErrNotFound  = errors.New("no such container")
)

// Act starts, stops or restarts a container by name, after checking that the
// container exists right now and that the policy allows the action on it.
func Act(ctx context.Context, docker Func, p Policy, name, action string) error {
	if action != "start" && action != "stop" && action != "restart" {
		return ErrBadAction
	}
	if !NameRE.MatchString(name) {
		return ErrNotFound
	}
	cs, err := List(ctx, docker, p)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if c.Name != name {
			continue
		}
		if !slices.Contains(c.Actions, action) {
			return ErrRefused
		}
		_, err := docker(ctx, action, name)
		return err
	}
	return ErrNotFound
}

// LogArgs are the docker arguments for a log tail of n lines, optionally
// following new output.
func LogArgs(name string, tail int, follow bool) []string {
	args := []string{"logs", "--timestamps", "--tail", fmt.Sprint(tail)}
	if follow {
		args = append(args, "--follow")
	}
	return append(args, name)
}

// Logs returns the last tail lines of a container's output, stdout and
// stderr interleaved as docker reports them.
func Logs(ctx context.Context, name string, tail int) ([]byte, error) {
	if !NameRE.MatchString(name) {
		return nil, ErrNotFound
	}
	out, err := exec.CommandContext(ctx, "docker", LogArgs(name, tail, false)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "No such container") {
			return nil, ErrNotFound
		}
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("docker logs: %s", msg)
	}
	return out, nil
}
