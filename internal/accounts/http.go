package accounts

import (
	"encoding/json"
	"net/http"
)

// Handler serves GET /api/accounts: a JSON list of profiles (never secrets).
func Handler() http.Handler { return HandlerFor(FromEnv) }

// HandlerFor is Handler with an explicit source of Roots.
func HandlerFor(roots func() Roots) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ps := All(roots())
		if ps == nil {
			ps = []Profile{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ps)
	})
}
