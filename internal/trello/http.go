package trello

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
)

const (
	serviceName = "Trello"
	maxBody     = 16 << 10
	callTime    = 45 * time.Second
)

func fail(w http.ResponseWriter, err error) { extapi.Fail(w, serviceName, err) }

// MoveRequest is the body of PUT /api/trello/cards/{id}.
type MoveRequest struct {
	List string `json:"list"`
}

// LinkRequest is the body of POST /api/trello/link: attach a Trello card (URL,
// short link or id) to a native card.
type LinkRequest struct {
	Board  string `json:"board"`
	Card   string `json:"card"`
	Trello string `json:"trello"`
}

// LinkResult is what a link answers with.
type LinkResult struct {
	Card   boards.Card `json:"card"`
	Remote Card        `json:"remote"`
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
//	GET  /api/trello/status        whether a key and token are set
//	GET  /api/trello/boards        the member's open boards
//	GET  /api/trello/boards/{id}   a board's lists and cards
//	POST /api/trello/cards         add a card {list, name, desc}
//	PUT  /api/trello/cards/{id}    move a card {list}
//	POST /api/trello/link          link a native card to a Trello card
//
// Every POST and PUT needs X-Lucid-Confirm. Reads are cached for a minute.
func Register(mux *http.ServeMux, s *Service) {
	ctx := func(r *http.Request) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), callTime)
	}
	mux.HandleFunc("GET /api/trello/status", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.Status())
	})
	mux.HandleFunc("GET /api/trello/boards", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := ctx(r)
		defer cancel()
		b, err := s.Boards(c)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, b)
	})
	mux.HandleFunc("GET /api/trello/boards/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := ctx(r)
		defer cancel()
		b, err := s.Board(c, r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, b)
	})
	mux.HandleFunc("POST /api/trello/cards", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in NewCard
		if !decode(w, r, &in) {
			return
		}
		c, cancel := ctx(r)
		defer cancel()
		card, err := s.AddCard(c, in)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusCreated, card)
	})
	mux.HandleFunc("PUT /api/trello/cards/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in MoveRequest
		if !decode(w, r, &in) {
			return
		}
		c, cancel := ctx(r)
		defer cancel()
		card, err := s.MoveCard(c, r.PathValue("id"), in.List)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, card)
	})
	mux.HandleFunc("POST /api/trello/link", func(w http.ResponseWriter, r *http.Request) {
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

// Link attaches a Trello card to a native card, storing its id and URL. The
// second return is an HTTP status for failures that are not the remote's.
func (s *Service) Link(ctx context.Context, in LinkRequest) (*LinkResult, int, error) {
	v, err := s.Open()
	if err != nil {
		return nil, http.StatusInternalServerError, errors.New("cannot open the vault")
	}
	b, err := boards.Get(v, in.Board)
	if err != nil {
		if errors.Is(err, boards.ErrBoardNotFound) {
			return nil, http.StatusNotFound, err
		}
		return nil, http.StatusBadRequest, err
	}
	var card *boards.Card
	for i := range b.Cards {
		if b.Cards[i].ID == in.Card {
			card = &b.Cards[i]
		}
	}
	if card == nil {
		return nil, http.StatusNotFound, boards.ErrCardNotFound
	}
	remote, err := s.Card(ctx, in.Trello)
	if err != nil {
		if errors.Is(err, extapi.ErrBadRequest) {
			return nil, http.StatusBadRequest, ErrBadCardRef
		}
		return nil, 0, err
	}
	card.Trello, card.TrelloURL = remote.ID, remote.URL
	if err := boards.UpdateCard(v, in.Board, *card); err != nil {
		return nil, http.StatusInternalServerError, errors.New("could not save the link on the card")
	}
	return &LinkResult{Card: *card, Remote: *remote}, 0, nil
}
