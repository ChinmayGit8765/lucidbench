package nextup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
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

// decode reads a small JSON body, refusing unknown fields.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid request JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// RankResponse is the body of POST /api/nextup/rank: the re-ordered list and
// what the call cost.
type RankResponse struct {
	View
	Usage agentexec.Usage `json:"usage"`
}

// Register adds the Next up routes to mux:
//
//	GET  /api/nextup            the candidates, best first, with their scores, actions and the hidden ones
//	POST /api/nextup/rank       {provider?, profile?, model?}: ask an agent to order the list (spends)
//	POST /api/nextup/snooze     {id, days: 1|7}
//	POST /api/nextup/dismiss    {id, reason}: "Not now", remembered for scoring
//	POST /api/nextup/restore    {id}: bring a snoozed or set-aside item back
//	PUT  /api/nextup/settings   {focus_project, pinned, schedule_hours, provider, model}
//
// Listing never runs a provider. Every write needs X-Lucid-Confirm; so does
// rank, which spends on the user's account. One ranking runs at a time.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/nextup", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.List(r.Context()))
	})
	busy := make(chan struct{}, 1)
	mux.HandleFunc("POST /api/nextup/rank", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req RankRequest
		if !decode(w, r, &req) {
			return
		}
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		default:
			http.Error(w, "a ranking is already running", http.StatusConflict)
			return
		}
		v, rk, err := s.Rank(r.Context(), req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, RankResponse{View: v, Usage: rk.Usage})
	})
	mux.HandleFunc("POST /api/nextup/snooze", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req struct {
			ID   string `json:"id"`
			Days int    `json:"days"`
		}
		if !decode(w, r, &req) {
			return
		}
		until, err := s.Store.Snooze(req.ID, req.Days, s.now())
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"id": req.ID, "until": until})
	})
	mux.HandleFunc("POST /api/nextup/dismiss", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req struct {
			ID     string `json:"id"`
			Reason string `json:"reason"`
		}
		if !decode(w, r, &req) {
			return
		}
		if err := CheckID(req.ID); err != nil {
			fail(w, err)
			return
		}
		// The project and kind are read here, never taken from the caller.
		project, kind := "", ""
		if c, err := s.Find(r.Context(), req.ID); err == nil {
			project, kind = c.Project, c.Kind
		}
		if err := s.Store.Dismiss(req.ID, req.Reason, project, kind, s.now()); err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"id": req.ID, "reason": req.Reason})
	})
	mux.HandleFunc("POST /api/nextup/restore", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &req) {
			return
		}
		if err := s.Store.Restore(req.ID); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /api/nextup/settings", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req Settings
		if !decode(w, r, &req) {
			return
		}
		out, err := s.Store.SaveSettings(req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
}

// Top is the phone remote's read-only view: the first n items, with
// confidential ones reduced to a placeholder title and no project.
type Top struct {
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	Project      string `json:"project,omitempty"`
	ProjectName  string `json:"project_name,omitempty"`
	Score        int    `json:"score"`
	Why          string `json:"why,omitempty"`
	Confidential bool   `json:"confidential"`
}

// PrivateTitle stands in for a confidential item's title off the desktop.
const PrivateTitle = "Confidential item"

// TopN returns the first n visible items, redacted for a device off this
// machine. Ids are left out: the phone has no action to take on them.
func (s *Service) TopN(ctx context.Context, n int) []Top {
	v := s.List(ctx)
	out := []Top{}
	for _, c := range v.Items {
		if len(out) == n {
			break
		}
		t := Top{Kind: c.Kind, Score: c.Score, Title: c.Title, Project: c.Project, ProjectName: c.ProjectName, Confidential: c.Confidential}
		if len(c.Parts) > 0 {
			t.Why = c.Parts[0].Why
			// An unblocking reason names other projects, which may be confidential.
			if c.Parts[0].Factor == "unblocking" {
				t.Why = "unblocks other projects"
			}
		}
		if c.Confidential {
			t.Title, t.Project, t.ProjectName, t.Why = PrivateTitle, "", "", ""
		}
		out = append(out, t)
	}
	return out
}
