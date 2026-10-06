// Package stripetest is a fake Stripe API for tests and for looking at the
// Payments page without a Stripe account. It speaks the real wire shape: form
// encoded writes, Bearer authentication, Idempotency-Key replays, lists
// paged with limit, starting_after and has_more, and error objects with a
// code and a message.
//
// It never contains a real key, and it keeps a log of every request so a test
// can assert what was (or was not) called.
package stripetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Request is one call the fake received.
type Request struct {
	Method, Path string
	Query, Form  url.Values
	Auth         string
	Idempotency  string
}

// Fake is the fake API. Set the exported fields before the first request.
type Fake struct {
	Server *httptest.Server

	// WebhookSecret is returned once by a webhook endpoint create.
	WebhookSecret string
	// RateLimit is how many requests (counted from the first) answer 429.
	RateLimit int
	// Fail maps "METHOD /path" to a status and error code: that call fails.
	Fail map[string]Failure
	// Livemode is what the account and balance report.
	Livemode bool

	mu      sync.Mutex
	reqs    []Request
	seq     int
	limited int
	replay  map[string]stored
	objs    map[string][]map[string]any // collection path -> newest first
	balance map[string]any
	account map[string]any
}

// Failure is an error answer.
type Failure struct {
	Status  int
	Code    string
	Message string
}

type stored struct {
	status int
	body   []byte
}

// New starts a fake with a small demo account: a few payments, one product
// with two prices, a payment link and a webhook endpoint.
func New() *Fake {
	f := &Fake{replay: map[string]stored{}, objs: map[string][]map[string]any{}, Fail: map[string]Failure{}}
	now := time.Now().Unix()
	f.account = map[string]any{
		"id": "acct_fake0001", "country": "AU", "default_currency": "aud", "charges_enabled": true, "payouts_enabled": true,
		"business_profile": map[string]any{"name": "Fixture Studio"},
	}
	f.balance = map[string]any{
		"available": []any{map[string]any{"amount": 128450, "currency": "aud"}},
		"pending":   []any{map[string]any{"amount": 9900, "currency": "aud"}},
	}
	for i, p := range []struct {
		amount int64
		status string
		desc   string
		ageH   int64
	}{
		{4900, "succeeded", "Pro plan, monthly", 3},
		{4900, "succeeded", "Pro plan, monthly", 29},
		{12000, "succeeded", "Workshop ticket", 53},
		{4900, "requires_payment_method", "Pro plan, monthly", 70},
		{2500, "canceled", "Sticker pack", 120},
		{4900, "succeeded", "Pro plan, monthly", 200},
	} {
		received := int64(0)
		if p.status == "succeeded" {
			received = p.amount
		}
		f.objs["/v1/payment_intents"] = append(f.objs["/v1/payment_intents"], map[string]any{
			"id": fmt.Sprintf("pi_fake%04d", i+1), "amount": p.amount, "amount_received": received, "currency": "aud",
			"status": p.status, "created": now - p.ageH*3600, "description": p.desc,
		})
	}
	f.objs["/v1/products"] = []map[string]any{{"id": "prod_fake0001", "name": "Pro plan", "description": "Everything, monthly", "active": true, "created": now - 86400*20}}
	f.objs["/v1/prices"] = []map[string]any{
		{"id": "price_fake0002", "product": "prod_fake0001", "unit_amount": 49000, "currency": "aud", "type": "recurring", "active": true, "recurring": map[string]any{"interval": "year", "interval_count": 1}},
		{"id": "price_fake0001", "product": "prod_fake0001", "unit_amount": 4900, "currency": "aud", "type": "recurring", "active": true, "nickname": "Monthly", "recurring": map[string]any{"interval": "month", "interval_count": 1}},
	}
	f.objs["/v1/payment_links"] = []map[string]any{{"id": "plink_fake0001", "url": "https://buy.example.test/test_fake0001", "active": true}}
	f.objs["/v1/webhook_endpoints"] = []map[string]any{{"id": "we_fake0001", "url": "https://example.test/hooks/stripe", "status": "enabled", "enabled_events": []any{"checkout.session.completed"}}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// URL is the base URL to give as integrations.stripe.api_url.
func (f *Fake) URL() string { return f.Server.URL }

// Close stops the fake.
func (f *Fake) Close() { f.Server.Close() }

// Requests returns a copy of the request log.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}

// Count returns how many requests had the method (empty = any).
func (f *Fake) Count(method string) int {
	n := 0
	for _, r := range f.Requests() {
		if method == "" || r.Method == method {
			n++
		}
	}
	return n
}

// AddPayments appends n succeeded payments of the given amount, newest first
// in the list, to exercise pagination. ageHours is the age of the first one.
func (f *Fake) AddPayments(n int, amount int64, ageHours int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().Unix()
	var add []map[string]any
	for i := 0; i < n; i++ {
		add = append(add, map[string]any{
			"id": fmt.Sprintf("pi_bulk%04d", i), "amount": amount, "amount_received": amount, "currency": "aud",
			"status": "succeeded", "created": now - ageHours*3600 - int64(i), "description": "bulk",
		})
	}
	f.objs["/v1/payment_intents"] = append(add, f.objs["/v1/payment_intents"]...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func apiError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"type": "invalid_request_error", "code": code, "message": msg}})
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	form := url.Values{}
	if r.Method == http.MethodPost {
		form = r.PostForm
	}
	f.mu.Lock()
	f.reqs = append(f.reqs, Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Form: form,
		Auth: r.Header.Get("Authorization"), Idempotency: r.Header.Get("Idempotency-Key")})
	f.mu.Unlock()

	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" || tok == r.Header.Get("Authorization") {
		// The real API echoes a redacted key; the fake echoes all of it, so a
		// test can prove the app never passes the message on.
		apiError(w, http.StatusUnauthorized, "api_key_missing", "No API key provided: "+tok)
		return
	}
	f.mu.Lock()
	if f.limited < f.RateLimit {
		f.limited++
		f.mu.Unlock()
		w.Header().Set("Retry-After", "0")
		apiError(w, http.StatusTooManyRequests, "rate_limit", "Too many requests with "+tok)
		return
	}
	f.mu.Unlock()
	if fl, ok := f.Fail[r.Method+" "+r.URL.Path]; ok {
		apiError(w, fl.Status, fl.Code, fl.Message+" "+tok)
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/account":
		a := map[string]any{}
		for k, v := range f.account {
			a[k] = v
		}
		a["livemode"] = f.Livemode
		writeJSON(w, http.StatusOK, a)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/balance":
		writeJSON(w, http.StatusOK, f.balance)
	case r.Method == http.MethodGet:
		f.list(w, r)
	case r.Method == http.MethodPost:
		f.create(w, r, tok)
	default:
		apiError(w, http.StatusMethodNotAllowed, "method_not_allowed", "no")
	}
}

func (f *Fake) list(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	all, ok := f.objs[r.URL.Path]
	f.mu.Unlock()
	if !ok {
		apiError(w, http.StatusNotFound, "resource_missing", "Unrecognized request URL")
		return
	}
	q := r.URL.Query()
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > 100 {
		limit = 10
	}
	var rows []map[string]any
	for _, o := range all {
		if q.Get("active") == "true" && o["active"] == false {
			continue
		}
		if g := q.Get("created[gte]"); g != "" {
			min, _ := strconv.ParseInt(g, 10, 64)
			if c, _ := o["created"].(int64); c != 0 && c < min {
				continue
			}
		}
		rows = append(rows, o)
	}
	if after := q.Get("starting_after"); after != "" {
		i := 0
		for j, o := range rows {
			if o["id"] == after {
				i = j + 1
				break
			}
		}
		if i > len(rows) {
			i = len(rows)
		}
		rows = rows[i:]
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	if rows == nil {
		rows = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": rows, "has_more": more, "url": r.URL.Path})
}

var prefixes = map[string]string{
	"/v1/products": "prod_", "/v1/prices": "price_", "/v1/payment_links": "plink_", "/v1/webhook_endpoints": "we_",
}

func (f *Fake) create(w http.ResponseWriter, r *http.Request, tok string) {
	prefix, ok := prefixes[r.URL.Path]
	if !ok {
		// Anything that is not a plain create (refunds, payouts, transfers)
		// would be a bug in the caller: make it loud.
		apiError(w, http.StatusBadRequest, "unexpected_call", "the fake does not implement "+r.URL.Path)
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		apiError(w, http.StatusBadRequest, "parameter_invalid_empty", "bodies are form encoded")
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	f.mu.Lock()
	if idem != "" {
		if s, ok := f.replay[r.URL.Path+"|"+idem]; ok {
			f.mu.Unlock()
			w.Header().Set("Idempotent-Replayed", "true")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s.status)
			_, _ = w.Write(s.body)
			return
		}
	}
	f.mu.Unlock()
	form := r.PostForm
	obj := map[string]any{"active": true, "created": time.Now().Unix(), "livemode": f.Livemode}
	need := func(k string) bool {
		if form.Get(k) == "" {
			apiError(w, http.StatusBadRequest, "parameter_missing", "Missing required param: "+k+" "+tok)
			return false
		}
		return true
	}
	switch r.URL.Path {
	case "/v1/products":
		if !need("name") {
			return
		}
		obj["name"], obj["description"] = form.Get("name"), form.Get("description")
	case "/v1/prices":
		if !need("product") || !need("currency") || !need("unit_amount") {
			return
		}
		amt, err := strconv.ParseInt(form.Get("unit_amount"), 10, 64)
		if err != nil || amt < 0 {
			apiError(w, http.StatusBadRequest, "parameter_invalid_integer", "Invalid integer: "+form.Get("unit_amount"))
			return
		}
		obj["product"], obj["unit_amount"], obj["currency"], obj["nickname"] = form.Get("product"), amt, form.Get("currency"), form.Get("nickname")
		obj["type"] = "one_time"
		if iv := form.Get("recurring[interval]"); iv != "" {
			obj["type"] = "recurring"
			obj["recurring"] = map[string]any{"interval": iv, "interval_count": 1}
		}
	case "/v1/payment_links":
		if !need("line_items[0][price]") {
			return
		}
		obj["url"] = "https://buy.example.test/test_" + strconv.Itoa(f.next())
	case "/v1/webhook_endpoints":
		if !need("url") || !need("enabled_events[]") {
			return
		}
		obj["url"], obj["status"] = form.Get("url"), "enabled"
		ev := make([]any, 0)
		for _, e := range form["enabled_events[]"] {
			ev = append(ev, e)
		}
		obj["enabled_events"] = ev
	}
	id := prefix + "fake" + fmt.Sprintf("%04d", f.next()+100)
	obj["id"] = id
	f.mu.Lock()
	f.objs[r.URL.Path] = append([]map[string]any{obj}, f.objs[r.URL.Path]...)
	f.mu.Unlock()
	resp := map[string]any{}
	for k, v := range obj {
		resp[k] = v
	}
	if r.URL.Path == "/v1/webhook_endpoints" && f.WebhookSecret != "" {
		resp["secret"] = f.WebhookSecret
	}
	body, _ := json.Marshal(resp)
	if idem != "" {
		f.mu.Lock()
		f.replay[r.URL.Path+"|"+idem] = stored{http.StatusOK, body}
		f.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (f *Fake) next() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return f.seq
}
