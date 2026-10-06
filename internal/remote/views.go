package remote

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/ci"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/usage"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// Redacted replaces every title and name of a confidential project.
const Redacted = "confidential"

// Work is the part of the Work service the remote uses.
type Work interface {
	List() []work.Session
	Get(id string) (work.Session, error)
	Events(id string, from int) ([]agentexec.Event, bool, <-chan struct{}, error)
	Stop(id string) (work.Session, error)
	FollowUp(id, prompt string) (work.Session, error)
}

// Council is the part of the council service the remote uses.
type Council interface {
	List() ([]council.Summary, error)
	Get(id string) (*council.Session, error)
	ApproveChecked(id, project string, anyway bool) (*boards.Card, error)
	BeginAgain(id, notes string) (*council.Session, error)
}

// Services are what the remote surface reads and acts on. A nil field
// leaves its part out of the summary.
type Services struct {
	Work     Work
	Council  Council
	CI       func(ctx context.Context) ci.Summary
	Usage    func() *usage.Summary
	Projects func() (*projects.List, error)
	Vault    memory.Opener
}

// privacy answers "is this confidential?" for one request. It fails closed:
// when projects.yaml cannot be read, every session with a project is
// treated as confidential.
type privacy struct {
	list  *projects.List
	err   error
	vault *memory.Vault
}

func (s Services) privacy() *privacy {
	p := &privacy{}
	if s.Projects != nil {
		p.list, p.err = s.Projects()
	} else {
		p.err = errors.New("no project list")
	}
	if s.Vault != nil {
		p.vault, _ = s.Vault()
	}
	return p
}

func (p *privacy) project(id string) bool {
	if id == "" {
		return false
	}
	if p.err != nil || p.list == nil {
		return true
	}
	// A project no longer in projects.yaml cannot be checked, so it counts
	// as confidential too.
	pr := p.list.Find(id)
	return pr == nil || pr.Visibility == "confidential"
}

// page reports whether a Memory page is confidential; a page that cannot be
// checked counts as confidential.
func (p *privacy) page(path string) bool {
	if path == "" {
		return false
	}
	if p.vault == nil {
		return true
	}
	c, err := p.vault.IsConfidential(path)
	return err != nil || c
}

// repo reports whether a CI repository (owner/name) belongs to a
// confidential project. Without a project list every repo counts.
func (p *privacy) repo(r string) bool {
	if p.err != nil || p.list == nil {
		return true
	}
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.TrimPrefix(s, "https://")
		s = strings.TrimPrefix(s, "github.com/")
		return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	}
	want := norm(r)
	for _, pr := range p.list.Projects {
		if pr.Visibility == "confidential" && pr.Repo != "" && norm(pr.Repo) == want {
			return true
		}
	}
	return false
}

func (p *privacy) work(se work.Session) bool {
	return p.project(se.Project) || p.page(se.Brief)
}

func (p *privacy) council(project, brief string) bool {
	return p.project(project) || p.page(brief)
}

// WorkView is a Work session as the phone sees it: no prompt, answer,
// paths, branch or patches.
type WorkView struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Project      string     `json:"project"`
	Provider     string     `json:"provider"`
	Status       string     `json:"status"`
	Started      time.Time  `json:"started"`
	Ended        *time.Time `json:"ended,omitempty"`
	Events       int        `json:"events"`
	Turns        int        `json:"turns"`
	Files        int        `json:"files"`
	Added        int        `json:"added"`
	Deleted      int        `json:"deleted"`
	PRState      string     `json:"pr_state,omitempty"`
	Error        string     `json:"error,omitempty"`
	Confidential bool       `json:"confidential"`
}

func workView(se work.Session, conf bool) WorkView {
	v := WorkView{
		ID: se.ID, Title: se.Title, Project: se.Project, Provider: se.Provider, Status: se.Status,
		Started: se.Started, Ended: se.Ended, Events: se.Events, Turns: len(se.Turns), PRState: se.PRState, Error: se.Error,
	}
	if se.PR != "" && v.PRState == "" {
		v.PRState = "open"
	}
	if se.Diff != nil {
		v.Files, v.Added, v.Deleted = len(se.Diff.Files), se.Diff.Added, se.Diff.Deleted
	}
	if conf {
		v.Title, v.Project, v.Error, v.Confidential = Redacted, Redacted, "", true
	}
	return v
}

// EventView is one Work event: its kind, title and text only.
type EventView struct {
	Time  time.Time `json:"time"`
	Kind  string    `json:"kind"`
	Title string    `json:"title,omitempty"`
	Body  string    `json:"body,omitempty"`
}

func eventView(ev agentexec.Event) EventView {
	v := EventView{Time: ev.Time, Kind: ev.Kind, Title: ev.Title, Body: ev.Body}
	if ev.Kind == agentexec.KindImage {
		v.Body = "" // a screenshot's desktop API path; the phone cannot fetch it
	}
	return v
}

// CouncilItem is a council run waiting for approval, in the summary.
type CouncilItem struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Project      string    `json:"project,omitempty"`
	Created      time.Time `json:"created"`
	Confidential bool      `json:"confidential"`
}

// BriefView is a council brief as the phone sees it.
type BriefView struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Project      string    `json:"project,omitempty"`
	Status       string    `json:"status"`
	Stage        string    `json:"stage"`
	Brief        string    `json:"brief"`
	Blockers     []string  `json:"blockers"`
	Rounds       int       `json:"rounds"`
	Card         string    `json:"card,omitempty"`
	Created      time.Time `json:"created"`
	Confidential bool      `json:"confidential"`
}

// Attention is one "needs you" entry.
type Attention struct {
	Severity string `json:"severity"` // error | warning | info
	Kind     string `json:"kind"`     // brief | session | ci | usage
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Target   string `json:"target,omitempty"` // a council or Work session id
}

// CIView is the CI line of the summary.
type CIView struct {
	Configured   bool     `json:"configured"`
	Runs         int      `json:"runs_24h"`
	InProgress   int      `json:"in_progress"`
	PassRate     *float64 `json:"pass_rate"`
	FailingRepos []string `json:"failing_repos"`
}

// UsageView is today's usage.
type UsageView struct {
	Tokens  int64          `json:"tokens"`
	CostUSD float64        `json:"cost_usd"`
	Windows []WindowView   `json:"windows"`
	By      []ProviderView `json:"providers"`
}

// ProviderView is one provider's tokens today.
type ProviderView struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Tokens int64  `json:"tokens"`
}

// WindowView is a provider rate-limit window.
type WindowView struct {
	Provider    string  `json:"provider"`
	Label       string  `json:"label"`
	UsedPercent float64 `json:"used_percent"`
}

// Overview is GET /r/api/overview.
type Overview struct {
	Attention []Attention   `json:"attention"`
	Running   []WorkView    `json:"running"`
	Awaiting  []CouncilItem `json:"awaiting"`
	CI        *CIView       `json:"ci"`
	Usage     *UsageView    `json:"usage"`
	FollowUp  bool          `json:"follow_up"` // a waiting session takes a follow-up prompt (always true now)
	Generated time.Time     `json:"generated_at"`
}

var severityRank = map[string]int{"error": 0, "warning": 1, "info": 2}

func (s Services) overview(ctx context.Context) Overview {
	pv := s.privacy()
	out := Overview{Attention: []Attention{}, Running: []WorkView{}, Awaiting: []CouncilItem{}, FollowUp: true, Generated: time.Now().UTC()}
	if s.Work != nil {
		for _, se := range s.Work.List() {
			conf := pv.work(se)
			v := workView(se, conf)
			switch {
			case se.Status == work.StatusRunning:
				out.Running = append(out.Running, v)
			case se.Status == work.StatusWaiting && !se.Removed:
				out.Attention = append(out.Attention, Attention{Severity: "warning", Kind: "session", Title: v.Title, Detail: "The agent is waiting for your follow-up", Target: se.ID})
			case se.Status == work.StatusFailed && !se.Removed:
				out.Attention = append(out.Attention, Attention{Severity: "error", Kind: "session", Title: v.Title, Detail: "The session failed", Target: se.ID})
			case se.Status == work.StatusDone && !se.Removed && se.PR == "" && se.Diff != nil && len(se.Diff.Files) > 0:
				out.Attention = append(out.Attention, Attention{Severity: "info", Kind: "session", Title: v.Title, Detail: "Finished with a diff to review", Target: se.ID})
			}
		}
	}
	if s.Council != nil {
		if list, err := s.Council.List(); err == nil {
			for _, c := range list {
				if c.Status != council.StatusDraft {
					continue
				}
				conf := pv.council(c.Project, c.BriefPath)
				it := CouncilItem{ID: c.ID, Title: c.Title, Project: c.Project, Created: c.Created, Confidential: conf}
				if conf {
					it.Title, it.Project = Redacted, Redacted
				}
				out.Awaiting = append(out.Awaiting, it)
				out.Attention = append(out.Attention, Attention{Severity: "warning", Kind: "brief", Title: it.Title, Detail: "A brief waits for your approval", Target: c.ID})
			}
		}
	}
	if s.CI != nil {
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		sum := s.CI(cctx)
		cancel()
		v := &CIView{Configured: sum.Configured, Runs: sum.Runs24h.Total, InProgress: sum.Runs24h.InProgress, PassRate: sum.Runs24h.PassRate, FailingRepos: []string{}}
		for _, r := range sum.FailingRepos {
			if pv.repo(r) {
				r = Redacted
			}
			v.FailingRepos = append(v.FailingRepos, r)
		}
		for _, r := range v.FailingRepos {
			out.Attention = append(out.Attention, Attention{Severity: "error", Kind: "ci", Title: r, Detail: "The latest CI run failed"})
		}
		out.CI = v
	}
	if s.Usage != nil {
		if sum := s.Usage(); sum != nil {
			out.Usage = usageView(sum)
			for _, w := range out.Usage.Windows {
				if w.UsedPercent >= 80 {
					out.Attention = append(out.Attention, Attention{Severity: "warning", Kind: "usage", Title: w.Provider + " " + w.Label, Detail: "Usage window above 80 %"})
				}
			}
		}
	}
	sort.SliceStable(out.Attention, func(i, j int) bool {
		return severityRank[out.Attention[i].Severity] < severityRank[out.Attention[j].Severity]
	})
	return out
}

func usageView(sum *usage.Summary) *UsageView {
	v := &UsageView{Windows: []WindowView{}, By: []ProviderView{}}
	today := time.Now().Format("2006-01-02")
	for _, p := range sum.Providers {
		var tokens int64
		for _, d := range p.Daily {
			if d.Date == today {
				tokens += d.Total
			}
		}
		if tokens > 0 {
			v.By = append(v.By, ProviderView{ID: p.ID, Label: p.Label, Tokens: tokens})
		}
		v.Tokens += tokens
		for _, w := range p.Windows {
			if w.UsedPercent != nil && !w.Stale {
				v.Windows = append(v.Windows, WindowView{Provider: p.Label, Label: w.Label, UsedPercent: *w.UsedPercent})
			}
		}
	}
	for _, d := range sum.Lucidbench.Daily {
		if d.Date == today {
			v.CostUSD += d.CostUSD
		}
	}
	return v
}

func briefView(sess *council.Session, conf bool) BriefView {
	v := BriefView{
		ID: sess.ID, Title: sess.Title, Project: sess.Project, Status: sess.Status, Stage: sess.Stage,
		Brief: sess.Brief, Blockers: []string{}, Rounds: len(sess.Rounds), Created: sess.Created,
	}
	if sess.Card != nil {
		v.Card = sess.Card.ID
	}
	for _, p := range council.LatestBlockers(sess) {
		v.Blockers = append(v.Blockers, p.Text)
	}
	if conf {
		v.Title, v.Project, v.Brief, v.Blockers, v.Confidential = Redacted, Redacted, "", []string{}, true
	}
	return v
}
