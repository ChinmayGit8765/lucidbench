package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The CDP client is small on purpose: Chrome's DevTools protocol is JSON
// messages over one WebSocket per target, and this package needs about ten
// methods. golang.org/x/net/websocket was already in the module graph, so
// there is no new module and the binary grows by the websocket package only.

// maxMessage caps one CDP message. A screencast frame or a screenshot is a
// few hundred KB of base64.
const maxMessage = 32 << 20

// RPCError is an error Chrome answered a call with.
type RPCError struct {
	Method  string
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("%s: %s (code %d)", e.Method, e.Message, e.Code)
}

// ErrClosed is returned by calls on a connection that has ended.
var ErrClosed = errors.New("the browser connection closed")

type reply struct {
	result json.RawMessage
	err    error
}

// Conn is one CDP WebSocket. Calls and events are matched by the read loop.
type Conn struct {
	ws     *wsConn
	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan reply
	handler func(method string, params json.RawMessage)

	done chan struct{}
	err  error // why the read loop ended; set before done closes
}

// Dial opens a CDP connection. No Origin header is sent, so Chrome needs no
// --remote-allow-origins flag (see ws.go).
func Dial(ctx context.Context, wsURL string) (*Conn, error) {
	ws, err := wsDial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	c := &Conn{ws: ws, pending: map[int64]chan reply{}, done: make(chan struct{})}
	go c.readLoop()
	return c, nil
}

// OnEvent sets the function that receives protocol events. Set it before the
// first call that enables events. It runs on the read loop, so it must not
// wait for a call's answer; use Notify for acknowledgements.
func (c *Conn) OnEvent(fn func(method string, params json.RawMessage)) {
	c.mu.Lock()
	c.handler = fn
	c.mu.Unlock()
}

// Done is closed when the connection has ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err says why the connection ended, or nil while it is open.
func (c *Conn) Err() error {
	select {
	case <-c.done:
		return c.err
	default:
		return nil
	}
}

// Close ends the connection.
func (c *Conn) Close() { c.ws.close() }

type message struct {
	ID     int64           `json:"id"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *Conn) readLoop() {
	var err error
	for {
		var raw []byte
		if raw, err = c.ws.readMessage(); err != nil {
			break
		}
		var m message
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		if m.Method != "" && m.ID == 0 {
			c.mu.Lock()
			h := c.handler
			c.mu.Unlock()
			if h != nil {
				h(m.Method, m.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.pending[m.ID]
		delete(c.pending, m.ID)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		r := reply{result: m.Result}
		if m.Error != nil {
			r.err = &RPCError{Code: m.Error.Code, Message: m.Error.Message}
		}
		ch <- r
	}
	c.mu.Lock()
	c.err = ErrClosed
	if err != nil && !errors.Is(err, net.ErrClosed) {
		c.err = fmt.Errorf("%w: %v", ErrClosed, err)
	}
	for id, ch := range c.pending {
		ch <- reply{err: c.err}
		delete(c.pending, id)
	}
	// Closed under the lock, so a Call that registers after the drain sees it.
	close(c.done)
	c.mu.Unlock()
}

func (c *Conn) send(id int64, method string, params any) error {
	b, err := json.Marshal(struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{id, method, params})
	if err != nil {
		return err
	}
	return c.ws.writeText(b)
}

// Call sends a method and waits for its answer.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	ch := make(chan reply, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, c.err
	default:
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(id, method, params); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		var rpc *RPCError
		if errors.As(r.err, &rpc) {
			rpc.Method = method
		}
		return r.result, r.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Notify sends a method and does not wait for the answer.
func (c *Conn) Notify(method string, params any) error {
	return c.send(c.nextID.Add(1), method, params)
}

// ---- the DevTools HTTP endpoint ----

// Target is one tab (a "page" target).
type Target struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
	// WSURL is the target's own socket, rewritten to the address Lucidbench
	// reaches the container on.
	WSURL string `json:"-"`
}

// Version is what /json/version says.
type Version struct {
	Browser string `json:"browser"`
	// WSURL is the browser-level socket: what LUCID_BROWSER_CDP holds.
	WSURL string `json:"-"`
}

// Endpoint is the DevTools HTTP address of the container: http://127.0.0.1:<port>.
type Endpoint struct {
	Base string
	HTTP *http.Client
}

func (e Endpoint) client() *http.Client {
	if e.HTTP != nil {
		return e.HTTP
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func (e Endpoint) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, e.Base+path, nil)
	if err != nil {
		return err
	}
	resp, err := e.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the browser answered %s to %s", resp.Status, path)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(out)
}

// fix points a ws:// address the container reported (0.0.0.0:9222, or the
// container's own port) at the address Lucidbench reaches it on.
func (e Endpoint) fix(ws string) string {
	b, err := url.Parse(e.Base)
	if err != nil || ws == "" {
		return ws
	}
	u, err := url.Parse(ws)
	if err != nil {
		return ws
	}
	u.Host = b.Host
	return u.String()
}

// Version reads the browser's version and its browser-level socket.
func (e Endpoint) Version(ctx context.Context) (Version, error) {
	var v struct {
		Browser string `json:"Browser"`
		WS      string `json:"webSocketDebuggerUrl"`
	}
	if err := e.do(ctx, http.MethodGet, "/json/version", &v); err != nil {
		return Version{}, err
	}
	return Version{Browser: v.Browser, WSURL: e.fix(v.WS)}, nil
}

type rawTarget struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
	WS    string `json:"webSocketDebuggerUrl"`
}

func (e Endpoint) target(r rawTarget) Target {
	return Target{ID: r.ID, Title: r.Title, URL: r.URL, WSURL: e.fix(r.WS)}
}

// Targets lists the open tabs.
func (e Endpoint) Targets(ctx context.Context) ([]Target, error) {
	var raw []rawTarget
	if err := e.do(ctx, http.MethodGet, "/json/list", &raw); err != nil {
		return nil, err
	}
	out := []Target{}
	for _, r := range raw {
		if r.Type == "page" && r.WS != "" {
			out = append(out, e.target(r))
		}
	}
	return out, nil
}

// NewTarget opens a blank tab. The page is navigated over its own socket, so
// the URL never goes through the query string.
func (e Endpoint) NewTarget(ctx context.Context) (Target, error) {
	var r rawTarget
	if err := e.do(ctx, http.MethodPut, "/json/new?about:blank", &r); err != nil {
		return Target{}, err
	}
	return e.target(r), nil
}

// CloseTarget closes a tab.
func (e Endpoint) CloseTarget(ctx context.Context, id string) error {
	if id == "" || strings.ContainsAny(id, "/?#") {
		return errors.New("bad tab id")
	}
	return e.do(ctx, http.MethodGet, "/json/close/"+id, nil)
}
