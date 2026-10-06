package setup

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// MaxBody is the largest request body accepted.
const MaxBody = 256 << 10

// Register adds the setup routes to mux:
//
//	GET  /api/setup                      {needed, ui, projects, projects_hint}
//	GET  /api/setup/dirs?path=           the folders in path (home when empty)
//	POST /api/setup/scan                 {root} → git repositories up to two levels down
//	POST /api/setup/projects/preview     {entries} → the YAML that would be appended
//	POST /api/setup/projects             {entries} → append to projects.yaml (confirm)
//
// Scan and preview read only. Adding needs X-Lucid-Confirm; when the file
// cannot be appended to safely it answers 409 with the snippet to paste.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.Status())
	})
	mux.HandleFunc("GET /api/setup/dirs", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.List(r.URL.Query().Get("path"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, l)
	})
	mux.HandleFunc("POST /api/setup/scan", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Root string `json:"root"`
		}
		if !decode(w, r, &in) {
			return
		}
		res, err := s.Scan(in.Root)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
	entries := func(w http.ResponseWriter, r *http.Request) ([]Entry, bool) {
		var in struct {
			Entries []Entry `json:"entries"`
		}
		ok := decode(w, r, &in)
		return in.Entries, ok
	}
	mux.HandleFunc("POST /api/setup/projects/preview", func(w http.ResponseWriter, r *http.Request) {
		es, ok := entries(w, r)
		if !ok {
			return
		}
		res, err := s.Preview(es)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/setup/projects", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		es, ok := entries(w, r)
		if !ok {
			return
		}
		res, err := s.Add(es)
		switch {
		case errors.Is(err, ErrNotAppendable):
			apiutil.WriteJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "snippet": res.Snippet, "path_hint": res.PathHint})
		case err != nil:
			fail(w, err)
		default:
			apiutil.WriteJSON(w, http.StatusOK, res)
		}
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrBadRequest) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
