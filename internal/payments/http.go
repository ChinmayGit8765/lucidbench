package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/extapi"
	"github.com/ChinmayGit8765/lucidbench/internal/stripe"
)

const (
	serviceName = "Stripe"
	maxBody     = 32 << 10
	callTime    = 45 * time.Second
	execTime    = 90 * time.Second
)

// LiveRefusal is the answer to any write that is not in test mode.
const LiveRefusal = "live mode writes are not supported in this version"

func fail(w http.ResponseWriter, err error) {
	var v ValidationError
	switch {
	case errors.As(err, &v):
		http.Error(w, v.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrUnknownProject):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrPlanChanged):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, stripe.ErrLiveWrite):
		http.Error(w, LiveRefusal, http.StatusForbidden)
	case stripe.Code(err) != "" && stripe.StatusOf(err) == http.StatusForbidden:
		// A restricted key without a permission is answered 403 with a code;
		// say so instead of "token invalid, create a new one".
		http.Error(w, "Stripe refused the request ("+stripe.Code(err)+"); a restricted key may be missing a permission", http.StatusForbidden)
	default:
		extapi.Fail(w, serviceName, err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

// Register adds the routes to mux:
//
//	GET  /api/payments/status          whether a key is set, and its mode
//	GET  /api/payments/account         the account (name, country, mode)
//	GET  /api/payments/balance         available and pending money
//	GET  /api/payments/charges         recent payments (?limit=, ?days= adds the volume)
//	GET  /api/payments/products        products with their prices
//	GET  /api/payments/links           payment links
//	GET  /api/payments/webhooks        webhook endpoints
//	POST /api/payments/plan            build a setup plan; creates nothing
//	POST /api/payments/plan/execute    run a reviewed plan, in test mode only
//	GET  /api/payments/audit           the newest writes
//	GET  /api/payments/project/{id}    what a project has been set up with
//
// The execute route needs X-Lucid-Confirm and answers 403 for a live key
// before it sends anything to Stripe. Reads are cached for a minute.
func Register(mux *http.ServeMux, s *Service) {
	ctx := func(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), d)
	}
	read := func(path string, get func(context.Context, *http.Request) (any, error)) {
		mux.HandleFunc("GET /api/payments/"+path, func(w http.ResponseWriter, r *http.Request) {
			c, cancel := ctx(r, callTime)
			defer cancel()
			v, err := get(c, r)
			if err != nil {
				fail(w, err)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, v)
		})
	}
	mux.HandleFunc("GET /api/payments/status", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.Stripe.Status())
	})
	read("account", func(c context.Context, _ *http.Request) (any, error) { return s.Stripe.Account(c) })
	read("balance", func(c context.Context, _ *http.Request) (any, error) { return s.Stripe.Balance(c) })
	read("charges", func(c context.Context, r *http.Request) (any, error) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		return s.Stripe.Payments(c, stripe.PaymentQuery{Limit: limit, Days: days})
	})
	read("products", func(c context.Context, _ *http.Request) (any, error) { return s.Stripe.Products(c) })
	read("links", func(c context.Context, _ *http.Request) (any, error) {
		l, err := s.Stripe.PaymentLinks(c)
		return map[string]any{"links": l, "mode": s.Stripe.Mode()}, err
	})
	read("webhooks", func(c context.Context, _ *http.Request) (any, error) {
		l, err := s.Stripe.Webhooks(c)
		return map[string]any{"webhooks": l, "mode": s.Stripe.Mode()}, err
	})
	read("audit", func(_ context.Context, r *http.Request) (any, error) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		l, err := s.AuditLog(limit)
		return map[string]any{"entries": l}, err
	})
	read("project/{id}", func(_ context.Context, r *http.Request) (any, error) { return s.Linked(r.PathValue("id")) })

	mux.HandleFunc("POST /api/payments/plan", func(w http.ResponseWriter, r *http.Request) {
		var in PlanRequest
		if !decode(w, r, &in) {
			return
		}
		p, err := s.Plan(in)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, p)
	})
	mux.HandleFunc("POST /api/payments/plan/execute", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var in Plan
		if !decode(w, r, &in) {
			return
		}
		// Refuse before anything else: a live (or unrecognised) key never
		// gets as far as building a request.
		if st := s.Stripe.Status(); st.Configured && !st.WritesAllowed {
			http.Error(w, LiveRefusal, http.StatusForbidden)
			return
		}
		c, cancel := ctx(r, execTime)
		defer cancel()
		res, err := s.Execute(c, in)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
}
