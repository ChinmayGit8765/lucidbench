package themes

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// MaxBody is the largest request body accepted by POST /api/themes.
const MaxBody = 8 << 20

var assetTypes = map[string]string{
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// Register adds the theme routes to mux:
//
//	GET    /api/themes                       presets plus user themes
//	GET    /api/themes/{id}/export           a theme with its assets inline
//	GET    /api/themes/{id}/assets/{file}    one image of a user theme
//	POST   /api/themes                       create or replace a user theme (a Bundle)
//	DELETE /api/themes/{id}                  delete a user theme
//
// POST and DELETE need X-Lucid-Confirm.
func Register(mux *http.ServeMux, s *Store) {
	mux.HandleFunc("GET /api/themes", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.List())
	})
	mux.HandleFunc("GET /api/themes/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Export(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="`+b.Theme.ID+`.theme.json"`)
		apiutil.WriteJSON(w, http.StatusOK, b)
	})
	mux.HandleFunc("GET /api/themes/{id}/assets/{file}", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.AssetPath(r.PathValue("id"), r.PathValue("file"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", assetTypes[filepath.Ext(p)])
		h.Set("X-Content-Type-Options", "nosniff")
		// Opened directly, an asset can neither run script nor load anything.
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		h.Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "", st.ModTime(), f)
	})
	mux.HandleFunc("POST /api/themes", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var b Bundle
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&b); err != nil {
			http.Error(w, "invalid theme JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		t, err := s.Save(b)
		switch {
		case errors.Is(err, ErrBuiltIn):
			http.Error(w, err.Error(), http.StatusForbidden)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			apiutil.WriteJSON(w, http.StatusOK, t)
		}
	})
	mux.HandleFunc("DELETE /api/themes/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		err := s.Delete(r.PathValue("id"))
		switch {
		case errors.Is(err, ErrBuiltIn):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})
}
