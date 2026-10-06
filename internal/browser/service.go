// Package browser is the agent browser: a headless Chromium in a container
// that Lucidbench starts on demand, watches live and lets the user take over.
//
// The container has its own throw-away profile and mounts nothing from the
// host, so it never sees the user's browser, cookies or files. Its DevTools
// port is published on 127.0.0.1 only. The power supervisor stops it after
// it has been idle (see Service.Tick) and every start and stop is written to
// the power activity log. The image is pulled from Docker Hub the first time
// it is started and is not redistributed.
package browser

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/power"
)

// Container and image.
const (
	ContainerName = "lucidbench-browser"
	LabelBrowser  = "lucidbench.browser"
	// DefaultImage is chromedp/headless-shell (BSD-3-Clause, Chromium under
	// its own licences), pinned to a Chromium version.
	DefaultImage = "chromedp/headless-shell:155.0.8059.26"
	// cdpPort is the port the image's socat listens on inside the container.
	cdpPort = 9222
	// DefaultIdle is how long the browser runs after it was last used.
	DefaultIdle = 10 * time.Minute
	// DefaultFrameInterval caps the screencast at about 15 frames a second.
	DefaultFrameInterval = 66 * time.Millisecond
)

// Errors, each mapped to an HTTP status by the routes.
var (
	ErrNotRunning = errors.New("the browser is sleeping: start it first")
	ErrNoTab      = errors.New("no such tab")
)

// Status is the state of the agent browser.
type Status struct {
	// State is sleeping, starting or running.
	State       string `json:"state"`
	Image       string `json:"image"`
	IdleMinutes int    `json:"idle_minutes"`
	// Version is the Chromium version, when it is running.
	Version string `json:"version,omitempty"`
	// CDP is the browser-level DevTools address on 127.0.0.1, the value an
	// attached Work session gets as LUCID_BROWSER_CDP.
	CDP  string   `json:"cdp,omitempty"`
	Tabs []Target `json:"tabs"`
	// Held says an attached Work session keeps it awake.
	Held bool   `json:"held,omitempty"`
	Note string `json:"note,omitempty"`
}

// Service starts and stops the agent browser and drives it. It is a
// power.Extra: the supervisor stops it on its regular check once it has been
// idle, and "Sleep everything idle" stops it too.
type Service struct {
	Docker    docker.Func
	Log       *power.ActivityLog
	Image     string
	IdleAfter time.Duration
	// ShotsDir holds the screenshots that were attached to a Work session.
	ShotsDir string
	// Sink adds an event to a running Work session's timeline; nil means
	// screenshots cannot be attached.
	Sink func(session string, ev agentexec.Event) error
	// FrameInterval is the shortest time between two forwarded frames.
	FrameInterval time.Duration
	// Now, FreePort and Ready are replaced in tests.
	Now      func() time.Time
	FreePort func() (int, error)
	Ready    func(ctx context.Context, e Endpoint) error

	startMu sync.Mutex // one start or stop at a time

	mu       sync.Mutex
	touched  time.Time
	starting bool
	base     string
	holds    map[string]int // Work sessions that keep it awake
	conns    map[string]*Conn
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) image() string {
	if s.Image != "" {
		return s.Image
	}
	return DefaultImage
}

func (s *Service) idle() time.Duration {
	if s.IdleAfter > 0 {
		return s.IdleAfter
	}
	return DefaultIdle
}

func (s *Service) touch() {
	s.mu.Lock()
	s.touched = s.now()
	s.mu.Unlock()
}

func (s *Service) record(e power.Entry) {
	if e.At.IsZero() {
		e.At = s.now()
	}
	if s.Log != nil {
		_ = s.Log.Append(e)
	}
}

// RunArgs is the `docker run` command line: bound to 127.0.0.1, with no
// volume or bind mount, so no host profile or folder can get in. The
// profile lives in the container and goes with it (--rm).
func RunArgs(image string, port int) []string {
	return []string{
		"run", "-d", "--rm", "--name", ContainerName,
		"--label", LabelBrowser + "=1",
		"--memory", "1g", "--shm-size", "256m", "--cpus", "2",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, cdpPort),
		"--add-host", DockerHost + ":host-gateway",
		image,
		// Passed on to headless-shell by the image's run.sh.
		"--user-data-dir=/tmp/lucidbench-profile", "--window-size=1280,800",
		"--disable-dev-shm-usage", "--hide-scrollbars", "--mute-audio",
	}
}

// lookup returns the DevTools base address of the running container, or "".
func (s *Service) lookup(ctx context.Context) (string, error) {
	out, err := s.Docker(ctx, "ps", "--filter", "name=^"+ContainerName+"$", "--format", "{{json .}}")
	if err != nil {
		return "", err
	}
	cs, err := docker.ParsePS(out)
	if err != nil {
		return "", err
	}
	for _, c := range cs {
		if c.Name != ContainerName || c.State != "running" {
			continue
		}
		po, err := s.Docker(ctx, "port", ContainerName, strconv.Itoa(cdpPort)+"/tcp")
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(po), "\n") {
			if _, port, err := net.SplitHostPort(strings.TrimSpace(line)); err == nil {
				// Always the loopback address, whatever docker reports.
				return "http://127.0.0.1:" + port, nil
			}
		}
	}
	return "", nil
}

// endpoint returns the running container's DevTools endpoint.
func (s *Service) endpoint(ctx context.Context) (Endpoint, error) {
	s.mu.Lock()
	base := s.base
	s.mu.Unlock()
	if base == "" {
		var err error
		if base, err = s.lookup(ctx); err != nil {
			return Endpoint{}, err
		}
		if base == "" {
			return Endpoint{}, ErrNotRunning
		}
		s.mu.Lock()
		s.base = base
		s.mu.Unlock()
	}
	return Endpoint{Base: base}, nil
}

// forget drops what was cached about a container that went away.
func (s *Service) forget() {
	s.mu.Lock()
	s.base = ""
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

// Status reports the browser without starting it and without counting as
// activity, so an open page does not keep it awake.
func (s *Service) Status(ctx context.Context) (Status, error) {
	st := Status{State: "sleeping", Image: s.image(), IdleMinutes: int(s.idle().Minutes()), Tabs: []Target{}}
	s.mu.Lock()
	starting, held := s.starting, len(s.holds) > 0
	s.mu.Unlock()
	st.Held = held
	base, err := s.lookup(ctx)
	if err != nil {
		return st, err
	}
	if base == "" {
		s.forget()
		if starting {
			st.State = "starting"
		}
		return st, nil
	}
	s.mu.Lock()
	s.base = base
	s.mu.Unlock()
	e := Endpoint{Base: base}
	v, err := e.Version(ctx)
	if err != nil {
		st.State, st.Note = "starting", "the container is up and the browser is not answering yet"
		return st, nil
	}
	st.State, st.Version, st.CDP = "running", v.Browser, v.WSURL
	if tabs, err := e.Targets(ctx); err == nil {
		st.Tabs = tabs
	}
	return st, nil
}

// Start runs the container. When it is already up the call counts as activity.
func (s *Service) Start(ctx context.Context) (Status, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if base, err := s.lookup(ctx); err != nil {
		return Status{}, err
	} else if base != "" {
		s.mu.Lock()
		s.base = base
		s.mu.Unlock()
		s.touch()
		return s.Status(ctx)
	}
	free := s.FreePort
	if free == nil {
		free = freePort
	}
	port, err := free()
	if err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	s.starting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
	}()
	began := s.now()
	e := power.Entry{Kind: "browser", Name: ContainerName, Action: "start", Reason: "opened from Lucidbench"}
	if _, err := s.Docker(ctx, RunArgs(s.image(), port)...); err != nil {
		e.Error = err.Error()
		s.record(e)
		return Status{}, err
	}
	ep := Endpoint{Base: "http://127.0.0.1:" + strconv.Itoa(port)}
	ready := s.Ready
	if ready == nil {
		ready = waitReady
	}
	if err := ready(ctx, ep); err != nil {
		e.Error = "started but did not answer: " + err.Error()
		e.Seconds = s.now().Sub(began).Seconds()
		s.record(e)
		_, _ = s.Docker(context.WithoutCancel(ctx), "stop", ContainerName)
		return Status{}, errors.New(e.Error)
	}
	s.mu.Lock()
	s.base = ep.Base
	s.mu.Unlock()
	e.OK, e.Seconds = true, s.now().Sub(began).Seconds()
	s.record(e)
	s.touch()
	return s.Status(ctx)
}

// Stop stops the container; one that is not running is not an error.
func (s *Service) Stop(ctx context.Context) error {
	return s.stop(ctx, false, "stopped from Lucidbench")
}

func (s *Service) stop(ctx context.Context, auto bool, reason string) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	began := s.now()
	e := power.Entry{Kind: "browser", Name: ContainerName, Action: "stop", Auto: auto, Reason: reason}
	s.forget()
	_, err := s.Docker(ctx, "stop", ContainerName)
	if err != nil && strings.Contains(err.Error(), "No such container") {
		return nil
	}
	e.OK, e.Seconds = err == nil, s.now().Sub(began).Seconds()
	if err != nil {
		e.Error = err.Error()
	}
	s.record(e)
	return err
}

// Attach starts the browser for a Work session and keeps it awake until
// release is called. cdp is the browser-level DevTools address the session's
// agent may drive.
func (s *Service) Attach(ctx context.Context, session string) (cdp string, release func(), err error) {
	st, err := s.Start(ctx)
	if err != nil {
		return "", nil, err
	}
	if st.CDP == "" {
		return "", nil, errors.New("the browser started but did not report its DevTools address")
	}
	s.mu.Lock()
	if s.holds == nil {
		s.holds = map[string]int{}
	}
	s.holds[session]++
	s.mu.Unlock()
	var once sync.Once
	return st.CDP, func() {
		once.Do(func() {
			s.mu.Lock()
			if s.holds[session]--; s.holds[session] <= 0 {
				delete(s.holds, session)
			}
			s.mu.Unlock()
			s.touch() // the idle time starts when the session lets go
		})
	}, nil
}

// Tick stops the browser once it has been idle for the idle time. It never
// stops while an attached Work session is running, since the agent drives
// the browser over CDP where Lucidbench cannot see it. A container the
// daemon did not start itself (it was restarted since) counts as used now.
func (s *Service) Tick(ctx context.Context) {
	base, err := s.lookup(ctx)
	if err != nil || base == "" {
		return
	}
	s.mu.Lock()
	held, last := len(s.holds) > 0, s.touched
	s.mu.Unlock()
	if held || last.IsZero() {
		s.touch()
		return
	}
	if idle := s.now().Sub(last); idle >= s.idle() {
		_ = s.stop(ctx, true, fmt.Sprintf("idle for %d min", int(idle.Minutes())))
	}
}

// Sleep stops the browser, for "Sleep everything idle".
func (s *Service) Sleep(ctx context.Context) []power.Outcome {
	base, err := s.lookup(ctx)
	if err != nil || base == "" {
		return nil
	}
	o := power.Outcome{Kind: "browser", Name: ContainerName}
	if err := s.stop(ctx, false, "sleep everything idle"); err != nil {
		o.Reason = err.Error()
	} else {
		o.Stopped, o.Reason = true, "stopped"
	}
	return []power.Outcome{o}
}

var _ power.Extra = (*Service)(nil)

// freePort asks the OS for a free loopback port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitReady polls the DevTools endpoint for up to 30 seconds.
func waitReady(ctx context.Context, e Endpoint) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if _, last = e.Version(ctx); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	return last
}

// ---- tabs and driving ----

// tab finds a tab by id, or the first one; with none open it opens a blank
// one when create is set.
func (s *Service) tab(ctx context.Context, id string, create bool) (Endpoint, Target, error) {
	e, err := s.endpoint(ctx)
	if err != nil {
		return e, Target{}, err
	}
	ts, err := e.Targets(ctx)
	if err != nil {
		s.forget()
		return e, Target{}, err
	}
	if id != "" {
		for _, t := range ts {
			if t.ID == id {
				return e, t, nil
			}
		}
		return e, Target{}, ErrNoTab
	}
	if len(ts) > 0 {
		return e, ts[0], nil
	}
	if !create {
		return e, Target{}, ErrNoTab
	}
	t, err := e.NewTarget(ctx)
	return e, t, err
}

// conn returns a cached command connection to a tab.
func (s *Service) conn(ctx context.Context, t Target) (*Conn, error) {
	s.mu.Lock()
	c := s.conns[t.ID]
	s.mu.Unlock()
	if c != nil && c.Err() == nil {
		return c, nil
	}
	c, err := Dial(ctx, t.WSURL)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.conns == nil {
		s.conns = map[string]*Conn{}
	}
	s.conns[t.ID] = c
	s.mu.Unlock()
	return c, nil
}

// call runs one CDP method on a tab, and redials once when the cached
// connection turns out to be dead.
func (s *Service) call(ctx context.Context, t Target, method string, params any) ([]byte, error) {
	for try := 0; ; try++ {
		c, err := s.conn(ctx, t)
		if err != nil {
			return nil, err
		}
		out, err := c.Call(ctx, method, params)
		if errors.Is(err, ErrClosed) && try == 0 {
			continue
		}
		return out, err
	}
}

// Navigation actions.
const (
	ActionGo      = "go"
	ActionBack    = "back"
	ActionForward = "forward"
	ActionReload  = "reload"
	ActionClose   = "close"
)

// NavRequest is POST /api/browser/navigate.
type NavRequest struct {
	// Target is a tab id; empty means the first tab (one is opened when there is none).
	Target string `json:"target,omitempty"`
	URL    string `json:"url,omitempty"`
	// Action is go (the default), back, forward, reload or close.
	Action string `json:"action,omitempty"`
	// NewTab opens the URL in a new tab.
	NewTab bool `json:"new_tab,omitempty"`
}

// NavResult says where the tab went.
type NavResult struct {
	Target string `json:"target"`
	// URL is what was opened: a localhost address is rewritten to
	// host.docker.internal.
	URL string `json:"url,omitempty"`
	// ErrorText is Chrome's own reason when the page did not load.
	ErrorText string `json:"error_text,omitempty"`
}

// Navigate drives a tab. It counts as activity.
func (s *Service) Navigate(ctx context.Context, r NavRequest) (NavResult, error) {
	if r.Action == "" {
		r.Action = ActionGo
	}
	var open string
	if r.Action == ActionGo {
		var err error
		if open, err = CheckURL(r.URL); err != nil {
			return NavResult{}, err
		}
	}
	e, t, err := s.tab(ctx, r.Target, r.Action == ActionGo)
	if err != nil {
		return NavResult{}, err
	}
	s.touch()
	res := NavResult{Target: t.ID}
	switch r.Action {
	case ActionGo:
		if r.NewTab {
			if t, err = e.NewTarget(ctx); err != nil {
				return res, err
			}
			res.Target = t.ID
		}
		out, err := s.call(ctx, t, "Page.navigate", map[string]string{"url": open})
		if err != nil {
			return res, err
		}
		res.URL = open
		res.ErrorText = errorText(out)
	case ActionReload:
		_, err = s.call(ctx, t, "Page.reload", map[string]any{})
	case ActionBack, ActionForward:
		err = s.step(ctx, t, r.Action == ActionForward)
	case ActionClose:
		err = e.CloseTarget(ctx, t.ID)
	default:
		return res, fmt.Errorf("%w: action must be go, back, forward, reload or close", ErrURL)
	}
	return res, err
}

func errorText(out []byte) string {
	var v struct {
		ErrorText string `json:"errorText"`
	}
	_ = json.Unmarshal(out, &v)
	return v.ErrorText
}

// step moves one entry through the tab's history; at an end it does nothing.
func (s *Service) step(ctx context.Context, t Target, forward bool) error {
	out, err := s.call(ctx, t, "Page.getNavigationHistory", map[string]any{})
	if err != nil {
		return err
	}
	var h struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			ID int `json:"id"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(out, &h); err != nil {
		return err
	}
	i := h.CurrentIndex - 1
	if forward {
		i = h.CurrentIndex + 1
	}
	if i < 0 || i >= len(h.Entries) {
		return nil
	}
	_, err = s.call(ctx, t, "Page.navigateToHistoryEntry", map[string]int{"entryId": h.Entries[i].ID})
	return err
}

// Input forwards one mouse or keyboard event to a tab. It counts as activity.
func (s *Service) Input(ctx context.Context, target string, in Input) error {
	_, t, err := s.tab(ctx, target, false)
	if err != nil {
		return err
	}
	s.touch()
	c, err := s.conn(ctx, t)
	if err != nil {
		return err
	}
	if err := in.dispatch(ctx, c); err != nil {
		if errors.Is(err, ErrClosed) {
			// One retry on a fresh connection.
			if c, cerr := s.conn(ctx, t); cerr == nil {
				return in.dispatch(ctx, c)
			}
		}
		return err
	}
	return nil
}

// Shot is a PNG screenshot.
type Shot struct {
	Target string `json:"target"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	// PNG is the image, base64 encoded.
	PNG   string `json:"png"`
	Bytes int    `json:"bytes"`
	// Session and File say it was added to a Work session's timeline.
	Session string `json:"session,omitempty"`
	File    string `json:"file,omitempty"`
}

// Screenshot captures a tab as PNG. With a session id it is also saved and
// added to that running session's timeline as an image event.
func (s *Service) Screenshot(ctx context.Context, target, session string) (Shot, error) {
	_, t, err := s.tab(ctx, target, false)
	if err != nil {
		return Shot{}, err
	}
	s.touch()
	out, err := s.call(ctx, t, "Page.captureScreenshot", map[string]string{"format": "png"})
	if err != nil {
		return Shot{}, err
	}
	var v struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(out, &v); err != nil || v.Data == "" {
		return Shot{}, errors.New("the browser returned no image")
	}
	raw, err := base64.StdEncoding.DecodeString(v.Data)
	if err != nil {
		return Shot{}, err
	}
	sh := Shot{Target: t.ID, URL: t.URL, Title: t.Title, PNG: v.Data, Bytes: len(raw)}
	if session == "" {
		return sh, nil
	}
	if s.Sink == nil || s.ShotsDir == "" {
		return sh, errors.New("screenshots cannot be added to a session here")
	}
	name, err := s.saveShot(raw)
	if err != nil {
		return sh, err
	}
	title := "Browser screenshot"
	if t.URL != "" {
		title += ": " + t.URL
	}
	ev := agentexec.Event{Time: s.now().UTC(), Kind: "image", Title: title, Body: ShotPath + name}
	if err := s.Sink(session, ev); err != nil {
		return sh, err
	}
	sh.Session, sh.File = session, name
	return sh, nil
}

// ShotPath is where the API serves saved screenshots.
const ShotPath = "/api/browser/shots/"

func (s *Service) saveShot(png []byte) (string, error) {
	if err := os.MkdirAll(s.ShotsDir, 0o700); err != nil {
		return "", err
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	name := hex.EncodeToString(b[:]) + ".png"
	return name, os.WriteFile(filepath.Join(s.ShotsDir, name), png, 0o600)
}

// Shot opens a saved screenshot.
func (s *Service) OpenShot(name string) (*os.File, error) {
	if !ShotName(name) || s.ShotsDir == "" {
		return nil, ErrNoTab
	}
	return os.Open(filepath.Join(s.ShotsDir, name))
}

// ShotName reports whether name is one this package saved.
func ShotName(name string) bool {
	n, ok := strings.CutSuffix(name, ".png")
	if !ok || len(n) != 16 {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
}

// ---- screencast ----

// Frame is one JPEG screencast frame.
type Frame struct {
	// JPEG is the image, base64 encoded.
	JPEG string `json:"jpeg"`
	// Width and Height are the page's size in CSS pixels, which the pointer
	// positions sent back are in.
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type screencastFrame struct {
	Data     string `json:"data"`
	Metadata struct {
		DeviceWidth  float64 `json:"deviceWidth"`
		DeviceHeight float64 `json:"deviceHeight"`
	} `json:"metadata"`
	SessionID int `json:"sessionId"`
}

// Stream sends the tab's screencast to fn until ctx ends, fn fails or the
// browser goes away. Chrome only sends the next frame once the last one is
// acknowledged, and a frame is acknowledged after fn took it, so a slow
// reader slows Chrome down; frames are also held back to FrameInterval. When
// frames arrive faster than that, only the newest is forwarded, so the last
// frame of a burst is never lost.
func (s *Service) Stream(ctx context.Context, target string, fn func(Frame) error) error {
	_, t, err := s.tab(ctx, target, false)
	if err != nil {
		return err
	}
	s.touch()
	c, err := Dial(ctx, t.WSURL)
	if err != nil {
		return err
	}
	defer c.Close()
	latest := make(chan screencastFrame, 1)
	ack := func(id int) { _ = c.Notify("Page.screencastFrameAck", map[string]int{"sessionId": id}) }
	c.OnEvent(func(method string, params json.RawMessage) {
		if method != "Page.screencastFrame" {
			return
		}
		var f screencastFrame
		if json.Unmarshal(params, &f) != nil {
			return
		}
		for {
			select {
			case latest <- f:
				return
			default:
			}
			select {
			case old := <-latest:
				ack(old.SessionID) // dropped, but Chrome still wants its ack
			default:
			}
		}
	})
	_, _ = c.Call(ctx, "Page.bringToFront", map[string]any{})
	if _, err := c.Call(ctx, "Page.startScreencast", map[string]any{
		"format": "jpeg", "quality": 60, "maxWidth": 1280, "maxHeight": 800, "everyNthFrame": 1,
	}); err != nil {
		return err
	}
	defer func() {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_, _ = c.Call(sctx, "Page.stopScreencast", map[string]any{})
	}()
	interval := s.FrameInterval
	if interval <= 0 {
		interval = DefaultFrameInterval
	}
	var last time.Time
	keep := time.NewTicker(30 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.Done():
			return c.Err()
		case <-keep.C:
			s.touch() // someone is watching
		case f := <-latest:
			if wait := interval - time.Since(last); wait > 0 {
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return nil
				}
			}
			last = time.Now()
			if err := fn(Frame{JPEG: f.Data, Width: f.Metadata.DeviceWidth, Height: f.Metadata.DeviceHeight}); err != nil {
				return err
			}
			ack(f.SessionID)
		}
	}
}
