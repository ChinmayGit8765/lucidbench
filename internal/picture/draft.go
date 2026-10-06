package picture

// "Draft from code": the user's own CLI reads a listing of the repository's
// file names and the top of its README and sketches a Mermaid service and
// data-flow diagram. The CLI runs with no tools (agentexec.ToolsNone), so
// this prompt is all it sees. Confidential projects are refused, and the
// answer is only returned: the user previews it and saves it.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Errors the draft step returns; the handler maps each to a status.
var (
	ErrConfidential = errors.New("confidential")
	ErrUnavailable  = errors.New("unavailable")
	ErrBadRequest   = errors.New("bad request")
)

const (
	treeDepth   = 3
	treeMax     = 300
	readmeLines = 200
	readmeBytes = 24 << 10
)

// treeSkip names folders that say nothing about the architecture.
var treeSkip = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "vendor": true, "target": true, "build": true,
	".venv": true, "venv": true, "__pycache__": true, ".next": true, "bin": true, "obj": true, ".cache": true,
}

// secretName reports whether a file name looks like a credential; such names
// are left out of the listing.
func secretName(n string) bool {
	l := strings.ToLower(n)
	return strings.HasPrefix(l, ".env") || strings.HasSuffix(l, ".pem") || strings.HasSuffix(l, ".key") ||
		strings.HasPrefix(l, "id_rsa") || strings.HasPrefix(l, "id_ed25519") || strings.Contains(l, "secret") || strings.Contains(l, "credential")
}

// Tree returns up to treeMax relative paths below root, three levels deep,
// folders with a trailing slash, shallow first.
func Tree(root string) []string {
	var out []string
	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		var sub []os.DirEntry
		for _, e := range entries {
			name := e.Name()
			if len(out) >= treeMax || secretName(name) {
				continue
			}
			if e.IsDir() {
				if treeSkip[name] {
					continue
				}
				out = append(out, rel+name+"/")
				sub = append(sub, e)
				continue
			}
			if e.Type().IsRegular() {
				out = append(out, rel+name)
			}
		}
		if depth < treeDepth {
			for _, e := range sub {
				walk(filepath.Join(dir, e.Name()), rel+e.Name()+"/", depth+1)
			}
		}
	}
	walk(root, "", 1)
	if len(out) > treeMax {
		out = out[:treeMax]
	}
	return out
}

// ReadmeHead returns the first readmeLines lines of the repository's README,
// or "" when it has none.
func ReadmeHead(root string) string {
	ents, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	for _, e := range ents {
		l := strings.ToLower(e.Name())
		if !e.Type().IsRegular() || (l != "readme.md" && l != "readme" && l != "readme.txt" && l != "readme.markdown") {
			continue
		}
		f, err := os.Open(filepath.Join(root, e.Name()))
		if err != nil {
			return ""
		}
		defer f.Close()
		var b strings.Builder
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for n := 0; n < readmeLines && sc.Scan() && b.Len() < readmeBytes; n++ {
			b.WriteString(sc.Text() + "\n")
		}
		return b.String()
	}
	return ""
}

const draftSystem = `You are a software architect. You are given a repository's file and folder names and the start of its README. Sketch the system as one Mermaid flowchart (flowchart LR) of its services, programs, data stores and external systems and how data flows between them. Use only what the names and README support; do not invent components. Keep it under 25 nodes. Reply with the Mermaid source only, in one mermaid code block, with no other text.`

// DraftPrompt is the whole user prompt: the tree and the README excerpt.
func DraftPrompt(tree []string, readme string) string {
	var b strings.Builder
	b.WriteString("File tree (3 levels, folders end with /):\n")
	for _, f := range tree {
		b.WriteString(f + "\n")
	}
	b.WriteString("\nREADME (first 200 lines):\n")
	if strings.TrimSpace(readme) == "" {
		b.WriteString("(none)\n")
	} else {
		b.WriteString(readme)
	}
	return b.String()
}

// ExtractMermaid pulls the diagram out of a reply: the first fenced block if
// there is one, else the whole text.
func ExtractMermaid(text string) string {
	t := strings.TrimSpace(text)
	if i := strings.Index(t, "```"); i >= 0 {
		rest := t[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			rest = rest[:j]
		}
		t = strings.TrimSpace(rest)
	}
	return t + "\n"
}

// DraftRequest chooses the provider and model; both may be empty.
type DraftRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// DraftResult is the preview. Nothing in it has been saved.
type DraftResult struct {
	Mermaid  string          `json:"mermaid"`
	Provider string          `json:"provider"`
	Model    string          `json:"model,omitempty"`
	Usage    agentexec.Usage `json:"usage"`
}

// candidate is one provider and model to try.
type candidate struct{ provider, model string }

// candidates is the order tried when the user chose nothing: the cheapest
// first (Claude's small model), then the other signed-in CLIs.
func candidates(r DraftRequest) []candidate {
	if r.Provider != "" {
		m := r.Model
		if m == "" && r.Provider == "claude" {
			m = "haiku"
		}
		return []candidate{{r.Provider, m}}
	}
	return []candidate{{"claude", firstNonEmpty(r.Model, "haiku")}, {"codex", r.Model}, {"grok", r.Model}}
}

// Draft asks the user's CLI for a diagram of p's repository.
func Draft(ctx context.Context, run *agentexec.Runner, p projects.Project, r DraftRequest) (*DraftResult, error) {
	if p.Visibility == "confidential" {
		return nil, fmt.Errorf("%w: %s is confidential and is never sent to an AI provider", ErrConfidential, p.ID)
	}
	if p.LocalPath == "" {
		return nil, fmt.Errorf("%w: %s has no local_path in projects.yaml", ErrBadRequest, p.ID)
	}
	if st, err := os.Stat(p.LocalPath); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("%w: the project folder is not there", ErrBadRequest)
	}
	tree := Tree(p.LocalPath)
	if len(tree) == 0 {
		return nil, fmt.Errorf("%w: the project folder is empty", ErrBadRequest)
	}
	if r.Provider != "" {
		if _, ok := agentexec.Logins[r.Provider]; !ok {
			return nil, fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
		}
	}
	if run == nil {
		run = &agentexec.Runner{}
	}
	prompt := DraftPrompt(tree, ReadmeHead(p.LocalPath))
	var last error
	for _, c := range candidates(r) {
		res, err := run.Run(ctx, agentexec.Request{
			Provider: c.provider, Model: c.model, SystemPrompt: draftSystem, Prompt: prompt,
			Tools: agentexec.ToolsNone, Timeout: agentexec.TimeoutNone,
		})
		if errors.Is(err, agentexec.ErrCLIMissing) || errors.Is(err, agentexec.ErrNotSignedIn) {
			last = err
			continue
		}
		if errors.Is(err, agentexec.ErrInContainer) {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if err != nil {
			return nil, fmt.Errorf("%s could not draft it: %v", c.provider, err)
		}
		src := ExtractMermaid(res.Text)
		if strings.TrimSpace(src) == "" {
			return nil, fmt.Errorf("%s returned no diagram", c.provider)
		}
		return &DraftResult{Mermaid: src, Provider: c.provider, Model: firstNonEmpty(res.Usage.Model, c.model), Usage: res.Usage}, nil
	}
	return nil, fmt.Errorf("%w: no CLI is installed and signed in (claude, codex or grok): %v", ErrUnavailable, last)
}
