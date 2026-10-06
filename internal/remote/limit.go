package remote

import (
	"sync"
	"time"
)

// Limiter counts failures per key (a client address) in a sliding window.
// Once a key reaches Max failures it is refused until its oldest failure in
// the window has aged out.
type Limiter struct {
	Max    int
	Window time.Duration
	Now    func() time.Time

	mu    sync.Mutex
	fails map[string][]time.Time
}

// NewLimiter returns a limiter of max failures per window.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window}
}

func (l *Limiter) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// recent drops the failures older than the window and returns the rest.
func (l *Limiter) recent(key string, now time.Time) []time.Time {
	ts := l.fails[key]
	i := 0
	for i < len(ts) && now.Sub(ts[i]) >= l.Window {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = ts
	return ts
}

// Allowed reports whether key may try again now.
func (l *Limiter) Allowed(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		return true
	}
	return len(l.recent(key, l.now())) < l.Max
}

// Fail records one failure for key.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fails == nil {
		l.fails = map[string][]time.Time{}
	}
	now := l.now()
	ts := l.recent(key, now)
	// Keep the map small under a flood: at most Max entries per key and a
	// bounded number of keys.
	if len(ts) >= l.Max {
		ts = ts[1:]
	}
	if len(l.fails) > 4096 {
		for k := range l.fails {
			l.recent(k, now)
		}
	}
	l.fails[key] = append(ts, now)
}
