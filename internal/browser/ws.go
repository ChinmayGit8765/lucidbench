package browser

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// A WebSocket client of about a hundred lines, hand-rolled for one reason:
// Chrome refuses a connection that carries an Origin header unless it was
// started with --remote-allow-origins, and that flag would let any web page
// in the user's own browser drive the agent browser (the port is on
// loopback, but a page can still open a WebSocket to it). A client that sends
// no Origin needs no such flag. golang.org/x/net/websocket always sends one.
// Only text messages are used; there is no compression and no extension.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type wsConn struct {
	c  net.Conn
	br *bufio.Reader

	wmu sync.Mutex // writes are whole frames, from calls and from pongs
}

// wsDial opens a ws:// connection and does the opening handshake.
func wsDial(ctx context.Context, raw string) (*wsConn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" {
		return nil, fmt.Errorf("the browser address must be ws://, not %q", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "80")
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*wsConn, error) {
		c.Close()
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return fail(err)
	}
	k := base64.StdEncoding.EncodeToString(key[:])
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + k + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		return fail(err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodGet})
	if err != nil {
		return fail(err)
	}
	resp.Body.Close()
	sum := sha1.Sum([]byte(k + wsGUID))
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		return fail(fmt.Errorf("the browser refused the connection: %s", resp.Status))
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{c: c, br: br}, nil
}

func (w *wsConn) close() { _ = w.c.Close() }

// frame writes one masked frame, as a client must.
func (w *wsConn) frame(op byte, p []byte) error {
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n <= 0xffff:
		h = append(h, 0x80|126, byte(n>>8), byte(n))
	default:
		h = binary.BigEndian.AppendUint64(append(h, 0x80|127), uint64(n))
	}
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}
	out := make([]byte, 0, len(h)+4+len(p))
	out = append(append(out, h...), mask[:]...)
	for i, b := range p {
		out = append(out, b^mask[i%4])
	}
	w.wmu.Lock()
	defer w.wmu.Unlock()
	_, err := w.c.Write(out)
	return err
}

// writeText sends one text message.
func (w *wsConn) writeText(p []byte) error { return w.frame(0x1, p) }

var errWSClosed = errors.New("websocket closed")

// readMessage returns the next text or binary message, joining fragments.
// Pings are answered; a close frame ends the connection.
func (w *wsConn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.br, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.br, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.br, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > maxMessage || uint64(len(msg))+n > maxMessage {
			return nil, errors.New("a browser message is too large")
		}
		var mask [4]byte
		masked := h[1]&0x80 != 0
		if masked {
			if _, err := io.ReadFull(w.br, mask[:]); err != nil {
				return nil, err
			}
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.br, p); err != nil {
			return nil, err
		}
		if masked {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8:
			return nil, errWSClosed
		case 0x9:
			if err := w.frame(0xA, p); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		}
		msg = append(msg, p...)
		if fin {
			return msg, nil
		}
	}
}
