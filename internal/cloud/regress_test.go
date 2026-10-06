package cloud

import (
	"context"
	"strings"
	"testing"
)

// A browser that closes the page cancels its request; the cached inventory
// must still be the real answer, not "context canceled".
func TestCancelledRequestDoesNotPoisonCache(t *testing.T) {
	f := vercelFake()
	s := svc(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sum, _ := s.Get(ctx, "vercel", false)
	if sum.State != StateConnected {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestWranglerAccountNameHidesEmail(t *testing.T) {
	f := wranglerFake()
	f.replies["wrangler whoami --json"] = reply{out: `{"loggedIn":true,"accounts":[{"id":"a1","name":"dev@example.com's Account"}]}`}
	sum := get(t, svc(f), "wrangler")
	if strings.Contains(sum.Account, "@") || sum.State != StateConnected {
		t.Errorf("account = %q", sum.Account)
	}
}

func TestScrubDropsToolNoiseAndJSON(t *testing.T) {
	got := scrub("<claude-code-hint v=\"1\" type=\"plugin\" />\nFetching projects in team\nError: boom")
	if got != "Error: boom" {
		t.Errorf("scrub = %q", got)
	}
	f := &fake{replies: map[string]reply{"vercel whoami --format json": {out: `[{"x":1}]`, fail: true}}}
	if sum := get(t, svc(f), "vercel"); strings.Contains(sum.Message, "[{") {
		t.Errorf("stdout JSON leaked into the message: %q", sum.Message)
	}
}
