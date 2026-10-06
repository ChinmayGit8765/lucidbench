// Package payments sets up Stripe payments for a project: it turns a short
// form into a reviewable plan (the list of objects to create), runs the plan
// in test mode only, records every write in an audit log, and links the
// created ids to the project.
//
// Nothing here moves money, and nothing here stores a secret. A webhook
// endpoint's signing secret is returned once in the execute answer and is not
// written to the audit log, the project file or any log.
package payments

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ChinmayGit8765/lucidbench/internal/stripe"
)

// PriceSpec is one price of the product. Amount is in the currency's smallest
// unit (cents). An empty Interval is a one-off price.
type PriceSpec struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Interval string `json:"interval,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}

// PlanRequest is the setup form.
type PlanRequest struct {
	Project       string      `json:"project"`
	ProductName   string      `json:"product_name"`
	Description   string      `json:"description,omitempty"`
	Prices        []PriceSpec `json:"prices"`
	SuccessURL    string      `json:"success_url"`
	WebhookURL    string      `json:"webhook_url,omitempty"`
	WebhookEvents []string    `json:"webhook_events,omitempty"`
}

// Step kinds.
const (
	KindProduct     = "product"
	KindPrice       = "price"
	KindPaymentLink = "payment_link"
	KindWebhook     = "webhook_endpoint"
)

// Step is one object the plan creates.
type Step struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Detail says what is sent, in words.
	Detail string `json:"detail"`
	// Path is the Stripe endpoint the step POSTs to.
	Path string `json:"path"`
	// IdempotencyKey is sent as Idempotency-Key, so running the same plan
	// again after a failure continues instead of creating duplicates.
	IdempotencyKey string `json:"idempotency_key"`
	// Needs lists the steps whose ids this one uses.
	Needs []string `json:"needs"`
}

// Plan is what a setup would create. Building it changes nothing.
type Plan struct {
	// ID names one run of the plan; every idempotency key contains it.
	ID string `json:"id"`
	// Digest fingerprints Request, so an execute can prove it is the plan
	// that was reviewed.
	Digest  string      `json:"digest"`
	Project string      `json:"project"`
	Mode    stripe.Mode `json:"mode"`
	// Writable is true when the plan can be executed: the key is a test key.
	Writable bool `json:"writable"`
	// Refusal says why it cannot be, when Writable is false.
	Refusal  string      `json:"refusal,omitempty"`
	Request  PlanRequest `json:"request"`
	Steps    []Step      `json:"steps"`
	Warnings []string    `json:"warnings"`
}

// ValidationError is a form problem the user can fix.
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

var (
	projectRE  = regexp.MustCompile(`^[a-z0-9-]+$`)
	currencyRE = regexp.MustCompile(`^[a-z]{3}$`)
	eventRE    = regexp.MustCompile(`^(\*|[a-z0-9_]+(\.[a-z0-9_*]+)*)$`)
	planIDRE   = regexp.MustCompile(`^[a-f0-9]{16}$`)
)

var intervals = []string{"day", "week", "month", "year"}

// Limits, kept well inside what Stripe accepts.
const (
	maxName      = 250
	maxDesc      = 1000
	maxNickname  = 80
	maxPrices    = 10
	maxURL       = 2000
	maxEvents    = 20
	maxAmount    = 99_999_999
	defaultEvent = "checkout.session.completed"
)

func okURL(s string, httpsOnly bool) error {
	if len(s) > maxURL {
		return errors.New("is too long")
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && (httpsOnly || u.Scheme != "http")) {
		if httpsOnly {
			return errors.New("must be an https URL")
		}
		return errors.New("must be an http or https URL")
	}
	return nil
}

// Normalize trims and lower-cases the form and checks it. It returns a
// ValidationError for anything the user has to change.
func Normalize(r PlanRequest) (PlanRequest, error) {
	r.Project = strings.TrimSpace(r.Project)
	r.ProductName = strings.Join(strings.Fields(r.ProductName), " ")
	r.Description = strings.TrimSpace(r.Description)
	r.SuccessURL = strings.TrimSpace(r.SuccessURL)
	r.WebhookURL = strings.TrimSpace(r.WebhookURL)
	bad := func(msg string) (PlanRequest, error) { return r, ValidationError(msg) }
	if !projectRE.MatchString(r.Project) || len(r.Project) > 64 {
		return bad("project: choose one of your projects")
	}
	if r.ProductName == "" {
		return bad("product name: required")
	}
	if utf8.RuneCountInString(r.ProductName) > maxName {
		return bad("product name: at most 250 characters")
	}
	if utf8.RuneCountInString(r.Description) > maxDesc {
		return bad("description: at most 1000 characters")
	}
	if len(r.Prices) == 0 {
		return bad("prices: add at least one price")
	}
	if len(r.Prices) > maxPrices {
		return bad("prices: at most 10")
	}
	recurring := map[string]bool{}
	for i := range r.Prices {
		p := &r.Prices[i]
		p.Currency = strings.ToLower(strings.TrimSpace(p.Currency))
		p.Interval = strings.ToLower(strings.TrimSpace(p.Interval))
		p.Nickname = strings.Join(strings.Fields(p.Nickname), " ")
		n := i + 1
		switch {
		case p.Amount < 1 || p.Amount > maxAmount:
			return bad(fmt.Sprintf("price %d: the amount must be above zero and below 1,000,000 in the main unit", n))
		case !currencyRE.MatchString(p.Currency):
			return bad(fmt.Sprintf("price %d: the currency must be a three-letter code such as usd", n))
		case p.Interval != "" && !slices.Contains(intervals, p.Interval):
			return bad(fmt.Sprintf("price %d: the interval must be empty (one-off), day, week, month or year", n))
		case utf8.RuneCountInString(p.Nickname) > maxNickname:
			return bad(fmt.Sprintf("price %d: the nickname is at most 80 characters", n))
		}
		if p.Interval != "" {
			recurring[p.Interval] = true
		}
	}
	if len(recurring) > 1 {
		return bad("prices: a payment link cannot mix recurring intervals; use one interval, or make a plan per interval")
	}
	if r.SuccessURL == "" {
		return bad("success URL: required, where buyers land after paying")
	}
	if err := okURL(r.SuccessURL, false); err != nil {
		return bad("success URL: " + err.Error())
	}
	if r.WebhookURL == "" {
		r.WebhookEvents = nil
		return r, nil
	}
	if err := okURL(r.WebhookURL, true); err != nil {
		return bad("webhook URL: " + err.Error())
	}
	var events []string
	for _, e := range r.WebhookEvents {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || slices.Contains(events, e) {
			continue
		}
		if !eventRE.MatchString(e) || len(e) > 100 {
			return bad("webhook events: names look like checkout.session.completed")
		}
		events = append(events, e)
	}
	if len(events) == 0 {
		events = []string{defaultEvent}
	}
	if len(events) > maxEvents {
		return bad("webhook events: at most 20")
	}
	r.WebhookEvents = events
	return r, nil
}

// zeroDecimal are the currencies whose smallest unit is the main unit.
var zeroDecimal = map[string]bool{
	"bif": true, "clp": true, "djf": true, "gnf": true, "jpy": true, "kmf": true, "krw": true, "mga": true,
	"pyg": true, "rwf": true, "ugx": true, "vnd": true, "vuv": true, "xaf": true, "xof": true, "xpf": true,
}

// FormatAmount writes an amount in the smallest unit as "49.00 AUD".
func FormatAmount(amount int64, currency string) string {
	c := strings.ToUpper(currency)
	if zeroDecimal[strings.ToLower(currency)] {
		return fmt.Sprintf("%d %s", amount, c)
	}
	return fmt.Sprintf("%d.%02d %s", amount/100, amount%100, c)
}

func digest(r PlanRequest) string {
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("payments: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// steps derives the steps of a normalized request. It is deterministic, so a
// plan can be rebuilt from its request at execute time.
func steps(id string, r PlanRequest) []Step {
	key := func(step string) string { return "lucid-" + id + "-" + step }
	out := []Step{{
		ID: "s1", Kind: KindProduct, Path: "/v1/products", IdempotencyKey: key("s1"), Needs: []string{},
		Title:  fmt.Sprintf("Create the product %q", r.ProductName),
		Detail: productDetail(r),
	}}
	var priceIDs []string
	for i, p := range r.Prices {
		sid := fmt.Sprintf("s%d", i+2)
		priceIDs = append(priceIDs, sid)
		what := "A one-off price"
		if p.Interval != "" {
			what = "A price charged every " + p.Interval
		}
		out = append(out, Step{
			ID: sid, Kind: KindPrice, Path: "/v1/prices", IdempotencyKey: key(sid), Needs: []string{"s1"},
			Title:  "Create a price of " + FormatAmount(p.Amount, p.Currency),
			Detail: what + " on the product.",
		})
	}
	next := len(r.Prices) + 2
	linkID := fmt.Sprintf("s%d", next)
	out = append(out, Step{
		ID: linkID, Kind: KindPaymentLink, Path: "/v1/payment_links", IdempotencyKey: key(linkID), Needs: priceIDs,
		Title:  fmt.Sprintf("Create a payment link for %d price%s", len(r.Prices), plural(len(r.Prices))),
		Detail: "A hosted page buyers pay on, then are sent to " + r.SuccessURL + ".",
	})
	if r.WebhookURL != "" {
		wid := fmt.Sprintf("s%d", next+1)
		out = append(out, Step{
			ID: wid, Kind: KindWebhook, Path: "/v1/webhook_endpoints", IdempotencyKey: key(wid), Needs: []string{},
			Title:  "Create a webhook endpoint at " + r.WebhookURL,
			Detail: "Stripe sends these events there: " + strings.Join(r.WebhookEvents, ", ") + ". Its signing secret is shown once, when it is created.",
		})
	}
	return out
}

func productDetail(r PlanRequest) string {
	if r.Description == "" {
		return "A product with no description."
	}
	return "A product described as " + fmt.Sprintf("%q", r.Description) + "."
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// refusal explains why a mode cannot execute, or "".
func refusal(m stripe.Mode) string {
	switch m {
	case stripe.ModeTest:
		return ""
	case stripe.ModeLive:
		return "live mode writes are not supported in this version"
	}
	return "no Stripe test key is set, so nothing can be created"
}

// BuildPlan builds the plan for a form. mode is the mode of the configured
// key. It writes nothing and calls nobody.
func BuildPlan(r PlanRequest, mode stripe.Mode) (*Plan, error) {
	r, err := Normalize(r)
	if err != nil {
		return nil, err
	}
	return planFor(newID(), r, mode), nil
}

func planFor(id string, r PlanRequest, mode stripe.Mode) *Plan {
	p := &Plan{ID: id, Digest: digest(r), Project: r.Project, Mode: mode, Writable: mode == stripe.ModeTest,
		Refusal: refusal(mode), Request: r, Steps: steps(id, r), Warnings: []string{}}
	hasRecurring, hasOneOff := false, false
	for _, pr := range r.Prices {
		if pr.Interval != "" {
			hasRecurring = true
		} else {
			hasOneOff = true
		}
	}
	if hasRecurring && hasOneOff {
		p.Warnings = append(p.Warnings, "This mixes one-off and recurring prices; Stripe decides when the link is created whether it accepts them together.")
	}
	if u, err := url.Parse(r.SuccessURL); err == nil && u.Scheme == "http" {
		p.Warnings = append(p.Warnings, "The success URL is not https; fine for testing, not for a live site.")
	}
	return p
}
