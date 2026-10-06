package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// connect is swapped in tests.
var connect = Connect

// EnsureCluster, when set, runs before a job is submitted through the API.
// The daemon sets it to the power supervisor's EnsureCluster, which starts a
// sleeping cluster and waits until it is ready. Nil does nothing.
var EnsureCluster func(ctx context.Context) error

// Handler serves GET /api/jobs (JSON list), GET /api/jobs/{name}/logs (plain
// text) and POST /api/jobs/hello (submit the hello job). It answers 503 when
// no local cluster is reachable.
func Handler(w http.ResponseWriter, req *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(req.URL.Path, "/api/jobs"), "/")
	if req.Method == http.MethodPost && rest == "hello" {
		submitHello(w, req)
		return
	}
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r, err := connect()
	if err != nil {
		unavailable(w, err)
		return
	}
	switch {
	case rest == "":
		list, err := r.List(req.Context())
		if err != nil {
			unavailable(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)
	case strings.HasSuffix(rest, "/logs") && !strings.Contains(strings.TrimSuffix(rest, "/logs"), "/"):
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if err := r.Logs(req.Context(), strings.TrimSuffix(rest, "/logs"), w); err != nil {
			// Headers may already be sent; best effort.
			http.Error(w, err.Error(), http.StatusNotFound)
		}
	default:
		http.NotFound(w, req)
	}
}

func submitHello(w http.ResponseWriter, req *http.Request) {
	if EnsureCluster != nil {
		if err := EnsureCluster(req.Context()); err != nil {
			unavailable(w, err)
			return
		}
	}
	r, err := connect()
	if err != nil {
		unavailable(w, err)
		return
	}
	j, err := r.SubmitHello(req.Context())
	if err != nil {
		unavailable(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"name": j.Name})
}

func unavailable(w http.ResponseWriter, err error) {
	msg := "local cluster unavailable: " + err.Error()
	if errors.Is(err, ErrNoCluster) {
		msg = ErrNoCluster.Error()
	}
	http.Error(w, msg, http.StatusServiceUnavailable)
}
