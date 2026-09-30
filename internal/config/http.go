package config

import (
	"encoding/json"
	"net/http"
)

// Response is the body of GET /api/config: the effective, non-secret config.
type Response struct {
	Config
	Sources  map[string]string `json:"sources"`
	Warnings []string          `json:"warnings"`
}

// Handler serves GET /api/config for the UI.
func Handler(c *Config, warnings []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		src := map[string]string{}
		for _, k := range Keys() {
			src[k] = c.Source(k)
		}
		if warnings == nil {
			warnings = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Response{Config: *c, Sources: src, Warnings: warnings})
	})
}
