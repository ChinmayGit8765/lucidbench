// Package hostinfo answers two questions for the UI: where the engine keeps
// its files (Settings, General) and which tools the host has (the
// requirement pills on extension cards).
package hostinfo

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
)

// About is the body of GET /api/about.
type About struct {
	Version     string `json:"version"`
	ConfigFile  string `json:"config_file"`
	ConfigFound bool   `json:"config_found"`
	DataDir     string `json:"data_dir"`
	InContainer bool   `json:"in_container"`
	OS          string `json:"os"`
}

// Tidy shows a path under the home directory as ~/...
func Tidy(p, home string) string {
	if home == "" || p == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		if rel == "." {
			return "~"
		}
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

// AboutHandler serves GET /api/about.
func AboutHandler(cfg *config.Config, goos string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		home, _ := os.UserHomeDir()
		data, _ := config.DataDir()
		apiutil.WriteJSON(w, http.StatusOK, About{
			Version:     version.Version,
			ConfigFile:  Tidy(cfg.File, home),
			ConfigFound: cfg.FileFound,
			DataDir:     Tidy(data, home),
			InContainer: cluster.InContainer(),
			OS:          goos,
		})
	})
}

// CLIs are the command-line tools extensions may require. Only these names
// are ever looked up.
var CLIs = []string{
	"docker", "kubectl", "gh", "gcloud", "wrangler", "vercel", "aws", "az",
	"stripe", "psql", "redis-cli", "claude", "codex", "grok",
}

// EnvVars are the environment variables extensions may require. Only their
// presence is reported, never their values.
var EnvVars = []string{"LINEAR_API_KEY", "TRELLO_API_KEY", "TRELLO_TOKEN", "STRIPE_API_KEY"}

// Tools is the body of GET /api/host/tools.
type Tools struct {
	CLIs   map[string]bool `json:"clis"`
	Env    map[string]bool `json:"env"`
	Docker bool            `json:"docker"`
}

// Detector finds tools; its fields are swapped in tests.
type Detector struct {
	LookPath func(string) (string, error)
	Getenv   func(string) string
	// DockerUp reports whether the docker engine answers.
	DockerUp func(ctx context.Context) bool

	mu     sync.Mutex
	cached *Tools
	at     time.Time
}

// NewDetector returns a Detector for the real host.
func NewDetector() *Detector {
	return &Detector{
		LookPath: exec.LookPath,
		Getenv:   os.Getenv,
		DockerUp: func(ctx context.Context) bool {
			return exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Run() == nil
		},
	}
}

// Detect reports the tools, cached for 30 seconds.
func (d *Detector) Detect(ctx context.Context) Tools {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cached != nil && time.Since(d.at) < 30*time.Second {
		return *d.cached
	}
	t := Tools{CLIs: map[string]bool{}, Env: map[string]bool{}}
	for _, c := range CLIs {
		_, err := d.LookPath(c)
		t.CLIs[c] = err == nil
	}
	for _, e := range EnvVars {
		t.Env[e] = d.Getenv(e) != ""
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	t.Docker = d.DockerUp(cctx)
	d.cached, d.at = &t, time.Now()
	return t
}

// ToolsHandler serves GET /api/host/tools.
func ToolsHandler(d *Detector) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, d.Detect(r.Context()))
	})
}
