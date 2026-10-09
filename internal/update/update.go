// Package update tells the user when a newer Lucidbench is out. It asks
// GitHub Releases at most once every six hours, keeps the answer in
// <DataDir>/update.json, and never downloads or installs anything: the UI
// shows a quiet banner with links to the release notes and the download.
//
// A failed check is not an error the UI shows: the last good answer stays,
// the status says the check could not reach GitHub, and the next attempt
// waits for the same six hours. A development build ("dev") never asks
// GitHub; it only checks against a feed set for tests (FeedEnv).
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Window is how long one answer from GitHub is reused.
const Window = 6 * time.Hour

// ManualWindow is the shortest gap between two checks the user asks for.
const ManualWindow = time.Minute

// DefaultAPI is GitHub's REST API.
const DefaultAPI = "https://api.github.com"

// FeedEnv names an environment variable that replaces DefaultAPI. It exists
// for the end-to-end tests, which point it at a fake release server; it is
// not a config key.
const FeedEnv = "LUCID_E2E_UPDATE_URL"

// FileName is the cache file under the data dir.
const FileName = "update.json"

// MaxExcerpt is the longest notes excerpt, in characters.
const MaxExcerpt = 400

// Release is the part of a GitHub release this package reads.
type Release struct {
	Tag        string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest is the newest release found, as kept in the cache.
type Latest struct {
	Version      string `json:"version"`
	URL          string `json:"url"`
	DownloadURL  string `json:"download_url"`
	NotesExcerpt string `json:"notes_excerpt"`
	Prerelease   bool   `json:"prerelease"`
}

// cacheFile is <DataDir>/update.json.
type cacheFile struct {
	Repo string `json:"repo"`
	// Channel is "prerelease" when prereleases were considered, else "stable".
	Channel   string    `json:"channel"`
	CheckedAt time.Time `json:"checked_at"`
	// Error is why the last attempt failed; "" when it worked.
	Error  string  `json:"error,omitempty"`
	Latest *Latest `json:"latest,omitempty"`
}

// Status is the body of GET /api/update.
type Status struct {
	Enabled bool   `json:"enabled"`
	Current string `json:"current"`
	Repo    string `json:"repo,omitempty"`
	// Latest, URL, NotesExcerpt and Prerelease describe the newest release
	// found; they are empty until a check has worked.
	Latest       string `json:"latest,omitempty"`
	URL          string `json:"url,omitempty"`
	DownloadURL  string `json:"download_url,omitempty"`
	NotesExcerpt string `json:"notes_excerpt,omitempty"`
	Prerelease   bool   `json:"prerelease"`
	// Available is true when Latest is newer than Current.
	Available bool       `json:"available"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	NextCheck *time.Time `json:"next_check,omitempty"`
	// Offline is true when the last attempt could not reach GitHub.
	Offline bool `json:"offline,omitempty"`
	// Note says, in plain words, why there is no check or no answer.
	Note string `json:"note,omitempty"`
}

// Service checks for updates. The zero value of every optional field works.
type Service struct {
	// Enabled is config update_check.
	Enabled bool
	// Repo is owner/name.
	Repo string
	// Current is the running version (version.Version).
	Current string
	// BaseURL replaces DefaultAPI (tests); "" means GitHub.
	BaseURL string
	DataDir string
	Client  *http.Client
	Now     func() time.Time

	mu sync.Mutex
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) path() string { return filepath.Join(s.DataDir, FileName) }

func (s *Service) load() cacheFile {
	var c cacheFile
	b, err := os.ReadFile(s.path())
	if err == nil {
		_ = json.Unmarshal(b, &c) // an unreadable cache only costs one check
	}
	return c
}

func (s *Service) save(c cacheFile) {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(s.DataDir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(s.DataDir, ".update-*.tmp")
	if err != nil {
		return
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return
	}
	if os.Rename(tmp.Name(), s.path()) != nil {
		os.Remove(tmp.Name())
	}
}

// Check returns the update status. It asks GitHub only when the cached
// answer is older than Window, or, with force, older than ManualWindow.
func (s *Service) Check(ctx context.Context, force bool) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Enabled: s.Enabled, Current: s.Current, Repo: s.Repo}
	if !s.Enabled {
		st.Note = "Update checks are off (update_check: false in your config)."
		return st
	}
	cur, curOK := Parse(s.Current)
	if !curOK && s.BaseURL == "" {
		st.Note = "This is a development build, so it does not check for updates."
		return st
	}
	channel := "stable"
	if !curOK || cur.Pre != "" {
		channel = "prerelease"
	}
	now := s.now()
	c := s.load()
	if c.Repo != s.Repo || c.Channel != channel {
		c = cacheFile{}
	}
	wait := Window
	if force {
		wait = ManualWindow
	}
	if c.CheckedAt.IsZero() || now.Sub(c.CheckedAt) >= wait || c.CheckedAt.After(now) {
		latest, err := s.fetch(ctx, channel == "prerelease")
		c.Repo, c.Channel, c.CheckedAt, c.Error = s.Repo, channel, now.UTC(), ""
		switch {
		case err != nil:
			c.Error = err.Error()
		default:
			c.Latest = latest
		}
		s.save(c)
	}
	at, next := c.CheckedAt, c.CheckedAt.Add(Window)
	st.CheckedAt, st.NextCheck = &at, &next
	if c.Error != "" {
		st.Offline = true
		st.Note = "Could not reach GitHub to check for updates; Lucidbench tries again later."
	}
	if l := c.Latest; l != nil {
		st.Latest, st.URL, st.DownloadURL, st.NotesExcerpt, st.Prerelease = l.Version, l.URL, l.DownloadURL, l.NotesExcerpt, l.Prerelease
		if lv, ok := Parse(l.Version); ok {
			st.Available = !curOK || Compare(lv, cur) > 0
		}
	} else if c.Error == "" {
		st.Note = "No releases are published yet."
	}
	return st
}

// fetch reads the releases list and returns the newest one that fits the
// channel, or nil when there is none.
func (s *Service) fetch(ctx context.Context, pre bool) (*Latest, error) {
	base := s.BaseURL
	if base == "" {
		base = DefaultAPI
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	url := strings.TrimRight(base, "/") + "/repos/" + s.Repo + "/releases?per_page=30"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lucidbench/"+s.Current)
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	var rels []Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rels); err != nil {
		return nil, errors.New("GitHub's answer could not be read")
	}
	return Pick(rels, pre), nil
}

// Pick returns the newest release by semver, skipping drafts, tags that are
// not versions, and prereleases unless pre is set.
func Pick(rels []Release, pre bool) *Latest {
	var best *Release
	var bestV Semver
	for i := range rels {
		r := &rels[i]
		v, ok := Parse(r.Tag)
		if r.Draft || !ok || (!pre && (r.Prerelease || v.Pre != "")) {
			continue
		}
		if best == nil || Compare(v, bestV) > 0 {
			best, bestV = r, v
		}
	}
	if best == nil {
		return nil
	}
	l := &Latest{Version: bestV.String(), URL: best.HTMLURL, DownloadURL: best.HTMLURL, NotesExcerpt: Excerpt(best.Body), Prerelease: best.Prerelease || bestV.Pre != ""}
	for _, suffix := range []string{"-setup.exe", ".exe", ".msi"} {
		for _, a := range best.Assets {
			if strings.HasSuffix(strings.ToLower(a.Name), suffix) && strings.HasPrefix(a.URL, "https://") {
				l.DownloadURL = a.URL
				return l
			}
		}
	}
	return l
}

// Excerpt is the start of release notes in plain lines: headings and blank
// lines dropped, cut at MaxExcerpt characters on a line or word boundary.
func Excerpt(body string) string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		lines = append(lines, l)
	}
	out := strings.Join(lines, "\n")
	if utf8.RuneCountInString(out) <= MaxExcerpt {
		return out
	}
	r := []rune(out)[:MaxExcerpt]
	cut := string(r)
	if i := strings.LastIndexAny(cut, "\n "); i > MaxExcerpt/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \n,;:") + "…"
}

// Register adds the update routes:
//
//	GET  /api/update        the cached status; asks GitHub when the answer is older than six hours
//	POST /api/update/check  asks GitHub now (at most once a minute); needs X-Lucid-Confirm
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		apiutil.WriteJSON(w, http.StatusOK, s.Check(r.Context(), false))
	})
	mux.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		apiutil.WriteJSON(w, http.StatusOK, s.Check(r.Context(), true))
	})
}
