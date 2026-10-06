package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/assess"
)

// MaxAssessBody is the largest request body accepted by the assessment routes.
const MaxAssessBody = 32 << 10

// Errors the suggest step returns; the handler maps each to a status.
var (
	ErrConfidential = errors.New("confidential")
	ErrUnavailable  = errors.New("unavailable")
	ErrBadRequest   = errors.New("bad request")
)

// AssessAPI serves the assessment routes.
type AssessAPI struct {
	Load func() (*List, error)  // nil = Load
	Dir  func() (string, error) // nil = AssessDir
	Now  func() time.Time
	// Runner runs the user's own CLI for "Suggest answers from the repo";
	// nil = the default runner.
	Runner *agentexec.Runner

	mu sync.Mutex // one write at a time
}

func (a *AssessAPI) load() (*List, error) {
	if a.Load != nil {
		return a.Load()
	}
	return Load()
}

func (a *AssessAPI) dir() (string, error) {
	if a.Dir != nil {
		return a.Dir()
	}
	return AssessDir()
}

func (a *AssessAPI) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// project finds a project, answering 404 itself when there is none.
func (a *AssessAPI) project(w http.ResponseWriter, id string) *Project {
	l, err := a.load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	p := l.Find(id)
	if p == nil {
		http.Error(w, fmt.Sprintf("no project %q in projects.yaml", id), http.StatusNotFound)
	}
	return p
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxAssessBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// RegisterAssessment adds the assessment routes to mux:
//
//	GET  /api/assess/kinds                        every kind with its questions
//	GET  /api/projects/{id}/assessment            the project's assessment and what it suggests
//	PUT  /api/projects/{id}/assessment            confirm {kind, answers}; assessed_at is set here
//	POST /api/projects/{id}/assessment/suggest    {kind}: answers guessed from the project's files
//
// PUT and POST need X-Lucid-Confirm. Nothing is saved by the suggest route.
func RegisterAssessment(mux *http.ServeMux, a *AssessAPI) {
	mux.HandleFunc("GET /api/assess/kinds", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		apiutil.WriteJSON(w, http.StatusOK, assess.Kinds())
	})
	mux.HandleFunc("GET /api/projects/{id}/assessment", func(w http.ResponseWriter, r *http.Request) {
		p := a.project(w, r.PathValue("id"))
		if p == nil {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		apiutil.WriteJSON(w, http.StatusOK, view(p))
	})
	mux.HandleFunc("PUT /api/projects/{id}/assessment", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct {
			Kind    string            `json:"kind"`
			Answers map[string]string `json:"answers"`
			// AssessedAt is accepted so that a fetched assessment can be sent
			// back, and ignored: the time is set here.
			AssessedAt json.RawMessage `json:"assessed_at"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		p := a.project(w, r.PathValue("id"))
		if p == nil {
			return
		}
		next := assess.Assessment{Kind: in.Kind, Answers: in.Answers, AssessedAt: a.now().UTC().Truncate(time.Second)}
		if next.Answers == nil {
			next.Answers = map[string]string{}
		}
		if err := assess.Validate(next); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		dir, err := a.dir()
		if err == nil {
			a.mu.Lock()
			err = writeAssessment(dir, p.ID, next)
			a.mu.Unlock()
		}
		if err != nil {
			http.Error(w, "cannot save the assessment: "+err.Error(), http.StatusInternalServerError)
			return
		}
		p.Assessment, p.AssessmentFrom = &next, "app"
		apiutil.WriteJSON(w, http.StatusOK, view(p))
	})
	mux.HandleFunc("POST /api/projects/{id}/assessment/suggest", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct {
			Kind string `json:"kind"`
		}
		if !decodeBody(w, r, &in) {
			return
		}
		p := a.project(w, r.PathValue("id"))
		if p == nil {
			return
		}
		k, ok := assess.Get(in.Kind)
		if !ok {
			http.Error(w, fmt.Sprintf("unknown kind %q", in.Kind), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		answers, provider, err := a.suggestAnswers(ctx, *p, k)
		switch {
		case errors.Is(err, ErrConfidential):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, ErrBadRequest):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrUnavailable):
			http.Error(w, err.Error(), http.StatusConflict)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			apiutil.WriteJSON(w, http.StatusOK, map[string]any{"kind": k.ID, "answers": answers, "provider": provider})
		}
	})
}
