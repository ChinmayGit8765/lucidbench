package k8s

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Service serves the /api/k8s routes.
type Service struct {
	// Connect returns a clientset for the local cluster.
	Connect func() (kubernetes.Interface, error)
}

// EventLimit is how many events GET /api/k8s/events returns.
const EventLimit = 50

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$`)

// Register adds the /api/k8s routes to mux:
//
//	GET    /api/k8s/namespaces
//	GET    /api/k8s/nodes
//	GET    /api/k8s/pods[?namespace=ns]
//	GET    /api/k8s/jobs[?namespace=ns]
//	GET    /api/k8s/events[?namespace=ns]                 newest 50
//	GET    /api/k8s/pods/{ns}/{name}/logs?tail=200[&container=c][&follow=1]
//	DELETE /api/k8s/jobs/{ns}/{name}                      finished jobs only
//
// Every route answers 503 when the cluster is unreachable.
func Register(mux *http.ServeMux, s *Service) {
	read := func(path string, fn func(ctx context.Context, c *Client, ns string) (any, error)) {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			ns := r.URL.Query().Get("namespace")
			if ns != "" && !nameRE.MatchString(ns) {
				http.Error(w, "invalid namespace", http.StatusBadRequest)
				return
			}
			c, ok := s.client(w)
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			v, err := fn(ctx, c, ns)
			if err != nil {
				unavailable(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, v)
		})
	}
	read("/api/k8s/namespaces", func(ctx context.Context, c *Client, _ string) (any, error) { return c.Namespaces(ctx) })
	read("/api/k8s/nodes", func(ctx context.Context, c *Client, _ string) (any, error) { return c.Nodes(ctx) })
	read("/api/k8s/pods", func(ctx context.Context, c *Client, ns string) (any, error) { return c.Pods(ctx, ns) })
	read("/api/k8s/jobs", func(ctx context.Context, c *Client, ns string) (any, error) { return c.Jobs(ctx, ns) })
	read("/api/k8s/events", func(ctx context.Context, c *Client, ns string) (any, error) {
		return c.Events(ctx, ns, EventLimit)
	})

	mux.HandleFunc("GET /api/k8s/pods/{ns}/{name}/logs", func(w http.ResponseWriter, r *http.Request) {
		ns, name, q := r.PathValue("ns"), r.PathValue("name"), r.URL.Query()
		container := q.Get("container")
		if !nameRE.MatchString(ns) || !nameRE.MatchString(name) || (container != "" && !nameRE.MatchString(container)) {
			http.Error(w, "invalid pod reference", http.StatusBadRequest)
			return
		}
		tail := int64(200)
		if t := q.Get("tail"); t != "" {
			n, err := strconv.ParseInt(t, 10, 64)
			if err != nil || n < 1 || n > 5000 {
				http.Error(w, "tail must be 1-5000", http.StatusBadRequest)
				return
			}
			tail = n
		}
		c, ok := s.client(w)
		if !ok {
			return
		}
		follow := q.Get("follow") == "1"
		ctx := r.Context()
		if !follow {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
		}
		rc, err := c.Logs(ctx, ns, name, container, tail, follow)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		defer rc.Close()
		if !follow {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.Copy(w, rc)
			return
		}
		stream(w, rc)
	})

	mux.HandleFunc("DELETE /api/k8s/jobs/{ns}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		ns, name := r.PathValue("ns"), r.PathValue("name")
		if !nameRE.MatchString(ns) || !nameRE.MatchString(name) {
			http.Error(w, "invalid job reference", http.StatusBadRequest)
			return
		}
		c, ok := s.client(w)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		err := c.DeleteJob(ctx, ns, name)
		switch {
		case errors.Is(err, ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, ErrNotFinished):
			http.Error(w, err.Error(), http.StatusConflict)
		case err != nil:
			unavailable(w, err)
		default:
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"namespace": ns, "name": name, "status": "deleted"})
		}
	})
}

func (s *Service) client(w http.ResponseWriter) (*Client, bool) {
	cs, err := s.Connect()
	if err != nil {
		unavailable(w, err)
		return nil, false
	}
	return &Client{CS: cs}, true
}

// stream relays a log as server-sent events, one event per line.
func stream(w http.ResponseWriter, rc io.Reader) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if _, err := io.WriteString(w, "data: "+strings.ReplaceAll(sc.Text(), "\r", "")+"\n\n"); err != nil {
			return
		}
		fl.Flush()
	}
	_, _ = io.WriteString(w, "event: end\ndata: \n\n")
	fl.Flush()
}

func unavailable(w http.ResponseWriter, err error) {
	msg := "local cluster unavailable: " + err.Error()
	if errors.Is(err, ErrNoCluster) {
		msg = ErrNoCluster.Error()
	}
	http.Error(w, msg, http.StatusServiceUnavailable)
}
