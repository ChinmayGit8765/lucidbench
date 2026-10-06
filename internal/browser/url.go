package browser

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ErrURL means a URL was refused.
var ErrURL = errors.New("bad url")

// DockerHost is how the container reaches this machine.
const DockerHost = "host.docker.internal"

// CheckURL accepts only http and https URLs with a host. file:, chrome:,
// devtools:, javascript:, data: and a string with no scheme are refused
// before anything reaches the browser. It returns the URL to open: a
// loopback host (a dev server on this machine, such as localhost:5173)
// becomes host.docker.internal, because inside the container "localhost"
// is the container itself.
func CheckURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errorf("enter a URL")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return "", errorf("the URL has a control character")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errorf("%v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "":
		return "", errorf("%q has no scheme; start it with http:// or https://", raw)
	default:
		return "", errorf("only http and https URLs open here, not %s:", strings.ToLower(u.Scheme))
	}
	if u.Hostname() == "" {
		return "", errorf("%q has no host", raw)
	}
	if isLoopback(u.Hostname()) {
		if p := u.Port(); p != "" {
			u.Host = net.JoinHostPort(DockerHost, p)
		} else {
			u.Host = DockerHost
		}
	}
	return u.String(), nil
}

func errorf(format string, a ...any) error {
	return wrapErr{msg: fmt.Sprintf(format, a...)}
}

type wrapErr struct{ msg string }

func (e wrapErr) Error() string { return e.msg }
func (e wrapErr) Unwrap() error { return ErrURL }

// isLoopback reports a host that means "this machine".
func isLoopback(h string) bool {
	h = strings.ToLower(strings.Trim(h, "[]"))
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "0.0.0.0" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
