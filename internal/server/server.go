// Package server wires the lucidd HTTP routes.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/cluster"
	"github.com/ChinmayGit8765/lucidbench/internal/jobs"
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

// New returns the root handler: /api/* routes plus the embedded UI.
func New() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", HealthHandler)
	mux.Handle("/api/accounts", accounts.Handler())
	mux.HandleFunc("/api/cluster", cluster.Handler)
	mux.HandleFunc("/api/jobs/", jobs.Handler)
	mux.HandleFunc("/api/jobs", jobs.Handler)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", webui.Handler())
	return mux
}
