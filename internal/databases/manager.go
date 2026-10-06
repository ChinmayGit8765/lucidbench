package databases

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/docker"
	"github.com/ChinmayGit8765/lucidbench/internal/power"
)

// managerPrefix names every manager container: lucidbench-mgr-<connection id>.
const managerPrefix = "lucidbench-mgr-"

// DefaultManagerIdle is how long a manager runs after it was last opened.
const DefaultManagerIdle = 10 * time.Minute

// ManagerSpec describes an embedded manager: a web UI that runs as a
// container, bound to 127.0.0.1, and pulled from its registry the first time
// it is opened. Lucidbench does not redistribute these images.
type ManagerSpec struct {
	Engine string `json:"engine"`
	Name   string `json:"name"`
	Image  string `json:"image"`
	// Port is the port the image serves on inside the container.
	Port    int    `json:"-"`
	License string `json:"license"`
	// Writes says the tool can change data, so it is refused for a read-only
	// or prod connection.
	Writes bool `json:"writes"`
}

// ManagerSpecs lists the managers. Redis and Mongo have none: RedisInsight is
// not under a permissive licence, so they get the native view only.
func ManagerSpecs() []ManagerSpec {
	return []ManagerSpec{
		{Engine: Postgres, Name: "pgweb", Image: "sosedoff/pgweb:latest", Port: 8081, License: "MIT"},
		{Engine: MySQL, Name: "Adminer", Image: "adminer:latest", Port: 8080, License: "Apache-2.0 or GPL-2.0 (used under Apache-2.0)", Writes: true},
	}
}

// SpecFor returns the manager for an engine.
func SpecFor(engine string) (ManagerSpec, bool) {
	for _, s := range ManagerSpecs() {
		if s.Engine == engine {
			return s, true
		}
	}
	return ManagerSpec{}, false
}

// Errors from the manager actions.
var (
	ErrNoManager = errors.New("this engine has no embedded manager; use the query box")
	ErrManagerRW = errors.New("this manager can write, so it is not offered for a read-only or prod connection")
)

// ManagerStatus is the state of one connection's manager.
type ManagerStatus struct {
	Available bool   `json:"available"`
	Name      string `json:"name,omitempty"`
	Image     string `json:"image,omitempty"`
	License   string `json:"license,omitempty"`
	Running   bool   `json:"running"`
	URL       string `json:"url,omitempty"`
	// IdleMinutes is how long it runs after the last time it was opened.
	IdleMinutes int `json:"idle_minutes,omitempty"`
	// Note explains why it is not available.
	Note string `json:"note,omitempty"`
}

// Managers starts and stops the manager containers. It is also a
// power.Extra: the supervisor stops an idle one on its regular check, and
// "Sleep everything idle" stops them all.
type Managers struct {
	Docker    docker.Func
	Log       *power.ActivityLog
	IdleAfter time.Duration
	// Now, FreePort and Ready are replaced in tests.
	Now      func() time.Time
	FreePort func() (int, error)
	Ready    func(ctx context.Context, url string) error
	// TempDir holds the env file for the moment of `docker run`.
	TempDir string

	mu      sync.Mutex
	touched map[string]time.Time
}

func (m *Managers) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Managers) idle() time.Duration {
	if m.IdleAfter > 0 {
		return m.IdleAfter
	}
	return DefaultManagerIdle
}

func (m *Managers) touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.touched == nil {
		m.touched = map[string]time.Time{}
	}
	m.touched[id] = m.now()
}

func (m *Managers) record(e power.Entry) {
	if e.At.IsZero() {
		e.At = m.now()
	}
	if m.Log != nil {
		_ = m.Log.Append(e)
	}
}

func containerName(id string) string { return managerPrefix + id }

// running returns the host port of a running manager container, or 0.
func (m *Managers) running(ctx context.Context, spec ManagerSpec, id string) (int, error) {
	out, err := m.Docker(ctx, "ps", "--filter", "name=^"+containerName(id)+"$", "--format", "{{json .}}")
	if err != nil {
		return 0, err
	}
	cs, err := docker.ParsePS(out)
	if err != nil {
		return 0, err
	}
	for _, c := range cs {
		if c.Name == containerName(id) && c.State == "running" {
			po, err := m.Docker(ctx, "port", c.Name, strconv.Itoa(spec.Port)+"/tcp")
			if err != nil {
				return 0, err
			}
			// "127.0.0.1:49153", one line per binding.
			for _, line := range strings.Split(string(po), "\n") {
				if _, port, err := net.SplitHostPort(strings.TrimSpace(line)); err == nil {
					if n, err := strconv.Atoi(port); err == nil {
						return n, nil
					}
				}
			}
		}
	}
	return 0, nil
}

func managerURL(spec ManagerSpec, p Profile, port int) string {
	u := fmt.Sprintf("http://127.0.0.1:%d/", port)
	if spec.Engine == MySQL {
		q := url.Values{"server": {containerHost(p.Host) + ":" + strconv.Itoa(p.Port)}}
		if p.User != "" {
			q.Set("username", p.User)
		}
		if p.Database != "" {
			q.Set("db", p.Database)
		}
		u += "?" + q.Encode()
	}
	return u
}

// containerHost is how a container reaches a host the profile names: this
// machine's loopback becomes host.docker.internal.
func containerHost(host string) string {
	if isLoopback(host) {
		return "host.docker.internal"
	}
	return host
}

func (m *Managers) allowed(p Profile) (ManagerSpec, error) {
	spec, ok := SpecFor(p.Engine)
	if !ok {
		return spec, ErrNoManager
	}
	if spec.Writes && (p.Readonly || p.Prod) {
		return spec, ErrManagerRW
	}
	return spec, nil
}

// Status reports a connection's manager without starting anything or
// counting as activity.
func (m *Managers) Status(ctx context.Context, p Profile) (ManagerStatus, error) {
	spec, err := m.allowed(p)
	st := ManagerStatus{Name: spec.Name, Image: spec.Image, License: spec.License, IdleMinutes: int(m.idle().Minutes())}
	if err != nil {
		st.Note = err.Error()
		if errors.Is(err, ErrNoManager) {
			st.Name, st.Image, st.License, st.IdleMinutes = "", "", "", 0
		}
		return st, nil
	}
	st.Available = true
	port, err := m.running(ctx, spec, p.ID)
	if err != nil {
		return st, err
	}
	if port > 0 {
		st.Running, st.URL = true, managerURL(spec, p, port)
	}
	return st, nil
}

// Start runs the manager for a connection, or, when it is already up, counts
// the call as activity (the page repeats it while the frame is open, which
// is what keeps the manager awake).
func (m *Managers) Start(ctx context.Context, p Profile, password string) (ManagerStatus, error) {
	spec, err := m.allowed(p)
	if err != nil {
		return ManagerStatus{}, err
	}
	st, err := m.Status(ctx, p)
	if err != nil {
		return st, err
	}
	if st.Running {
		m.touch(p.ID)
		return st, nil
	}
	if strings.ContainsAny(password, "\r\n\x00") {
		return st, errors.New("the password has a line break, which a container environment cannot hold")
	}
	free := m.FreePort
	if free == nil {
		free = freePort
	}
	port, err := free()
	if err != nil {
		return st, err
	}
	envLines := ""
	if spec.Engine == Postgres {
		u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(containerHost(p.Host), strconv.Itoa(p.Port)), Path: "/" + p.Database}
		if p.User != "" {
			u.User = url.UserPassword(p.User, password)
		}
		mode := "disable"
		if !isLoopback(p.Host) {
			mode = "require"
		}
		u.RawQuery = "sslmode=" + mode
		envLines = "PGWEB_DATABASE_URL=" + u.String() + "\n"
	}
	// The connection string goes in through an env file that exists only for
	// the moment of `docker run`, so it is not on the command line.
	dir := m.TempDir
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "lucidbench-mgr-*.env")
	if err != nil {
		return st, err
	}
	envPath := f.Name()
	defer os.Remove(envPath)
	if _, err := f.WriteString(envLines); err != nil {
		f.Close()
		return st, err
	}
	if err := f.Close(); err != nil {
		return st, err
	}
	args := []string{
		"run", "-d", "--rm", "--name", containerName(p.ID),
		"--label", LabelManager + "=" + p.ID,
		"--memory", "256m",
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", port, spec.Port),
		"--add-host", "host.docker.internal:host-gateway",
		"--env-file", filepath.ToSlash(envPath),
		spec.Image,
	}
	if spec.Engine == Postgres {
		args = append(args, "--bind=0.0.0.0", "--listen="+strconv.Itoa(spec.Port), "--readonly")
	}
	began := m.now()
	e := power.Entry{Kind: "manager", Name: containerName(p.ID), Action: "start", Reason: spec.Name + " opened from Lucidbench"}
	if _, err := m.Docker(ctx, args...); err != nil {
		e.Error = scrub(err.Error(), password)
		m.record(e)
		return st, errors.New(e.Error)
	}
	st.Running, st.URL = true, managerURL(spec, p, port)
	ready := m.Ready
	if ready == nil {
		ready = waitHTTP
	}
	if err := ready(ctx, st.URL); err != nil {
		e.Error = "started but did not answer: " + err.Error()
		e.Seconds = m.now().Sub(began).Seconds()
		m.record(e)
		_, _ = m.Docker(context.WithoutCancel(ctx), "stop", containerName(p.ID))
		return ManagerStatus{Available: true, Name: spec.Name, Image: spec.Image, License: spec.License}, errors.New(e.Error)
	}
	e.OK, e.Seconds = true, m.now().Sub(began).Seconds()
	m.record(e)
	m.touch(p.ID)
	return st, nil
}

// Stop stops a connection's manager; one that is not running is not an error.
func (m *Managers) Stop(ctx context.Context, id string) error {
	return m.stop(ctx, id, false, "stopped from Lucidbench")
}

func (m *Managers) stop(ctx context.Context, id string, auto bool, reason string) error {
	began := m.now()
	e := power.Entry{Kind: "manager", Name: containerName(id), Action: "stop", Auto: auto, Reason: reason}
	_, err := m.Docker(ctx, "stop", containerName(id))
	if err != nil && strings.Contains(err.Error(), "No such container") {
		return nil
	}
	e.OK, e.Seconds = err == nil, m.now().Sub(began).Seconds()
	if err != nil {
		e.Error = err.Error()
	}
	m.record(e)
	m.mu.Lock()
	delete(m.touched, id)
	m.mu.Unlock()
	return err
}

// runningIDs lists the connection ids that have a running manager.
func (m *Managers) runningIDs(ctx context.Context) ([]string, error) {
	out, err := m.Docker(ctx, "ps", "--filter", "label="+LabelManager, "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	cs, err := docker.ParsePS(out)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, c := range cs {
		if c.State == "running" && strings.HasPrefix(c.Name, managerPrefix) {
			ids = append(ids, strings.TrimPrefix(c.Name, managerPrefix))
		}
	}
	return ids, nil
}

// Tick stops managers that have not been opened for the idle time. One that
// the daemon did not start itself (it was restarted since) counts as opened
// now, so a restart never kills a manager at once.
func (m *Managers) Tick(ctx context.Context) {
	ids, err := m.runningIDs(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		m.mu.Lock()
		last, ok := m.touched[id]
		m.mu.Unlock()
		if !ok {
			m.touch(id)
			continue
		}
		if idle := m.now().Sub(last); idle >= m.idle() {
			_ = m.stop(ctx, id, true, fmt.Sprintf("idle for %d min", int(idle.Minutes())))
		}
	}
}

// Sleep stops every running manager.
func (m *Managers) Sleep(ctx context.Context) []power.Outcome {
	ids, err := m.runningIDs(ctx)
	if err != nil {
		return nil
	}
	var out []power.Outcome
	for _, id := range ids {
		o := power.Outcome{Kind: "manager", Name: containerName(id)}
		if err := m.stop(ctx, id, false, "sleep everything idle"); err != nil {
			o.Reason = err.Error()
		} else {
			o.Stopped, o.Reason = true, "stopped"
		}
		out = append(out, o)
	}
	return out
}

var _ power.Extra = (*Managers)(nil)

// freePort asks the OS for a free loopback port. Another process could take
// it before docker binds it; the start then fails with docker's own message.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitHTTP polls a URL until it answers, for up to 30 seconds.
func waitHTTP(ctx context.Context, u string) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			last = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return last
}
