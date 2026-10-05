package council

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// MaxBody is the largest request body accepted by the council routes.
const MaxBody = 64 << 10

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrConfidential):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrBadRequest), errors.Is(err, boards.ErrBadInput), errors.Is(err, boards.ErrBadColumn):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrBusy), errors.Is(err, ErrApproved):
		http.Error(w, err.Error(), http.StatusConflict)
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

// Register adds the council routes to mux:
//
//	GET  /api/council/sessions                 sessions, newest first
//	POST /api/council/sessions                 start {input, project?, proposer?, critics?, rounds?}
//	GET  /api/council/sessions/{id}            one session (poll)
//	GET  /api/council/sessions/{id}/events     live snapshots (SSE)
//	POST /api/council/sessions/{id}/approve    {project?}; returns the new card
//	POST /api/council/sessions/{id}/again      {notes}; one more round with the notes
//
// POST needs X-Lucid-Confirm. A start or an "again" answers at once with the
// session; the run continues in the background.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/council/sessions", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.List()
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, list)
	})
	mux.HandleFunc("POST /api/council/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in StartRequest
		if !decode(w, r, &in) {
			return
		}
		sess, err := s.Begin(in)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusAccepted, sess)
	})
	mux.HandleFunc("GET /api/council/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.Get(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, sess)
	})
	mux.HandleFunc("GET /api/council/sessions/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		events(s, w, r)
	})
	mux.HandleFunc("POST /api/council/sessions/{id}/approve", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct {
			Project string `json:"project"`
		}
		if r.ContentLength != 0 && !decode(w, r, &in) {
			return
		}
		card, err := s.Approve(r.PathValue("id"), in.Project)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, card)
	})
	mux.HandleFunc("POST /api/council/sessions/{id}/again", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in struct {
			Notes string `json:"notes"`
		}
		if !decode(w, r, &in) {
			return
		}
		sess, err := s.BeginAgain(r.PathValue("id"), in.Notes)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusAccepted, sess)
	})
}

// events streams the session as Server-Sent Events: an "session" event with
// the whole session after every change, then "end" when the run is over.
func events(s *Service, w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	// Subscribe first, so nothing between the snapshot and the stream is lost.
	ch, cancel := s.subscribe(id)
	defer cancel()
	sess, err := s.Get(id)
	if err != nil {
		fail(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	send := func(event string, data []byte) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		flusher.Flush()
	}
	first, _ := json.Marshal(sess)
	send("session", first)
	if sess.Status != StatusRunning {
		send("end", []byte(`{}`))
		return
	}
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case u, ok := <-ch:
			if !ok {
				// The run ended; the last snapshot may have been dropped.
				if last, err := s.Get(id); err == nil {
					data, _ := json.Marshal(last)
					send("session", data)
				}
				send("end", []byte(`{}`))
				return
			}
			send("session", u.data)
			if u.done {
				send("end", []byte(`{}`))
				return
			}
		}
	}
}
