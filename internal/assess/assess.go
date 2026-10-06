// Package assess judges a project on a few preliminary questions: is it a
// game, a product, a video system and so on. The answers map to suggestions
// that help manage its development: done criteria for Council briefs, check
// commands for Work sessions, and a risk level with reasons.
//
// The kinds, their questions and their rules are data, one YAML file each in
// kinds/, embedded in the binary. This package is a leaf: it imports nothing
// from the rest of Lucidbench, so projects, council and work can all use it.
package assess

import (
	"embed"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

//go:embed kinds/*.yaml
var kindFS embed.FS

// Question types.
const (
	TypeChoice = "choice"
	TypeText   = "text"
	TypeBool   = "bool"
)

// Levels are the risk levels, lowest first.
var Levels = []string{"low", "medium", "high"}

// MaxText is the longest accepted text answer.
const MaxText = 500

// Question is one step of the flow. A bool answer is "true" or "false".
type Question struct {
	ID      string   `json:"id" yaml:"id"`
	Text    string   `json:"text" yaml:"text"`
	Type    string   `json:"type" yaml:"type"`
	Options []string `json:"options,omitempty" yaml:"options"`
}

// StringList reads either one string or a list of strings from YAML.
type StringList []string

// UnmarshalYAML accepts a scalar as a list of one.
func (l *StringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = StringList{n.Value}
		return nil
	}
	var s []string
	if err := n.Decode(&s); err != nil {
		return err
	}
	*l = s
	return nil
}

// RiskNote raises the risk level and says why.
type RiskNote struct {
	Level  string `json:"level" yaml:"level"`
	Reason string `json:"reason" yaml:"reason"`
}

// Rule maps answers to suggestions. It applies when every key in When has an
// answer equal to one of its values; the value "*" matches any non-empty
// answer. An empty When always applies.
type Rule struct {
	When     map[string]StringList `json:"when,omitempty" yaml:"when"`
	Criteria []string              `json:"criteria,omitempty" yaml:"criteria"`
	Commands []string              `json:"commands,omitempty" yaml:"commands"`
	Risk     *RiskNote             `json:"risk,omitempty" yaml:"risk"`
}

// Kind is one kind of project.
type Kind struct {
	ID        string     `json:"id" yaml:"id"`
	Name      string     `json:"name" yaml:"name"`
	Blurb     string     `json:"blurb" yaml:"blurb"`
	Order     int        `json:"-" yaml:"order"`
	Types     []string   `json:"types,omitempty" yaml:"types"` // projects.yaml `type` values this kind is the default for
	Questions []Question `json:"questions" yaml:"questions"`
	Rules     []Rule     `json:"-" yaml:"rules"`
}

// Assessment is a project's confirmed answers.
type Assessment struct {
	Kind       string            `json:"kind" yaml:"kind"`
	Answers    map[string]string `json:"answers" yaml:"answers"`
	AssessedAt time.Time         `json:"assessed_at" yaml:"assessed_at"`
}

// Risk is a level and the reasons for it.
type Risk struct {
	Level   string   `json:"level"`
	Reasons []string `json:"reasons"`
}

// Suggestion is what an assessment computes.
type Suggestion struct {
	DoneCriteria  []string `json:"done_criteria"`
	CheckCommands []string `json:"check_commands"`
	Risk          Risk     `json:"risk"`
}

var (
	once    sync.Once
	kinds   []Kind
	loadErr error
)

// Kinds returns every kind in display order. It panics if an embedded kind
// is invalid; a test keeps that from shipping.
func Kinds() []Kind {
	once.Do(func() { kinds, loadErr = load() })
	if loadErr != nil {
		panic("assess: " + loadErr.Error())
	}
	return slices.Clone(kinds)
}

// Get returns the kind with the given id.
func Get(id string) (Kind, bool) {
	for _, k := range Kinds() {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// KindForType returns the kind to preselect for a projects.yaml type, or "".
func KindForType(t string) string {
	for _, k := range Kinds() {
		if slices.Contains(k.Types, t) {
			return k.ID
		}
	}
	return ""
}

func load() ([]Kind, error) {
	entries, err := kindFS.ReadDir("kinds")
	if err != nil {
		return nil, err
	}
	var out []Kind
	for _, e := range entries {
		data, err := kindFS.ReadFile("kinds/" + e.Name())
		if err != nil {
			return nil, err
		}
		k, err := parseKind(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if want := strings.TrimSuffix(e.Name(), ".yaml"); k.ID != want {
			return nil, fmt.Errorf("%s: id %q must match the file name", e.Name(), k.ID)
		}
		out = append(out, k)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

func parseKind(data []byte) (Kind, error) {
	var k Kind
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&k); err != nil {
		return k, err
	}
	return k, validateKind(k)
}

func validateKind(k Kind) error {
	if k.ID == "" || k.Name == "" || k.Blurb == "" {
		return fmt.Errorf("id, name and blurb are required")
	}
	if n := len(k.Questions); n < 5 || n > 7 {
		return fmt.Errorf("%s: want 5 to 7 questions, got %d", k.ID, n)
	}
	qs := map[string]Question{}
	for _, q := range k.Questions {
		if q.ID == "" || q.Text == "" {
			return fmt.Errorf("%s: a question needs an id and text", k.ID)
		}
		if _, dup := qs[q.ID]; dup {
			return fmt.Errorf("%s: duplicate question %q", k.ID, q.ID)
		}
		qs[q.ID] = q
		switch q.Type {
		case TypeChoice:
			if len(q.Options) < 2 {
				return fmt.Errorf("%s.%s: a choice needs at least two options", k.ID, q.ID)
			}
		case TypeText, TypeBool:
			if len(q.Options) > 0 {
				return fmt.Errorf("%s.%s: only a choice has options", k.ID, q.ID)
			}
		default:
			return fmt.Errorf("%s.%s: unknown type %q", k.ID, q.ID, q.Type)
		}
	}
	for i, r := range k.Rules {
		for qid, vals := range r.When {
			q, ok := qs[qid]
			if !ok {
				return fmt.Errorf("%s rule %d: unknown question %q", k.ID, i+1, qid)
			}
			for _, v := range vals {
				if v == "*" {
					continue
				}
				switch q.Type {
				case TypeChoice:
					if !slices.Contains(q.Options, v) {
						return fmt.Errorf("%s rule %d: %q is not an option of %s", k.ID, i+1, v, qid)
					}
				case TypeBool:
					if v != "true" && v != "false" {
						return fmt.Errorf("%s rule %d: %s is a bool: use \"true\" or \"false\"", k.ID, i+1, qid)
					}
				}
			}
		}
		if r.Risk != nil && !slices.Contains(Levels, r.Risk.Level) {
			return fmt.Errorf("%s rule %d: unknown risk level %q", k.ID, i+1, r.Risk.Level)
		}
		if r.Risk != nil && r.Risk.Reason == "" {
			return fmt.Errorf("%s rule %d: a risk needs a reason", k.ID, i+1)
		}
	}
	return nil
}

// Validate checks an assessment against its kind. Every choice and bool
// question must be answered; text answers are optional.
func Validate(a Assessment) error {
	k, ok := Get(a.Kind)
	if !ok {
		ids := []string{}
		for _, k := range Kinds() {
			ids = append(ids, k.ID)
		}
		return fmt.Errorf("unknown kind %q (want one of %s)", a.Kind, strings.Join(ids, ", "))
	}
	known := map[string]bool{}
	for _, q := range k.Questions {
		known[q.ID] = true
		v := a.Answers[q.ID]
		switch q.Type {
		case TypeChoice:
			if v == "" {
				return fmt.Errorf("answers.%s: required", q.ID)
			}
			if !slices.Contains(q.Options, v) {
				return fmt.Errorf("answers.%s: unknown value %q (want one of %s)", q.ID, v, strings.Join(q.Options, ", "))
			}
		case TypeBool:
			if v != "true" && v != "false" {
				return fmt.Errorf("answers.%s: want true or false", q.ID)
			}
		case TypeText:
			if len([]rune(v)) > MaxText {
				return fmt.Errorf("answers.%s: longer than %d characters", q.ID, MaxText)
			}
		}
	}
	for id := range a.Answers {
		if !known[id] {
			return fmt.Errorf("answers.%s: not a question of kind %s", id, a.Kind)
		}
	}
	return nil
}

func (r Rule) applies(answers map[string]string) bool {
	for qid, vals := range r.When {
		v := answers[qid]
		if !(slices.Contains(vals, v) || (v != "" && slices.Contains(vals, "*"))) {
			return false
		}
	}
	return true
}

// Suggest computes the suggestions for an assessment. An unknown kind gives
// an empty suggestion with a low risk.
func Suggest(a Assessment) Suggestion {
	s := Suggestion{DoneCriteria: []string{}, CheckCommands: []string{}, Risk: Risk{Level: "low", Reasons: []string{}}}
	k, ok := Get(a.Kind)
	if !ok {
		return s
	}
	for _, r := range k.Rules {
		if !r.applies(a.Answers) {
			continue
		}
		for _, c := range r.Criteria {
			if !slices.Contains(s.DoneCriteria, c) {
				s.DoneCriteria = append(s.DoneCriteria, c)
			}
		}
		for _, c := range r.Commands {
			if !slices.Contains(s.CheckCommands, c) {
				s.CheckCommands = append(s.CheckCommands, c)
			}
		}
		if r.Risk != nil {
			if slices.Index(Levels, r.Risk.Level) > slices.Index(Levels, s.Risk.Level) {
				s.Risk.Level = r.Risk.Level
			}
			s.Risk.Reasons = append(s.Risk.Reasons, r.Risk.Reason)
		}
	}
	return s
}
