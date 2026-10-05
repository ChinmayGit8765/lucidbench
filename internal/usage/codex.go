package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cxTotals are the running token totals a Codex session reports.
type cxTotals struct{ In, Cached, Out, CW, Total int64 }

// cxEv is the growth between two token_count events.
type cxEv struct {
	TS              int64
	Model           string
	In, CR, Out, CW int64
}

// cxWin is one rate-limit window as Codex wrote it; nil means unknown.
type cxWin struct {
	Used    *float64
	Minutes *int64
	Resets  *int64
}

type codexEntry struct {
	FileState
	Project string
	Model   string
	Prev    cxTotals
	Events  []cxEv
	// Latest rate limits seen in this file, and when.
	RLTS      int64
	Primary   *cxWin
	Secondary *cxWin
}

type codexLine struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type  string `json:"type"`
		Cwd   string `json:"cwd"`
		Model string `json:"model"`
		Info  *struct {
			Total *struct {
				In     int64 `json:"input_tokens"`
				Cached int64 `json:"cached_input_tokens"`
				Out    int64 `json:"output_tokens"`
				CW     int64 `json:"cache_write_input_tokens"`
				Total  int64 `json:"total_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
		RateLimits *struct {
			Primary   *rlJSON `json:"primary"`
			Secondary *rlJSON `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

type rlJSON struct {
	Used    *float64 `json:"used_percent"`
	Minutes *int64   `json:"window_minutes"`
	Resets  *int64   `json:"resets_at"`
}

func (r *rlJSON) win() *cxWin {
	if r == nil {
		return nil
	}
	return &cxWin{Used: r.Used, Minutes: r.Minutes, Resets: r.Resets}
}

var (
	keyTokenCount = []byte(`"token_count"`)
	keyMeta       = []byte(`"session_meta"`)
	keyTurn       = []byte(`"turn_context"`)
)

// folderOf is the last element of a working directory, whichever separator
// the logging machine used.
func folderOf(cwd string) string {
	cwd = strings.TrimRight(strings.ReplaceAll(cwd, `\`, "/"), "/")
	b := path.Base(cwd)
	if b == "." || b == "/" || b == "" {
		return ""
	}
	return b
}

func (e *codexEntry) apply(line []byte, cutoff int64) {
	isCount := bytes.Contains(line, keyTokenCount)
	if !isCount && !bytes.Contains(line, keyMeta) && !bytes.Contains(line, keyTurn) {
		return
	}
	var l codexLine
	if json.Unmarshal(line, &l) != nil {
		return
	}
	switch {
	case l.Type == "session_meta":
		if f := folderOf(l.Payload.Cwd); f != "" {
			e.Project = f
		}
	case l.Type == "turn_context":
		if l.Payload.Model != "" {
			e.Model = l.Payload.Model
		}
	case l.Type == "event_msg" && l.Payload.Type == "token_count":
		t, err := time.Parse(time.RFC3339Nano, l.Timestamp)
		if err != nil {
			return
		}
		if l.Payload.Info != nil && l.Payload.Info.Total != nil {
			x := l.Payload.Info.Total
			cur := cxTotals{x.In, x.Cached, x.Out, x.CW, x.Total}
			d := cxTotals{cur.In - e.Prev.In, cur.Cached - e.Prev.Cached, cur.Out - e.Prev.Out, cur.CW - e.Prev.CW, cur.Total - e.Prev.Total}
			if d.In < 0 || d.Cached < 0 || d.Out < 0 || d.CW < 0 || d.Total < 0 {
				d = cur // totals restarted
			}
			e.Prev = cur
			if d != (cxTotals{}) && t.Unix() >= cutoff {
				ev := cxEv{TS: t.Unix(), Model: e.Model, In: d.In - d.Cached, CR: d.Cached, Out: d.Out, CW: d.CW}
				if ev.In < 0 {
					ev.In = 0
				}
				if ev.In+ev.CR+ev.Out+ev.CW == 0 {
					ev.In = d.Total // only a grand total was reported
				}
				e.Events = append(e.Events, ev)
			}
		}
		if rl := l.Payload.RateLimits; rl != nil && (rl.Primary != nil || rl.Secondary != nil) && t.Unix() >= e.RLTS {
			e.RLTS, e.Primary, e.Secondary = t.Unix(), rl.Primary.win(), rl.Secondary.win()
		}
	}
}

func refreshCodex(p string, old *codexEntry, cutoff int64) (*codexEntry, bool) {
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false
	}
	e := &codexEntry{}
	from := int64(0)
	if old != nil {
		from, _ = old.plan(info, f)
		if from < 0 {
			return old, false
		}
		if from > 0 {
			cp := *old
			cp.Events = append([]cxEv(nil), old.Events...)
			e = &cp
		}
	}
	off, err := readLines(f, from, func(line []byte) { e.apply(line, cutoff) })
	if err != nil {
		return nil, false
	}
	e.Size, e.ModNano, e.Offset, e.Head = info.Size(), info.ModTime().UnixNano(), off, headHash(f, info.Size())
	return e, true
}

// scanCodex brings the cache up to date for the rollout logs under dir and
// returns the entries.
func scanCodex(dirs []string, c *cache, now time.Time) (entries []*codexEntry, changed bool) {
	var files []string
	seen := map[string]bool{}
	for _, d := range dirs {
		d = filepath.Clean(d)
		if seen[d] {
			continue
		}
		seen[d] = true
		files = append(files, walkJSONL(filepath.Join(d, "sessions"), "rollout-")...)
	}
	cutoff := now.Add(-retention).Unix()
	var mu sync.Mutex
	parallel(files, func(p string) {
		mu.Lock()
		old := c.Codex[p]
		mu.Unlock()
		e, upd := refreshCodex(p, old, cutoff)
		if e == nil {
			return
		}
		mu.Lock()
		if upd {
			c.Codex[p] = e
			changed = true
		}
		mu.Unlock()
	})
	live := map[string]bool{}
	for _, p := range files {
		live[p] = true
	}
	for p := range c.Codex {
		if !live[p] {
			delete(c.Codex, p)
			changed = true
		}
	}
	for _, e := range c.Codex {
		entries = append(entries, e)
	}
	return entries, changed
}
