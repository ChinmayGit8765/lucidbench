package remote

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry is one line of audit.jsonl. It never holds a token, a pairing
// code or any content: only who did what to which id.
type AuditEntry struct {
	Time   time.Time `json:"time"`
	Action string    `json:"action"` // enable, disable, pair, pair_failed, auth_failed, revoke, stop, approve, send_back
	Device string    `json:"device,omitempty"`
	Name   string    `json:"name,omitempty"`   // the device's name
	Target string    `json:"target,omitempty"` // a session id
	Result string    `json:"result"`           // ok, or the error
	Client string    `json:"client,omitempty"` // the remote address, without port
}

// Audit appends to audit.jsonl.
type Audit struct {
	Path string
	mu   sync.Mutex
}

// Add appends one entry; a failed write is ignored, so an audit problem
// never blocks the operator.
func (a *Audit) Add(e AuditEntry) {
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(a.Path), 0o700)
	f, err := os.OpenFile(a.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// Recent returns up to n entries, newest first.
func (a *Audit) Recent(n int) ([]AuditEntry, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.Open(a.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []AuditEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var all []AuditEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var e AuditEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	out := make([]AuditEntry, 0, n)
	for i := len(all) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, all[i])
	}
	return out, sc.Err()
}
