package boards

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// MaxBody is the largest request body accepted by the board routes.
const MaxBody = 64 << 10

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBoardNotFound), errors.Is(err, ErrCardNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrBadColumn), errors.Is(err, ErrBadInput):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		memory.Fail(w, err)
	}
}

// Register adds the board routes to mux:
//
//	GET  /api/boards                       board summaries
//	GET  /api/boards/{id}                  a board with its cards
//	POST /api/boards/{id}/cards            add a card
//	PUT  /api/boards/{id}/cards/{card}     update fields, or move with {column, index}
//
// POST and PUT need X-Lucid-Confirm. A PUT changes only the fields in its
// body. With an "index" it also moves the card to that position in "column"
// (the card's own column when none is given).
func Register(mux *http.ServeMux, open memory.Opener) {
	with := func(w http.ResponseWriter, fn func(v *memory.Vault)) {
		v, err := open()
		if err != nil {
			http.Error(w, "cannot open the vault: "+err.Error(), http.StatusInternalServerError)
			return
		}
		fn(v)
	}
	mux.HandleFunc("GET /api/boards", func(w http.ResponseWriter, r *http.Request) {
		with(w, func(v *memory.Vault) {
			list, err := List(v)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, list)
		})
	})
	mux.HandleFunc("GET /api/boards/{id}", func(w http.ResponseWriter, r *http.Request) {
		with(w, func(v *memory.Vault) {
			b, err := Get(v, r.PathValue("id"))
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, b)
		})
	})
	mux.HandleFunc("POST /api/boards/{id}/cards", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var c Card
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			http.Error(w, "invalid card JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		with(w, func(v *memory.Vault) {
			added, err := AddCard(v, r.PathValue("id"), c)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusCreated, added)
		})
	})
	mux.HandleFunc("PUT /api/boards/{id}/cards/{card}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		id, cardID := r.PathValue("id"), r.PathValue("card")
		with(w, func(v *memory.Vault) {
			b, err := Get(v, id)
			if err != nil {
				fail(w, err)
				return
			}
			var in struct {
				Card
				Index *int `json:"index"`
			}
			for _, c := range b.Cards {
				if c.ID == cardID {
					in.Card = c // fields missing from the body keep their value
				}
			}
			if in.ID == "" {
				fail(w, ErrCardNotFound)
				return
			}
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				http.Error(w, "invalid card JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			in.ID = cardID
			col := in.Column
			if in.Index != nil {
				// Update the fields where the card is, then place it.
				in.Column = ""
				if err := UpdateCard(v, id, in.Card); err != nil {
					fail(w, err)
					return
				}
				if err := MoveCard(v, id, cardID, col, *in.Index); err != nil {
					fail(w, err)
					return
				}
			} else if err := UpdateCard(v, id, in.Card); err != nil {
				fail(w, err)
				return
			}
			b, err = Get(v, id)
			if err != nil {
				fail(w, err)
				return
			}
			for _, c := range b.Cards {
				if c.ID == cardID {
					apiutil.WriteJSON(w, http.StatusOK, c)
					return
				}
			}
			fail(w, ErrCardNotFound)
		})
	})
}
