package nextup

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/usage"
)

// Context is what scoring needs besides the candidates.
type Context struct {
	Now       time.Time
	Settings  Settings
	Dismissed map[string]Dismissal
	Usage     *usage.Summary
}

// Points. Every term is shown in the breakdown with its reason.
const (
	ptsCIFailing      = 40
	ptsPRFailing      = 35
	ptsWaiting        = 32
	ptsFailed         = 30
	ptsOverdue        = 30 // plus 2 a day, up to 20 more
	ptsDueToday       = 28
	ptsDueSoon        = 20 // within 3 days
	ptsDueWeek        = 10 // within 7 days
	ptsPRReview       = 18
	ptsPRDraft        = 10
	ptsBriefDraft     = 14
	ptsBriefReady     = 6
	ptsApprovedNoCard = 12
	ptsCouncilFailed  = 8
	ptsDoing          = 12
	ptsUnblockEach    = 15 // per project unblocked, up to 3
	ptsFeedEach       = 5  // per live project it builds into, up to 3
	ptsStaleMax       = 10 // one a day after the first, up to 10
	ptsFocus          = 25
	ptsPinned         = 12
	ptsSmall          = 6
	ptsLarge          = -6
	ptsBlocked        = -25
	ptsHeadroomFull   = -30 // a window at 90 % or more
	ptsHeadroomLow    = -15 // a window at 75 % or more
	ptsSetAsideEach   = -5  // per item set aside on the same project in 30 days, up to 3
)

var linearPoints = map[int]int{1: 30, 2: 18, 3: 8}

// ScoreAll scores every candidate and sorts them, best first; ties go by id.
func ScoreAll(cs []Candidate, cx Context) {
	asides := setAsideByProject(cx)
	for i := range cs {
		Score(&cs[i], cx, asides)
	}
	sort.SliceStable(cs, func(a, b int) bool { return less(cs[a], cs[b]) })
}

// setAsideByProject counts the items set aside in the last 30 days per project.
func setAsideByProject(cx Context) map[string]int {
	out := map[string]int{}
	for _, d := range cx.Dismissed {
		if d.Project != "" && cx.Now.Sub(d.At) <= 30*24*time.Hour {
			out[d.Project]++
		}
	}
	return out
}

// Score computes one candidate's parts and total.
func Score(c *Candidate, cx Context, asides map[string]int) {
	var parts []Part
	add := func(factor string, pts int, why string) {
		if pts != 0 {
			parts = append(parts, Part{Factor: factor, Points: pts, Why: why})
		}
	}
	s := c.sig

	// Urgency.
	switch {
	case s.ciFailing:
		add("urgency", ptsCIFailing, "CI is failing on the default branch")
	case s.prFailing:
		add("urgency", ptsPRFailing, "the pull request's checks are failing")
	case s.waiting:
		add("urgency", ptsWaiting, "an agent is waiting for your reply")
	case s.failed:
		add("urgency", ptsFailed, "the session failed")
	case s.prReview:
		add("urgency", ptsPRReview, "the pull request waits for a review")
	case s.prDraft:
		add("urgency", ptsPRDraft, "a draft pull request is open")
	case s.briefDraft:
		add("urgency", ptsBriefDraft, "a brief waits for your approval")
		if s.briefReady {
			add("urgency", ptsBriefReady, "no blockers are open: it is ready")
		}
	case s.approvedNoCard:
		add("urgency", ptsApprovedNoCard, "an approved brief has no card yet")
	case s.councilFailed:
		add("urgency", ptsCouncilFailed, "the council did not finish")
	}
	if p, ok := linearPoints[s.linearPriority]; ok {
		add("urgency", p, fmt.Sprintf("Linear priority %d", s.linearPriority))
	}
	if days, ok := dueIn(c.Due, cx.Now); ok {
		switch {
		case days < 0:
			extra := min(2*-days, 20)
			add("urgency", ptsOverdue+extra, fmt.Sprintf("overdue by %s", dayCount(-days)))
		case days == 0:
			add("urgency", ptsDueToday, "due today")
		case days <= 3:
			add("urgency", ptsDueSoon, fmt.Sprintf("due in %s", dayCount(days)))
		case days <= 7:
			add("urgency", ptsDueWeek, fmt.Sprintf("due in %s", dayCount(days)))
		}
	}
	if s.doing {
		add("urgency", ptsDoing, "already in progress: finish what you started")
	}

	// Unblocking value.
	if n := len(s.unblocks); n > 0 {
		add("unblocking", ptsUnblockEach*min(n, 3), "unblocks "+names(s.unblocks))
	}
	if n := len(s.feeds); n > 0 {
		add("unblocking", ptsFeedEach*min(n, 3), "builds into "+names(s.feeds))
	}

	// Staleness.
	if !c.Updated.IsZero() {
		days := int(cx.Now.Sub(c.Updated).Hours() / 24)
		if days >= 2 {
			add("staleness", min(days-1, ptsStaleMax), fmt.Sprintf("untouched for %s", dayCount(days)))
		}
	}

	// Focus.
	if c.Project != "" {
		if c.Project == cx.Settings.FocusProject {
			add("focus", ptsFocus, "your focus project")
		} else if contains(cx.Settings.Pinned, c.Project) {
			add("focus", ptsPinned, "a pinned project")
		}
	}

	// Effort hints, from labels.
	switch {
	case hasLabel(c.Labels, "small", "quick", "easy", "effort:s", "effort:xs"):
		add("effort", ptsSmall, "labelled small")
	case hasLabel(c.Labels, "large", "big", "effort:l", "effort:xl"):
		add("effort", ptsLarge, "labelled large")
	}

	if s.blocked {
		add("blocked", ptsBlocked, "blocked: something else has to happen first")
	}

	// Usage headroom: only work that would spend is held back.
	if c.Action.Spends && s.provider != "" {
		if pct, label, ok := tightestWindow(cx.Usage, s.provider, cx.Now); ok {
			switch {
			case pct >= 90:
				add("headroom", ptsHeadroomFull, fmt.Sprintf("%s's %s is at %.0f %%", s.provider, label, pct))
			case pct >= 75:
				add("headroom", ptsHeadroomLow, fmt.Sprintf("%s's %s is at %.0f %%", s.provider, label, pct))
			}
		}
	}

	if n := asides[c.Project]; n > 0 && c.Project != "" {
		add("set_aside", ptsSetAsideEach*min(n, 3), fmt.Sprintf("you set aside %s on this project lately", itemCount(n)))
	}

	total := 0
	for _, p := range parts {
		total += p.Points
	}
	if parts == nil {
		parts = []Part{}
	}
	c.Parts, c.Score = parts, total
}

// dueIn is how many whole days from today's date a due date is.
func dueIn(due string, now time.Time) (int, bool) {
	due = strings.TrimSpace(due)
	if due == "" {
		return 0, false
	}
	loc := now.Location()
	var t time.Time
	var err error
	if len(due) == len("2006-01-02") {
		t, err = time.ParseInLocation("2006-01-02", due, loc)
	} else {
		t, err = time.Parse(time.RFC3339, due)
		t = t.In(loc)
	}
	if err != nil {
		return 0, false
	}
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, loc) }
	return int(math.Round(day(t).Sub(day(now)).Hours() / 24)), true
}

// tightestWindow is a provider's most used rate-limit window that has not
// reset since it was read.
func tightestWindow(sum *usage.Summary, provider string, now time.Time) (float64, string, bool) {
	if sum == nil {
		return 0, "", false
	}
	best, label, ok := 0.0, "", false
	for _, p := range sum.Providers {
		if p.ID != provider {
			continue
		}
		for _, w := range p.Windows {
			if w.UsedPercent == nil || w.Stale || (w.ResetsAt != nil && w.ResetsAt.Before(now)) {
				continue
			}
			if !ok || *w.UsedPercent > best {
				best, ok = *w.UsedPercent, true
				label = w.Label
				if label == "" {
					label = w.Name + " window"
				}
			}
		}
	}
	return best, label, ok
}

func names(ns []string) string {
	if len(ns) > 3 {
		return strings.Join(ns[:3], ", ") + fmt.Sprintf(" and %d more", len(ns)-3)
	}
	return strings.Join(ns, ", ")
}

func dayCount(n int) string {
	if n == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", n)
}

func itemCount(n int) string {
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
