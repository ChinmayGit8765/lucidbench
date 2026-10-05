package work

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// MaxBody is the largest request body accepted by the work routes.
const MaxBody = 64 << 10

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrRefused):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrConflict):
		http.Error(w, err.Error(), http.StatusConflict)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Register adds the work routes to mux:
//
//	GET  /api/work/sessions               every session, newest first
//	POST /api/work/sessions               start {card?, project, prompt?, provider, profile?, harness}
//	GET  /api/work/sessions/{id}          one session with its diff summary
//	GET  /api/work/sessions/{id}/events   server-sent events: each normalised event, then "end"
//	GET  /api/work/sessions/{id}/raw      raw.log as text
//	POST /api/work/sessions/{id}/stop     stop the agent
//	POST /api/work/sessions/{id}/pr       push the branch and open a draft PR
//	POST /api/work/sessions/{id}/remove   remove the worktree {discard?}
//
// Every POST needs X-Lucid-Confirm.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/work/sessions", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.List())
	})
	mux.HandleFunc("POST /api/work/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req StartRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid session JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		se, err := s.Start(req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusCreated, se)
	})
	mux.HandleFunc("GET /api/work/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		se, err := s.Get(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, se)
	})
	mux.HandleFunc("GET /api/work/sessions/{id}/raw", func(w http.ResponseWriter, r *http.Request) {
		data, err := s.RawLog(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /api/work/sessions/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		stream(w, r, s, r.PathValue("id"))
	})
	action := func(path string, fn func(id string, r *http.Request) (Session, error)) {
		mux.HandleFunc("POST /api/work/sessions/{id}/"+path, func(w http.ResponseWriter, r *http.Request) {
			if !apiutil.Confirmed(w, r) {
				return
			}
			se, err := fn(r.PathValue("id"), r)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, se)
		})
	}
	action("stop", func(id string, _ *http.Request) (Session, error) { return s.Stop(id) })
	action("pr", func(id string, _ *http.Request) (Session, error) { return s.OpenPR(id) })
	action("remove", func(id string, r *http.Request) (Session, error) {
		var in struct {
			Discard bool `json:"discard"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, MaxBody)).Decode(&in); err != nil && err != io.EOF {
			return Session{}, fmt.Errorf("%w: invalid JSON: %v", ErrBadRequest, err)
		}
		return s.Remove(id, in.Discard)
	})
}

// stream sends a session's events as server-sent events. Each event's id is
// its index, so a reconnecting EventSource resumes where it left off. A
// "session" event carries the record whenever it changes; "end" follows once
// the run is over.
func stream(w http.ResponseWriter, r *http.Request, s *Service, id string) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	next := 0
	if v, err := strconv.Atoi(r.Header.Get("Last-Event-ID")); err == nil {
		next = v + 1
	}
	if _, _, _, err := s.Events(id, next); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, v any, eid int) bool {
		data, _ := json.Marshal(v)
		msg := ""
		if eid >= 0 {
			msg += "id: " + strconv.Itoa(eid) + "\n"
		}
		if event != "" {
			msg += "event: " + event + "\n"
		}
		if _, err := io.WriteString(w, msg+"data: "+string(data)+"\n\n"); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	sent := ""
	for {
		evs, running, changed, err := s.Events(id, next)
		if err != nil {
			return
		}
		for _, ev := range evs {
			if !send("", ev, next) {
				return
			}
			next++
		}
		// The record goes out when its status changes, not on every event.
		if se, err := s.Get(id); err == nil && (se.Status != sent || !running) {
			if !send("session", se, -1) {
				return
			}
			sent = se.Status
		}
		if !running {
			_, _ = io.WriteString(w, "event: end\ndata: \n\n")
			fl.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
