package assistant

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// MaxRequest is the largest request body accepted.
const MaxRequest = 64 << 10

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrBadRequest), errors.Is(err, agentexec.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, ErrConfidential):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, ErrBusy):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, agentexec.ErrInContainer):
		http.Error(w, err.Error(), http.StatusNotImplemented)
	case errors.Is(err, agentexec.ErrCLIMissing), errors.Is(err, agentexec.ErrNotSignedIn):
		http.Error(w, err.Error(), http.StatusFailedDependency)
	case errors.Is(err, agentexec.ErrTimeout):
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	case errors.Is(err, ErrBadOutput):
		http.Error(w, err.Error(), http.StatusBadGateway)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxRequest))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		http.Error(w, "invalid request JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// CheckRequest is the body of POST /api/assistant/check.
type CheckRequest struct {
	Action Action `json:"action"`
	// Bot limits the catalog to the bot's allowed actions.
	Bot string `json:"bot,omitempty"`
}

// Register adds the assistant routes to mux:
//
//	GET    /api/assistant/catalog                        the action catalog, providers, item types and steps
//	GET    /api/assistant/conversations                  summaries, newest first
//	GET    /api/assistant/conversations/{id}             one conversation
//	DELETE /api/assistant/conversations/{id}             remove it
//	POST   /api/assistant/conversations/{id}/outcome     {message, proposal, status, note?}: record Apply/Skip
//	POST   /api/assistant/turn                           {conversation?, message, provider?, model?, profile?, bot?, project?, page?}
//	POST   /api/assistant/check                          {action, bot?}: validate one action again, fresh
//	POST   /api/assistant/braindump                      {text, provider?, model?, profile?}: items, a preview
//	GET    /api/assistant/bots                           saved bots
//	PUT    /api/assistant/bots/{id}                      save a bot
//	DELETE /api/assistant/bots/{id}                      remove a bot
//	GET    /api/assistant/bots/import                    agents found in the CLIs' folders, read-only
//	POST   /api/assistant/bots/import                    {key, persona}: save one as a bot
//
// Every change needs X-Lucid-Confirm, and so do turn and braindump, which
// spend on the user's account. Check changes nothing. Nothing here applies
// a proposal: the UI does, through the routes each proposal names.
func Register(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/assistant/catalog", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, map[string]any{
			"actions": Catalog, "providers": Providers, "types": ItemTypes, "next": NextSteps, "sprites": SpriteSlots,
			"prompts": map[string]string{"assistant": PromptVersion(ChatPrompt), "braindump": PromptVersion(BraindumpPrompt)},
		})
	})
	mux.HandleFunc("GET /api/assistant/conversations", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.List()
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, l)
	})
	mux.HandleFunc("GET /api/assistant/conversations/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, err := s.Conversation(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, c)
	})
	mux.HandleFunc("DELETE /api/assistant/conversations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		if err := s.Delete(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/assistant/conversations/{id}/outcome", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var o Outcome
		if !decode(w, r, &o) {
			return
		}
		c, err := s.Record(r.PathValue("id"), o)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, c)
	})
	mux.HandleFunc("POST /api/assistant/turn", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req TurnRequest
		if !decode(w, r, &req) {
			return
		}
		res, err := s.Turn(r.Context(), req)
		if err != nil && res == nil {
			fail(w, err)
			return
		}
		// A failed model call is part of the conversation: it comes back
		// with the error on the assistant's message.
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST /api/assistant/check", func(w http.ResponseWriter, r *http.Request) {
		var req CheckRequest
		if !decode(w, r, &req) {
			return
		}
		p, err := s.Check(req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, p)
	})
	busy := make(chan struct{}, 1)
	mux.HandleFunc("POST /api/assistant/braindump", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req BraindumpRequest
		if !decode(w, r, &req) {
			return
		}
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		default:
			http.Error(w, "a braindump is already being parsed", http.StatusConflict)
			return
		}
		res, err := s.Braindump(r.Context(), req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("GET /api/assistant/bots", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.Bots())
	})
	mux.HandleFunc("GET /api/assistant/bots/import", func(w http.ResponseWriter, r *http.Request) {
		apiutil.WriteJSON(w, http.StatusOK, s.ImportCandidates())
	})
	mux.HandleFunc("POST /api/assistant/bots/import", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var req ImportRequest
		if !decode(w, r, &req) {
			return
		}
		b, err := s.Import(req)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, b)
	})
	mux.HandleFunc("PUT /api/assistant/bots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		var b Bot
		if !decode(w, r, &b) {
			return
		}
		if b.ID != r.PathValue("id") {
			http.Error(w, "the bot's id does not match the route", http.StatusBadRequest)
			return
		}
		saved, err := s.SaveBot(b)
		if err != nil {
			fail(w, err)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, saved)
	})
	mux.HandleFunc("DELETE /api/assistant/bots/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		if err := s.DeleteBot(r.PathValue("id")); err != nil {
			fail(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// Check validates one action against the current state, for Apply: the UI
// checks again right before it sends anything, so a page made since the
// proposal or a project removed from projects.yaml is caught.
func (s *Service) Check(req CheckRequest) (*Proposal, error) {
	w, err := s.World()
	if err != nil {
		return nil, err
	}
	var allowed []string
	if req.Bot != "" {
		b, err := s.Bot(req.Bot)
		if err != nil {
			return nil, err
		}
		allowed = b.AllowedActions
	}
	p := Validate(req.Action, w, allowed)
	return &p, nil
}
