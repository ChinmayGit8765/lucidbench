package power

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one start or stop, automatic or from a button.
type Entry struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // cluster | runner | stack | manager
	Name   string    `json:"name"`
	Action string    `json:"action"` // start | stop
	Auto   bool      `json:"auto"`
	Reason string    `json:"reason,omitempty"`
	OK     bool      `json:"ok"`
	Error  string    `json:"error,omitempty"`
	// Was is the container status before a start, such as "Exited (128) 2 hours ago".
	Was string `json:"was,omitempty"`
	// Restarted says a cluster node was restarted because its API did not come up.
	Restarted bool    `json:"restarted,omitempty"`
	Seconds   float64 `json:"seconds,omitempty"`
}

// ActivityLog appends entries to a JSON-lines file.
type ActivityLog struct {
	Path string

	mu sync.Mutex
}

// MaxLogBytes is the size past which the log is trimmed to its newest lines.
const (
	MaxLogBytes = 1 << 20
	KeepLines   = 1000
)

// Append adds an entry, creating the file and its folder as needed.
func (l *ActivityLog) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	st, statErr := f.Stat()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && statErr == nil && st.Size() > MaxLogBytes {
		err = l.trim()
	}
	return err
}

// trim keeps the newest lines, at most KeepLines and half of MaxLogBytes, so
// the next trim is far off. The caller holds l.mu.
func (l *ActivityLog) trim() error {
	lines, err := l.lines()
	if err != nil {
		return err
	}
	keep, size := 0, 0
	for i := len(lines) - 1; i >= 0 && keep < KeepLines; i-- {
		size += len(lines[i]) + 1
		if size > MaxLogBytes/2 {
			break
		}
		keep++
	}
	lines = lines[len(lines)-keep:]
	tmp := l.Path + ".tmp"
	if err := os.WriteFile(tmp, append(bytes.Join(lines, []byte("\n")), '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.Path)
}

func (l *ActivityLog) lines() ([][]byte, error) {
	b, err := os.ReadFile(l.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 {
			out = append(out, append([]byte(nil), line...))
		}
	}
	return out, sc.Err()
}

// Recent returns up to n entries, newest first. Lines that do not parse are
// skipped.
func (l *ActivityLog) Recent(n int) ([]Entry, error) {
	l.mu.Lock()
	lines, err := l.lines()
	l.mu.Unlock()
	out := []Entry{}
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		var e Entry
		if json.Unmarshal(lines[i], &e) == nil {
			out = append(out, e)
		}
	}
	return out, err
}
