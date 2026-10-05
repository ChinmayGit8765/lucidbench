package ci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Runner is a self-hosted runner registered on a GitHub repository.
type Runner struct {
	Repo   string   `json:"repo"`
	ID     int64    `json:"id"`
	Name   string   `json:"name"`
	OS     string   `json:"os"`
	Status string   `json:"status"` // online or offline
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels"`
	// Container is the local container running this runner, when one matches.
	Container string `json:"container,omitempty"`
}

// Run is a GitHub Actions workflow run.
type Run struct {
	Repo         string `json:"repo"`
	ID           int64  `json:"id"`
	RunNumber    int    `json:"run_number"`
	Name         string `json:"name"`
	Title        string `json:"title"`
	Branch       string `json:"branch"`
	Event        string `json:"event"`
	Status       string `json:"status"`     // queued, in_progress, completed, ...
	Conclusion   string `json:"conclusion"` // success, failure, cancelled, ... or "" while running
	CreatedAt    string `json:"created_at"`
	RunStartedAt string `json:"run_started_at"`
	UpdatedAt    string `json:"updated_at"`
	HTMLURL      string `json:"html_url"`
	Actor        string `json:"actor,omitempty"`
}

// DefaultAPI is the GitHub REST API base URL.
const DefaultAPI = "https://api.github.com"

// CacheTTL is how long a GitHub response is reused, to respect rate limits.
const CacheTTL = 30 * time.Second

// ErrNoToken means no GitHub token could be found.
var ErrNoToken = errors.New("no GitHub token: set the variable named by ci.github.token (GITHUB_TOKEN by default) or sign in with `gh auth login`")

// TokenSource finds the GitHub token. Value is returned to the HTTP client
// only; callers must never log, print or return it.
type TokenSource struct {
	Ref    config.SecretRef
	Getenv func(string) string
	// GH runs `gh auth token`; nil disables the fallback.
	GH func(ctx context.Context) (string, error)

	mu      sync.Mutex
	ghToken string
	ghAt    time.Time
}

// Token returns the token and where it came from: "env", "gh" or "none".
func (t *TokenSource) Token(ctx context.Context) (value, source string) {
	if t.Getenv != nil && t.Ref != "" {
		if v := strings.TrimSpace(t.Ref.Resolve(t.Getenv)); v != "" {
			return v, "env"
		}
	}
	if t.GH == nil {
		return "", "none"
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ghToken != "" && time.Since(t.ghAt) < 5*time.Minute {
		return t.ghToken, "gh"
	}
	if t.ghAt.IsZero() || time.Since(t.ghAt) > 30*time.Second {
		v, err := t.GH(ctx)
		t.ghAt = time.Now()
		t.ghToken = ""
		if err == nil {
			t.ghToken = strings.TrimSpace(v)
		}
	}
	if t.ghToken == "" {
		return "", "none"
	}
	return t.ghToken, "gh"
}

// Source reports where the token comes from without returning it.
func (t *TokenSource) Source(ctx context.Context) string {
	_, s := t.Token(ctx)
	return s
}

// GHAuthToken runs `gh auth token` when the GitHub CLI is installed.
func GHAuthToken(ctx context.Context) (string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "auth", "token").Output()
	return string(out), err
}

// GitHub is a small cached client for the Actions endpoints Lucidbench uses.
type GitHub struct {
	BaseURL string
	HTTP    *http.Client
	Tokens  *TokenSource
	TTL     time.Duration

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at   time.Time
	body []byte
	err  error
}

// APIError is a non-2xx answer from GitHub. It holds GitHub's message, never
// request headers.
type APIError struct {
	Status  int
	Path    string
	Message string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("GitHub %s: %d %s", e.Path, e.Status, msg)
}

func (g *GitHub) base() string {
	if g.BaseURL != "" {
		return strings.TrimRight(g.BaseURL, "/")
	}
	return DefaultAPI
}

func (g *GitHub) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (g *GitHub) do(ctx context.Context, method, path string) ([]byte, error) {
	tok, _ := g.Tokens.Token(ctx)
	if tok == "" {
		return nil, ErrNoToken
	}
	req, err := http.NewRequestWithContext(ctx, method, g.base()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "lucidbench")
	resp, err := g.client().Do(req)
	if err != nil {
		// url.Error carries only the URL, which holds no credentials.
		return nil, fmt.Errorf("GitHub %s: %w", path, errors.Unwrap(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		var m struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &m)
		return nil, &APIError{Status: resp.StatusCode, Path: path, Message: m.Message}
	}
	return body, nil
}

// get fetches path through the cache. Errors are cached too, so a repo that
// fails is not retried on every poll.
func (g *GitHub) get(ctx context.Context, path string) ([]byte, error) {
	ttl := g.TTL
	if ttl == 0 {
		ttl = CacheTTL
	}
	g.mu.Lock()
	if c, ok := g.cache[path]; ok && time.Since(c.at) < ttl {
		g.mu.Unlock()
		return c.body, c.err
	}
	g.mu.Unlock()
	body, err := g.do(ctx, http.MethodGet, path)
	if errors.Is(err, ErrNoToken) || ctx.Err() != nil {
		return nil, err // not worth caching: fixable at once
	}
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[string]cached{}
	}
	g.cache[path] = cached{at: time.Now(), body: body, err: err}
	g.mu.Unlock()
	return body, err
}

// invalidate drops cached responses for a repository.
func (g *GitHub) invalidate(repo string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	prefix := repoPath(repo) + "/"
	for k := range g.cache {
		if strings.HasPrefix(k, prefix) {
			delete(g.cache, k)
		}
	}
}

func repoPath(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
}

// Runners lists the repository's self-hosted runners.
func (g *GitHub) Runners(ctx context.Context, repo string) ([]Runner, error) {
	body, err := g.get(ctx, repoPath(repo)+"/actions/runners?per_page=100")
	if err != nil {
		return nil, err
	}
	return parseRunners(repo, body)
}

func parseRunners(repo string, body []byte) ([]Runner, error) {
	var resp struct {
		Runners []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			OS     string `json:"os"`
			Status string `json:"status"`
			Busy   bool   `json:"busy"`
			Labels []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"runners"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse runners for %s: %w", repo, err)
	}
	out := []Runner{}
	for _, r := range resp.Runners {
		labels := []string{}
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, Runner{Repo: repo, ID: r.ID, Name: r.Name, OS: r.OS, Status: r.Status, Busy: r.Busy, Labels: labels})
	}
	return out, nil
}

// RunsPerRepo is how many recent workflow runs are fetched per repository.
const RunsPerRepo = 10

// Runs lists the repository's most recent workflow runs.
func (g *GitHub) Runs(ctx context.Context, repo string) ([]Run, error) {
	body, err := g.get(ctx, repoPath(repo)+"/actions/runs?per_page="+strconv.Itoa(RunsPerRepo))
	if err != nil {
		return nil, err
	}
	return parseRuns(repo, body)
}

func parseRuns(repo string, body []byte) ([]Run, error) {
	var resp struct {
		WorkflowRuns []struct {
			ID           int64   `json:"id"`
			RunNumber    int     `json:"run_number"`
			Name         string  `json:"name"`
			DisplayTitle string  `json:"display_title"`
			HeadBranch   string  `json:"head_branch"`
			Event        string  `json:"event"`
			Status       string  `json:"status"`
			Conclusion   *string `json:"conclusion"`
			CreatedAt    string  `json:"created_at"`
			RunStartedAt string  `json:"run_started_at"`
			UpdatedAt    string  `json:"updated_at"`
			HTMLURL      string  `json:"html_url"`
			Actor        *struct {
				Login string `json:"login"`
			} `json:"actor"`
		} `json:"workflow_runs"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse runs for %s: %w", repo, err)
	}
	out := []Run{}
	for _, r := range resp.WorkflowRuns {
		run := Run{
			Repo: repo, ID: r.ID, RunNumber: r.RunNumber, Name: r.Name, Title: r.DisplayTitle,
			Branch: r.HeadBranch, Event: r.Event, Status: r.Status, CreatedAt: r.CreatedAt,
			RunStartedAt: r.RunStartedAt, UpdatedAt: r.UpdatedAt, HTMLURL: r.HTMLURL,
		}
		if r.Conclusion != nil {
			run.Conclusion = *r.Conclusion
		}
		if r.Actor != nil {
			run.Actor = r.Actor.Login
		}
		out = append(out, run)
	}
	return out, nil
}

// RerunFailed re-runs the failed jobs of a workflow run.
func (g *GitHub) RerunFailed(ctx context.Context, repo string, id int64) error {
	_, err := g.do(ctx, http.MethodPost, repoPath(repo)+"/actions/runs/"+strconv.FormatInt(id, 10)+"/rerun-failed-jobs")
	g.invalidate(repo)
	return err
}
