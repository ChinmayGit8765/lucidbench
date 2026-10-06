package projects

// "Suggest answers from the repo": the user's own CLI reads a listing of the
// project's file names and guesses the answers. The CLI runs with no tools
// (agentexec.ToolsNone), so it sees only the names and the project's one-line
// summary built here, never file contents. Confidential projects are refused,
// and nothing the CLI answers is saved: the user confirms every answer.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/assess"
)

// suggestProviders is the order the user's CLIs are tried in.
var suggestProviders = []string{"claude", "codex", "grok"}

const (
	listingDepth = 2
	listingMax   = 150
)

// listingSkip names folders that say nothing about the project's kind.
var listingSkip = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "target": true, "dist": true, "build": true,
	".venv": true, "venv": true, "__pycache__": true, ".next": true, "bin": true, "obj": true, ".cache": true,
}

// secretName reports whether a file name looks like a credential; such names
// are left out of the listing.
func secretName(n string) bool {
	l := strings.ToLower(n)
	return strings.HasPrefix(l, ".env") || strings.HasSuffix(l, ".pem") || strings.HasSuffix(l, ".key") ||
		strings.HasPrefix(l, "id_rsa") || strings.HasPrefix(l, "id_ed25519") || strings.Contains(l, "secret") || strings.Contains(l, "credential")
}

// repoListing returns up to listingMax relative paths below root, folders
// with a trailing slash, shallow first.
func repoListing(root string) []string {
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
			if len(out) >= listingMax || secretName(name) {
				continue
			}
			if e.IsDir() {
				if listingSkip[name] {
					continue
				}
				out = append(out, rel+name+"/")
				sub = append(sub, e)
				continue
			}
			out = append(out, rel+name)
		}
		if depth < listingDepth {
			for _, e := range sub {
				walk(filepath.Join(dir, e.Name()), rel+e.Name()+"/", depth+1)
			}
		}
	}
	walk(root, "", 1)
	if len(out) > listingMax {
		out = out[:listingMax]
	}
	return out
}

const suggestSystem = `You help a developer classify their own project. You are given the project's name, a one-line summary, a list of its file and folder names, and a list of questions. Answer each question you can from the names alone, and leave out any you cannot tell. Reply with one JSON object and nothing else: question ids as keys, answers as string values. For a choice, use exactly one of its options. For a bool, use "true" or "false". Do not guess.`

func suggestPrompt(p Project, k assess.Kind, files []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", p.Name)
	if p.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", strings.Join(strings.Fields(p.Summary), " "))
	}
	fmt.Fprintf(&b, "Kind being assessed: %s (%s)\n\nFiles and folders:\n", k.Name, k.Blurb)
	for _, f := range files {
		b.WriteString(f + "\n")
	}
	b.WriteString("\nQuestions:\n")
	for _, q := range k.Questions {
		fmt.Fprintf(&b, "- %s (%s): %s", q.ID, q.Type, q.Text)
		if len(q.Options) > 0 {
			fmt.Fprintf(&b, " Options: %s", strings.Join(q.Options, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// parseSuggestion keeps the answers in text that fit the kind's questions.
// Anything else the model said is dropped rather than trusted.
func parseSuggestion(text string, k assess.Kind) (map[string]string, error) {
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j < i {
		return nil, errors.New("the answer was not JSON")
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(text[i:j+1]), &raw); err != nil {
		return nil, fmt.Errorf("the answer was not JSON: %v", err)
	}
	out := map[string]string{}
	for _, q := range k.Questions {
		v, ok := raw[q.ID]
		if !ok {
			continue
		}
		var s string
		switch x := v.(type) {
		case string:
			s = strings.TrimSpace(x)
		case bool:
			s = fmt.Sprint(x)
		default:
			continue
		}
		switch q.Type {
		case assess.TypeChoice:
			if idx := slices.IndexFunc(q.Options, func(o string) bool { return strings.EqualFold(o, s) }); idx >= 0 {
				out[q.ID] = q.Options[idx]
			}
		case assess.TypeBool:
			switch strings.ToLower(s) {
			case "true", "yes":
				out[q.ID] = "true"
			case "false", "no":
				out[q.ID] = "false"
			}
		case assess.TypeText:
			if r := []rune(s); s != "" && len(r) <= assess.MaxText {
				out[q.ID] = s
			}
		}
	}
	return out, nil
}

// suggestAnswers asks the first usable CLI. It returns the answers it could
// use and the provider that gave them.
func (a *AssessAPI) suggestAnswers(ctx context.Context, p Project, k assess.Kind) (map[string]string, string, error) {
	if p.Visibility == "confidential" {
		return nil, "", fmt.Errorf("%w: %s is confidential and is never sent to an AI provider", ErrConfidential, p.ID)
	}
	if p.LocalPath == "" {
		return nil, "", fmt.Errorf("%w: %s has no local_path in projects.yaml", ErrBadRequest, p.ID)
	}
	if st, err := os.Stat(p.LocalPath); err != nil || !st.IsDir() {
		return nil, "", fmt.Errorf("%w: the folder %s is not there", ErrBadRequest, HomeHint(p.LocalPath, homeDir()))
	}
	files := repoListing(p.LocalPath)
	if len(files) == 0 {
		return nil, "", fmt.Errorf("%w: the project folder is empty", ErrBadRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = &agentexec.Runner{}
	}
	var last error
	for _, prov := range suggestProviders {
		res, err := runner.Run(ctx, agentexec.Request{
			Provider: prov, SystemPrompt: suggestSystem, Prompt: suggestPrompt(p, k, files),
			Tools: agentexec.ToolsNone, Timeout: agentexec.TimeoutNone,
		})
		if errors.Is(err, agentexec.ErrCLIMissing) || errors.Is(err, agentexec.ErrNotSignedIn) {
			last = err
			continue
		}
		if errors.Is(err, agentexec.ErrInContainer) {
			return nil, "", fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		if err != nil {
			return nil, "", fmt.Errorf("%s could not answer: %v", prov, err)
		}
		answers, err := parseSuggestion(res.Text, k)
		if err != nil {
			return nil, "", fmt.Errorf("%s: %v", prov, err)
		}
		return answers, prov, nil
	}
	return nil, "", fmt.Errorf("%w: no CLI is installed and signed in (claude, codex or grok): %v", ErrUnavailable, last)
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}
