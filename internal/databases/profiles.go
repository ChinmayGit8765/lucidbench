// Package databases is the Databases extension: it finds database
// containers on the local Docker engine, keeps connection profiles in
// <data dir>/databases.yaml, and reads each database through its own Go
// driver (Postgres, MySQL and MariaDB, Redis, MongoDB).
//
// It only reads. Queries run inside a read-only transaction (SQL), through an
// allowlist of commands (Redis) or as a find with a limit (Mongo). A profile
// holds the password as an env:NAME reference, never the value, and no
// response, error or log line carries a resolved password.
package databases

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Engines the extension speaks. MariaDB uses the MySQL driver.
const (
	Postgres = "postgres"
	MySQL    = "mysql"
	Redis    = "redis"
	Mongo    = "mongo"
)

// Engines lists the engine names, in display order.
func Engines() []string { return []string{Postgres, MySQL, Redis, Mongo} }

// DefaultPort is the port an engine listens on by default.
func DefaultPort(engine string) int {
	return map[string]int{Postgres: 5432, MySQL: 3306, Redis: 6379, Mongo: 27017}[engine]
}

// FileName is the profile file inside the data directory.
const FileName = "databases.yaml"

// Profile is one saved connection. Password is an env:NAME reference to a
// variable in the daemon's environment; it is never a value.
type Profile struct {
	ID       string           `yaml:"id" json:"id"`
	Engine   string           `yaml:"engine" json:"engine"`
	Host     string           `yaml:"host" json:"host"`
	Port     int              `yaml:"port" json:"port"`
	Database string           `yaml:"database,omitempty" json:"database,omitempty"`
	User     string           `yaml:"user,omitempty" json:"user,omitempty"`
	Password config.SecretRef `yaml:"password,omitempty" json:"password,omitempty"`
	// Readonly is kept for the day writes exist. Every connection is read-only
	// today, and a Prod connection always is.
	Readonly bool   `yaml:"readonly" json:"readonly"`
	Label    string `yaml:"label,omitempty" json:"label,omitempty"`
	Prod     bool   `yaml:"prod,omitempty" json:"prod,omitempty"`
}

// ErrInvalid wraps every profile validation failure.
var ErrInvalid = errors.New("invalid connection")

var (
	idRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)
	hostRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,252}$|^\[[0-9A-Fa-f:.]+\]$`)
)

// Validate checks a profile and returns it normalised: a literal password is
// refused (the error never echoes it), and a prod connection is read-only.
func (p Profile) Validate() (Profile, error) {
	bad := func(format string, a ...any) (Profile, error) {
		return Profile{}, fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
	}
	p.ID, p.Host = strings.TrimSpace(p.ID), strings.TrimSpace(p.Host)
	p.Label = strings.TrimSpace(p.Label)
	if !idRE.MatchString(p.ID) {
		return bad("id must be lowercase letters, digits, - or _ (at most 63 characters)")
	}
	if !contains(Engines(), p.Engine) {
		return bad("engine must be one of %s", strings.Join(Engines(), ", "))
	}
	if !hostRE.MatchString(p.Host) {
		return bad("host must be a host name or address")
	}
	if p.Port == 0 {
		p.Port = DefaultPort(p.Engine)
	}
	if p.Port < 1 || p.Port > 65535 {
		return bad("port must be 1-65535")
	}
	if len(p.Label) > 80 || len(p.Database) > 200 || len(p.User) > 200 || strings.ContainsAny(p.Database+p.User+p.Label, "\r\n\x00") {
		return bad("label, database and user must be short single-line text")
	}
	if p.Password != "" {
		ref, err := config.ParseSecretRef(string(p.Password))
		if err != nil {
			return bad("password: %v", err)
		}
		p.Password = ref
	}
	if p.Prod {
		p.Readonly = true
	}
	return p, nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

type file struct {
	Connections []Profile `yaml:"connections"`
}

// Store reads and writes databases.yaml. The file is owned by the app: it is
// rewritten whole on every change, so comments in it are not kept.
type Store struct {
	Path string

	mu sync.Mutex
}

// Load returns the saved profiles, sorted by label then id. A missing file is
// an empty list; a profile that fails validation (for example one edited by
// hand to hold a literal password) fails the load by naming its id.
func (s *Store) Load() ([]Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

func (s *Store) load() ([]Profile, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %s", FileName, strings.TrimPrefix(err.Error(), "yaml: "))
	}
	out := make([]Profile, 0, len(f.Connections))
	seen := map[string]bool{}
	for i, p := range f.Connections {
		v, err := p.Validate()
		if err != nil {
			return nil, fmt.Errorf("%s: connection %d (%q): %w", FileName, i+1, p.ID, err)
		}
		if seen[v.ID] {
			return nil, fmt.Errorf("%s: id %q appears twice", FileName, v.ID)
		}
		seen[v.ID] = true
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].display()), strings.ToLower(out[j].display())
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (p Profile) display() string {
	if p.Label != "" {
		return p.Label
	}
	return p.ID
}

// ErrExists is returned when saving an id that is already taken.
var ErrExists = errors.New("a connection with this id already exists")

// ErrNotFound is returned for an id that is not saved.
var ErrNotFound = errors.New("no such saved connection")

// Add saves a new profile.
func (s *Store) Add(p Profile) (Profile, error) {
	p, err := p.Validate()
	if err != nil {
		return Profile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return Profile{}, err
	}
	for _, x := range list {
		if x.ID == p.ID {
			return Profile{}, ErrExists
		}
	}
	return p, s.write(append(list, p))
}

// Remove deletes a saved profile.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.load()
	if err != nil {
		return err
	}
	kept := list[:0:0]
	for _, x := range list {
		if x.ID != id {
			kept = append(kept, x)
		}
	}
	if len(kept) == len(list) {
		return ErrNotFound
	}
	return s.write(kept)
}

// Get returns one saved profile.
func (s *Store) Get(id string) (Profile, error) {
	list, err := s.Load()
	if err != nil {
		return Profile{}, err
	}
	for _, x := range list {
		if x.ID == id {
			return x, nil
		}
	}
	return Profile{}, ErrNotFound
}

func (s *Store) write(list []Profile) error {
	b, err := yaml.Marshal(file{Connections: list})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	header := []byte("# Lucidbench connection profiles. Passwords are env:NAME references, never values.\n")
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(header, b...), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
