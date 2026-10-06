package ideas

import (
	"errors"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Register adds the idea routes to mux:
//
//	GET /api/ideas         every idea, newest first
//	GET /api/ideas/{id...} one idea's whole structure; a hand-made card's id
//	                       is card:<board>/<card id>, so the id may hold a "/"
//
// Both only read. They never ask gh or git: the PR state shown is the one
// Work last read.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/ideas", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /api/ideas/{id...}", func(w http.ResponseWriter, r *http.Request) {
		idea, err := s.Get(r.PathValue("id"))
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		default:
			apiutil.WriteJSON(w, http.StatusOK, idea)
		}
	})
}
