package team

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/mcp"
)

// API serves the team routes.
type API struct {
	Store *Store
	// Accounts and MCP describe this machine for Check; nil skips that part.
	Accounts func() []accounts.Profile
	MCP      func() mcp.Matrix
	// Estimate is built per request (it reads the records); nil skips budgets.
	Estimate func() func(role, provider string) (float64, int)
}

// View is a team with everything found about it. Its Problems hold the read
// problems, the schema errors and Check's warnings, in that order.
type View struct {
	*Resolved
	Estimates []Estimate `json:"estimates"`
}

func (a *API) reality(confidential bool, project string) Reality {
	r := Reality{Confidential: confidential, Project: project}
	if a.Accounts != nil {
		r.Accounts = a.Accounts()
	}
	if a.MCP != nil {
		r.MCP = a.MCP()
	}
	if a.Estimate != nil {
		r.Estimate = a.Estimate()
	}
	return r
}

// view checks a resolved team against the schema and this machine.
func (a *API) view(res *Resolved) View {
	ps := append([]Problem{}, res.Problems...)
	ps = append(ps, Validate(res.Team)...)
	more, ests := Check(res.Team, a.reality(res.Confidential, res.ProjectName))
	res.Problems = append(ps, more...)
	return View{Resolved: res, Estimates: ests}
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrTarget):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ValidateRequest is the body of POST /api/team/validate: a YAML text (an
// import) or a team as JSON (the editor), and optionally the project it is for.
type ValidateRequest struct {
	YAML    string          `json:"yaml,omitempty"`
	Team    json.RawMessage `json:"team,omitempty"`
	Project string          `json:"project,omitempty"`
}

// ValidateResponse is a parsed team and its problems; Team is null when the
// input could not be read at all.
type ValidateResponse struct {
	Team      *Team      `json:"team"`
	Problems  []Problem  `json:"problems"`
	Estimates []Estimate `json:"estimates"`
	Valid     bool       `json:"valid"`
}

// Register adds the team routes to mux:
//
//	GET  /api/projects/{id}/team   the project's team, where it came from, where it can be saved, its problems and cost estimates
//	PUT  /api/projects/{id}/team   {team, target: repo|data} saves it (needs X-Lucid-Confirm); a schema error is 400
//	POST /api/team/validate        {yaml | team, project?} parses and checks without saving
//	GET  /api/team/default         the config default (or the built-in one)
func Register(mux *http.ServeMux, a *API) {
	mux.HandleFunc("GET /api/projects/{id}/team", func(w http.ResponseWriter, r *http.Request) {
		res, err := a.Store.Resolve(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, a.view(res))
	})
	mux.HandleFunc("PUT /api/projects/{id}/team", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct {
			Team   json.RawMessage `json:"team"`
			Target string          `json:"target"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		t, err := ParseJSON(in.Team)
		if err != nil {
			http.Error(w, "invalid team: "+err.Error(), http.StatusBadRequest)
			return
		}
		res, err := a.Store.Save(r.PathValue("id"), t, in.Target)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, a.view(res))
	})
	mux.HandleFunc("POST /api/team/validate", func(w http.ResponseWriter, r *http.Request) {
		var in ValidateRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*MaxBytes))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		var t Team
		var err error
		switch {
		case strings.TrimSpace(in.YAML) != "":
			t, err = Parse([]byte(in.YAML))
		case len(in.Team) > 0:
			t, err = ParseJSON(in.Team)
		default:
			http.Error(w, "send yaml or team", http.StatusBadRequest)
			return
		}
		out := ValidateResponse{Problems: []Problem{}, Estimates: []Estimate{}}
		if err != nil {
			out.Problems = append(out.Problems, Problem{Severity: SevError, Code: "parse", Message: err.Error()})
			apiutil.WriteJSON(w, http.StatusOK, out)
			return
		}
		t = Normalise(t)
		out.Team = &t
		out.Problems = append(out.Problems, Validate(t)...)
		confidential, name := false, ""
		if in.Project != "" {
			p, err := a.Store.project(in.Project)
			if err != nil {
				fail(w, err)
				return
			}
			confidential, name = p.Visibility == "confidential", p.Name
		}
		more, ests := Check(t, a.reality(confidential, name))
		out.Problems = append(out.Problems, more...)
		out.Estimates = ests
		out.Valid = !HasErrors(out.Problems)
		apiutil.WriteJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("GET /api/team/default", func(w http.ResponseWriter, r *http.Request) {
		t, source, ps := a.Store.DefaultTeam()
		if ps == nil {
			ps = []Problem{}
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"team": t, "source": source, "problems": ps})
	})
}
