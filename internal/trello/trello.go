// Package trello reads Trello boards (lists and cards), adds cards and moves
// them between lists, and links native board cards to Trello cards.
//
// The remote owns a Trello card: a native card only keeps its id and URL, and
// nothing is synced back. The key and token are read from the environment
// variables named by integrations.trello.key and .token when a request needs
// them. They are sent in an Authorization header, never in a URL, so a
// transport error cannot quote them; they are never logged, stored or
// returned.
package trello

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
	"github.com/ChinmayGit8765/lucidbench/internal/memory"
)

// Service talks to Trello. Build it with New; tests set the fields directly.
type Service struct {
	API    string
	Key    config.SecretRef
	Token  config.SecretRef
	Getenv func(string) string
	Doer   extapi.Doer
	Cache  extapi.Cache
	// Open opens the vault for the link route.
	Open memory.Opener
}

// New returns a Service for the real environment.
func New(c config.TrelloConfig, open memory.Opener) *Service {
	return &Service{API: strings.TrimRight(c.APIURL, "/"), Key: c.Key, Token: c.Token, Getenv: os.Getenv, Open: open}
}

// Status says whether the connector can work, without calling Trello.
type Status struct {
	// Configured is true when both the key and the token variables are set.
	Configured bool `json:"configured"`
	// KeySet and TokenSet say which is missing.
	KeySet   bool `json:"key_set"`
	TokenSet bool `json:"token_set"`
	// KeyRef and TokenRef are the references (env:NAME), never the values.
	KeyRef   string `json:"key_ref"`
	TokenRef string `json:"token_ref"`
}

// Status reports the credential state.
func (s *Service) Status() Status {
	k, t := s.secret(s.Key), s.secret(s.Token)
	return Status{Configured: k != "" && t != "", KeySet: k != "", TokenSet: t != "", KeyRef: s.Key.String(), TokenRef: s.Token.String()}
}

func (s *Service) secret(r config.SecretRef) string {
	if s.Getenv == nil || r == "" {
		return ""
	}
	return strings.TrimSpace(r.Resolve(s.Getenv))
}

// ---- wire types ----

// Board is a Trello board.
type Board struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	LastActivity string `json:"last_activity,omitempty"`
}

// List is a column of a board.
type List struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	Pos  float64 `json:"pos"`
}

// Label is a card label.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// Card is a Trello card.
type Card struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Desc      string  `json:"desc,omitempty"`
	ListID    string  `json:"list_id"`
	BoardID   string  `json:"board_id,omitempty"`
	URL       string  `json:"url"`
	Due       string  `json:"due,omitempty"`
	Pos       float64 `json:"pos"`
	Labels    []Label `json:"labels"`
	ShortLink string  `json:"short_link,omitempty"`
}

// BoardView is a board with its open lists and cards.
type BoardView struct {
	Board Board  `json:"board"`
	Lists []List `json:"lists"`
	Cards []Card `json:"cards"`
}

type rawCard struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Desc      string  `json:"desc"`
	IDList    string  `json:"idList"`
	IDBoard   string  `json:"idBoard"`
	URL       string  `json:"url"`
	Due       *string `json:"due"`
	Pos       float64 `json:"pos"`
	ShortLink string  `json:"shortLink"`
	Labels    []struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"labels"`
}

func (r rawCard) card() Card {
	c := Card{ID: r.ID, Name: r.Name, Desc: r.Desc, ListID: r.IDList, BoardID: r.IDBoard, URL: r.URL, Pos: r.Pos, ShortLink: r.ShortLink, Labels: []Label{}}
	if r.Due != nil {
		c.Due = *r.Due
	}
	for _, l := range r.Labels {
		c.Labels = append(c.Labels, Label{Name: l.Name, Color: l.Color})
	}
	return c
}

const cardFields = "name,desc,idList,idBoard,url,due,pos,labels,shortLink"

// ---- HTTP ----

// ErrBadID means an id is not shaped like a Trello id, which are
// alphanumeric: it never reaches a URL path.
var ErrBadID = errors.New("not a Trello id")

var idRE = regexp.MustCompile(`^[A-Za-z0-9]{1,40}$`)

func checkID(id string) error {
	if !idRE.MatchString(id) {
		return ErrBadID
	}
	return nil
}

// call sends one request. query values go in the URL, form values in a form
// body; the credentials go in the Authorization header.
func (s *Service) call(ctx context.Context, method, path string, query, form url.Values, out any) error {
	key, tok := s.secret(s.Key), s.secret(s.Token)
	if key == "" || tok == "" {
		return extapi.ErrNotConfigured
	}
	u := s.API + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	body, err := s.Doer.Do(ctx, func() (*http.Request, error) {
		var rd *strings.Reader
		if form != nil {
			rd = strings.NewReader(form.Encode())
		} else {
			rd = strings.NewReader("")
		}
		req, err := http.NewRequest(method, u, rd)
		if err != nil {
			return nil, err
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf(`OAuth oauth_consumer_key="%s", oauth_token="%s"`, key, tok))
		return req, nil
	})
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return extapi.ErrRemote
	}
	return nil
}

// Boards lists the open boards of the member. Cached for a minute.
func (s *Service) Boards(ctx context.Context) ([]Board, error) {
	if v, ok := s.Cache.Get("boards"); ok {
		return v.([]Board), nil
	}
	var raw []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		URL          string `json:"url"`
		LastActivity string `json:"dateLastActivity"`
	}
	q := url.Values{"filter": {"open"}, "fields": {"name,url,dateLastActivity"}}
	if err := s.call(ctx, http.MethodGet, "/members/me/boards", q, nil, &raw); err != nil {
		return nil, err
	}
	out := []Board{}
	for _, b := range raw {
		out = append(out, Board{ID: b.ID, Name: b.Name, URL: b.URL, LastActivity: b.LastActivity})
	}
	s.Cache.Put("boards", out)
	return out, nil
}

// MyCards lists the open cards the member is assigned to, on every board.
// Cached for a minute.
func (s *Service) MyCards(ctx context.Context) ([]Card, error) {
	if v, ok := s.Cache.Get("mycards"); ok {
		return v.([]Card), nil
	}
	var raw []rawCard
	if err := s.call(ctx, http.MethodGet, "/members/me/cards", url.Values{"filter": {"open"}, "fields": {cardFields}}, nil, &raw); err != nil {
		return nil, err
	}
	out := []Card{}
	for _, c := range raw {
		out = append(out, c.card())
	}
	s.Cache.Put("mycards", out)
	return out, nil
}

// Board returns one board with its open lists and cards. Cached for a minute.
func (s *Service) Board(ctx context.Context, id string) (*BoardView, error) {
	if err := checkID(id); err != nil {
		return nil, extapi.ErrBadRequest
	}
	if v, ok := s.Cache.Get("board|" + id); ok {
		return v.(*BoardView), nil
	}
	var b struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := s.call(ctx, http.MethodGet, "/boards/"+id, url.Values{"fields": {"name,url"}}, nil, &b); err != nil {
		return nil, err
	}
	var lists []struct {
		ID   string  `json:"id"`
		Name string  `json:"name"`
		Pos  float64 `json:"pos"`
	}
	if err := s.call(ctx, http.MethodGet, "/boards/"+id+"/lists", url.Values{"filter": {"open"}, "fields": {"name,pos"}}, nil, &lists); err != nil {
		return nil, err
	}
	var cards []rawCard
	if err := s.call(ctx, http.MethodGet, "/boards/"+id+"/cards", url.Values{"filter": {"open"}, "fields": {cardFields}}, nil, &cards); err != nil {
		return nil, err
	}
	v := &BoardView{Board: Board{ID: b.ID, Name: b.Name, URL: b.URL}, Lists: []List{}, Cards: []Card{}}
	for _, l := range lists {
		v.Lists = append(v.Lists, List{ID: l.ID, Name: l.Name, Pos: l.Pos})
	}
	for _, c := range cards {
		v.Cards = append(v.Cards, c.card())
	}
	s.Cache.Put("board|"+id, v)
	return v, nil
}

// NewCard is the input of AddCard.
type NewCard struct {
	List string `json:"list"`
	Name string `json:"name"`
	Desc string `json:"desc,omitempty"`
}

// AddCard adds a card at the bottom of a list and drops the cached reads.
func (s *Service) AddCard(ctx context.Context, in NewCard) (*Card, error) {
	name := strings.Join(strings.Fields(in.Name), " ")
	if checkID(in.List) != nil || name == "" {
		return nil, extapi.ErrBadRequest
	}
	form := url.Values{"idList": {in.List}, "name": {name}, "pos": {"bottom"}}
	if in.Desc != "" {
		form.Set("desc", in.Desc)
	}
	var r rawCard
	if err := s.call(ctx, http.MethodPost, "/cards", url.Values{"fields": {cardFields}}, form, &r); err != nil {
		return nil, err
	}
	s.Cache.Clear()
	c := r.card()
	return &c, nil
}

// MoveCard moves a card to the bottom of another list and drops the cached reads.
func (s *Service) MoveCard(ctx context.Context, id, list string) (*Card, error) {
	if checkID(id) != nil || checkID(list) != nil {
		return nil, extapi.ErrBadRequest
	}
	var r rawCard
	form := url.Values{"idList": {list}, "pos": {"bottom"}}
	if err := s.call(ctx, http.MethodPut, "/cards/"+id, url.Values{"fields": {cardFields}}, form, &r); err != nil {
		return nil, err
	}
	s.Cache.Clear()
	c := r.card()
	return &c, nil
}

var urlRE = regexp.MustCompile(`^/c/([A-Za-z0-9]{8})(?:/|$)`)

// ErrBadCardRef means the text is neither a card id nor a Trello card URL.
var ErrBadCardRef = errors.New("not a Trello card link or id")

// ParseCardRef takes a card URL (trello.com/c/<shortLink>/...), a short link
// or a card id.
func ParseCardRef(s string) (string, error) {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && (u.Host == "trello.com" || u.Host == "www.trello.com") {
		if m := urlRE.FindStringSubmatch(u.Path); m != nil {
			return m[1], nil
		}
		return "", ErrBadCardRef
	}
	if checkID(s) != nil {
		return "", ErrBadCardRef
	}
	return s, nil
}

// Card fetches one card by id or short link.
func (s *Service) Card(ctx context.Context, ref string) (*Card, error) {
	id, err := ParseCardRef(ref)
	if err != nil {
		return nil, extapi.ErrBadRequest
	}
	var r rawCard
	if err := s.call(ctx, http.MethodGet, "/cards/"+id, url.Values{"fields": {cardFields}}, nil, &r); err != nil {
		return nil, err
	}
	c := r.card()
	return &c, nil
}
