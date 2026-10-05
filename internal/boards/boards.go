// Package boards stores kanban boards as Markdown in the Memory vault, in the
// Obsidian Kanban plugin's format, so the same file opens in both.
//
// A board is Boards/<id>.md:
//
//	---
//	kanban-plugin: basic
//	board: work
//	title: Work
//	---
//
//	## Inbox
//
//	- [ ] Card title
//		id:: c-7f3a
//		project:: my-app
//
//	## Done
//
//	**Complete**
//
//	%% kanban:settings
//	...
//	%%
//
// Each "## heading" is a column and each "- [ ] line" a card. Indented
// "key:: value" lines under a card are its metadata: id, project, memory,
// council, work, due and labels. Anything else in the file (the plugin's
// settings block, text between cards, metadata keys this package does not
// know) is kept as it was and written back in place.
package boards

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// Errors returned by the package. The HTTP layer maps each to a status.
var (
	ErrBoardNotFound = errors.New("board not found")
	ErrCardNotFound  = errors.New("card not found")
	ErrBadColumn     = errors.New("no such column")
	ErrBadInput      = errors.New("bad input")
)

// DefaultBoard is created on first use.
const DefaultBoard = "work"

// DefaultColumns are the columns of the default board.
var DefaultColumns = []string{"Inbox", "Ready", "In progress", "Review", "Done"}

// Dir is the vault folder that holds the boards.
const Dir = "Boards"

// Card is one task. Column is where it sits; Done mirrors the checkbox.
type Card struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Column  string   `json:"column"`
	Project string   `json:"project,omitempty"`
	Memory  string   `json:"memory,omitempty"`
	Council string   `json:"council,omitempty"`
	Work    string   `json:"work,omitempty"`
	Due     string   `json:"due,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	Done    bool     `json:"done"`

	extra []string // indented lines this package does not know, kept verbatim
}

// Board is a whole board, cards in column order then top to bottom.
type Board struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Columns []string `json:"columns"`
	Cards   []Card   `json:"cards"`
}

// BoardSummary is one row of List.
type BoardSummary struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Columns  int       `json:"columns"`
	Cards    int       `json:"cards"`
	Modified time.Time `json:"modified"`
}

var (
	idRE     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	cardRE   = regexp.MustCompile(`^- \[([ xX])\] ?(.*)$`)
	metaRE   = regexp.MustCompile(`^[ \t]+(id|project|memory|council|work|due|labels):: ?(.*)$`)
	headRE   = regexp.MustCompile(`^## +(.+?) *$`)
	settings = "%% kanban:settings"
)

// mu serialises read-modify-write cycles on boards.
var mu sync.Mutex

// item is one line of a column: a card, or a line kept as it was.
type item struct {
	card *Card
	raw  string
}

type column struct {
	name  string
	items []item
}

// doc is a parsed board file.
type doc struct {
	front   map[string]any
	pre     []string // lines before the first column
	cols    []*column
	trailer []string // from the plugin's settings block to the end
}

func parse(front map[string]any, body string) *doc {
	d := &doc{front: front}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	var cur *column
	var card *Card
	for i, l := range lines {
		if strings.HasPrefix(l, settings) {
			d.trailer = lines[i:]
			break
		}
		if m := headRE.FindStringSubmatch(l); m != nil {
			cur = &column{name: m[1]}
			d.cols = append(d.cols, cur)
			card = nil
			continue
		}
		if cur == nil {
			d.pre = append(d.pre, l)
			continue
		}
		if m := cardRE.FindStringSubmatch(l); m != nil {
			card = &Card{Title: m[2], Column: cur.name, Done: m[1] != " "}
			cur.items = append(cur.items, item{card: card})
			continue
		}
		if card != nil && (strings.HasPrefix(l, "\t") || strings.HasPrefix(l, " ")) && strings.TrimSpace(l) != "" {
			if m := metaRE.FindStringSubmatch(l); m != nil {
				card.set(m[1], strings.TrimSpace(m[2]))
			} else {
				card.extra = append(card.extra, l)
			}
			continue
		}
		card = nil
		cur.items = append(cur.items, item{raw: l})
	}
	return d
}

func (c *Card) set(key, val string) {
	switch key {
	case "id":
		c.ID = val
	case "project":
		c.Project = val
	case "memory":
		c.Memory = val
	case "council":
		c.Council = val
	case "work":
		c.Work = val
	case "due":
		c.Due = val
	case "labels":
		c.Labels = nil
		for _, p := range strings.Split(val, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.Labels = append(c.Labels, p)
			}
		}
	}
}

func (d *doc) body() string {
	var lines []string
	lines = append(lines, d.pre...)
	for _, col := range d.cols {
		lines = append(lines, "## "+col.name)
		for _, it := range col.items {
			if it.card == nil {
				lines = append(lines, it.raw)
				continue
			}
			c := it.card
			box := " "
			if c.Done {
				box = "x"
			}
			lines = append(lines, "- ["+box+"] "+c.Title)
			for _, kv := range [][2]string{
				{"id", c.ID}, {"project", c.Project}, {"memory", c.Memory}, {"council", c.Council},
				{"work", c.Work}, {"due", c.Due}, {"labels", strings.Join(c.Labels, ", ")},
			} {
				if kv[1] != "" {
					lines = append(lines, "\t"+kv[0]+":: "+kv[1])
				}
			}
			lines = append(lines, c.extra...)
		}
	}
	lines = append(lines, d.trailer...)
	return strings.Join(lines, "\n") + "\n"
}

func (d *doc) column(name string) *column {
	for _, c := range d.cols {
		if c.name == name {
			return c
		}
	}
	return nil
}

// find returns the card with id, and the column holding it.
func (d *doc) find(id string) (*Card, *column, int) {
	for _, col := range d.cols {
		for i, it := range col.items {
			if it.card != nil && it.card.ID == id {
				return it.card, col, i
			}
		}
	}
	return nil, nil, -1
}

// insert puts card in col as its index-th card; an index out of range means
// the end.
func (col *column) insert(card *Card, index int) {
	card.Column = col.name
	var cardPos []int
	for i, it := range col.items {
		if it.card != nil {
			cardPos = append(cardPos, i)
		}
	}
	var at int
	switch {
	case index >= 0 && index < len(cardPos):
		at = cardPos[index]
	case len(cardPos) > 0:
		at = cardPos[len(cardPos)-1] + 1
	default:
		// An empty column: after the blank lines and "**Complete**" marker
		// that follow its heading.
		for at < len(col.items) && col.items[at].card == nil && (col.items[at].raw == "" || col.items[at].raw == "**Complete**") {
			at++
		}
	}
	col.items = append(col.items, item{})
	copy(col.items[at+1:], col.items[at:])
	col.items[at] = item{card: card}
	// Keep a blank line after the last card of the column.
	if next := at + 1; at == lastCard(col) && (next >= len(col.items) || col.items[next].card != nil || col.items[next].raw != "") {
		col.items = append(col.items, item{})
		copy(col.items[next+1:], col.items[next:])
		col.items[next] = item{raw: ""}
	}
}

func lastCard(col *column) int {
	n := -1
	for i, it := range col.items {
		if it.card != nil {
			n = i
		}
	}
	return n
}

func (d *doc) remove(col *column, i int) {
	col.items = append(col.items[:i], col.items[i+1:]...)
}

func (d *doc) board(id string) *Board {
	b := &Board{ID: id, Title: id, Columns: []string{}, Cards: []Card{}}
	if t, ok := d.front["title"].(string); ok && strings.TrimSpace(t) != "" {
		b.Title = strings.TrimSpace(t)
	}
	for _, col := range d.cols {
		b.Columns = append(b.Columns, col.name)
		for _, it := range col.items {
			if it.card != nil {
				b.Cards = append(b.Cards, *it.card)
			}
		}
	}
	return b
}

func newID(d *doc) string {
	for {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			panic(err) // crypto/rand does not fail on supported platforms
		}
		id := "c-" + hex.EncodeToString(b[:])
		if c, _, _ := d.find(id); c == nil {
			return id
		}
	}
}

func path(id string) string { return Dir + "/" + id + ".md" }

func checkID(id string) error {
	if !idRE.MatchString(id) || len(id) > 64 {
		return fmt.Errorf("%w: board id %q must be lowercase letters, digits and dashes", ErrBadInput, id)
	}
	return nil
}

// load reads board id. A missing default board is created. Cards without an
// id (a board made in Obsidian) get one, and the file is updated.
func load(v *memory.Vault, id string) (*doc, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	pg, err := v.Read(path(id))
	switch {
	case errors.Is(err, memory.ErrNotFound) && id == DefaultBoard:
		d := newDefault()
		return d, save(v, id, d)
	case errors.Is(err, memory.ErrNotFound):
		return nil, fmt.Errorf("%w: %s", ErrBoardNotFound, id)
	case err != nil:
		return nil, err
	}
	d := parse(pg.Front, pg.Body)
	if d.front == nil {
		d.front = map[string]any{}
	}
	changed := false
	for _, col := range d.cols {
		for _, it := range col.items {
			if it.card != nil && it.card.ID == "" {
				it.card.ID = newID(d)
				changed = true
			}
		}
	}
	if changed {
		if err := save(v, id, d); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func newDefault() *doc {
	d := &doc{
		front: map[string]any{"kanban-plugin": "basic", "board": DefaultBoard, "title": "Work"},
		pre:   []string{""},
	}
	for _, name := range DefaultColumns {
		col := &column{name: name, items: []item{{raw: ""}}}
		if name == "Done" {
			col.items = []item{{raw: ""}, {raw: "**Complete**"}, {raw: ""}}
		}
		d.cols = append(d.cols, col)
	}
	return d
}

func save(v *memory.Vault, id string, d *doc) error {
	return v.Write(&memory.Page{Path: path(id), Front: d.front, Body: d.body()})
}

// List returns every board in the vault's Boards folder.
func List(v *memory.Vault) ([]BoardSummary, error) {
	mu.Lock()
	defer mu.Unlock()
	ents, err := v.List(Dir)
	if errors.Is(err, memory.ErrNotFound) {
		return []BoardSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []BoardSummary{}
	for _, e := range ents {
		if e.Dir || !strings.HasSuffix(e.Name, ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name, ".md")
		if checkID(id) != nil {
			continue
		}
		pg, err := v.Read(e.Path)
		if err != nil {
			continue
		}
		b := parse(pg.Front, pg.Body).board(id)
		out = append(out, BoardSummary{ID: id, Title: b.Title, Columns: len(b.Columns), Cards: len(b.Cards), Modified: e.Modified})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns board id. The default board "work" is created when missing;
// any other missing board is ErrBoardNotFound.
func Get(v *memory.Vault, id string) (*Board, error) {
	mu.Lock()
	defer mu.Unlock()
	d, err := load(v, id)
	if err != nil {
		return nil, err
	}
	return d.board(id), nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func (c *Card) tidy() error {
	c.Title = oneLine(c.Title)
	if c.Title == "" {
		return fmt.Errorf("%w: a card needs a title", ErrBadInput)
	}
	c.Project, c.Memory, c.Council, c.Work, c.Due = oneLine(c.Project), oneLine(c.Memory), oneLine(c.Council), oneLine(c.Work), oneLine(c.Due)
	var labels []string
	for _, l := range c.Labels {
		if l = oneLine(strings.ReplaceAll(l, ",", " ")); l != "" {
			labels = append(labels, l)
		}
	}
	c.Labels = labels
	return nil
}

// AddCard adds c to the end of its column (the first column when none is
// named) and returns it with its new ID.
func AddCard(v *memory.Vault, board string, c Card) (Card, error) {
	mu.Lock()
	defer mu.Unlock()
	d, err := load(v, board)
	if err != nil {
		return Card{}, err
	}
	if err := c.tidy(); err != nil {
		return Card{}, err
	}
	if len(d.cols) == 0 {
		return Card{}, fmt.Errorf("%w: board %s has no columns", ErrBadColumn, board)
	}
	col := d.cols[0]
	if c.Column != "" {
		if col = d.column(c.Column); col == nil {
			return Card{}, fmt.Errorf("%w: %q", ErrBadColumn, c.Column)
		}
	}
	card := &Card{
		ID: newID(d), Title: c.Title, Project: c.Project, Memory: c.Memory, Council: c.Council,
		Work: c.Work, Due: c.Due, Labels: c.Labels, Done: c.Done,
	}
	col.insert(card, -1)
	if err := save(v, board, d); err != nil {
		return Card{}, err
	}
	return *card, nil
}

// UpdateCard replaces the fields of the card with c.ID. A different Column
// moves the card to the end of that column; use MoveCard to pick a position.
// The ID and any lines this package does not know stay as they were.
func UpdateCard(v *memory.Vault, board string, c Card) error {
	mu.Lock()
	defer mu.Unlock()
	d, err := load(v, board)
	if err != nil {
		return err
	}
	card, col, i := d.find(c.ID)
	if card == nil {
		return fmt.Errorf("%w: %q", ErrCardNotFound, c.ID)
	}
	if err := c.tidy(); err != nil {
		return err
	}
	var dst *column
	if c.Column != "" && c.Column != col.name {
		if dst = d.column(c.Column); dst == nil {
			return fmt.Errorf("%w: %q", ErrBadColumn, c.Column)
		}
	}
	card.Title, card.Project, card.Memory, card.Council, card.Work, card.Due, card.Labels, card.Done =
		c.Title, c.Project, c.Memory, c.Council, c.Work, c.Due, c.Labels, c.Done
	if dst != nil {
		d.remove(col, i)
		dst.insert(card, -1)
	}
	return save(v, board, d)
}

// MoveCard moves a card to column as its index-th card. An index below zero
// or past the end means the bottom. Moving does not tick the card.
func MoveCard(v *memory.Vault, board, cardID, column string, index int) error {
	mu.Lock()
	defer mu.Unlock()
	d, err := load(v, board)
	if err != nil {
		return err
	}
	card, col, i := d.find(cardID)
	if card == nil {
		return fmt.Errorf("%w: %q", ErrCardNotFound, cardID)
	}
	dst := d.column(column)
	if dst == nil {
		return fmt.Errorf("%w: %q", ErrBadColumn, column)
	}
	d.remove(col, i)
	dst.insert(card, index)
	return save(v, board, d)
}
