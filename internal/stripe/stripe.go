// Package stripe reads a Stripe account and creates the few objects a payment
// setup needs (products, prices, payment links and webhook endpoints), in test
// mode only.
//
// Safety rules, enforced here rather than by callers:
//
//   - The key is read from the environment variable named by
//     integrations.stripe.key when a request needs it. It travels in an
//     Authorization header, never in a URL, and is never logged, stored or
//     returned. Failures are reduced to the fixed errors of internal/extapi
//     (plus Stripe's short error code when it is a plain identifier), so
//     neither a transport error nor anything Stripe wrote can carry a key.
//   - The mode (test or live) is read from the key prefix on every call. A key
//     of an unknown shape counts as live.
//   - Create is the only write. It refuses every path outside a short
//     allow-list and refuses everything when the mode is not test. There is no
//     refund, payout or transfer call, in any mode, so this package cannot
//     move money.
package stripe

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
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
)

// Mode is whether a key works on test data or on real money.
type Mode string

// The modes. Anything that is not a recognisable test key is treated as live
// or unknown, and writes are refused for both.
const (
	ModeTest    Mode = "test"
	ModeLive    Mode = "live"
	ModeUnknown Mode = "unknown"
)

// ModeOf reads the mode from a key's prefix: secret and restricted keys look
// like <kind>_<mode>_<random>. The prefix is never returned or logged.
func ModeOf(key string) Mode {
	parts := strings.SplitN(strings.TrimSpace(key), "_", 3)
	if len(parts) != 3 || parts[2] == "" || (parts[0] != "sk" && parts[0] != "rk") {
		return ModeUnknown
	}
	switch parts[1] {
	case "test":
		return ModeTest
	case "live":
		return ModeLive
	}
	return ModeUnknown
}

// Errors of the write path.
var (
	// ErrLiveWrite means a write was attempted without a test-mode key.
	ErrLiveWrite = errors.New("live mode writes are not supported in this version")
	// ErrNotAllowed means a write to a path outside the allow-list.
	ErrNotAllowed = errors.New("that Stripe call is not allowed")
	// ErrNoIdempotencyKey means a write without an Idempotency-Key.
	ErrNoIdempotencyKey = errors.New("a write needs an idempotency key")
)

// writePaths are the only endpoints Create will POST to.
var writePaths = map[string]bool{
	"/v1/products":          true,
	"/v1/prices":            true,
	"/v1/payment_links":     true,
	"/v1/webhook_endpoints": true,
}

// Error is a failed call that Stripe described with a plain error code.
type Error struct {
	Err    error
	Status int
	Code   string
}

func (e *Error) Error() string {
	if e.Code == "" {
		return e.Err.Error()
	}
	return e.Err.Error() + " (" + e.Code + ")"
}

// Unwrap lets errors.Is match the extapi error underneath.
func (e *Error) Unwrap() error { return e.Err }

// Code returns Stripe's error code of a failed call, or "".
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// StatusOf returns the HTTP status of a failed call that carried a code, or 0.
func StatusOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

var codeRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// classify turns an error answer into an extapi error carrying Stripe's code.
// Only a code of plain lower-case words is kept, never a message.
func classify(status int, body []byte) error {
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil || !codeRE.MatchString(e.Error.Code) {
		return nil
	}
	base := extapi.ErrBadRequest
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		base = extapi.ErrUnauthorized
	case status == http.StatusNotFound:
		base = extapi.ErrNotFound
	case status >= 500:
		base = extapi.ErrRemote
	}
	return &Error{Err: base, Code: e.Error.Code, Status: status}
}

// Service talks to Stripe. Build it with New; tests set the fields directly.
type Service struct {
	API    string
	Key    config.SecretRef
	Getenv func(string) string
	Doer   extapi.Doer
	Cache  extapi.Cache
	// Now is the clock; nil uses time.Now.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// New returns a Service for the real environment.
func New(c config.StripeConfig) *Service {
	s := &Service{API: strings.TrimRight(c.APIURL, "/"), Key: c.Key, Getenv: os.Getenv}
	s.Doer.Classify = classify
	return s
}

func (s *Service) key() string {
	if s.Getenv == nil || s.Key == "" {
		return ""
	}
	return strings.TrimSpace(s.Key.Resolve(s.Getenv))
}

// Mode reports the mode of the configured key; ModeUnknown when none is set.
func (s *Service) Mode() Mode { return ModeOf(s.key()) }

// Status says whether the connector can work, without calling Stripe.
type Status struct {
	Configured bool `json:"configured"`
	// KeyRef is the reference (env:NAME), never the value.
	KeyRef string `json:"key_ref"`
	// Mode is "test", "live" or "unknown" (not set, or not a Stripe key).
	Mode Mode `json:"mode"`
	// WritesAllowed is true only for a test-mode key.
	WritesAllowed bool `json:"writes_allowed"`
}

// Status reports the credential state.
func (s *Service) Status() Status {
	m := s.Mode()
	return Status{Configured: s.key() != "", KeyRef: s.Key.String(), Mode: m, WritesAllowed: m == ModeTest}
}

// call sends one request. Query values go in the URL, form values in a
// form-encoded body, the way the Stripe API takes them.
func (s *Service) call(ctx context.Context, method, path string, query, form url.Values, idem string, out any) error {
	key := s.key()
	if key == "" {
		return extapi.ErrNotConfigured
	}
	u := s.API + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	body, err := s.Doer.Do(ctx, func() (*http.Request, error) {
		var req *http.Request
		var err error
		if form != nil {
			req, err = http.NewRequest(method, u, strings.NewReader(form.Encode()))
			if err == nil {
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
		} else {
			req, err = http.NewRequest(method, u, nil)
		}
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		if idem != "" {
			req.Header.Set("Idempotency-Key", idem)
		}
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

// Paging limits: a page holds at most 100 objects and a list stops after
// maxPages, so a service that always says has_more cannot loop forever.
const (
	pageSize = 100
	maxPages = 10
)

type page struct {
	Data    []json.RawMessage `json:"data"`
	HasMore bool              `json:"has_more"`
}

// list reads a paginated list (limit, starting_after, has_more) until maxItems
// objects are in. The bool says whether more exist beyond what was returned.
func list[T any](ctx context.Context, s *Service, path string, q url.Values, maxItems int) ([]T, bool, error) {
	items := []T{}
	after := ""
	for n := 0; n < maxPages; n++ {
		q2 := url.Values{}
		for k, v := range q {
			q2[k] = v
		}
		q2.Set("limit", fmt.Sprint(min(pageSize, maxItems-len(items))))
		if after != "" {
			q2.Set("starting_after", after)
		}
		var p page
		if err := s.call(ctx, http.MethodGet, path, q2, nil, "", &p); err != nil {
			return nil, false, err
		}
		for i, raw := range p.Data {
			var t T
			var id struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &t) != nil || json.Unmarshal(raw, &id) != nil {
				return nil, false, extapi.ErrRemote
			}
			items = append(items, t)
			after = id.ID
			if len(items) >= maxItems {
				return items, p.HasMore || i < len(p.Data)-1, nil
			}
		}
		if !p.HasMore || len(p.Data) == 0 || after == "" {
			return items, false, nil
		}
	}
	return items, true, nil
}

// cached runs load unless a fresh result is in the cache.
func cached[T any](s *Service, key string, load func() (T, error)) (T, error) {
	if v, ok := s.Cache.Get(key); ok {
		return v.(T), nil
	}
	v, err := load()
	if err != nil {
		var zero T
		return zero, err
	}
	s.Cache.Put(key, v)
	return v, nil
}

// ---- reads ----

// Account is the connected account.
type Account struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Country         string `json:"country,omitempty"`
	DefaultCurrency string `json:"default_currency,omitempty"`
	ChargesEnabled  bool   `json:"charges_enabled"`
	PayoutsEnabled  bool   `json:"payouts_enabled"`
	Mode            Mode   `json:"mode"`
}

// Account reads the account the key belongs to. Cached for a minute.
func (s *Service) Account(ctx context.Context) (*Account, error) {
	return cached(s, "account", func() (*Account, error) {
		var raw struct {
			ID              string `json:"id"`
			Country         string `json:"country"`
			DefaultCurrency string `json:"default_currency"`
			ChargesEnabled  bool   `json:"charges_enabled"`
			PayoutsEnabled  bool   `json:"payouts_enabled"`
			BusinessProfile struct {
				Name string `json:"name"`
			} `json:"business_profile"`
			Settings struct {
				Dashboard struct {
					DisplayName string `json:"display_name"`
				} `json:"dashboard"`
			} `json:"settings"`
		}
		if err := s.call(ctx, http.MethodGet, "/v1/account", nil, nil, "", &raw); err != nil {
			return nil, err
		}
		name := raw.BusinessProfile.Name
		if name == "" {
			name = raw.Settings.Dashboard.DisplayName
		}
		return &Account{ID: raw.ID, Name: name, Country: raw.Country, DefaultCurrency: raw.DefaultCurrency,
			ChargesEnabled: raw.ChargesEnabled, PayoutsEnabled: raw.PayoutsEnabled, Mode: s.Mode()}, nil
	})
}

// Amount is money in the currency's smallest unit.
type Amount struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Balance is what Stripe holds for the account.
type Balance struct {
	Available []Amount `json:"available"`
	Pending   []Amount `json:"pending"`
	Mode      Mode     `json:"mode"`
}

// Balance reads the balance. Cached for a minute.
func (s *Service) Balance(ctx context.Context) (*Balance, error) {
	return cached(s, "balance", func() (*Balance, error) {
		var raw struct {
			Available []Amount `json:"available"`
			Pending   []Amount `json:"pending"`
		}
		if err := s.call(ctx, http.MethodGet, "/v1/balance", nil, nil, "", &raw); err != nil {
			return nil, err
		}
		b := &Balance{Available: raw.Available, Pending: raw.Pending, Mode: s.Mode()}
		if b.Available == nil {
			b.Available = []Amount{}
		}
		if b.Pending == nil {
			b.Pending = []Amount{}
		}
		return b, nil
	})
}

// Payment is one payment intent.
type Payment struct {
	ID          string `json:"id"`
	Amount      int64  `json:"amount"`
	Received    int64  `json:"amount_received"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
	Created     int64  `json:"created"`
	Description string `json:"description,omitempty"`
}

// Payments is a page of recent payments, newest first.
type Payments struct {
	Items   []Payment `json:"items"`
	HasMore bool      `json:"has_more"`
	// Volume is the money received by succeeded payments in the window asked
	// for, per currency; it is empty when no window was asked for. It is a
	// lower bound when HasMore is true.
	Volume []Amount `json:"volume"`
	Mode   Mode     `json:"mode"`
}

// PaymentQuery selects payments. Days > 0 reads every payment of the last
// Days days (up to 500) and totals the succeeded ones; otherwise Limit
// payments (default 25, at most 100) are read.
type PaymentQuery struct {
	Limit int
	Days  int
}

// Payments reads recent payment intents. Cached for a minute.
func (s *Service) Payments(ctx context.Context, q PaymentQuery) (*Payments, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}
	limit = min(limit, pageSize)
	days := min(max(q.Days, 0), 90)
	return cached(s, fmt.Sprintf("payments|%d|%d", limit, days), func() (*Payments, error) {
		vals := url.Values{}
		count := limit
		if days > 0 {
			vals.Set("created[gte]", fmt.Sprint(s.now().Unix()-int64(days)*86400))
			count = 500
		}
		items, more, err := list[Payment](ctx, s, "/v1/payment_intents", vals, count)
		if err != nil {
			return nil, err
		}
		out := &Payments{Items: items, HasMore: more, Volume: []Amount{}, Mode: s.Mode()}
		if days > 0 {
			totals := map[string]int64{}
			var order []string
			for _, p := range items {
				if p.Status != "succeeded" {
					continue
				}
				if _, ok := totals[p.Currency]; !ok {
					order = append(order, p.Currency)
				}
				totals[p.Currency] += p.Received
			}
			for _, c := range order {
				out.Volume = append(out.Volume, Amount{Amount: totals[c], Currency: c})
			}
		}
		return out, nil
	})
}

// Price is one price of a product.
type Price struct {
	ID            string `json:"id"`
	Product       string `json:"product"`
	Amount        *int64 `json:"amount"`
	Currency      string `json:"currency"`
	Type          string `json:"type"`
	Interval      string `json:"interval,omitempty"`
	IntervalCount int    `json:"interval_count,omitempty"`
	Nickname      string `json:"nickname,omitempty"`
}

// Product is a product with its active prices.
type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Created     int64   `json:"created"`
	Prices      []Price `json:"prices"`
}

// Catalog is the active products and prices.
type Catalog struct {
	Products []Product `json:"products"`
	HasMore  bool      `json:"has_more"`
	Mode     Mode      `json:"mode"`
}

// Products reads the active products with their active prices. Cached for a minute.
func (s *Service) Products(ctx context.Context) (*Catalog, error) {
	return cached(s, "products", func() (*Catalog, error) {
		type rawProduct struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Created     int64  `json:"created"`
		}
		type rawPrice struct {
			ID         string `json:"id"`
			Product    string `json:"product"`
			UnitAmount *int64 `json:"unit_amount"`
			Currency   string `json:"currency"`
			Type       string `json:"type"`
			Nickname   string `json:"nickname"`
			Recurring  *struct {
				Interval      string `json:"interval"`
				IntervalCount int    `json:"interval_count"`
			} `json:"recurring"`
		}
		active := url.Values{"active": {"true"}}
		prods, more, err := list[rawProduct](ctx, s, "/v1/products", active, 100)
		if err != nil {
			return nil, err
		}
		prices, more2, err := list[rawPrice](ctx, s, "/v1/prices", active, 300)
		if err != nil {
			return nil, err
		}
		c := &Catalog{Products: []Product{}, HasMore: more || more2, Mode: s.Mode()}
		idx := map[string]int{}
		for _, p := range prods {
			idx[p.ID] = len(c.Products)
			c.Products = append(c.Products, Product{ID: p.ID, Name: p.Name, Description: p.Description, Created: p.Created, Prices: []Price{}})
		}
		for _, p := range prices {
			i, ok := idx[p.Product]
			if !ok {
				continue
			}
			pr := Price{ID: p.ID, Product: p.Product, Amount: p.UnitAmount, Currency: p.Currency, Type: p.Type, Nickname: p.Nickname}
			if p.Recurring != nil {
				pr.Interval, pr.IntervalCount = p.Recurring.Interval, p.Recurring.IntervalCount
			}
			c.Products[i].Prices = append(c.Products[i].Prices, pr)
		}
		return c, nil
	})
}

// PaymentLink is a hosted payment page.
type PaymentLink struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// PaymentLinks lists payment links. Cached for a minute.
func (s *Service) PaymentLinks(ctx context.Context) ([]PaymentLink, error) {
	return cached(s, "links", func() ([]PaymentLink, error) {
		l, _, err := list[PaymentLink](ctx, s, "/v1/payment_links", nil, 100)
		return l, err
	})
}

// Webhook is a webhook endpoint. Stripe only shows a signing secret when the
// endpoint is created, so none is read here.
type Webhook struct {
	ID            string   `json:"id"`
	URL           string   `json:"url"`
	Status        string   `json:"status"`
	EnabledEvents []string `json:"enabled_events"`
	Description   string   `json:"description,omitempty"`
}

// Webhooks lists webhook endpoints. Cached for a minute.
func (s *Service) Webhooks(ctx context.Context) ([]Webhook, error) {
	return cached(s, "webhooks", func() ([]Webhook, error) {
		l, _, err := list[Webhook](ctx, s, "/v1/webhook_endpoints", nil, 100)
		for i := range l {
			if l[i].EnabledEvents == nil {
				l[i].EnabledEvents = []string{}
			}
		}
		return l, err
	})
}

// ---- the one write ----

// Created is the part of a created object that callers need.
type Created struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
	// Secret is a webhook endpoint's signing secret. Stripe returns it once,
	// in the create answer; hand it to the user and drop it.
	Secret string `json:"secret,omitempty"`
}

// Create POSTs a form to one of the allow-listed collection paths with an
// Idempotency-Key, in test mode only, and drops the cached reads. The mode
// and path are checked before any request is built.
func (s *Service) Create(ctx context.Context, path string, form url.Values, idem string) (*Created, error) {
	if !writePaths[path] {
		return nil, ErrNotAllowed
	}
	if s.key() == "" {
		return nil, extapi.ErrNotConfigured
	}
	if s.Mode() != ModeTest {
		return nil, ErrLiveWrite
	}
	if idem == "" {
		return nil, ErrNoIdempotencyKey
	}
	if form == nil {
		form = url.Values{}
	}
	var c Created
	if err := s.call(ctx, http.MethodPost, path, nil, form, idem, &c); err != nil {
		return nil, err
	}
	s.Cache.Clear()
	return &c, nil
}
