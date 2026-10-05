package docker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Service serves the /api/docker routes.
type Service struct {
	Docker Func
	Policy Policy
}

// ContainersResponse is the body of GET /api/docker/containers.
type ContainersResponse struct {
	Groups  []Group `json:"groups"`
	Total   int     `json:"total"`
	Running int     `json:"running"`
}

// Register adds the /api/docker routes to mux:
//
//	GET  /api/docker/containers
//	GET  /api/docker/stats
//	GET  /api/docker/images
//	GET  /api/docker/volumes
//	GET  /api/docker/containers/{name}/logs?tail=200[&follow=1]   follow streams SSE
//	POST /api/docker/containers/{name}/{action}                    start, stop, restart
//
// Every route answers 503 when the docker engine is unreachable.
func Register(mux *http.ServeMux, s *Service) {
	read := func(path string, fn func(ctx context.Context) (any, error)) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			v, err := fn(ctx)
			if err != nil {
				http.Error(w, "docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, v)
		})
	}
	read("/api/docker/containers", func(ctx context.Context) (any, error) {
		cs, err := List(ctx, s.Docker, s.Policy)
		if err != nil {
			return nil, err
		}
		up := 0
		for _, c := range cs {
			if c.State == "running" {
				up++
			}
		}
		return ContainersResponse{Groups: GroupContainers(cs), Total: len(cs), Running: up}, nil
	})
	read("/api/docker/stats", func(ctx context.Context) (any, error) { return Stats(ctx, s.Docker) })
	read("/api/docker/images", func(ctx context.Context) (any, error) { return Images(ctx, s.Docker) })
	read("/api/docker/volumes", func(ctx context.Context) (any, error) { return Volumes(ctx, s.Docker) })

	mux.HandleFunc("GET /api/docker/containers/{name}/logs", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		tail := 200
		if t := r.URL.Query().Get("tail"); t != "" {
			n, err := strconv.Atoi(t)
			if err != nil || n < 1 || n > 5000 {
				http.Error(w, "tail must be 1-5000", http.StatusBadRequest)
				return
			}
			tail = n
		}
		if !NameRE.MatchString(name) {
			http.Error(w, ErrNotFound.Error(), http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("follow") == "1" {
			follow(w, r, name, tail)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		out, err := Logs(ctx, name, tail)
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(out)
		}
	})

	mux.HandleFunc("POST /api/docker/containers/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		name, action := r.PathValue("name"), r.PathValue("action")
		err := Act(ctx, s.Docker, s.Policy, name, action)
		switch {
		case errors.Is(err, ErrBadAction):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, ErrRefused):
			http.Error(w, "refused: "+err.Error(), http.StatusForbidden)
		case errors.Is(err, ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case err != nil:
			http.Error(w, err.Error(), http.StatusBadGateway)
		default:
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"name": name, "action": action, "status": "ok"})
		}
	})
}

// follow streams `docker logs --follow` as server-sent events, one event per
// line, until the client goes away or the container stops.
func follow(w http.ResponseWriter, r *http.Request, name string, tail int) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	cmd := exec.CommandContext(r.Context(), "docker", LogArgs(name, tail, true)...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		http.Error(w, "docker unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	go func() { pw.CloseWithError(cmd.Wait()) }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.ReplaceAll(sc.Text(), "\r", "")
		if _, err := io.WriteString(w, "data: "+line+"\n\n"); err != nil {
			return
		}
		fl.Flush()
	}
	_, _ = io.WriteString(w, "event: end\ndata: \n\n")
	fl.Flush()
}
