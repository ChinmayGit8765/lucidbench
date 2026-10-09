package assistant

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// Message is one side of an exchange.
type Message struct {
	Role     string    `json:"role"` // user | assistant
	Text     string    `json:"text"`
	Time     time.Time `json:"time"`
	Provider string    `json:"provider,omitempty"`
	Model    string    `json:"model,omitempty"`
	Bot      string    `json:"bot,omitempty"`
	Project  string    `json:"project,omitempty"`
	Page     string    `json:"page,omitempty"`
	// Prompt is the system prompt's version (assistant messages).
	Prompt    string           `json:"prompt_version,omitempty"`
	Proposals []Proposal       `json:"proposals,omitempty"`
	Usage     *agentexec.Usage `json:"usage,omitempty"`
	Error     string           `json:"error,omitempty"`
	// Note is a problem with the answer that did not stop it, such as an
	// actions block that was not valid JSON.
	Note string `json:"note,omitempty"`
}

// Conversation is a whole chat, folded from its file.
type Conversation struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project,omitempty"`
	Bot      string    `json:"bot,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Messages []Message `json:"messages"`
}

// Summary is one row of the conversation list.
type Summary struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Project  string    `json:"project,omitempty"`
	Bot      string    `json:"bot,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Messages int       `json:"messages"`
	Pending  int       `json:"pending"`
	CostUSD  float64   `json:"cost_usd"`
}

// line is one record of a conversation file: its meta (first), a message,
// or the outcome of a proposal.
type line struct {
	Type    string   `json:"type"` // meta | message | outcome
	Meta    *meta    `json:"meta,omitempty"`
	Message *Message `json:"message,omitempty"`
	Outcome *Outcome `json:"outcome,omitempty"`
}

type meta struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Created time.Time `json:"created"`
	Project string    `json:"project,omitempty"`
	Bot     string    `json:"bot,omitempty"`
}

// Outcome records what the user did with one proposal.
type Outcome struct {
	Message  int       `json:"message"`  // index in Messages
	Proposal int       `json:"proposal"` // index in that message's proposals
	Status   string    `json:"status"`   // applied | skipped | failed
	Note     string    `json:"note,omitempty"`
	Time     time.Time `json:"time"`
}

var convIDRE = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

func newID(now time.Time) string { return now.UTC().Format("20060102-150405") + "-" + randHex(3) }

func (s *Service) convPath(id string) (string, error) {
	if !convIDRE.MatchString(id) {
		return "", fmt.Errorf("%w: no conversation %q", ErrNotFound, id)
	}
	return filepath.Join(s.Dir, id+".jsonl"), nil
}

func (s *Service) appendLine(id string, l line) error {
	p, err := s.convPath(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Conversation reads one conversation.
func (s *Service) Conversation(id string) (*Conversation, error) {
	p, err := s.convPath(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: no conversation %q", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Conversation{ID: id, Messages: []Message{}}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for sc.Scan() {
		var l line
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue // a line cut short by a crash
		}
		switch {
		case l.Type == "meta" && l.Meta != nil:
			c.Title, c.Created, c.Project, c.Bot = l.Meta.Title, l.Meta.Created, l.Meta.Project, l.Meta.Bot
			c.Updated = c.Created
		case l.Type == "message" && l.Message != nil:
			c.Messages = append(c.Messages, *l.Message)
			c.Updated = l.Message.Time
		case l.Type == "outcome" && l.Outcome != nil:
			o := l.Outcome
			if o.Message >= 0 && o.Message < len(c.Messages) && o.Proposal >= 0 && o.Proposal < len(c.Messages[o.Message].Proposals) {
				pr := &c.Messages[o.Message].Proposals[o.Proposal]
				pr.Status, pr.Note = o.Status, o.Note
			}
		}
	}
	return c, sc.Err()
}

// List returns every conversation, newest first.
func (s *Service) List() ([]Summary, error) {
	files, _ := filepath.Glob(filepath.Join(s.Dir, "*.jsonl"))
	out := []Summary{}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		c, err := s.Conversation(id)
		if err != nil {
			continue
		}
		sum := Summary{ID: c.ID, Title: c.Title, Project: c.Project, Bot: c.Bot, Created: c.Created, Updated: c.Updated, Messages: len(c.Messages)}
		for _, m := range c.Messages {
			if m.Usage != nil {
				sum.CostUSD += m.Usage.CostUSD
			}
			for _, p := range m.Proposals {
				if p.Valid && p.Status == StatusPending {
					sum.Pending++
				}
			}
		}
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Delete removes a conversation.
func (s *Service) Delete(id string) error {
	p, err := s.convPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: no conversation %q", ErrNotFound, id)
	} else if err != nil {
		return err
	}
	return nil
}

// Record saves what the user did with a proposal: applied, skipped or
// failed. It never applies anything itself.
func (s *Service) Record(id string, o Outcome) (*Conversation, error) {
	switch o.Status {
	case StatusApplied, StatusSkipped, StatusFailed:
	default:
		return nil, fmt.Errorf("%w: status must be applied, skipped or failed", ErrBadRequest)
	}
	c, err := s.Conversation(id)
	if err != nil {
		return nil, err
	}
	if o.Message < 0 || o.Message >= len(c.Messages) || o.Proposal < 0 || o.Proposal >= len(c.Messages[o.Message].Proposals) {
		return nil, fmt.Errorf("%w: no such proposal", ErrNotFound)
	}
	if len(o.Note) > 500 {
		o.Note = o.Note[:500]
	}
	o.Time = s.now().UTC()
	if err := s.appendLine(id, line{Type: "outcome", Outcome: &o}); err != nil {
		return nil, err
	}
	pr := &c.Messages[o.Message].Proposals[o.Proposal]
	pr.Status, pr.Note = o.Status, o.Note
	return c, nil
}
