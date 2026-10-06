package work

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/boards"
)

// moveBack puts a card in Review, as a person dragging it would.
func (f *fixture) moveBack(id string) error {
	return boards.MoveCard(f.vault, boards.DefaultBoard, id, ColumnReview, -1)
}

func TestListOmitsBulkButGetKeepsIt(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	mux := http.NewServeMux()
	Register(mux, f.svc)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	body := func(p string) string {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		b, _ := io.ReadAll(r.Body)
		return string(b)
	}
	list, one := body("/api/work/sessions"), body("/api/work/sessions/"+se.ID)
	if strings.Contains(list, "+hello") || strings.Contains(list, "Add a README line") {
		t.Errorf("list carries the patch or prompt: %s", list)
	}
	if !strings.Contains(one, "+hello") || !strings.Contains(one, "Add a README line") {
		t.Errorf("get lost the patch or prompt: %s", one)
	}
	if len(list) > 5<<10 {
		t.Errorf("one-session list is %d bytes", len(list))
	}
}

func TestParsePR(t *testing.T) {
	for _, c := range []struct {
		name, json, state string
		checks            PRChecks
	}{
		{"draft", `{"state":"OPEN","isDraft":true,"mergedAt":null,"statusCheckRollup":[]}`, PRDraft, PRChecks{}},
		{"open with mixed checks", `{"state":"OPEN","isDraft":false,"mergedAt":null,"statusCheckRollup":[
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SKIPPED"},
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"},
			{"__typename":"CheckRun","status":"COMPLETED","conclusion":"CANCELLED"},
			{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""},
			{"__typename":"CheckRun","status":"QUEUED","conclusion":""},
			{"__typename":"StatusContext","state":"SUCCESS"},
			{"__typename":"StatusContext","state":"PENDING"},
			{"__typename":"StatusContext","state":"ERROR"}]}`, PROpen, PRChecks{Passing: 3, Failing: 3, Pending: 3}},
		{"merged", `{"state":"MERGED","isDraft":false,"mergedAt":"2026-10-05T10:00:00Z","statusCheckRollup":null}`, PRMerged, PRChecks{}},
		{"merged by date only", `{"state":"OPEN","isDraft":false,"mergedAt":"2026-10-05T10:00:00Z"}`, PRMerged, PRChecks{}},
		{"closed", `{"state":"CLOSED","isDraft":true,"mergedAt":null}`, PRClosed, PRChecks{}},
	} {
		got, err := parsePR([]byte(c.json))
		if err != nil || got.State != c.state || got.Checks != c.checks {
			t.Errorf("%s: %+v %v, want %s %+v", c.name, got, err, c.state, c.checks)
		}
	}
	if _, err := parsePR([]byte("Creating draft pull request")); err == nil {
		t.Error("text is not JSON")
	}
}

func TestMergedPRMovesTheCardToDoneOnce(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, c, _ := startCardSession(t, f)
	opened, err := f.svc.OpenPR(se.ID)
	if err != nil || opened.PR == "" {
		t.Fatal(opened.PR, err)
	}

	// No gh: nothing is known, nothing breaks, and it is not asked again at once.
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{}, errors.New("gh is not on PATH") }
	got := f.svc.RefreshPR(se.ID)
	if got.PRState != "" || got.PRChecks != nil || got.PRChecked.IsZero() {
		t.Errorf("gh missing: %+v", got)
	}

	f.svc.PRView = func(_, url string) (PRInfo, error) {
		if url != opened.PR {
			t.Errorf("asked about %s", url)
		}
		return PRInfo{State: PROpen, Checks: PRChecks{Passing: 2, Pending: 1}}, nil
	}
	got = f.svc.RefreshPR(se.ID)
	if got.PRState != PROpen || got.PRChecks == nil || got.PRChecks.Passing != 2 || got.PRChecks.Pending != 1 {
		t.Errorf("open: %+v", got)
	}
	if card := f.card(t, c.ID); card.Column == ColumnDone || card.Done {
		t.Errorf("an open PR must not finish the card: %+v", card)
	}

	f.svc.PRView = func(string, string) (PRInfo, error) {
		return PRInfo{State: PRMerged, Checks: PRChecks{Passing: 3}}, nil
	}
	got = f.svc.RefreshPR(se.ID)
	if got.PRState != PRMerged || !got.CardDone {
		t.Errorf("merged: %+v", got)
	}
	if card := f.card(t, c.ID); card.Column != ColumnDone || !card.Done {
		t.Errorf("card after merge: %+v", card)
	}

	// Moved again by hand: the next refresh leaves it alone.
	if err := f.moveBack(c.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.RefreshPR(se.ID)
	if card := f.card(t, c.ID); card.Column == ColumnDone {
		t.Errorf("the card was moved to Done twice: %+v", card)
	}
}

func TestPollPRsSkipsFreshAndMerged(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	opened, err := f.svc.OpenPR(se.ID)
	if err != nil {
		t.Fatal(err)
	}
	calls := make(chan string, 8)
	f.svc.PRView = func(_, url string) (PRInfo, error) { calls <- url; return PRInfo{State: PRDraft}, nil }
	f.svc.PollPRs()
	select {
	case <-calls:
	case <-time.After(10 * time.Second):
		t.Fatal("a stale PR was not refreshed")
	}
	// Wait for the refresh to settle, then a second poll inside the minute does nothing.
	deadline := time.Now().Add(10 * time.Second)
	for f.svc.mustGet(opened.ID).PRState != PRDraft && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	f.svc.PollPRs()
	select {
	case u := <-calls:
		t.Errorf("asked again inside a minute: %s", u)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestRefreshPRRoute(t *testing.T) {
	f := newFixture(t, newRepo(t, true))
	se, _, _ := startCardSession(t, f)
	if _, err := f.svc.OpenPR(se.ID); err != nil {
		t.Fatal(err)
	}
	f.svc.PRView = func(string, string) (PRInfo, error) { return PRInfo{State: PRMerged}, nil }
	mux := http.NewServeMux()
	Register(mux, f.svc)
	for _, c := range []struct {
		id      string
		confirm bool
		want    int
	}{{se.ID, false, http.StatusForbidden}, {"nope", true, http.StatusNotFound}, {se.ID, true, http.StatusOK}} {
		req := httptest.NewRequest(http.MethodPost, "/api/work/sessions/"+c.id+"/pr/refresh", nil)
		if c.confirm {
			req.Header.Set("X-Lucid-Confirm", "yes")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s confirm=%v: %d %s", c.id, c.confirm, rec.Code, rec.Body)
		}
	}
	if got := f.svc.mustGet(se.ID); got.PRState != PRMerged || !got.CardDone {
		t.Errorf("after refresh: %+v", got)
	}
}

func TestSummaryIsSmall(t *testing.T) {
	big := strings.Repeat("x", 20<<10)
	se := Session{
		ID: "abcd1234", Provider: "claude", Project: "demo", Title: "t", Prompt: big, Answer: big,
		Status: StatusDone, Started: time.Now(), AllowedCommands: DefaultAllowed(t.TempDir(), []string{"go", "gofmt"}),
		Diff: &Diff{Base: "main", Stat: " a.go | 2 +-", Added: 1, Deleted: 1,
			Commits: []Commit{{SHA: strings.Repeat("a", 40), Subject: "feat: x"}},
			Files:   []FileChange{{Path: "a.go", Added: 1, Deleted: 1, Patch: big, Cut: true}}},
	}
	b, _ := json.Marshal(se.Summary())
	if len(b) > 5<<10 {
		t.Errorf("summary is %d bytes", len(b))
	}
	if strings.Contains(string(b), "xxxx") {
		t.Error("a patch, prompt or answer is still in the summary")
	}
	sum := se.Summary()
	if sum.Diff.Files[0].Path != "a.go" || sum.Diff.Files[0].Added != 1 || len(sum.Diff.Commits) != 1 {
		t.Errorf("counts lost: %+v", sum.Diff)
	}
	// The original keeps its bulk.
	if se.Prompt != big || se.Diff.Files[0].Patch != big {
		t.Error("Summary changed the session it was called on")
	}
}
