package payments

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/stripe"
	"github.com/ChinmayGit8765/lucidbench/internal/stripe/stripetest"
)

// Key-shaped sentinels are built at runtime so no such literal is in source.
func join(parts ...string) string { return strings.Join(parts, "_") }

type rig struct {
	t     *testing.T
	svc   *Service
	fake  *stripetest.Fake
	h     http.Handler
	dir   string
	key   string
	whSec string
	logs  *bytes.Buffer
}

func newRig(t *testing.T, mode string) *rig {
	t.Helper()
	r := &rig{t: t, key: join("sk", mode, "KEYSENTINEL91827"), whSec: join("wh"+"sec", "WEBHOOKSENTINEL55"), logs: &bytes.Buffer{}}
	r.fake = stripetest.New()
	r.fake.WebhookSecret = r.whSec
	t.Cleanup(r.fake.Close)
	log.SetOutput(r.logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	st := stripe.New(configFor(r.fake.URL()))
	st.Getenv = func(k string) string {
		if k == "STRIPE_API_KEY" {
			return r.key
		}
		return ""
	}
	st.Doer.Sleep = func(time.Duration) {}
	r.dir = filepath.Join(t.TempDir(), "payments")
	r.svc = &Service{Stripe: st, Dir: r.dir, Projects: func() (*projects.List, error) {
		return &projects.List{Projects: []projects.Project{{ID: "demo-app"}, {ID: "other"}}}, nil
	}}
	mux := http.NewServeMux()
	Register(mux, r.svc)
	r.h = mux
	return r
}

func (r *rig) do(method, path string, body any, confirm bool) (int, string) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if confirm {
		req.Header.Set("X-Lucid-Confirm", "yes")
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

func form() PlanRequest {
	return PlanRequest{
		Project: "demo-app", ProductName: "  Pro   plan ", Description: "Everything",
		Prices: []PriceSpec{
			{Amount: 4900, Currency: "AUD", Interval: "month", Nickname: "Monthly"},
			{Amount: 49000, Currency: "aud", Interval: "month"},
		},
		SuccessURL: "https://example.test/thanks",
		WebhookURL: "https://example.test/hooks/stripe", WebhookEvents: []string{"checkout.session.completed", "invoice.paid"},
	}
}

func (r *rig) plan(req PlanRequest) Plan {
	r.t.Helper()
	code, body := r.do("POST", "/api/payments/plan", req, false)
	if code != 200 {
		r.t.Fatalf("plan %d %s", code, body)
	}
	var p Plan
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		r.t.Fatal(err)
	}
	return p
}

func TestPlanBuildsStepsAndWritesNothing(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	if !p.Writable || p.Mode != stripe.ModeTest || p.Refusal != "" || p.Project != "demo-app" {
		t.Fatalf("plan %+v", p)
	}
	var kinds, keys []string
	for _, s := range p.Steps {
		kinds = append(kinds, s.Kind)
		keys = append(keys, s.IdempotencyKey)
		if s.Path == "" || s.Title == "" || !strings.Contains(s.IdempotencyKey, p.ID) {
			t.Errorf("step %+v", s)
		}
	}
	if got := strings.Join(kinds, ","); got != "product,price,price,payment_link,webhook_endpoint" {
		t.Fatalf("steps %s", got)
	}
	if p.Steps[0].Title != `Create the product "Pro plan"` || p.Steps[1].Title != "Create a price of 49.00 AUD" {
		t.Fatalf("titles %q / %q", p.Steps[0].Title, p.Steps[1].Title)
	}
	if got := strings.Join(p.Steps[3].Needs, ","); got != "s2,s3" {
		t.Fatalf("link needs %s", got)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("duplicate idempotency key %s", k)
		}
		seen[k] = true
	}
	if n := r.fake.Count(""); n != 0 {
		t.Fatalf("building a plan called Stripe %d times", n)
	}
	if _, err := os.Stat(r.dir); err == nil {
		t.Fatal("building a plan wrote to the data dir")
	}
	// No webhook URL, no webhook step.
	f := form()
	f.WebhookURL, f.WebhookEvents = "", nil
	if p := r.plan(f); len(p.Steps) != 4 {
		t.Fatalf("%d steps without a webhook", len(p.Steps))
	}
}

func TestPlanValidation(t *testing.T) {
	r := newRig(t, "test")
	cases := map[string]func(*PlanRequest){
		"project":     func(f *PlanRequest) { f.Project = "../etc" },
		"unknown":     func(f *PlanRequest) { f.Project = "nope" },
		"name":        func(f *PlanRequest) { f.ProductName = "   " },
		"no prices":   func(f *PlanRequest) { f.Prices = nil },
		"zero amount": func(f *PlanRequest) { f.Prices[0].Amount = 0 },
		"currency":    func(f *PlanRequest) { f.Prices[0].Currency = "dollars" },
		"interval":    func(f *PlanRequest) { f.Prices[0].Interval = "fortnight" },
		"mixed":       func(f *PlanRequest) { f.Prices[0].Interval = "year" },
		"success":     func(f *PlanRequest) { f.SuccessURL = "javascript:alert(1)" },
		"no success":  func(f *PlanRequest) { f.SuccessURL = "" },
		"hook http":   func(f *PlanRequest) { f.WebhookURL = "http://example.test/h" },
		"hook event":  func(f *PlanRequest) { f.WebhookEvents = []string{"Not An Event"} },
		"userinfo":    func(f *PlanRequest) { f.SuccessURL = "https://user:pw@example.test/" },
	}
	for name, mut := range cases {
		f := form()
		mut(&f)
		if code, body := r.do("POST", "/api/payments/plan", f, false); code != 400 {
			t.Errorf("%s: %d %s", name, code, body)
		}
	}
	if code, _ := r.do("POST", "/api/payments/plan", map[string]any{"project": "demo-app", "surprise": 1}, false); code != 400 {
		t.Errorf("unknown field: %d", code)
	}
}

func TestExecuteCreatesObjectsInOrderWithIdempotencyKeys(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	code, body := r.do("POST", "/api/payments/plan/execute", p, true)
	if code != 200 {
		t.Fatalf("execute %d %s", code, body)
	}
	var res Result
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "complete" || len(res.Steps) != 5 || res.Mode != stripe.ModeTest || res.WebhookSecret != r.whSec {
		t.Fatalf("result %+v", res)
	}
	for _, s := range res.Steps {
		if s.Status != "created" || s.ObjectID == "" {
			t.Fatalf("step %+v", s)
		}
	}
	var posts []stripetest.Request
	for _, q := range r.fake.Requests() {
		if q.Method == "POST" {
			posts = append(posts, q)
		}
	}
	if len(posts) != 5 {
		t.Fatalf("%d POSTs", len(posts))
	}
	for i, q := range posts {
		if q.Idempotency != p.Steps[i].IdempotencyKey || q.Path != p.Steps[i].Path || q.Auth != "Bearer "+r.key {
			t.Errorf("POST %d: %+v", i, q)
		}
		if q.Form.Get("metadata[lucid_plan]") != p.ID {
			t.Errorf("POST %d has no plan metadata: %v", i, q.Form)
		}
	}
	if posts[0].Form.Get("name") != "Pro plan" || posts[0].Form.Get("description") != "Everything" {
		t.Errorf("product form %v", posts[0].Form)
	}
	if f := posts[1].Form; f.Get("product") != res.Steps[0].ObjectID || f.Get("unit_amount") != "4900" || f.Get("currency") != "aud" || f.Get("recurring[interval]") != "month" || f.Get("nickname") != "Monthly" {
		t.Errorf("price form %v", f)
	}
	if f := posts[3].Form; f.Get("line_items[0][price]") != res.Steps[1].ObjectID || f.Get("line_items[1][price]") != res.Steps[2].ObjectID ||
		f.Get("after_completion[redirect][url]") != "https://example.test/thanks" || f.Get("line_items[1][quantity]") != "1" {
		t.Errorf("link form %v", f)
	}
	if f := posts[4].Form; f.Get("url") != "https://example.test/hooks/stripe" || strings.Join(f["enabled_events[]"], ",") != "checkout.session.completed,invoice.paid" {
		t.Errorf("webhook form %v", f)
	}

	// Linked to the project: ids only.
	var linked Linked
	code, body = r.do("GET", "/api/payments/project/demo-app", nil, false)
	if code != 200 || json.Unmarshal([]byte(body), &linked) != nil || len(linked.Runs) != 1 {
		t.Fatalf("project %d %s", code, body)
	}
	run := linked.Runs[0]
	if run.Product != res.Steps[0].ObjectID || len(run.Prices) != 2 || run.PaymentLink != res.Steps[3].ObjectID || run.Webhook != res.Steps[4].ObjectID || run.PaymentLinkURL == "" || run.Plan != p.ID {
		t.Fatalf("run %+v", run)
	}
	// The audit log has one line per write.
	code, body = r.do("GET", "/api/payments/audit", nil, false)
	var au struct{ Entries []Audit }
	if code != 200 || json.Unmarshal([]byte(body), &au) != nil || len(au.Entries) != 5 {
		t.Fatalf("audit %d %s", code, body)
	}
	if e := au.Entries[0]; e.Action != "webhook_endpoint.create" || e.Mode != stripe.ModeTest || !e.OK || len(e.IDs) != 1 || e.Plan != p.ID || e.Time == "" {
		t.Fatalf("newest audit entry %+v", e)
	}
}

func TestSecretsNeverLeave(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	code, body := r.do("POST", "/api/payments/plan/execute", p, true)
	if code != 200 || strings.Count(body, r.whSec) != 1 {
		t.Fatalf("the webhook secret must be in the execute answer exactly once: %d", code)
	}
	var all []string
	for _, path := range []string{"status", "account", "balance", "charges?days=7", "products", "links", "webhooks", "audit", "project/demo-app"} {
		c, b := r.do("GET", "/api/payments/"+path, nil, false)
		if c != 200 {
			t.Fatalf("%s: %d %s", path, c, b)
		}
		all = append(all, b)
	}
	// What is on disk.
	err := filepath.WalkDir(r.dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(path)
			all = append(all, string(b))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// A failing call with a hostile error body.
	r.fake.Fail["GET /v1/balance"] = stripetest.Failure{Status: 401, Code: "api_key_expired", Message: "bad key"}
	r.svc.Stripe.Cache.Clear()
	c, b := r.do("GET", "/api/payments/balance", nil, false)
	if c != 401 {
		t.Fatalf("expired key answered %d", c)
	}
	all = append(all, b, r.logs.String())
	joined := strings.Join(all, "\n")
	for name, secret := range map[string]string{"key": r.key, "webhook secret": r.whSec, "key tail": "KEYSENTINEL"} {
		if strings.Contains(joined, secret) {
			t.Errorf("the %s leaked into a response, a file or a log", name)
		}
	}
	// And the wording of the error body never passed through.
	if strings.Contains(b, "bad key") {
		t.Errorf("Stripe's message was passed on: %s", b)
	}
}

func TestExecuteNeedsConfirmAndAMatchingPlan(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	if code, _ := r.do("POST", "/api/payments/plan/execute", p, false); code != 403 {
		t.Fatalf("without confirm: %d", code)
	}
	tampered := p
	tampered.Request.Prices = append([]PriceSpec(nil), p.Request.Prices...)
	tampered.Request.Prices[0].Amount = 1
	if code, _ := r.do("POST", "/api/payments/plan/execute", tampered, true); code != 409 {
		t.Fatalf("tampered plan: %d", code)
	}
	badID := p
	badID.ID = "../../x"
	if code, _ := r.do("POST", "/api/payments/plan/execute", badID, true); code != 409 {
		t.Fatalf("bad plan id: %d", code)
	}
	other := p
	other.Request.Project = "nope"
	if code, _ := r.do("POST", "/api/payments/plan/execute", other, true); code != 400 {
		t.Fatalf("unknown project: %d", code)
	}
	if n := r.fake.Count(""); n != 0 {
		t.Fatalf("%d calls were made for refused executes", n)
	}
}

func TestLiveModeRefusesEveryWrite(t *testing.T) {
	for _, mode := range []string{"live"} {
		r := newRig(t, mode)
		p := r.plan(form())
		if p.Writable || p.Mode != stripe.ModeLive || !strings.Contains(p.Refusal, "live mode writes are not supported") {
			t.Fatalf("live plan %+v", p)
		}
		code, body := r.do("POST", "/api/payments/plan/execute", p, true)
		if code != 403 || !strings.Contains(body, "live mode writes are not supported in this version") {
			t.Fatalf("execute %d %s", code, body)
		}
		if n := r.fake.Count(""); n != 0 {
			t.Fatalf("a refused live write still sent %d requests", n)
		}
		if _, err := os.Stat(r.dir); err == nil {
			t.Fatal("a refused write left files behind")
		}
		// Reads still work in live mode.
		if c, _ := r.do("GET", "/api/payments/balance", nil, false); c != 200 {
			t.Fatalf("live read %d", c)
		}
	}
	// A key of an unknown shape counts as live.
	r := newRig(t, "test")
	r.key = "something-else"
	p := r.plan(form())
	if code, _ := r.do("POST", "/api/payments/plan/execute", p, true); code != 403 || r.fake.Count("") != 0 {
		t.Fatalf("unknown key: %d, %d calls", code, r.fake.Count(""))
	}
	// No key at all is "not set up", not a refusal.
	r.key = ""
	if code, _ := r.do("POST", "/api/payments/plan/execute", p, true); code != 412 {
		t.Fatalf("no key: %d", code)
	}
}

func TestPartialFailureIsReportedAndRetryContinues(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	r.fake.Fail["POST /v1/payment_links"] = stripetest.Failure{Status: 400, Code: "url_invalid", Message: "A secret message"}
	code, body := r.do("POST", "/api/payments/plan/execute", p, true)
	if code != 200 {
		t.Fatalf("%d %s", code, body)
	}
	var res Result
	_ = json.Unmarshal([]byte(body), &res)
	var st []string
	for _, s := range res.Steps {
		st = append(st, s.Status)
	}
	if res.Status != "partial" || strings.Join(st, ",") != "created,created,created,failed,skipped" {
		t.Fatalf("result %s %v", res.Status, st)
	}
	if e := res.Steps[3].Error; !strings.Contains(e, "url_invalid") || strings.Contains(e, "secret message") {
		t.Fatalf("step error %q", e)
	}
	if res.WebhookSecret != "" {
		t.Fatal("a secret came from a step that never ran")
	}
	// What did get created is linked and audited, and the failure is audited.
	_, b := r.do("GET", "/api/payments/project/demo-app", nil, false)
	var l Linked
	_ = json.Unmarshal([]byte(b), &l)
	if len(l.Runs) != 1 || l.Runs[0].Product != res.Steps[0].ObjectID || l.Runs[0].PaymentLink != "" {
		t.Fatalf("linked %+v", l)
	}
	_, b = r.do("GET", "/api/payments/audit", nil, false)
	var au struct{ Entries []Audit }
	_ = json.Unmarshal([]byte(b), &au)
	if len(au.Entries) != 4 || au.Entries[0].OK || !strings.Contains(au.Entries[0].Error, "url_invalid") {
		t.Fatalf("audit %+v", au.Entries)
	}

	// Fix the cause and run the same plan again: the finished steps replay,
	// the rest are created, and nothing is duplicated.
	delete(r.fake.Fail, "POST /v1/payment_links")
	code, body = r.do("POST", "/api/payments/plan/execute", p, true)
	var res2 Result
	_ = json.Unmarshal([]byte(body), &res2)
	if code != 200 || res2.Status != "complete" {
		t.Fatalf("retry %d %+v", code, res2)
	}
	for i := 0; i < 3; i++ {
		if res2.Steps[i].ObjectID != res.Steps[i].ObjectID {
			t.Errorf("step %d was created again: %s vs %s", i, res2.Steps[i].ObjectID, res.Steps[i].ObjectID)
		}
	}
	_, b = r.do("GET", "/api/payments/project/demo-app", nil, false)
	l = Linked{}
	_ = json.Unmarshal([]byte(b), &l)
	if len(l.Runs) != 1 || l.Runs[0].PaymentLink == "" || l.Runs[0].Webhook == "" {
		t.Fatalf("linked after retry %+v", l)
	}
}

func TestFirstStepFailureCreatesNothing(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	r.fake.Fail["POST /v1/products"] = stripetest.Failure{Status: 403, Code: "permission_error", Message: "restricted key"}
	_, body := r.do("POST", "/api/payments/plan/execute", p, true)
	var res Result
	_ = json.Unmarshal([]byte(body), &res)
	if res.Status != "failed" || res.Steps[0].Status != "failed" || res.Steps[1].Status != "skipped" {
		t.Fatalf("result %+v", res)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "demo-app.yaml")); err == nil {
		t.Fatal("a project link was written for a plan that created nothing")
	}
}

func TestAuditLinesCarryNoSecret(t *testing.T) {
	r := newRig(t, "test")
	p := r.plan(form())
	r.do("POST", "/api/payments/plan/execute", p, true)
	b, err := os.ReadFile(filepath.Join(r.dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d audit lines", len(lines))
	}
	for _, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Fatal(err)
		}
		for k := range m {
			switch k {
			case "time", "mode", "action", "plan", "project", "ids", "ok", "error":
			default:
				t.Errorf("unexpected audit field %q", k)
			}
		}
		if strings.Contains(ln, r.key) || strings.Contains(ln, r.whSec) || strings.Contains(strings.ToLower(ln), "secret") {
			t.Errorf("audit line carries a secret: %s", ln)
		}
	}
}

func TestProjectIDNeverBecomesAPath(t *testing.T) {
	r := newRig(t, "test")
	for _, id := range []string{"..", "a%2Fb", "x.yaml", "A"} {
		if c, _ := r.do("GET", "/api/payments/project/"+id, nil, false); c == 200 {
			t.Errorf("%q answered 200", id)
		}
	}
}

func TestRestrictedKeyPermissionErrorIsNotCalledAnInvalidToken(t *testing.T) {
	r := newRig(t, "test")
	r.fake.Fail["GET /v1/balance"] = stripetest.Failure{Status: 403, Code: "permission_error", Message: "no"}
	c, b := r.do("GET", "/api/payments/balance", nil, false)
	if c != 403 || !strings.Contains(b, "permission_error") || strings.Contains(b, "token invalid") {
		t.Fatalf("%d %s", c, b)
	}
	// A rejected key with no code keeps the generic answer.
	r.fake.Fail["GET /v1/balance"] = stripetest.Failure{Status: 401, Message: "no"}
	r.svc.Stripe.Cache.Clear()
	if c, b := r.do("GET", "/api/payments/balance", nil, false); c != 401 || !strings.Contains(b, "token invalid") {
		t.Fatalf("%d %s", c, b)
	}
}

func TestFormatAmount(t *testing.T) {
	for in, want := range map[[2]any]string{{int64(4900), "aud"}: "49.00 AUD", {int64(5), "usd"}: "0.05 USD", {int64(500), "jpy"}: "500 JPY"} {
		if got := FormatAmount(in[0].(int64), in[1].(string)); got != want {
			t.Errorf("%v = %q, want %q", in, got, want)
		}
	}
}
