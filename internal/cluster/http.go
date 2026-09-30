package cluster

import (
	"encoding/json"
	"net/http"
)

// Handler serves GET /api/cluster. It answers 503 when Docker is unreachable.
func Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	info, err := Describe()
	if err != nil {
		http.Error(w, "docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}
