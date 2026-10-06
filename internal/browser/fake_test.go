package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/ChinmayGit8765/lucidbench/internal/power"
)

// tinyPNG is a 1x1 PNG.
var tinyPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}

// fakeChrome serves what the DevTools HTTP endpoint and one page socket do:
// /json/version, /json/list, /json/new, /json/close, and a WebSocket that
// answers the methods this package calls and records what it was sent.
type fakeChrome struct {
	srv *httptest.Server

	mu     sync.Mutex
	calls  []call // every CDP request, in order
	acks   []int  // screencastFrameAck session ids
	closed []string
	tabs   []string // ids
	// burst is how many frames to send after startScreencast.
	burst int
	// navError is the errorText Page.navigate answers with.
	navError string
}

type call struct {
	Method string
	Params json.RawMessage
}

func newFakeChrome(t *testing.T) *fakeChrome {
	t.Helper()
	f := &fakeChrome{tabs: []string{"TAB1"}}
	mux := http.NewServeMux()
	// The ws URLs deliberately carry the wrong host and port, as a container's
	// 0.0.0.0:9222 would: the client must rewrite them.
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Browser":"Chrome/155.0.0.0","webSocketDebuggerUrl":"ws://0.0.0.0:9222/devtools/browser/BR"}`)
	})
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var parts []string
		for _, id := range f.tabs {
			parts = append(parts, fmt.Sprintf(`{"id":%q,"type":"page","title":"T","url":"https://example.com/","webSocketDebuggerUrl":"ws://0.0.0.0:9222/devtools/page/%s"}`, id, id))
		}
		parts = append(parts, `{"id":"SW","type":"service_worker","url":"x","webSocketDebuggerUrl":"ws://0.0.0.0:9222/devtools/page/SW"}`)
		fmt.Fprintf(w, "[%s]", strings.Join(parts, ","))
	})
	mux.HandleFunc("/json/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "use PUT", http.StatusMethodNotAllowed)
			return
		}
		f.mu.Lock()
		id := fmt.Sprintf("TAB%d", len(f.tabs)+1)
		f.tabs = append(f.tabs, id)
		f.mu.Unlock()
		fmt.Fprintf(w, `{"id":%q,"type":"page","url":"about:blank","webSocketDebuggerUrl":"ws://0.0.0.0:9222/devtools/page/%s"}`, id, id)
	})
	mux.HandleFunc("/json/close/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.closed = append(f.closed, strings.TrimPrefix(r.URL.Path, "/json/close/"))
		f.mu.Unlock()
		fmt.Fprint(w, "Target is closing")
	})
	mux.Handle("/devtools/page/", websocket.Server{
		// No Origin check: the client sends none.
		Handshake: func(c *websocket.Config, r *http.Request) error {
			if r.Header.Get("Origin") != "" {
				return errors.New("an Origin header was sent")
			}
			return nil
		},
		Handler: f.page,
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeChrome) endpoint() Endpoint { return Endpoint{Base: f.srv.URL} }

func (f *fakeChrome) page(ws *websocket.Conn) {
	for {
		var raw string
		if websocket.Message.Receive(ws, &raw) != nil {
			return
		}
		var m struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal([]byte(raw), &m)
		f.mu.Lock()
		f.calls = append(f.calls, call{m.Method, m.Params})
		burst, navErr := f.burst, f.navError
		f.mu.Unlock()
		result := `{}`
		switch m.Method {
		case "Page.navigate":
			result = `{"frameId":"F"}`
			if navErr != "" {
				result = fmt.Sprintf(`{"frameId":"F","errorText":%q}`, navErr)
			}
		case "Page.captureScreenshot":
			result = fmt.Sprintf(`{"data":%q}`, base64.StdEncoding.EncodeToString(tinyPNG))
		case "Page.getNavigationHistory":
			result = `{"currentIndex":1,"entries":[{"id":10},{"id":11},{"id":12}]}`
		case "Page.screencastFrameAck":
			var p struct {
				SessionID int `json:"sessionId"`
			}
			_ = json.Unmarshal(m.Params, &p)
			f.mu.Lock()
			f.acks = append(f.acks, p.SessionID)
			f.mu.Unlock()
			continue
		case "Input.dispatchMouseEvent":
			if strings.Contains(string(m.Params), `"x":-1`) {
				_ = websocket.Message.Send(ws, fmt.Sprintf(`{"id":%d,"error":{"code":-32602,"message":"bad coordinates"}}`, m.ID))
				continue
			}
		}
		_ = websocket.Message.Send(ws, fmt.Sprintf(`{"id":%d,"result":%s}`, m.ID, result))
		if m.Method == "Page.startScreencast" {
			for i := 1; i <= burst; i++ {
				_ = websocket.Message.Send(ws, fmt.Sprintf(
					`{"method":"Page.screencastFrame","params":{"data":"RlJBTUU%d","sessionId":%d,"metadata":{"deviceWidth":1280,"deviceHeight":800}}}`, i, i))
			}
		}
	}
}

func (f *fakeChrome) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.Method
	}
	return out
}

func (f *fakeChrome) find(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---- the docker side ----

type fakeDocker struct {
	mu      sync.Mutex
	calls   [][]string
	running bool
	port    string
	runErr  error
}

func (d *fakeDocker) run(_ context.Context, args ...string) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, args)
	switch args[0] {
	case "ps":
		if !d.running {
			return nil, nil
		}
		return []byte(`{"ID":"x","Names":"` + ContainerName + `","Image":"chromedp/headless-shell","State":"running","Labels":"lucidbench.browser=1"}` + "\n"), nil
	case "port":
		return []byte("0.0.0.0:" + d.port + "\n[::]:" + d.port + "\n"), nil
	case "run":
		if d.runErr != nil {
			return nil, d.runErr
		}
		d.running = true
		return []byte("cid\n"), nil
	case "stop":
		if !d.running {
			return nil, errors.New("docker stop: Error response from daemon: No such container: " + ContainerName)
		}
		d.running = false
		return []byte(ContainerName), nil
	}
	return nil, nil
}

func (d *fakeDocker) ran(verb string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, c := range d.calls {
		if c[0] == verb {
			n++
		}
	}
	return n
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newUp is newService with the browser started.
func newUp(t *testing.T, f *fakeChrome) (*Service, *fakeDocker, *clock, *power.ActivityLog) {
	t.Helper()
	s, d, c, l := newService(t, f)
	if _, err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, d, c, l
}

// newService is a Service on a fake docker whose published port is the fake
// Chrome's.
func newService(t *testing.T, f *fakeChrome) (*Service, *fakeDocker, *clock, *power.ActivityLog) {
	t.Helper()
	port := f.srv.Listener.Addr().String()[strings.LastIndex(f.srv.Listener.Addr().String(), ":")+1:]
	d := &fakeDocker{port: port}
	c := &clock{t: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)}
	log := &power.ActivityLog{Path: filepath.Join(t.TempDir(), "activity.jsonl")}
	s := &Service{
		Docker: d.run, Log: log, Now: c.now, ShotsDir: filepath.Join(t.TempDir(), "shots"),
		FreePort: func() (int, error) { return 49300, nil },
		Ready:    func(context.Context, Endpoint) error { return nil },
	}
	return s, d, c, log
}
