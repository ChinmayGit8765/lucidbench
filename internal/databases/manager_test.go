package databases

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/power"
)

// mgrDocker is a docker that remembers calls and a running manager.
type mgrDocker struct {
	calls   [][]string
	running map[string]bool // container name
	envBody string          // the env file's content at `docker run` time
	envPath string
	runErr  error
}

func (d *mgrDocker) run(_ context.Context, args ...string) ([]byte, error) {
	d.calls = append(d.calls, args)
	switch args[0] {
	case "ps":
		var b strings.Builder
		for n := range d.running {
			b.WriteString(`{"ID":"x","Names":"` + n + `","Image":"sosedoff/pgweb","State":"running","Labels":"lucidbench.manager=` + strings.TrimPrefix(n, managerPrefix) + `"}` + "\n")
		}
		return []byte(b.String()), nil
	case "port":
		return []byte("127.0.0.1:49153\n"), nil
	case "run":
		if d.runErr != nil {
			return nil, d.runErr
		}
		for i, a := range args {
			if a == "--env-file" {
				d.envPath = args[i+1]
				body, _ := os.ReadFile(d.envPath)
				d.envBody = string(body)
			}
			if a == "--name" {
				d.running[args[i+1]] = true
			}
		}
		return []byte("cid\n"), nil
	case "stop":
		if !d.running[args[1]] {
			return nil, errors.New("docker stop: Error response from daemon: No such container: " + args[1])
		}
		delete(d.running, args[1])
		return []byte(args[1]), nil
	}
	return nil, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newMgr(t *testing.T) (*Managers, *mgrDocker, *clock, *power.ActivityLog) {
	t.Helper()
	d := &mgrDocker{running: map[string]bool{}}
	c := &clock{t: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)}
	log := &power.ActivityLog{Path: filepath.Join(t.TempDir(), "activity.jsonl")}
	m := &Managers{
		Docker: d.run, Log: log, Now: c.now, TempDir: t.TempDir(),
		FreePort: func() (int, error) { return 49153, nil },
		Ready:    func(context.Context, string) error { return nil },
	}
	return m, d, c, log
}

var pgProfile = Profile{ID: "shop", Engine: Postgres, Host: "127.0.0.1", Port: 55432, Database: "shopdb", User: "shop", Readonly: true}

func TestManagerStartBindsLoopbackAndKeepsThePasswordOffTheCommandLine(t *testing.T) {
	m, d, _, log := newMgr(t)
	st, err := m.Start(context.Background(), pgProfile, secret)
	if err != nil || !st.Running || st.URL != "http://127.0.0.1:49153/" {
		t.Fatalf("start = %+v, %v", st, err)
	}
	var run []string
	for _, c := range d.calls {
		if c[0] == "run" {
			run = c
		}
	}
	joined := strings.Join(run, " ")
	for _, want := range []string{"-p 127.0.0.1:49153:8081", "--rm", "--name lucidbench-mgr-shop", "--label lucidbench.manager=shop", "sosedoff/pgweb:latest", "--readonly", "--add-host host.docker.internal:host-gateway"} {
		if !strings.Contains(joined, want) {
			t.Errorf("docker run is missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, secret) {
		t.Errorf("the password is on the command line: %s", joined)
	}
	if !strings.Contains(d.envBody, "PGWEB_DATABASE_URL=postgres://shop:"+secret+"@host.docker.internal:55432/shopdb?sslmode=disable") {
		t.Errorf("env file = %q", d.envBody)
	}
	if _, err := os.Stat(d.envPath); err == nil {
		t.Error("the env file must be deleted once docker run returns")
	}
	es, _ := log.Recent(5)
	if len(es) != 1 || es[0].Kind != "manager" || es[0].Action != "start" || !es[0].OK || strings.Contains(es[0].Reason+es[0].Error, secret) {
		t.Errorf("activity = %+v", es)
	}
}

func TestManagerRemoteHostAsksForTLS(t *testing.T) {
	m, d, _, _ := newMgr(t)
	p := pgProfile
	p.Host, p.Port = "db.example.com", 5432
	if _, err := m.Start(context.Background(), p, "pw"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(d.envBody, "@db.example.com:5432/shopdb?sslmode=require") {
		t.Errorf("env file = %q", d.envBody)
	}
}

func TestManagerSecondStartTouchesInsteadOfRunning(t *testing.T) {
	m, d, _, _ := newMgr(t)
	if _, err := m.Start(context.Background(), pgProfile, secret); err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, c := range d.calls {
		if c[0] == "run" {
			runs++
		}
	}
	if st, err := m.Start(context.Background(), pgProfile, secret); err != nil || !st.Running {
		t.Fatalf("second start: %+v %v", st, err)
	}
	after := 0
	for _, c := range d.calls {
		if c[0] == "run" {
			after++
		}
	}
	if runs != 1 || after != 1 {
		t.Errorf("docker run was called %d then %d times; an open manager is only touched", runs, after)
	}
}

func TestManagerIdleStop(t *testing.T) {
	m, d, c, log := newMgr(t)
	if _, err := m.Start(context.Background(), pgProfile, secret); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(9 * time.Minute)
	m.Tick(context.Background())
	if !d.running["lucidbench-mgr-shop"] {
		t.Fatal("stopped before the idle time")
	}
	// Opening it again restarts the clock.
	if _, err := m.Start(context.Background(), pgProfile, secret); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(9 * time.Minute)
	m.Tick(context.Background())
	if !d.running["lucidbench-mgr-shop"] {
		t.Fatal("stopped 9 minutes after it was last opened")
	}
	c.t = c.t.Add(2 * time.Minute)
	m.Tick(context.Background())
	if d.running["lucidbench-mgr-shop"] {
		t.Fatal("still running after the idle time")
	}
	es, _ := log.Recent(5)
	if len(es) < 2 || es[0].Action != "stop" || !es[0].Auto || !strings.Contains(es[0].Reason, "idle for") || es[0].Kind != "manager" {
		t.Errorf("activity = %+v", es)
	}
}

func TestManagerFoundAfterARestartGetsAFullIdleTime(t *testing.T) {
	m, d, c, _ := newMgr(t)
	d.running["lucidbench-mgr-orphan"] = true
	m.Tick(context.Background())
	if !d.running["lucidbench-mgr-orphan"] {
		t.Fatal("a manager the daemon did not start itself must not be stopped at once")
	}
	c.t = c.t.Add(11 * time.Minute)
	m.Tick(context.Background())
	if d.running["lucidbench-mgr-orphan"] {
		t.Fatal("it must stop after the idle time")
	}
}

func TestManagerSleepStopsAll(t *testing.T) {
	m, d, _, _ := newMgr(t)
	d.running["lucidbench-mgr-a"], d.running["lucidbench-mgr-b"] = true, true
	out := m.Sleep(context.Background())
	if len(out) != 2 || !out[0].Stopped || !out[1].Stopped || len(d.running) != 0 {
		t.Errorf("outcomes = %+v, running = %v", out, d.running)
	}
}

func TestManagerRules(t *testing.T) {
	m, _, _, _ := newMgr(t)
	for _, e := range []string{Redis, Mongo} {
		if _, err := m.Start(context.Background(), Profile{ID: "x", Engine: e, Host: "127.0.0.1"}, ""); !errors.Is(err, ErrNoManager) {
			t.Errorf("%s: %v (Redis and Mongo have no embedded manager)", e, err)
		}
	}
	my := Profile{ID: "blog", Engine: MySQL, Host: "127.0.0.1", Port: 3306, Readonly: true}
	if _, err := m.Start(context.Background(), my, "pw"); !errors.Is(err, ErrManagerRW) {
		t.Errorf("Adminer can write, so a read-only connection must refuse it: %v", err)
	}
	my.Prod = true
	my.Readonly = false
	if _, err := m.Start(context.Background(), my, "pw"); !errors.Is(err, ErrManagerRW) {
		t.Errorf("and so must a prod connection: %v", err)
	}
	st, err := m.Status(context.Background(), my)
	if err != nil || st.Available || st.Note == "" {
		t.Errorf("status = %+v, %v", st, err)
	}
}

func TestManagerAdminerURLPrefillsNothingSecret(t *testing.T) {
	m, d, _, _ := newMgr(t)
	my := Profile{ID: "blog", Engine: MySQL, Host: "localhost", Port: 3306, Database: "blog", User: "blogger"}
	st, err := m.Start(context.Background(), my, "pw")
	if err != nil {
		t.Fatal(err)
	}
	want := "http://127.0.0.1:49153/?db=blog&server=host.docker.internal%3A3306&username=blogger"
	if st.URL != want {
		t.Errorf("url = %s, want %s", st.URL, want)
	}
	if d.envBody != "" || strings.Contains(st.URL, "pw") {
		t.Errorf("Adminer gets no password: env=%q url=%s", d.envBody, st.URL)
	}
}

func TestManagerStartFailureIsScrubbedAndLogged(t *testing.T) {
	m, d, _, log := newMgr(t)
	d.runErr = errors.New("docker run: pull access denied for " + secret)
	_, err := m.Start(context.Background(), pgProfile, secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("err = %v", err)
	}
	es, _ := log.Recent(1)
	if len(es) != 1 || es[0].OK || strings.Contains(es[0].Error, secret) {
		t.Errorf("activity = %+v", es)
	}
}

func TestManagerNeverReadyIsStopped(t *testing.T) {
	m, d, _, _ := newMgr(t)
	m.Ready = func(context.Context, string) error { return errors.New("connection refused") }
	if _, err := m.Start(context.Background(), pgProfile, secret); err == nil {
		t.Fatal("want an error")
	}
	if len(d.running) != 0 {
		t.Errorf("a manager that never answered must not be left running: %v", d.running)
	}
}
