package work

import (
	"errors"
	"testing"
	"time"
)

// A merged PR ends a waiting session: nothing waits for a follow-up any more,
// the timeline closes, and the worktree comes off without a discard.
func TestMergedPREndsAWaitingSession(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	if _, err := f.svc.OpenPR(se.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{State: PROpen}, nil }
	if got := f.svc.RefreshPR(se.ID); got.Status != StatusWaiting {
		t.Fatalf("an open PR ended the session: %s", got.Status)
	}
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{State: PRMerged}, nil }
	got := f.svc.RefreshPR(se.ID)
	if got.Status != StatusMerged || got.Ended == nil || !got.CardDone {
		t.Fatalf("after the merge: %+v", got)
	}
	if _, err := f.svc.FollowUp(se.ID, "one more thing"); !errors.Is(err, ErrConflict) {
		t.Errorf("follow-up on a merged session: %v", err)
	}
	if _, err := f.svc.End(se.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("end on a merged session: %v", err)
	}
	if _, open, _, _ := f.svc.Events(se.ID, 0); open {
		t.Error("the timeline of a merged session is still open")
	}
	// Saved: a restart finds it merged.
	if again, _ := New(f.svc.Dir, nil, nil, nil).Get(se.ID); again.Status != StatusMerged {
		t.Errorf("after a restart: %s", again.Status)
	}
	// Its commits landed with the PR: Remove needs no discard.
	removed, err := f.svc.Remove(se.ID, false)
	if err != nil || !removed.Removed || removed.Status != StatusMerged {
		t.Fatalf("remove: %+v %v", removed, err)
	}
}

// A closed PR ends a waiting session as closed; reopened, it waits again.
func TestClosedPREndsAndReopenRevives(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	if _, err := f.svc.OpenPR(se.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{State: PRClosed}, nil }
	if got := f.svc.RefreshPR(se.ID); got.Status != StatusClosed || got.CardDone {
		t.Fatalf("after closing: %+v", got)
	}
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{State: PROpen}, nil }
	if got := f.svc.RefreshPR(se.ID); got.Status != StatusWaiting {
		t.Fatalf("after reopening: %s", got.Status)
	}
}

// A merge seen while a turn runs lets the turn finish, then ends the session.
func TestMergeSeenWhileATurnRuns(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	installFakes(t, "slow")
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "take your time", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		evs, _, _, _ := f.svc.Events(se.ID, 0)
		if len(evs) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	e, _ := f.svc.get(se.ID)
	f.svc.update(e, func(x *Session) { x.PR = "https://github.com/example/demo/pull/7" })
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{State: PRMerged}, nil }
	if got := f.svc.RefreshPR(se.ID); got.Status != StatusRunning || got.PRState != PRMerged {
		t.Fatalf("the merge cut the running turn short: %s %s", got.Status, got.PRState)
	}
	got, err := f.svc.Stop(se.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusMerged || got.Turns[0].Status != StatusStopped {
		t.Errorf("after the turn: %s %+v", got.Status, got.Turns)
	}
}

// A session saved waiting on a PR that had already merged ends on load.
func TestLoadEndsWaitingOnAMergedPR(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	turns := []Turn{{N: 1, Mode: TurnFirst, Status: StatusDone, Started: now, Ended: &now}}
	stale := Session{ID: "a1b2c3d4", Status: StatusWaiting, Started: now, Ended: &now, PR: "https://github.com/example/demo/pull/7", PRState: PRMerged, Turns: turns}
	open := Session{ID: "d4c3b2a1", Status: StatusWaiting, Started: now, Ended: &now, PR: "https://github.com/example/demo/pull/8", PRState: PROpen, Turns: turns}
	for _, se := range []*Session{&stale, &open} {
		if err := (&Service{Dir: dir}).save(se); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(dir, nil, nil, nil)
	if got, _ := svc.Get(stale.ID); got.Status != StatusMerged {
		t.Errorf("stale: %s", got.Status)
	}
	if got, _ := svc.Get(open.ID); got.Status != StatusWaiting {
		t.Errorf("open: %s", got.Status)
	}
	if got, _ := New(dir, nil, nil, nil).Get(stale.ID); got.Status != StatusMerged {
		t.Errorf("not saved: %s", got.Status)
	}
}
