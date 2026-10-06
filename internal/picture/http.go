package picture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/databases"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Service serves the /api/picture routes. The collector fields are the
// machine's inventory for "From live state"; a nil one is skipped.
type Service struct {
	Projects func() (*projects.List, error)
	Store    *Store
	Runner   *agentexec.Runner

	Containers func(context.Context) ([]docker.Container, error)
	Databases  func(context.Context) ([]databases.Discovered, error)
	Runners    func(context.Context) ([]ci.Container, error)
}

// Summary is one row of GET /api/picture.
type Summary struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Where   string `json:"where"`
	Count   int    `json:"count"`
	// Confidential projects cannot be drafted by AI.
	Confidential bool `json:"confidential"`
}

// Listing is the body of GET /api/picture/{project}.
type Listing struct {
	Project  Summary `json:"project"`
	Where    string  `json:"where"`
	Dir      string  `json:"dir"`
	Items    []Item  `json:"items"`
	CanDraft bool    `json:"can_draft"`
	// DraftWhy says why drafting is unavailable.
	DraftWhy string `json:"draft_why,omitempty"`
}

// Live is the body of GET /api/picture/{project}/live.
type Live struct {
	Mermaid string `json:"mermaid"`
	// Notes name inventories that could not be read, so a sparse diagram is
	// explained rather than silent.
	Notes []string `json:"notes"`
}

func (s *Service) project(id string) (*projects.Project, error) {
	l, err := s.Projects()
	if err != nil {
		return nil, err
	}
	p := l.Find(id)
	if p == nil {
		return nil, fmt.Errorf("%w: no project %q in projects.yaml", ErrNotFound, id)
	}
	return p, nil
}

func (s *Service) summary(p projects.Project) Summary {
	loc := s.Store.Locate(p)
	n := 0
	if items, err := s.Store.List(p); err == nil {
		n = len(items)
	}
	return Summary{Project: p.ID, Name: p.Name, Where: loc.Where, Count: n, Confidential: p.Visibility == "confidential"}
}

func (s *Service) listing(p projects.Project) (*Listing, error) {
	items, err := s.Store.List(p)
	if err != nil {
		return nil, err
	}
	sum := s.summary(p)
	sum.Count = len(items)
	l := &Listing{Project: sum, Where: sum.Where, Dir: s.Store.Locate(p).Dir, Items: items, CanDraft: true}
	switch {
	case p.Visibility == "confidential":
		l.CanDraft, l.DraftWhy = false, "This project is confidential, so its code is never sent to an AI provider."
	case repoRoot(p) == "":
		l.CanDraft, l.DraftWhy = false, "Drafting reads the repository: set local_path for this project in projects.yaml."
	}
	return l, nil
}

// live collects the inventory and draws the project.
func (s *Service) live(ctx context.Context, p projects.Project, all []projects.Project) Live {
	var (
		cs    []docker.Container
		dbs   []databases.Discovered
		rs    []ci.Container
		notes = []string{}
	)
	if s.Containers != nil {
		var err error
		if cs, err = s.Containers(ctx); err != nil {
			notes = append(notes, "containers: docker is not reachable")
		}
	}
	if s.Databases != nil {
		var err error
		if dbs, err = s.Databases(ctx); err != nil {
			notes = append(notes, "databases: docker is not reachable")
		}
	}
	if s.Runners != nil {
		var err error
		if rs, err = s.Runners(ctx); err != nil {
			notes = append(notes, "CI runners: docker is not reachable")
		}
	}
	in := Select(p, cs, dbs, rs)
	in.Names = map[string]string{}
	for _, o := range all {
		in.Names[o.ID] = o.Name
	}
	return Live{Mermaid: Generate(in), Notes: notes}
}

func fail(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, memory.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, ErrBadPath), errors.Is(err, ErrBadRequest), errors.Is(err, memory.ErrBadPath):
		code = http.StatusBadRequest
	case errors.Is(err, ErrTooBig):
		code = http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrConfidential):
		code = http.StatusForbidden
	case errors.Is(err, ErrUnavailable):
		code = http.StatusServiceUnavailable
	}
	http.Error(w, err.Error(), code)
}

// Register adds the /api/picture routes to mux:
//
//	GET  /api/picture                                   every project with its store and picture count
//	GET  /api/picture/{project}                         the project's pictures and where they live
//	GET  /api/picture/{project}/item?name=&kind=        one picture's source
//	GET  /api/picture/{project}/target?name=&kind=      the exact path a save would write
//	PUT  /api/picture/{project}/item?name=&kind=        {"content": "..."}; needs the confirm header
//	GET  /api/picture/{project}/live                    a Mermaid diagram from live state
//	POST /api/picture/{project}/draft                   {"provider","model"}: AI draft, never saved; confirm header
//
// kind is mermaid or excalidraw. Names are one lower-case segment, so a
// request cannot leave docs/picture or Picture/<project>.
func Register(mux *http.ServeMux, s *Service) {
	withProject := func(w http.ResponseWriter, r *http.Request, fn func(p projects.Project)) {
		p, err := s.project(r.PathValue("project"))
		if err != nil {
			fail(w, err)
			return
		}
		fn(*p)
	}
	mux.HandleFunc("GET /api/picture", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.Projects()
		if err != nil {
			fail(w, err)
			return
		}
		out := []Summary{}
		for _, p := range l.Projects {
			out = append(out, s.summary(p))
		}
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /api/picture/{project}", func(w http.ResponseWriter, r *http.Request) {
		withProject(w, r, func(p projects.Project) {
			l, err := s.listing(p)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, l)
		})
	})
	mux.HandleFunc("GET /api/picture/{project}/item", func(w http.ResponseWriter, r *http.Request) {
		withProject(w, r, func(p projects.Project) {
			q := r.URL.Query()
			src, err := s.Store.Read(p, q.Get("name"), q.Get("kind"))
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"name": q.Get("name"), "kind": q.Get("kind"), "content": src})
		})
	})
	mux.HandleFunc("GET /api/picture/{project}/target", func(w http.ResponseWriter, r *http.Request) {
		withProject(w, r, func(p projects.Project) {
			q := r.URL.Query()
			loc, path, err := s.Store.Target(p, q.Get("name"), q.Get("kind"))
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"where": loc.Where, "path": path})
		})
	})
	mux.HandleFunc("PUT /api/picture/{project}/item", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		withProject(w, r, func(p projects.Project) {
			var in struct {
				Content string `json:"content"`
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxSize+4096))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				http.Error(w, "the body must be JSON with a content string: "+err.Error(), http.StatusBadRequest)
				return
			}
			q := r.URL.Query()
			item, err := s.Store.Write(p, q.Get("name"), q.Get("kind"), in.Content)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, item)
		})
	})
	mux.HandleFunc("GET /api/picture/{project}/live", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.Projects()
		if err != nil {
			fail(w, err)
			return
		}
		p := l.Find(r.PathValue("project"))
		if p == nil {
			fail(w, fmt.Errorf("%w: no project %q in projects.yaml", ErrNotFound, r.PathValue("project")))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		apiutil.WriteJSON(w, http.StatusOK, s.live(ctx, *p, l.Projects))
	})
	mux.HandleFunc("POST /api/picture/{project}/draft", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		withProject(w, r, func(p projects.Project) {
			var in DraftRequest
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
			defer cancel()
			res, err := Draft(ctx, s.Runner, p, in)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, res)
		})
	})
}
