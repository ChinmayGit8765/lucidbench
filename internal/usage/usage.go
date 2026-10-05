// Package usage reports how many tokens the user's AI CLIs have spent, read
// from the logs those CLIs already keep on this machine. It returns numbers
// and project folder names only: no prompt or message text is ever kept.
//
// Claude Code and Codex are read from their local logs; Lucidbench's own
// council and work runs from the usage it recorded; Grok and Cursor are
// reported as not available. Cost is shown only where the source states it.
package usage

import (
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// MaxDays is the longest range a summary can cover.
const MaxDays = 90

// Totals are token counts. Total counts everything, cache reads included.
type Totals struct {
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheRead  int64   `json:"cache_read"`
	CacheWrite int64   `json:"cache_write"`
	Total      int64   `json:"total"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
}

func (t *Totals) add(in, out, cr, cw int64) {
	t.Input += in
	t.Output += out
	t.CacheRead += cr
	t.CacheWrite += cw
	t.Total += in + out + cr + cw
}

// Day is one local calendar day.
type Day struct {
	Date string `json:"date"` // YYYY-MM-DD, local time
	Totals
}

// Bucket is a named slice of the totals: a model, a project folder, a
// provider or a source.
type Bucket struct {
	Name string `json:"name"`
	Runs int    `json:"runs,omitempty"`
	Totals
}

// Window is a rate-limit window as the provider CLI last reported it.
type Window struct {
	Name          string     `json:"name"` // primary | secondary
	Label         string     `json:"label"`
	UsedPercent   *float64   `json:"used_percent"`
	WindowMinutes *int64     `json:"window_minutes"`
	ResetsAt      *time.Time `json:"resets_at"`
	// Stale means the window has reset since it was observed, so the used
	// percentage no longer describes the current window.
	Stale      bool      `json:"stale,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
}

// Provider is one provider's usage over the requested range.
type Provider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Status is ok, empty (source found, nothing in range), missing (no logs
	// found), unavailable (no local source yet) or disabled.
	Status   string   `json:"status"`
	Note     string   `json:"note,omitempty"`
	Sources  int      `json:"sources"`
	Daily    []Day    `json:"daily"`
	Totals   Totals   `json:"totals"`
	Models   []Bucket `json:"models"`
	Projects []Bucket `json:"projects"`
	Windows  []Window `json:"windows"`
	// WindowNote says why Windows is empty, when the provider has none.
	WindowNote string `json:"window_note,omitempty"`
}

// Own is what Lucidbench's own agent runs spent.
type Own struct {
	Runs      int      `json:"runs"`
	Daily     []Day    `json:"daily"`
	Totals    Totals   `json:"totals"`
	Sources   []Bucket `json:"sources"`
	Providers []Bucket `json:"providers"`
}

// Summary is the body of GET /api/usage/summary.
type Summary struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Days        int        `json:"days"`
	Providers   []Provider `json:"providers"`
	Lucidbench  Own        `json:"lucidbench"`
}

// Service computes summaries. It keeps a parse cache in the data dir so a
// repeat request only reads what changed.
type Service struct {
	Roots   func() accounts.Roots
	DataDir string
	// Loc buckets events into days; nil means the machine's local zone.
	Loc *time.Location
	Now func() time.Time

	mu    sync.Mutex
	cache *cache
}

// NewService builds the service for a config and data dir.
func NewService(cfg *config.Config, dataDir string) *Service {
	return &Service{Roots: func() accounts.Roots { return accounts.FromConfig(cfg) }, DataDir: dataDir}
}

// sample is one counted event, whatever its source.
type sample struct {
	TS              int64
	Model, Project  string
	In, Out, CR, CW int64
}

// Summary computes the usage of the last days days, today included.
func (s *Service) Summary(days int) *Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if days < 1 {
		days = 1
	}
	if days > MaxDays {
		days = MaxDays
	}
	loc, now := s.Loc, time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if loc == nil {
		loc = time.Local
	}
	roots := s.Roots()
	cachePath := filepath.Join(s.DataDir, "usage-cache.gob")
	if s.cache == nil {
		s.cache = loadCache(cachePath)
	}
	out := &Summary{GeneratedAt: now.UTC(), Days: days}
	dirty := false

	// Claude Code.
	cl := Provider{ID: "claude", Label: "Claude", Status: "disabled", WindowNote: "Not exposed locally. Run /usage in Claude Code."}
	if roots.On("claude") {
		def := filepath.Join(roots.Home, ".claude")
		cr := []claudeRoot{{def, roots.Home}}
		for _, d := range roots.ExtraClaudeDirs {
			if d != "" {
				cr = append(cr, claudeRoot{d, roots.Home})
			}
		}
		recs, files, changed := scanClaude(cr, s.cache, now)
		dirty = dirty || changed
		var ss []sample
		for _, r := range recs {
			ss = append(ss, sample{r.TS, r.Model, r.Project, r.In, r.Out, r.CR, r.CW})
		}
		cl = fill(cl, ss, files, days, now, loc)
		if files == 0 {
			cl.Status, cl.Note = "missing", "No Claude Code logs found on this machine."
		}
	}
	out.Providers = append(out.Providers, cl)

	// Codex.
	cx := Provider{ID: "codex", Label: "Codex", Status: "disabled"}
	if roots.On("codex") {
		home := roots.CodexHome
		if home == "" {
			home = filepath.Join(roots.Home, ".codex")
		}
		dirs := append([]string{home}, roots.ExtraDirs["codex"]...)
		entries, changed := scanCodex(dirs, roots.Home, s.cache, now)
		dirty = dirty || changed
		var ss []sample
		var latest *codexEntry
		for _, e := range entries {
			for _, ev := range e.Events {
				ss = append(ss, sample{ev.TS, ev.Model, e.Project, ev.In, ev.Out, ev.CR, ev.CW})
			}
			if (e.Primary != nil || e.Secondary != nil) && (latest == nil || e.RLTS > latest.RLTS) {
				latest = e
			}
		}
		cx = fill(cx, ss, len(entries), days, now, loc)
		if len(entries) == 0 {
			cx.Status, cx.Note = "missing", "No Codex sessions found on this machine."
		}
		if latest != nil {
			cx.Windows = windows(latest, now)
		}
		if len(cx.Windows) == 0 {
			cx.WindowNote = "Codex has not reported a rate-limit window yet."
		}
	}
	out.Providers = append(out.Providers,
		cx,
		Provider{ID: "grok", Label: "Grok", Status: "unavailable", Note: "Not available yet.", Daily: zeroDays(days, now, loc)},
		Provider{ID: "cursor", Label: "Cursor", Status: "unavailable", Note: "Not available yet.", Daily: zeroDays(days, now, loc)},
	)
	for i := range out.Providers {
		p := &out.Providers[i]
		if p.Daily == nil {
			p.Daily = zeroDays(days, now, loc)
		}
		if p.Models == nil {
			p.Models = []Bucket{}
		}
		if p.Projects == nil {
			p.Projects = []Bucket{}
		}
		if p.Windows == nil {
			p.Windows = []Window{}
		}
	}

	if dirty {
		_ = s.cache.save(cachePath) // a cache that cannot be saved only costs speed
	}
	out.Lucidbench = summariseOwn(readOwn(s.DataDir), days, now, loc)
	return out
}

func dayKey(ts int64, loc *time.Location) string {
	return time.Unix(ts, 0).In(loc).Format("2006-01-02")
}

func zeroDays(days int, now time.Time, loc *time.Location) []Day {
	out := make([]Day, days)
	t := now.In(loc)
	for i := 0; i < days; i++ {
		out[i].Date = t.AddDate(0, 0, -(days - 1 - i)).Format("2006-01-02")
	}
	return out
}

func topBuckets(m map[string]*Bucket, n int) []Bucket {
	out := make([]Bucket, 0, len(m))
	for _, b := range m {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Total != out[j].Total {
			return out[i].Total > out[j].Total
		}
		return out[i].Name < out[j].Name
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

func bump(m map[string]*Bucket, name string, in, out, cr, cw int64) {
	b := m[name]
	if b == nil {
		b = &Bucket{Name: name}
		m[name] = b
	}
	b.Runs++
	b.add(in, out, cr, cw)
}

// fill buckets samples into the provider's daily series, totals, models and
// projects.
func fill(p Provider, ss []sample, files, days int, now time.Time, loc *time.Location) Provider {
	p.Daily = zeroDays(days, now, loc)
	idx := map[string]int{}
	for i, d := range p.Daily {
		idx[d.Date] = i
	}
	models, projects := map[string]*Bucket{}, map[string]*Bucket{}
	for _, s := range ss {
		i, ok := idx[dayKey(s.TS, loc)]
		if !ok {
			continue
		}
		p.Daily[i].add(s.In, s.Out, s.CR, s.CW)
		p.Totals.add(s.In, s.Out, s.CR, s.CW)
		m, pr := s.Model, s.Project
		if m == "" {
			m = "unknown"
		}
		if pr == "" {
			pr = "unknown"
		}
		bump(models, m, s.In, s.Out, s.CR, s.CW)
		bump(projects, pr, s.In, s.Out, s.CR, s.CW)
	}
	p.Sources = files
	p.Models, p.Projects = topBuckets(models, 8), topBuckets(projects, 8)
	p.Status = "ok"
	if p.Totals.Total == 0 {
		p.Status = "empty"
	}
	return p
}

func windowLabel(name string, minutes *int64) string {
	if minutes == nil || *minutes <= 0 {
		return name
	}
	m := *minutes
	switch {
	case m == 10080:
		return "weekly"
	case m%1440 == 0:
		return strconv.FormatInt(m/1440, 10) + "d"
	case m%60 == 0:
		return strconv.FormatInt(m/60, 10) + "h"
	}
	return fmt.Sprintf("%dm", m)
}

func windows(e *codexEntry, now time.Time) []Window {
	var out []Window
	for _, w := range []struct {
		name string
		w    *cxWin
	}{{"primary", e.Primary}, {"secondary", e.Secondary}} {
		if w.w == nil {
			continue
		}
		win := Window{Name: w.name, Label: windowLabel(w.name, w.w.Minutes), UsedPercent: w.w.Used, WindowMinutes: w.w.Minutes, ObservedAt: time.Unix(e.RLTS, 0).UTC()}
		if w.w.Resets != nil {
			t := time.Unix(*w.w.Resets, 0).UTC()
			win.ResetsAt = &t
			win.Stale = t.Before(now)
		}
		out = append(out, win)
	}
	return out
}

func summariseOwn(runs []ownRun, days int, now time.Time, loc *time.Location) Own {
	o := Own{Daily: zeroDays(days, now, loc), Sources: []Bucket{}, Providers: []Bucket{}}
	idx := map[string]int{}
	for i, d := range o.Daily {
		idx[d.Date] = i
	}
	src, prov := map[string]*Bucket{}, map[string]*Bucket{}
	for _, r := range runs {
		i, ok := idx[dayKey(r.At.Unix(), loc)]
		if !ok {
			continue
		}
		o.Runs++
		o.Daily[i].add(r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheWrite)
		o.Daily[i].CostUSD += r.CostUSD
		o.Totals.add(r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheWrite)
		o.Totals.CostUSD += r.CostUSD
		for _, x := range []struct {
			m    map[string]*Bucket
			name string
		}{{src, r.Source}, {prov, r.Provider}} {
			name := x.name
			if name == "" {
				name = "unknown"
			}
			bump(x.m, name, r.InputTokens, r.OutputTokens, r.CacheRead, r.CacheWrite)
			x.m[name].CostUSD += r.CostUSD
		}
	}
	o.Sources, o.Providers = topBuckets(src, 0), topBuckets(prov, 0)
	return o
}

// Register adds GET /api/usage/summary?days=N (1 to 90, default 7).
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/usage/summary", func(w http.ResponseWriter, r *http.Request) {
		days := 7
		if v := r.URL.Query().Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > MaxDays {
				http.Error(w, "days must be a number from 1 to "+strconv.Itoa(MaxDays), http.StatusBadRequest)
				return
			}
			days = n
		}
		w.Header().Set("Cache-Control", "no-store")
		apiutil.WriteJSON(w, http.StatusOK, s.Summary(days))
	})
}
