package browser

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// MaxBody is the largest request body the browser routes accept.
const MaxBody = 16 << 10

func fail(w http.ResponseWriter, err error) {
	var rpc *RPCError
	code := http.StatusBadGateway
	switch {
	case errors.Is(err, ErrURL), errors.Is(err, ErrInput):
		code = http.StatusBadRequest
	case errors.Is(err, ErrNotRunning):
		code = http.StatusConflict
	case errors.Is(err, ErrNoTab):
		code = http.StatusNotFound
	case errors.As(err, &rpc):
		code = http.StatusBadGateway
	}
	http.Error(w, err.Error(), code)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// Register adds the /api/browser routes to mux:
//
//	GET  /api/browser                       state (sleeping | starting | running), the tabs and the DevTools address
//	POST /api/browser/start                 start the container (pulls the image the first time)
//	POST /api/browser/stop                  stop it
//	POST /api/browser/navigate              {url, target?, action?: go|back|forward|reload|close, new_tab?}
//	GET  /api/browser/stream?target=<id>    server-sent events: a "frame" event per JPEG frame
//	POST /api/browser/input                 {takeover: true, target?, type: move|down|up|click|scroll|key|text, ...}
//	POST /api/browser/screenshot            {target?, session?} a PNG; with a session it joins that Work session's timeline
//	GET  /api/browser/shots/{name}          a screenshot saved for a Work session
//
// Every POST needs X-Lucid-Confirm: yes. Only http and https URLs open.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/browser", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		st, err := s.Status(ctx)
		if err != nil {
			// docker is not there or not answering: say so, still as a state.
			st.Note = err.Error()
		}
		apiutil.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/browser/start", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		// The first start pulls the image, which can take a while.
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		st, err := s.Start(ctx)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/browser/stop", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		if err := s.Stop(ctx); err != nil {
			fail(w, err)
			return
		}
		st, _ := s.Status(ctx)
		apiutil.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/browser/navigate", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req NavRequest
		if !decode(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		res, err := s.Navigate(ctx, req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/browser/input", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req struct {
			Input
			// Takeover must be true: the page sends it only while the user has
			// switched take-over on.
			Takeover bool   `json:"takeover"`
			Target   string `json:"target,omitempty"`
		}
		if !decode(w, r, &req) {
			return
		}
		if !req.Takeover {
			http.Error(w, "input is only accepted while taking over the browser", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := s.Input(ctx, req.Target, req.Input); err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/browser/screenshot", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req struct {
			Target  string `json:"target,omitempty"`
			Session string `json:"session,omitempty"`
		}
		if !decode(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		sh, err := s.Screenshot(ctx, req.Target, req.Session)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, sh)
	})
	mux.HandleFunc("GET /api/browser/shots/{name}", func(w http.ResponseWriter, r *http.Request) {
		f, err := s.OpenShot(r.PathValue("name"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.Copy(w, f)
	})
	mux.HandleFunc("GET /api/browser/stream", func(w http.ResponseWriter, r *http.Request) {
		stream(w, r, s)
	})
}

// stream is the server-sent events handler: one "frame" event per JPEG frame,
// an "error" event if the stream ends because of a failure.
func stream(w http.ResponseWriter, r *http.Request, s *Service) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// Check the browser before committing to a stream response.
	if _, err := s.endpoint(r.Context()); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, v any) error {
		data, _ := json.Marshal(v)
		if _, err := io.WriteString(w, "event: "+event+"\ndata: "+string(data)+"\n\n"); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}
	if err := s.Stream(r.Context(), r.URL.Query().Get("target"), func(f Frame) error { return send("frame", f) }); err != nil && r.Context().Err() == nil {
		_ = send("error", map[string]string{"error": err.Error()})
	}
}
