// Package server wires the lucidd HTTP routes.
package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"runtime"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/hostinfo"
	"github.com/ChinmayGit8765/lucidbench/internal/jobs"
	"github.com/ChinmayGit8765/lucidbench/internal/k8s"
	"github.com/ChinmayGit8765/lucidbench/internal/mcp"
	"github.com/ChinmayGit8765/lucidbench/internal/prefs"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/themes"
	"github.com/ChinmayGit8765/lucidbench/internal/version"
	"github.com/ChinmayGit8765/lucidbench/internal/webui"
)

// HealthResponse is the body of GET /api/health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// HealthHandler reports daemon liveness and version.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok", Version: version.Version})
}

// New returns the root handler: /api/* routes plus the embedded UI. An
// optional config (from config.Load) is applied; without one, defaults are used.
func New(cfgs ...*config.Config) http.Handler {
	cfg := config.Default()
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", HealthHandler)
	mux.Handle("/api/accounts", accounts.HandlerFor(func() accounts.Roots { return accounts.FromConfig(cfg) }))
	mux.HandleFunc("/api/cluster", cluster.Handler)
	mux.Handle("/api/config", config.Handler(cfg, nil))
	mux.HandleFunc("/api/jobs/", jobs.Handler)
	mux.HandleFunc("/api/jobs", jobs.Handler)
	ci.Register(mux, ci.New(cfg.CI))
	runners := ci.Filter{ComposeProject: cfg.CI.Runners.ComposeProject, ImageMatch: cfg.CI.Runners.ImageMatch}
	docker.Register(mux, &docker.Service{Docker: docker.Exec, Policy: docker.Policy{
		Projects: append([]string{"lucidbench"}, cfg.Docker.AllowedProjects...),
		IsRunner: runners.Matches,
	}})
	k8s.Register(mux, &k8s.Service{Connect: k8s.Clientset})
	mux.Handle("/api/projects", projects.Handler())
	mux.Handle("/api/mcp", mcp.HandlerFor(func() mcp.Roots { return mcp.FromConfig(cfg) }))
	// UI prefs and themes live in the data dir; without one they are kept in
	// a "lucidbench" folder under the working directory.
	data, err := config.DataDir()
	if err != nil {
		data = "lucidbench"
	}
	mux.Handle("/api/prefs", prefs.Handler(&prefs.Store{Path: filepath.Join(data, prefs.FileName), LegacyTheme: cfg.UI.Theme}))
	themes.Register(mux, &themes.Store{Dir: filepath.Join(data, "themes")})
	mux.Handle("GET /api/about", hostinfo.AboutHandler(cfg, runtime.GOOS))
	mux.Handle("GET /api/host/tools", hostinfo.ToolsHandler(hostinfo.NewDetector()))
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", webui.Handler())
	return mux
}
