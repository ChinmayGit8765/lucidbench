package boards

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// sample is a board as the Obsidian Kanban plugin writes it: blank lines
// around the front matter and between columns, a **Complete** marker, a
// checked card, inline dates and tags in titles, a note under a card, a
// metadata key this package does not know, and the settings block at the end.
const sample = "---\n\nkanban-plugin: board\n\n---\n\n## Backlog\n\n- [ ] Write the spec #docs\n- [ ] Call the vendor @{2026-10-10}\n\tA note the plugin keeps under the card\n- [ ] Tracked task\n\tid:: c-1111\n\tproject:: my-app\n\tmemory:: Inbox/brief.md\n\tcustom:: keep me\n\tlabels:: a, b\n\n\n## In progress\n\n- [ ] Wire up login\n\n\n## Done\n\n**Complete**\n\n- [x] Set up the repo\n\n\n\n%% kanban:settings\n```\n{\"kanban-plugin\":\"board\",\"list-collapse\":[false,false,false]}\n```\n%%\n"

func vaultWith(t *testing.T, files map[string]string) *memory.Vault {
	t.Helper()
	v, err := memory.Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		abs := filepath.Join(v.Root(), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func fileOf(t *testing.T, v *memory.Vault, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(v.Root(), "Boards", id+".md"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

func titles(b *Board, col string) []string {
	var out []string
	for _, c := range b.Cards {
		if c.Column == col {
			out = append(out, c.Title)
		}
	}
	return out
}

func TestDefaultBoardCreatedOnFirstGet(t *testing.T) {
	v := vaultWith(t, nil)
	if _, err := Get(v, "other"); !errors.Is(err, ErrBoardNotFound) {
		t.Errorf("unknown board = %v", err)
	}
	if list, _ := List(v); len(list) != 0 {
		t.Errorf("list before first use = %+v", list)
	}
	b, err := Get(v, "work")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.Columns, []string{"Inbox", "Ready", "In progress", "Review", "Done"}) || b.Title != "Work" || len(b.Cards) != 0 {
		t.Errorf("board = %+v", b)
	}
	f := fileOf(t, v, "work")
	for _, want := range []string{"kanban-plugin: basic", "board: work", "## In progress", "**Complete**"} {
		if !strings.Contains(f, want) {
			t.Errorf("file lacks %q:\n%s", want, f)
		}
	}
	if list, _ := List(v); len(list) != 1 || list[0].ID != "work" || list[0].Columns != 5 {
		t.Errorf("list = %+v", list)
	}
	// A second Get changes nothing.
	if _, err := Get(v, "work"); err != nil || fileOf(t, v, "work") != f {
		t.Errorf("second Get rewrote the board: %v", err)
	}
	for _, bad := range []string{"../x", "Work", "a b", "", "a/b", ".hidden"} {
		if _, err := Get(v, bad); !errors.Is(err, ErrBadInput) {
			t.Errorf("Get(%q) = %v", bad, err)
		}
	}
}

func TestParseObsidianSample(t *testing.T) {
	v := vaultWith(t, map[string]string{"Boards/sample.md": sample})
	b, err := Get(v, "sample")
	if err != nil {
		t.Fatal(err)
	}
	if b.Title != "sample" || !reflect.DeepEqual(b.Columns, []string{"Backlog", "In progress", "Done"}) {
		t.Fatalf("board = %+v", b)
	}
	if got := titles(b, "Backlog"); !reflect.DeepEqual(got, []string{"Write the spec #docs", "Call the vendor @{2026-10-10}", "Tracked task"}) {
		t.Errorf("backlog = %v", got)
	}
	var tracked, done Card
	for _, c := range b.Cards {
		switch c.Title {
		case "Tracked task":
			tracked = c
		case "Set up the repo":
			done = c
		}
		if !strings.HasPrefix(c.ID, "c-") || len(c.ID) != 6 {
			t.Errorf("card %q id = %q", c.Title, c.ID)
		}
	}
	if tracked.ID != "c-1111" || tracked.Project != "my-app" || tracked.Memory != "Inbox/brief.md" || !reflect.DeepEqual(tracked.Labels, []string{"a", "b"}) {
		t.Errorf("tracked = %+v", tracked)
	}
	if !done.Done || done.Column != "Done" {
		t.Errorf("done = %+v", done)
	}
	// Get gave the id-less cards an id and saved it; nothing else was lost.
	f := fileOf(t, v, "sample")
	for _, keep := range []string{
		"kanban-plugin: board", "\tA note the plugin keeps under the card\n", "\tcustom:: keep me\n", "**Complete**\n",
		"%% kanban:settings\n```\n{\"kanban-plugin\":\"board\",\"list-collapse\":[false,false,false]}\n```\n%%\n",
	} {
		if !strings.Contains(f, keep) {
			t.Errorf("lost %q:\n%s", keep, f)
		}
	}
	if strings.Count(f, "id:: c-") != 5 {
		t.Errorf("want 5 card ids in the file:\n%s", f)
	}
	// Once ids exist, reading again is a no-op.
	if _, err := Get(v, "sample"); err != nil || fileOf(t, v, "sample") != f {
		t.Errorf("second Get rewrote the file: %v", err)
	}
}

func TestRoundTripStable(t *testing.T) {
	front, body := memory.SplitFront(sample)
	d1 := parse(front, body)
	out1 := d1.body()
	d2 := parse(front, out1)
	out2 := d2.body()
	if out1 != out2 {
		t.Errorf("write is not stable:\n%s\n---\n%s", out1, out2)
	}
	if !reflect.DeepEqual(d1.board("x"), d2.board("x")) {
		t.Errorf("parse(write(parse)) differs:\n%+v\n%+v", d1.board("x"), d2.board("x"))
	}
	// Unknown lines and the settings block survive the first normalisation.
	for _, keep := range []string{"\tA note the plugin keeps under the card", "\tcustom:: keep me", "**Complete**", "%% kanban:settings", "list-collapse"} {
		if !strings.Contains(out1, keep) {
			t.Errorf("lost %q", keep)
		}
	}
	// CRLF files read the same.
	crlf := parse(front, strings.ReplaceAll(body, "\n", "\r\n")).body()
	if crlf != out1 {
		t.Errorf("CRLF board differs:\n%q\n%q", crlf, out1)
	}
}

func TestAddUpdateMove(t *testing.T) {
	v := vaultWith(t, map[string]string{"Boards/sample.md": sample})
	a, err := AddCard(v, "sample", Card{Title: "  New\ncard  ", Column: "In progress", Project: "p", Labels: []string{"x", " y,z "}, Due: "2026-11-01"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "New card" || !strings.HasPrefix(a.ID, "c-") || a.Column != "In progress" || !reflect.DeepEqual(a.Labels, []string{"x", "y z"}) {
		t.Errorf("added = %+v", a)
	}
	b2, _ := AddCard(v, "sample", Card{Title: "Second"}) // no column: the first
	if b2.Column != "Backlog" || b2.ID == a.ID {
		t.Errorf("second = %+v", b2)
	}
	if _, err := AddCard(v, "sample", Card{Title: "x", Column: "Nope"}); !errors.Is(err, ErrBadColumn) {
		t.Errorf("bad column = %v", err)
	}
	if _, err := AddCard(v, "sample", Card{Title: " \n "}); !errors.Is(err, ErrBadInput) {
		t.Errorf("empty title = %v", err)
	}
	b, _ := Get(v, "sample")
	if got := titles(b, "In progress"); !reflect.DeepEqual(got, []string{"Wire up login", "New card"}) {
		t.Errorf("in progress = %v", got)
	}
	if got := titles(b, "Backlog"); got[len(got)-1] != "Second" {
		t.Errorf("backlog = %v", got)
	}

	// Update fields in place, and via Column to the end of another column.
	a.Title, a.Done, a.Memory = "Renamed", true, "Inbox/n.md"
	if err := UpdateCard(v, "sample", a); err != nil {
		t.Fatal(err)
	}
	a.Column = "Done"
	if err := UpdateCard(v, "sample", a); err != nil {
		t.Fatal(err)
	}
	b, _ = Get(v, "sample")
	if got := titles(b, "Done"); !reflect.DeepEqual(got, []string{"Set up the repo", "Renamed"}) {
		t.Errorf("done = %v", got)
	}
	if err := UpdateCard(v, "sample", Card{ID: "c-nope", Title: "x"}); !errors.Is(err, ErrCardNotFound) {
		t.Errorf("update missing = %v", err)
	}

	// Move to a position; moving does not tick.
	if err := MoveCard(v, "sample", b2.ID, "Backlog", 0); err != nil {
		t.Fatal(err)
	}
	if err := MoveCard(v, "sample", "c-1111", "Done", 1); err != nil {
		t.Fatal(err)
	}
	b, _ = Get(v, "sample")
	if got := titles(b, "Backlog"); got[0] != "Second" || len(got) != 3 {
		t.Errorf("backlog = %v", got)
	}
	if got := titles(b, "Done"); !reflect.DeepEqual(got, []string{"Set up the repo", "Tracked task", "Renamed"}) {
		t.Errorf("done = %v", got)
	}
	for _, c := range b.Cards {
		if c.ID == "c-1111" && (c.Done || c.Project != "my-app") {
			t.Errorf("moved card = %+v", c)
		}
	}
	// An index past the end means the bottom; an empty column works.
	if err := MoveCard(v, "sample", "c-1111", "Done", 99); err != nil {
		t.Fatal(err)
	}
	if err := MoveCard(v, "sample", b2.ID, "Backlog", -1); err != nil {
		t.Fatal(err)
	}
	if err := MoveCard(v, "sample", "c-1111", "Nope", 0); !errors.Is(err, ErrBadColumn) {
		t.Errorf("move to bad column = %v", err)
	}
	if err := MoveCard(v, "sample", "c-zzzz", "Done", 0); !errors.Is(err, ErrCardNotFound) {
		t.Errorf("move missing = %v", err)
	}

	// Through all that, the foreign lines and the settings block stayed.
	f := fileOf(t, v, "sample")
	for _, keep := range []string{"\tA note the plugin keeps under the card\n", "\tcustom:: keep me\n", "**Complete**\n", "%% kanban:settings\n```\n{\"kanban-plugin\":\"board\"", "kanban-plugin: board"} {
		if !strings.Contains(f, keep) {
			t.Errorf("lost %q:\n%s", keep, f)
		}
	}
	// And the file is stable: parse and write again gives the same bytes.
	front, body := memory.SplitFront(f)
	if out := parse(front, body).body(); out != body {
		t.Errorf("file is not a fixed point:\n%s\n---\n%s", body, out)
	}
}

func TestMoveIntoEmptyColumnOfDefaultBoard(t *testing.T) {
	v := vaultWith(t, nil)
	c1, err := AddCard(v, "work", Card{Title: "one"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MoveCard(v, "work", c1.ID, "Done", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := AddCard(v, "work", Card{Title: "two", Column: "Done"}); err != nil {
		t.Fatal(err)
	}
	b, _ := Get(v, "work")
	if got := titles(b, "Done"); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Errorf("done = %v", got)
	}
	f := fileOf(t, v, "work")
	if !strings.Contains(f, "## Done\n\n**Complete**\n\n- [ ] one\n\tid:: "+c1.ID+"\n- [ ] two\n") {
		t.Errorf("layout:\n%s", f)
	}
	front, body := memory.SplitFront(f)
	if out := parse(front, body).body(); out != body {
		t.Errorf("not a fixed point:\n%s", out)
	}
}
