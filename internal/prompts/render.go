package prompts

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// varRE matches {{project.name}} and the like. Only names in the variable
// map are replaced; anything else is left as written, so a template can hold
// literal braces.
var varRE = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*)\s*\}\}`)

// Variables are the names a template may use. Each is filled from the
// project picked in Studio, or by Work from the session's project.
var Variables = []struct{ Name, Hint string }{
	{"project.id", "the project's id in projects.yaml"},
	{"project.name", "the project's name"},
	{"project.repo", "its GitHub repo, owner/name"},
	{"project.local_path", "its checkout, with your home folder as ~"},
	{"project.summary", "its one-line summary"},
	{"project.kind", "its assessed kind"},
	{"project.risk", "its assessed risk level"},
	{"project.criteria", "the done criteria its assessment suggests, one per line"},
	{"project.checks", "the check commands its assessment suggests, one per line"},
	{"date", "today, as YYYY-MM-DD"},
}

func knownVar(name string) bool {
	for _, v := range Variables {
		if v.Name == name {
			return true
		}
	}
	return false
}

// Expand replaces the known variables in s with their values. It returns the
// names it could not fill: unknown names and known ones without a value.
func Expand(s string, vars map[string]string) (string, []string) {
	var missing []string
	out := varRE.ReplaceAllStringFunc(s, func(m string) string {
		name := varRE.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok && strings.TrimSpace(v) != "" {
			return v
		}
		missing = append(missing, name)
		return m
	})
	return out, missing
}

// Rendered is a prompt ready to send.
type Rendered struct {
	Text string `json:"text"`
	// Chars counts characters (runes), Tokens estimates tokens as chars / 4.
	Chars  int `json:"chars"`
	Tokens int `json:"tokens"`
	// Unfilled are the variable names left as written.
	Unfilled []string `json:"unfilled"`
}

// EstimateTokens is the rough rule of four characters to a token. It is an
// estimate: the real count depends on the model's tokenizer.
func EstimateTokens(s string) int { return (utf8.RuneCountInString(s) + 3) / 4 }

// Heading is how a section is introduced in the text. The role has none: it
// opens the prompt. A task whose body already starts with a heading (a card's
// "# Task: title", or a prompt composed in Studio) keeps its own.
func heading(id, body string) string {
	switch id {
	case Role:
		return ""
	case Task:
		if strings.HasPrefix(body, "#") {
			return ""
		}
		return "# Task\n\n"
	}
	return "## " + SectionInfo[id].Title + "\n\n"
}

// Render joins the sections in order, skipping empty ones, after expanding
// the variables. Sources are added to the context section, each under its
// own heading.
func Render(sections []Section, vars map[string]string, sources []Source) Rendered {
	var parts []string
	unfilled := map[string]bool{}
	hasContext := false
	for _, s := range sections {
		if s.ID == Context {
			hasContext = true
		}
	}
	for _, s := range sections {
		body, miss := Expand(strings.TrimSpace(strings.ReplaceAll(s.Body, "\r\n", "\n")), vars)
		for _, m := range miss {
			unfilled[m] = true
		}
		if s.ID == Context || (!hasContext && s.ID == Task && len(sources) > 0) {
			if extra := sourcesText(sources); extra != "" {
				if s.ID == Context {
					body = strings.TrimSpace(body + "\n\n" + extra)
				} else {
					parts = append(parts, "## Context\n\n"+extra)
				}
			}
		}
		if body == "" {
			continue
		}
		parts = append(parts, heading(s.ID, body)+body)
	}
	text := strings.Join(parts, "\n\n") + "\n"
	if len(parts) == 0 {
		text = ""
	}
	out := Rendered{Text: text, Chars: utf8.RuneCountInString(text), Tokens: EstimateTokens(text), Unfilled: []string{}}
	for m := range unfilled {
		out.Unfilled = append(out.Unfilled, m)
	}
	sort.Strings(out.Unfilled)
	return out
}

// ForWork drops what Work adds to every prompt itself: the role, and the
// constraint lines that are the builder template's safety rules. What is
// left is what a Work session gets as its task.
func ForWork(sections []Section) []Section {
	b, _ := Builtin("builder")
	safety := map[string]bool{}
	for _, l := range strings.Split(b.Body(Constraints), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			safety[l] = true
		}
	}
	var out []Section
	for _, s := range sections {
		switch s.ID {
		case Role:
			continue
		case Constraints:
			var keep []string
			for _, l := range strings.Split(strings.ReplaceAll(s.Body, "\r\n", "\n"), "\n") {
				if !safety[strings.TrimSpace(l)] {
					keep = append(keep, l)
				}
			}
			s.Body = strings.Join(keep, "\n")
		}
		out = append(out, s)
	}
	return out
}

func sourcesText(sources []Source) string {
	var b strings.Builder
	for i, s := range sources {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "### %s\n\n", s.Label)
		text := strings.TrimSpace(s.Text)
		if s.Format == FormatCode {
			text = "```\n" + text + "\n```"
		}
		b.WriteString(text)
	}
	return b.String()
}

// Finding is one lint result. An error blocks sending.
type Finding struct {
	Severity string `json:"severity"` // error | warning | info
	Code     string `json:"code"`
	Message  string `json:"message"`
	Section  string `json:"section,omitempty"`
}

// Lint severities.
const (
	SevError   = "error"
	SevWarning = "warning"
	SevInfo    = "info"
)

// MaxCouncilInput mirrors council.MaxInput: a longer braindump is refused.
const MaxCouncilInput = 20000

var doneRE = regexp.MustCompile(`(?im)(done (when|criteria)|acceptance criteria|^\s*[-*] \[[ xX]\]|proof:|definition of done)`)

// Lint checks a rendered prompt before it is sent to target.
func Lint(sections []Section, r Rendered, sources []Source, target string) []Finding {
	out := []Finding{}
	body := map[string]string{}
	for _, s := range sections {
		body[s.ID] = strings.TrimSpace(s.Body)
	}
	external := target != TargetCopy
	for _, s := range sources {
		if s.Confidential && external {
			out = append(out, Finding{Severity: SevError, Code: "confidential", Section: Context,
				Message: fmt.Sprintf("%s is confidential: it never goes to a provider. Remove it, or copy the prompt to use it locally.", s.Label)})
		}
	}
	if strings.TrimSpace(body[Task]) == "" {
		out = append(out, Finding{Severity: SevError, Code: "empty-task", Section: Task, Message: "The task is empty: say what the agent should do."})
	}
	if body[Verify] == "" {
		f := Finding{Severity: SevWarning, Code: "no-verify", Section: Verify, Message: "No verify section: the agent will not know how to prove its work."}
		if target == TargetWork {
			f.Severity, f.Message = SevInfo, "No verify section: Work adds its default one (check the done criteria, run the tests)."
		}
		if target != TargetCouncil {
			out = append(out, f)
		}
	}
	if body[Report] == "" && target != TargetCouncil {
		f := Finding{Severity: SevWarning, Code: "no-report", Section: Report, Message: "No report section: say what the answer must contain."}
		if target == TargetWork {
			f.Severity, f.Message = SevInfo, "No report section: Work adds its default three-part report."
		}
		out = append(out, f)
	}
	if !doneRE.MatchString(body[Task]+"\n"+body[Verify]+"\n"+body[Contract]+"\n"+contextText(sources)) && target != TargetCouncil {
		out = append(out, Finding{Severity: SevWarning, Code: "no-done-criteria", Section: Task,
			Message: `No done criteria: add "Done when …" lines, each with a proof the agent can run.`})
	}
	for _, hit := range FindSecrets(r.Text) {
		out = append(out, Finding{Severity: SevWarning, Code: "secret",
			Message: fmt.Sprintf("Looks like a secret (%s: %s). Remove it before sending; prompts are logged by the provider.", hit.Kind, hit.Excerpt)})
	}
	for _, name := range r.Unfilled {
		msg := fmt.Sprintf("{{%s}} has no value and is left as written.", name)
		if strings.HasPrefix(name, "project.") && knownVar(name) {
			msg = fmt.Sprintf("{{%s}} has no value: pick a project, or it is not set for this one.", name)
		} else if !knownVar(name) {
			msg = fmt.Sprintf("{{%s}} is not a variable Studio knows; it is sent as written.", name)
		}
		out = append(out, Finding{Severity: SevInfo, Code: "unfilled", Message: msg})
	}
	if target == TargetCouncil && r.Chars > MaxCouncilInput {
		out = append(out, Finding{Severity: SevError, Code: "too-long",
			Message: fmt.Sprintf("The Council takes at most %d characters; this is %d. Drop a source or shorten the text.", MaxCouncilInput, r.Chars)})
	}
	sort.SliceStable(out, func(i, j int) bool { return sevRank[out[i].Severity] < sevRank[out[j].Severity] })
	return out
}

var sevRank = map[string]int{SevError: 0, SevWarning: 1, SevInfo: 2}

func contextText(sources []Source) string {
	var b strings.Builder
	for _, s := range sources {
		b.WriteString(s.Text + "\n")
	}
	return b.String()
}

// Blocked reports whether any finding is an error.
func Blocked(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == SevError {
			return true
		}
	}
	return false
}

// SecretHit is one secret-looking string, shown cut so the secret itself is
// never echoed back whole.
type SecretHit struct {
	Kind    string `json:"kind"`
	Excerpt string `json:"excerpt"`
}

var secretPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
	// Written in parts so this file is not itself flagged as holding a key.
	{"private key", regexp.MustCompile(`-----BEGIN [A-Z ]*` + `PRIVATE` + ` KEY-----`)},
	{"API key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)},
	{"GitHub token", regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,})`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)},
	{"JWT", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"credential", regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|token|password|passwd)\b\s*[:=]\s*["']?([A-Za-z0-9_\-/+=.]{12,})`)},
}

// FindSecrets lists the secret-looking strings in text.
func FindSecrets(text string) []SecretHit {
	var out []SecretHit
	seen := map[string]bool{}
	for _, p := range secretPatterns {
		for _, m := range p.re.FindAllString(text, 5) {
			if seen[m] {
				continue
			}
			seen[m] = true
			cut := m
			if r := []rune(m); len(r) > 10 {
				cut = string(r[:10]) + "…"
			}
			out = append(out, SecretHit{Kind: p.kind, Excerpt: cut})
		}
	}
	return out
}
