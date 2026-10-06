package payments

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
	"github.com/ChinmayGit8765/lucidbench/internal/stripe"
)

// Service builds and runs plans. Dir is <data dir>/payments.
type Service struct {
	Stripe *stripe.Service
	Dir    string
	// Projects lists the user's projects; a plan is for one of them.
	Projects func() (*projects.List, error)
	// Now is the clock; nil uses time.Now.
	Now func() time.Time

	mu sync.Mutex
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Errors the HTTP layer maps to statuses.
var (
	// ErrUnknownProject means the project is not in projects.yaml.
	ErrUnknownProject = errors.New("project: not one of your projects")
	// ErrPlanChanged means the plan to run is not the one that was reviewed.
	ErrPlanChanged = errors.New("the plan does not match what was reviewed; build it again")
)

func (s *Service) checkProject(id string) error {
	if !projectRE.MatchString(id) {
		return ValidationError("project: choose one of your projects")
	}
	if s.Projects == nil {
		return ErrUnknownProject
	}
	l, err := s.Projects()
	if err != nil {
		return ErrUnknownProject
	}
	for _, p := range l.Projects {
		if p.ID == id {
			return nil
		}
	}
	return ErrUnknownProject
}

// Plan validates the form and builds the plan. It writes nothing and does not
// call Stripe.
func (s *Service) Plan(r PlanRequest) (*Plan, error) {
	r, err := Normalize(r)
	if err != nil {
		return nil, err
	}
	if err := s.checkProject(r.Project); err != nil {
		return nil, err
	}
	return planFor(newID(), r, s.Stripe.Mode()), nil
}

// StepResult is what one step did.
type StepResult struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	// Status is created, failed or skipped (an earlier step failed).
	Status   string `json:"status"`
	ObjectID string `json:"object_id,omitempty"`
	URL      string `json:"url,omitempty"`
	Error    string `json:"error,omitempty"`
}

// Result is the outcome of running a plan.
type Result struct {
	PlanID  string      `json:"plan_id"`
	Project string      `json:"project"`
	Mode    stripe.Mode `json:"mode"`
	// Status is complete, partial (some objects exist) or failed (none).
	Status string       `json:"status"`
	Steps  []StepResult `json:"steps"`
	// WebhookSecret is the new endpoint's signing secret. It is shown here
	// once: it is not saved, logged or audited, and a later read has no way
	// to return it.
	WebhookSecret string `json:"webhook_secret,omitempty"`
	// Note reports a problem that is not a Stripe failure, such as the link
	// file not being saved.
	Note string `json:"note,omitempty"`
}

// Execute runs a reviewed plan in test mode. The caller has already refused
// live mode; this checks again before the first call.
func (s *Service) Execute(ctx context.Context, in Plan) (*Result, error) {
	if m := s.Stripe.Mode(); m != stripe.ModeTest {
		if !s.Stripe.Status().Configured {
			return nil, extapi.ErrNotConfigured
		}
		return nil, stripe.ErrLiveWrite
	}
	if !planIDRE.MatchString(in.ID) {
		return nil, ErrPlanChanged
	}
	r, err := Normalize(in.Request)
	if err != nil {
		return nil, err
	}
	if err := s.checkProject(r.Project); err != nil {
		return nil, err
	}
	plan := planFor(in.ID, r, stripe.ModeTest)
	if plan.Digest != in.Digest {
		return nil, ErrPlanChanged
	}

	res := &Result{PlanID: plan.ID, Project: plan.Project, Mode: stripe.ModeTest, Steps: []StepResult{}}
	ids := map[string]string{} // step id -> created object id
	failed := false
	for _, st := range plan.Steps {
		sr := StepResult{ID: st.ID, Kind: st.Kind, Title: st.Title, Status: "skipped"}
		if failed {
			res.Steps = append(res.Steps, sr)
			continue
		}
		form := s.form(plan, st, ids)
		c, err := s.Stripe.Create(ctx, st.Path, form, st.IdempotencyKey)
		if err != nil {
			sr.Status, sr.Error = "failed", errText(err)
			failed = true
			s.audit(Audit{Mode: stripe.ModeTest, Action: st.Kind + ".create", Plan: plan.ID, Project: plan.Project, OK: false, Error: sr.Error})
			res.Steps = append(res.Steps, sr)
			continue
		}
		sr.Status, sr.ObjectID, sr.URL = "created", c.ID, c.URL
		ids[st.ID] = c.ID
		if st.Kind == KindWebhook {
			res.WebhookSecret = c.Secret
		}
		s.audit(Audit{Mode: stripe.ModeTest, Action: st.Kind + ".create", Plan: plan.ID, Project: plan.Project, IDs: []string{c.ID}, OK: true})
		res.Steps = append(res.Steps, sr)
	}
	created := 0
	for _, sr := range res.Steps {
		if sr.Status == "created" {
			created++
		}
	}
	switch {
	case created == len(res.Steps):
		res.Status = "complete"
	case created == 0:
		res.Status = "failed"
	default:
		res.Status = "partial"
	}
	if created > 0 {
		if err := s.link(plan, res); err != nil {
			res.Note = "The objects were created, but linking them to the project could not be saved."
		}
	}
	return res, nil
}

// form builds the request body of a step; earlier ids come from ids.
func (s *Service) form(p *Plan, st Step, ids map[string]string) url.Values {
	r := p.Request
	f := url.Values{}
	f.Set("metadata[lucid_plan]", p.ID)
	switch st.Kind {
	case KindProduct:
		f.Set("name", r.ProductName)
		if r.Description != "" {
			f.Set("description", r.Description)
		}
	case KindPrice:
		var i int
		_, _ = fmt.Sscanf(st.ID, "s%d", &i)
		spec := r.Prices[i-2]
		f.Set("product", ids["s1"])
		f.Set("unit_amount", fmt.Sprint(spec.Amount))
		f.Set("currency", spec.Currency)
		if spec.Interval != "" {
			f.Set("recurring[interval]", spec.Interval)
		}
		if spec.Nickname != "" {
			f.Set("nickname", spec.Nickname)
		}
	case KindPaymentLink:
		for i, need := range st.Needs {
			f.Set(fmt.Sprintf("line_items[%d][price]", i), ids[need])
			f.Set(fmt.Sprintf("line_items[%d][quantity]", i), "1")
		}
		f.Set("after_completion[type]", "redirect")
		f.Set("after_completion[redirect][url]", r.SuccessURL)
	case KindWebhook:
		f.Set("url", r.WebhookURL)
		for _, e := range r.WebhookEvents {
			f.Add("enabled_events[]", e)
		}
		f.Set("description", "Lucidbench setup for "+p.Project)
	}
	return f
}

// errText reduces an error to fixed words plus Stripe's plain error code.
func errText(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "the request timed out"
	case errors.Is(err, stripe.ErrLiveWrite), errors.Is(err, stripe.ErrNotAllowed), errors.Is(err, stripe.ErrNoIdempotencyKey),
		errors.Is(err, extapi.ErrNotConfigured), errors.Is(err, extapi.ErrUnauthorized), errors.Is(err, extapi.ErrRateLimited),
		errors.Is(err, extapi.ErrNotFound), errors.Is(err, extapi.ErrBadRequest), errors.Is(err, extapi.ErrUnreachable), errors.Is(err, extapi.ErrRemote):
		return err.Error()
	}
	return "the call failed"
}

// ---- audit ----

// Audit is one line of audit.jsonl: a write, with no secret in it.
type Audit struct {
	Time    string      `json:"time"`
	Mode    stripe.Mode `json:"mode"`
	Action  string      `json:"action"`
	Plan    string      `json:"plan,omitempty"`
	Project string      `json:"project,omitempty"`
	IDs     []string    `json:"ids"`
	OK      bool        `json:"ok"`
	Error   string      `json:"error,omitempty"`
}

const auditFile = "audit.jsonl"

func (s *Service) audit(a Audit) {
	a.Time = s.now().UTC().Format(time.RFC3339)
	if a.IDs == nil {
		a.IDs = []string{}
	}
	b, err := json.Marshal(a)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if os.MkdirAll(s.Dir, 0o700) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, auditFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

// maxAuditRead bounds how much of the log one read looks at.
const maxAuditRead = 2 << 20

// AuditLog returns up to limit of the newest audit lines, newest first.
func (s *Service) AuditLog(limit int) ([]Audit, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(filepath.Join(s.Dir, auditFile))
	if errors.Is(err, os.ErrNotExist) {
		return []Audit{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > maxAuditRead {
		if _, err := f.Seek(st.Size()-maxAuditRead, 0); err != nil {
			return nil, err
		}
	}
	var all []Audit
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var a Audit
		if json.Unmarshal(bytes.TrimSpace(sc.Bytes()), &a) == nil && a.Action != "" {
			all = append(all, a)
		}
	}
	slices.Reverse(all)
	if len(all) > limit {
		all = all[:limit]
	}
	if all == nil {
		all = []Audit{}
	}
	return all, nil
}

// ---- links to the project ----

// Run is one executed plan's objects, as saved beside the project.
type Run struct {
	Plan           string   `yaml:"plan" json:"plan"`
	Mode           string   `yaml:"mode" json:"mode"`
	Created        string   `yaml:"created" json:"created"`
	Product        string   `yaml:"product,omitempty" json:"product,omitempty"`
	Prices         []string `yaml:"prices,omitempty" json:"prices"`
	PaymentLink    string   `yaml:"payment_link,omitempty" json:"payment_link,omitempty"`
	PaymentLinkURL string   `yaml:"payment_link_url,omitempty" json:"payment_link_url,omitempty"`
	Webhook        string   `yaml:"webhook_endpoint,omitempty" json:"webhook_endpoint,omitempty"`
}

// Linked is <data dir>/payments/<project>.yaml.
type Linked struct {
	Version int    `yaml:"version" json:"version"`
	Project string `yaml:"project" json:"project"`
	Runs    []Run  `yaml:"runs" json:"runs"`
}

func (s *Service) linkPath(project string) (string, error) {
	if !projectRE.MatchString(project) || len(project) > 64 {
		return "", ValidationError("project: choose one of your projects")
	}
	return filepath.Join(s.Dir, project+".yaml"), nil
}

// Linked reads what the project has been set up with.
func (s *Service) Linked(project string) (*Linked, error) {
	p, err := s.linkPath(project)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return readLinked(p, project)
}

func readLinked(path, project string) (*Linked, error) {
	l := &Linked{Version: 1, Project: project, Runs: []Run{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, l); err != nil {
		return nil, err
	}
	if l.Runs == nil {
		l.Runs = []Run{}
	}
	return l, nil
}

// link saves the created ids. A run with the same plan id (a retry) is
// replaced rather than listed twice. Only ids are written.
func (s *Service) link(p *Plan, res *Result) error {
	path, err := s.linkPath(p.Project)
	if err != nil {
		return err
	}
	run := Run{Plan: p.ID, Mode: string(stripe.ModeTest), Created: s.now().UTC().Format(time.RFC3339), Prices: []string{}}
	for _, sr := range res.Steps {
		if sr.Status != "created" {
			continue
		}
		switch sr.Kind {
		case KindProduct:
			run.Product = sr.ObjectID
		case KindPrice:
			run.Prices = append(run.Prices, sr.ObjectID)
		case KindPaymentLink:
			run.PaymentLink, run.PaymentLinkURL = sr.ObjectID, sr.URL
		case KindWebhook:
			run.Webhook = sr.ObjectID
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l, err := readLinked(path, p.Project)
	if err != nil {
		return err
	}
	l.Runs = slices.DeleteFunc(l.Runs, func(r Run) bool { return r.Plan == p.ID })
	l.Runs = append(l.Runs, run)
	b, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".link-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}
