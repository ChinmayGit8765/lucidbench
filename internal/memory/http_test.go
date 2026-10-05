package memory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// call sends one request to mux. body "" sends none; confirm adds the
// X-Lucid-Confirm header.
func call(mux http.Handler, method, target, body string, confirm bool) *httptest.ResponseRecorder {
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, rd)
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHTTP(t *testing.T) {
	v := open(t)
	mux := http.NewServeMux()
	Register(mux, func() (*Vault, error) { return v, nil })

	// Every write needs the confirm header.
	for _, c := range []struct{ method, target, body string }{
		{"PUT", "/api/memory/page?path=a.md", `{"body":"x"}`},
		{"DELETE", "/api/memory/page?path=a.md", ""},
		{"POST", "/api/memory/move", `{"from":"a.md","to":"b.md"}`},
	} {
		if rec := call(mux, c.method, c.target, c.body, false); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without confirm = %d", c.method, c.target, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "a.md")); err == nil {
		t.Fatal("an unconfirmed write landed")
	}

	// Write, read, front matter round trip.
	rec := call(mux, "PUT", "/api/memory/page?path=Inbox%2Fidea.md", `{"front":{"status":"draft","custom":[1,2]},"body":"# Idea\nsee [[other]]\n"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	var pg Page
	if err := json.Unmarshal(rec.Body.Bytes(), &pg); err != nil || pg.Path != "Inbox/idea.md" || pg.Title != "idea" || pg.Front["status"] != "draft" {
		t.Fatalf("page = %+v, %v", pg, err)
	}
	rec = call(mux, "GET", "/api/memory/page?path=Inbox/idea.md", "", false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"custom":[1,2]`) {
		t.Errorf("GET page = %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("pages must not be cached")
	}

	// The tree, search and backlinks.
	if err := os.WriteFile(filepath.Join(v.Root(), "other.md"), []byte("the other page"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec = call(mux, "GET", "/api/memory/tree?dir=", "", false)
	var tree []Entry
	_ = json.Unmarshal(rec.Body.Bytes(), &tree)
	if rec.Code != http.StatusOK || len(tree) != 2 || tree[0].Name != "Inbox" || !tree[0].Dir || tree[1].Name != "other.md" {
		t.Errorf("tree = %d %s", rec.Code, rec.Body)
	}
	rec = call(mux, "GET", "/api/memory/search?q="+url.QueryEscape("OTHER")+"&limit=5", "", false)
	var hits []Hit
	_ = json.Unmarshal(rec.Body.Bytes(), &hits)
	if rec.Code != http.StatusOK || len(hits) != 2 {
		t.Errorf("search = %d %s", rec.Code, rec.Body)
	}
	rec = call(mux, "GET", "/api/memory/backlinks?path=other.md", "", false)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `["Inbox/idea.md"]` {
		t.Errorf("backlinks = %d %s", rec.Code, rec.Body)
	}

	// Move, then delete to the trash.
	if rec := call(mux, "POST", "/api/memory/move", `{"from":"Inbox/idea.md","to":"Ready/idea.md"}`, true); rec.Code != http.StatusOK {
		t.Errorf("move = %d %s", rec.Code, rec.Body)
	}
	if rec := call(mux, "POST", "/api/memory/move", `{"from":"Ready/idea.md","to":"other.md"}`, true); rec.Code != http.StatusConflict {
		t.Errorf("move onto existing = %d", rec.Code)
	}
	if rec := call(mux, "DELETE", "/api/memory/page?path=Ready/idea.md", "", true); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d %s", rec.Code, rec.Body)
	}
	if rec := call(mux, "GET", "/api/memory/page?path=Ready/idea.md", "", false); rec.Code != http.StatusNotFound {
		t.Errorf("deleted page = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), ".trash", "Ready", "idea.md")); err != nil {
		t.Errorf("not trashed: %v", err)
	}

	// Bad input.
	for _, c := range []struct {
		method, target, body string
		want                 int
	}{
		{"GET", "/api/memory/page?path=../x.md", "", http.StatusBadRequest},
		{"GET", "/api/memory/page?path=C:/x.md", "", http.StatusBadRequest},
		{"GET", "/api/memory/page?path=%2Fetc%2Fx.md", "", http.StatusBadRequest},
		{"GET", "/api/memory/tree?dir=..", "", http.StatusBadRequest},
		{"GET", "/api/memory/page?path=nope.md", "", http.StatusNotFound},
		{"PUT", "/api/memory/page?path=../x.md", `{"body":"x"}`, http.StatusBadRequest},
		{"PUT", "/api/memory/page?path=x.md", `{"nonsense":1}`, http.StatusBadRequest},
		{"PUT", "/api/memory/page?path=x.md", `not json`, http.StatusBadRequest},
		{"POST", "/api/memory/move", `{"from":"a.md","to":"../b.md"}`, http.StatusBadRequest},
		{"DELETE", "/api/memory/page?path=nope.md", "", http.StatusNotFound},
	} {
		if rec := call(mux, c.method, c.target, c.body, true); rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.target, rec.Code, c.want, rec.Body)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(v.Root()), "x.md")); err == nil {
		t.Error("a traversal write escaped the vault")
	}
}

func TestLazyOpenerOpensOnFirstRequest(t *testing.T) {
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("LUCID_DATA_DIR", data)
	mux := http.NewServeMux()
	Register(mux, LazyOpener(config.Default()))
	if _, err := os.Stat(filepath.Join(data, "memory")); err == nil {
		t.Fatal("registering routes created the vault")
	}
	if rec := call(mux, "GET", "/api/memory/tree", "", false); rec.Code != http.StatusOK {
		t.Fatalf("tree = %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(data, "memory", "Inbox")); err != nil {
		t.Errorf("the first request should create the default vault: %v", err)
	}
}
