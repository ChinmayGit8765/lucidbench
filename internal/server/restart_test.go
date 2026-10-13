package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// The route needs POST and the confirm header, and says plainly when the
// daemon cannot restart itself.
func TestRestartRouteNeedsConfirm(t *testing.T) {
	calls := 0
	h := NewWith(config.Default(), Deps{Restart: func() error { calls++; return nil }})
	do := func(method string, confirm bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/engine/restart", nil)
		if confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := do(http.MethodGet, true); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", rec.Code)
	}
	if rec := do(http.MethodPost, false); rec.Code != http.StatusForbidden || calls != 0 {
		t.Errorf("without confirm: %d, %d calls", rec.Code, calls)
	}
	rec := do(http.MethodPost, true)
	if rec.Code != http.StatusAccepted || calls != 1 || !strings.Contains(rec.Body.String(), `"restarting":true`) {
		t.Errorf("with confirm: %d %s, %d calls", rec.Code, rec.Body, calls)
	}

	// A failure before anything stops is reported, and nil means 501.
	h = NewWith(config.Default(), Deps{Restart: func() error { return errors.New("no exe") }})
	if rec := do(http.MethodPost, true); rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "no exe") {
		t.Errorf("failing restart: %d %s", rec.Code, rec.Body)
	}
	h = New()
	if rec := do(http.MethodPost, true); rec.Code != http.StatusNotImplemented {
		t.Errorf("no restarter: %d", rec.Code)
	}
}

// restartRig records the order of a Restarter's steps.
type restartRig struct {
	mu    sync.Mutex
	steps []string
	env   []string
	done  chan int
}

func (g *restartRig) add(s string) {
	g.mu.Lock()
	g.steps = append(g.steps, s)
	g.mu.Unlock()
}

func (g *restartRig) restarter(inPlace bool, startErr error) *Restarter {
	g.done = make(chan int, 1)
	return &Restarter{
		Exe:  func() (string, error) { return "/opt/lucidd", nil },
		Args: []string{"--flag"},
		Env:  []string{"HOME=/h", RestartEnv + "=1"},
		Shutdown: func(context.Context) error {
			g.add("shutdown")
			return nil
		},
		Start: func(exe string, args, env []string) error {
			g.add("start " + exe + " " + strings.Join(args, " "))
			g.env = env
			return startErr
		},
		Exec: func(exe string, args, env []string) error {
			g.add("exec " + exe + " " + strings.Join(args, " "))
			g.env = env
			return errors.New("exec failed")
		},
		Exit:    func(code int) { g.done <- code },
		InPlace: inPlace,
	}
}

func (g *restartRig) wait(t *testing.T) int {
	t.Helper()
	select {
	case code := <-g.done:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("the restart never exited")
		return -1
	}
}

// Windows: the new process starts first (marked, so it waits for the
// port), then the old one shuts down and exits cleanly. A second restart
// while one is under way is refused.
func TestRestarterStartsThenExits(t *testing.T) {
	g := &restartRig{}
	r := g.restarter(false, nil)
	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	if err := r.Restart(); !errors.Is(err, errRestarting) {
		t.Errorf("second restart: %v", err)
	}
	if code := g.wait(t); code != 0 {
		t.Errorf("exit %d", code)
	}
	if want := []string{"start /opt/lucidd --flag", "shutdown"}; !slices.Equal(g.steps, want) {
		t.Errorf("steps %v, want %v", g.steps, want)
	}
	if !slices.Equal(g.env, []string{"HOME=/h", RestartEnv + "=1"}) {
		t.Errorf("env %v", g.env)
	}
}

// When the new process cannot start, nothing stops and the error comes back.
func TestRestarterKeepsRunningWhenStartFails(t *testing.T) {
	g := &restartRig{}
	r := g.restarter(false, errors.New("access denied"))
	if err := r.Restart(); err == nil || err.Error() != "access denied" {
		t.Fatalf("err %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if !slices.Equal(g.steps, []string{"start /opt/lucidd --flag"}) || len(g.done) != 0 {
		t.Errorf("steps %v", g.steps)
	}
	// It can be tried again.
	if err := r.Restart(); err == nil || err.Error() != "access denied" {
		t.Errorf("retry: %v", err)
	}
}

// Elsewhere: shut down, then exec in place; an exec that fails exits 1.
func TestRestarterExecsInPlace(t *testing.T) {
	g := &restartRig{}
	r := g.restarter(true, nil)
	if err := r.Restart(); err != nil {
		t.Fatal(err)
	}
	if code := g.wait(t); code != 1 {
		t.Errorf("exit %d after a failed exec", code)
	}
	if want := []string{"shutdown", "exec /opt/lucidd --flag"}; !slices.Equal(g.steps, want) {
		t.Errorf("steps %v, want %v", g.steps, want)
	}
}

// Health carries the process start time, so the UI sees a restart.
func TestHealthSaysWhenItStarted(t *testing.T) {
	rec := httptest.NewRecorder()
	New().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if !strings.Contains(rec.Body.String(), `"started":"`+started.UTC().Format(time.RFC3339Nano)+`"`) {
		t.Errorf("body %s", rec.Body)
	}
}
