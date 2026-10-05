package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

const psOut = `{"ID":"a1","Names":"lucidbench-lucidd-1","Image":"lucidbench/lucidd:dev","State":"running","Status":"Up 2 hours","Ports":"127.0.0.1:7420->7420/tcp","CreatedAt":"2026-10-03 15:10:53 +1000 AEST","Labels":"com.docker.compose.project=lucidbench,com.docker.compose.service=lucidd,com.docker.compose.project.working_dir=/home/you/lucidbench"}
{"ID":"b2","Names":"lucidbench-control-plane","Image":"kindest/node:v1","State":"running","Status":"Up 5 days","Ports":"127.0.0.1:6443->6443/tcp","Labels":"io.x-k8s.kind.cluster=lucidbench,io.x-k8s.kind.role=control-plane"}
{"ID":"c3","Names":"ci-runner-1","Image":"example/github-runner:latest","State":"exited","Status":"Exited (0) 1 hour ago","Labels":"com.docker.compose.project=ci"}
{"ID":"d4","Names":"shop-db-1","Image":"postgres:17","State":"running","Status":"Up 1 day","Labels":"com.docker.compose.project=shop"}
{"ID":"e5","Names":"loner","Image":"busybox","State":"exited","Status":"Exited (0)","Labels":""}
`

const inspectOut = `[{"Id":"a1ffff","State":{"StartedAt":"2026-10-05T01:00:00Z"},"Config":{"Env":["SECRET=hunter2"]}},
{"Id":"e5ffff","State":{"StartedAt":"0001-01-01T00:00:00Z"}}]`

type fake struct {
	calls [][]string
}

func (f *fake) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "ps":
		return []byte(psOut), nil
	case "inspect":
		return []byte(inspectOut), nil
	case "stats":
		return []byte(`{"BlockIO":"1MB / 2MB","CPUPerc":"63.06%","ID":"b2","MemPerc":"4.96%","MemUsage":"781MiB / 15GiB","Name":"lucidbench-control-plane","NetIO":"3MB / 1MB","PIDs":"303"}` + "\n"), nil
	case "images":
		return []byte(`{"ID":"ac6c","Repository":"lucidbench/lucidd","Tag":"dev","Size":"153MB","CreatedAt":"2026-10-03","CreatedSince":"2 days ago"}` + "\n"), nil
	case "volume":
		return []byte(`{"Driver":"local","Labels":"com.docker.compose.project=shop,secret.path=/home/you/x","Mountpoint":"/var/lib/docker/volumes/v/_data","Name":"shop_data","Scope":"local"}` + "\n" +
			`{"Driver":"local","Labels":"com.docker.volume.anonymous=","Name":"8d94","Scope":"local"}` + "\n"), nil
	}
	return nil, nil
}

func (f *fake) acted() [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[0] == "start" || c[0] == "stop" || c[0] == "restart" {
			out = append(out, c)
		}
	}
	return out
}

var policy = Policy{
	Projects: []string{"lucidbench"},
	IsRunner: func(_, image string) bool { return strings.Contains(image, "github-runner") },
}

func byName(cs []Container, name string) Container {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return Container{}
}

func TestListAllowedActions(t *testing.T) {
	cs, err := List(context.Background(), (&fake{}).run, policy)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"lucidbench-lucidd-1":      {"stop", "restart"},
		"lucidbench-control-plane": {"restart"},
		"ci-runner-1":              {"start"},
		"shop-db-1":                {},
		"loner":                    {},
	}
	for name, acts := range want {
		if got := byName(cs, name).Actions; !slices.Equal(got, acts) {
			t.Errorf("%s actions = %v, want %v", name, got, acts)
		}
	}
	if c := byName(cs, "lucidbench-lucidd-1"); c.StartedAt != "2026-10-05T01:00:00Z" || c.Service != "lucidd" {
		t.Errorf("lucidd = %+v", c)
	}
	if byName(cs, "loner").StartedAt != "" {
		t.Error("zero start time should be dropped")
	}
	// Extra allowed projects come from config.
	p := policy
	p.Projects = append(p.Projects, "shop")
	cs, _ = List(context.Background(), (&fake{}).run, p)
	if got := byName(cs, "shop-db-1").Actions; !slices.Equal(got, []string{"stop", "restart"}) {
		t.Errorf("allowed project actions = %v", got)
	}
}

func TestNoLabelsOrEnvLeak(t *testing.T) {
	cs, _ := List(context.Background(), (&fake{}).run, policy)
	b, _ := json.Marshal(GroupContainers(cs))
	for _, bad := range []string{"working_dir", "/home/you", "hunter2", "SECRET"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("container JSON leaks %q: %s", bad, b)
		}
	}
	vs, err := Volumes(context.Background(), (&fake{}).run)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(vs)
	if strings.Contains(string(b), "/home/you") || strings.Contains(string(b), "/var/lib/docker") {
		t.Errorf("volume JSON leaks paths: %s", b)
	}
	if vs[0].Project != "shop" || vs[1].Anonymous != true || vs[0].Anonymous {
		t.Errorf("volumes = %+v", vs)
	}
}

func TestGroupContainers(t *testing.T) {
	cs, _ := List(context.Background(), (&fake{}).run, policy)
	gs := GroupContainers(cs)
	var got []string
	for _, g := range gs {
		got = append(got, g.Kind+":"+g.Project)
	}
	want := []string{"compose:ci", "compose:lucidbench", "compose:shop", "kind:lucidbench", "standalone:"}
	if !slices.Equal(got, want) {
		t.Errorf("groups = %v, want %v", got, want)
	}
}

func TestActRefusesOutsidePolicy(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, action string
		want         error
	}{
		{"shop-db-1", "stop", ErrRefused},
		{"loner", "start", ErrRefused},
		{"lucidbench-control-plane", "stop", ErrRefused},
		{"ci-runner-1", "stop", ErrRefused}, // already stopped: only start is offered
		{"nope", "start", ErrNotFound},
		{"../etc", "start", ErrNotFound},
		{"lucidbench-lucidd-1", "rm", ErrBadAction},
	}
	for _, c := range cases {
		f := &fake{}
		if err := Act(ctx, f.run, policy, c.name, c.action); !errors.Is(err, c.want) {
			t.Errorf("Act(%s, %s) = %v, want %v", c.name, c.action, err, c.want)
		}
		if len(f.acted()) != 0 {
			t.Errorf("Act(%s, %s) ran %v", c.name, c.action, f.acted())
		}
	}
	for _, c := range [][2]string{{"lucidbench-lucidd-1", "restart"}, {"lucidbench-control-plane", "restart"}, {"ci-runner-1", "start"}} {
		f := &fake{}
		if err := Act(ctx, f.run, policy, c[0], c[1]); err != nil {
			t.Errorf("Act(%v) = %v", c, err)
		}
		if a := f.acted(); len(a) != 1 || a[0][0] != c[1] || a[0][1] != c[0] {
			t.Errorf("Act(%v) ran %v", c, a)
		}
	}
}

func TestStatsAndImages(t *testing.T) {
	ss, err := Stats(context.Background(), (&fake{}).run)
	if err != nil || len(ss) != 1 || ss[0].CPUPerc != "63.06%" || ss[0].Name != "lucidbench-control-plane" {
		t.Fatalf("stats = %+v, %v", ss, err)
	}
	is, err := Images(context.Background(), (&fake{}).run)
	if err != nil || len(is) != 1 || is[0].Tag != "dev" {
		t.Fatalf("images = %+v, %v", is, err)
	}
}

func TestHTTP(t *testing.T) {
	f := &fake{}
	mux := http.NewServeMux()
	Register(mux, &Service{Docker: f.run, Policy: policy})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/docker/containers", nil))
	var body ContainersResponse
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Total != 5 || body.Running != 3 {
		t.Fatalf("containers: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/docker/containers/lucidbench-lucidd-1/restart", nil))
	if rec.Code != http.StatusForbidden || len(f.acted()) != 0 {
		t.Fatalf("POST without confirm: %d, acted %v", rec.Code, f.acted())
	}

	for path, code := range map[string]int{
		"/api/docker/containers/lucidbench-lucidd-1/restart": 200,
		"/api/docker/containers/shop-db-1/stop":              403,
		"/api/docker/containers/nope/start":                  404,
		"/api/docker/containers/loner/delete":                400,
	} {
		req := httptest.NewRequest("POST", path, nil)
		req.Header.Set("X-Lucid-Confirm", "yes")
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != code {
			t.Errorf("POST %s = %d, want %d (%s)", path, rec.Code, code, rec.Body)
		}
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/docker/containers/x/logs?tail=0", nil))
	if rec.Code != 400 {
		t.Errorf("tail=0: %d", rec.Code)
	}
}

func TestHTTPDockerDown(t *testing.T) {
	mux := http.NewServeMux()
	down := func(context.Context, ...string) ([]byte, error) { return nil, errors.New("docker ps: cannot connect") }
	Register(mux, &Service{Docker: down, Policy: policy})
	for _, p := range []string{"/api/docker/containers", "/api/docker/stats", "/api/docker/images", "/api/docker/volumes"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s = %d, want 503", p, rec.Code)
		}
	}
}
