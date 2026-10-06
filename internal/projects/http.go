package projects

import (
	"encoding/json"
	"net/http"
)

// Handler serves GET /api/projects. The file is read on every request, so
// edits show up without restarting the daemon.
func Handler() http.Handler {
	return HandlerFor(func() *List {
		l, err := Load()
		if err != nil {
			return &List{Projects: []Project{}, Errors: []string{err.Error()}}
		}
		return l
	})
}

// HandlerFor is Handler with an explicit loader.
func HandlerFor(load func() *List) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(load())
	})
}
