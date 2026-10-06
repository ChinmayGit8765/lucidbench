package sections

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Catalog is the body of GET /api/sections/catalog: what a section may use.
type Catalog struct {
	APIs          []API     `json:"apis"`
	Templates     []Section `json:"templates"`
	Views         []string  `json:"views"`
	Formats       []string  `json:"formats"`
	Ops           []string  `json:"ops"`
	PromptVersion string    `json:"prompt_version"`
}

// CheckResult is the body of POST /api/sections/check.
type CheckResult struct {
	Section  *Section  `json:"section"`
	Valid    bool      `json:"valid"`
	Problems []Problem `json:"problems"`
}

func problems(err error) []Problem {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Problems
	}
	return []Problem{{"", err.Error()}}
}

func fail(w http.ResponseWriter, err error) {
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		apiutil.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "problems": ve.Problems})
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrFull):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, ErrBadRequest), errors.Is(err, agentexec.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, agentexec.ErrInContainer):
		http.Error(w, err.Error(), http.StatusNotImplemented)
	case errors.Is(err, agentexec.ErrCLIMissing), errors.Is(err, agentexec.ErrNotSignedIn):
		http.Error(w, err.Error(), http.StatusFailedDependency)
	case errors.Is(err, agentexec.ErrTimeout):
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	case errors.Is(err, ErrBadOutput):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBytes))
	if err != nil {
		http.Error(w, "the section is larger than 16 KB", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return data, true
}

// Register adds the section routes to mux:
//
//	GET    /api/sections                      the user's sections, and files that are not valid sections
//	GET    /api/sections/catalog              the allowed routes, the built-in templates, views, formats and filter ops
//	POST   /api/sections/check                validate a section without saving it
//	PUT    /api/sections/{id}                 save a section (the body's id must match)
//	DELETE /api/sections/{id}                 remove a section
//	POST   /api/sections/templates/{id}       add a copy of a built-in template
//	POST   /api/sections/generate             {description, provider?, profile?, model?, placement?}: a validated preview, not saved
//
// Every change needs X-Lucid-Confirm, and so does generate, which spends on
// the user's account. One generation runs at a time.
func Register(mux *http.ServeMux, st *Store, g *Generator) {
	mux.HandleFunc("GET /api/sections", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, st.List())
	})
	mux.HandleFunc("GET /api/sections/catalog", func(w http.ResponseWriter, r *http.Request) {
		ts := make([]Section, 0, len(Templates))
		for _, t := range Templates {
			ts = append(ts, Normalise(t))
		}
		apiutil.WriteJSON(w, http.StatusOK, Catalog{APIs: APIs, Templates: ts, Views: Views, Formats: Formats, Ops: Ops, PromptVersion: PromptVersion()})
	})
	mux.HandleFunc("POST /api/sections/check", func(w http.ResponseWriter, r *http.Request) {
		data, ok := readBody(w, r)
		if !ok {
			return
		}
		s, err := Check(data)
		out := CheckResult{Valid: err == nil, Problems: []Problem{}}
		if err != nil {
			out.Problems = problems(err)
		}
		if err == nil || !errors.Is(err, ErrInvalid) || s.ID != "" || s.Title != "" {
			out.Section = &s
		}
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("PUT /api/sections/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		data, ok := readBody(w, r)
		if !ok {
			return
		}
		s, err := Parse(data)
		if err != nil {
			fail(w, err)
			return
		}
		if s.ID != r.PathValue("id") {
			http.Error(w, "the section's id does not match the route", http.StatusBadRequest)
			return
		}
		saved, err := st.Save(s)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, saved)
	})
	mux.HandleFunc("DELETE /api/sections/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		if err := st.Delete(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/sections/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		t, ok := Template(r.PathValue("id"))
		if !ok {
			http.Error(w, "no template "+r.PathValue("id"), http.StatusNotFound)
			return
		}
		t.ID = st.FreeID(t.ID)
		saved, err := st.Save(t)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, saved)
	})
	busy := make(chan struct{}, 1)
	mux.HandleFunc("POST /api/sections/generate", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req GenerateRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid request JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		default:
			http.Error(w, "a section is already being generated", http.StatusConflict)
			return
		}
		out, err := g.Generate(r.Context(), req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
}
