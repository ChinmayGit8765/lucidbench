package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Lock is an exclusive per-profile lock file.
type Lock struct{ path string }

// LockPath returns the lock file path under the Lucidbench data dir.
func LockPath(provider, profile string) (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "locks", provider+"-"+profile+".lock"), nil
}

// LockPathIn returns the lock path under an explicit config dir.
func LockPathIn(configDir, provider, profile string) string {
	return filepath.Join(configDir, "lucidbench", "locks", provider+"-"+profile+".lock")
}

// pidAlive is a variable so tests can stub it.
var pidAlive = processAlive

// Acquire creates the lock with O_EXCL. A lock held by a dead PID is replaced.
func Acquire(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, werr := fmt.Fprintf(f, "%d\n", os.Getpid())
			cerr := f.Close()
			if werr != nil || cerr != nil {
				os.Remove(path)
				return nil, errors.Join(werr, cerr)
			}
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			if errors.Is(rerr, os.ErrNotExist) {
				continue
			}
			return nil, rerr
		}
		pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
		if perr == nil && pidAlive(pid) {
			return nil, fmt.Errorf("profile is in use by pid %d (lock %s)", pid, path)
		}
		// Stale (dead pid or unparseable): remove and retry once.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("could not acquire lock %s", path)
}

// Release removes the lock file.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	return os.Remove(l.path)
}
