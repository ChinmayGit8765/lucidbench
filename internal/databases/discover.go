package databases

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/docker"
)

// Container labels Lucidbench puts on what it starts, so the manager
// containers are found again after a restart and never listed as databases.
const (
	LabelManager = "lucidbench.manager"
	// LabelTest marks throwaway containers made by tests.
	LabelTest = "lucidbench.test"
)

// Discovered is a database container on the local Docker engine, running or
// stopped. Passwords are never read: the inspect template below prints only
// the four default database and user variables, and nothing else of the
// environment ever leaves docker.
type Discovered struct {
	// ID is "docker:<container name>".
	ID      string `json:"id"`
	Name    string `json:"name"`
	Engine  string `json:"engine"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Running bool   `json:"running"`
	// Host and Port are where the container is published on this machine;
	// Port is 0 when it publishes nothing, or when it is stopped and docker
	// picks the port at the next start.
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database,omitempty"`
	User     string `json:"user,omitempty"`
	// SavedAs is the id of a saved connection that points at this container.
	SavedAs string `json:"saved_as,omitempty"`
	Compose string `json:"compose_project,omitempty"`
}

// imageEngine maps the last path element of an image (without tag or digest)
// to an engine. It is an exact list, so postgres-exporter or redisinsight are
// not mistaken for databases.
var imageEngine = map[string]string{
	"postgres": Postgres, "postgresql": Postgres, "postgis": Postgres, "timescaledb": Postgres, "pgvector": Postgres,
	"mysql": MySQL, "mariadb": MySQL,
	"redis": Redis, "redis-stack": Redis, "redis-stack-server": Redis, "valkey": Redis,
	"mongo": Mongo, "mongodb": Mongo,
}

// EngineOfImage returns the engine an image runs, or "".
func EngineOfImage(image string) string {
	if i := strings.IndexByte(image, '@'); i >= 0 {
		image = image[:i]
	}
	if i := strings.LastIndexByte(image, '/'); i >= 0 {
		image = image[i+1:]
	}
	if i := strings.IndexByte(image, ':'); i >= 0 {
		image = image[:i]
	}
	return imageEngine[strings.ToLower(image)]
}

// inspectFormat prints, per container: id, the published ports now, the
// published ports it was created with, then each default-name variable as a
// tab-separated NAME=value. The filtering runs inside docker, so the other
// variables (passwords among them) are never sent to this process.
const inspectFormat = `{{.Id}}{{"\t"}}{{json .NetworkSettings.Ports}}{{"\t"}}{{json .HostConfig.PortBindings}}` +
	`{{range .Config.Env}}{{$p := split . "="}}{{$k := index $p 0}}` +
	`{{if or (eq $k "POSTGRES_DB") (eq $k "POSTGRES_USER") (eq $k "MYSQL_DATABASE") (eq $k "MYSQL_USER") (eq $k "MARIADB_DATABASE") (eq $k "MARIADB_USER")}}{{"\t"}}{{.}}{{end}}{{end}}`

// nameVars are the only variables ever read, in Go as well as in the template.
var nameVars = map[string]bool{
	"POSTGRES_DB": true, "POSTGRES_USER": true,
	"MYSQL_DATABASE": true, "MYSQL_USER": true,
	"MARIADB_DATABASE": true, "MARIADB_USER": true,
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type inspectInfo struct {
	ports map[string]int // container port ("5432/tcp") to published host port
	names map[string]string
}

// parseInspect reads the output of docker inspect with inspectFormat.
func parseInspect(out []byte) map[string]inspectInfo {
	res := map[string]inspectInfo{}
	for _, line := range strings.Split(string(out), "\n") {
		parts := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		info := inspectInfo{ports: map[string]int{}, names: map[string]string{}}
		// The created-with bindings first, then what is published now (which
		// wins, since a random port is only known once running).
		for _, js := range []string{parts[2], parts[1]} {
			var m map[string][]portBinding
			if json.Unmarshal([]byte(js), &m) != nil {
				continue
			}
			for cp, bs := range m {
				for _, b := range bs {
					if n, err := strconv.Atoi(b.HostPort); err == nil && n > 0 {
						info.ports[cp] = n
						break
					}
				}
			}
		}
		for _, kv := range parts[3:] {
			if k, v, ok := strings.Cut(kv, "="); ok && nameVars[k] {
				info.names[k] = v
			}
		}
		res[parts[0]] = info
	}
	return res
}

// defaults returns the default database and user an image creates.
func defaults(engine string, names map[string]string) (db, user string) {
	switch engine {
	case Postgres:
		user = firstNonEmpty(names["POSTGRES_USER"], "postgres")
		db = firstNonEmpty(names["POSTGRES_DB"], user)
	case MySQL:
		user = firstNonEmpty(names["MYSQL_USER"], names["MARIADB_USER"], "root")
		db = firstNonEmpty(names["MYSQL_DATABASE"], names["MARIADB_DATABASE"])
	}
	return db, user
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// Discover lists the database containers, running or stopped, sorted with
// the running ones first.
func Discover(ctx context.Context, run docker.Func) ([]Discovered, error) {
	out, err := run(ctx, "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	cs, err := docker.ParsePS(out)
	if err != nil {
		return nil, err
	}
	var found []docker.Container
	for _, c := range cs {
		if EngineOfImage(c.Image) != "" {
			found = append(found, c)
		}
	}
	// Labels are not kept by ParsePS, and the managers' own containers are
	// recognised by their name prefix instead.
	kept := found[:0:0]
	for _, c := range found {
		if !strings.HasPrefix(c.Name, managerPrefix) {
			kept = append(kept, c)
		}
	}
	found = kept
	info := map[string]inspectInfo{}
	if len(found) > 0 {
		args := []string{"inspect", "--format", inspectFormat}
		for _, c := range found {
			args = append(args, c.ID)
		}
		// Ports and names are a nicety; the list stands without them.
		if o, err := run(ctx, args...); err == nil {
			info = parseInspect(o)
		}
	}
	list := []Discovered{}
	for _, c := range found {
		engine := EngineOfImage(c.Image)
		in := info[c.ID]
		d := Discovered{
			ID: "docker:" + c.Name, Name: c.Name, Engine: engine, Image: c.Image,
			State: c.State, Running: c.State == "running", Host: "127.0.0.1", Compose: c.Project,
		}
		d.Port = in.ports[strconv.Itoa(DefaultPort(engine))+"/tcp"]
		d.Database, d.User = defaults(engine, in.names)
		list = append(list, d)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Running != list[j].Running {
			return list[i].Running
		}
		return list[i].Name < list[j].Name
	})
	return list, nil
}

// MarkSaved sets SavedAs on discovered containers that a saved profile
// points at: the same engine on a loopback host and the same port.
func MarkSaved(ds []Discovered, saved []Profile) {
	for i := range ds {
		for _, p := range saved {
			if p.Engine == ds[i].Engine && ds[i].Port != 0 && p.Port == ds[i].Port && isLoopback(p.Host) {
				ds[i].SavedAs = p.ID
				break
			}
		}
	}
}

func isLoopback(host string) bool {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0":
		return true
	}
	return false
}
