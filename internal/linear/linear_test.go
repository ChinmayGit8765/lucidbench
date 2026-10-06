package linear

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/boards"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// secret is the fake API key. It must never appear in a response or a log.
const secret = "lin_api_SECRETSENTINEL0123456789"

// fake is a Linear GraphQL server. handle answers one operation.
type fake struct {
	mu     sync.Mutex
	calls  []call
	handle func(c call, w http.ResponseWriter)
	srv    *httptest.Server
}

type call struct {
	Query string
	Vars  map[string]any
	Auth  string
}

func newFake(t *testing.T, handle func(c call, w http.ResponseWriter)) *fake {
	t.Helper()
	f := &fake{handle: handle}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		c := call{in.Query, in.Variables, r.Header.Get("Authorization")}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		f.handle(c, w)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func issueJSON(n int, state string) map[string]any {
	return map[string]any{
		"id": fmt.Sprintf("id-%d", n), "identifier": fmt.Sprintf("ENG-%d", n), "title": fmt.Sprintf("Issue %d", n),
		"url": fmt.Sprintf("https://linear.app/acme/issue/ENG-%d/issue-%d", n, n), "priority": 2, "priorityLabel": "High",
		"updatedAt": "2026-10-01T00:00:00Z",
		"state":     map[string]any{"id": "s-" + state, "name": state, "type": "started", "color": "#f2c94c", "position": 2},
		"team":      map[string]any{"id": "t1", "key": "ENG"},
		"project":   map[string]any{"id": "p1", "name": "Launch"},
		"assignee":  map[string]any{"id": "u1", "name": "Sam"},
		"labels":    map[string]any{"nodes": []any{map[string]any{"name": "bug", "color": "#eb5757"}}},
	}
}

func service(t *testing.T, f *fake) (*Service, *[]time.Duration) {
	t.Helper()
	var slept []time.Duration
	s := New(config.LinearConfig{Token: "env:LINEAR_TEST_KEY", APIURL: f.srv.URL}, nil, nil)
	s.Getenv = func(n string) string {
		if n == "LINEAR_TEST_KEY" {
			return secret
		}
		return ""
	}
	s.Doer.Sleep = func(d time.Duration) { slept = append(slept, d) }
	return s, &slept
}

func TestIssuesPaginateAndCache(t *testing.T) {
	f := newFake(t, func(c call, w http.ResponseWriter) {
		after, _ := c.Vars["after"].(string)
		switch after {
		case "":
			reply(w, map[string]any{"data": map[string]any{"issues": map[string]any{
				"nodes": []any{issueJSON(1, "Doing"), issueJSON(2, "Todo")}, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "c1"}}}})
		case "c1":
			reply(w, map[string]any{"data": map[string]any{"issues": map[string]any{
				"nodes": []any{issueJSON(3, "Doing")}, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}}}})
		default:
			t.Errorf("unexpected cursor %q", after)
		}
	})
	s, _ := service(t, f)
	now := time.Unix(1000, 0)
	s.Cache.Now = func() time.Time { return now }
	l, err := s.Issues(t.Context(), Filter{Team: "t1", Project: "p1", Me: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Issues) != 3 || l.Truncated || l.Issues[2].Identifier != "ENG-3" || l.Issues[0].State.Name != "Doing" || l.Issues[0].Assignee.Name != "Sam" || l.Issues[0].Labels[0].Name != "bug" {
		t.Fatalf("issues = %+v", l)
	}
	if f.count() != 2 {
		t.Fatalf("calls = %d, want 2 pages", f.count())
	}
	if f.calls[0].Auth != secret {
		t.Errorf("Authorization header = %q, want the key without a scheme", f.calls[0].Auth)
	}
	filter, _ := json.Marshal(f.calls[0].Vars["filter"])
	for _, want := range []string{`"team":{"id":{"eq":"t1"}}`, `"project":{"id":{"eq":"p1"}}`, `"assignee":{"isMe":{"eq":true}}`, `"nin":["completed","canceled"]`} {
		if !strings.Contains(string(filter), want) {
			t.Errorf("filter %s lacks %s", filter, want)
		}
	}
	// A second read inside a minute is served from the cache, and a different
	// filter is not.
	if _, err := s.Issues(t.Context(), Filter{Team: "t1", Project: "p1", Me: true}); err != nil || f.count() != 2 {
		t.Errorf("cached read: err=%v calls=%d", err, f.count())
	}
	now = now.Add(61 * time.Second)
	if _, err := s.Issues(t.Context(), Filter{Team: "t1", Project: "p1", Me: true}); err != nil || f.count() != 4 {
		t.Errorf("expired read: err=%v calls=%d", err, f.count())
	}
}

func TestIssuesTruncatedAtMaxPages(t *testing.T) {
	f := newFake(t, func(c call, w http.ResponseWriter) {
		reply(w, map[string]any{"data": map[string]any{"issues": map[string]any{
			"nodes": []any{issueJSON(1, "Doing")}, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "more"}}}})
	})
	s, _ := service(t, f)
	l, err := s.Issues(t.Context(), Filter{})
	if err != nil || !l.Truncated || f.count() != MaxPages {
		t.Fatalf("err=%v truncated=%v calls=%d", err, l != nil && l.Truncated, f.count())
	}
}

func TestMeta(t *testing.T) {
	f := newFake(t, func(c call, w http.ResponseWriter) {
		reply(w, map[string]any{"data": map[string]any{
			"viewer": map[string]any{"id": "u1", "name": "Sam"},
			"teams": map[string]any{"nodes": []any{map[string]any{"id": "t1", "key": "ENG", "name": "Engineering",
				"activeCycle": map[string]any{"id": "cy1", "number": 7, "name": "", "startsAt": "2026-10-01", "endsAt": "2026-10-15", "progress": 0.4},
				"states":      map[string]any{"nodes": []any{map[string]any{"id": "s1", "name": "Todo", "type": "unstarted", "color": "#ccc", "position": 1}}}}}},
			"projects": map[string]any{"nodes": []any{map[string]any{"id": "p1", "name": "Launch", "state": "started", "teams": map[string]any{"nodes": []any{map[string]any{"id": "t1"}}}}}},
		}})
	})
	s, _ := service(t, f)
	m, err := s.Meta(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if m.Viewer.Name != "Sam" || len(m.Teams) != 1 || m.Teams[0].ActiveCycle.Number != 7 || m.Teams[0].States[0].Name != "Todo" || m.Projects[0].TeamIDs[0] != "t1" {
		t.Errorf("meta = %+v", m)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name   string
		handle func(w http.ResponseWriter)
		want   error
	}{
		{"401", func(w http.ResponseWriter) { http.Error(w, "no", 401) }, extapi.ErrUnauthorized},
		{"authentication error object", func(w http.ResponseWriter) {
			w.WriteHeader(400)
			reply(w, map[string]any{"errors": []any{map[string]any{"message": "x", "extensions": map[string]any{"type": "authentication error", "code": "AUTHENTICATION_ERROR"}}}})
		}, extapi.ErrUnauthorized},
		{"graphql not found", func(w http.ResponseWriter) {
			reply(w, map[string]any{"errors": []any{map[string]any{"message": "Entity not found: Issue"}}})
		}, extapi.ErrNotFound},
		{"500", func(w http.ResponseWriter) { http.Error(w, "boom", 500) }, extapi.ErrRemote},
		{"not json", func(w http.ResponseWriter) { _, _ = io.WriteString(w, "<html>") }, extapi.ErrRemote},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t, func(_ call, w http.ResponseWriter) { c.handle(w) })
			s, _ := service(t, f)
			if _, err := s.Meta(t.Context()); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
	t.Run("no key", func(t *testing.T) {
		f := newFake(t, func(call, http.ResponseWriter) { t.Error("called without a key") })
		s, _ := service(t, f)
		s.Getenv = func(string) string { return "" }
		if _, err := s.Meta(t.Context()); !errors.Is(err, extapi.ErrNotConfigured) {
			t.Errorf("err = %v", err)
		}
		if s.Status().Configured {
			t.Error("status says configured")
		}
	})
}

func TestRateLimitBackoff(t *testing.T) {
	t.Run("429 with Retry-After then success", func(t *testing.T) {
		n := 0
		f := newFake(t, func(_ call, w http.ResponseWriter) {
			if n++; n <= 2 {
				w.Header().Set("Retry-After", "3")
				http.Error(w, "slow down", 429)
				return
			}
			reply(w, map[string]any{"data": map[string]any{"viewer": map[string]any{"id": "u1", "name": "Sam"}, "teams": map[string]any{"nodes": []any{}}, "projects": map[string]any{"nodes": []any{}}}})
		})
		s, slept := service(t, f)
		if _, err := s.Meta(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(*slept) != 2 || (*slept)[0] != 3*time.Second || (*slept)[1] != 3*time.Second {
			t.Errorf("slept %v", *slept)
		}
	})
	t.Run("always limited gives up with doubling waits", func(t *testing.T) {
		f := newFake(t, func(_ call, w http.ResponseWriter) { http.Error(w, "slow down", 429) })
		s, slept := service(t, f)
		if _, err := s.Meta(t.Context()); !errors.Is(err, extapi.ErrRateLimited) {
			t.Fatalf("err = %v", err)
		}
		if f.count() != extapi.MaxAttempts || len(*slept) != 2 || (*slept)[0] != time.Second || (*slept)[1] != 2*time.Second {
			t.Errorf("calls=%d slept=%v", f.count(), *slept)
		}
	})
	t.Run("Linear's RATELIMITED error object is retried too", func(t *testing.T) {
		f := newFake(t, func(_ call, w http.ResponseWriter) {
			w.WriteHeader(400)
			reply(w, map[string]any{"errors": []any{map[string]any{"message": "limit", "extensions": map[string]any{"code": "RATELIMITED"}}}})
		})
		s, _ := service(t, f)
		if _, err := s.Meta(t.Context()); !errors.Is(err, extapi.ErrRateLimited) || f.count() != extapi.MaxAttempts {
			t.Errorf("err=%v calls=%d", err, f.count())
		}
	})
}

// vaultService is a Service whose promote and link routes use a temp vault
// holding one card, and a fake that answers issueCreate and issue(id).
func vaultService(t *testing.T) (*Service, *fake, *memory.Vault, boards.Card) {
	t.Helper()
	v, err := memory.Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	card, err := boards.AddCard(v, "work", boards.Card{Title: "Ship the beta", Column: "Ready", Project: "my-app"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t, func(c call, w http.ResponseWriter) {
		switch {
		case strings.Contains(c.Query, "issueCreate"):
			in, _ := c.Vars["input"].(map[string]any)
			if in["title"] != "Ship the beta" || in["teamId"] != "t1" || in["projectId"] != "p1" {
				t.Errorf("create input = %v", in)
			}
			reply(w, map[string]any{"data": map[string]any{"issueCreate": map[string]any{"success": true, "issue": issueJSON(7, "Todo")}}})
		case strings.Contains(c.Query, "issue(id"):
			if c.Vars["id"] != "ENG-9" {
				reply(w, map[string]any{"errors": []any{map[string]any{"message": "Entity not found: Issue"}}})
				return
			}
			reply(w, map[string]any{"data": map[string]any{"issue": issueJSON(9, "Doing")}})
		default:
			t.Errorf("unexpected query %.40s", c.Query)
		}
	})
	s, _ := service(t, f)
	s.Open = func() (*memory.Vault, error) { return v, nil }
	return s, f, v, card
}

func post(h http.Handler, path string, body any, confirm bool) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mux(s *Service) *http.ServeMux {
	m := http.NewServeMux()
	Register(m, s)
	return m
}

func boardFile(t *testing.T, v *memory.Vault) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.Root(), "Boards", "work.md"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func TestPromoteWritesTheCardFields(t *testing.T) {
	s, f, v, card := vaultService(t)
	h := mux(s)
	rec := post(h, "/api/linear/promote", PromoteRequest{Board: "work", Card: card.ID, Team: "t1", Project: "p1"}, true)
	if rec.Code != 200 {
		t.Fatalf("promote = %d %s", rec.Code, rec.Body)
	}
	var res PromoteResult
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Card.Linear != "ENG-7" || res.Card.LinearURL != "https://linear.app/acme/issue/ENG-7/issue-7" || res.Issue.Identifier != "ENG-7" {
		t.Errorf("result = %+v", res)
	}
	file := boardFile(t, v)
	for _, want := range []string{"\tlinear:: ENG-7\n", "\tlinear_url:: https://linear.app/acme/issue/ENG-7/issue-7\n", "\tproject:: my-app\n"} {
		if !strings.Contains(file, want) {
			t.Errorf("board file lacks %q:\n%s", want, file)
		}
	}
	if !strings.Contains(file, "## Ready\n\n- [ ] Ship the beta\n") {
		t.Errorf("card left its column:\n%s", file)
	}
	// Promoting again must not create a second issue.
	rec = post(h, "/api/linear/promote", PromoteRequest{Board: "work", Card: card.ID, Team: "t1"}, true)
	if rec.Code != http.StatusConflict || f.count() != 1 {
		t.Errorf("second promote = %d (calls %d): %s", rec.Code, f.count(), rec.Body)
	}
}

func TestPromoteNeedsConfirmAndAValidCard(t *testing.T) {
	s, f, v, card := vaultService(t)
	h := mux(s)
	if rec := post(h, "/api/linear/promote", PromoteRequest{Board: "work", Card: card.ID, Team: "t1"}, false); rec.Code != http.StatusForbidden {
		t.Errorf("without confirm = %d", rec.Code)
	}
	if rec := post(h, "/api/linear/promote", PromoteRequest{Board: "work", Card: "c-nope", Team: "t1"}, true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown card = %d", rec.Code)
	}
	if rec := post(h, "/api/linear/promote", PromoteRequest{Board: "work", Card: card.ID}, true); rec.Code != http.StatusBadRequest {
		t.Errorf("no team = %d", rec.Code)
	}
	if f.count() != 0 || strings.Contains(boardFile(t, v), "linear::") {
		t.Errorf("nothing should have happened: calls=%d", f.count())
	}
}

func TestPromoteRemoteFailureLeavesTheCardAlone(t *testing.T) {
	s, _, v, card := vaultService(t)
	s.Getenv = func(string) string { return "" }
	rec := post(mux(s), "/api/linear/promote", PromoteRequest{Board: "work", Card: card.ID, Team: "t1"}, true)
	if rec.Code != http.StatusPreconditionFailed || strings.Contains(boardFile(t, v), "linear::") {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestLinkExistingIssue(t *testing.T) {
	s, _, v, card := vaultService(t)
	h := mux(s)
	for _, id := range []string{"ENG-9", "eng-9", "https://linear.app/acme/issue/ENG-9/some-title"} {
		rec := post(h, "/api/linear/link", LinkRequest{Board: "work", Card: card.ID, Identifier: id}, true)
		if rec.Code != 200 {
			t.Fatalf("link %q = %d %s", id, rec.Code, rec.Body)
		}
	}
	if file := boardFile(t, v); !strings.Contains(file, "\tlinear:: ENG-9\n") || !strings.Contains(file, "linear_url:: https://linear.app/acme/issue/ENG-9/issue-9\n") {
		t.Errorf("board file:\n%s", file)
	}
	if rec := post(h, "/api/linear/link", LinkRequest{Board: "work", Card: card.ID, Identifier: "not an issue"}, true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad identifier = %d", rec.Code)
	}
	if rec := post(h, "/api/linear/link", LinkRequest{Board: "work", Card: card.ID, Identifier: "ENG-404"}, true); rec.Code != http.StatusNotFound {
		t.Errorf("missing issue = %d", rec.Code)
	}
}

func TestParseIdentifier(t *testing.T) {
	for in, want := range map[string]string{"ENG-12": "ENG-12", " eng-12 ": "ENG-12", "https://linear.app/acme/issue/ENG-12/a-b": "ENG-12", "https://linear.app/acme/issue/ENG-12": "ENG-12"} {
		if got, err := ParseIdentifier(in); err != nil || got != want {
			t.Errorf("ParseIdentifier(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "ENG", "12", "https://example.com/issue/ENG-12", "ENG-12; drop", "https://linear.app/acme/project/x"} {
		if _, err := ParseIdentifier(in); err == nil {
			t.Errorf("ParseIdentifier(%q) accepted", in)
		}
	}
}

func TestHTTPErrorsSayTokenInvalid(t *testing.T) {
	f := newFake(t, func(_ call, w http.ResponseWriter) { http.Error(w, "no", 401) })
	s, _ := service(t, f)
	rec := httptest.NewRecorder()
	mux(s).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/linear/meta", nil))
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), "token invalid") {
		t.Errorf("= %d %s", rec.Code, rec.Body)
	}
}

// TestSecretNeverLeaks drives success, 401, 429, 5xx and transport failures,
// with a fake that echoes the Authorization header into every error body, and
// checks that neither a response nor the log ever holds the key.
func TestSecretNeverLeaks(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	mode := "ok"
	f := newFake(t, func(c call, w http.ResponseWriter) {
		echo := "bad credential " + c.Auth
		switch mode {
		case "401":
			http.Error(w, echo, 401)
		case "429":
			http.Error(w, echo, 429)
		case "500":
			http.Error(w, echo, 500)
		case "gql":
			w.WriteHeader(400)
			reply(w, map[string]any{"errors": []any{map[string]any{"message": echo}}})
		default:
			reply(w, map[string]any{"data": map[string]any{"viewer": map[string]any{"id": "u1", "name": "Sam"}, "teams": map[string]any{"nodes": []any{}}, "projects": map[string]any{"nodes": []any{}},
				"issues": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{}}}})
		}
	})
	s, _ := service(t, f)
	s.MCP = func() bool { return true }
	h := mux(s)
	routes := []string{"/api/linear/status", "/api/linear/meta", "/api/linear/issues", "/api/linear/issues/ENG-1"}
	check := func(label string, rec *httptest.ResponseRecorder) {
		if strings.Contains(rec.Body.String(), secret) || strings.Contains(fmt.Sprint(rec.Header()), secret) {
			t.Errorf("%s leaked the key: %s", label, rec.Body)
		}
	}
	for _, m := range []string{"ok", "401", "429", "500", "gql"} {
		mode = m
		s.Cache.Clear()
		for _, p := range routes {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
			check(m+" "+p, rec)
		}
		check(m+" create", post(h, "/api/linear/issues", NewIssue{Team: "t1", Title: "x"}, true))
	}
	// Transport failure: the error from net/http quotes the URL.
	f.srv.Close()
	s.Cache.Clear()
	for _, p := range routes {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		check("closed "+p, rec)
	}
	if strings.Contains(logs.String(), secret) {
		t.Errorf("log holds the key: %s", logs.String())
	}
	// And the status route reports only the reference.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/linear/status", nil))
	var st Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || !st.Configured || !st.MCP || st.TokenRef != "env:LINEAR_TEST_KEY" {
		t.Errorf("status = %+v (%v)", st, err)
	}
}
