package docker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{t: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)} }
func down(context.Context, ...string) error {
	return errors.New("docker ps: Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?")
}

// counting is a docker that is down until up is set, and counts its spawns.
type counting struct {
	mu    sync.Mutex
	n     int
	up    bool
	other error
}

func (c *counting) run(ctx context.Context, args ...string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	if c.other != nil {
		return nil, c.other
	}
	if !c.up {
		return nil, down(ctx, args...)
	}
	return []byte("ok"), nil
}

func TestBreakerBacksOffWhileTheEngineIsDown(t *testing.T) {
	clk := newClock()
	d := &counting{}
	b := &Breaker{Run: d.run, Now: clk.now}
	ctx := context.Background()

	_, err := b.Do(ctx, "ps", "-a")
	if !errors.Is(err, ErrUnreachable) || !Unreachable(err) {
		t.Fatalf("first failure = %v, want an unreachable error", err)
	}
	// A page polling every few seconds within the backoff spawns nothing.
	for i := 0; i < 20; i++ {
		clk.add(400 * time.Millisecond)
		if _, err := b.Do(ctx, "stats"); !errors.Is(err, ErrUnreachable) {
			t.Fatalf("call %d inside the backoff = %v", i, err)
		}
	}
	if d.n != 1 || !b.Down() {
		t.Fatalf("spawns = %d (want 1), down = %v", d.n, b.Down())
	}
	// Each window lets one probe through, and the windows double.
	clk.add(BackoffMin)
	_, _ = b.Do(ctx, "ps")
	if d.n != 2 {
		t.Fatalf("after the first window spawns = %d, want 2", d.n)
	}
	clk.add(BackoffMin + time.Second) // the second window is 2×BackoffMin
	_, _ = b.Do(ctx, "ps")
	if d.n != 2 {
		t.Fatalf("the second window should still be closed: spawns = %d", d.n)
	}
	clk.add(BackoffMin)
	_, _ = b.Do(ctx, "ps")
	if d.n != 3 {
		t.Fatalf("after the second window spawns = %d, want 3", d.n)
	}
	// A minute of polling every 2 s once backed off: a handful of spawns at most.
	before := d.n
	for i := 0; i < 30; i++ {
		clk.add(2 * time.Second)
		_, _ = b.Do(ctx, "ps")
	}
	if got := d.n - before; got > 2 {
		t.Fatalf("spawns in a backed-off minute = %d, want at most 2", got)
	}
	// The wait never grows past BackoffMax.
	for i := 0; i < 12; i++ {
		clk.add(BackoffMax)
		_, _ = b.Do(ctx, "ps")
	}
	n := d.n
	clk.add(BackoffMax)
	_, _ = b.Do(ctx, "ps")
	if d.n != n+1 {
		t.Fatalf("a probe should run after BackoffMax")
	}

	// The engine comes back: the next probe succeeds and closes the breaker.
	d.mu.Lock()
	d.up = true
	d.mu.Unlock()
	clk.add(BackoffMax)
	if out, err := b.Do(ctx, "ps"); err != nil || string(out) != "ok" {
		t.Fatalf("probe after recovery = %q, %v", out, err)
	}
	n = d.n
	for i := 0; i < 5; i++ {
		if _, err := b.Do(ctx, "ps"); err != nil {
			t.Fatal(err)
		}
	}
	if d.n != n+5 || b.Down() {
		t.Fatalf("a healthy engine must be called every time: %d calls, down = %v", d.n-n, b.Down())
	}
}

func TestBreakerIgnoresOrdinaryFailures(t *testing.T) {
	clk := newClock()
	d := &counting{up: true, other: fmt.Errorf("docker start: Error response from daemon: No such container: x")}
	b := &Breaker{Run: d.run, Now: clk.now}
	for i := 0; i < 3; i++ {
		if _, err := b.Do(context.Background(), "start", "x"); err == nil || errors.Is(err, ErrUnreachable) {
			t.Fatalf("an ordinary failure must pass through untouched: %v", err)
		}
	}
	if d.n != 3 || b.Down() {
		t.Fatalf("spawns = %d, down = %v", d.n, b.Down())
	}
	// A cancelled request is not a verdict on the engine either.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.other = nil
	d.up = false
	_, _ = b.Do(ctx, "ps")
	if b.Down() {
		t.Fatal("a cancelled call must not open the breaker")
	}
}

func TestBreakerLetsOneProbeThrough(t *testing.T) {
	clk := newClock()
	gate := make(chan struct{})
	var mu sync.Mutex
	spawns := 0
	b := &Breaker{Now: clk.now}
	b.Run = func(ctx context.Context, args ...string) ([]byte, error) {
		mu.Lock()
		spawns++
		first := spawns == 1
		mu.Unlock()
		if !first {
			<-gate
		}
		return nil, down(ctx, args...)
	}
	_, _ = b.Do(context.Background(), "ps")
	clk.add(BackoffMin)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = b.Do(context.Background(), "ps") // the probe, held at the gate
	}()
	for {
		mu.Lock()
		s := spawns
		mu.Unlock()
		if s == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		if _, err := b.Do(context.Background(), "ps"); !errors.Is(err, ErrUnreachable) {
			t.Fatalf("a call during the probe = %v", err)
		}
	}
	close(gate)
	wg.Wait()
	if spawns != 2 {
		t.Fatalf("spawns = %d, want 2 (one failure, one probe)", spawns)
	}
}

func TestUnreachable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{errors.New("docker ps: error during connect: Get \"http://%2F%2F.%2Fpipe%2Fdocker_engine/v1.47/containers/json\": open //./pipe/docker_engine: The system cannot find the file specified."), true},
		{errors.New("docker ps: Cannot connect to the Docker daemon (fake docker for tests)"), true},
		{fmt.Errorf("docker ps: %w", exec.ErrNotFound), true},
		{errors.New("docker logs: Error response from daemon: No such container: x"), false},
		{nil, false},
	} {
		if got := Unreachable(tc.err); got != tc.want {
			t.Errorf("Unreachable(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
