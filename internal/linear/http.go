package linear

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
)

const (
	serviceName = "Linear"
	maxBody     = 16 << 10
	callTime    = 45 * time.Second
)

func fail(w http.ResponseWriter, err error) { extapi.Fail(w, serviceName, err) }

// PromoteRequest is the body of POST /api/linear/promote: create an issue
// from a native card and link the two.
type PromoteRequest struct {
	Board   string `json:"board"`
	Card    string `json:"card"`
	Team    string `json:"team"`
	Project string `json:"project,omitempty"`
}

// LinkRequest is the body of POST /api/linear/link: attach an existing issue
// (identifier or URL) to a native card.
type LinkRequest struct {
	Board      string `json:"board"`
	Card       string `json:"card"`
	Identifier string `json:"identifier"`
}

// PromoteResult is what a promote answers with.
type PromoteResult struct {
	Card  boards.Card `json:"card"`
	Issue Issue       `json:"issue"`
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// Register adds the routes to mux:
//
//	GET  /api/linear/status                 whether a key is set, whether an MCP server exists
//	GET  /api/linear/meta                   viewer, teams (states, active cycle), projects
//	GET  /api/linear/issues                 ?team= &project= &me=1 &done=1
//	GET  /api/linear/issues/{identifier}    one issue
//	POST /api/linear/issues                 create an issue {team, project, title, description}
//	POST /api/linear/promote                create an issue from a card and link it
//	POST /api/linear/link                   link a card to an existing issue
//
// Every POST needs X-Lucid-Confirm. Reads are cached for a minute.
func Register(mux *http.ServeMux, s *Service) {
	ctx := func(r *http.Request) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), callTime)
	}
	mux.HandleFunc("GET /api/linear/status", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.Status())
	})
	mux.HandleFunc("GET /api/linear/meta", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := ctx(r)
		defer cancel()
		m, err := s.Meta(c)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, m)
	})
	mux.HandleFunc("GET /api/linear/issues", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f := Filter{Team: q.Get("team"), Project: q.Get("project"), Me: q.Get("me") == "1", Done: q.Get("done") == "1"}
		c, cancel := ctx(r)
		defer cancel()
		l, err := s.Issues(c, f)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, l)
	})
	mux.HandleFunc("GET /api/linear/issues/{identifier}", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := ctx(r)
		defer cancel()
		i, err := s.Issue(c, r.PathValue("identifier"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, i)
	})
	mux.HandleFunc("POST /api/linear/issues", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in NewIssue
		if !decode(w, r, &in) {
			return
		}
		c, cancel := ctx(r)
		defer cancel()
		i, err := s.CreateIssue(c, in)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusCreated, i)
	})
	mux.HandleFunc("POST /api/linear/promote", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in PromoteRequest
		if !decode(w, r, &in) {
			return
		}
		c, cancel := ctx(r)
		defer cancel()
		res, status, err := s.Promote(c, in)
		if err != nil {
			if status != 0 {
				http.Error(w, err.Error(), status)
				return
			}
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/linear/link", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in LinkRequest
		if !decode(w, r, &in) {
			return
		}
		c, cancel := ctx(r)
		defer cancel()
		res, status, err := s.Link(c, in)
		if err != nil {
			if status != 0 {
				http.Error(w, err.Error(), status)
				return
			}
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
}

// findCard returns the board's card, or an error with the status to answer.
func (s *Service) findCard(board, id string) (boards.Card, int, error) {
	v, err := s.Open()
	if err != nil {
		return boards.Card{}, http.StatusInternalServerError, errors.New("cannot open the vault")
	}
	b, err := boards.Get(v, board)
	if err != nil {
		if errors.Is(err, boards.ErrBoardNotFound) {
			return boards.Card{}, http.StatusNotFound, err
		}
		return boards.Card{}, http.StatusBadRequest, err
	}
	for _, c := range b.Cards {
		if c.ID == id {
			return c, 0, nil
		}
	}
	return boards.Card{}, http.StatusNotFound, boards.ErrCardNotFound
}

// save writes the link fields onto the card.
func (s *Service) save(board string, c boards.Card, i *Issue) (boards.Card, error) {
	v, err := s.Open()
	if err != nil {
		return c, err
	}
	c.Linear, c.LinearURL = i.Identifier, i.URL
	if err := boards.UpdateCard(v, board, c); err != nil {
		return c, err
	}
	return c, nil
}

// Promote creates a Linear issue titled like the card and stores its
// identifier and URL on the card. A card that already links an issue is
// refused, so a second click never creates a duplicate. The second return is
// an HTTP status for failures that are not the remote's.
func (s *Service) Promote(ctx context.Context, in PromoteRequest) (*PromoteResult, int, error) {
	card, status, err := s.findCard(in.Board, in.Card)
	if err != nil {
		return nil, status, err
	}
	if card.Linear != "" {
		return nil, http.StatusConflict, errors.New("this card is already linked to " + card.Linear)
	}
	if strings.TrimSpace(in.Team) == "" {
		return nil, http.StatusBadRequest, errors.New("pick a team")
	}
	issue, err := s.CreateIssue(ctx, NewIssue{
		Team: in.Team, Project: in.Project, Title: card.Title,
		Description: "Created from a Lucidbench card.",
	})
	if err != nil {
		return nil, 0, err
	}
	saved, err := s.save(in.Board, card, issue)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("created " + issue.Identifier + " in Linear but could not save it on the card; link it from the card")
	}
	return &PromoteResult{Card: saved, Issue: *issue}, 0, nil
}

// Link attaches an existing issue to a card.
func (s *Service) Link(ctx context.Context, in LinkRequest) (*PromoteResult, int, error) {
	card, status, err := s.findCard(in.Board, in.Card)
	if err != nil {
		return nil, status, err
	}
	issue, err := s.Issue(ctx, in.Identifier)
	if err != nil {
		if errors.Is(err, extapi.ErrBadRequest) {
			return nil, http.StatusBadRequest, ErrBadIdentifier
		}
		return nil, 0, err
	}
	saved, err := s.save(in.Board, card, issue)
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("could not save the link on the card")
	}
	return &PromoteResult{Card: saved, Issue: *issue}, 0, nil
}
