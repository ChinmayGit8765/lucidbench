package council

import (
	"errors"
	"strings"
	"testing"
)

func TestApproveOverABlocker(t *testing.T) {
	fakes(t, map[string][]string{
		"claude": {brief("Draft"), brief("Revision one"), brief("Revision two")},
		"codex":  {block, block},
		"grok":   {ok, concern},
	})
	s, v := newService(t)
	sess := start(t, s, StartRequest{Input: dump})
	if len(sess.Rounds) != 2 || len(LatestBlockers(sess)) == 0 {
		t.Fatalf("setup: rounds %d, blockers %v", len(sess.Rounds), LatestBlockers(sess))
	}

	// Without saying so, a brief with an open blocker is refused and stays a draft.
	if _, err := s.ApproveChecked(sess.ID, "demo-app", false); !errors.Is(err, ErrBlockers) {
		t.Fatalf("approve over a blocker: %v", err)
	}
	if got, _ := s.Get(sess.ID); got.Status != StatusDraft || got.ApprovedWithBlockers {
		t.Errorf("a refused approval changed the session: %s %v", got.Status, got.ApprovedWithBlockers)
	}
	if pg, _ := v.Read(sess.BriefPath); pg.Front["status"] == "approved" {
		t.Error("the page was approved anyway")
	}

	// Approve anyway: the card is made and the session says so.
	if _, err := s.ApproveChecked(sess.ID, "demo-app", true); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(sess.ID)
	if got.Status != StatusApproved || !got.ApprovedWithBlockers {
		t.Errorf("status %s approved_with_blockers %v", got.Status, got.ApprovedWithBlockers)
	}
	if last := got.Log[len(got.Log)-1]; !strings.Contains(last.Text, "blocker") {
		t.Errorf("log: %+v", last)
	}
}

func TestApproveWithoutBlockersIsNotFlagged(t *testing.T) {
	fakes(t, map[string][]string{"claude": {brief("Draft")}, "codex": {ok}, "grok": {ok}})
	s, _ := newService(t)
	sess := start(t, s, StartRequest{Input: dump})
	// Saying "anyway" when nothing is open does not mark the session.
	if _, err := s.ApproveChecked(sess.ID, "demo-app", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(sess.ID); got.ApprovedWithBlockers {
		t.Error("flagged with no blocker")
	}
}
