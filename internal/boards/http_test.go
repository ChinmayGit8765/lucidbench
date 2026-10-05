package boards

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

func call(mux http.Handler, method, target, body string, confirm bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHTTP(t *testing.T) {
	v := vaultWith(t, map[string]string{"Boards/sample.md": sample})
	mux := http.NewServeMux()
	Register(mux, func() (*memory.Vault, error) { return v, nil })

	// Reading the default board creates it.
	rec := call(mux, "GET", "/api/boards/work", "", false)
	var b Board
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || rec.Code != http.StatusOK || len(b.Columns) != 5 || b.Cards == nil {
		t.Fatalf("GET work = %d %s", rec.Code, rec.Body)
	}
	rec = call(mux, "GET", "/api/boards", "", false)
	var list []BoardSummary
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list) != 2 || list[0].ID != "sample" || list[1].ID != "work" {
		t.Errorf("list = %d %s", rec.Code, rec.Body)
	}
	if rec := call(mux, "GET", "/api/boards/nope", "", false); rec.Code != http.StatusNotFound {
		t.Errorf("unknown board = %d", rec.Code)
	}
	if rec := call(mux, "GET", "/api/boards/..%2Fx", "", false); rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
		t.Errorf("bad board id = %d", rec.Code)
	}

	// Writes need the confirm header.
	add := `{"title":"Ship it","project":"my-app","labels":["a"],"column":"Ready"}`
	if rec := call(mux, "POST", "/api/boards/work/cards", add, false); rec.Code != http.StatusForbidden {
		t.Errorf("POST without confirm = %d", rec.Code)
	}
	if rec := call(mux, "PUT", "/api/boards/work/cards/c-1", `{"title":"x"}`, false); rec.Code != http.StatusForbidden {
		t.Errorf("PUT without confirm = %d", rec.Code)
	}
	if got, _ := Get(v, "work"); len(got.Cards) != 0 {
		t.Fatalf("an unconfirmed write landed: %+v", got.Cards)
	}

	rec = call(mux, "POST", "/api/boards/work/cards", add, true)
	var card Card
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil || rec.Code != http.StatusCreated || card.ID == "" || card.Column != "Ready" || card.Project != "my-app" {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	if rec := call(mux, "POST", "/api/boards/work/cards", `{"title":"x","column":"Nope"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad column = %d", rec.Code)
	}
	if rec := call(mux, "POST", "/api/boards/work/cards", `{"title":""}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("empty title = %d", rec.Code)
	}
	if rec := call(mux, "POST", "/api/boards/work/cards", `{"bogus":1}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field = %d", rec.Code)
	}
	if rec := call(mux, "POST", "/api/boards/nope/cards", `{"title":"x"}`, true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown board = %d", rec.Code)
	}

	// A partial PUT changes only what it names.
	rec = call(mux, "PUT", "/api/boards/work/cards/"+card.ID, `{"due":"2026-12-01","done":true}`, true)
	var upd Card
	if err := json.Unmarshal(rec.Body.Bytes(), &upd); err != nil || rec.Code != http.StatusOK ||
		upd.Title != "Ship it" || upd.Project != "my-app" || upd.Due != "2026-12-01" || !upd.Done || upd.Column != "Ready" {
		t.Fatalf("partial PUT = %d %s", rec.Code, rec.Body)
	}
	// {column, index} moves.
	call(mux, "POST", "/api/boards/work/cards", `{"title":"Second","column":"Review"}`, true)
	rec = call(mux, "PUT", "/api/boards/work/cards/"+card.ID, `{"column":"Review","index":0}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("move = %d %s", rec.Code, rec.Body)
	}
	got, _ := Get(v, "work")
	if r := titles(got, "Review"); len(r) != 2 || r[0] != "Ship it" || r[1] != "Second" {
		t.Errorf("review = %v", r)
	}
	if r := titles(got, "Ready"); len(r) != 0 {
		t.Errorf("ready = %v", r)
	}
	// A card's own column with an index reorders it.
	rec = call(mux, "PUT", "/api/boards/work/cards/"+card.ID, `{"index":1}`, true)
	got, _ = Get(v, "work")
	if r := titles(got, "Review"); rec.Code != http.StatusOK || r[0] != "Second" || r[1] != "Ship it" {
		t.Errorf("reorder = %d, review = %v", rec.Code, r)
	}
	if rec := call(mux, "PUT", "/api/boards/work/cards/c-nope", `{"title":"x"}`, true); rec.Code != http.StatusNotFound {
		t.Errorf("unknown card = %d", rec.Code)
	}
	if rec := call(mux, "PUT", "/api/boards/work/cards/"+card.ID, `{"column":"Nope","index":0}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("move to bad column = %d", rec.Code)
	}
	if rec := call(mux, "PUT", "/api/boards/work/cards/"+card.ID, `{"title":""}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("blank title = %d", rec.Code)
	}
}
