package databases

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// fail maps an error to a status code.
func fail(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	switch {
	case errors.Is(err, ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, ErrNotSaved), errors.Is(err, ErrReadOnly), errors.Is(err, ErrManagerRW):
		code = http.StatusForbidden
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrEmptyQuery), errors.Is(err, ErrNoManager):
		code = http.StatusBadRequest
	case errors.Is(err, ErrExists):
		code = http.StatusConflict
	case errors.Is(err, docker.ErrNotFound):
		code = http.StatusNotFound
	}
	http.Error(w, err.Error(), code)
}

// Register adds the /api/databases routes to mux:
//
//	GET    /api/databases[?health=1]              discovered containers and saved connections
//	POST   /api/databases                         save a connection (password: env:NAME)
//	DELETE /api/databases/{id}                    remove a saved connection
//	GET    /api/databases/{id}/health             connect, ping, version, size
//	GET    /api/databases/{id}/schema             tables or collections with columns
//	POST   /api/databases/{id}/query              {"query": "..."}, read-only
//	GET    /api/databases/{id}/manager            state of the embedded manager
//	POST   /api/databases/{id}/manager/{start|stop}
//
// Every POST and DELETE needs X-Lucid-Confirm: yes, and so does the query
// route, since it reaches a database. No response carries a password: a
// saved connection shows its env:NAME reference only.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/databases", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		l, err := s.List(ctx, r.URL.Query().Get("health") == "1")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, l)
	})

	mux.HandleFunc("POST /api/databases", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var p Profile
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		if err := dec.Decode(&p); err != nil {
			http.Error(w, "the body must be a JSON connection", http.StatusBadRequest)
			return
		}
		saved, err := s.Store.Add(p)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusCreated, Saved{Profile: saved, PasswordSet: saved.Password == "" || saved.Password.Resolve(s.Getenv) != ""})
	})

	mux.HandleFunc("DELETE /api/databases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		id := r.PathValue("id")
		if err := s.Store.Remove(id); err != nil {
			fail(w, err)
			return
		}
		// A manager for a removed connection has nothing left to show.
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		_ = s.Managers.Stop(ctx, id)
		apiutil.WriteJSON(w, http.StatusOK, map[string]string{"id": id, "status": "removed"})
	})

	mux.HandleFunc("GET /api/databases/{id}/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		h, err := s.Health(ctx, r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, h)
	})

	mux.HandleFunc("GET /api/databases/{id}/schema", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		t, err := s.Schema(ctx, r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"tables": t})
	})

	mux.HandleFunc("POST /api/databases/{id}/query", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var body struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
			http.Error(w, `the body must be {"query": "..."}`, http.StatusBadRequest)
			return
		}
		res, err := s.Query(r.Context(), r.PathValue("id"), body.Query)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /api/databases/{id}/manager", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.profile(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		st, err := s.Managers.Status(ctx, p)
		if err != nil {
			http.Error(w, "docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, st)
	})

	mux.HandleFunc("POST /api/databases/{id}/manager/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		action := r.PathValue("action")
		if action != "start" && action != "stop" {
			http.Error(w, "action must be start or stop", http.StatusBadRequest)
			return
		}
		p, err := s.profile(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		// An image may need pulling; a request that goes away must not
		// leave a half-started container behind.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
		defer cancel()
		if action == "stop" {
			if err := s.Managers.Stop(ctx, p.ID); err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, ManagerStatus{Available: true})
			return
		}
		pw, err := s.password(p)
		if err != nil {
			fail(w, err)
			return
		}
		st, err := s.Managers.Start(ctx, p, pw)
		if err != nil {
			fail(w, scrubErr(err, pw))
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, st)
	})
}
