package power

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// Register adds the /api/power routes to mux:
//
//	GET  /api/power                                the state of every managed thing and recent activity
//	POST /api/power/{kind}/{name}/{start|stop}     kind: cluster, runner, stack
//	POST /api/power/sleep                          sleep everything idle (on-demand things only)
//
// Every POST needs X-Lucid-Confirm: yes. Starting the cluster answers 202 at
// once; GET /api/power shows it "starting" until it is ready or failed.
func Register(mux *http.ServeMux, s *Supervisor) {
	mux.HandleFunc("GET /api/power", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		apiutil.WriteJSON(w, http.StatusOK, s.State(ctx))
	})

	mux.HandleFunc("POST /api/power/sleep", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{"outcomes": s.SleepIdle(ctx)})
	})

	mux.HandleFunc("POST /api/power/{kind}/{name}/{action}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		kind, name, action := r.PathValue("kind"), r.PathValue("name"), r.PathValue("action")
		if action != "start" && action != "stop" {
			http.Error(w, "action must be start or stop", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		var err error
		switch kind {
		case "cluster":
			if name != s.ClusterName {
				http.Error(w, "no cluster named "+name, http.StatusNotFound)
				return
			}
			if action == "start" {
				s.MarkStarting()
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), s.startTimeout()+30*time.Second)
					defer cancel()
					if err := s.StartCluster(ctx, false, "started from Lucidbench"); err != nil {
						log.Printf("power: start cluster: %v", err)
					}
				}()
				apiutil.WriteJSON(w, http.StatusAccepted, map[string]string{"kind": kind, "name": name, "state": "starting"})
				return
			}
			err = s.StopCluster(ctx, false, "stopped from Lucidbench")
		case "runner":
			if action == "start" {
				err = s.runnerAction(ctx, name, "start", false, "started from Lucidbench")
			} else {
				err = s.StopRunner(ctx, name)
			}
		case "stack":
			_, err = s.StackAction(ctx, name, action, false, map[string]string{"start": "started", "stop": "stopped"}[action]+" from Lucidbench")
		default:
			http.Error(w, "kind must be cluster, runner or stack", http.StatusNotFound)
			return
		}
		switch {
		case err == nil:
			apiutil.WriteJSON(w, http.StatusOK, map[string]string{"kind": kind, "name": name, "action": action, "status": "ok"})
		case errors.Is(err, ErrClusterBusy), errors.Is(err, ErrRunnerBusy):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, ErrNoCluster), errors.Is(err, ErrUnknownStack), errors.Is(err, ci.ErrNotRunner), errors.Is(err, docker.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		case errors.Is(err, docker.ErrRefused):
			http.Error(w, "refused: "+err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	})
}

func (s *Supervisor) startTimeout() time.Duration {
	if s.StartTimeout > 0 {
		return s.StartTimeout
	}
	return DefaultStartTimeout
}
