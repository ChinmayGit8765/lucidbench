package prompts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/council"
)

// Store keeps the user's templates and snippets in Dir (<DataDir>/prompts):
// templates/<id>.yaml and snippets/<id>.yaml. A user template with a
// built-in's id overrides it in Studio; Work's safety rules never change.
type Store struct {
	Dir string
	Now func() time.Time
}

// Snippet is a reusable piece of text, usually for one section.
type Snippet struct {
	ID      string    `json:"id" yaml:"id"`
	Name    string    `json:"name" yaml:"name"`
	Section string    `json:"section,omitempty" yaml:"section"`
	Body    string    `json:"body" yaml:"body"`
	Updated time.Time `json:"updated" yaml:"updated"`
}

// Size limits for what the user saves.
const (
	MaxSectionChars = 60000
	MaxSnippetChars = 20000
)

// ErrReadOnly refuses a change to a council template.
var ErrReadOnly = errors.New("read-only")

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// CouncilTemplates are the council's system prompts as read-only templates.
func CouncilTemplates() []Template {
	mk := func(id, name, desc, prompt string, order int) Template {
		version, body := splitVersion(prompt)
		return Template{
			ID: id, Name: name, Description: desc, Target: TargetCouncil, Order: order,
			Sections: []Section{{ID: Role, Body: body}}, Source: SourceCouncil, ReadOnly: true,
			Note: "The council's system prompt " + version + ", versioned in internal/council/prompts. Read-only here.",
		}
	}
	return []Template{
		mk("council-propose", "Council · propose", "How the proposer turns a braindump into a brief.", council.ProposePrompt, 100),
		mk("council-critique", "Council · critique", "How each critic answers: a verdict and points by severity, as JSON.", council.CritiquePrompt, 101),
		mk("council-synthesise", "Council · synthesise", "How the proposer revises the brief from the critics' points.", council.SynthesisePrompt, 102),
	}
}

// splitVersion reads "<!-- name vN -->" off the first line.
func splitVersion(p string) (string, string) {
	if strings.HasPrefix(p, "<!--") {
		if i := strings.Index(p, "-->"); i >= 0 {
			head := strings.Fields(strings.TrimSpace(p[4:i]))
			v := "unversioned"
			if len(head) >= 2 {
				v = head[len(head)-1]
			}
			return v, strings.TrimSpace(p[i+3:])
		}
	}
	return "unversioned", strings.TrimSpace(p)
}

func (s *Store) templatesDir() string { return filepath.Join(s.Dir, "templates") }
func (s *Store) snippetsDir() string  { return filepath.Join(s.Dir, "snippets") }

// userTemplates reads the user's own templates; unreadable files are skipped.
func (s *Store) userTemplates() []Template {
	var out []Template
	ents, _ := os.ReadDir(s.templatesDir())
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".yaml")
		if e.IsDir() || !ok || !idRE.MatchString(id) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.templatesDir(), e.Name()))
		if err != nil {
			continue
		}
		t, err := parseTemplate(data)
		if err != nil || t.ID != id {
			continue
		}
		t.Source = SourceUser
		out = append(out, t)
	}
	return out
}

// Templates lists the built-ins (with the user's overrides in their place),
// the user's own templates, then the council's read-only ones.
func (s *Store) Templates() []Template {
	user := map[string]Template{}
	for _, t := range s.userTemplates() {
		user[t.ID] = t
	}
	var out []Template
	for _, b := range Builtins() {
		if u, ok := user[b.ID]; ok {
			orig := b
			u.Overrides, u.Builtin = true, &orig
			out = append(out, u)
			delete(user, b.ID)
			continue
		}
		out = append(out, b)
	}
	var own []Template
	for _, t := range user {
		own = append(own, t)
	}
	sort.Slice(own, func(i, j int) bool { return strings.ToLower(own[i].Name) < strings.ToLower(own[j].Name) })
	out = append(out, own...)
	return append(out, CouncilTemplates()...)
}

// Template returns one template by id, as Templates lists it.
func (s *Store) Template(id string) (Template, error) {
	for _, t := range s.Templates() {
		if t.ID == id {
			return t, nil
		}
	}
	return Template{}, fmt.Errorf("%w: no template %q", ErrNotFound, id)
}

func checkSizes(t Template) error {
	for _, sec := range t.Sections {
		if len(sec.Body) > MaxSectionChars {
			return fmt.Errorf("%w: the %s section is longer than %d characters", ErrInvalid, sec.ID, MaxSectionChars)
		}
	}
	if len(t.Name) > 80 || len(t.Description) > 300 {
		return fmt.Errorf("%w: the name or description is too long", ErrInvalid)
	}
	return nil
}

// SaveTemplate writes a user template (or an override of a built-in).
func (s *Store) SaveTemplate(t Template) (Template, error) {
	t.Name = strings.TrimSpace(t.Name)
	if strings.HasPrefix(t.ID, "council-") {
		return Template{}, fmt.Errorf("%w: the council's prompts are versioned with the code", ErrReadOnly)
	}
	if t.Version == 0 {
		t.Version = 1
	}
	if err := validate(t); err != nil {
		return Template{}, err
	}
	if err := checkSizes(t); err != nil {
		return Template{}, err
	}
	data, err := yaml.Marshal(struct {
		ID          string    `yaml:"id"`
		Name        string    `yaml:"name"`
		Description string    `yaml:"description,omitempty"`
		Target      string    `yaml:"target"`
		Order       int       `yaml:"order"`
		Version     int       `yaml:"version"`
		Sections    []Section `yaml:"sections"`
	}{t.ID, t.Name, t.Description, t.Target, t.Order, t.Version, t.Sections})
	if err != nil {
		return Template{}, err
	}
	if err := writeFile(filepath.Join(s.templatesDir(), t.ID+".yaml"), data); err != nil {
		return Template{}, err
	}
	return s.Template(t.ID)
}

// DeleteTemplate removes a user template; for an override, the built-in
// comes back.
func (s *Store) DeleteTemplate(id string) error {
	if !idRE.MatchString(id) {
		return fmt.Errorf("%w: no template %q", ErrNotFound, id)
	}
	err := os.Remove(filepath.Join(s.templatesDir(), id+".yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		if _, ok := Builtin(id); ok || strings.HasPrefix(id, "council-") {
			return fmt.Errorf("%w: built-in templates cannot be deleted", ErrReadOnly)
		}
		return fmt.Errorf("%w: no template %q", ErrNotFound, id)
	}
	return err
}

// Snippets lists the user's snippets, newest first.
func (s *Store) Snippets() []Snippet {
	out := []Snippet{}
	ents, _ := os.ReadDir(s.snippetsDir())
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".yaml")
		if e.IsDir() || !ok || !idRE.MatchString(id) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.snippetsDir(), e.Name()))
		if err != nil {
			continue
		}
		var sn Snippet
		if yaml.Unmarshal(data, &sn) != nil || sn.ID != id {
			continue
		}
		out = append(out, sn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// SaveSnippet writes a snippet.
func (s *Store) SaveSnippet(sn Snippet) (Snippet, error) {
	sn.Name = strings.TrimSpace(sn.Name)
	switch {
	case !idRE.MatchString(sn.ID):
		return Snippet{}, fmt.Errorf("%w: id must be lowercase letters, digits and dashes", ErrInvalid)
	case sn.Name == "" || len(sn.Name) > 80:
		return Snippet{}, fmt.Errorf("%w: the snippet needs a name of at most 80 characters", ErrInvalid)
	case strings.TrimSpace(sn.Body) == "":
		return Snippet{}, fmt.Errorf("%w: the snippet is empty", ErrInvalid)
	case len(sn.Body) > MaxSnippetChars:
		return Snippet{}, fmt.Errorf("%w: the snippet is longer than %d characters", ErrInvalid, MaxSnippetChars)
	}
	if _, ok := SectionInfo[sn.Section]; sn.Section != "" && !ok {
		return Snippet{}, fmt.Errorf("%w: unknown section %q", ErrInvalid, sn.Section)
	}
	sn.Updated = s.now().UTC()
	data, err := yaml.Marshal(sn)
	if err != nil {
		return Snippet{}, err
	}
	return sn, writeFile(filepath.Join(s.snippetsDir(), sn.ID+".yaml"), data)
}

// DeleteSnippet removes a snippet.
func (s *Store) DeleteSnippet(id string) error {
	if !idRE.MatchString(id) {
		return fmt.Errorf("%w: no snippet %q", ErrNotFound, id)
	}
	err := os.Remove(filepath.Join(s.snippetsDir(), id+".yaml"))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: no snippet %q", ErrNotFound, id)
	}
	return err
}

// writeFile writes atomically: a temporary file, then a rename.
func writeFile(p string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".prompt-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
