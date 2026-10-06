package work

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

// PR states a session's pull request can be in.
const (
	PRDraft  = "draft"
	PROpen   = "open"
	PRMerged = "merged"
	PRClosed = "closed"
)

// prFresh is how long a fetched PR state is trusted before gh is asked again.
const prFresh = 60 * time.Second

// PRChecks counts a PR's status checks.
type PRChecks struct {
	Passing int `json:"passing"`
	Failing int `json:"failing"`
	Pending int `json:"pending"`
}

// PRInfo is what gh says about a pull request.
type PRInfo struct {
	State  string // one of the PR* constants
	Checks PRChecks
}

// ghPR is the JSON of `gh pr view --json state,isDraft,mergedAt,statusCheckRollup`.
type ghPR struct {
	State             string  `json:"state"`
	IsDraft           bool    `json:"isDraft"`
	MergedAt          *string `json:"mergedAt"`
	StatusCheckRollup []struct {
		Type       string `json:"__typename"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		State      string `json:"state"` // a commit status, not a check run
	} `json:"statusCheckRollup"`
}

// parsePR reads gh's answer.
func parsePR(data []byte) (PRInfo, error) {
	var g ghPR
	if err := json.Unmarshal(data, &g); err != nil {
		return PRInfo{}, err
	}
	var info PRInfo
	switch {
	case strings.EqualFold(g.State, "MERGED") || (g.MergedAt != nil && *g.MergedAt != ""):
		info.State = PRMerged
	case strings.EqualFold(g.State, "CLOSED"):
		info.State = PRClosed
	case g.IsDraft:
		info.State = PRDraft
	default:
		info.State = PROpen
	}
	for _, c := range g.StatusCheckRollup {
		state := strings.ToUpper(c.State)
		if c.Type == "StatusContext" || (c.Status == "" && state != "") {
			switch state {
			case "SUCCESS":
				info.Checks.Passing++
			case "FAILURE", "ERROR":
				info.Checks.Failing++
			default:
				info.Checks.Pending++
			}
			continue
		}
		if !strings.EqualFold(c.Status, "COMPLETED") {
			info.Checks.Pending++
			continue
		}
		switch strings.ToUpper(c.Conclusion) {
		case "SUCCESS", "NEUTRAL", "SKIPPED":
			info.Checks.Passing++
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			info.Checks.Failing++
		default:
			info.Checks.Pending++
		}
	}
	return info, nil
}

// ghPRView asks gh for a PR's state and checks. A missing gh or a failed call
// is an error the caller tolerates.
func ghPRView(dir, url string) (PRInfo, error) {
	if _, err := lookPath("gh"); err != nil {
		return PRInfo{}, err
	}
	out, err := runIn(dir, "gh", "pr", "view", url, "--json", "state,isDraft,mergedAt,statusCheckRollup")
	if err != nil {
		return PRInfo{}, err
	}
	return parsePR([]byte(out))
}

// Summary is the session without its bulk: the prompt (which carries the whole
// brief), the answer and the diff patches. The list uses it so polling it
// stays small; the full record is GET /api/work/sessions/{id}.
func (se Session) Summary() Session {
	se.Prompt, se.Answer = "", ""
	if se.Diff != nil {
		d := *se.Diff
		d.Files = make([]FileChange, len(se.Diff.Files))
		for i, f := range se.Diff.Files {
			f.Patch, f.Cut = "", false
			d.Files[i] = f
		}
		se.Diff = &d
	}
	return se
}

// PollPRs starts a background refresh of every session whose PR state is
// older than a minute. It returns at once: the list keeps showing the last
// state it has, and the next poll shows the new one. Merged is final, so a
// merged PR is not asked about again.
func (s *Service) PollPRs() {
	s.mu.Lock()
	s.load()
	es := make([]*entry, 0, len(s.sessions))
	for _, e := range s.sessions {
		es = append(es, e)
	}
	s.mu.Unlock()
	for _, e := range es {
		e.mu.Lock()
		due := e.s.PR != "" && e.s.PRState != PRMerged && !e.polling && time.Since(e.s.PRChecked) > prFresh
		if due {
			e.polling = true
		}
		id := e.s.ID
		e.mu.Unlock()
		if due {
			go func() {
				s.RefreshPR(id)
				e.mu.Lock()
				e.polling = false
				e.mu.Unlock()
			}()
		}
	}
}

// RefreshPR reads the session's PR from GitHub now and stores its state and
// checks. When the PR has been merged it moves the card to Done, once.
func (s *Service) RefreshPR(id string) Session {
	e, err := s.get(id)
	if err != nil {
		return Session{}
	}
	e.mu.Lock()
	url, repo := e.s.PR, e.s.RepoPath
	e.mu.Unlock()
	if url == "" {
		return s.mustGet(id)
	}
	view := s.PRView
	if view == nil {
		view = ghPRView
	}
	info, err := view(repo, url)
	var board, card string
	s.update(e, func(x *Session) {
		// Whatever the answer, wait a minute before asking again.
		x.PRChecked = time.Now().UTC()
		if err != nil {
			return
		}
		x.PRState, x.PRChecks = info.State, &info.Checks
		if info.State == PRMerged && !x.CardDone {
			x.CardDone = true
			board, card = x.Board, x.Card
		}
	})
	if card != "" {
		log.Printf("work: PR %s was merged: moving card %s on %s to Done", url, card, board)
		s.moveCard(board, card, ColumnDone, url, true)
	}
	return s.mustGet(id)
}
