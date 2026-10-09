// Package assistant is Lucidbench's built-in assistant: a chat with the
// user's own CLI (claude, codex or grok, or a saved bot on one of them) that
// answers in text and may propose actions from a fixed catalog. Every turn
// runs with agentexec.ToolsNone, so the model never touches the machine;
// each proposal is validated here and applied only when the user clicks
// Apply, through Lucidbench's own routes.
//
// Conversations live in <DataDir>/assistant/<id>.jsonl, saved bots in
// <DataDir>/bots/<id>.yaml, and one usage record per model call in
// <DataDir>/assistant/runs/, which the Usage page counts as "assistant".
package assistant

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/accounts"
	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/prompts"
)

// ChatPrompt is what the model is told on every turn, versioned with the code.
//
//go:embed prompts/assistant.md
var ChatPrompt string

// BraindumpPrompt splits a braindump into items.
//
//go:embed prompts/braindump.md
var BraindumpPrompt string

var versionRE = regexp.MustCompile(`^<!--\s*\S+\s+(v\d+)\s*-->`)

// PromptVersion reads "<!-- name vN -->" from a prompt's first line.
func PromptVersion(p string) string {
	if m := versionRE.FindStringSubmatch(p); m != nil {
		return m[1]
	}
	return "unversioned"
}

func stripVersion(p string) string {
	if i := strings.Index(p, "-->"); i >= 0 && strings.HasPrefix(p, "<!--") {
		return strings.TrimLeft(p[i+3:], "\r\n")
	}
	return p
}

// Providers is the order the user's CLIs are tried in when none is chosen.
var Providers = []string{"claude", "codex", "grok"}

// Limits on a turn.
const (
	MaxMessage = 8000
	// History is how many earlier messages are resent with each turn (the
	// CLIs keep no session for a tool-less answer), and HistoryChars caps
	// their total length.
	History      = 12
	HistoryChars = 16000
	// PageChars caps the current page sent as context.
	PageChars   = 4000
	TurnTimeout = 3 * time.Minute
)

// Errors the HTTP layer maps to statuses.
var (
	ErrBadRequest   = errors.New("bad request")
	ErrNotFound     = errors.New("not found")
	ErrConfidential = errors.New("confidential")
	ErrBusy         = errors.New("busy")
	ErrBadOutput    = errors.New("the model did not return a usable answer")
)

// Service runs assistant turns and keeps conversations and bots.
type Service struct {
	// Dir holds conversations (<id>.jsonl) and runs/.
	Dir string
	// BotsDir holds the saved bots.
	BotsDir  string
	Runner   *agentexec.Runner
	Projects func() (*projects.List, error)
	// Vault opens Memory; nil means no vault (no pages, no cards).
	Vault memory.Opener
	// Existing lists the cards and ideas a braindump item is compared with.
	Existing func() []Existing
	// Roots locates the CLIs' config folders for bot import; nil means the
	// user's home.
	Roots func() accounts.Roots
	Now   func() time.Time

	mu   sync.Mutex
	busy map[string]bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) runner() *agentexec.Runner {
	if s.Runner != nil {
		return s.Runner
	}
	return &agentexec.Runner{}
}

func (s *Service) projects() ([]projects.Project, error) {
	if s.Projects == nil {
		return nil, nil
	}
	l, err := s.Projects()
	if err != nil {
		return nil, fmt.Errorf("cannot read projects.yaml: %w", err)
	}
	return l.Projects, nil
}

// World reads what proposals are checked against, fresh.
func (s *Service) World() (*World, error) {
	ps, err := s.projects()
	if err != nil {
		return nil, err
	}
	w := &World{Projects: ps}
	if s.Vault != nil {
		if v, err := s.Vault(); err == nil {
			w.PageExists = func(p string) bool {
				_, err := v.Read(p)
				return err == nil
			}
			// Read the work board without creating it.
			if _, err := v.Read(boards.Dir + "/" + boards.DefaultBoard + ".md"); err == nil {
				if b, err := boards.Get(v, boards.DefaultBoard); err == nil {
					w.Columns = b.Columns
				}
			}
		}
	}
	return w, nil
}

// pickProvider resolves the provider for a call: the asked one, else the
// first installed CLI.
func (s *Service) pickProvider(want string) (string, error) {
	if want != "" {
		if _, ok := agentexec.Logins[want]; !ok {
			return "", fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
		}
		return want, nil
	}
	look := s.runner().LookPath
	if look == nil {
		look = exec.LookPath
	}
	for _, p := range Providers {
		if _, err := look(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: no provider CLI is installed (claude, codex or grok)", agentexec.ErrCLIMissing)
}

// RunRecord is one model call's cost, for the Usage page.
type RunRecord struct {
	Kind         string          `json:"kind"` // chat | braindump
	Conversation string          `json:"conversation,omitempty"`
	Created      time.Time       `json:"created"`
	Error        string          `json:"error,omitempty"`
	Usage        agentexec.Usage `json:"usage"`
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) record(kind, conv string, u agentexec.Usage, runErr error) {
	if s.Dir == "" || (u.DurationMS == 0 && u.InputTokens == 0 && u.OutputTokens == 0 && u.CostUSD == 0) {
		return
	}
	rec := RunRecord{Kind: kind, Conversation: conv, Created: s.now().UTC(), Usage: u}
	if runErr != nil {
		rec.Error = agentexec.Excerpt(runErr.Error())
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	dir := filepath.Join(s.Dir, "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, rec.Created.Format("20060102-150405")+"-"+randHex(3)+".json"), data, 0o600)
}

// projectContext lists the projects for the model. A confidential project
// shows as its id only: the model learns it exists and nothing else.
func projectContext(ps []projects.Project) string {
	if len(ps) == 0 {
		return "Projects: none yet (projects.yaml is empty or missing).\n"
	}
	var b strings.Builder
	b.WriteString("Projects (id · name · kind):\n")
	for _, p := range ps {
		if p.Visibility == "confidential" {
			fmt.Fprintf(&b, "- %s · confidential: details withheld\n", p.ID)
			continue
		}
		extra := ""
		if p.LocalPath == "" {
			extra = " · no local checkout"
		}
		fmt.Fprintf(&b, "- %s · %s · %s%s\n", p.ID, oneLine(p.Name), p.Category, extra)
	}
	return b.String()
}

// checkConfidential refuses text that would carry a confidential project or
// page to a provider: the subject project is confidential, the text names a
// confidential project, or it links a confidential Memory page. It is the
// rule Council and Work follow, plus the name check, because the assistant
// sees free text.
func (s *Service) checkConfidential(ps []projects.Project, subject string, texts ...string) error {
	if subject != "" {
		found := false
		for _, p := range ps {
			if p.ID != subject {
				continue
			}
			found = true
			if p.Visibility == "confidential" {
				return fmt.Errorf("%w: %s is a confidential project; the assistant never sends it to a provider", ErrConfidential, p.ID)
			}
		}
		if !found {
			return fmt.Errorf("%w: no project %q in projects.yaml", ErrBadRequest, subject)
		}
	}
	all := strings.ToLower(strings.Join(texts, "\n"))
	for _, p := range ps {
		if p.Visibility != "confidential" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(p.Name))
		if len(name) >= 3 && name != p.ID && containsWord(all, name) {
			return fmt.Errorf("%w: the message names the confidential project %s; the assistant never sends it to a provider", ErrConfidential, p.ID)
		}
	}
	var links []string
	for _, t := range texts {
		links = append(links, memory.Links(t)...)
	}
	if len(links) == 0 {
		return nil
	}
	if s.Vault == nil {
		return fmt.Errorf("%w: the message links Memory pages, but there is no vault to check them against", ErrConfidential)
	}
	v, err := s.Vault()
	if err != nil {
		return fmt.Errorf("%w: the message links Memory pages, but the vault cannot be opened to check them: %v", ErrConfidential, err)
	}
	for _, l := range links {
		for _, p := range resolveLink(v, l) {
			if conf, err := v.IsConfidential(p); err == nil && conf {
				return fmt.Errorf("%w: [[%s]] is a confidential Memory page; the assistant never sends it to a provider", ErrConfidential, l)
			}
		}
	}
	return nil
}

// containsWord reports whether needle appears in hay with no letter or
// digit right before or after it.
func containsWord(hay, needle string) bool {
	for i := 0; ; {
		j := strings.Index(hay[i:], needle)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(needle)
		if (at == 0 || !isWordByte(hay[at-1])) && (end == len(hay) || !isWordByte(hay[end])) {
			return true
		}
		i = at + 1
	}
}

func isWordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_'
}

// resolveLink returns the vault paths a [[link]] can mean: the page at that
// path, a folder at that path, and every page with that name (the council's
// rule).
func resolveLink(v *memory.Vault, link string) []string {
	if i := strings.IndexAny(link, "|#"); i >= 0 {
		link = link[:i]
	}
	link = strings.TrimSpace(link)
	var out []string
	if _, err := v.Read(link + ".md"); err == nil {
		out = append(out, link+".md")
	}
	if ents, err := v.List(link); err == nil && ents != nil {
		out = append(out, link)
	}
	name := strings.ToLower(path.Base(link))
	root := v.Root()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		if strings.ToLower(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))) != name {
			return nil
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

// claim marks a conversation busy; release with the returned func.
func (s *Service) claim(id string) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy == nil {
		s.busy = map[string]bool{}
	}
	if s.busy[id] {
		return nil, fmt.Errorf("%w: a turn is already running in this conversation", ErrBusy)
	}
	s.busy[id] = true
	return func() {
		s.mu.Lock()
		delete(s.busy, id)
		s.mu.Unlock()
	}, nil
}

// TurnRequest is the body of POST /api/assistant/turn.
type TurnRequest struct {
	// Conversation continues one; empty starts a new one.
	Conversation string `json:"conversation,omitempty"`
	Message      string `json:"message"`
	Provider     string `json:"provider,omitempty"`
	Profile      string `json:"profile,omitempty"`
	Model        string `json:"model,omitempty"`
	// Bot talks to a saved bot: its provider, model and persona, and only
	// its allowed actions.
	Bot string `json:"bot,omitempty"`
	// Project is the conversation's subject; a confidential one is refused.
	Project string `json:"project,omitempty"`
	// Page is the Memory page the user is looking at, sent as context.
	Page string `json:"page,omitempty"`
}

// TurnResult is the answer to one turn.
type TurnResult struct {
	Conversation Conversation `json:"conversation"`
}

// Turn runs one exchange: it checks the message, builds the context, runs
// the CLI with no tools, validates the proposed actions and appends both
// messages to the conversation.
func (s *Service) Turn(ctx context.Context, req TurnRequest) (*TurnResult, error) {
	msg := strings.TrimSpace(req.Message)
	if msg == "" || len(msg) > MaxMessage {
		return nil, fmt.Errorf("%w: write a message of 1 to %d characters", ErrBadRequest, MaxMessage)
	}
	if hits := prompts.FindSecrets(msg); len(hits) > 0 {
		return nil, fmt.Errorf("%w: the message has what looks like a secret (%s); remove it first", ErrBadRequest, hits[0].Kind)
	}
	var conv *Conversation
	if req.Conversation != "" {
		c, err := s.Conversation(req.Conversation)
		if err != nil {
			return nil, err
		}
		conv = c
		if req.Project == "" {
			req.Project = c.Project
		}
		if req.Bot == "" {
			req.Bot = c.Bot
		}
	}
	var bot *Bot
	if req.Bot != "" {
		b, err := s.Bot(req.Bot)
		if err != nil {
			return nil, err
		}
		bot = b
		req.Provider, req.Model, req.Profile = b.Provider, b.Model, b.Profile
	}
	ps, err := s.projects()
	if err != nil {
		return nil, err
	}
	req.Project = strings.TrimSpace(req.Project)
	texts := []string{msg}
	if bot != nil {
		// The persona goes into the system prompt, so it is checked too.
		texts = append(texts, bot.Name, bot.Persona)
	}
	if conv != nil {
		for _, m := range conv.Messages {
			texts = append(texts, m.Text)
		}
	}
	if err := s.checkConfidential(ps, req.Project, texts...); err != nil {
		return nil, err
	}
	pageText, err := s.pageContext(req.Page)
	if err != nil {
		return nil, err
	}
	provider, err := s.pickProvider(req.Provider)
	if err != nil {
		return nil, err
	}
	model := req.Model
	if model == "" && bot == nil {
		model = prompts.CheapModels[provider]
	}
	w, err := s.World()
	if err != nil {
		return nil, err
	}

	if conv == nil {
		conv = &Conversation{ID: newID(s.now()), Title: firstLine(msg, 60), Created: s.now().UTC(), Project: req.Project, Bot: req.Bot, Messages: []Message{}}
		if err := s.appendLine(conv.ID, line{Type: "meta", Meta: &meta{ID: conv.ID, Title: conv.Title, Created: conv.Created, Project: conv.Project, Bot: conv.Bot}}); err != nil {
			return nil, err
		}
	}
	release, err := s.claim(conv.ID)
	if err != nil {
		return nil, err
	}
	defer release()

	system := stripVersion(ChatPrompt)
	var allowed []string
	if bot != nil {
		allowed = bot.AllowedActions
		system += "\n\n## You are the user's bot \"" + bot.Name + "\"\n\n" + bot.Persona
		if allowed != nil {
			system += "\n\nThis bot may propose only these actions: " + strings.Join(allowed, ", ") + ". Propose no other kind."
		}
	}
	prompt := s.buildPrompt(ps, w, conv, req.Project, pageText, msg)

	user := Message{Role: "user", Text: msg, Time: s.now().UTC(), Provider: provider, Model: model, Bot: req.Bot, Project: req.Project, Page: req.Page}
	if err := s.appendLine(conv.ID, line{Type: "message", Message: &user}); err != nil {
		return nil, err
	}
	conv.Messages = append(conv.Messages, user)

	res, runErr := s.runner().Run(ctx, agentexec.Request{
		Provider: provider, Profile: req.Profile, Model: model, SystemPrompt: system, Prompt: prompt,
		Tools: agentexec.ToolsNone, Timeout: TurnTimeout,
	})
	reply := Message{Role: "assistant", Time: s.now().UTC(), Provider: provider, Model: model, Bot: req.Bot, Prompt: PromptVersion(ChatPrompt), Proposals: []Proposal{}}
	if res != nil {
		s.record("chat", conv.ID, res.Usage, runErr)
		reply.Usage = &res.Usage
	}
	if runErr != nil {
		reply.Error = runErr.Error()
	} else {
		prose, actions, perr := ParseActions(res.Text)
		reply.Text = prose
		if perr != nil {
			reply.Note = perr.Error()
		}
		for _, a := range actions {
			reply.Proposals = append(reply.Proposals, Validate(a, w, allowed))
		}
		if reply.Text == "" && len(reply.Proposals) == 0 {
			reply.Error = ErrBadOutput.Error() + ": the answer was empty"
		}
	}
	if err := s.appendLine(conv.ID, line{Type: "message", Message: &reply}); err != nil {
		return nil, err
	}
	conv.Messages = append(conv.Messages, reply)
	conv.Updated = reply.Time
	if runErr != nil {
		return &TurnResult{Conversation: *conv}, runErr
	}
	return &TurnResult{Conversation: *conv}, nil
}

// pageContext reads the page the user is looking at, refusing a
// confidential one.
func (s *Service) pageContext(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}
	if s.Vault == nil {
		return "", fmt.Errorf("%w: there is no Memory vault", ErrBadRequest)
	}
	v, err := s.Vault()
	if err != nil {
		return "", err
	}
	pg, err := v.Read(p)
	if err != nil {
		return "", fmt.Errorf("%w: cannot read the page %s: %v", ErrBadRequest, p, err)
	}
	if pg.Confidential {
		return "", fmt.Errorf("%w: %s is a confidential Memory page; the assistant never sends it to a provider", ErrConfidential, p)
	}
	body := pg.Body
	if len(body) > PageChars {
		body = body[:PageChars] + "\n…(cut)"
	}
	return "Current page: " + pg.Path + " (" + pg.Title + ")\n\n" + body, nil
}

// buildPrompt is the turn's prompt: the state of Lucidbench, the earlier
// messages and the new one.
func (s *Service) buildPrompt(ps []projects.Project, w *World, conv *Conversation, subject, page, msg string) string {
	var b strings.Builder
	b.WriteString("## Lucidbench state\n\n")
	b.WriteString(projectContext(ps))
	fmt.Fprintf(&b, "\nWork board columns: %s\n", strings.Join(w.columns(), ", "))
	if subject != "" {
		fmt.Fprintf(&b, "\nThis conversation is about the project %s.\n", subject)
	}
	if page != "" {
		b.WriteString("\n" + page + "\n")
	}
	if h := history(conv.Messages); h != "" {
		b.WriteString("\n## Earlier in this conversation\n\n" + h)
	}
	b.WriteString("\n## The user's message\n\n" + msg + "\n")
	return b.String()
}

// history is the last History messages, newest kept when HistoryChars is
// reached, with each proposal's outcome.
func history(ms []Message) string {
	if len(ms) > History {
		ms = ms[len(ms)-History:]
	}
	var parts []string
	total := 0
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		text := m.Text
		if len(text) > 2000 {
			text = text[:2000] + "…"
		}
		who := "User"
		if m.Role == "assistant" {
			who = "You"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s: %s\n", who, text)
		for _, p := range m.Proposals {
			fmt.Fprintf(&b, "  (proposed %s: %s, %s)\n", p.Action, p.Summary, p.Status)
		}
		if total+b.Len() > HistoryChars {
			break
		}
		total += b.Len()
		parts = append([]string{b.String()}, parts...)
	}
	return strings.Join(parts, "")
}
