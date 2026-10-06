package runner

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// authFiles lists, per provider, the only files copied from the host config
// dir into a run. Everything else there (hooks, MCP servers, settings,
// history, backups) stays on the host and never loads inside the container.
var authFiles = map[string][]string{
	"claude": {".credentials.json"},
	"codex":  {"auth.json"},
	"grok":   {"auth.json"},
}

// ownerFile is the record in each run folder naming the process that owns
// the run and the lock file it holds.
const ownerFile = "owner"

// ownerRecord is the owner file's content: the owner's PID, then the name of
// the lock file it holds (if any), one per line.
func ownerRecord(pid int, lockName string) []byte {
	if lockName == "" {
		return []byte(fmt.Sprintf("%d\n", pid))
	}
	return []byte(fmt.Sprintf("%d\n%s\n", pid, lockName))
}

// Staged is a per-run, throwaway config dir holding only auth material.
//
// Privacy: the auth files are copied between the host config dir and a temp
// dir under the Lucidbench data dir, and mounted into a local container. They
// never leave the machine and are never logged or printed.
type Staged struct {
	RunDir    string // <DataDir>/runs/<id>, removed by Finish
	ConfigDir string // the provider config dir under RunDir, mounted at the provider's config path
	WorkDir   string // empty writable dir mounted as the container's cwd

	hostDir string
	before  map[string][]byte // staged auth file contents at copy time
	hostAt  map[string][]byte // host auth file contents at copy time
}

// Stage creates runs/<id> under dataDir and copies the provider's auth files
// from hostDir into it. It fails if none of the auth files exist.
func Stage(dataDir, provider, hostDir string) (*Staged, error) {
	return stage(dataDir, provider, hostDir, "")
}

// stage is Stage for a run holding the lock file named lockName (empty if
// none). The owner record is written before any auth file, so Sweep can tell
// a crashed run from a live one even if staging dies halfway.
func stage(dataDir, provider, hostDir, lockName string) (*Staged, error) {
	names, ok := authFiles[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	runDir := filepath.Join(dataDir, "runs", hex.EncodeToString(id[:]))
	st := &Staged{
		RunDir:    runDir,
		ConfigDir: filepath.Join(runDir, "home", providers[provider]),
		WorkDir:   filepath.Join(runDir, "work"),
		hostDir:   hostDir,
		before:    map[string][]byte{},
		hostAt:    map[string][]byte{},
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runDir, ownerFile), ownerRecord(os.Getpid(), lockName), 0o600); err != nil {
		os.RemoveAll(runDir)
		return nil, err
	}
	for _, d := range []string{st.ConfigDir, st.WorkDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			os.RemoveAll(runDir)
			return nil, err
		}
	}
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(hostDir, n))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			os.RemoveAll(runDir)
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(st.ConfigDir, n), data, 0o600); err != nil {
			os.RemoveAll(runDir)
			return nil, err
		}
		st.before[n], st.hostAt[n] = data, data
	}
	if len(st.before) == 0 {
		os.RemoveAll(runDir)
		return nil, fmt.Errorf("no credentials (%v) in %s: log in on the host first", names, hostDir)
	}
	return st, nil
}

// Finish copies a refreshed auth file back to the host, then deletes the temp
// dir. OAuth refresh can rotate tokens, so dropping a refreshed file would
// invalidate the host login. A file is copied back only if the CLI changed it
// and the host copy is untouched since staging (otherwise the host's newer
// login wins). It returns the names of the files copied back.
func (s *Staged) Finish() (refreshed []string, err error) {
	defer os.RemoveAll(s.RunDir)
	for n, was := range s.before {
		now, rerr := os.ReadFile(filepath.Join(s.ConfigDir, n))
		if rerr != nil || len(now) == 0 || bytes.Equal(now, was) {
			continue
		}
		hostPath := filepath.Join(s.hostDir, n)
		cur, herr := os.ReadFile(hostPath)
		if herr != nil || !bytes.Equal(cur, s.hostAt[n]) {
			err = errors.Join(err, fmt.Errorf("host %s changed during the run; refreshed copy discarded", n))
			continue
		}
		if werr := writeAtomic(hostPath, now); werr != nil {
			err = errors.Join(err, werr)
			continue
		}
		refreshed = append(refreshed, n)
	}
	return refreshed, err
}

// writeAtomic replaces path via a temp file in the same dir and a rename.
func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".lucid-auth-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Options describe one `lucid run`.
type Options struct {
	Provider, Profile, Prompt string
	Home                      string // host home dir (host profile only)
	DataDir                   string // Lucidbench data dir (runs/ lives here)
	LockPath                  string
}

// Exec runs docker with the given args and returns its exit code.
type Exec func(dockerArgs []string) int

// Run holds the profile lock for the whole run: it stages auth, runs docker,
// copies back a refreshed token and cleans up, so two runs never race on a
// token refresh. Volume profiles skip staging.
func Run(o Options, exec Exec) (code int, refreshed []string, err error) {
	if !ValidProvider(o.Provider) {
		return 2, nil, fmt.Errorf("unknown provider %q (want claude, codex or grok)", o.Provider)
	}
	if o.Profile != HostProfile && !ValidProfile(o.Profile) {
		return 2, nil, fmt.Errorf("invalid profile name %q", o.Profile)
	}
	lock, err := Acquire(o.LockPath)
	if err != nil {
		return 1, nil, fmt.Errorf("cannot run %s with profile %q: %w", o.Provider, o.Profile, err)
	}
	defer lock.Release()
	var st *Staged
	if o.Profile == HostProfile {
		if st, err = stage(o.DataDir, o.Provider, HostDir(o.Home, o.Provider), filepath.Base(o.LockPath)); err != nil {
			return 1, nil, err
		}
	}
	args, err := Args(o.Provider, o.Profile, o.Prompt, st)
	if err != nil {
		if st != nil {
			st.Finish()
		}
		return 2, nil, err
	}
	code = exec(args)
	if st != nil {
		refreshed, err = st.Finish()
	}
	return code, refreshed, err
}
