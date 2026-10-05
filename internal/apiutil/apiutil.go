// Package apiutil holds the small HTTP helpers shared by the API packages.
package apiutil

import (
	"encoding/json"
	"net/http"
)

// ConfirmHeader must be "yes" on every request that changes something. The
// web UI sends it; a cross-site form or link cannot, and a cross-site script
// would need a CORS preflight that lucidd never grants.
const ConfirmHeader = "X-Lucid-Confirm"

// Confirmed reports whether r carries the confirm header, answering 403 when
// it does not.
func Confirmed(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get(ConfirmHeader) != "yes" {
		http.Error(w, "missing header "+ConfirmHeader+": yes", http.StatusForbidden)
		return false
	}
	return true
}

// WriteJSON writes v as an uncached JSON response.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
