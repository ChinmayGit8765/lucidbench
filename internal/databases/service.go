package databases

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/power"
)

// Service ties discovery, the saved profiles, the drivers and the managers
// together behind the HTTP routes.
type Service struct {
	Store    *Store
	Docker   docker.Func
	Getenv   func(string) string
	Open     map[string]Opener
	Managers *Managers
}

// New builds the service with the real drivers and docker CLI. log may be
// nil; with it, manager starts and stops appear in the power activity.
func New(dataDir string, log *power.ActivityLog) *Service {
	return &Service{
		Store:  &Store{Path: filepath.Join(dataDir, FileName)},
		Docker: docker.Exec,
		Getenv: os.Getenv,
		Open: map[string]Opener{
			Postgres: OpenPostgres, MySQL: OpenMySQL, Redis: OpenRedis, Mongo: OpenMongo,
		},
		Managers: &Managers{Docker: docker.Exec, Log: log},
	}
}

// scrubbed is an error whose text has the password removed, and which still
// matches the sentinel it wraps (ErrReadOnly and the others).
type scrubbed struct {
	msg string
	err error
}

func (s *scrubbed) Error() string { return s.msg }
func (s *scrubbed) Unwrap() error { return s.err }

// scrub removes a password from text. Driver errors can echo what they were
// given, so every error that leaves the service goes through it.
func scrub(msg, password string) string {
	if password == "" {
		return msg
	}
	return strings.ReplaceAll(msg, password, "***")
}

func scrubErr(err error, password string) error {
	if err == nil || password == "" {
		return err
	}
	if m := err.Error(); strings.Contains(m, password) {
		return &scrubbed{msg: scrub(m, password), err: err}
	}
	return err
}

// ErrNotSaved is returned for discovered containers, which have no password
// and so cannot be read until they are saved as a connection.
var ErrNotSaved = errors.New("save this container as a connection first")

// password resolves a profile's password reference. A reference whose
// variable is unset is an error that names the variable, not a value.
func (s *Service) password(p Profile) (string, error) {
	if p.Password == "" {
		return "", nil
	}
	v := p.Password.Resolve(s.Getenv)
	if v == "" {
		return "", fmt.Errorf("the environment variable %s is not set (or empty) in the Lucidbench daemon; set it and restart Lucidbench", p.Password.Env())
	}
	return v, nil
}

func (s *Service) profile(id string) (Profile, error) {
	if strings.HasPrefix(id, "docker:") {
		return Profile{}, ErrNotSaved
	}
	return s.Store.Get(id)
}

// with opens a connection for a saved profile, runs fn, and closes it.
func (s *Service) with(ctx context.Context, id string, fn func(context.Context, Profile, Conn) error) (err error) {
	p, err := s.profile(id)
	if err != nil {
		return err
	}
	pw, err := s.password(p)
	if err != nil {
		return err
	}
	defer func() { err = scrubErr(err, pw) }()
	open := s.Open[p.Engine]
	if open == nil {
		return fmt.Errorf("no driver for %s", p.Engine)
	}
	octx, cancel := context.WithTimeout(ctx, ConnectTimeout)
	c, err := open(octx, p, pw)
	cancel()
	if err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}
	defer c.Close()
	return fn(ctx, p, c)
}

// Health is the result of a connect and ping.
type Health struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latency_ms"`
	Version   string `json:"version,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	Error     string `json:"error,omitempty"`
}

// Health connects, pings, and reads the version and size. A failure to
// connect is a result (OK false), not an error; an unknown id is an error.
func (s *Service) Health(ctx context.Context, id string) (Health, error) {
	p, err := s.profile(id)
	if err != nil {
		return Health{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, ConnectTimeout+QueryTimeout)
	defer cancel()
	h := Health{SizeBytes: -1}
	began := time.Now()
	err = s.with(ctx, p.ID, func(ctx context.Context, _ Profile, c Conn) error {
		if err := c.Ping(ctx); err != nil {
			return err
		}
		h.LatencyMS = time.Since(began).Milliseconds()
		info, err := c.Info(ctx)
		if err != nil {
			return err
		}
		h.Version, h.SizeBytes = info.Version, info.SizeBytes
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Health{}, err
		}
		h.Error = err.Error()
		return h, nil
	}
	h.OK = true
	return h, nil
}

// Schema lists the tables or collections of a saved connection.
func (s *Service) Schema(ctx context.Context, id string) (tables []Table, err error) {
	ctx, cancel := context.WithTimeout(ctx, ConnectTimeout+2*QueryTimeout)
	defer cancel()
	err = s.with(ctx, id, func(ctx context.Context, _ Profile, c Conn) error {
		tables, err = c.Schema(ctx)
		return err
	})
	if tables == nil {
		tables = []Table{}
	}
	return tables, err
}

// Query runs a read-only query on a saved connection.
func (s *Service) Query(ctx context.Context, id, q string) (res *Result, err error) {
	ctx, cancel := context.WithTimeout(ctx, ConnectTimeout+QueryTimeout+5*time.Second)
	defer cancel()
	// A refusal needs no connection, and the engine is known from the profile.
	p, err := s.profile(id)
	if err != nil {
		return nil, err
	}
	switch p.Engine {
	case Postgres, MySQL:
		if err := CheckSQL(q); err != nil {
			return nil, err
		}
	case Redis:
		args, perr := ParseRedisCommand(q)
		if perr != nil {
			return nil, perr
		}
		if err := CheckRedis(args); err != nil {
			return nil, err
		}
	case Mongo:
		if _, err := ParseMongoQuery(q); err != nil {
			return nil, err
		}
	}
	err = s.with(ctx, id, func(ctx context.Context, _ Profile, c Conn) error {
		res, err = c.Query(ctx, q)
		return err
	})
	return res, err
}

// Saved is a saved connection as the API shows it: the password stays the
// env:NAME reference, with a flag for whether that variable is set.
type Saved struct {
	Profile
	PasswordSet bool    `json:"password_set"`
	Health      *Health `json:"health,omitempty"`
	HasManager  bool    `json:"has_manager"`
}

// Listing is the body of GET /api/databases.
type Listing struct {
	Discovered []Discovered `json:"discovered"`
	Saved      []Saved      `json:"saved"`
	// DockerError is set when the docker engine could not be read; saved
	// connections are still listed.
	DockerError string        `json:"docker_error,omitempty"`
	Managers    []ManagerSpec `json:"managers"`
	Limits      struct {
		Rows           int `json:"rows"`
		TimeoutSeconds int `json:"timeout_seconds"`
	} `json:"limits"`
	RedisCommands []string `json:"redis_commands"`
}

// List returns what is discovered and what is saved. With health it also
// checks every saved connection, in parallel.
func (s *Service) List(ctx context.Context, withHealth bool) (*Listing, error) {
	saved, err := s.Store.Load()
	if err != nil {
		return nil, err
	}
	l := &Listing{Discovered: []Discovered{}, Saved: []Saved{}, Managers: ManagerSpecs(), RedisCommands: RedisAllowed()}
	l.Limits.Rows, l.Limits.TimeoutSeconds = RowLimit, int(QueryTimeout.Seconds())
	if ds, err := Discover(ctx, s.Docker); err != nil {
		l.DockerError = err.Error()
	} else {
		MarkSaved(ds, saved)
		l.Discovered = ds
	}
	for _, p := range saved {
		_, hasMgr := SpecFor(p.Engine)
		l.Saved = append(l.Saved, Saved{Profile: p, PasswordSet: p.Password == "" || p.Password.Resolve(s.Getenv) != "", HasManager: hasMgr})
	}
	if withHealth {
		var wg sync.WaitGroup
		for i := range l.Saved {
			wg.Add(1)
			go func(sv *Saved) {
				defer wg.Done()
				if h, err := s.Health(ctx, sv.ID); err == nil {
					sv.Health = &h
				}
			}(&l.Saved[i])
		}
		wg.Wait()
	}
	return l, nil
}
