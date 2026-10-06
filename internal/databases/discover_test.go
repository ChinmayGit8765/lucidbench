package databases

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const psOut = `{"ID":"aa11","Names":"shop-db-1","Image":"postgres:16-alpine","State":"running","Status":"Up 1 hour","Ports":"0.0.0.0:55432->5432/tcp","Labels":"com.docker.compose.project=shop,com.docker.compose.service=db"}
{"ID":"bb22","Names":"cache","Image":"docker.io/library/redis:7-alpine","State":"exited","Status":"Exited (0) 2 days ago","Ports":"","Labels":""}
{"ID":"cc33","Names":"blog-mysql","Image":"mariadb:11","State":"running","Status":"Up 3 hours","Ports":"3306/tcp, 127.0.0.1:53306->3306/tcp","Labels":""}
{"ID":"dd44","Names":"docs","Image":"mongo@sha256:abcdef","State":"running","Status":"Up 3 hours","Ports":"","Labels":""}
{"ID":"ee55","Names":"pg-metrics","Image":"quay.io/prometheuscommunity/postgres-exporter:v0.15","State":"running","Status":"Up","Labels":""}
{"ID":"ff66","Names":"web","Image":"nginx:1","State":"running","Status":"Up","Labels":""}
{"ID":"gg77","Names":"lucidbench-mgr-shop","Image":"sosedoff/pgweb:latest","State":"running","Status":"Up","Labels":"lucidbench.manager=shop"}
`

// inspectOut is what docker prints for inspectFormat. The password variable
// is here on purpose: the template would not print it, and if a docker that
// ignores the filter ever did, Go must not pass it on.
var inspectOut = strings.Join([]string{
	"aa11\t" + `{"5432/tcp":[{"HostIp":"0.0.0.0","HostPort":"55432"}]}` + "\t" + `{"5432/tcp":[{"HostIp":"","HostPort":""}]}` + "\tPOSTGRES_USER=shop\tPOSTGRES_DB=shopdb\tPOSTGRES_PASSWORD=PASSWORD-SENTINEL-1",
	"bb22\t{}\t" + `{"6379/tcp":[{"HostIp":"127.0.0.1","HostPort":"56379"}]}`,
	"cc33\t" + `{"3306/tcp":[{"HostIp":"127.0.0.1","HostPort":"53306"}]}` + "\t{}\tMYSQL_DATABASE=blog\tMYSQL_USER=blogger",
	"dd44\t" + `{"27017/tcp":null}` + "\t{}",
	"",
}, "\n")

func fakeDocker(t *testing.T) func(context.Context, ...string) ([]byte, error) {
	t.Helper()
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "ps":
			return []byte(psOut), nil
		case "inspect":
			if args[1] != "--format" {
				t.Errorf("inspect must use the filtering --format, got %v", args)
			}
			return []byte(inspectOut), nil
		}
		return nil, nil
	}
}

func TestEngineOfImage(t *testing.T) {
	for image, want := range map[string]string{
		"postgres:16-alpine":              Postgres,
		"postgres":                        Postgres,
		"docker.io/bitnami/postgresql:16": Postgres,
		"postgis/postgis:16-3.4":          Postgres,
		"mysql:8":                         MySQL,
		"mariadb:11":                      MySQL,
		"redis:7-alpine":                  Redis,
		"valkey/valkey:8":                 Redis,
		"mongo@sha256:abc":                Mongo,
		"localhost:5000/team/mongo:7":     Mongo,
		"quay.io/prometheuscommunity/postgres-exporter":  "",
		"redislabs/redisinsight:latest":                  "",
		"nginx":                                          "",
		"sosedoff/pgweb":                                 "",
		"myregistry.example.com:5000/app/postgres-admin": "",
	} {
		if got := EngineOfImage(image); got != want {
			t.Errorf("EngineOfImage(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestDiscover(t *testing.T) {
	ds, err := Discover(context.Background(), fakeDocker(t))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Discovered{}
	var names []string
	for _, d := range ds {
		by[d.Name] = d
		names = append(names, d.Name)
	}
	if len(ds) != 4 {
		t.Fatalf("want the 4 database containers (not the exporter, nginx or the manager), got %v", names)
	}
	// Running first, then by name.
	if want := []string{"blog-mysql", "docs", "shop-db-1", "cache"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", names, want)
	}
	pg := by["shop-db-1"]
	if pg.ID != "docker:shop-db-1" || pg.Engine != Postgres || !pg.Running || pg.Port != 55432 || pg.User != "shop" || pg.Database != "shopdb" || pg.Compose != "shop" {
		t.Errorf("postgres = %+v", pg)
	}
	// A stopped container with a fixed binding still shows its port.
	if r := by["cache"]; r.Running || r.Port != 56379 || r.Engine != Redis || r.State != "exited" {
		t.Errorf("redis = %+v", r)
	}
	if m := by["blog-mysql"]; m.Port != 53306 || m.Database != "blog" || m.User != "blogger" {
		t.Errorf("mysql = %+v", m)
	}
	// No published port is port 0 and the defaults still apply.
	if g := by["docs"]; g.Port != 0 || g.Engine != Mongo {
		t.Errorf("mongo = %+v", g)
	}
	b, _ := json.Marshal(ds)
	if strings.Contains(string(b), "PASSWORD-SENTINEL-1") || strings.Contains(strings.ToLower(string(b)), "password") {
		t.Errorf("discovery output carries a password: %s", b)
	}
}

func TestDiscoverDefaults(t *testing.T) {
	db, user := defaults(Postgres, nil)
	if db != "postgres" || user != "postgres" {
		t.Errorf("postgres defaults = %q %q", db, user)
	}
	db, user = defaults(Postgres, map[string]string{"POSTGRES_USER": "app"})
	if db != "app" || user != "app" {
		t.Errorf("postgres with user only = %q %q (the database defaults to the user)", db, user)
	}
	if _, user = defaults(MySQL, nil); user != "root" {
		t.Errorf("mysql user = %q", user)
	}
}

func TestInspectTemplateNeverNamesAPassword(t *testing.T) {
	if strings.Contains(strings.ToUpper(inspectFormat), "PASSWORD") {
		t.Error("the inspect template must not mention a password variable")
	}
}

func TestMarkSaved(t *testing.T) {
	ds := []Discovered{{Name: "a", Engine: Postgres, Port: 55432}, {Name: "b", Engine: Redis, Port: 0}}
	MarkSaved(ds, []Profile{{ID: "mine", Engine: Postgres, Host: "localhost", Port: 55432}, {ID: "remote", Engine: Redis, Host: "cache.example.com", Port: 0}})
	if ds[0].SavedAs != "mine" || ds[1].SavedAs != "" {
		t.Errorf("SavedAs = %q, %q", ds[0].SavedAs, ds[1].SavedAs)
	}
}
