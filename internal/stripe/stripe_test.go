package stripe

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
	"github.com/ChinmayGit8765/lucidbench/internal/stripe/stripetest"
)

var seq atomic.Int64

func join(parts ...string) string { return strings.Join(parts, "_") }

// fakeKey builds a key-shaped sentinel at runtime, so no key-shaped literal
// is ever written in source.
func fakeKey(kind, mode string) string {
	return strings.Join([]string{kind, mode, "SENTINEL" + time.Now().Format("150405") + string(rune('a'+seq.Add(1)%26))}, "_")
}

func newSvc(t *testing.T, key string) (*Service, *stripetest.Fake, *[]time.Duration) {
	t.Helper()
	f := stripetest.New()
	t.Cleanup(f.Close)
	var slept []time.Duration
	s := New(configFor(f.URL()))
	s.Getenv = func(k string) string {
		if k == "STRIPE_API_KEY" {
			return key
		}
		return ""
	}
	s.Doer.Sleep = func(d time.Duration) { slept = append(slept, d) }
	return s, f, &slept
}

func TestModeOf(t *testing.T) {
	for key, want := range map[string]Mode{
		fakeKey("sk", "test"):               ModeTest,
		fakeKey("rk", "test"):               ModeTest,
		fakeKey("sk", "live"):               ModeLive,
		fakeKey("rk", "live"):               ModeLive,
		"":                                  ModeUnknown,
		"hello":                             ModeUnknown,
		join("pk", "test", "abc"):           ModeUnknown, // a publishable key is not a secret key
		join("sk", "test", ""):              ModeUnknown,
		join("sk", "prod", "abc"):           ModeUnknown,
		"  " + fakeKey("sk", "test") + "\n": ModeTest,
	} {
		if got := ModeOf(key); got != want {
			t.Errorf("ModeOf(%q) = %q, want %q", key[:min(len(key), 6)], got, want)
		}
	}
}

func TestStatusNeverHoldsTheKey(t *testing.T) {
	key := fakeKey("sk", "test")
	s, _, _ := newSvc(t, key)
	st := s.Status()
	if !st.Configured || st.Mode != ModeTest || !st.WritesAllowed || st.KeyRef != "env:STRIPE_API_KEY" {
		t.Fatalf("status %+v", st)
	}
	s.Getenv = func(string) string { return "" }
	if st := s.Status(); st.Configured || st.Mode != ModeUnknown || st.WritesAllowed {
		t.Fatalf("unset status %+v", st)
	}
	if _, err := s.Balance(context.Background()); !errors.Is(err, extapi.ErrNotConfigured) {
		t.Fatalf("unset balance err = %v", err)
	}
}

func TestReadsUseBearerAndFormShape(t *testing.T) {
	key := fakeKey("rk", "test")
	s, f, _ := newSvc(t, key)
	ctx := context.Background()
	a, err := s.Account(ctx)
	if err != nil || a.Name != "Fixture Studio" || a.Country != "AU" || a.Mode != ModeTest {
		t.Fatalf("account %+v %v", a, err)
	}
	b, err := s.Balance(ctx)
	if err != nil || len(b.Available) != 1 || b.Available[0].Amount != 128450 || b.Pending[0].Amount != 9900 {
		t.Fatalf("balance %+v %v", b, err)
	}
	cat, err := s.Products(ctx)
	if err != nil || len(cat.Products) != 1 || len(cat.Products[0].Prices) != 2 {
		t.Fatalf("catalog %+v %v", cat, err)
	}
	if p := cat.Products[0].Prices[1]; p.Interval != "month" || p.Amount == nil || *p.Amount != 4900 || p.Nickname != "Monthly" {
		t.Fatalf("price %+v", p)
	}
	if l, err := s.PaymentLinks(ctx); err != nil || len(l) != 1 || !l[0].Active {
		t.Fatalf("links %+v %v", l, err)
	}
	if w, err := s.Webhooks(ctx); err != nil || len(w) != 1 || w[0].EnabledEvents[0] != "checkout.session.completed" {
		t.Fatalf("webhooks %+v %v", w, err)
	}
	for _, r := range f.Requests() {
		if r.Auth != "Bearer "+key {
			t.Errorf("%s %s auth = %q", r.Method, r.Path, r.Auth)
		}
		if strings.Contains(r.Path, key) || strings.Contains(r.Query.Encode(), key) {
			t.Errorf("the key reached a URL: %s %s", r.Path, r.Query.Encode())
		}
		if r.Method != "GET" || r.Idempotency != "" {
			t.Errorf("a read sent %s with idempotency %q", r.Method, r.Idempotency)
		}
	}
}

func TestPaymentsPaginateAndTotal(t *testing.T) {
	s, f, _ := newSvc(t, fakeKey("sk", "test"))
	f.AddPayments(230, 1000, 1)
	ctx := context.Background()

	// Seven days: 230 bulk payments plus the demo ones inside the window,
	// read across three pages of at most 100.
	p, err := s.Payments(ctx, PaymentQuery{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	if p.HasMore || len(p.Items) != 230+5 {
		t.Fatalf("items %d more %v", len(p.Items), p.HasMore)
	}
	want := int64(230*1000 + 4900*2 + 12000)
	if len(p.Volume) != 1 || p.Volume[0].Currency != "aud" || p.Volume[0].Amount != want {
		t.Fatalf("volume %+v, want %d", p.Volume, want)
	}
	pages, cursors := 0, 0
	for _, r := range f.Requests() {
		if r.Path == "/v1/payment_intents" {
			pages++
			if r.Query.Get("starting_after") != "" {
				cursors++
			}
			if r.Query.Get("limit") == "" || r.Query.Get("created[gte]") == "" {
				t.Errorf("query %v", r.Query)
			}
		}
	}
	if pages != 3 || cursors != 2 {
		t.Fatalf("pages=%d cursors=%d", pages, cursors)
	}

	// A plain page stops at the limit and says there is more.
	q, err := s.Payments(ctx, PaymentQuery{Limit: 50})
	if err != nil || len(q.Items) != 50 || !q.HasMore || len(q.Volume) != 0 {
		t.Fatalf("limited %d more %v err %v", len(q.Items), q.HasMore, err)
	}
	if q.Items[0].Status == "" || q.Items[0].Currency != "aud" || q.Items[0].Created == 0 {
		t.Fatalf("item %+v", q.Items[0])
	}
}

func TestPaginationCannotLoopForever(t *testing.T) {
	s, f, _ := newSvc(t, fakeKey("sk", "test"))
	f.AddPayments(1200, 1, 1)
	p, err := s.Payments(context.Background(), PaymentQuery{Days: 30})
	if err != nil || len(p.Items) != 500 || !p.HasMore {
		t.Fatalf("items %d more %v err %v", len(p.Items), p.HasMore, err)
	}
	if n := f.Count("GET"); n != 5 {
		t.Fatalf("%d requests for a 500 item cap", n)
	}
}

func TestReadsAreCachedForAMinute(t *testing.T) {
	s, f, _ := newSvc(t, fakeKey("sk", "test"))
	now := time.Now()
	s.Cache.Now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.Balance(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.Count("GET"); n != 1 {
		t.Fatalf("%d calls for three reads in a minute", n)
	}
	now = now.Add(61 * time.Second)
	if _, err := s.Balance(ctx); err != nil || f.Count("GET") != 2 {
		t.Fatalf("not refreshed after a minute: %v %d", err, f.Count("GET"))
	}
}

func TestRateLimitBacksOffThenGivesUp(t *testing.T) {
	s, f, slept := newSvc(t, fakeKey("sk", "test"))
	f.RateLimit = 2
	if _, err := s.Balance(context.Background()); err != nil {
		t.Fatalf("after two 429s: %v", err)
	}
	if len(*slept) != 2 || f.Count("GET") != 3 {
		t.Fatalf("slept %v, %d requests", *slept, f.Count("GET"))
	}
	s2, f2, _ := newSvc(t, fakeKey("sk", "test"))
	f2.RateLimit = 10
	if _, err := s2.Balance(context.Background()); !errors.Is(err, extapi.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
}

func TestErrorsCarryACodeButNeverTheKeyOrTheMessage(t *testing.T) {
	key := fakeKey("sk", "test")
	s, f, _ := newSvc(t, key)
	f.Fail["GET /v1/balance"] = stripetest.Failure{Status: 403, Code: "secret_key_required", Message: "The key lacks a permission"}
	_, err := s.Balance(context.Background())
	if !errors.Is(err, extapi.ErrUnauthorized) || Code(err) != "secret_key_required" {
		t.Fatalf("err = %v code %q", err, Code(err))
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "lacks") {
		t.Fatalf("error leaks: %v", err)
	}
	// An unusual code is dropped rather than passed on.
	f.Fail["GET /v1/account"] = stripetest.Failure{Status: 400, Code: "Has Spaces And " + key}
	_, err = s.Account(context.Background())
	if !errors.Is(err, extapi.ErrBadRequest) || Code(err) != "" || strings.Contains(err.Error(), key) {
		t.Fatalf("err = %v code %q", err, Code(err))
	}
	// A 401 that echoes the key in its message.
	s.Getenv = func(string) string { return key }
	f.Fail["GET /v1/account"] = stripetest.Failure{Status: 401, Message: "Invalid API Key provided: " + key}
	s.Cache.Clear()
	_, err = s.Account(context.Background())
	if !errors.Is(err, extapi.ErrUnauthorized) || strings.Contains(err.Error(), key) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateSendsFormAndIdempotencyKey(t *testing.T) {
	s, f, _ := newSvc(t, fakeKey("sk", "test"))
	form := url.Values{"name": {"Pro plan"}}
	c, err := s.Create(context.Background(), "/v1/products", form, "idem-1")
	if err != nil || !strings.HasPrefix(c.ID, "prod_") {
		t.Fatalf("created %+v %v", c, err)
	}
	reqs := f.Requests()
	r := reqs[len(reqs)-1]
	if r.Method != "POST" || r.Idempotency != "idem-1" || r.Form.Get("name") != "Pro plan" {
		t.Fatalf("request %+v", r)
	}
	// The same key replays the same object instead of creating another.
	c2, err := s.Create(context.Background(), "/v1/products", form, "idem-1")
	if err != nil || c2.ID != c.ID {
		t.Fatalf("replay %+v %v", c2, err)
	}
	// A write drops the cached reads.
	if _, err := s.Products(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := f.Count("GET")
	if _, err := s.Create(context.Background(), "/v1/products", url.Values{"name": {"Other"}}, "idem-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Products(context.Background()); err != nil || f.Count("GET") == n {
		t.Fatalf("cache survived a write: %v", err)
	}
}

func TestLiveAndUnknownModesNeverWrite(t *testing.T) {
	for _, key := range []string{fakeKey("sk", "live"), fakeKey("rk", "live"), "not-a-stripe-key"} {
		s, f, _ := newSvc(t, key)
		_, err := s.Create(context.Background(), "/v1/products", url.Values{"name": {"x"}}, "idem")
		if !errors.Is(err, ErrLiveWrite) {
			t.Errorf("err = %v", err)
		}
		if n := f.Count(""); n != 0 {
			t.Errorf("a refused write still sent %d requests", n)
		}
		// Reads still work in live mode (except for the unknown key shape, which the fake accepts too).
		if _, err := s.Balance(context.Background()); err != nil {
			t.Errorf("read: %v", err)
		}
	}
}

func TestMoneyMovingCallsDoNotExist(t *testing.T) {
	s, f, _ := newSvc(t, fakeKey("sk", "test"))
	for _, p := range []string{"/v1/refunds", "/v1/payouts", "/v1/transfers", "/v1/charges", "/v1/payment_intents", "/v1/payment_intents/pi_1/capture", "/v1/products/prod_1", "/v1/products/", "v1/products", ""} {
		if _, err := s.Create(context.Background(), p, url.Values{"amount": {"1"}}, "idem"); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%q: err = %v", p, err)
		}
	}
	if _, err := s.Create(context.Background(), "/v1/products", url.Values{"name": {"x"}}, ""); !errors.Is(err, ErrNoIdempotencyKey) {
		t.Errorf("no idempotency key: %v", err)
	}
	if n := f.Count(""); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}
