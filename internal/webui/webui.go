// Package webui serves the built web UI embedded into the binary.
package webui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/web"
)

const fallbackMessage = "Lucidbench UI not built. Run `npm run build` in web/ and rebuild lucidd.\n"

// Handler serves the embedded UI with SPA fallback to index.html.
func Handler() http.Handler {
	sub, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return fallback()
	}
	return handlerFor(sub)
}

func handlerFor(fsys fs.FS) http.Handler {
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		return fallback()
	}
	files := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" {
			if st, err := fs.Stat(fsys, p); err == nil && !st.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
			// A missing file 404s, but Memory routes end in page file names.
			if path.Ext(p) != "" && !strings.HasPrefix(p, "memory/") {
				http.NotFound(w, r)
				return
			}
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

func fallback() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(fallbackMessage))
	})
}
