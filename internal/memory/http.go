package memory

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// MaxBody is the largest request body accepted by the memory routes.
const MaxBody = 8 << 20

// Opener returns the vault, opening it on first use.
type Opener func() (*Vault, error)

// LazyOpener opens the configured vault on the first request, not at
// startup, so a daemon that never touches Memory never creates its folder.
// A failure is not remembered: the next request tries again.
func LazyOpener(cfg *config.Config) Opener {
	var (
		mu sync.Mutex
		v  *Vault
	)
	return func() (*Vault, error) {
		mu.Lock()
		defer mu.Unlock()
		if v != nil {
			return v, nil
		}
		nv, err := OpenConfigured(cfg)
		if err != nil {
			return nil, err
		}
		v = nv
		return v, nil
	}
}

// Fail answers err with the status that fits it: 400 for a bad path, 404 for
// a missing page, 409 for a name already taken, 500 otherwise.
func Fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBadPath):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrExists):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Register adds the memory routes to mux:
//
//	GET    /api/memory/tree?dir=        folder listing
//	GET    /api/memory/page?path=       read a page
//	PUT    /api/memory/page?path=       write a page: {front, body, title}
//	DELETE /api/memory/page?path=       move a page or folder to .trash/
//	POST   /api/memory/move             {from, to}
//	POST   /api/memory/restore          {path}: the newest trashed copy back to path (undo)
//	GET    /api/memory/search?q=&limit= full-text hits
//	GET    /api/memory/backlinks?path=  pages that link to a page
//	GET    /api/memory/info             vault folder and page count
//
// PUT, DELETE and POST need X-Lucid-Confirm.
func Register(mux *http.ServeMux, open Opener) {
	// with opens the vault, answering 500 when it cannot be opened.
	with := func(w http.ResponseWriter, fn func(v *Vault)) {
		v, err := open()
		if err != nil {
			http.Error(w, "cannot open the vault: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fn(v)
	}
	mux.HandleFunc("GET /api/memory/tree", func(w http.ResponseWriter, r *http.Request) {
		with(w, func(v *Vault) {
			ents, err := v.List(r.URL.Query().Get("dir"))
			if err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, ents)
		})
	})
	mux.HandleFunc("GET /api/memory/page", func(w http.ResponseWriter, r *http.Request) {
		with(w, func(v *Vault) {
			p, err := v.Read(r.URL.Query().Get("path"))
			if err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, p)
		})
	})
	mux.HandleFunc("PUT /api/memory/page", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct {
			Title string         `json:"title"`
			Front map[string]any `json:"front"`
			Body  string         `json:"body"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid page JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		with(w, func(v *Vault) {
			path := r.URL.Query().Get("path")
			if err := v.Write(&Page{Path: path, Title: in.Title, Front: in.Front, Body: in.Body}); err != nil {
				Fail(w, err)
				return
			}
			p, err := v.Read(path)
			if err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, p)
		})
	})
	mux.HandleFunc("DELETE /api/memory/page", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		with(w, func(v *Vault) {
			if err := v.Delete(r.URL.Query().Get("path")); err != nil {
				Fail(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	})
	mux.HandleFunc("POST /api/memory/move", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct{ From, To string }
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid move JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		with(w, func(v *Vault) {
			if err := v.Move(in.From, in.To); err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"path": in.To})
		})
	})
	mux.HandleFunc("POST /api/memory/restore", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct{ Path string }
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid restore JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		with(w, func(v *Vault) {
			if err := v.Restore(in.Path); err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"path": in.Path})
		})
	})
	mux.HandleFunc("GET /api/memory/search", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		with(w, func(v *Vault) {
			hits, err := v.Search(r.URL.Query().Get("q"), limit)
			if err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, hits)
		})
	})
	mux.HandleFunc("GET /api/memory/backlinks", func(w http.ResponseWriter, r *http.Request) {
		with(w, func(v *Vault) {
			links, err := v.Backlinks(r.URL.Query().Get("path"))
			if err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, links)
		})
	})
	mux.HandleFunc("GET /api/memory/info", func(w http.ResponseWriter, r *http.Request) {
		with(w, func(v *Vault) {
			// Folder notes and the board files do not count as pages, so a
			// vault that only holds the default board still reads as empty.
			n := 0
			err := v.walk(func(rel, _ string) error {
				if path.Base(rel) != FolderFile && !strings.HasPrefix(rel, "Boards/") {
					n++
				}
				return nil
			})
			if err != nil {
				Fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, Info{Root: v.Root(), Pages: n})
		})
	})
}

// Info is the body of GET /api/memory/info.
type Info struct {
	Root  string `json:"root"`  // absolute vault folder
	Pages int    `json:"pages"` // Markdown pages, not counting folder notes and boards
}
