package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// RestartEnv is set in the environment of a daemon that replaced an earlier
// one. Its listener waits for the old process to let go of the port.
const RestartEnv = "LUCID_RESTARTED"

// Restarter replaces the running daemon with a fresh copy of itself, so a
// setting saved to config.yaml applies. Every step is a field, so tests
// run the whole path without stopping anything.
//
// On Windows there is no exec: the new process is started first (with
// RestartEnv, so it waits for the port) and the old one then shuts down
// and exits. A desktop app's sidecar stays in the app's kill-on-close job,
// since a child joins its parent's job. Elsewhere the old process shuts
// down and execs itself in place, keeping its PID (PID 1 in a container).
type Restarter struct {
	// Exe is the daemon's executable (os.Executable).
	Exe func() (string, error)
	// Args are the arguments after the program name; Env the environment.
	Args []string
	Env  []string
	// Shutdown closes the listeners and waits for requests in flight.
	Shutdown func(ctx context.Context) error
	// Start starts exe as a new process (Windows); Exec replaces this
	// process with it (elsewhere) and returns only on failure.
	Start func(exe string, args, env []string) error
	Exec  func(exe string, args, env []string) error
	// Exit ends this process.
	Exit func(code int)
	// InPlace is true where Exec is used instead of Start.
	InPlace bool
	// Delay lets the reply reach the browser before the listener closes.
	Delay time.Duration

	busy atomic.Bool
}

// errRestarting answers a second restart while the first is under way.
var errRestarting = errors.New("the engine is already restarting")

// Restart begins a restart. What can fail before the old daemon stops
// (finding the executable, starting the new process on Windows) is
// returned; the rest happens after Delay, on its own goroutine.
func (r *Restarter) Restart() error {
	if !r.busy.CompareAndSwap(false, true) {
		return errRestarting
	}
	exe, err := r.Exe()
	if err != nil {
		r.busy.Store(false)
		return err
	}
	env := append(slices.DeleteFunc(slices.Clone(r.Env), func(kv string) bool {
		return strings.HasPrefix(kv, RestartEnv+"=")
	}), RestartEnv+"=1")
	if !r.InPlace {
		if err := r.Start(exe, r.Args, env); err != nil {
			r.busy.Store(false)
			return err
		}
	}
	go func() {
		time.Sleep(r.Delay)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := r.Shutdown(ctx); err != nil {
			log.Print("restart: shutdown: ", err)
		}
		cancel()
		if r.InPlace {
			err := r.Exec(exe, r.Args, env)
			log.Print("restart: exec: ", err)
			r.Exit(1)
			return
		}
		r.Exit(0)
	}()
	return nil
}

// errNoRestart answers POST /api/engine/restart when the daemon was built
// without a Restarter (a handler in a test, an embedder).
var errNoRestart = errors.New("this engine cannot restart itself: stop it and start it again")

// restartHandler serves POST /api/engine/restart: with the confirm header,
// the daemon restarts and answers 202 first. The UI then waits for
// /api/health to report a new start time.
func restartHandler(restart func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !apiutil.Confirmed(w, r) {
			return
		}
		if restart == nil {
			http.Error(w, errNoRestart.Error(), http.StatusNotImplemented)
			return
		}
		if err := restart(); err != nil {
			http.Error(w, "could not restart: "+err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusAccepted, map[string]any{"restarting": true, "started": started.UTC().Format(time.RFC3339Nano)})
	}
}
