package usage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// claudeRec is one assistant message's usage. Only numbers, the message id
// and the model name are kept; message text is never retained.
type claudeRec struct {
	ID    string
	TS    int64 // unix seconds
	Model string
	In    int64
	Out   int64
	CR    int64
	CW    int64
}

func (r claudeRec) weight() int64 { return r.In + r.Out + r.CR + r.CW }

type claudeEntry struct {
	FileState
	Project string
	Recs    []claudeRec
}

// claudeLine is the part of a Claude Code log line this package reads.
type claudeLine struct {
	Timestamp string `json:"timestamp"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

var usageKey = []byte(`"usage"`)

func parseClaudeLine(line []byte, cutoff int64) (claudeRec, bool) {
	if !bytes.Contains(line, usageKey) {
		return claudeRec{}, false
	}
	var l claudeLine
	if json.Unmarshal(line, &l) != nil || l.Message == nil || l.Message.Usage == nil {
		return claudeRec{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil || t.Unix() < cutoff {
		return claudeRec{}, false
	}
	u := l.Message.Usage
	if l.Message.Model == "<synthetic>" {
		return claudeRec{}, false
	}
	return claudeRec{ID: l.Message.ID, TS: t.Unix(), Model: l.Message.Model, In: u.Input, Out: u.Output, CR: u.CacheRead, CW: u.CacheCreation}, true
}

// addRec keeps one record per message id within a file; a message that is
// logged several times while it streams ends up with its largest usage.
func addRec(e *claudeEntry, idx map[string]int, r claudeRec) {
	if r.ID != "" {
		if i, ok := idx[r.ID]; ok {
			if r.weight() >= e.Recs[i].weight() {
				e.Recs[i] = r
			}
			return
		}
		idx[r.ID] = len(e.Recs)
	}
	e.Recs = append(e.Recs, r)
}

// claudeRoot is a Claude config dir and the home its folder names are
// shortened against.
type claudeRoot struct{ Dir, Home string }

// encodeHome mirrors how Claude Code names a project folder after its path:
// every character that is not a letter or digit becomes "-".
func encodeHome(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// projectName is the project folder's name with the home prefix removed ("~"
// for the home itself). It is the only path-derived text that leaves a log.
func projectName(folder, home string) string {
	h := encodeHome(strings.TrimRight(home, `/\`))
	if h != "" {
		if folder == h {
			return "~"
		}
		if strings.HasPrefix(folder, h+"-") {
			folder = folder[len(h)+1:]
		}
	}
	if folder == "" {
		return "~"
	}
	return folder
}

// scanClaude brings the cache up to date for every log under the roots and
// returns the deduplicated records with their project names.
func scanClaude(roots []claudeRoot, c *cache, now time.Time) (recs []projRec, files int, changed bool) {
	type job struct{ path, project string }
	var jobs []job
	seenDir := map[string]bool{}
	for _, r := range roots {
		dir := filepath.Clean(r.Dir)
		if seenDir[dir] {
			continue
		}
		seenDir[dir] = true
		base := filepath.Join(dir, "projects")
		for _, p := range walkJSONL(base, "") {
			rel, err := filepath.Rel(base, p)
			if err != nil {
				continue
			}
			first := strings.Split(filepath.ToSlash(rel), "/")[0]
			if first == filepath.Base(p) { // a log directly in projects/, no folder
				first = ""
			}
			jobs = append(jobs, job{p, projectName(first, r.Home)})
		}
	}
	cutoff := now.Add(-retention).Unix()
	var mu sync.Mutex
	parallel(jobs, func(j job) {
		mu.Lock()
		old := c.Claude[j.path]
		mu.Unlock()
		e, upd := refreshClaude(j.path, j.project, old, cutoff)
		if e == nil {
			return
		}
		mu.Lock()
		if upd {
			c.Claude[j.path] = e
			changed = true
		}
		mu.Unlock()
	})
	live := map[string]bool{}
	for _, j := range jobs {
		live[j.path] = true
	}
	for p := range c.Claude {
		if !live[p] {
			delete(c.Claude, p)
			changed = true
		}
	}

	// Deduplicate across files: a resumed session repeats earlier messages.
	best := map[string]projRec{}
	for _, e := range c.Claude {
		files++
		for _, r := range e.Recs {
			pr := projRec{claudeRec: r, Project: e.Project}
			if r.ID == "" {
				recs = append(recs, pr)
				continue
			}
			if o, ok := best[r.ID]; !ok || r.weight() > o.weight() {
				best[r.ID] = pr
			}
		}
	}
	for _, r := range best {
		recs = append(recs, r)
	}
	return recs, files, changed
}

type projRec struct {
	claudeRec
	Project string
}

// refreshClaude returns the up-to-date entry for one file and whether it
// differs from old.
func refreshClaude(path, project string, old *claudeEntry, cutoff int64) (*claudeEntry, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false
	}
	e := &claudeEntry{Project: project}
	from := int64(0)
	if old != nil {
		from, _ = old.plan(info, f)
		if from < 0 {
			if old.Project == project {
				return old, false
			}
			moved := *old
			moved.Project = project
			return &moved, true
		}
		if from > 0 {
			e.Recs = append([]claudeRec(nil), old.Recs...)
		}
	}
	idx := map[string]int{}
	for i, r := range e.Recs {
		if r.ID != "" {
			idx[r.ID] = i
		}
	}
	off, err := readLines(f, from, func(line []byte) {
		if r, ok := parseClaudeLine(line, cutoff); ok {
			addRec(e, idx, r)
		}
	})
	if err != nil {
		return nil, false
	}
	e.Size, e.ModNano, e.Offset, e.Head = info.Size(), info.ModTime().UnixNano(), off, headHash(f, info.Size())
	return e, true
}
