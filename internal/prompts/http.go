package prompts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// MaxBody is the largest request body the prompt routes accept.
const MaxBody = 512 << 10

// MaxSources is the most context sources one render takes.
const MaxSources = 12

// API serves the Prompt Studio routes.
type API struct {
	Store    *Store
	Resolver *Resolver
	Improver *Improver
	Now      func() time.Time
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrConfidential):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrBadRef), errors.Is(err, ErrInvalid), errors.Is(err, agentexec.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrReadOnly):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, agentexec.ErrCLIMissing), errors.Is(err, agentexec.ErrNotSignedIn), errors.Is(err, agentexec.ErrInContainer):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	case errors.Is(err, agentexec.ErrTimeout):
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	default:
		memory.Fail(w, err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// SectionMeta is one section's id, heading and hint, for the editor.
type SectionMeta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Hint  string `json:"hint"`
}

// RenderRequest is the body of POST /api/prompts/render.
type RenderRequest struct {
	Sections []Section `json:"sections"`
	Context  []Ref     `json:"context,omitempty"`
	Project  string    `json:"project,omitempty"` // fills the {{project.*}} variables
	Target   string    `json:"target"`            // work | council | copy
}

// SourceInfo is a context source as the render answer lists it: no text.
type SourceInfo struct {
	Ref
	Label        string `json:"label"`
	Chars        int    `json:"chars"`
	Tokens       int    `json:"tokens"`
	Confidential bool   `json:"confidential"`
	Truncated    bool   `json:"truncated,omitempty"`
}

// RenderResponse is the final text, its size and its lint.
type RenderResponse struct {
	Rendered
	// TokensNote says the count is an estimate.
	TokensNote string       `json:"tokens_note"`
	Lint       []Finding    `json:"lint"`
	Blocked    bool         `json:"blocked"`
	Sources    []SourceInfo `json:"sources"`
}

// RenderPrompt resolves the context sources again (their text and their
// confidentiality are read here, never taken from the caller), renders and
// lints.
func (a *API) RenderPrompt(in RenderRequest) (RenderResponse, error) {
	switch in.Target {
	case TargetWork, TargetCouncil, TargetCopy:
	case "":
		in.Target = TargetCopy
	default:
		return RenderResponse{}, errors.New("target must be work, council or copy")
	}
	if len(in.Context) > MaxSources {
		return RenderResponse{}, errors.New("too many context sources")
	}
	for _, s := range in.Sections {
		if _, ok := SectionInfo[s.ID]; !ok {
			return RenderResponse{}, errors.New("unknown section " + s.ID)
		}
	}
	var sources []Source
	var problems []Finding
	for _, ref := range in.Context {
		s, err := a.Resolver.Resolve(ref)
		if err != nil {
			problems = append(problems, Finding{Severity: SevError, Code: "source", Section: Context, Message: "A context source cannot be read: " + err.Error()})
			continue
		}
		sources = append(sources, s)
	}
	vars := a.Resolver.Vars(in.Project)
	if vars == nil {
		vars = map[string]string{}
	}
	vars["date"] = a.now().Format("2006-01-02")
	secs := in.Sections
	if in.Target == TargetWork {
		secs = ForWork(secs)
	}
	r := Render(secs, vars, sources)
	// Never null: a clean prompt answers "lint": [].
	lint := append(append([]Finding{}, problems...), Lint(in.Sections, r, sources, in.Target)...)
	if in.Target == TargetWork {
		lint = append(lint, Finding{Severity: SevInfo, Code: "work-adds",
			Message: "Work puts its own role and safety rules first (worktree only, commit, never push, the allowed commands), so they are left out here."})
	}
	if in.Project != "" {
		if p, err := a.Resolver.project(in.Project); err == nil && p.Visibility == "confidential" && in.Target != TargetCopy {
			lint = append([]Finding{{Severity: SevError, Code: "confidential",
				Message: p.Name + " is a confidential project: its prompts never go to a provider. Copy the prompt to use it locally."}}, lint...)
		}
	}
	out := RenderResponse{Rendered: r, TokensNote: "estimate: characters / 4", Lint: lint, Blocked: Blocked(lint), Sources: []SourceInfo{}}
	for _, s := range sources {
		out.Sources = append(out.Sources, SourceInfo{Ref: s.Ref, Label: s.Label, Chars: s.Chars, Tokens: s.Tokens, Confidential: s.Confidential, Truncated: s.Truncated})
	}
	return out, nil
}

// Register adds the Prompt Studio routes to mux:
//
//	GET    /api/prompts/templates          templates, section headings and hints, variables
//	PUT    /api/prompts/templates/{id}     save a user template (or an override of a built-in)
//	DELETE /api/prompts/templates/{id}     delete a user template or override
//	GET    /api/prompts/snippets           the user's snippets
//	PUT    /api/prompts/snippets/{id}      save a snippet
//	DELETE /api/prompts/snippets/{id}      delete a snippet
//	GET    /api/prompts/context?kind=...   one context source: text, size, confidential
//	POST   /api/prompts/render             {sections, context, project, target}: text, token estimate, lint
//	POST   /api/prompts/improve            {text|sections, provider?, model?, project?}: a preview, never applied
//
// PUT, DELETE and improve need X-Lucid-Confirm; render changes nothing.
func Register(mux *http.ServeMux, a *API) {
	mux.HandleFunc("GET /api/prompts/templates", func(w http.ResponseWriter, r *http.Request) {
		secs := make([]SectionMeta, 0, len(SectionIDs))
		for _, id := range SectionIDs {
			secs = append(secs, SectionMeta{ID: id, Title: SectionInfo[id].Title, Hint: SectionInfo[id].Hint})
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{
			"templates": a.Store.Templates(),
			"sections":  secs,
			"variables": Variables,
		})
	})
	mux.HandleFunc("PUT /api/prompts/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var t Template
		if !decode(w, r, &t) {
			return
		}
		t.ID = r.PathValue("id")
		saved, err := a.Store.SaveTemplate(t)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, saved)
	})
	mux.HandleFunc("DELETE /api/prompts/templates/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		if err := a.Store.DeleteTemplate(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/prompts/snippets", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, a.Store.Snippets())
	})
	mux.HandleFunc("PUT /api/prompts/snippets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var sn Snippet
		if !decode(w, r, &sn) {
			return
		}
		sn.ID = r.PathValue("id")
		saved, err := a.Store.SaveSnippet(sn)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, saved)
	})
	mux.HandleFunc("DELETE /api/prompts/snippets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		if err := a.Store.DeleteSnippet(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/prompts/context", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		s, err := a.Resolver.Resolve(Ref{Kind: q.Get("kind"), Project: q.Get("project"), Path: q.Get("path"), Board: q.Get("board"), ID: q.Get("id")})
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, s)
	})
	mux.HandleFunc("POST /api/prompts/render", func(w http.ResponseWriter, r *http.Request) {
		var in RenderRequest
		if !decode(w, r, &in) {
			return
		}
		out, err := a.RenderPrompt(in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /api/prompts/improve", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in ImproveRequest
		if !decode(w, r, &in) {
			return
		}
		in.Text = strings.TrimSpace(in.Text)
		ctx, cancel := context.WithTimeout(r.Context(), ImproveTimeout+30*time.Second)
		defer cancel()
		out, err := a.Improver.Improve(ctx, in)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
}
