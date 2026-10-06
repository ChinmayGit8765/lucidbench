package boards

import (
	"strings"
	"testing"
)

func TestRemoteLinksRoundTrip(t *testing.T) {
	v := vaultWith(t, nil)
	c, err := AddCard(v, "work", Card{Title: "Ship it", Linear: "ENG-12", LinearURL: "https://linear.app/acme/issue/ENG-12/ship-it", Trello: "5f1c", TrelloURL: "https://trello.com/c/abc"})
	if err != nil {
		t.Fatal(err)
	}
	file := fileOf(t, v, "work")
	for _, want := range []string{"\tlinear:: ENG-12\n", "\tlinear_url:: https://linear.app/acme/issue/ENG-12/ship-it\n", "\ttrello:: 5f1c\n", "\ttrello_url:: https://trello.com/c/abc\n"} {
		if !strings.Contains(file, want) {
			t.Errorf("file lacks %q:\n%s", want, file)
		}
	}
	// A later edit of another field keeps the links; clearing them removes the lines.
	c.Title = "Ship it now"
	if err := UpdateCard(v, "work", c); err != nil {
		t.Fatal(err)
	}
	b, _ := Get(v, "work")
	if got := b.Cards[0]; got.Linear != "ENG-12" || got.TrelloURL != "https://trello.com/c/abc" || got.Title != "Ship it now" {
		t.Errorf("after update %+v", got)
	}
	c.Linear, c.LinearURL = "", ""
	if err := UpdateCard(v, "work", c); err != nil {
		t.Fatal(err)
	}
	if file = fileOf(t, v, "work"); strings.Contains(file, "linear") || !strings.Contains(file, "trello:: 5f1c") {
		t.Errorf("after clearing:\n%s", file)
	}
}
