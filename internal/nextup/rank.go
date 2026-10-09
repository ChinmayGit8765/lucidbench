package nextup

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

// SystemPrompt is what the ranking agent is told, versioned with the code.
//
//go:embed prompts/rank.md
var SystemPrompt string

// PromptVersion reads "<!-- rank vN -->" from the prompt's first line.
func PromptVersion() string {
	if m := regexp.MustCompile(`^<!--\s*rank\s+(v\d+)\s*-->`).FindStringSubmatch(SystemPrompt); m != nil {
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

// Limits on one ranking.
const (
	MaxRankItems     = 30
	MaxReason        = 300
	MaxActionName    = 32
	MaxPrompt        = 2000
	MaxContext       = 160
	RankTimeout      = 3 * time.Minute
	privateRankNotes = "Private item, ranked by its score."
)

// ErrBadOutput is an answer that holds no usable ranking.
var ErrBadOutput = errors.New("the model did not return a usable ranking")

// RankRequest is the body of POST /api/nextup/rank.
type RankRequest struct {
	Provider string `json:"provider,omitempty"`
	Profile  string `json:"profile,omitempty"`
	Model    string `json:"model,omitempty"`
}

// RunRecord is one ranking's cost, for the Usage page.
type RunRecord struct {
	Kind    string          `json:"kind"` // "rank"
	Created time.Time       `json:"created"`
	Error   string          `json:"error,omitempty"`
	Usage   agentexec.Usage `json:"usage"`
}

// Ranker asks the user's CLI to order the candidates, with no tools.
type Ranker struct {
	Runner *agentexec.Runner
	// RunsDir keeps one record per call; "" records nothing.
	RunsDir string
	// Scout is the default team's scout role: its provider, model and
	// profile, "" when it has none.
	Scout func() (provider, model, profile string)
	Now   func() time.Time
}

func (g *Ranker) runner() *agentexec.Runner {
	if g.Runner != nil {
		return g.Runner
	}
	return &agentexec.Runner{}
}

func (g *Ranker) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

// pick chooses the provider, model and profile: the request's, then the
// saved settings', then the team scout's, then the first installed CLI;
// the model defaults to the provider's cheap one.
func (g *Ranker) pick(req RankRequest, set Settings) (string, string, string, error) {
	provider, model, profile := req.Provider, req.Model, req.Profile
	if provider == "" && set.Provider != "" {
		provider = set.Provider
		if model == "" {
			model = set.Model
		}
	}
	if provider == "" && g.Scout != nil {
		if p, m, pr := g.Scout(); p != "" {
			provider = p
			if model == "" {
				model = m
			}
			if profile == "" {
				profile = pr
			}
		}
	}
	if provider == "" {
		look := g.runner().LookPath
		if look == nil {
			look = exec.LookPath
		}
		for _, p := range Providers {
			if _, err := look(p); err == nil {
				provider = p
				break
			}
		}
		if provider == "" {
			return "", "", "", fmt.Errorf("%w: no provider CLI is installed (claude, codex or grok)", agentexec.ErrCLIMissing)
		}
	}
	if _, ok := agentexec.Logins[provider]; !ok {
		return "", "", "", fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
	}
	if model == "" {
		model = prompts.CheapModels[provider]
	}
	return provider, model, profile, nil
}

func (g *Ranker) record(u agentexec.Usage, runErr error) {
	if g.RunsDir == "" || (u.DurationMS == 0 && u.InputTokens == 0 && u.OutputTokens == 0 && u.CostUSD == 0) {
		return
	}
	rec := RunRecord{Kind: "rank", Created: g.now().UTC(), Usage: u}
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

// inputItem is one candidate as the agent sees it. A confidential one has
// only ID and Score: every other field is omitted, not blanked.
type inputItem struct {
	ID      string   `json:"id"`
	Score   int      `json:"score"`
	Kind    string   `json:"kind,omitempty"`
	Title   string   `json:"title,omitempty"`
	Project string   `json:"project,omitempty"`
	Due     string   `json:"due,omitempty"`
	Context string   `json:"context,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	Why     []string `json:"why,omitempty"`
	Action  string   `json:"action,omitempty"`
}

// rankInput is the whole input.
type rankInput struct {
	Today      string      `json:"today"`
	Candidates []inputItem `json:"candidates"`
}

// BuildInput turns candidates into the agent's input and the alias map
// (c1, c2, … to the real ids). Real ids never reach the agent: a card id or
// a project in an id could name something confidential.
func BuildInput(cs []Candidate, now time.Time) (string, map[string]int, error) {
	if len(cs) > MaxRankItems {
		cs = cs[:MaxRankItems]
	}
	in := rankInput{Today: now.Format("2006-01-02"), Candidates: make([]inputItem, 0, len(cs))}
	alias := map[string]int{}
	for i, c := range cs {
		id := fmt.Sprintf("c%d", i+1)
		alias[id] = i
		it := inputItem{ID: id, Score: c.Score}
		if !c.Confidential {
			it.Kind, it.Title, it.Project, it.Due, it.Labels = c.Kind, excerpt(c.Title, 160), c.ProjectName, c.Due, c.Labels
			it.Context = excerpt(c.Context, MaxContext)
			it.Action = c.Action.Kind
			for _, p := range c.Parts {
				why := p.Why
				// Unblocking names other projects, which may be confidential.
				if p.Factor == "unblocking" {
					why = "builds into other projects"
					if strings.HasPrefix(p.Why, "unblocks") {
						why = "unblocks other projects"
					}
				}
				it.Why = append(it.Why, fmt.Sprintf("%s %+d: %s", p.Factor, p.Points, why))
			}
		}
		in.Candidates = append(in.Candidates, it)
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", nil, err
	}
	return "Rank these candidates.\n\n" + string(b), alias, nil
}

// answerItem is one entry of the agent's answer, before validation.
type answerItem struct {
	ID              string `json:"id"`
	Reason          string `json:"reason"`
	SuggestedAction string `json:"suggested_action"`
	SuggestedPrompt string `json:"suggested_prompt"`
}

// ParseRanking finds the ranking in a model answer and validates it against
// the candidates it was given: unknown and repeated ids are dropped, text is
// cut to its limits, and an unknown action name is left out. Confidential
// candidates keep no suggested prompt.
func ParseRanking(text string, cs []Candidate, alias map[string]int) ([]RankedItem, error) {
	var raw []answerItem
	start := strings.IndexAny(text, "[{")
	if start < 0 {
		return nil, fmt.Errorf("%w: no JSON in the answer (it began %q)", ErrBadOutput, agentexec.Excerpt(text))
	}
	body := text[start:]
	if body[0] == '{' {
		end := strings.LastIndex(body, "}")
		if end < 0 {
			return nil, fmt.Errorf("%w: the JSON object is not closed", ErrBadOutput)
		}
		var obj struct {
			Ranking []answerItem `json:"ranking"`
		}
		if err := json.Unmarshal([]byte(body[:end+1]), &obj); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
		}
		raw = obj.Ranking
	} else {
		end := strings.LastIndex(body, "]")
		if end < 0 {
			return nil, fmt.Errorf("%w: the JSON list is not closed", ErrBadOutput)
		}
		if err := json.Unmarshal([]byte(body[:end+1]), &raw); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
		}
	}
	out := []RankedItem{}
	seen := map[int]bool{}
	for _, r := range raw {
		i, ok := alias[strings.TrimSpace(r.ID)]
		if !ok || seen[i] || i >= len(cs) {
			continue
		}
		seen[i] = true
		c := cs[i]
		it := RankedItem{ID: c.ID, Reason: cut(unalias(r.Reason, cs, alias), MaxReason)}
		if a := strings.TrimSpace(r.SuggestedAction); len(a) <= MaxActionName && contains(ActionKinds, a) {
			it.SuggestedAction = a
		}
		if c.Confidential {
			it.Reason = privateRankNotes
		} else if p := strings.TrimSpace(r.SuggestedPrompt); p != "" && c.Action.Work != nil && len(prompts.FindSecrets(p)) == 0 {
			it.SuggestedPrompt = cutRunes(p, MaxPrompt)
		}
		out = append(out, it)
		if len(out) == len(cs) {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: it named none of the candidates", ErrBadOutput)
	}
	return out, nil
}

var aliasRE = regexp.MustCompile(`\bc\d{1,3}\b`)

// unalias replaces the aliases a model wrote in a reason ("unblocks c3")
// with what the user knows them by: a short title, or "a private item".
func unalias(text string, cs []Candidate, alias map[string]int) string {
	return aliasRE.ReplaceAllStringFunc(text, func(a string) string {
		i, ok := alias[a]
		if !ok || i >= len(cs) {
			return a
		}
		if cs[i].Confidential {
			return "a private item"
		}
		return "“" + excerpt(cs[i].Title, 48) + "”"
	})
}

// cut is one line of at most n runes.
func cut(s string, n int) string { return cutRunes(oneLine(s), n) }

func cutRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// Rank asks the agent to order cs (best first, at most MaxRankItems) and
// returns the validated ranking. The call's usage is recorded either way.
func (g *Ranker) Rank(ctx context.Context, cs []Candidate, req RankRequest, set Settings) (*Ranking, error) {
	if len(cs) == 0 {
		return nil, fmt.Errorf("%w: there is nothing to rank", ErrBadRequest)
	}
	if len(cs) > MaxRankItems {
		cs = cs[:MaxRankItems]
	}
	provider, model, profile, err := g.pick(req, set)
	if err != nil {
		return nil, err
	}
	now := g.now()
	prompt, alias, err := BuildInput(cs, now)
	if err != nil {
		return nil, err
	}
	res, err := g.runner().Run(ctx, agentexec.Request{
		Provider: provider, Profile: profile, Model: model, SystemPrompt: systemPrompt(), Prompt: prompt,
		Tools: agentexec.ToolsNone, Timeout: RankTimeout,
	})
	if res != nil {
		g.record(res.Usage, err)
	}
	if err != nil {
		return nil, err
	}
	items, err := ParseRanking(res.Text, cs, alias)
	if err != nil {
		return nil, err
	}
	return &Ranking{At: now.UTC(), Provider: provider, Model: model, PromptVersion: PromptVersion(), Usage: res.Usage, Items: items}, nil
}

// Rank ranks the visible candidates with the agent and keeps the answer
// until the next ranking.
func (s *Service) Rank(ctx context.Context, req RankRequest) (View, *Ranking, error) {
	if s.Ranker == nil {
		return View{}, nil, errors.New("no ranker")
	}
	st := s.Store.Load()
	cs, errs, _ := s.candidates(ctx, st)
	v := s.view(cs, errs, State{Snoozed: st.Snoozed, Dismissed: st.Dismissed, Settings: st.Settings})
	r, err := s.Ranker.Rank(ctx, v.Items, req, st.Settings)
	if err != nil {
		return View{}, nil, err
	}
	st, err = s.Store.Update(func(x *State) error {
		x.LastRank = r
		return nil
	})
	if err != nil {
		return View{}, nil, err
	}
	return s.view(cs, errs, st), r, nil
}
