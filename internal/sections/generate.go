package sections

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/prompts"
)

// SystemPrompt is what the model is told, versioned with the code.
//
//go:embed prompts/section.md
var SystemPrompt string

// PromptVersion reads "<!-- section vN -->" from the prompt's first line.
func PromptVersion() string {
	if m := regexp.MustCompile(`^<!--\s*section\s+(v\d+)\s*-->`).FindStringSubmatch(SystemPrompt); m != nil {
		return m[1]
	}
	return "unversioned"
}

func systemPrompt() string {
	if i := strings.Index(SystemPrompt, "-->"); i >= 0 {
		return strings.TrimLeft(SystemPrompt[i+3:], "\r\n")
	}
	return SystemPrompt
}

// Providers is the order the user's CLIs are tried in when none is chosen.
var Providers = []string{"claude", "codex", "grok"}

// Limits on a description and on one call.
const (
	MinDescription   = 3
	MaxDescriptionIn = 1000
	GenerateTimeout  = 2 * time.Minute
)

// Errors the HTTP layer maps to statuses.
var (
	ErrBadRequest = errors.New("bad request")
	ErrBadOutput  = errors.New("the model did not return a usable section")
)

// GenerateRequest is the body of POST /api/sections/generate.
type GenerateRequest struct {
	Description string `json:"description"`
	Provider    string `json:"provider,omitempty"`
	Profile     string `json:"profile,omitempty"`
	Model       string `json:"model,omitempty"`
	Placement   string `json:"placement,omitempty"`
}

// Generated is a preview: valid, not saved.
type Generated struct {
	Section  Section         `json:"section"`
	Provider string          `json:"provider"`
	Model    string          `json:"model,omitempty"`
	Prompt   string          `json:"prompt_version"`
	Usage    agentexec.Usage `json:"usage"`
}

// RunRecord is one generation's cost, for the Usage page.
type RunRecord struct {
	Kind    string          `json:"kind"` // "section"
	Created time.Time       `json:"created"`
	Error   string          `json:"error,omitempty"`
	Usage   agentexec.Usage `json:"usage"`
}

// Generator asks the user's CLI for a section, with no tools.
type Generator struct {
	Runner *agentexec.Runner
	Store  *Store
	// RunsDir keeps one record per call; "" records nothing.
	RunsDir string
	Now     func() time.Time
}

func (g *Generator) runner() *agentexec.Runner {
	if g.Runner != nil {
		return g.Runner
	}
	return &agentexec.Runner{}
}

func (g *Generator) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Generator) pickProvider(want string) (string, error) {
	if want != "" {
		if _, ok := agentexec.Logins[want]; !ok {
			return "", fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
		}
		return want, nil
	}
	look := g.runner().LookPath
	if look == nil {
		look = exec.LookPath
	}
	for _, p := range Providers {
		if _, err := look(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: no provider CLI is installed (claude, codex or grok)", agentexec.ErrCLIMissing)
}

func (g *Generator) record(u agentexec.Usage, runErr error) {
	if g.RunsDir == "" || (u.DurationMS == 0 && u.InputTokens == 0 && u.OutputTokens == 0 && u.CostUSD == 0) {
		return
	}
	rec := RunRecord{Kind: "section", Created: g.now().UTC(), Usage: u}
	if runErr != nil {
		rec.Error = agentexec.Excerpt(runErr.Error())
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	if err := os.MkdirAll(g.RunsDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(g.RunsDir, rec.Created.Format("20060102-150405")+"-"+hex.EncodeToString(b)+".json"), data, 0o600)
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Generate describes a section in words to the chosen CLI and returns it
// validated. Nothing is saved.
func (g *Generator) Generate(ctx context.Context, req GenerateRequest) (*Generated, error) {
	d := strings.TrimSpace(req.Description)
	if len(d) < MinDescription || len(d) > MaxDescriptionIn {
		return nil, fmt.Errorf("%w: describe the section in %d to %d characters", ErrBadRequest, MinDescription, MaxDescriptionIn)
	}
	if hits := prompts.FindSecrets(d); len(hits) > 0 {
		return nil, fmt.Errorf("%w: the description has what looks like a secret (%s); remove it first", ErrBadRequest, hits[0].Kind)
	}
	if req.Placement != "" && req.Placement != PlaceOverview && req.Placement != PlaceProject {
		return nil, fmt.Errorf("%w: placement must be overview or project", ErrBadRequest)
	}
	provider, err := g.pickProvider(req.Provider)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = prompts.CheapModels[provider]
	}
	ask := "The user's description:\n\n" + d
	if req.Placement == PlaceProject {
		ask += "\n\nThis section goes on each project's page: use placement \"project\"."
	}
	res, err := g.runner().Run(ctx, agentexec.Request{
		Provider: provider, Profile: req.Profile, Model: model, SystemPrompt: systemPrompt(), Prompt: ask,
		Tools: agentexec.ToolsNone, Timeout: GenerateTimeout,
	})
	if res != nil {
		g.record(res.Usage, err)
	}
	if err != nil {
		return nil, err
	}
	s, err := fromAnswer(res.Text)
	if err != nil {
		return nil, err
	}
	if req.Placement != "" {
		s.Placement = req.Placement
	}
	base := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s.ID), "-"), "-")
	if base == "" {
		base = strings.Trim(slugRE.ReplaceAllString(strings.ToLower(s.Title), "-"), "-")
	}
	if g.Store != nil {
		s.ID = g.Store.FreeID(base)
	} else if base != "" {
		s.ID = base
	}
	s = Normalise(s)
	if err := Validate(s); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
	}
	return &Generated{Section: s, Provider: provider, Model: model, Prompt: PromptVersion(), Usage: res.Usage}, nil
}

// fromAnswer finds the JSON object in a model answer, tolerating a code
// fence or prose around it.
func fromAnswer(text string) (Section, error) {
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j <= i {
		return Section{}, fmt.Errorf("%w: no JSON object in the answer (it began %q)", ErrBadOutput, agentexec.Excerpt(text))
	}
	s, err := Parse([]byte(text[i : j+1]))
	if err != nil {
		return Section{}, fmt.Errorf("%w: %v", ErrBadOutput, err)
	}
	return s, nil
}
