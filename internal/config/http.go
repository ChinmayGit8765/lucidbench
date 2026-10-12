package config

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strconv"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
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

// FileView is the body of GET /api/config/file: the writable keys as the
// config file (and the environment) set them now, which may differ from
// what the running daemon uses until it restarts.
type FileView struct {
	Path  string `json:"path"`
	Found bool   `json:"found"`
	// Values holds each writable key's effective value from the file.
	Values  map[string]any    `json:"values"`
	Sources map[string]string `json:"sources"`
	// Pending are keys saved in the file that the running daemon does not
	// use yet: a restart applies them.
	Pending []string `json:"pending"`
	Error   string   `json:"error,omitempty"`
}

// maxEditBody bounds a save request.
const maxEditBody = 64 << 10

// RegisterWrite adds the routes that save settings to the config file:
//
//	GET  /api/config/file      the writable keys as the file sets them
//	POST /api/config/preview   {set: {key: value}}: the diff, nothing written
//	POST /api/config/save      the same, written (X-Lucid-Confirm: yes)
//
// running is the config the daemon started with; running.File is the file
// that is written.
func RegisterWrite(mux *http.ServeMux, running *Config, getenv func(string) string) {
	if getenv == nil {
		getenv = os.Getenv
	}
	mux.HandleFunc("GET /api/config/file", func(w http.ResponseWriter, r *http.Request) {
		v := FileView{Path: running.File, Values: map[string]any{}, Sources: map[string]string{}, Pending: []string{}}
		now := running
		if running.File != "" {
			got, _, err := LoadFrom(running.File, getenv)
			if err != nil {
				v.Error = err.Error()
			} else {
				now = got
			}
		}
		v.Found = now.FileFound
		for k := range Writable {
			v.Values[k] = now.typed(k)
			v.Sources[k] = now.Source(k)
			if now.value(k) != running.value(k) {
				v.Pending = append(v.Pending, k)
			}
		}
		sort.Strings(v.Pending)
		apiutil.WriteJSON(w, http.StatusOK, v)
	})
	edit := func(save bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if save && !apiutil.Confirmed(w, r) {
				return
			}
			var in struct {
				Set map[string]any `json:"set"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxEditBody))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			do := PlanEdit
			if save {
				do = SaveEdit
			}
			e, err := do(running.File, in.Set, running, getenv)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, e)
		}
	}
	mux.HandleFunc("POST /api/config/preview", edit(false))
	mux.HandleFunc("POST /api/config/save", edit(true))
}

// typed is a writable key's value as JSON shows it: a number, a bool or text.
func (c *Config) typed(key string) any {
	switch Writable[key] {
	case kindInt:
		n, _ := strconv.Atoi(c.value(key))
		return n
	case kindBool:
		return c.value(key) == "true"
	}
	return c.value(key)
}
