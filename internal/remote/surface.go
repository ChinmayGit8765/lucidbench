package remote

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/council"
	"github.com/ChinmayGit8765/lucidbench/internal/work"
)

// maxBody is the largest request body the remote accepts.
const maxBody = 8 << 10

// MaxNotes is the longest "send back" note.
const MaxNotes = 4000

// Surface is the remote listener's handler: the phone page and an allowlist
// of routes. Every route not listed in Routes is 404.
type Surface struct {
	Services Services
	Devices  *Devices
	Audit    *Audit
	// Assets is the built phone page (web/dist/r); nil serves a short notice.
	Assets fs.FS
	// PairLimit counts failed pairings per client and AuthLimit failed
	// tokens per client; PairGlobal counts failed pairings from anyone.
	PairLimit, PairGlobal, AuthLimit *Limiter
}

// Routes is the whole remote API, for the docs and the allowlist test.
var Routes = []string{
	"POST /r/api/pair",
	"GET /r/api/overview",
	"GET /r/api/work/sessions",
	"GET /r/api/work/sessions/{id}/events",
	"POST /r/api/work/sessions/{id}/stop",
	"POST /r/api/work/sessions/{id}/followup",
	"GET /r/api/council/sessions/{id}",
	"POST /r/api/council/sessions/{id}/approve",
	"POST /r/api/council/sessions/{id}/send-back",
}

// NewSurface returns a surface with the default limits: 5 failed pairings
// per client and 20 overall per 10 minutes, 10 failed tokens per client per
// 10 minutes.
func NewSurface(svc Services, devices *Devices, audit *Audit, assets fs.FS) *Surface {
	return &Surface{
		Services: svc, Devices: devices, Audit: audit, Assets: assets,
		PairLimit:  NewLimiter(5, 10*time.Minute),
		PairGlobal: NewLimiter(20, 10*time.Minute),
		AuthLimit:  NewLimiter(10, 10*time.Minute),
	}
}

func clientOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// secure sets the headers every remote answer carries. No CORS header is
// ever set, so another origin's script cannot read an answer.
func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; manifest-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "no-referrer")
		hd.Set("Cross-Origin-Opener-Policy", "same-origin")
		hd.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.ServeHTTP(w, r)
	})
}

// Handler returns the remote listener's handler.
func (s *Surface) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /r/api/pair", s.pair)
	mux.HandleFunc("GET /r/api/overview", s.authed(func(w http.ResponseWriter, r *http.Request, _ Device) {
		apiutil.WriteJSON(w, http.StatusOK, s.Services.overview(r.Context()))
	}))
	mux.HandleFunc("GET /r/api/work/sessions", s.authed(s.sessions))
	mux.HandleFunc("GET /r/api/work/sessions/{id}/events", s.authed(s.events))
	mux.HandleFunc("POST /r/api/work/sessions/{id}/stop", s.write("stop", s.stop))
	mux.HandleFunc("POST /r/api/work/sessions/{id}/followup", s.write("follow_up", s.followUp))
	mux.HandleFunc("GET /r/api/council/sessions/{id}", s.authed(s.brief))
	mux.HandleFunc("POST /r/api/council/sessions/{id}/approve", s.write("approve", s.approve))
	mux.HandleFunc("POST /r/api/council/sessions/{id}/send-back", s.write("send_back", s.sendBack))
	mux.Handle("GET /r/", s.page())
	mux.Handle("GET /r", s.page())
	// Anything else, any method: 404, the same as a route that never existed.
	mux.Handle("/", http.NotFoundHandler())
	return secure(mux)
}

// page serves the phone page's files, and nothing above dist/r.
func (s *Surface) page() http.Handler {
	if s.Assets == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "the phone page is not built; run npm run build in web/ and rebuild lucidd", http.StatusServiceUnavailable)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/r")
		p = strings.TrimPrefix(p, "/")
		if strings.HasPrefix(p, "api/") || p == "api" {
			http.NotFound(w, r)
			return
		}
		if p != "" {
			if st, err := fs.Stat(s.Assets, p); err != nil || st.IsDir() {
				http.NotFound(w, r)
				return
			}
		}
		if p == "" || p == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			p = "index.html"
		}
		http.ServeFileFS(w, r, s.Assets, p)
	})
}

type authedFunc func(w http.ResponseWriter, r *http.Request, dev Device)

// authed checks the Bearer token. Too many failures from one client answer
// 429 until the window passes, whatever the token.
func (s *Surface) authed(fn authedFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := clientOf(r)
		if !s.AuthLimit.Allowed(client) {
			w.Header().Set("Retry-After", "600")
			http.Error(w, "too many failed attempts; try again later", http.StatusTooManyRequests)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		dev, err := s.Devices.Auth(strings.TrimSpace(token))
		if !ok || err != nil {
			s.AuthLimit.Fail(client)
			s.Audit.Add(AuditEntry{Action: "auth_failed", Result: "refused", Client: client})
			w.Header().Set("WWW-Authenticate", `Bearer realm="lucidbench"`)
			http.Error(w, "pair this phone again: the token is missing, unknown or revoked", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		fn(w, r, dev)
	}
}

// write is authed plus the confirm header, and every attempt is audited.
func (s *Surface) write(action string, fn func(w http.ResponseWriter, r *http.Request, dev Device) (int, error)) http.HandlerFunc {
	return s.authed(func(w http.ResponseWriter, r *http.Request, dev Device) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		code, err := fn(w, r, dev)
		result := "ok"
		if err != nil {
			result = err.Error()
			http.Error(w, err.Error(), code)
		}
		s.Audit.Add(AuditEntry{Action: action, Device: dev.ID, Name: dev.Name, Target: r.PathValue("id"), Result: result, Client: clientOf(r)})
	})
}

func (s *Surface) pair(w http.ResponseWriter, r *http.Request) {
	client := clientOf(r)
	if !s.PairLimit.Allowed(client) || !s.PairGlobal.Allowed("*") {
		w.Header().Set("Retry-After", "600")
		http.Error(w, "too many failed pairing attempts; try again later", http.StatusTooManyRequests)
		return
	}
	if !apiutil.Confirmed(w, r) {
		return
	}
	var in struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	token, dev, err := s.Devices.Pair(in.Code, in.Name)
	if err != nil {
		if errors.Is(err, ErrBadCode) {
			s.PairLimit.Fail(client)
			s.PairGlobal.Fail("*")
			s.Audit.Add(AuditEntry{Action: "pair_failed", Result: err.Error(), Client: client})
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		http.Error(w, "could not save the device", http.StatusInternalServerError)
		return
	}
	s.Audit.Add(AuditEntry{Action: "pair", Device: dev.ID, Name: dev.Name, Result: "ok", Client: client})
	apiutil.WriteJSON(w, http.StatusOK, map[string]any{"token": token, "device": dev.Public()})
}

func (s *Surface) sessions(w http.ResponseWriter, r *http.Request, _ Device) {
	out := []WorkView{}
	if s.Services.Work != nil {
		pv := s.Services.privacy()
		for _, se := range s.Services.Work.List() {
			if se.Removed {
				continue
			}
			out = append(out, workView(se, pv.work(se)))
		}
	}
	apiutil.WriteJSON(w, http.StatusOK, out)
}

// workSession finds a session and refuses a confidential one with 403.
func (s *Surface) workSession(id string) (work.Session, int, error) {
	if s.Services.Work == nil {
		return work.Session{}, http.StatusNotFound, errors.New("work is not available")
	}
	se, err := s.Services.Work.Get(id)
	if err != nil {
		return se, http.StatusNotFound, errors.New("no such session")
	}
	if s.Services.privacy().work(se) {
		return se, http.StatusForbidden, errors.New("this session belongs to a confidential project; open it on the desktop")
	}
	return se, 0, nil
}

// events streams a session's events as server-sent events, like the desktop
// route, with each event cut down to its kind, title and text. The device is
// checked again on every change, ping and revocation, so a revoked phone's
// stream ends at once.
func (s *Surface) events(w http.ResponseWriter, r *http.Request, dev Device) {
	id := r.PathValue("id")
	if _, code, err := s.workSession(id); err != nil {
		http.Error(w, err.Error(), code)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	next := 0
	if v, err := strconv.Atoi(r.Header.Get("Last-Event-ID")); err == nil && v >= 0 {
		next = v + 1
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(event string, v any, eid int) bool {
		data, _ := json.Marshal(v)
		msg := ""
		if eid >= 0 {
			msg += "id: " + strconv.Itoa(eid) + "\n"
		}
		if event != "" {
			msg += "event: " + event + "\n"
		}
		if _, err := io.WriteString(w, msg+"data: "+string(data)+"\n\n"); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	sent := ""
	for {
		revoked := s.Devices.Revoked()
		if !s.Devices.Has(dev.ID) {
			return
		}
		evs, running, changed, err := s.Services.Work.Events(id, next)
		if err != nil {
			return
		}
		for _, ev := range evs {
			if !send("", eventView(ev), next) {
				return
			}
			next++
		}
		if se, err := s.Services.Work.Get(id); err == nil && (se.Status != sent || !running) {
			if !send("session", workView(se, false), -1) {
				return
			}
			sent = se.Status
		}
		if !running {
			_, _ = io.WriteString(w, "event: end\ndata: \n\n")
			fl.Flush()
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-revoked:
		case <-changed:
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *Surface) stop(w http.ResponseWriter, r *http.Request, _ Device) (int, error) {
	id := r.PathValue("id")
	if s.Services.Work == nil {
		return http.StatusNotFound, errors.New("no such session")
	}
	// Stopping is allowed for a confidential session too: it only ends a run,
	// and the answer is redacted.
	se, err := s.Services.Work.Get(id)
	if err != nil {
		return http.StatusNotFound, errors.New("no such session")
	}
	conf := s.Services.privacy().work(se)
	se, err = s.Services.Work.Stop(id)
	if err != nil {
		if errors.Is(err, work.ErrConflict) {
			return http.StatusConflict, errors.New("the session is not running")
		}
		return http.StatusInternalServerError, errors.New("the session could not be stopped")
	}
	apiutil.WriteJSON(w, http.StatusOK, workView(se, conf))
	return 0, nil
}

// followUp runs a new turn of a waiting session, like the desktop's
// follow-up. A confidential session is refused with 403 before anything is
// read from the body.
func (s *Surface) followUp(w http.ResponseWriter, r *http.Request, _ Device) (int, error) {
	id := r.PathValue("id")
	if _, code, err := s.workSession(id); err != nil {
		return code, err
	}
	var in struct {
		Prompt string `json:"prompt"`
	}
	if err := decodeBody(r, &in); err != nil {
		return http.StatusBadRequest, err
	}
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Prompt == "" {
		return http.StatusBadRequest, errors.New("write a follow-up")
	}
	if len(in.Prompt) > MaxNotes {
		return http.StatusBadRequest, errors.New("the follow-up is too long")
	}
	se, err := s.Services.Work.FollowUp(id, in.Prompt)
	switch {
	case errors.Is(err, work.ErrConflict):
		// The desktop's message may name the worktree; the phone gets none.
		return http.StatusConflict, errors.New("the session is not waiting for a follow-up: a turn is running, or it has ended")
	case errors.Is(err, work.ErrNotFound):
		return http.StatusNotFound, errors.New("no such session")
	case errors.Is(err, work.ErrBadRequest):
		return http.StatusBadRequest, err
	case err != nil:
		return http.StatusInternalServerError, errors.New("the follow-up could not start")
	}
	apiutil.WriteJSON(w, http.StatusOK, workView(se, false))
	return 0, nil
}

// councilSession finds a council run and refuses a confidential one.
func (s *Surface) councilSession(id string) (*council.Session, int, error) {
	if s.Services.Council == nil {
		return nil, http.StatusNotFound, errors.New("no such brief")
	}
	sess, err := s.Services.Council.Get(id)
	if err != nil {
		return nil, http.StatusNotFound, errors.New("no such brief")
	}
	if s.Services.privacy().council(sess.Project, sess.BriefPath) {
		return sess, http.StatusForbidden, errors.New("this brief belongs to a confidential project; open it on the desktop")
	}
	return sess, 0, nil
}

func (s *Surface) brief(w http.ResponseWriter, r *http.Request, _ Device) {
	sess, code, err := s.councilSession(r.PathValue("id"))
	if err != nil {
		if code == http.StatusForbidden {
			apiutil.WriteJSON(w, code, briefView(sess, true))
			return
		}
		http.Error(w, err.Error(), code)
		return
	}
	apiutil.WriteJSON(w, http.StatusOK, briefView(sess, false))
}

func decodeBody(r *http.Request, v any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && err != io.EOF {
		return errors.New("invalid JSON")
	}
	return nil
}

func councilCode(err error) int {
	switch {
	case errors.Is(err, council.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, council.ErrBusy), errors.Is(err, council.ErrApproved), errors.Is(err, council.ErrBlockers):
		return http.StatusConflict
	case errors.Is(err, council.ErrBadRequest):
		return http.StatusBadRequest
	case errors.Is(err, council.ErrConfidential):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

func (s *Surface) approve(w http.ResponseWriter, r *http.Request, _ Device) (int, error) {
	var in struct {
		ApprovedWithBlockers bool `json:"approved_with_blockers"`
	}
	if err := decodeBody(r, &in); err != nil {
		return http.StatusBadRequest, err
	}
	sess, code, err := s.councilSession(r.PathValue("id"))
	if err != nil {
		return code, err
	}
	if sess.Status != council.StatusDraft {
		return http.StatusConflict, errors.New("the brief is not waiting for approval")
	}
	card, err := s.Services.Council.ApproveChecked(sess.ID, "", in.ApprovedWithBlockers)
	if err != nil {
		return councilCode(err), err
	}
	apiutil.WriteJSON(w, http.StatusOK, map[string]any{"status": council.StatusApproved, "card": card.ID, "column": card.Column})
	return 0, nil
}

func (s *Surface) sendBack(w http.ResponseWriter, r *http.Request, _ Device) (int, error) {
	var in struct {
		Notes string `json:"notes"`
	}
	if err := decodeBody(r, &in); err != nil {
		return http.StatusBadRequest, err
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if in.Notes == "" {
		return http.StatusBadRequest, errors.New("say what to change")
	}
	if len(in.Notes) > MaxNotes {
		return http.StatusBadRequest, errors.New("the notes are too long")
	}
	sess, code, err := s.councilSession(r.PathValue("id"))
	if err != nil {
		return code, err
	}
	if sess.Status != council.StatusDraft {
		return http.StatusConflict, errors.New("the brief is not waiting for approval")
	}
	if _, err := s.Services.Council.BeginAgain(sess.ID, in.Notes); err != nil {
		return councilCode(err), err
	}
	apiutil.WriteJSON(w, http.StatusAccepted, map[string]any{"status": council.StatusRunning})
	return 0, nil
}
