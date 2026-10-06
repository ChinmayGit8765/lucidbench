package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

var ctx = context.Background()

func TestEndpointRewritesContainerAddressesAndListsPagesOnly(t *testing.T) {
	f := newFakeChrome(t)
	e := f.endpoint()
	v, err := e.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(f.srv.URL, "http://")
	if v.WSURL != "ws://"+host+"/devtools/browser/BR" || v.Browser != "Chrome/155.0.0.0" {
		t.Errorf("version = %+v", v)
	}
	ts, err := e.Targets(ctx)
	if err != nil || len(ts) != 1 || ts[0].ID != "TAB1" || ts[0].WSURL != "ws://"+host+"/devtools/page/TAB1" {
		t.Errorf("targets = %+v, %v", ts, err)
	}
	nt, err := e.NewTarget(ctx)
	if err != nil || nt.ID != "TAB2" || !strings.HasPrefix(nt.WSURL, "ws://"+host+"/") {
		t.Errorf("new target = %+v, %v", nt, err)
	}
	if err := e.CloseTarget(ctx, "TAB2"); err != nil {
		t.Error(err)
	}
	if err := e.CloseTarget(ctx, "../x"); err == nil {
		t.Error("a tab id with a slash was accepted")
	}
}

func TestNavigateOverCDP(t *testing.T) {
	f := newFakeChrome(t)
	s, _, _, _ := newUp(t, f)
	res, err := s.Navigate(ctx, NavRequest{URL: "https://example.com/a?b=1"})
	if err != nil || res.Target != "TAB1" || res.URL != "https://example.com/a?b=1" {
		t.Fatalf("navigate = %+v, %v", res, err)
	}
	c := f.find("Page.navigate")
	if len(c) != 1 || !strings.Contains(string(c[0].Params), `"url":"https://example.com/a?b=1"`) {
		t.Errorf("Page.navigate calls = %+v", c)
	}
	f.mu.Lock()
	f.navError = "net::ERR_NAME_NOT_RESOLVED"
	f.mu.Unlock()
	res, err = s.Navigate(ctx, NavRequest{URL: "https://nope.invalid/"})
	if err != nil || res.ErrorText != "net::ERR_NAME_NOT_RESOLVED" {
		t.Errorf("a page that did not load = %+v, %v", res, err)
	}
	// A new tab navigates the new tab, not the first.
	res, err = s.Navigate(ctx, NavRequest{URL: "https://example.org/", NewTab: true})
	if err != nil || res.Target != "TAB2" {
		t.Errorf("new tab = %+v, %v", res, err)
	}
	if _, err := s.Navigate(ctx, NavRequest{Action: ActionClose, Target: "TAB2"}); err != nil || len(f.closed) != 1 || f.closed[0] != "TAB2" {
		t.Errorf("close = %v, %v", f.closed, err)
	}
	if _, err := s.Navigate(ctx, NavRequest{Target: "GONE", URL: "https://example.com/"}); !errors.Is(err, ErrNoTab) {
		t.Errorf("unknown tab: %v", err)
	}
}

func TestBackForwardReload(t *testing.T) {
	f := newFakeChrome(t)
	s, _, _, _ := newUp(t, f)
	for _, a := range []string{ActionBack, ActionForward, ActionReload} {
		if _, err := s.Navigate(ctx, NavRequest{Action: a}); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
	}
	h := f.find("Page.navigateToHistoryEntry")
	// The fake's history is at index 1 of entries 10, 11, 12.
	if len(h) != 2 || !strings.Contains(string(h[0].Params), `"entryId":10`) || !strings.Contains(string(h[1].Params), `"entryId":12`) {
		t.Errorf("history calls = %+v", h)
	}
	if len(f.find("Page.reload")) != 1 {
		t.Errorf("methods = %v", f.methods())
	}
}

func TestOnlyHTTPAndHTTPSOpen(t *testing.T) {
	f := newFakeChrome(t)
	s, _, _, _ := newUp(t, f)
	for _, u := range []string{
		"file:///etc/passwd", "FILE:///C:/Windows/win.ini", "chrome://settings", "devtools://devtools/bundled/inspector.html",
		"javascript:alert(1)", "data:text/html,hi", "about:blank", "view-source:https://example.com", "ftp://example.com/",
		"example.com", "localhost:5173", "//example.com/", "", "  ", "http://", "https:///path",
	} {
		_, err := s.Navigate(ctx, NavRequest{URL: u})
		if !errors.Is(err, ErrURL) {
			t.Errorf("%q: err = %v, want a refusal", u, err)
		}
	}
	if m := f.methods(); len(m) != 0 {
		t.Errorf("a refused URL reached the browser: %v", m)
	}
	for in, want := range map[string]string{
		"https://example.com/x":            "https://example.com/x",
		"HTTP://Example.com":               "http://Example.com",
		"http://localhost:5173/":           "http://host.docker.internal:5173/",
		"http://127.0.0.1:8080/a?b=1#c":    "http://host.docker.internal:8080/a?b=1#c",
		"http://[::1]:3000/":               "http://host.docker.internal:3000/",
		"http://localhost/":                "http://host.docker.internal/",
		"http://app.localhost:80/":         "http://host.docker.internal:80/",
		"https://user:pw@example.com:8443": "https://user:pw@example.com:8443",
	} {
		got, err := CheckURL(in)
		if err != nil || got != want {
			t.Errorf("CheckURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestInputDispatch(t *testing.T) {
	f := newFakeChrome(t)
	s, _, _, _ := newUp(t, f)
	for _, in := range []Input{
		{Type: "move", X: 10, Y: 20},
		{Type: "click", X: 30, Y: 40},
		{Type: "click", X: 5, Y: 6, Button: "right"},
		{Type: "scroll", X: 1, Y: 2, DY: 120},
		{Type: "key", Key: "a", Code: "KeyA"},
		{Type: "key", Key: "a", Code: "KeyA", Modifiers: 2},
		{Type: "key", Key: "Enter", Code: "Enter"},
		{Type: "key", Key: "ArrowDown", Code: "ArrowDown", Action: "down"},
		{Type: "text", Text: "héllo"},
	} {
		if err := s.Input(ctx, "", in); err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
	}
	mouse := f.find("Input.dispatchMouseEvent")
	var got []string
	for _, c := range mouse {
		var p struct {
			Type   string  `json:"type"`
			X      float64 `json:"x"`
			Button string  `json:"button"`
			DeltaY float64 `json:"deltaY"`
		}
		_ = json.Unmarshal(c.Params, &p)
		got = append(got, p.Type+":"+p.Button)
		if p.Type == "mouseWheel" && p.DeltaY != 120 {
			t.Errorf("wheel delta = %v", p.DeltaY)
		}
	}
	want := "mouseMoved: mousePressed:left mouseReleased:left mousePressed:right mouseReleased:right mouseWheel:"
	if strings.Join(got, " ") != want {
		t.Errorf("mouse events = %v, want %s", got, want)
	}
	keys := f.find("Input.dispatchKeyEvent")
	var kinds []string
	for _, c := range keys {
		var p struct {
			Type     string   `json:"type"`
			Text     string   `json:"text"`
			VK       int      `json:"windowsVirtualKeyCode"`
			Commands []string `json:"commands"`
		}
		_ = json.Unmarshal(c.Params, &p)
		kinds = append(kinds, p.Type+":"+p.Text)
		if p.Type == "rawKeyDown" && p.VK == 0 && len(p.Commands) == 0 {
			t.Errorf("raw key down with no key code: %s", c.Params)
		}
	}
	// a (text), ctrl+a (raw, select all), Enter (text \r), ArrowDown (raw down only)
	wantKeys := "keyDown:a keyUp: rawKeyDown: keyUp: keyDown:\r keyUp: rawKeyDown:"
	if strings.Join(kinds, " ") != wantKeys {
		t.Errorf("key events = %q, want %q", strings.Join(kinds, " "), wantKeys)
	}
	if !strings.Contains(string(keys[2].Params), `"commands":["selectAll"]`) {
		t.Errorf("ctrl+a = %s", keys[2].Params)
	}
	if ins := f.find("Input.insertText"); len(ins) != 1 || !strings.Contains(string(ins[0].Params), "héllo") {
		t.Errorf("insertText = %+v", ins)
	}
	for _, bad := range []Input{
		{Type: "wave"}, {Type: "click", Button: "back"}, {Type: "key"}, {Type: "key", Key: "NoSuchKey"},
		{Type: "text"}, {Type: "text", Text: strings.Repeat("x", MaxInputText+1)},
	} {
		if err := s.Input(ctx, "", bad); !errors.Is(err, ErrInput) {
			t.Errorf("%+v: err = %v, want ErrInput", bad, err)
		}
	}
	// Chrome's own refusal comes back as an error.
	var rpc *RPCError
	if err := s.Input(ctx, "", Input{Type: "move", X: -1}); !errors.As(err, &rpc) || rpc.Method != "Input.dispatchMouseEvent" {
		t.Errorf("rpc error = %v", err)
	}
}

func TestScreenshotAddsAnImageEventToARunningSession(t *testing.T) {
	f := newFakeChrome(t)
	s, _, _, _ := newUp(t, f)
	var events []agentexec.Event
	var sess []string
	s.Sink = func(session string, ev agentexec.Event) error {
		sess, events = append(sess, session), append(events, ev)
		return nil
	}
	sh, err := s.Screenshot(ctx, "", "")
	if err != nil || sh.Bytes != len(tinyPNG) || sh.URL != "https://example.com/" || len(events) != 0 {
		t.Fatalf("plain screenshot = %+v, %v, events %d", sh, err, len(events))
	}
	sh, err = s.Screenshot(ctx, "TAB1", "abc123")
	if err != nil || sh.File == "" || len(events) != 1 || sess[0] != "abc123" {
		t.Fatalf("session screenshot = %+v, %v, events %d", sh, err, len(events))
	}
	ev := events[0]
	if ev.Kind != "image" || ev.Body != ShotPath+sh.File || !strings.Contains(ev.Title, "example.com") {
		t.Errorf("event = %+v", ev)
	}
	saved, err := os.ReadFile(filepath.Join(s.ShotsDir, sh.File))
	if err != nil || string(saved) != string(tinyPNG) {
		t.Errorf("saved file = %q, %v", saved, err)
	}
	// A session that is not running refuses the event.
	s.Sink = func(string, agentexec.Event) error { return errors.New("session is not running") }
	if _, err := s.Screenshot(ctx, "", "gone"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("sink refusal = %v", err)
	}
}

func TestStreamForwardsFramesAcksThemAndCapsTheRate(t *testing.T) {
	f := newFakeChrome(t)
	f.burst = 12
	s, _, _, _ := newUp(t, f)
	s.FrameInterval = 40 * time.Millisecond
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var frames []Frame
	var times []time.Time
	done := make(chan error, 1)
	go func() {
		done <- s.Stream(cctx, "", func(fr Frame) error {
			frames, times = append(frames, fr), append(times, time.Now())
			return nil
		})
	}()
	// All 12 frames are acknowledged, forwarded or dropped.
	waitFor(t, "12 acks", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.acks) == 12 })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(frames) == 0 || len(frames) >= 12 {
		t.Errorf("forwarded %d of 12 frames; the cap should drop some", len(frames))
	}
	if last := frames[len(frames)-1]; last.JPEG != "RlJBTUUxMg" && last.JPEG != "RlJBTUU12" {
		t.Errorf("last forwarded frame = %q, want the newest of the burst", last.JPEG)
	}
	if frames[0].Width != 1280 || frames[0].Height != 800 {
		t.Errorf("frame size = %vx%v", frames[0].Width, frames[0].Height)
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 35*time.Millisecond {
			t.Errorf("frames %d and %d were %v apart, under the 40ms cap", i-1, i, gap)
		}
	}
	m := f.methods()
	if m[0] != "Page.bringToFront" || m[1] != "Page.startScreencast" {
		t.Errorf("methods = %v", m)
	}
	waitFor(t, "stopScreencast", func() bool { return len(f.find("Page.stopScreencast")) == 1 })
	p := string(f.find("Page.startScreencast")[0].Params)
	if !strings.Contains(p, `"format":"jpeg"`) || !strings.Contains(p, `"maxWidth":1280`) {
		t.Errorf("startScreencast = %s", p)
	}
}

func TestStreamAcksOnlyAfterTheFrameWasTaken(t *testing.T) {
	f := newFakeChrome(t)
	f.burst = 1
	s, _, _, _ := newUp(t, f)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	release := make(chan struct{})
	got := make(chan struct{})
	go s.Stream(cctx, "", func(Frame) error { close(got); <-release; return nil })
	<-got
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	n := len(f.acks)
	f.mu.Unlock()
	if n != 0 {
		t.Errorf("acked %d frames while the reader still held the first", n)
	}
	close(release)
	waitFor(t, "the ack", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.acks) == 1 })
}

func TestStreamEndsWhenTheReaderFails(t *testing.T) {
	f := newFakeChrome(t)
	f.burst = 3
	s, _, _, _ := newUp(t, f)
	boom := errors.New("client went away")
	if err := s.Stream(ctx, "", func(Frame) error { return boom }); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
	waitFor(t, "stopScreencast", func() bool { return len(f.find("Page.stopScreencast")) == 1 })
}

// ---- the container ----

func TestRunArgsBindLoopbackAndMountNothing(t *testing.T) {
	args := RunArgs(DefaultImage, 49300)
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p 127.0.0.1:49300:9222", "--rm", "--name " + ContainerName, "--add-host host.docker.internal:host-gateway", "--cap-drop ALL", "--memory 1g", DefaultImage} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q: %s", want, joined)
		}
	}
	for i, a := range args {
		switch {
		case a == "-v" || a == "--volume" || a == "--mount" || strings.HasPrefix(a, "--volume=") || strings.HasPrefix(a, "--mount="):
			t.Errorf("a mount: %v", args)
		case a == "-p" && !strings.HasPrefix(args[i+1], "127.0.0.1:"):
			t.Errorf("a port not bound to loopback: %q", args[i+1])
		case a == "--privileged" || a == "--network" || a == "--net" || a == "--pid" || a == "--ipc" || a == "--user-data-dir":
			t.Errorf("unexpected flag %q", a)
		}
	}
	if strings.Contains(joined, ":/") && !strings.Contains(joined, "127.0.0.1:49300:9222") {
		t.Errorf("a path mapping: %s", joined)
	}
	// The profile is the container's own, in /tmp.
	if !strings.Contains(joined, "--user-data-dir=/tmp/") {
		t.Errorf("no throw-away profile: %s", joined)
	}
	if strings.Contains(strings.ToLower(joined), "appdata") || strings.Contains(joined, "Users") || strings.Contains(joined, ".config") {
		t.Errorf("the host's profile is mentioned: %s", joined)
	}
}

func TestStartStopAndTheActivityLog(t *testing.T) {
	f := newFakeChrome(t)
	s, d, _, log := newService(t, f)
	st, err := s.Status(ctx)
	if err != nil || st.State != "sleeping" || len(st.Tabs) != 0 {
		t.Fatalf("status before start = %+v, %v", st, err)
	}
	st, err = s.Start(ctx)
	if err != nil || st.State != "running" || st.CDP == "" || len(st.Tabs) != 1 || st.Version != "Chrome/155.0.0.0" {
		t.Fatalf("start = %+v, %v", st, err)
	}
	if !strings.HasPrefix(st.CDP, "ws://127.0.0.1:") {
		t.Errorf("CDP = %q, want a loopback address", st.CDP)
	}
	if _, err := s.Start(ctx); err != nil || d.ran("run") != 1 {
		t.Errorf("a second start ran the container again (%d runs): %v", d.ran("run"), err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Errorf("stopping a stopped browser: %v", err)
	}
	if _, err := s.Navigate(ctx, NavRequest{URL: "https://example.com/"}); !errors.Is(err, ErrNotRunning) {
		t.Errorf("navigate while sleeping = %v", err)
	}
	es, _ := log.Recent(10)
	if len(es) != 2 || es[0].Action != "stop" || es[1].Action != "start" || es[0].Kind != "browser" || !es[0].OK || es[1].Name != ContainerName {
		t.Errorf("activity = %+v", es)
	}
}

func TestStartThatNeverAnswersIsStopped(t *testing.T) {
	f := newFakeChrome(t)
	s, d, _, log := newService(t, f)
	s.Ready = func(context.Context, Endpoint) error { return errors.New("no answer") }
	if _, err := s.Start(ctx); err == nil || !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("err = %v", err)
	}
	if d.running {
		t.Error("the container was left running")
	}
	if es, _ := log.Recent(5); len(es) != 1 || es[0].OK || es[0].Error == "" {
		t.Errorf("activity = %+v", es)
	}
}

func TestIdleStopWithAFakeClock(t *testing.T) {
	f := newFakeChrome(t)
	s, d, c, log := newService(t, f)
	s.IdleAfter = 10 * time.Minute
	if _, err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c.add(9 * time.Minute)
	s.Tick(ctx)
	if !d.running {
		t.Fatal("stopped before the idle time")
	}
	// Using it resets the clock.
	if _, err := s.Navigate(ctx, NavRequest{URL: "https://example.com/"}); err != nil {
		t.Fatal(err)
	}
	c.add(9 * time.Minute)
	s.Tick(ctx)
	if !d.running {
		t.Fatal("a navigate did not count as activity")
	}
	c.add(2 * time.Minute)
	s.Tick(ctx)
	if d.running {
		t.Fatal("still running after the idle time")
	}
	es, _ := log.Recent(1)
	if len(es) != 1 || !es[0].Auto || es[0].Action != "stop" || !strings.Contains(es[0].Reason, "idle for 11 min") {
		t.Errorf("activity = %+v", es)
	}
}

func TestStatusDoesNotCountAsActivity(t *testing.T) {
	f := newFakeChrome(t)
	s, d, c, _ := newService(t, f)
	s.IdleAfter = 10 * time.Minute
	if _, err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c.add(8 * time.Minute)
	if _, err := s.Status(ctx); err != nil {
		t.Fatal(err)
	}
	c.add(3 * time.Minute)
	s.Tick(ctx)
	if d.running {
		t.Error("polling the status kept the browser awake")
	}
}

func TestAnAttachedSessionKeepsItAwakeUntilReleased(t *testing.T) {
	f := newFakeChrome(t)
	s, d, c, _ := newService(t, f)
	s.IdleAfter = 10 * time.Minute
	cdp, release, err := s.Attach(ctx, "sess1")
	if err != nil || !strings.HasPrefix(cdp, "ws://127.0.0.1:") {
		t.Fatalf("attach = %q, %v", cdp, err)
	}
	c.add(3 * time.Hour)
	s.Tick(ctx)
	if !d.running {
		t.Fatal("stopped under a running session")
	}
	if st, _ := s.Status(ctx); !st.Held {
		t.Error("status does not say it is held")
	}
	release()
	release() // twice is fine
	c.add(9 * time.Minute)
	s.Tick(ctx)
	if !d.running {
		t.Fatal("the idle time did not start at release")
	}
	c.add(2 * time.Minute)
	s.Tick(ctx)
	if d.running {
		t.Fatal("still up after the idle time past release")
	}
}

func TestADaemonRestartDoesNotKillARunningBrowserAtOnce(t *testing.T) {
	f := newFakeChrome(t)
	s, d, c, _ := newService(t, f)
	s.IdleAfter = 10 * time.Minute
	d.running = true // started by an earlier daemon
	c.add(5 * time.Hour)
	s.Tick(ctx)
	if !d.running {
		t.Fatal("stopped at the first look")
	}
	c.add(11 * time.Minute)
	s.Tick(ctx)
	if d.running {
		t.Fatal("never stopped")
	}
}

func TestSleepStopsIt(t *testing.T) {
	f := newFakeChrome(t)
	s, d, _, _ := newService(t, f)
	if out := s.Sleep(ctx); len(out) != 0 {
		t.Errorf("sleeping a sleeping browser: %+v", out)
	}
	if _, err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	out := s.Sleep(ctx)
	if len(out) != 1 || out[0].Kind != "browser" || !out[0].Stopped || d.running {
		t.Errorf("outcomes = %+v, running = %v", out, d.running)
	}
}

// ---- the routes ----

func api(t *testing.T, f *fakeChrome) (*httptest.Server, *Service, *fakeDocker) {
	t.Helper()
	s, d, _, _ := newService(t, f)
	mux := http.NewServeMux()
	Register(mux, s)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, s, d
}

func post(t *testing.T, url, body string, confirm bool) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	_, _ = bufio.NewReader(resp.Body).WriteTo(&sb)
	return resp.StatusCode, sb.String()
}

func TestEveryPostNeedsTheConfirmHeader(t *testing.T) {
	f := newFakeChrome(t)
	srv, _, d := api(t, f)
	for _, p := range []string{"start", "stop", "navigate", "input", "screenshot"} {
		if code, _ := post(t, srv.URL+"/api/browser/"+p, `{"url":"https://example.com/"}`, false); code != http.StatusForbidden {
			t.Errorf("POST %s without the header = %d", p, code)
		}
	}
	if d.ran("run") != 0 || d.ran("stop") != 0 {
		t.Error("docker was called without the header")
	}
}

func TestRoutesEndToEnd(t *testing.T) {
	f := newFakeChrome(t)
	srv, s, d := api(t, f)
	if code, body := post(t, srv.URL+"/api/browser/navigate", `{"url":"https://example.com/"}`, true); code != http.StatusConflict {
		t.Errorf("navigate while sleeping = %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/browser/start", ``, true); code != 200 || !strings.Contains(body, `"state":"running"`) {
		t.Fatalf("start = %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/browser/navigate", `{"url":"file:///etc/passwd"}`, true); code != http.StatusBadRequest || !strings.Contains(body, "only http and https") {
		t.Errorf("file: = %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/browser/navigate", `{"url":"http://localhost:5173/"}`, true); code != 200 || !strings.Contains(body, "host.docker.internal:5173") {
		t.Errorf("preview = %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/browser/navigate", `{"url":"https://example.com/","bogus":1}`, true); code != http.StatusBadRequest {
		t.Errorf("unknown field = %d", code)
	}
	// Input needs the take-over flag in the body.
	if code, _ := post(t, srv.URL+"/api/browser/input", `{"type":"click","x":1,"y":2}`, true); code != http.StatusForbidden {
		t.Errorf("input without takeover = %d", code)
	}
	if code, body := post(t, srv.URL+"/api/browser/input", `{"takeover":true,"type":"click","x":1,"y":2}`, true); code != 200 {
		t.Errorf("input = %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/browser/input", `{"takeover":true,"type":"wave"}`, true); code != http.StatusBadRequest {
		t.Errorf("bad input = %d", code)
	}
	s.Sink = func(string, agentexec.Event) error { return nil }
	code, body := post(t, srv.URL+"/api/browser/screenshot", `{"session":"s1"}`, true)
	var sh Shot
	if code != 200 || json.Unmarshal([]byte(body), &sh) != nil || sh.File == "" {
		t.Fatalf("screenshot = %d %s", code, body)
	}
	resp, err := http.Get(srv.URL + ShotPath + sh.File)
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("shot get = %v %v", resp, err)
	}
	for _, bad := range []string{"../x.png", "abc.png", "zzzzzzzzzzzzzzzz.png"} {
		if r, _ := http.Get(srv.URL + ShotPath + bad); r != nil && r.StatusCode == 200 {
			t.Errorf("shot %q served", bad)
		}
	}
	if code, body := post(t, srv.URL+"/api/browser/stop", ``, true); code != 200 || !strings.Contains(body, `"state":"sleeping"`) || d.running {
		t.Errorf("stop = %d %s", code, body)
	}
}

func TestStreamRouteSendsFrameEvents(t *testing.T) {
	f := newFakeChrome(t)
	f.burst = 2
	srv, s, _ := api(t, f)
	s.FrameInterval = time.Millisecond
	if code, _ := post(t, srv.URL+"/api/browser/start", ``, true); code != 200 {
		t.Fatal("start failed")
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, srv.URL+"/api/browser/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("content type = %q", resp.Header.Get("Content-Type"))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	var event, data string
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "event: "); ok {
			event = v
		}
		if v, ok := strings.CutPrefix(line, "data: "); ok {
			data = v
			break
		}
	}
	var fr Frame
	if event != "frame" || json.Unmarshal([]byte(data), &fr) != nil || fr.JPEG == "" || fr.Width != 1280 {
		t.Errorf("event %q data %q", event, data)
	}
	// While sleeping the stream route refuses at once.
	if code, _ := post(t, srv.URL+"/api/browser/stop", ``, true); code != 200 {
		t.Fatal("stop failed")
	}
	r2, err := http.Get(srv.URL + "/api/browser/stream")
	if err != nil || r2.StatusCode != http.StatusConflict {
		t.Errorf("stream while sleeping = %v %v", r2, err)
	}
}
