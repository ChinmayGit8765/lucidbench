package docker

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Backoff bounds for an unreachable engine: the first failure waits
// BackoffMin before the next real docker call, each further failure doubles
// the wait, up to BackoffMax.
const (
	BackoffMin = 10 * time.Second
	BackoffMax = 2 * time.Minute
)

// ErrUnreachable marks an error returned without running docker because the
// engine was unreachable a moment ago. errors.Is(err, ErrUnreachable) holds
// for those, and for the failure that tripped the breaker.
var ErrUnreachable = errors.New("docker engine unreachable")

// Unreachable reports whether a docker CLI error means the engine (or the CLI
// itself) is not there, as opposed to a command that failed on its own (no
// such container, a bad flag).
func Unreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUnreachable) || errors.Is(err, exec.ErrNotFound) {
		return true
	}
	m := strings.ToLower(err.Error())
	for _, s := range []string{
		"cannot connect to the docker daemon",
		"is the docker daemon running",
		"error during connect",
		"docker daemon is not running",
		"docker_engine",
		"docker.sock",
		"executable file not found",
	} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// Breaker stops spawning the docker CLI while the engine is unreachable.
// After a failure that means "no engine", calls return that error at once
// until the backoff has passed; then one call probes for real (the others
// keep getting the cached error), and a success closes the breaker again.
// Every caller in the daemon shares one breaker through Exec, so a page that
// polls six docker endpoints costs one spawn per backoff window, not six per
// poll.
type Breaker struct {
	// Run spawns docker; Now is the clock. Both are swapped in tests.
	Run func(ctx context.Context, args ...string) ([]byte, error)
	Now func() time.Time

	mu       sync.Mutex
	failures int
	until    time.Time
	probing  bool
	last     error
}

// Do runs docker with args, or returns the cached unreachable error.
func (b *Breaker) Do(ctx context.Context, args ...string) ([]byte, error) {
	b.mu.Lock()
	now := b.Now()
	if b.failures > 0 && (now.Before(b.until) || b.probing) {
		err := b.last
		b.mu.Unlock()
		return nil, err
	}
	probe := b.failures > 0
	if probe {
		b.probing = true
	}
	b.mu.Unlock()

	out, err := b.Run(ctx, args...)

	b.mu.Lock()
	defer b.mu.Unlock()
	if probe {
		b.probing = false
	}
	switch {
	case err == nil:
		b.failures, b.last = 0, nil
	case Unreachable(err) && ctx.Err() == nil:
		b.failures++
		wait := BackoffMin << min(b.failures-1, 8)
		if wait > BackoffMax {
			wait = BackoffMax
		}
		b.until = b.Now().Add(wait)
		if !errors.Is(err, ErrUnreachable) {
			err = &unreachableError{err}
		}
		b.last = err
	}
	return out, err
}

// Down reports whether the breaker is open: the engine was unreachable at the
// last real call and the backoff has not yet passed.
func (b *Breaker) Down() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures > 0 && b.Now().Before(b.until)
}

type unreachableError struct{ err error }

func (e *unreachableError) Error() string { return e.err.Error() }
func (e *unreachableError) Unwrap() []error {
	return []error{e.err, ErrUnreachable}
}

// engine is the breaker every Exec call goes through.
var engine = &Breaker{Run: spawn, Now: time.Now}
