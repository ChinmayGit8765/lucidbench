package remote

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// fakeWork is an in-memory Work service.
type fakeWork struct {
	mu       sync.Mutex
	sessions map[string]work.Session
	events   map[string][]agentexec.Event
	stopped  []string
}

func (f *fakeWork) List() []work.Session {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []work.Session
	for _, s := range f.sessions {
		out = append(out, s)
	}
	return out
}

func (f *fakeWork) Get(id string) (work.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return s, work.ErrNotFound
	}
	return s, nil
}

func (f *fakeWork) Events(id string, from int) ([]agentexec.Event, bool, <-chan struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, false, nil, work.ErrNotFound
	}
	evs := f.events[id]
	if from > len(evs) {
		from = len(evs)
	}
	return evs[from:], s.Status == work.StatusRunning, make(chan struct{}), nil
}

func (f *fakeWork) Stop(id string) (work.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return s, work.ErrNotFound
	}
	if s.Status != work.StatusRunning {
		return s, work.ErrConflict
	}
	s.Status = work.StatusStopped
	f.sessions[id] = s
	f.stopped = append(f.stopped, id)
	return s, nil
}

// fakeCouncil is an in-memory council service.
type fakeCouncil struct {
	mu       sync.Mutex
	sessions map[string]*council.Session
	again    map[string]string
}

func (f *fakeCouncil) List() ([]council.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []council.Summary
	for _, s := range f.sessions {
		out = append(out, council.Summary{ID: s.ID, Title: s.Title, Project: s.Project, Status: s.Status, BriefPath: s.BriefPath})
	}
	return out, nil
}

func (f *fakeCouncil) Get(id string) (*council.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, council.ErrNotFound
	}
	c := *s
	return &c, nil
}

func (f *fakeCouncil) ApproveChecked(id, _ string, _ bool) (*boards.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, council.ErrNotFound
	}
	s.Status = council.StatusApproved
	return &boards.Card{ID: "c1", Column: "Ready"}, nil
}

func (f *fakeCouncil) BeginAgain(id, notes string) (*council.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.again == nil {
		f.again = map[string]string{}
	}
	f.again[id] = notes
	s := f.sessions[id]
	s.Status = council.StatusRunning
	return s, nil
}

type rig struct {
	t       *testing.T
	dir     string
	surface *Surface
	handler http.Handler
	work    *fakeWork
	council *fakeCouncil
}

func newRig(t *testing.T) *rig {
	t.Helper()
	dir := t.TempDir()
	fw := &fakeWork{sessions: map[string]work.Session{
		"w1": {ID: "w1", Title: "Open title", Project: "open", Provider: "claude", Status: work.StatusRunning},
		"w2": {ID: "w2", Title: "Secret plan", Project: "hush", Provider: "claude", Status: work.StatusRunning},
	}, events: map[string][]agentexec.Event{
		"w1": {{Kind: "text", Body: "hello"}, {Kind: "image", Body: "/api/browser/shots/x.png"}},
		"w2": {{Kind: "text", Body: "secret text"}},
	}}
	fc := &fakeCouncil{sessions: map[string]*council.Session{
		"c1": {ID: "c1", Title: "Open brief", Project: "open", Status: council.StatusDraft, Brief: "# Open brief\n"},
		"c2": {ID: "c2", Title: "Hush brief", Project: "hush", Status: council.StatusDraft, Brief: "# Hush\n"},
	}}
	list := &projects.List{Projects: []projects.Project{{ID: "open", Visibility: "private"}, {ID: "hush", Visibility: "confidential"}}}
	svc := Services{Work: fw, Council: fc, Projects: func() (*projects.List, error) { return list, nil }}
	devs := &Devices{Path: filepath.Join(dir, "devices.json")}
	s := NewSurface(svc, devs, &Audit{Path: filepath.Join(dir, "audit.jsonl")}, fstest.MapFS{
		"index.html":           {Data: []byte("<html>phone</html>")},
		"assets/remote.js":     {Data: []byte("js")},
		"manifest.webmanifest": {Data: []byte("{}")},
	})
	return &rig{t: t, dir: dir, surface: s, handler: s.Handler(), work: fw, council: fc}
}

func (r *rig) do(method, path, token string, confirm bool, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.168.1.50:5555"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}

func (r *rig) pair() string {
	r.t.Helper()
	code, _ := r.surface.Devices.NewCode()
	rec := r.do("POST", "/r/api/pair", "", true, `{"code":"`+code+`","name":"Test phone"}`)
	if rec.Code != 200 {
		r.t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Token == "" {
		r.t.Fatal("no token")
	}
	return out.Token
}

func TestAllowlistEverythingElse404(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	paths := []string{
		"/", "/settings", "/accounts", "/work", "/api/health", "/api/config", "/api/accounts", "/api/prefs",
		"/api/memory/page?path=Inbox/a.md", "/api/memory/tree", "/api/payments/status", "/api/cloud", "/api/databases",
		"/api/browser", "/api/browser/input", "/api/work/sessions", "/api/work/sessions/w1/raw", "/api/work/sessions/w1/pr",
		"/api/council/sessions", "/api/council/sessions/c1/approve", "/api/remote", "/api/remote/devices", "/api/remote/pair",
		"/api/usage/summary", "/api/ci/summary", "/api/power", "/api/jobs", "/api/projects", "/api/mcp",
		"/r/api", "/r/api/", "/r/api/settings", "/r/api/accounts", "/r/api/work/sessions/w1", "/r/api/work/sessions/w1/raw",
		"/r/api/work/sessions/w1/pr", "/r/api/work/sessions/w1/remove", "/r/api/council/sessions", "/r/api/council/sessions/c1/again",
		"/r/api/council/sessions/c1/events", "/r/api/memory/page", "/r/api/payments", "/r/api/devices", "/r/api/remote/pair",
		"/r/../api/config", "/r/assets/missing.js", "/r/assets", "/r/api/overview/x", "/r/api/pair/x",
	}
	for _, p := range paths {
		for _, m := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
			rec := r.do(m, p, tok, true, "")
			if rec.Code == http.StatusMovedPermanently || rec.Code == http.StatusTemporaryRedirect || rec.Code == http.StatusPermanentRedirect {
				// The mux cleans a path with a redirect; where it lands must be 404 too.
				rec = r.do(m, rec.Header().Get("Location"), tok, true, "")
			}
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s: got %d, want 404", m, p, rec.Code)
			}
		}
	}
	// Wrong methods on allowed paths are refused too (405 from the mux is fine; never served).
	for _, p := range []string{"/r/api/overview", "/r/api/work/sessions"} {
		if rec := r.do("DELETE", p, tok, true, ""); rec.Code == http.StatusOK {
			t.Errorf("DELETE %s served", p)
		}
	}
}

func TestEveryListedRouteAnswers(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	// A finished session, so its event stream ends.
	s := r.work.sessions["w1"]
	s.Status = work.StatusDone
	r.work.sessions["w1"] = s
	for _, route := range Routes {
		method, p, _ := strings.Cut(route, " ")
		p = strings.ReplaceAll(p, "{id}", "w1")
		if strings.Contains(p, "council") {
			p = strings.ReplaceAll(p, "w1", "c1")
		}
		body := ""
		if strings.HasSuffix(p, "send-back") {
			body = `{"notes":"shorter"}`
		}
		rec := r.do(method, p, tok, true, body)
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s: 404 %s", route, rec.Body)
		}
	}
}

func TestPhonePageServed(t *testing.T) {
	r := newRig(t)
	for _, p := range []string{"/r", "/r/", "/r/assets/remote.js", "/r/manifest.webmanifest"} {
		rec := r.do("GET", p, "", false, "")
		if rec.Code != 200 {
			t.Errorf("%s: %d", p, rec.Code)
		}
		if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Errorf("%s: CSP %q", p, csp)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s: CORS header set", p)
		}
	}
}

func TestAuthAndConfirm(t *testing.T) {
	r := newRig(t)
	if rec := r.do("GET", "/r/api/overview", "", false, ""); rec.Code != 401 {
		t.Fatalf("no token: %d", rec.Code)
	}
	tok := r.pair()
	if rec := r.do("GET", "/r/api/overview", tok, false, ""); rec.Code != 200 {
		t.Fatalf("overview: %d %s", rec.Code, rec.Body)
	}
	// Writes need the confirm header as well as the token.
	if rec := r.do("POST", "/r/api/work/sessions/w1/stop", tok, false, ""); rec.Code != 403 {
		t.Fatalf("stop without confirm: %d", rec.Code)
	}
	if rec := r.do("POST", "/r/api/council/sessions/c1/approve", tok, false, ""); rec.Code != 403 {
		t.Fatalf("approve without confirm: %d", rec.Code)
	}
	if rec := r.do("POST", "/r/api/work/sessions/w1/stop", "", true, ""); rec.Code != 401 {
		t.Fatalf("stop without token: %d", rec.Code)
	}
	if len(r.work.stopped) != 0 {
		t.Fatal("stopped without confirm")
	}
	if rec := r.do("POST", "/r/api/work/sessions/w1/stop", tok, true, ""); rec.Code != 200 {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if rec := r.do("POST", "/r/api/council/sessions/c1/approve", tok, true, ""); rec.Code != 200 {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	if rec := r.do("POST", "/r/api/pair", "", false, `{"code":"x"}`); rec.Code != 403 {
		t.Fatalf("pair without confirm: %d", rec.Code)
	}
	// The audit has the writes, and never the token.
	b, _ := os.ReadFile(filepath.Join(r.dir, "audit.jsonl"))
	for _, want := range []string{`"action":"stop"`, `"action":"approve"`, `"action":"pair"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("audit lacks %s:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), tok) {
		t.Fatal("the audit holds the token")
	}
}

func TestFollowUpRefused(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	rec := r.do("POST", "/r/api/work/sessions/w1/followup", tok, true, `{"prompt":"go on"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("follow-up: %d %s", rec.Code, rec.Body)
	}
}

func TestSendBack(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	if rec := r.do("POST", "/r/api/council/sessions/c1/send-back", tok, true, `{"notes":""}`); rec.Code != 400 {
		t.Fatalf("empty notes: %d", rec.Code)
	}
	if rec := r.do("POST", "/r/api/council/sessions/c1/send-back", tok, true, `{"notes":"Cut scope"}`); rec.Code != 202 {
		t.Fatalf("send back: %d %s", rec.Code, rec.Body)
	}
	if r.council.again["c1"] != "Cut scope" {
		t.Fatal("notes not passed on")
	}
}

func TestConfidentialRedacted(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	rec := r.do("GET", "/r/api/overview", tok, false, "")
	body := rec.Body.String()
	for _, leak := range []string{"Secret plan", "Hush brief", "hush"} {
		if strings.Contains(body, leak) {
			t.Errorf("overview leaks %q: %s", leak, body)
		}
	}
	if !strings.Contains(body, "Open title") || !strings.Contains(body, `"title":"confidential"`) {
		t.Errorf("overview: %s", body)
	}
	rec = r.do("GET", "/r/api/work/sessions", tok, false, "")
	if strings.Contains(rec.Body.String(), "Secret plan") || !strings.Contains(rec.Body.String(), `"confidential":true`) {
		t.Errorf("sessions: %s", rec.Body)
	}
	if rec := r.do("GET", "/r/api/work/sessions/w2/events", tok, false, ""); rec.Code != 403 || strings.Contains(rec.Body.String(), "secret text") {
		t.Errorf("confidential events: %d %s", rec.Code, rec.Body)
	}
	rec = r.do("GET", "/r/api/council/sessions/c2", tok, false, "")
	if rec.Code != 403 || strings.Contains(rec.Body.String(), "Hush") {
		t.Errorf("confidential brief: %d %s", rec.Code, rec.Body)
	}
	if rec := r.do("POST", "/r/api/council/sessions/c2/approve", tok, true, ""); rec.Code != 403 {
		t.Errorf("confidential approve: %d", rec.Code)
	}
	if r.council.sessions["c2"].Status != council.StatusDraft {
		t.Error("a confidential brief was approved from the phone")
	}
	// A failing project list makes every project confidential.
	r.surface.Services.Projects = func() (*projects.List, error) { return nil, errors.New("broken") }
	rec = r.do("GET", "/r/api/work/sessions", tok, false, "")
	if strings.Contains(rec.Body.String(), "Open title") {
		t.Errorf("fail-open: %s", rec.Body)
	}
}

func TestEventsTail(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	s := r.work.sessions["w1"]
	s.Status = work.StatusDone
	r.work.sessions["w1"] = s
	rec := r.do("GET", "/r/api/work/sessions/w1/events", tok, false, "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("events: %d %s", rec.Code, rec.Body)
	}
	b := rec.Body.String()
	if !strings.Contains(b, `"body":"hello"`) || !strings.Contains(b, "event: end") {
		t.Fatalf("stream: %s", b)
	}
	if strings.Contains(b, "/api/browser/shots") {
		t.Fatalf("image path leaked: %s", b)
	}
}

func TestAuthRateLimit(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	bad := hex.EncodeToString(randomBytes(32))
	for i := 0; i < 10; i++ {
		if rec := r.do("GET", "/r/api/overview", bad, false, ""); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	// Locked out: even the good token waits.
	if rec := r.do("GET", "/r/api/overview", tok, false, ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 10 failures: %d", rec.Code)
	}
}

func TestPairRateLimit(t *testing.T) {
	r := newRig(t)
	code, _ := r.surface.Devices.NewCode()
	for i := 0; i < 5; i++ {
		if rec := r.do("POST", "/r/api/pair", "", true, `{"code":"WRONG","name":"x"}`); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	if rec := r.do("POST", "/r/api/pair", "", true, `{"code":"`+code+`","name":"x"}`); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures: %d", rec.Code)
	}
}

func TestLimiterWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(2, time.Minute)
	l.Now = func() time.Time { return now }
	l.Fail("a")
	if !l.Allowed("a") {
		t.Fatal("one failure blocks")
	}
	l.Fail("a")
	if l.Allowed("a") {
		t.Fatal("two failures allowed")
	}
	if !l.Allowed("b") {
		t.Fatal("another client blocked")
	}
	now = now.Add(time.Minute)
	if !l.Allowed("a") {
		t.Fatal("not allowed after the window")
	}
}

func TestPairCodeSingleUseAndExpiry(t *testing.T) {
	now := time.Unix(5000, 0)
	d := &Devices{Path: filepath.Join(t.TempDir(), "devices.json"), Now: func() time.Time { return now }}
	if _, _, err := d.Pair("", "x"); !errors.Is(err, ErrBadCode) {
		t.Fatal("pairing without a code")
	}
	code, exp := d.NewCode()
	if exp.Sub(now) != PairTTL || PairTTL != 5*time.Minute {
		t.Fatalf("expiry %v", exp.Sub(now))
	}
	tok, dev, err := d.Pair(strings.ToLower(code), "Pixel")
	if err != nil || tok == "" || dev.Name != "Pixel" {
		t.Fatalf("pair: %v", err)
	}
	if _, _, err := d.Pair(code, "again"); !errors.Is(err, ErrBadCode) {
		t.Fatal("a code worked twice")
	}
	code, _ = d.NewCode()
	now = now.Add(PairTTL)
	if _, _, err := d.Pair(code, "late"); !errors.Is(err, ErrBadCode) {
		t.Fatal("an expired code worked")
	}
	// A new code replaces the old one.
	first, _ := d.NewCode()
	second, _ := d.NewCode()
	if _, _, err := d.Pair(first, "old"); !errors.Is(err, ErrBadCode) {
		t.Fatal("a replaced code worked")
	}
	if _, _, err := d.Pair(second, "new"); err != nil {
		t.Fatal(err)
	}
}

func TestTokenHashedAndRevocable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	d := &Devices{Path: path}
	code, _ := d.NewCode()
	tok, dev, err := d.Pair(code, "Phone\x00name")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token is not 32 bytes: %d %v", len(raw), err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), tok) || strings.Contains(string(b), code) {
		t.Fatal("devices.json holds the token or code")
	}
	if !strings.Contains(string(b), hex.EncodeToString(hashOf(tok))) {
		t.Fatal("devices.json lacks the token hash")
	}
	if strings.Contains(string(b), "\\u0000") {
		t.Fatal("control character kept in the name")
	}
	// A fresh store reads the file.
	d2 := &Devices{Path: path}
	if got, err := d2.Auth(tok); err != nil || got.ID != dev.ID {
		t.Fatalf("auth: %v", err)
	}
	if _, err := d2.Auth(tok + "x"); err == nil {
		t.Fatal("a wrong token passed")
	}
	if _, err := d2.Revoke(dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.Auth(tok); !errors.Is(err, ErrBadToken) {
		t.Fatal("a revoked token passed")
	}
	if _, err := d2.Revoke(dev.ID); !errors.Is(err, ErrDeviceMissing) {
		t.Fatal("revoked twice")
	}
	list, _ := d2.List()
	if len(list) != 0 {
		t.Fatalf("list: %v", list)
	}
}

func TestRevokedTokenRefusedBySurface(t *testing.T) {
	r := newRig(t)
	tok := r.pair()
	list, _ := r.surface.Devices.List()
	if _, err := r.surface.Devices.Revoke(list[0].ID); err != nil {
		t.Fatal(err)
	}
	if rec := r.do("GET", "/r/api/overview", tok, false, ""); rec.Code != 401 {
		t.Fatalf("revoked: %d", rec.Code)
	}
}

// countingListen records binds and listens on a free loopback port instead.
type countingListen struct {
	mu    sync.Mutex
	addrs []string
}

func (c *countingListen) listen(network, addr string) (net.Listener, error) {
	c.mu.Lock()
	c.addrs = append(c.addrs, addr)
	c.mu.Unlock()
	return net.Listen(network, "127.0.0.1:0")
}

func testManager(t *testing.T, ifs []Interface) (*Manager, *countingListen) {
	t.Helper()
	m := NewManager(t.TempDir())
	c := &countingListen{}
	m.Listen = c.listen
	m.Interfaces = func() ([]Interface, error) { return ifs, nil }
	t.Cleanup(m.Close)
	return m, c
}

var testIfs = []Interface{
	{Name: "lo", Address: "127.0.0.1", Kind: "loopback"},
	{Name: "eth0", Address: "192.168.1.20", Kind: "private"},
	{Name: "tailscale0", Address: "100.101.102.103", Kind: "tailnet"},
	{Name: "weird", Address: "0.0.0.0", Kind: "other"},
}

func TestNeverBindsWhenDisabled(t *testing.T) {
	m, c := testManager(t, testIfs)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	if len(c.addrs) != 0 || m.Status().Listening {
		t.Fatalf("bound with no settings: %v", c.addrs)
	}
	if _, err := m.Apply(Settings{Enabled: false, Mode: ModeLAN, Address: "192.168.1.20", Port: 7499}); err != nil {
		t.Fatal(err)
	}
	if len(c.addrs) != 0 {
		t.Fatalf("bound while disabled: %v", c.addrs)
	}
	// A fresh manager reading the saved, disabled settings binds nothing either.
	m2 := NewManager(m.Dir)
	m2.Listen, m2.Interfaces = c.listen, m.Interfaces
	if err := m2.Start(); err != nil || len(c.addrs) != 0 {
		t.Fatalf("bound on start: %v %v", err, c.addrs)
	}
}

func TestDefaultNeverBindsAllInterfaces(t *testing.T) {
	if s := DefaultSettings(); s.Enabled || s.Address != "" {
		t.Fatalf("default settings: %+v", s)
	}
	m, c := testManager(t, testIfs)
	// Enabled with the defaults: no interface chosen, so nothing binds.
	if _, err := m.Apply(Settings{Enabled: true}); err == nil || len(c.addrs) != 0 {
		t.Fatalf("enabled with no address: %v %v", err, c.addrs)
	}
	for _, a := range []string{"0.0.0.0", "::", "224.0.0.1"} {
		if _, err := m.Apply(Settings{Enabled: true, Mode: ModeLAN, Address: a, Port: 7499}); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
	if len(c.addrs) != 0 {
		t.Fatalf("bound: %v", c.addrs)
	}
	// Even if settings.json is edited by hand to 0.0.0.0, Start refuses it.
	if err := os.WriteFile(filepath.Join(m.Dir, "settings.json"), []byte(`{"enabled":true,"mode":"lan","address":"0.0.0.0","port":7499}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(); err == nil || len(c.addrs) != 0 {
		t.Fatalf("hand-edited 0.0.0.0 bound: %v %v", err, c.addrs)
	}
}

func TestBindsChosenAndTailnet(t *testing.T) {
	m, c := testManager(t, testIfs)
	st, err := m.Apply(Settings{Enabled: true, Mode: ModeLAN, Address: "192.168.1.20", Port: 7499})
	if err != nil || !st.Listening {
		t.Fatalf("lan: %v %+v", err, st)
	}
	if st, err = m.Apply(Settings{Enabled: true, Mode: ModeTailnet, Port: 7499}); err != nil || !st.Listening {
		t.Fatalf("tailnet: %v %+v", err, st)
	}
	if st.Tailnet != "100.101.102.103" {
		t.Fatalf("tailnet address: %q", st.Tailnet)
	}
	want := []string{"192.168.1.20:7499", "100.101.102.103:7499"}
	if strings.Join(c.addrs, ",") != strings.Join(want, ",") {
		t.Fatalf("binds %v, want %v", c.addrs, want)
	}
	// Not an address of this machine.
	if _, err := m.Apply(Settings{Enabled: true, Mode: ModeLAN, Address: "10.9.9.9", Port: 7499}); err == nil {
		t.Fatal("bound a foreign address")
	}
	// Tailnet mode without tailscale.
	m2, c2 := testManager(t, testIfs[:2])
	if _, err := m2.Apply(Settings{Enabled: true, Mode: ModeTailnet, Port: 7499}); err == nil || len(c2.addrs) != 0 {
		t.Fatal("tailnet mode bound without a tailnet address")
	}
}

func TestIsTailnet(t *testing.T) {
	for ip, want := range map[string]bool{"100.64.0.1": true, "100.127.255.254": true, "100.63.255.255": false, "100.128.0.1": false, "192.168.1.1": false} {
		if got := IsTailnet(net.ParseIP(ip)); got != want {
			t.Errorf("%s: %v", ip, got)
		}
	}
}

func TestQRSVG(t *testing.T) {
	svg, err := QRSVG("http://192.168.1.20:7421/r#pair=ABC")
	if err != nil || !strings.HasPrefix(svg, "<svg") || !strings.Contains(svg, "M4 4h1v1h-1z") {
		t.Fatalf("svg: %v %.80s", err, svg)
	}
}
