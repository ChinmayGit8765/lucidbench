package main

// The fake docker engine. It exists only for a spec that wrote
// $LUCID_E2E_STATE/docker.json (the containers, images and volumes it holds);
// without that file every call keeps failing as "no engine", which is what
// the other specs rely on. start, stop, restart and `run -d` change the file,
// so a later `ps` shows them, and every call is in calls.log as usual.
//
// `run -d -p 127.0.0.1:P:C` also starts a detached copy of this binary
// (`docker __serve P NAME`) that answers HTTP on P until the container is
// stopped, so a manager's readiness check can pass without a real image.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type fakeContainer struct {
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	State     string   `json:"state"`
	Status    string   `json:"status,omitempty"`
	Labels    string   `json:"labels,omitempty"`
	Env       []string `json:"env,omitempty"`
	HostPort  int      `json:"host_port,omitempty"`
	CtrPort   int      `json:"ctr_port,omitempty"`
	MemBytes  int64    `json:"mem_bytes,omitempty"`
	StartedAt string   `json:"started_at,omitempty"`
}

type fakeImage struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
}

type fakeVolume struct {
	Name   string `json:"name"`
	Labels string `json:"labels,omitempty"`
}

type dockerState struct {
	Containers []fakeContainer `json:"containers"`
	Images     []fakeImage     `json:"images"`
	Volumes    []fakeVolume    `json:"volumes"`
}

func dockerFile() string { return filepath.Join(stateDir(), "docker.json") }

func loadDocker() (*dockerState, bool) {
	if stateDir() == "" {
		return nil, false
	}
	b, err := os.ReadFile(dockerFile())
	if err != nil {
		return nil, false
	}
	var s dockerState
	if json.Unmarshal(b, &s) != nil {
		return nil, false
	}
	return &s, true
}

func (s *dockerState) save() {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(dockerFile(), b, 0o600)
}

func (s *dockerState) find(name string) *fakeContainer {
	for i := range s.Containers {
		if s.Containers[i].Name == name {
			return &s.Containers[i]
		}
	}
	return nil
}

func cid(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:])
}

func (c fakeContainer) ports() string {
	if c.HostPort == 0 || c.State != "running" {
		return ""
	}
	return fmt.Sprintf("127.0.0.1:%d->%d/tcp", c.HostPort, c.CtrPort)
}

func (c fakeContainer) status() string {
	if c.Status != "" && c.State == "running" {
		return c.Status
	}
	if c.State == "running" {
		return "Up 5 minutes"
	}
	return "Exited (0) 2 minutes ago"
}

// dockerCmd answers one docker call from the fake engine.
func dockerCmd(s *dockerState, args []string) int {
	if len(args) == 0 {
		return 0
	}
	switch args[0] {
	case "version":
		// The daemon's "is the engine up" check: a running engine answers.
		fmt.Println("27.0.0-fake")
		return 0
	case "ps":
		filterName, filterLabel := "", ""
		all := false
		for i := 1; i < len(args); i++ {
			switch {
			case args[i] == "-a":
				all = true
			case args[i] == "--filter" && i+1 < len(args):
				i++
				k, v, _ := strings.Cut(args[i], "=")
				if k == "name" {
					filterName = strings.TrimSuffix(strings.TrimPrefix(v, "^"), "$")
				} else if k == "label" {
					filterLabel = v
				}
			}
		}
		for _, c := range s.Containers {
			if !all && c.State != "running" {
				continue
			}
			if filterName != "" && c.Name != filterName {
				continue
			}
			if filterLabel != "" && !strings.Contains(c.Labels, filterLabel) {
				continue
			}
			out(m{"ID": cid(c.Name), "Names": c.Name, "Image": c.Image, "State": c.State, "Status": c.status(), "Ports": c.ports(),
				"CreatedAt": "2026-10-01 09:00:00 +0000 UTC", "Labels": c.Labels})
		}
	case "inspect":
		var docs []any
		for _, a := range args[1:] {
			for _, c := range s.Containers {
				if cid(c.Name) == a || c.Name == a {
					started := c.StartedAt
					if started == "" {
						started = time.Now().Add(-5 * time.Minute).UTC().Format(time.RFC3339)
					}
					docs = append(docs, m{"Id": cid(c.Name), "State": m{"StartedAt": started}, "Config": m{"Env": c.Env}})
				}
			}
		}
		out(docs)
	case "port":
		if len(args) >= 2 {
			if c := s.find(args[1]); c != nil && c.State == "running" && c.HostPort != 0 {
				fmt.Printf("127.0.0.1:%d\n", c.HostPort)
			}
		}
	case "start", "stop", "restart":
		if len(args) < 2 {
			return 1
		}
		c := s.find(args[1])
		if c == nil {
			fmt.Fprintln(os.Stderr, "Error response from daemon: No such container: "+args[1])
			return 1
		}
		if args[0] == "stop" {
			c.State = "exited"
		} else {
			c.State, c.StartedAt = "running", time.Now().UTC().Format(time.RFC3339)
		}
		s.save()
		fmt.Println(args[1])
	case "run":
		return dockerRun(s, args[1:])
	case "stats":
		for _, c := range s.Containers {
			if c.State == "running" {
				out(m{"ID": cid(c.Name), "Name": c.Name, "CPUPerc": "0.50%", "MemUsage": fmt.Sprintf("%dMiB / 8GiB", c.MemBytes>>20), "MemPerc": "1.00%", "NetIO": "1kB / 1kB", "BlockIO": "0B / 0B", "PIDs": "4"})
			}
		}
	case "images":
		for i, im := range s.Images {
			out(m{"ID": fmt.Sprintf("sha256:%012d", i), "Repository": im.Repository, "Tag": im.Tag, "Size": im.Size, "CreatedAt": "2026-09-01 00:00:00 +0000 UTC", "CreatedSince": "1 month ago"})
		}
	case "volume":
		tab := false
		for _, a := range args {
			if strings.Contains(a, `\t`) || strings.Contains(a, "\t") {
				tab = true
			}
		}
		for _, v := range s.Volumes {
			if tab {
				fmt.Printf("%s\t%s\n", v.Name, v.Labels)
			} else {
				out(m{"Name": v.Name, "Driver": "local", "Scope": "local", "Labels": v.Labels})
			}
		}
	case "logs":
		name := args[len(args)-1]
		if s.find(name) == nil {
			fmt.Fprintln(os.Stderr, "Error response from daemon: No such container: "+name)
			return 1
		}
		fmt.Println("fake log line one for " + name)
		fmt.Println("fake log line two for " + name)
	case "network":
		return 0
	default:
		fmt.Fprintln(os.Stderr, "fake docker: not supported in tests:", strings.Join(args, " "))
		return 1
	}
	return 0
}

// dockerRun is `docker run -d ...`: it records the container, keeps only the
// names (never the values) of an env file in calls.log, and serves HTTP on the
// published port.
func dockerRun(s *dockerState, args []string) int {
	c := fakeContainer{State: "running", MemBytes: 64 << 20, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	withValue := map[string]bool{"--name": true, "--label": true, "--memory": true, "-p": true, "--add-host": true, "--env-file": true}
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			break
		}
		if !withValue[a] || i+1 >= len(args) {
			continue
		}
		v := args[i+1]
		i++
		switch a {
		case "--name":
			c.Name = v
		case "--label":
			c.Labels = strings.Trim(c.Labels+","+v, ",")
		case "-p":
			parts := strings.Split(v, ":")
			if len(parts) >= 2 {
				c.HostPort, _ = strconv.Atoi(parts[len(parts)-2])
				c.CtrPort, _ = strconv.Atoi(parts[len(parts)-1])
			}
		case "--env-file":
			if b, err := os.ReadFile(v); err == nil {
				var keys []string
				for _, l := range strings.Split(string(b), "\n") {
					if k, _, ok := strings.Cut(l, "="); ok {
						keys = append(keys, k)
					}
				}
				logLine("docker run env-file keys: " + strings.Join(keys, ","))
			}
		}
	}
	if i < len(args) {
		c.Image = args[i]
	}
	if c.Name == "" || c.Image == "" {
		fmt.Fprintln(os.Stderr, "fake docker: run needs --name and an image")
		return 125
	}
	if old := s.find(c.Name); old != nil {
		fmt.Fprintln(os.Stderr, "Conflict. The container name is already in use")
		return 125
	}
	s.Containers = append(s.Containers, c)
	s.save()
	if c.HostPort != 0 {
		exe, err := os.Executable()
		if err == nil {
			cmd := exec.Command(exe, "__serve", strconv.Itoa(c.HostPort), c.Name)
			cmd.Args[0] = "docker"
			cmd.Env = os.Environ()
			if cmd.Start() == nil {
				_ = cmd.Process.Release()
			}
		}
	}
	fmt.Println(cid(c.Name))
	return 0
}

// serve answers HTTP on the port until the container is gone or stopped.
func serve(port, name string) int {
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return 1
	}
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprintln(w, "fake manager for "+name) }))
	}()
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		s, ok := loadDocker()
		if !ok {
			break
		}
		if c := s.find(name); c == nil || c.State != "running" {
			break
		}
	}
	return 0
}
