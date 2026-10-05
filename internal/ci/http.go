package ci

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

// ConfirmHeader must be "yes" on every POST. The web UI sends it; a cross-site
// form or link cannot, and a cross-site script would need a CORS preflight
// that lucidd never grants.
const ConfirmHeader = "X-Lucid-Confirm"

// Register adds the /api/ci routes to mux:
//
//	GET  /api/ci/summary
//	GET  /api/ci/runners
//	GET  /api/ci/runs[?repo=owner/name]
//	POST /api/ci/containers/{name}/{action}   action: start, stop, restart
//	POST /api/ci/runs/{repo}/{id}/rerun       repo URL-encoded (owner%2Fname)
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/ci/summary", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, s.Summary(ctx))
	})
	mux.HandleFunc("GET /api/ci/runners", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		writeJSON(w, http.StatusOK, s.Runners(ctx))
	})
	mux.HandleFunc("GET /api/ci/runs", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		resp, err := s.Runs(ctx, r.URL.Query().Get("repo"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	})
	mux.HandleFunc("POST /api/ci/containers/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !confirmed(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		name, action := r.PathValue("name"), r.PathValue("action")
		err := s.ContainerAction(ctx, name, action)
		switch {
		case errors.Is(err, ErrBadAction):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrNotRunner):
			http.Error(w, "refused: "+err.Error(), http.StatusForbidden)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			writeJSON(w, http.StatusOK, map[string]string{"name": name, "action": action, "status": "ok"})
		}
	})
	mux.HandleFunc("POST /api/ci/runs/{repo}/{id}/rerun", func(w http.ResponseWriter, r *http.Request) {
		if !confirmed(w, r) {
			return
		}
		repo := r.PathValue("repo")
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "run id must be a positive integer", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		err = s.Rerun(ctx, repo, id)
		switch {
		case errors.Is(err, ErrUnknownRepo):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, ErrNoToken):
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			writeJSON(w, http.StatusAccepted, map[string]any{"repo": repo, "id": id, "status": "requested"})
		}
	})
}

func confirmed(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(ConfirmHeader) != "yes" {
		http.Error(w, "missing header "+ConfirmHeader+": yes", http.StatusForbidden)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
