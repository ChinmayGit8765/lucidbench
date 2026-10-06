package work

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// attachFake is a Browser func that records what it was asked and what was
// let go.
type attachFake struct {
	mu       sync.Mutex
	sessions []string
	released int
	err      error
}

func (a *attachFake) attach(_ context.Context, session string) (string, func(), error) {
	if a.err != nil {
		return "", nil, a.err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions = append(a.sessions, session)
	return "ws://127.0.0.1:49555/devtools/browser/abc", func() {
		a.mu.Lock()
		a.released++
		a.mu.Unlock()
	}, nil
}

func (a *attachFake) counts() (attached, released int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sessions), a.released
}

// waitReleased waits up to d for n releases.
func (a *attachFake) waitReleased(n int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for {
		_, r := a.counts()
		if r >= n || time.Now().After(deadline) {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBrowserAttachedToASession(t *testing.T) {
	f := newFixture(t, newRepo(t, false))
	claudeLog, _ := installFakes(t, "edit")
	a := &attachFake{}
	f.svc.Browser = a.attach
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Check the page", Provider: "claude", Harness: "clean", Browser: true})
	if err != nil {
		t.Fatal(err)
	}
	if !se.Browser || !strings.Contains(se.Prompt, "## Browser") || !strings.Contains(se.Prompt, "LUCID_BROWSER_CDP") || !strings.Contains(se.Prompt, "host.docker.internal") {
		t.Errorf("session = browser %v, prompt %q", se.Browser, se.Prompt)
	}
	se = wait(t, f.svc, se.ID)
	if se.Status != StatusWaiting {
		t.Fatalf("%+v", se)
	}
	data, _ := os.ReadFile(claudeLog)
	var call struct{ Cdp, Tmp string }
	_ = json.Unmarshal(data, &call)
	if call.Cdp != "ws://127.0.0.1:49555/devtools/browser/abc" || call.Tmp == "" {
		t.Errorf("the CLI saw %+v", call)
	}
	if !strings.Contains(string(data), "## Browser") {
		t.Errorf("the prompt the CLI got has no browser note: %s", data)
	}
	if len(a.sessions) != 1 || a.sessions[0] != se.ID {
		t.Errorf("attached for %v, want the session %s", a.sessions, se.ID)
	}
	// The waiting session keeps the browser; ending it lets go at once.
	if _, r := a.counts(); r != 0 {
		t.Errorf("released %d times while waiting", r)
	}
	if _, err := f.svc.End(se.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.waitReleased(1, 5*time.Second); r != 1 {
		t.Errorf("released %d times", r)
	}
	time.Sleep(50 * time.Millisecond)
	if _, r := a.counts(); r != 1 {
		t.Errorf("released %d times after the end", r)
	}
}

// A waiting session lets go of the browser after the idle time; a follow-up
// attaches it again, with the address the CLI gets.
func TestBrowserHoldReleasedAfterIdle(t *testing.T) {
	f := newFixture(t, newRepo(t, false))
	claudeLog, _ := installFakes(t, "edit")
	a := &attachFake{}
	f.svc.Browser = a.attach
	f.svc.HoldIdle = 400 * time.Millisecond
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Check the page", Provider: "claude", Harness: "clean", Browser: true})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	if _, r := a.counts(); se.Status != StatusWaiting || r != 0 {
		t.Fatalf("%s, released %d at once", se.Status, r)
	}
	if r := a.waitReleased(1, 5*time.Second); r != 1 {
		t.Fatalf("released %d times after the idle time", r)
	}
	if _, err := f.svc.FollowUp(se.ID, "Look again"); err != nil {
		t.Fatal(err)
	}
	got := wait(t, f.svc, se.ID)
	if n, _ := a.counts(); n != 2 || got.Status != StatusWaiting {
		t.Errorf("attached %d times, status %s", n, got.Status)
	}
	data, _ := os.ReadFile(claudeLog)
	var call struct{ Cdp, Stdin string }
	_ = json.Unmarshal(data, &call)
	if call.Cdp == "" || !strings.Contains(call.Stdin, BrowserEnv) {
		t.Errorf("turn 2 saw %+v", call)
	}
	// Held again through the second wait, then let go once more.
	if r := a.waitReleased(2, 5*time.Second); r != 2 {
		t.Errorf("released %d times after the second wait", r)
	}
	if _, err := f.svc.End(se.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, r := a.counts(); r != 2 {
		t.Errorf("released %d times; End must not let go twice", r)
	}
}

// A follow-up that starts before the idle time ends keeps the hold through
// the turn, and does not let go of the old one early.
func TestBrowserHoldKeptThroughAFollowUp(t *testing.T) {
	f := newFixture(t, newRepo(t, false))
	installFakes(t, "edit")
	a := &attachFake{}
	f.svc.Browser = a.attach
	f.svc.HoldIdle = time.Hour
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Check the page", Provider: "claude", Harness: "clean", Browser: true})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, f.svc, se.ID)
	if _, err := f.svc.FollowUp(se.ID, "Look again"); err != nil {
		t.Fatal(err)
	}
	wait(t, f.svc, se.ID)
	// Re-attached for the new turn, the first hold let go: one still held.
	if n, r := a.counts(); n != 2 || r != 1 {
		t.Errorf("attached %d, released %d", n, r)
	}
	if _, err := f.svc.End(se.ID); err != nil {
		t.Fatal(err)
	}
	if r := a.waitReleased(2, 5*time.Second); r != 2 {
		t.Errorf("released %d after the end", r)
	}
}

func TestSessionWithoutABrowserGetsNone(t *testing.T) {
	f := newFixture(t, newRepo(t, false))
	claudeLog, _ := installFakes(t, "edit")
	a := &attachFake{}
	f.svc.Browser = a.attach
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "Say hello", Provider: "claude", Harness: "clean"})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	data, _ := os.ReadFile(claudeLog)
	var call struct{ Cdp string }
	_ = json.Unmarshal(data, &call)
	if se.Browser || call.Cdp != "" || len(a.sessions) != 0 || strings.Contains(se.Prompt, "## Browser") {
		t.Errorf("browser %v, cdp %q, attached %v", se.Browser, call.Cdp, a.sessions)
	}
}

func TestBrowserRefusals(t *testing.T) {
	f := newFixture(t, newRepo(t, false))
	installFakes(t, "edit")
	req := StartRequest{Project: "demo", Prompt: "x", Provider: "claude", Harness: "clean", Browser: true}
	if _, err := f.svc.Start(req); !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), "not available") {
		t.Errorf("no extension: %v", err)
	}
	a := &attachFake{err: errors.New("docker is not running")}
	f.svc.Browser = a.attach
	if _, err := f.svc.Start(req); !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), "docker is not running") {
		t.Errorf("browser failed to start: %v", err)
	}
	if len(f.svc.List()) != 0 {
		t.Errorf("a session was made without its browser: %+v", f.svc.List())
	}
}

func TestAddEventNeedsARunningSession(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	installFakes(t, "slow")
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "take your time", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	ev := agentexec.Event{Time: time.Now().UTC(), Kind: "image", Title: "Browser screenshot", Body: "/api/browser/shots/0123456789abcdef.png"}
	if err := f.svc.AddEvent(se.ID, ev); err != nil {
		t.Fatal(err)
	}
	evs, _, _, _ := f.svc.Events(se.ID, 0)
	found := false
	for _, e := range evs {
		found = found || (e.Kind == "image" && e.Body == ev.Body)
	}
	if !found {
		t.Errorf("events = %+v", evs)
	}
	if _, err := f.svc.Stop(se.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AddEvent(se.ID, ev); !errors.Is(err, ErrConflict) {
		t.Errorf("after stop: %v", err)
	}
	if err := f.svc.AddEvent("nosuchsession", ev); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: %v", err)
	}
}
