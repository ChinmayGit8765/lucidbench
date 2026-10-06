package prompts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// ImproveProviders is the order the user's CLIs are tried in when no
// provider is chosen.
var ImproveProviders = []string{"claude", "codex", "grok"}

// CheapModels is the model each provider uses for Improve by default: the
// cheapest one that restructures text well. "" leaves the CLI's default.
var CheapModels = map[string]string{"claude": "haiku", "codex": "", "grok": ""}

// MaxImprove is the longest rough prompt Improve takes, in bytes.
const MaxImprove = 20000

// ImproveTimeout bounds the one model call.
const ImproveTimeout = 2 * time.Minute

// ErrConfidential refuses to send a confidential project or page.
var ErrConfidential = errors.New("confidential")

const improveSystem = `You restructure a rough prompt for an AI coding agent into clear sections. You do not do the task.

Reply with one JSON object and nothing else, no code fence:
{"role": "", "context": "", "contract": "", "task": "", "constraints": "", "verify": "", "report": ""}

- role: who the agent is and what kind of job this is, one or two sentences.
- context: facts the agent needs that are in the rough prompt.
- contract: interfaces, routes, formats or files the work must keep, if the prompt names any.
- task: what to do, scoped to one slice, ending with "Done when" lines, each with a proof the agent can run or look at.
- constraints: what the agent must not do.
- verify: the commands or checks that prove the work, from the prompt or the obvious ones for the task.
- report: what the final answer must contain.

Rules:
- Keep every fact, name, path and requirement from the rough prompt. Do not invent facts about the code.
- Leave a section "" when the rough prompt gives nothing for it, except verify and report: propose those from the task.
- Write Markdown inside each value; use "- " bullets for lists. Keep it short and plain.`

// ImproveRequest is the body of POST /api/prompts/improve.
type ImproveRequest struct {
	Text     string    `json:"text,omitempty"`
	Sections []Section `json:"sections,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Model    string    `json:"model,omitempty"`
	Project  string    `json:"project,omitempty"`
}

// ImproveResult is a preview: the caller decides whether to apply it.
type ImproveResult struct {
	Sections []Section       `json:"sections"`
	Text     string          `json:"text"`
	Provider string          `json:"provider"`
	Model    string          `json:"model,omitempty"`
	Usage    agentexec.Usage `json:"usage"`
}

// Improver asks a provider to restructure a prompt.
type Improver struct {
	Runner   *agentexec.Runner
	Resolver *Resolver
	// RunsDir keeps one record per call (<DataDir>/prompts/runs), so the
	// Usage page counts what Improve spent; "" records nothing.
	RunsDir string
	Now     func() time.Time
}

// RunRecord is what one Improve call cost, as the Usage page reads it.
type RunRecord struct {
	Kind    string          `json:"kind"` // "improve"
	Created time.Time       `json:"created"`
	Error   string          `json:"error,omitempty"`
	Usage   agentexec.Usage `json:"usage"`
}

// record keeps the usage of one call. A call that failed before the CLI
// reported anything is not recorded.
func (im *Improver) record(u agentexec.Usage, runErr error) {
	if im.RunsDir == "" || (u.DurationMS == 0 && u.InputTokens == 0 && u.OutputTokens == 0 && u.CostUSD == 0) {
		return
	}
	now := time.Now
	if im.Now != nil {
		now = im.Now
	}
	rec := RunRecord{Kind: "improve", Created: now().UTC(), Usage: u}
	if runErr != nil {
		rec.Error = agentexec.Excerpt(runErr.Error())
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	_ = writeFile(filepath.Join(im.RunsDir, rec.Created.Format("20060102-150405")+"-"+hex.EncodeToString(b)+".json"), data)
}

func (im *Improver) runner() *agentexec.Runner {
	if im.Runner != nil {
		return im.Runner
	}
	return &agentexec.Runner{}
}

// pickProvider returns the requested provider, or the first installed one.
func (im *Improver) pickProvider(want string) (string, error) {
	if want != "" {
		if _, ok := agentexec.Logins[want]; !ok {
			return "", fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRef)
		}
		return want, nil
	}
	look := im.runner().LookPath
	if look == nil {
		look = exec.LookPath
	}
	for _, p := range ImproveProviders {
		if _, err := look(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: no provider CLI is installed (claude, codex or grok)", agentexec.ErrCLIMissing)
}

// check refuses what may not leave the machine: a confidential project, a
// [[link]] to a confidential Memory page, or a secret-looking string.
func (im *Improver) check(text, project string) error {
	r := im.Resolver
	if project != "" && r != nil {
		p, err := r.project(project)
		if err != nil {
			return err
		}
		if p.Visibility == "confidential" {
			return fmt.Errorf("%w: %s is a confidential project; Studio never sends it to a provider", ErrConfidential, p.Name)
		}
	}
	if links := memory.Links(text); len(links) > 0 {
		if r == nil || r.Vault == nil {
			return fmt.Errorf("%w: the prompt links Memory pages, but there is no vault to check them against", ErrConfidential)
		}
		v, err := r.Vault()
		if err != nil {
			return fmt.Errorf("%w: the prompt links Memory pages, but the vault cannot be opened to check them", ErrConfidential)
		}
		for _, l := range links {
			for _, p := range []string{l + ".md", l} {
				if conf, err := v.IsConfidential(p); err == nil && conf {
					return fmt.Errorf("%w: [[%s]] is a confidential Memory page; Studio never sends it to a provider", ErrConfidential, l)
				}
			}
		}
	}
	if hits := FindSecrets(text); len(hits) > 0 {
		return fmt.Errorf("%w: the prompt has what looks like a secret (%s: %s); remove it first", ErrBadRef, hits[0].Kind, hits[0].Excerpt)
	}
	return nil
}

// Improve restructures a rough prompt into sections with one model call and
// no tools. Nothing is applied: the result is a preview.
func (im *Improver) Improve(ctx context.Context, req ImproveRequest) (*ImproveResult, error) {
	text := strings.TrimSpace(req.Text)
	if text == "" && len(req.Sections) > 0 {
		text = strings.TrimSpace(Render(req.Sections, nil, nil).Text)
	}
	if text == "" {
		return nil, fmt.Errorf("%w: write a prompt first", ErrBadRef)
	}
	if len(text) > MaxImprove {
		return nil, fmt.Errorf("%w: the prompt is longer than %d characters", ErrBadRef, MaxImprove)
	}
	if err := im.check(text, req.Project); err != nil {
		return nil, err
	}
	provider, err := im.pickProvider(req.Provider)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = CheapModels[provider]
	}
	res, err := im.runner().Run(ctx, agentexec.Request{
		Provider: provider, SystemPrompt: improveSystem, Prompt: "The rough prompt:\n\n" + text,
		Tools: agentexec.ToolsNone, Model: model, Timeout: ImproveTimeout,
	})
	if res != nil {
		im.record(res.Usage, err)
	}
	if err != nil {
		return nil, err
	}
	secs, err := ParseSections(res.Text)
	if err != nil {
		return nil, fmt.Errorf("%s answered, but not with the sections: %v", provider, err)
	}
	out := &ImproveResult{Sections: secs, Provider: provider, Model: model, Usage: res.Usage}
	out.Text = Render(secs, nil, nil).Text
	return out, nil
}

// ParseSections reads a model's answer: a JSON object keyed by section id,
// tolerating a code fence or text around it, or else Markdown with one
// "## Section" heading per section.
func ParseSections(answer string) ([]Section, error) {
	t := strings.TrimSpace(answer)
	if i, j := strings.Index(t, "{"), strings.LastIndex(t, "}"); i >= 0 && j > i {
		var raw map[string]any
		if err := json.Unmarshal([]byte(t[i:j+1]), &raw); err == nil {
			var out []Section
			for _, id := range SectionIDs {
				if body := flatten(raw[id]); strings.TrimSpace(body) != "" {
					out = append(out, Section{ID: id, Body: strings.TrimSpace(body)})
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		}
	}
	var out []Section
	byTitle := map[string]string{}
	for id, info := range SectionInfo {
		byTitle[strings.ToLower(info.Title)] = id
	}
	locs := sectionRE.FindAllStringSubmatchIndex(t, -1)
	for i, m := range locs {
		id, ok := byTitle[strings.ToLower(strings.TrimSpace(t[m[2]:m[3]]))]
		if !ok {
			continue
		}
		end := len(t)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if body := strings.TrimSpace(t[m[1]:end]); body != "" {
			out = append(out, Section{ID: id, Body: body})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("no sections in the answer")
	}
	return out, nil
}

// flatten turns a JSON value into section text: a list becomes bullets.
func flatten(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var lines []string
		for _, it := range x {
			if s := strings.TrimSpace(flatten(it)); s != "" {
				lines = append(lines, "- "+strings.TrimPrefix(s, "- "))
			}
		}
		return strings.Join(lines, "\n")
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
