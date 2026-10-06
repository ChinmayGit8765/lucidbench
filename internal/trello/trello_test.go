package trello

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

// Fake credentials. They must never appear in a response, a URL or a log.
const (
	apiKey   = "KEYSENTINEL0123456789abcdef"
	apiToken = "TOKENSENTINEL0123456789abcdef0123456789"
)

const (
	boardID = "5f1c0000000000000000b0a1"
	listA   = "5f1c0000000000000000a001"
	listB   = "5f1c0000000000000000a002"
	cardID  = "5f1c0000000000000000c001"
)

type req struct {
	Method, Path, Query, Form, Auth string
}

type fake struct {
	mu     sync.Mutex
	reqs   []req
	handle func(r req, w http.ResponseWriter)
	srv    *httptest.Server
}

func newFake(t *testing.T, handle func(r req, w http.ResponseWriter)) *fake {
	t.Helper()
	f := &fake{handle: handle}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		q := req{r.Method, r.URL.Path, r.URL.RawQuery, string(b), r.Header.Get("Authorization")}
		f.mu.Lock()
		f.reqs = append(f.reqs, q)
		f.mu.Unlock()
		f.handle(q, w)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func cardJSON(id, list, name string) map[string]any {
	return map[string]any{"id": id, "name": name, "desc": "", "idList": list, "idBoard": boardID, "url": "https://trello.com/c/AbCd1234/1-" + name,
		"due": nil, "pos": 1024, "shortLink": "AbCd1234", "labels": []any{map[string]any{"name": "urgent", "color": "red"}}}
}

// standard answers the read routes the way Trello does.
func standard(r req, w http.ResponseWriter) {
	switch {
	case r.Path == "/members/me/boards":
		reply(w, []any{map[string]any{"id": boardID, "name": "Roadmap", "url": "https://trello.com/b/x/roadmap", "dateLastActivity": "2026-10-01T00:00:00Z"}})
	case r.Path == "/boards/"+boardID:
		reply(w, map[string]any{"id": boardID, "name": "Roadmap", "url": "https://trello.com/b/x/roadmap"})
	case r.Path == "/boards/"+boardID+"/lists":
		reply(w, []any{map[string]any{"id": listA, "name": "To do", "pos": 1}, map[string]any{"id": listB, "name": "Doing", "pos": 2}})
	case r.Path == "/boards/"+boardID+"/cards":
		reply(w, []any{cardJSON(cardID, listA, "first"), cardJSON("5f1c0000000000000000c002", listB, "second")})
	case r.Method == http.MethodPost && r.Path == "/cards":
		reply(w, cardJSON("5f1c0000000000000000c003", listA, "added"))
	case r.Method == http.MethodPut && r.Path == "/cards/"+cardID:
		reply(w, cardJSON(cardID, listB, "first"))
	case r.Method == http.MethodGet && (r.Path == "/cards/"+cardID || r.Path == "/cards/AbCd1234"):
		reply(w, cardJSON(cardID, listA, "first"))
	default:
		http.NotFound(w, nil)
	}
}

func service(t *testing.T, f *fake) (*Service, *[]time.Duration) {
	t.Helper()
	var slept []time.Duration
	s := New(config.TrelloConfig{Key: "env:TRELLO_TEST_KEY", Token: "env:TRELLO_TEST_TOKEN", APIURL: f.srv.URL}, nil)
	s.Getenv = func(n string) string {
		switch n {
		case "TRELLO_TEST_KEY":
			return apiKey
		case "TRELLO_TEST_TOKEN":
			return apiToken
		}
		return ""
	}
	s.Doer.Sleep = func(d time.Duration) { slept = append(slept, d) }
	return s, &slept
}

func TestReadBoardListsCards(t *testing.T) {
	f := newFake(t, func(r req, w http.ResponseWriter) { standard(r, w) })
	s, _ := service(t, f)
	bs, err := s.Boards(t.Context())
	if err != nil || len(bs) != 1 || bs[0].Name != "Roadmap" {
		t.Fatalf("boards = %+v, %v", bs, err)
	}
	v, err := s.Board(t.Context(), boardID)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Lists) != 2 || len(v.Cards) != 2 || v.Cards[0].ListID != listA || v.Cards[0].Labels[0].Name != "urgent" || v.Board.Name != "Roadmap" {
		t.Errorf("view = %+v", v)
	}
	// The credentials travel in the header, never in the URL.
	want := fmt.Sprintf(`OAuth oauth_consumer_key="%s", oauth_token="%s"`, apiKey, apiToken)
	for _, r := range f.reqs {
		if r.Auth != want || strings.Contains(r.Query, apiKey) || strings.Contains(r.Query, apiToken) || strings.Contains(r.Path, apiKey) {
			t.Errorf("request %s %s?%s auth=%q", r.Method, r.Path, r.Query, r.Auth)
		}
	}
	// Cached for a minute.
	n := f.count()
	if _, err := s.Board(t.Context(), boardID); err != nil || f.count() != n {
		t.Errorf("second read hit the API: %d -> %d (%v)", n, f.count(), err)
	}
}

func TestAddAndMoveCardWriteThenRefresh(t *testing.T) {
	f := newFake(t, func(r req, w http.ResponseWriter) { standard(r, w) })
	s, _ := service(t, f)
	h := mux(s)
	if _, err := s.Board(t.Context(), boardID); err != nil {
		t.Fatal(err)
	}
	if rec := send(h, http.MethodPost, "/api/trello/cards", NewCard{List: listA, Name: "  added  "}, false); rec.Code != http.StatusForbidden {
		t.Errorf("add without confirm = %d", rec.Code)
	}
	if rec := send(h, http.MethodPut, "/api/trello/cards/"+cardID, MoveRequest{List: listB}, false); rec.Code != http.StatusForbidden {
		t.Errorf("move without confirm = %d", rec.Code)
	}
	for _, r := range f.reqs {
		if r.Method != http.MethodGet {
			t.Fatalf("a write got through without confirm: %+v", r)
		}
	}
	rec := send(h, http.MethodPost, "/api/trello/cards", NewCard{List: listA, Name: "  added  "}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", rec.Code, rec.Body)
	}
	post := f.reqs[len(f.reqs)-1]
	if post.Method != http.MethodPost || post.Path != "/cards" || !strings.Contains(post.Form, "idList="+listA) || !strings.Contains(post.Form, "name=added") || !strings.Contains(post.Form, "pos=bottom") {
		t.Errorf("add request = %+v", post)
	}
	rec = send(h, http.MethodPut, "/api/trello/cards/"+cardID, MoveRequest{List: listB}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("move = %d %s", rec.Code, rec.Body)
	}
	put := f.reqs[len(f.reqs)-1]
	if put.Method != http.MethodPut || put.Path != "/cards/"+cardID || !strings.Contains(put.Form, "idList="+listB) {
		t.Errorf("move request = %+v", put)
	}
	// The writes dropped the cache, so the next board read goes to Trello.
	n := f.count()
	if _, err := s.Board(t.Context(), boardID); err != nil || f.count() == n {
		t.Errorf("board read after a write was cached (%v)", err)
	}
	// Ids that are not ids never reach a path.
	for _, bad := range []string{"../members/me", "a/b", "x y", ""} {
		if rec := send(h, http.MethodPut, "/api/trello/cards/"+strings.ReplaceAll(bad, " ", "%20"), MoveRequest{List: listB}, true); rec.Code == http.StatusOK {
			t.Errorf("card id %q accepted", bad)
		}
		if rec := send(h, http.MethodPost, "/api/trello/cards", NewCard{List: bad, Name: "x"}, true); rec.Code != http.StatusBadRequest {
			t.Errorf("list id %q = %d", bad, rec.Code)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"401", 401, extapi.ErrUnauthorized}, {"403", 403, extapi.ErrUnauthorized}, {"404", 404, extapi.ErrNotFound},
		{"400", 400, extapi.ErrBadRequest}, {"500", 500, extapi.ErrRemote},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t, func(_ req, w http.ResponseWriter) { http.Error(w, "invalid token", c.status) })
			s, _ := service(t, f)
			if _, err := s.Boards(t.Context()); !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
	t.Run("not json", func(t *testing.T) {
		f := newFake(t, func(_ req, w http.ResponseWriter) { _, _ = io.WriteString(w, "<html>") })
		s, _ := service(t, f)
		if _, err := s.Boards(t.Context()); !errors.Is(err, extapi.ErrRemote) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("only the key set", func(t *testing.T) {
		f := newFake(t, func(req, http.ResponseWriter) { t.Error("called without a token") })
		s, _ := service(t, f)
		s.Getenv = func(n string) string {
			if n == "TRELLO_TEST_KEY" {
				return apiKey
			}
			return ""
		}
		if _, err := s.Boards(t.Context()); !errors.Is(err, extapi.ErrNotConfigured) {
			t.Errorf("err = %v", err)
		}
		if st := s.Status(); st.Configured || !st.KeySet || st.TokenSet {
			t.Errorf("status = %+v", st)
		}
	})
	t.Run("401 over HTTP says token invalid", func(t *testing.T) {
		f := newFake(t, func(_ req, w http.ResponseWriter) { http.Error(w, "no", 401) })
		s, _ := service(t, f)
		rec := send(mux(s), http.MethodGet, "/api/trello/boards", nil, false)
		if rec.Code != 401 || !strings.Contains(rec.Body.String(), "token invalid") {
			t.Errorf("= %d %s", rec.Code, rec.Body)
		}
	})
}

func TestRateLimitBackoff(t *testing.T) {
	t.Run("retries then succeeds", func(t *testing.T) {
		n := 0
		f := newFake(t, func(r req, w http.ResponseWriter) {
			if n++; n <= 2 {
				w.Header().Set("Retry-After", "2")
				http.Error(w, "slow down", 429)
				return
			}
			standard(r, w)
		})
		s, slept := service(t, f)
		if _, err := s.Boards(t.Context()); err != nil {
			t.Fatal(err)
		}
		if len(*slept) != 2 || (*slept)[0] != 2*time.Second {
			t.Errorf("slept %v", *slept)
		}
	})
	t.Run("gives up with doubling waits", func(t *testing.T) {
		f := newFake(t, func(_ req, w http.ResponseWriter) { http.Error(w, "slow down", 429) })
		s, slept := service(t, f)
		if _, err := s.Boards(t.Context()); !errors.Is(err, extapi.ErrRateLimited) {
			t.Fatalf("err = %v", err)
		}
		if f.count() != extapi.MaxAttempts || len(*slept) != 2 || (*slept)[0] != time.Second || (*slept)[1] != 2*time.Second {
			t.Errorf("calls=%d slept=%v", f.count(), *slept)
		}
		rec := send(mux(s), http.MethodGet, "/api/trello/boards", nil, false)
		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("HTTP = %d", rec.Code)
		}
	})
}

func vaultService(t *testing.T) (*Service, *memory.Vault, boards.Card) {
	t.Helper()
	v, err := memory.Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	card, err := boards.AddCard(v, "work", boards.Card{Title: "Plan the launch", Column: "Ready"})
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t, func(r req, w http.ResponseWriter) { standard(r, w) })
	s, _ := service(t, f)
	s.Open = func() (*memory.Vault, error) { return v, nil }
	return s, v, card
}

func TestLinkWritesTheCardFields(t *testing.T) {
	s, v, card := vaultService(t)
	h := mux(s)
	for _, ref := range []string{cardID, "AbCd1234", "https://trello.com/c/AbCd1234/1-first"} {
		rec := send(h, http.MethodPost, "/api/trello/link", LinkRequest{Board: "work", Card: card.ID, Trello: ref}, true)
		if rec.Code != 200 {
			t.Fatalf("link %q = %d %s", ref, rec.Code, rec.Body)
		}
	}
	b, _ := os.ReadFile(filepath.Join(v.Root(), "Boards", "work.md"))
	file := strings.ReplaceAll(string(b), "\r\n", "\n")
	for _, want := range []string{"\ttrello:: " + cardID + "\n", "\ttrello_url:: https://trello.com/c/AbCd1234/1-first\n"} {
		if !strings.Contains(file, want) {
			t.Errorf("board file lacks %q:\n%s", want, file)
		}
	}
	for _, c := range []struct {
		name string
		in   LinkRequest
		code int
	}{
		{"not a card ref", LinkRequest{Board: "work", Card: card.ID, Trello: "https://example.com/c/AbCd1234"}, 400},
		{"unknown native card", LinkRequest{Board: "work", Card: "c-nope", Trello: cardID}, 404},
		{"unknown remote card", LinkRequest{Board: "work", Card: card.ID, Trello: "ZZZZ9999"}, 404},
		{"no confirm", LinkRequest{}, 403},
	} {
		if rec := send(h, http.MethodPost, "/api/trello/link", c.in, c.name != "no confirm"); rec.Code != c.code {
			t.Errorf("%s = %d %s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestParseCardRef(t *testing.T) {
	for in, want := range map[string]string{"AbCd1234": "AbCd1234", cardID: cardID, "https://trello.com/c/AbCd1234/12-name": "AbCd1234", "https://trello.com/c/AbCd1234": "AbCd1234"} {
		if got, err := ParseCardRef(in); err != nil || got != want {
			t.Errorf("ParseCardRef(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "a/b", "../x", "https://trello.com/b/AbCd1234/board", "https://example.com/c/AbCd1234"} {
		if _, err := ParseCardRef(in); err == nil {
			t.Errorf("ParseCardRef(%q) accepted", in)
		}
	}
}

func mux(s *Service) *http.ServeMux {
	m := http.NewServeMux()
	Register(m, s)
	return m
}

func send(h http.Handler, method, path string, body any, confirm bool) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	if confirm {
		r.Header.Set("X-Lucid-Confirm", "yes")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// TestSecretsNeverLeak drives success, 401, 429, 5xx and a dead server, with
// a fake that echoes the Authorization header into every error body, and
// checks that no response and no log line holds the key or the token.
func TestSecretsNeverLeak(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	mode := "ok"
	f := newFake(t, func(r req, w http.ResponseWriter) {
		echo := "invalid " + r.Auth
		switch mode {
		case "401":
			http.Error(w, echo, 401)
		case "429":
			http.Error(w, echo, 429)
		case "500":
			http.Error(w, echo, 500)
		case "400":
			http.Error(w, echo, 400)
		default:
			standard(r, w)
		}
	})
	s, _ := service(t, f)
	h := mux(s)
	leaks := func(label string, rec *httptest.ResponseRecorder) {
		if strings.Contains(rec.Body.String(), apiKey) || strings.Contains(rec.Body.String(), apiToken) || strings.Contains(fmt.Sprint(rec.Header()), apiKey) {
			t.Errorf("%s leaked a credential: %s", label, rec.Body)
		}
	}
	run := func() {
		for _, p := range []string{"/api/trello/status", "/api/trello/boards", "/api/trello/boards/" + boardID} {
			leaks(mode+" "+p, send(h, http.MethodGet, p, nil, false))
		}
		leaks(mode+" add", send(h, http.MethodPost, "/api/trello/cards", NewCard{List: listA, Name: "x"}, true))
		leaks(mode+" move", send(h, http.MethodPut, "/api/trello/cards/"+cardID, MoveRequest{List: listB}, true))
	}
	for _, m := range []string{"ok", "401", "429", "500", "400"} {
		mode = m
		s.Cache.Clear()
		run()
	}
	f.srv.Close() // transport errors from net/http quote the URL
	mode = "closed"
	s.Cache.Clear()
	run()
	if strings.Contains(logs.String(), apiKey) || strings.Contains(logs.String(), apiToken) {
		t.Errorf("log holds a credential: %s", logs.String())
	}
	var st Status
	if err := json.Unmarshal(send(h, http.MethodGet, "/api/trello/status", nil, false).Body.Bytes(), &st); err != nil || !st.Configured || st.KeyRef != "env:TRELLO_TEST_KEY" || st.TokenRef != "env:TRELLO_TEST_TOKEN" {
		t.Errorf("status = %+v (%v)", st, err)
	}
}
