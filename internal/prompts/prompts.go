// Package prompts is Prompt Studio's engine: sectioned prompt templates,
// the context a prompt can be given (a project, a Memory page, the project's
// rules, a repo map, a card, council decisions), rendering with variables,
// and lint that refuses to send a confidential source to a provider.
//
// The built-in templates are versioned with the code in templates/*.yaml.
// The user's own templates, overrides of the built-ins and snippets are kept
// in <DataDir>/prompts/. Work builds its agent prompt from the builder
// template; the council's three system prompts are listed read-only, so the
// prompts every agent gets can be read in one place.
package prompts

import (
	"embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

//go:embed templates/*.yaml
var templateFS embed.FS

// Section ids, in their usual order.
const (
	Role        = "role"
	Context     = "context"
	Contract    = "contract"
	Task        = "task"
	Constraints = "constraints"
	Verify      = "verify"
	Report      = "report"
)

// SectionIDs lists every section in its usual order.
var SectionIDs = []string{Role, Context, Contract, Task, Constraints, Verify, Report}

// SectionInfo is each section's heading and the hint the editor shows.
var SectionInfo = map[string]struct{ Title, Hint string }{
	Role:        {"Role", "Who the agent is and what kind of job this is, in one or two sentences."},
	Context:     {"Context", "What the agent needs to know: the project, pages, a repo map. Add sources on the right."},
	Contract:    {"Contract", "Interfaces, routes or formats the work must keep or build against."},
	Task:        {"Task", "What to do, scoped to one slice, with done criteria the agent can check."},
	Constraints: {"Constraints", "What the agent must not do: files, commands, scope."},
	Verify:      {"Verify", "How the agent proves it worked: the commands to run and what they should say."},
	Report:      {"Report", "What the answer must contain and in what shape."},
}

// Targets a prompt is sent to. Every provider is outside this machine; only
// copy keeps the text local.
const (
	TargetWork    = "work"
	TargetCouncil = "council"
	TargetCopy    = "copy"
)

// Template sources.
const (
	SourceBuiltin = "builtin"
	SourceCouncil = "council"
	SourceUser    = "user"
)

// Section is one part of a prompt.
type Section struct {
	ID   string `json:"id" yaml:"id"`
	Body string `json:"body" yaml:"body"`
}

// Template is a named set of sections.
type Template struct {
	ID          string    `json:"id" yaml:"id"`
	Name        string    `json:"name" yaml:"name"`
	Description string    `json:"description,omitempty" yaml:"description"`
	Target      string    `json:"target" yaml:"target"` // where it is usually sent
	Order       int       `json:"order" yaml:"order"`
	Version     int       `json:"version" yaml:"version"`
	Sections    []Section `json:"sections" yaml:"sections"`

	Source   string `json:"source" yaml:"-"`
	ReadOnly bool   `json:"read_only" yaml:"-"`
	// Overrides is set on a user template that replaces a built-in one of
	// the same id; Builtin then holds the original.
	Overrides bool      `json:"overrides,omitempty" yaml:"-"`
	Builtin   *Template `json:"builtin,omitempty" yaml:"-"`
	// Note says where a read-only template comes from.
	Note string `json:"note,omitempty" yaml:"-"`
}

// Body returns the body of section id, or "".
func (t Template) Body(id string) string {
	for _, s := range t.Sections {
		if s.ID == id {
			return s.Body
		}
	}
	return ""
}

var (
	builtinOnce sync.Once
	builtins    []Template
	builtinErr  error
)

// Builtins returns the templates versioned with the code, in display order.
// It panics if an embedded template is invalid; a test keeps that from
// shipping.
func Builtins() []Template {
	builtinOnce.Do(func() { builtins, builtinErr = loadBuiltins() })
	if builtinErr != nil {
		panic("prompts: " + builtinErr.Error())
	}
	out := make([]Template, len(builtins))
	for i, t := range builtins {
		out[i] = t
		out[i].Sections = slices.Clone(t.Sections)
	}
	return out
}

// Builtin returns the built-in template with the given id.
func Builtin(id string) (Template, bool) {
	for _, t := range Builtins() {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

func loadBuiltins() ([]Template, error) {
	entries, err := templateFS.ReadDir("templates")
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		data, err := templateFS.ReadFile("templates/" + e.Name())
		if err != nil {
			return nil, err
		}
		t, err := parseTemplate(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if want := strings.TrimSuffix(e.Name(), ".yaml"); t.ID != want {
			return nil, fmt.Errorf("%s: id %q must match the file name", e.Name(), t.ID)
		}
		t.Source = SourceBuiltin
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

func parseTemplate(data []byte) (Template, error) {
	var t Template
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&t); err != nil {
		return t, err
	}
	return t, validate(t)
}

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ErrInvalid is a template or snippet that does not follow the format.
var ErrInvalid = errors.New("invalid")

func validate(t Template) error {
	if !idRE.MatchString(t.ID) {
		return fmt.Errorf("%w: id must be lowercase letters, digits and dashes", ErrInvalid)
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("%w: the template needs a name", ErrInvalid)
	}
	switch t.Target {
	case TargetWork, TargetCouncil, TargetCopy:
	default:
		return fmt.Errorf("%w: target must be work, council or copy", ErrInvalid)
	}
	if len(t.Sections) == 0 {
		return fmt.Errorf("%w: the template has no sections", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, s := range t.Sections {
		if _, ok := SectionInfo[s.ID]; !ok {
			return fmt.Errorf("%w: unknown section %q (use %s)", ErrInvalid, s.ID, strings.Join(SectionIDs, ", "))
		}
		if seen[s.ID] {
			return fmt.Errorf("%w: section %q appears twice", ErrInvalid, s.ID)
		}
		seen[s.ID] = true
	}
	return nil
}
