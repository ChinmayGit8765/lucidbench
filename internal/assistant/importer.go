package assistant

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
)

// Import reads the agents the user already made in their CLIs, read-only,
// so they can be saved as bots. Only these folders are read, and only the
// files' own agent fields: a name, a description, a model and, when the user
// imports one with its instructions, the body. Credentials, config.toml and
// session files are never opened, and no CLI is run.
//
//	claude  <config dir>/agents/*.md     Markdown with front matter (subagents)
//	grok    ~/.grok/agents/*.md          Markdown with front matter (agents)
//	grok    ~/.grok/personas/*.toml      instructions, description, model (personas)
//	codex   <CODEX_HOME>/agents/*.toml   name, description, model, developer_instructions

// Candidate is one agent found in a CLI's folders.
type Candidate struct {
	Key         string `json:"key"`
	Source      string `json:"source"`
	Provider    string `json:"provider"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"`
	// HasBody says the agent has instructions that can be imported as the
	// bot's persona.
	HasBody bool `json:"has_body"`
	// File is the file's name, without its folder.
	File string `json:"file"`
}

// SourceNote says what was found for one provider.
type SourceNote struct {
	Provider string `json:"provider"`
	Found    int    `json:"found"`
	Note     string `json:"note"`
}

// ImportList is the body of GET /api/assistant/bots/import.
type ImportList struct {
	Candidates []Candidate  `json:"candidates"`
	Notes      []SourceNote `json:"notes"`
}

// Limits on what import reads.
const (
	maxImportFile  = 256 << 10
	maxImportFiles = 200
)

// agentDir is one folder to read.
type agentDir struct {
	source, provider, dir, ext string
}

func (s *Service) roots() accounts.Roots {
	if s.Roots != nil {
		return s.Roots()
	}
	return accounts.FromEnv()
}

func (s *Service) agentDirs() []agentDir {
	r := s.roots()
	var out []agentDir
	add := func(source, provider, dir, ext string) {
		if dir == "" {
			return
		}
		for _, d := range out {
			if strings.EqualFold(filepath.Clean(d.dir), filepath.Clean(dir)) && d.ext == ext {
				return
			}
		}
		out = append(out, agentDir{source, provider, dir, ext})
	}
	if r.On("claude") {
		if r.Home != "" {
			add("claude agents", "claude", filepath.Join(r.Home, ".claude", "agents"), ".md")
		}
		for _, d := range r.ExtraClaudeDirs {
			add("claude agents", "claude", filepath.Join(d, "agents"), ".md")
		}
	}
	if r.On("grok") {
		dirs := r.ExtraDirs["grok"]
		if r.Home != "" {
			dirs = append([]string{filepath.Join(r.Home, ".grok")}, dirs...)
		}
		for _, d := range dirs {
			add("grok agents", "grok", filepath.Join(d, "agents"), ".md")
			add("grok personas", "grok", filepath.Join(d, "personas"), ".toml")
		}
	}
	if r.On("codex") {
		home := r.CodexHome
		if home == "" && r.Home != "" {
			home = filepath.Join(r.Home, ".codex")
		}
		dirs := append([]string{home}, r.ExtraDirs["codex"]...)
		for _, d := range dirs {
			if d != "" {
				add("codex agents", "codex", filepath.Join(d, "agents"), ".toml")
			}
		}
	}
	return out
}

// parsed is what one agent file holds.
type parsed struct {
	name, description, model, body string
}

// readAgent reads one agent file. Symlinks, folders and files over the
// size cap are skipped.
func readAgent(path, ext string) (*parsed, bool) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxImportFile {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var p parsed
	switch ext {
	case ".md":
		front, body, ok := splitFrontMatter(data)
		if !ok {
			return nil, false
		}
		var f struct {
			Name        any `yaml:"name"`
			Description any `yaml:"description"`
			Model       any `yaml:"model"`
		}
		if yaml.Unmarshal(front, &f) != nil {
			return nil, false
		}
		p = parsed{name: str(f.Name), description: str(f.Description), model: str(f.Model), body: strings.TrimSpace(body)}
	case ".toml":
		var f struct {
			Name                  string `toml:"name"`
			Description           string `toml:"description"`
			Model                 string `toml:"model"`
			DeveloperInstructions string `toml:"developer_instructions"`
			Instructions          string `toml:"instructions"`
		}
		if _, err := toml.Decode(string(data), &f); err != nil {
			return nil, false
		}
		body := f.DeveloperInstructions
		if body == "" {
			body = f.Instructions
		}
		p = parsed{name: f.Name, description: f.Description, model: f.Model, body: strings.TrimSpace(body)}
	}
	if p.name == "" {
		p.name = stem
	}
	p.name = oneLine(p.name)
	p.description = oneLine(p.description)
	if !modelRE.MatchString(p.model) || strings.EqualFold(p.model, "inherit") {
		p.model = ""
	}
	return &p, true
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// splitFrontMatter splits "---\n<yaml>\n---\n<body>".
func splitFrontMatter(data []byte) ([]byte, string, bool) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, "", false
	}
	rest := text[4:]
	i := strings.Index(rest, "\n---")
	if i < 0 {
		return nil, "", false
	}
	body := rest[i+4:]
	if j := strings.IndexByte(body, '\n'); j >= 0 {
		body = body[j+1:]
	} else {
		body = ""
	}
	return []byte(rest[:i]), body, true
}

// scan lists every candidate with the file it came from.
func (s *Service) scan() ([]Candidate, map[string]string, []SourceNote) {
	var cands []Candidate
	files := map[string]string{}
	found := map[string]int{}
	looked := map[string]bool{}
	for i, d := range s.agentDirs() {
		ents, err := os.ReadDir(d.dir)
		if err != nil {
			continue
		}
		looked[d.provider] = true
		n := 0
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), d.ext) || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if n++; n > maxImportFiles {
				break
			}
			path := filepath.Join(d.dir, e.Name())
			p, ok := readAgent(path, d.ext)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%s:%d:%s", d.provider, i, e.Name())
			desc := p.description
			if len([]rune(desc)) > 300 {
				desc = string([]rune(desc)[:300]) + "…"
			}
			cands = append(cands, Candidate{Key: key, Source: d.source, Provider: d.provider, Name: p.name, Description: desc, Model: p.model, HasBody: p.body != "", File: e.Name()})
			files[key] = path
			found[d.provider]++
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Provider != cands[j].Provider {
			return cands[i].Provider < cands[j].Provider
		}
		return strings.ToLower(cands[i].Name) < strings.ToLower(cands[j].Name)
	})
	label := map[string]string{"claude": "Claude Code", "codex": "Codex", "grok": "Grok CLI"}
	where := map[string]string{
		"claude": "subagents in .claude/agents",
		"codex":  "agents in .codex/agents",
		"grok":   "agents in .grok/agents and personas in .grok/personas",
	}
	var notes []SourceNote
	r := s.roots()
	for _, p := range Providers {
		n := SourceNote{Provider: p, Found: found[p]}
		switch {
		case !r.On(p):
			n.Note = label[p] + " is turned off in Settings › Accounts."
		case found[p] > 0:
			n.Note = fmt.Sprintf("%d found: %s.", found[p], where[p])
		case looked[p]:
			n.Note = label[p] + " has no saved agents here; make one here."
		default:
			n.Note = label[p] + " exposes no saved agents on this machine (no " + where[p] + "); make one here."
		}
		if p == "grok" {
			n.Note += " Personas written inline in config.toml are not read: Lucidbench never opens config.toml."
		}
		notes = append(notes, n)
	}
	if cands == nil {
		cands = []Candidate{}
	}
	return cands, files, notes
}

// ImportCandidates lists the agents that can be imported.
func (s *Service) ImportCandidates() ImportList {
	c, _, n := s.scan()
	return ImportList{Candidates: c, Notes: n}
}

// ImportRequest is the body of POST /api/assistant/bots/import.
type ImportRequest struct {
	Key string `json:"key"`
	// Persona imports the agent's instructions as the persona; without it
	// the persona is the agent's description only.
	Persona bool `json:"persona"`
}

// Import saves one candidate as a bot.
func (s *Service) Import(req ImportRequest) (*Bot, error) {
	_, files, _ := s.scan()
	path, ok := files[req.Key]
	if !ok {
		return nil, fmt.Errorf("%w: no agent %q to import; list them again", ErrNotFound, req.Key)
	}
	provider, _, _ := strings.Cut(req.Key, ":")
	ext := strings.ToLower(filepath.Ext(path))
	p, ok := readAgent(path, ext)
	if !ok {
		return nil, fmt.Errorf("%w: the agent file cannot be read any more", ErrNotFound)
	}
	persona := p.description
	if req.Persona && p.body != "" {
		persona = p.body
	}
	if len(persona) > MaxPersona {
		persona = persona[:MaxPersona]
	}
	if persona == "" {
		persona = "You are " + p.name + "."
	}
	name := p.name
	if len(name) > MaxName {
		name = name[:MaxName]
	}
	source := provider + " agents"
	if strings.Contains(filepath.ToSlash(path), "/personas/") {
		source = provider + " personas"
	}
	return s.SaveBot(Bot{ID: s.FreeBotID(name), Name: name, Provider: provider, Model: p.model, Persona: persona, Source: source})
}
