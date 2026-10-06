// Package cloud is a read-only inventory of what the user has deployed, read
// through the cloud CLIs they are already signed in to (gcloud, wrangler,
// vercel, az, aws). Lucidbench never stores a cloud credential: each call
// runs the CLI as the user, asks for JSON, and keeps only the fields the UI
// shows. Tokens, emails other than the account id a CLI itself prints, and
// environment values are never returned.
package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Provider states.
const (
	StateConnected    = "connected"
	StateNotInstalled = "not_installed"
	StateNotSignedIn  = "not_signed_in"
	StateError        = "error"
)

// Resource statuses. An empty status means the resource has no health to
// report (a project list entry, for example).
const (
	StatusReady    = "ready"
	StatusFailed   = "failed"
	StatusBuilding = "building"
	StatusUnknown  = "unknown"
)

// Resource kinds. Deployments are history rows and never counted as health.
const (
	KindService    = "service"
	KindProject    = "project"
	KindPages      = "pages"
	KindWorker     = "worker"
	KindDeployment = "deployment"
	KindGCPProject = "gcp-project"
)

// Resource is one thing in a provider's inventory.
type Resource struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Region  string `json:"region,omitempty"`
	Project string `json:"project,omitempty"`
	Status  string `json:"status,omitempty"`
	// Detail is a short line: the last revision, the branch, the target.
	Detail  string `json:"detail,omitempty"`
	URL     string `json:"url,omitempty"`
	Console string `json:"console,omitempty"`
	// Updated is when it last deployed (RFC 3339); UpdatedText is the CLI's
	// own wording when it only gives "6 days ago".
	Updated     string `json:"updated,omitempty"`
	UpdatedText string `json:"updated_text,omitempty"`
}

// Section is one list inside a provider. Each fails on its own.
type Section struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Resources []Resource `json:"resources"`
	// Note explains an empty or partial list; Error is why it could not load.
	Note  string `json:"note,omitempty"`
	Error string `json:"error,omitempty"`
}

// Counts tallies the health of a provider's resources.
type Counts struct {
	Ready    int `json:"ready"`
	Failed   int `json:"failed"`
	Building int `json:"building"`
}

// Summary is one provider's inventory.
type Summary struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	CLI      string `json:"cli"`
	State    string `json:"state"`
	// Message is the one-line reason for any state but connected.
	Message string `json:"message,omitempty"`
	// Account is the id the CLI itself prints; Scope is its project, team or
	// account.
	Account  string    `json:"account,omitempty"`
	Scope    string    `json:"scope,omitempty"`
	Login    string    `json:"login"`
	Sections []Section `json:"sections"`
	Counts   Counts    `json:"counts"`
	Fetched  string    `json:"fetched"`
}

// Runner runs one CLI and returns its stdout and a short stderr.
type Runner func(ctx context.Context, name string, args ...string) (stdout []byte, stderr string, err error)

// callTimeout bounds each CLI call.
var callTimeout = 30 * time.Second

// maxOutput caps what is read from a CLI.
const maxOutput = 8 << 20

type limitWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// ExecRunner runs the CLI with os/exec. The environment is passed through
// so the CLI finds its own sign-in, and is never read or returned here.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "CLOUDSDK_CORE_DISABLE_PROMPTS=1", "WRANGLER_SEND_METRICS=false", "CI=")
	// A Windows shim (npm's .cmd) can leave a child holding the pipes after
	// the kill; give up on them soon after the context ends.
	cmd.WaitDelay = 3 * time.Second
	out, errw := &limitWriter{max: maxOutput}, &limitWriter{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, errw
	err := cmd.Run()
	return out.buf.Bytes(), errw.buf.String(), err
}

// cliError is a failed CLI call. SignedOut means the CLI said the user is
// not (or no longer) signed in.
type cliError struct {
	SignedOut bool
	Msg       string
}

func (e *cliError) Error() string { return e.Msg }

var (
	ansiRE   = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	emailRE  = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	tokenRE  = regexp.MustCompile(`[A-Za-z0-9_\-]{32,}|\b(?:AKIA|ASIA|AIDA|AROA)[A-Z0-9]{12,}\b|\b(?:ya29|ghp|gho|sk|pk|rk)[_.][A-Za-z0-9_.\-]{12,}`)
	signedRE = regexp.MustCompile(`(?i)(not (logged|signed) in|no existing credentials|auth(entication)? (login|required)|auth login|az login|wrangler login|vercel login|unable to locate credentials|expired ?token|token (has )?expired|reauthenticat|not authenticated|invalid.?client.?token|please (log|sign) in|run .?login|sso session)`)
)

// scrub makes a CLI's own words safe to show: no colour codes, no emails, no
// token-shaped strings, at most two lines joined, and short.
func scrub(s string) string {
	s = ansiRE.ReplaceAllString(s, "")
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.Trim(l, "✘▲⛅️🪵 \t\r"))
		if l == "" || strings.HasPrefix(l, "<claude-code-hint") || strings.HasPrefix(l, "> Update available") || strings.HasPrefix(l, "Fetching ") || strings.HasPrefix(l, "Logs were written") || strings.HasPrefix(l, "If you think this is a bug") {
			continue
		}
		keep = append(keep, l)
		if len(keep) == 2 {
			break
		}
	}
	line := strings.Join(keep, " ")
	line = emailRE.ReplaceAllString(line, "<email>")
	line = tokenRE.ReplaceAllString(line, "<redacted>")
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	return line
}

// caller runs one provider's CLI calls.
type caller struct {
	run Runner
	cli string
}

// raw runs the CLI and returns stdout, or a cliError.
func (c *caller) raw(ctx context.Context, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, stderr, err := c.run(cctx, c.cli, args...)
	if err == nil {
		return out, nil
	}
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return nil, &cliError{Msg: fmt.Sprintf("%s timed out after %d seconds", c.cli, int(callTimeout.Seconds()))}
	}
	// A CLI may explain on either stream.
	text := stderr
	if strings.TrimSpace(scrub(text)) == "" && !bytes.ContainsAny(bytes.TrimSpace(out)[:min(1, len(bytes.TrimSpace(out)))], "{[") {
		text = string(out)
	}
	msg := scrub(text)
	if msg == "" {
		msg = fmt.Sprintf("%s failed: %v", c.cli, scrub(err.Error()))
	}
	return nil, &cliError{SignedOut: signedRE.MatchString(text), Msg: msg}
}

// json runs the CLI and decodes the first JSON value in its stdout (some
// CLIs print a banner first).
func (c *caller) json(ctx context.Context, dst any, args ...string) error {
	out, err := c.raw(ctx, args...)
	if err != nil {
		return err
	}
	i := bytes.IndexAny(out, "{[")
	if i < 0 {
		return &cliError{Msg: c.cli + " printed no JSON"}
	}
	if err := json.NewDecoder(bytes.NewReader(out[i:])).Decode(dst); err != nil {
		return &cliError{Msg: c.cli + " printed JSON this version does not understand"}
	}
	return nil
}

func errText(err error) string {
	var ce *cliError
	if errors.As(err, &ce) {
		return ce.Msg
	}
	return scrub(err.Error())
}

// providerDef is one CLI adapter.
type providerDef struct {
	id, label, cli, login string
	fetch                 func(ctx context.Context, c *caller, targets []projects.Deploy) Summary
}

var providers = []providerDef{
	{"gcloud", "Google Cloud", "gcloud", "gcloud auth login", fetchGcloud},
	{"wrangler", "Cloudflare", "wrangler", "wrangler login", fetchWrangler},
	{"vercel", "Vercel", "vercel", "vercel login", fetchVercel},
	{"az", "Azure", "az", "az login", fetchAz},
	{"aws", "AWS", "aws", "aws configure sso", fetchAws},
}

// ProviderIDs lists the providers in display order.
func ProviderIDs() []string {
	ids := make([]string, len(providers))
	for i, p := range providers {
		ids[i] = p.id
	}
	return ids
}

func find(id string) *providerDef {
	for i := range providers {
		if providers[i].id == id {
			return &providers[i]
		}
	}
	return nil
}

// Service fetches and caches the inventory.
type Service struct {
	Run      Runner
	LookPath func(string) (string, error)
	// Projects loads the portfolio. Its deploy entries are matched against
	// the inventory, and name what the CLIs cannot list (Cloudflare Workers).
	Projects func() []projects.Project
	// TTL is how long a connected inventory is reused; others are kept for
	// a short while so a sign-in shows up quickly.
	TTL time.Duration
	Now func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	mu  sync.Mutex
	sum *Summary
	at  time.Time
	sig string
}

// DefaultTTL is how long a connected inventory is cached.
const DefaultTTL = 5 * time.Minute

// otherTTL is how long a missing or failed provider is cached.
const otherTTL = 30 * time.Second

// New returns a Service for the real host.
func New(load func() []projects.Project) *Service {
	return &Service{Run: ExecRunner, LookPath: exec.LookPath, Projects: load, TTL: DefaultTTL, Now: time.Now}
}

func (s *Service) entry(id string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string]*entry{}
	}
	e := s.entries[id]
	if e == nil {
		e = &entry{}
		s.entries[id] = e
	}
	return e
}

func (s *Service) targetsFor(id string) []projects.Deploy {
	if s.Projects == nil {
		return nil
	}
	var out []projects.Deploy
	for _, p := range s.Projects() {
		for _, d := range p.Deploy {
			if d.Provider == id {
				out = append(out, d)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Service+"|"+a.Region+"|"+a.Project < b.Service+"|"+b.Region+"|"+b.Project
	})
	return out
}

// Get returns one provider's inventory, from the cache unless force is set
// or the deploy entries for it changed. It returns false for an unknown id.
func (s *Service) Get(ctx context.Context, id string, force bool) (Summary, bool) {
	p := find(id)
	if p == nil {
		return Summary{}, false
	}
	targets := s.targetsFor(id)
	var sig strings.Builder
	for _, t := range targets {
		sig.WriteString(t.Service + "|" + t.Region + "|" + t.Project + ";")
	}
	e := s.entry(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	ttl := otherTTL
	if e.sum != nil && e.sum.State == StateConnected {
		ttl = s.TTL
	}
	if !force && e.sum != nil && e.sig == sig.String() && s.Now().Sub(e.at) < ttl {
		return *e.sum, true
	}
	// A browser that closes the page must not poison the cache with a
	// cancelled run: the fetch outlives the request.
	sum := s.fetch(context.WithoutCancel(ctx), p, targets)
	e.sum, e.at, e.sig = &sum, s.Now(), sig.String()
	return sum, true
}

func (s *Service) fetch(ctx context.Context, p *providerDef, targets []projects.Deploy) Summary {
	base := Summary{Provider: p.id, Label: p.label, CLI: p.cli, Login: p.login, Sections: []Section{}, Fetched: s.Now().UTC().Format(time.RFC3339)}
	if _, err := s.LookPath(p.cli); err != nil {
		base.State, base.Message = StateNotInstalled, p.cli+" is not installed"
		return base
	}
	sum := p.fetch(ctx, &caller{run: s.Run, cli: p.cli}, targets)
	sum.Provider, sum.Label, sum.CLI, sum.Login, sum.Fetched = base.Provider, base.Label, base.CLI, base.Login, base.Fetched
	if sum.Sections == nil {
		sum.Sections = []Section{}
	}
	for _, sec := range sum.Sections {
		for _, r := range sec.Resources {
			if r.Kind == KindDeployment {
				continue
			}
			switch r.Status {
			case StatusReady:
				sum.Counts.Ready++
			case StatusFailed:
				sum.Counts.Failed++
			case StatusBuilding:
				sum.Counts.Building++
			}
		}
	}
	return sum
}

// All returns every provider's inventory. Two CLIs run at a time: gcloud,
// wrangler and vercel each start a heavy runtime.
func (s *Service) All(ctx context.Context, force bool) []Summary {
	out := make([]Summary, len(providers))
	var wg sync.WaitGroup
	gate := make(chan struct{}, 2)
	for i, p := range providers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			out[i], _ = s.Get(ctx, p.id, force)
		}()
	}
	wg.Wait()
	return out
}

// failed turns a CLI error from the call that proves sign-in into a state.
func failed(sum Summary, err error) Summary {
	var ce *cliError
	if errors.As(err, &ce) && ce.SignedOut {
		sum.State, sum.Message = StateNotSignedIn, "not signed in"
		return sum
	}
	sum.State, sum.Message = StateError, errText(err)
	return sum
}

// sectionError records a failed section without failing the provider.
func sectionError(sec Section, err error) Section {
	sec.Resources = []Resource{}
	sec.Error = errText(err)
	return sec
}
