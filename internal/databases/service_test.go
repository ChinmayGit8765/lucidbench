package databases

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// secret is the sentinel password every test resolves. It is not key-shaped.
const secret = "PASSWORD-SENTINEL-9"

type fakeConn struct {
	queries []string
	err     error
}

func (f *fakeConn) Ping(context.Context) error { return f.err }
func (f *fakeConn) Info(context.Context) (Info, error) {
	return Info{Version: "PostgreSQL 16.1", SizeBytes: 8 << 20}, nil
}
func (f *fakeConn) Schema(context.Context) ([]Table, error) {
	n := int64(3)
	return []Table{{Schema: "public", Name: "t", Kind: "table", Rows: &n, Columns: []Column{{Name: "id", Type: "integer"}}}}, f.err
}
func (f *fakeConn) Query(_ context.Context, q string) (*Result, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return nil, f.err
	}
	return &Result{Columns: []string{"n"}, Rows: [][]string{{"1"}}, RowLimit: RowLimit}, nil
}
func (f *fakeConn) Close() {}

type env struct {
	svc    *Service
	conn   *fakeConn
	opened []string // passwords handed to the opener
	mux    *http.ServeMux
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{conn: &fakeConn{}}
	opener := func(_ context.Context, p Profile, pw string) (Conn, error) {
		e.opened = append(e.opened, pw)
		return e.conn, nil
	}
	e.svc = &Service{
		Store:  &Store{Path: filepath.Join(t.TempDir(), FileName)},
		Docker: fakeDocker(t),
		Getenv: func(k string) string {
			if k == "PG_PW" {
				return secret
			}
			return ""
		},
		Open:     map[string]Opener{Postgres: opener, MySQL: opener, Redis: opener, Mongo: opener},
		Managers: &Managers{Docker: fakeDocker(t)},
	}
	e.mux = http.NewServeMux()
	Register(e.mux, e.svc)
	return e
}

func (e *env) do(method, path, body string, confirm bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if confirm {
		r.Header.Set("X-Lucid-Confirm", "yes")
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

const saveBody = `{"id":"shop","engine":"postgres","host":"127.0.0.1","port":55432,"database":"shopdb","user":"shop","password":"env:PG_PW","readonly":true,"label":"Shop"}`

func TestSaveNeedsConfirmAndRejectsLiteralPasswords(t *testing.T) {
	e := newEnv(t)
	if w := e.do("POST", "/api/databases", saveBody, false); w.Code != 403 {
		t.Errorf("without the confirm header: %d", w.Code)
	}
	lit := strings.Replace(saveBody, "env:PG_PW", secret, 1)
	w := e.do("POST", "/api/databases", lit, true)
	if w.Code != 400 || strings.Contains(w.Body.String(), secret) {
		t.Errorf("a literal password: %d %q (must be 400 and never echoed)", w.Code, w.Body.String())
	}
	if l, _ := e.svc.Store.Load(); len(l) != 0 {
		t.Errorf("nothing may be saved: %v", l)
	}
	if w := e.do("POST", "/api/databases", saveBody, true); w.Code != 201 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/databases", saveBody, true); w.Code != 409 {
		t.Errorf("duplicate: %d", w.Code)
	}
}

func TestNoResponseCarriesAPassword(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", saveBody, true)
	for _, c := range []struct{ m, p, b string }{
		{"GET", "/api/databases", ""},
		{"GET", "/api/databases?health=1", ""},
		{"GET", "/api/databases/shop/health", ""},
		{"GET", "/api/databases/shop/schema", ""},
		{"POST", "/api/databases/shop/query", `{"query":"SELECT 1"}`},
		{"GET", "/api/databases/shop/manager", ""},
	} {
		w := e.do(c.m, c.p, c.b, true)
		if w.Code != 200 {
			t.Errorf("%s %s: %d %s", c.m, c.p, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("%s %s leaks the password: %s", c.m, c.p, w.Body)
		}
	}
	// The saved list shows the reference and whether it is set, nothing more.
	w := e.do("GET", "/api/databases", "", false)
	if !strings.Contains(w.Body.String(), `"password":"env:PG_PW"`) || !strings.Contains(w.Body.String(), `"password_set":true`) {
		t.Errorf("want the reference and password_set: %s", w.Body)
	}
	// The driver does get the resolved value.
	if len(e.opened) == 0 || e.opened[0] != secret {
		t.Errorf("opener passwords = %v", e.opened)
	}
}

func TestDriverErrorsAreScrubbed(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", saveBody, true)
	e.conn.err = fmt.Errorf(`dial postgres://shop:%s@127.0.0.1:55432/shopdb: refused`, secret)
	for _, p := range []string{"/api/databases/shop/health", "/api/databases?health=1"} {
		w := e.do("GET", p, "", false)
		if strings.Contains(w.Body.String(), secret) || !strings.Contains(w.Body.String(), "***") {
			t.Errorf("%s: %s", p, w.Body)
		}
	}
	w := e.do("GET", "/api/databases/shop/schema", "", false)
	if w.Code != 502 || strings.Contains(w.Body.String(), secret) {
		t.Errorf("schema error: %d %s", w.Code, w.Body)
	}
	w = e.do("POST", "/api/databases/shop/query", `{"query":"SELECT 1"}`, true)
	if w.Code != 502 || strings.Contains(w.Body.String(), secret) {
		t.Errorf("query error: %d %s", w.Code, w.Body)
	}
}

func TestQueryWritesAreRefusedBeforeConnecting(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", saveBody, true)
	for _, q := range []string{"INSERT INTO t VALUES (1)", "DROP TABLE t", "SELECT 1; DELETE FROM t"} {
		w := e.do("POST", "/api/databases/shop/query", fmt.Sprintf(`{"query":%q}`, q), true)
		if w.Code != 403 || !strings.Contains(w.Body.String(), "read-only") {
			t.Errorf("%q: %d %s", q, w.Code, w.Body)
		}
	}
	if len(e.opened) != 0 || len(e.conn.queries) != 0 {
		t.Errorf("a refused query must not reach the database: opened=%d queries=%v", len(e.opened), e.conn.queries)
	}
	if w := e.do("POST", "/api/databases/shop/query", `{"query":"SELECT 1"}`, false); w.Code != 403 {
		t.Errorf("the query route needs the confirm header: %d", w.Code)
	}
	if w := e.do("POST", "/api/databases/shop/query", `{"query":"  "}`, true); w.Code != 400 {
		t.Errorf("blank: %d", w.Code)
	}
	if w := e.do("POST", "/api/databases/shop/query", `{"query":"SELECT 1"}`, true); w.Code != 200 || len(e.conn.queries) != 1 {
		t.Errorf("a read goes through: %d %v", w.Code, e.conn.queries)
	}
}

func TestRedisAndMongoRefusalsThroughTheService(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", `{"id":"cache","engine":"redis","host":"127.0.0.1","port":56379}`, true)
	e.do("POST", "/api/databases", `{"id":"docs","engine":"mongo","host":"127.0.0.1","port":27017}`, true)
	for path, q := range map[string]string{
		"/api/databases/cache/query": "FLUSHALL",
		"/api/databases/docs/query":  `db.users.deleteMany({})`,
	} {
		w := e.do("POST", path, fmt.Sprintf(`{"query":%q}`, q), true)
		if w.Code != 403 {
			t.Errorf("%s %q: %d %s", path, q, w.Code, w.Body)
		}
	}
	if len(e.opened) != 0 {
		t.Error("refusals must not connect")
	}
}

func TestMissingEnvVarIsNamedNotValued(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", strings.Replace(saveBody, "PG_PW", "OTHER_PW", 1), true)
	w := e.do("GET", "/api/databases/shop/health", "", false)
	if !strings.Contains(w.Body.String(), "OTHER_PW") || !strings.Contains(w.Body.String(), `"ok":false`) {
		t.Errorf("health: %s", w.Body)
	}
	w = e.do("GET", "/api/databases", "", false)
	if !strings.Contains(w.Body.String(), `"password_set":false`) {
		t.Errorf("list: %s", w.Body)
	}
}

func TestDiscoveredContainersCannotBeReadUntilSaved(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/databases/docker:shop-db-1/health", "/api/databases/docker:shop-db-1/schema"} {
		if w := e.do("GET", p, "", false); w.Code != 403 || !strings.Contains(w.Body.String(), "save this container") {
			t.Errorf("%s: %d %s", p, w.Code, w.Body)
		}
	}
	w := e.do("GET", "/api/databases", "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"docker:shop-db-1"`) || strings.Contains(w.Body.String(), "PASSWORD-SENTINEL-1") {
		t.Errorf("list: %d %s", w.Code, w.Body)
	}
}

func TestListSurvivesDockerBeingDown(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", saveBody, true)
	e.svc.Docker = func(context.Context, ...string) ([]byte, error) { return nil, errors.New("docker ps: cannot connect") }
	w := e.do("GET", "/api/databases", "", false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "docker_error") || !strings.Contains(w.Body.String(), `"id":"shop"`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}

func TestDeleteAndHealth(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/databases", saveBody, true)
	w := e.do("GET", "/api/databases/shop/health", "", false)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"ok":true`)) || !bytes.Contains(w.Body.Bytes(), []byte("PostgreSQL 16.1")) {
		t.Errorf("health: %d %s", w.Code, w.Body)
	}
	if w := e.do("GET", "/api/databases/nope/health", "", false); w.Code != 404 {
		t.Errorf("unknown id: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/databases/shop", "", false); w.Code != 403 {
		t.Errorf("delete needs confirm: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/databases/shop", "", true); w.Code != 200 {
		t.Errorf("delete: %d %s", w.Code, w.Body)
	}
	if w := e.do("DELETE", "/api/databases/shop", "", true); w.Code != 404 {
		t.Errorf("delete twice: %d", w.Code)
	}
}
