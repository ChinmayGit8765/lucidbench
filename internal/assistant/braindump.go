package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/prompts"
)

// Item types and next steps a braindump item may have.
var (
	ItemTypes = []string{"idea", "feature", "bug", "chore", "question", "process"}
	NextSteps = []string{"council", "card", "idea", "park"}
)

// Limits on a braindump parse.
const (
	MaxBraindump = 20000
	MaxItems     = 30
	// Similar is the word overlap from which an item looks like an
	// existing card or idea.
	Similar = 0.6
)

// Existing is a card or idea an item is compared with.
type Existing struct {
	Kind  string `json:"kind"` // card | idea
	Title string `json:"title"`
	// Ref is the card id ("<board>/<id>") or the idea id.
	Ref string `json:"ref"`
}

// Item is one atomic thought from a braindump.
type Item struct {
	// Quote is the user's own words; QuoteFound says they were found in the
	// braindump as written.
	Quote       string `json:"quote"`
	QuoteFound  bool   `json:"quote_found"`
	Restatement string `json:"restatement"`
	Type        string `json:"type"`
	// Project is a known project id, or "" (none). NewProject is set when
	// the model thought it belongs to a project that is not listed.
	Project    string `json:"project"`
	NewProject bool   `json:"new_project,omitempty"`
	Next       string `json:"next"`
	// Similar is an existing card or idea with a close title.
	Similar *Existing `json:"similar,omitempty"`
}

// BraindumpRequest is the body of POST /api/assistant/braindump.
type BraindumpRequest struct {
	Text     string `json:"text"`
	Provider string `json:"provider,omitempty"`
	Profile  string `json:"profile,omitempty"`
	Model    string `json:"model,omitempty"`
}

// BraindumpResult is the parse, a preview: nothing is applied.
type BraindumpResult struct {
	Items    []Item          `json:"items"`
	Provider string          `json:"provider"`
	Model    string          `json:"model,omitempty"`
	Prompt   string          `json:"prompt_version"`
	Usage    agentexec.Usage `json:"usage"`
	Notes    []string        `json:"notes"`
}

// Braindump splits a dump into items with the user's CLI, with no tools.
func (s *Service) Braindump(ctx context.Context, req BraindumpRequest) (*BraindumpResult, error) {
	text := strings.TrimSpace(req.Text)
	if len(text) < 3 || len(text) > MaxBraindump {
		return nil, fmt.Errorf("%w: paste a braindump of 3 to %d characters", ErrBadRequest, MaxBraindump)
	}
	if hits := prompts.FindSecrets(text); len(hits) > 0 {
		return nil, fmt.Errorf("%w: the braindump has what looks like a secret (%s); remove it first", ErrBadRequest, hits[0].Kind)
	}
	ps, err := s.projects()
	if err != nil {
		return nil, err
	}
	if err := s.checkConfidential(ps, "", text); err != nil {
		return nil, err
	}
	provider, err := s.pickProvider(req.Provider)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" {
		model = prompts.CheapModels[provider]
	}
	prompt := projectContext(ps) + "\n## The braindump\n\n" + text + "\n"
	res, runErr := s.runner().Run(ctx, agentexec.Request{
		Provider: provider, Profile: req.Profile, Model: model, SystemPrompt: stripVersion(BraindumpPrompt), Prompt: prompt,
		Tools: agentexec.ToolsNone, Timeout: TurnTimeout,
	})
	if res != nil {
		s.record("braindump", "", res.Usage, runErr)
	}
	if runErr != nil {
		return nil, runErr
	}
	items, notes, err := parseItems(res.Text, text, ps)
	if err != nil {
		return nil, err
	}
	var existing []Existing
	if s.Existing != nil {
		existing = s.Existing()
	}
	for i := range items {
		items[i].Similar = closest(items[i], existing)
	}
	return &BraindumpResult{Items: items, Provider: provider, Model: model, Prompt: PromptVersion(BraindumpPrompt), Usage: res.Usage, Notes: notes}, nil
}

// parseItems reads the model's items and checks each one. Items with no
// restatement are dropped; unknown types and steps fall back to idea/park.
func parseItems(answer, dump string, ps []projects.Project) ([]Item, []string, error) {
	i, j := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if i < 0 || j <= i {
		return nil, nil, fmt.Errorf("%w: no JSON object in the answer (it began %q)", ErrBadOutput, agentexec.Excerpt(answer))
	}
	var doc struct {
		Items []struct {
			Quote       string `json:"quote"`
			Restatement string `json:"restatement"`
			Type        string `json:"type"`
			Project     string `json:"project"`
			Next        string `json:"next"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(answer[i : j+1])))
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrBadOutput, err)
	}
	known := map[string]bool{}
	for _, p := range ps {
		known[p.ID] = true
	}
	raw := doc.Items
	var items []Item
	notes := []string{}
	if len(raw) > MaxItems {
		notes = append(notes, fmt.Sprintf("the answer had %d items; the first %d are shown", len(raw), MaxItems))
		raw = raw[:MaxItems]
	}
	flat := normalSpace(dump)
	for _, r := range raw {
		it := Item{Quote: strings.TrimSpace(r.Quote), Restatement: oneLine(r.Restatement), Type: strings.ToLower(strings.TrimSpace(r.Type)), Project: strings.TrimSpace(r.Project), Next: strings.ToLower(strings.TrimSpace(r.Next))}
		if it.Restatement == "" {
			continue
		}
		if len(it.Restatement) > MaxTitle {
			it.Restatement = it.Restatement[:MaxTitle]
		}
		if len(it.Quote) > 2000 {
			it.Quote = it.Quote[:2000]
		}
		it.QuoteFound = it.Quote != "" && strings.Contains(flat, normalSpace(it.Quote))
		if !slices.Contains(ItemTypes, it.Type) {
			it.Type = "idea"
		}
		if !slices.Contains(NextSteps, it.Next) {
			it.Next = "park"
		}
		switch {
		case it.Project == "new":
			it.Project, it.NewProject = "", true
		case it.Project != "" && !known[it.Project]:
			it.Project, it.NewProject = "", true
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return nil, notes, fmt.Errorf("%w: the answer had no items", ErrBadOutput)
	}
	return items, notes, nil
}

func normalSpace(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// stop words are left out when titles are compared.
var stop = map[string]bool{"a": true, "an": true, "the": true, "to": true, "of": true, "for": true, "and": true, "or": true, "in": true, "on": true, "with": true, "is": true, "it": true, "my": true, "this": true, "that": true, "be": true, "at": true, "by": true}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !stop[w] {
			out[strings.TrimSuffix(w, "s")] = true
		}
	}
	return out
}

// Similarity is the word overlap of two titles: shared words over the
// smaller set, so "Write the README" matches "Write README for demo".
func Similarity(a, b string) float64 {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	small := min(len(wa), len(wb))
	if small < 2 && shared < 2 && len(wa) != len(wb) {
		// One word in common between a one-word title and a long one is noise.
		return float64(shared) / float64(max(len(wa), len(wb)))
	}
	return float64(shared) / float64(small)
}

// closest returns the existing card or idea most like the item, when it is
// at least Similar.
func closest(it Item, existing []Existing) *Existing {
	var best *Existing
	score := 0.0
	for i := range existing {
		if sc := Similarity(it.Restatement, existing[i].Title); sc >= Similar && sc > score {
			best, score = &existing[i], sc
		}
	}
	if best == nil {
		return nil
	}
	e := *best
	return &e
}
