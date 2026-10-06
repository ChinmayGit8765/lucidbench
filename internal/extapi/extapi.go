// Package extapi is the small shared core of the remote board connectors
// (Linear, Trello): one HTTP call with a back-off on rate limits, a short-lived
// response cache and the mapping of failures to HTTP answers.
//
// Secrets stay out of everything this package returns. Callers send
// credentials in a header, never in a URL, and a failure is reduced to one
// of a few fixed errors: neither a transport error (which would quote the
// URL) nor anything the remote service answered is passed on.
package extapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Errors a call can end in.
var (
	// ErrNotConfigured means no credential is set.
	ErrNotConfigured = errors.New("not configured")
	// ErrUnauthorized means the service rejected the credential (HTTP 401 or 403).
	ErrUnauthorized = errors.New("token invalid")
	// ErrRateLimited means the service kept answering 429 through every retry.
	ErrRateLimited = errors.New("rate limited")
	// ErrUnreachable means the request could not be completed.
	ErrUnreachable = errors.New("cannot reach the service")
	// ErrNotFound means the service answered 404.
	ErrNotFound = errors.New("not found")
	// ErrBadRequest means the service refused the request (other 4xx).
	ErrBadRequest = errors.New("the service refused the request")
	// ErrRemote means the service answered 5xx or something unreadable.
	ErrRemote = errors.New("the service answered with an error")
)

// CacheTTL is how long a read is reused.
const CacheTTL = 60 * time.Second

// MaxAttempts is how many times a rate-limited call is tried.
const MaxAttempts = 3

// MaxBody caps a response body read into memory.
const MaxBody = 16 << 20

// maxWait caps one back-off sleep, whatever Retry-After says.
const maxWait = 30 * time.Second

// Doer sends requests for one service.
type Doer struct {
	HTTP *http.Client
	// Sleep waits between rate-limited attempts; nil uses time.Sleep.
	Sleep func(time.Duration)
	// RateLimited, when set, also marks answers that are not HTTP 429 as rate
	// limits (Linear reports one as an error object).
	RateLimited func(status int, body []byte) bool
	// Classify, when set, may turn an error answer into one of the errors above
	// before the status code decides. It returns nil to leave it to the status.
	Classify func(status int, body []byte) error
}

// Do builds and sends a request, retrying on 429 with a back-off that follows
// Retry-After (seconds) when given and doubles from one second otherwise. It
// returns the body of a 2xx answer, and a fixed error for anything else.
func (d *Doer) Do(ctx context.Context, build func() (*http.Request, error)) ([]byte, error) {
	hc := d.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	sleep := d.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	wait := time.Second
	for attempt := 1; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, ErrBadRequest
		}
		res, err := hc.Do(req.WithContext(ctx))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, ErrUnreachable
		}
		body, rerr := io.ReadAll(io.LimitReader(res.Body, MaxBody))
		_ = res.Body.Close()
		switch {
		case res.StatusCode == http.StatusTooManyRequests || (d.RateLimited != nil && d.RateLimited(res.StatusCode, body)):
			if attempt >= MaxAttempts {
				return nil, ErrRateLimited
			}
			w := wait
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 {
				w = time.Duration(s) * time.Second
			}
			sleep(min(w, maxWait))
			wait *= 2
			continue
		case res.StatusCode >= 400 && d.Classify != nil && d.Classify(res.StatusCode, body) != nil:
			return nil, d.Classify(res.StatusCode, body)
		case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			return nil, ErrUnauthorized
		case res.StatusCode == http.StatusNotFound:
			return nil, ErrNotFound
		case res.StatusCode >= 500:
			return nil, ErrRemote
		case res.StatusCode >= 400:
			return nil, ErrBadRequest
		case rerr != nil:
			return nil, ErrUnreachable
		}
		return body, nil
	}
}

// Cache holds read results for CacheTTL.
type Cache struct {
	// Now is the clock; nil uses time.Now.
	Now func() time.Time

	mu sync.Mutex
	m  map[string]entry
}

type entry struct {
	v  any
	at time.Time
}

func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Get returns a cached value that is still fresh.
func (c *Cache) Get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok || c.now().Sub(e.at) >= CacheTTL {
		return nil, false
	}
	return e.v, true
}

// Put stores a value.
func (c *Cache) Put(key string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]entry{}
	}
	c.m[key] = entry{v, c.now()}
}

// Clear drops everything; a write calls it so the next read is fresh.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m = nil
}

// Fail answers a failed call. service is the display name ("Linear"). The
// text is built from the fixed errors above only, so nothing the remote said
// and no credential can reach the client.
func Fail(w http.ResponseWriter, service string, err error) {
	status, msg := http.StatusBadGateway, ""
	switch {
	case errors.Is(err, ErrNotConfigured):
		status, msg = http.StatusPreconditionFailed, service+" is not set up: no credential is configured"
	case errors.Is(err, ErrUnauthorized):
		status, msg = http.StatusUnauthorized, service+" token invalid: it was rejected, so create a new one and restart"
	case errors.Is(err, ErrRateLimited):
		status, msg = http.StatusTooManyRequests, service+" is rate limiting requests; try again in a minute"
	case errors.Is(err, ErrNotFound):
		status, msg = http.StatusNotFound, service+" has no such item"
	case errors.Is(err, ErrBadRequest):
		status, msg = http.StatusBadRequest, service+" refused the request"
	case errors.Is(err, ErrUnreachable):
		msg = "cannot reach " + service
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, msg = http.StatusGatewayTimeout, "the request to "+service+" timed out"
	default:
		msg = fmt.Sprintf("%s answered with an error", service)
	}
	http.Error(w, msg, status)
}
