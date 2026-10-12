package usage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
)

// Plan storage limits.
const (
	MaxPlanItems = 30
	MaxPlanTitle = 200
)

// planFile is <DataDir>/usage/plan.json.
type planFile struct {
	Version int        `json:"version"`
	Updated time.Time  `json:"updated"`
	Items   []PlanItem `json:"items"`
}

// PlanView is the body of GET and PUT /api/usage/plan.
type PlanView struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Hours       float64       `json:"hours"`
	Paces       []Pace        `json:"paces"`
	Items       []PlannedItem `json:"items"`
	Windows     []WindowPlan  `json:"windows"`
	Updated     *time.Time    `json:"updated,omitempty"`
}

var (
	planIDRE    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	planModelRE = regexp.MustCompile(`^[A-Za-z0-9._:\[\]-]{0,64}$`)
	planProjRE  = regexp.MustCompile(`^[a-z0-9-]{0,64}$`)
)

func (s *Service) planPath() string { return filepath.Join(s.DataDir, "usage", "plan.json") }

func (s *Service) readPlan() (planFile, error) {
	var f planFile
	b, err := os.ReadFile(s.planPath())
	if errors.Is(err, os.ErrNotExist) {
		return planFile{Version: 1, Items: []PlanItem{}}, nil
	}
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("usage/plan.json: %w", err)
	}
	if f.Items == nil {
		f.Items = []PlanItem{}
	}
	return f, nil
}

// cleanPlan checks and normalises a plan sent by the UI. Items without an id
// get one.
func cleanPlan(items []PlanItem) ([]PlanItem, error) {
	if len(items) > MaxPlanItems {
		return nil, fmt.Errorf("a plan holds at most %d items", MaxPlanItems)
	}
	out := make([]PlanItem, 0, len(items))
	seen := map[string]bool{}
	for i, it := range items {
		at := fmt.Sprintf("items[%d]", i)
		it.Title = strings.TrimSpace(it.Title)
		if it.Title == "" || utf8.RuneCountInString(it.Title) > MaxPlanTitle {
			return nil, fmt.Errorf("%s.title: 1 to %d characters", at, MaxPlanTitle)
		}
		if !contains(PlanKinds, it.Kind) {
			return nil, fmt.Errorf("%s.kind: want one of %s", at, strings.Join(PlanKinds, ", "))
		}
		if it.Provider != "" && !contains(PlanProviders, it.Provider) {
			return nil, fmt.Errorf("%s.provider: want one of %s, or empty to have one suggested", at, strings.Join(PlanProviders, ", "))
		}
		if !planModelRE.MatchString(it.Model) {
			return nil, fmt.Errorf("%s.model: not a model name", at)
		}
		if !planProjRE.MatchString(it.Project) {
			return nil, fmt.Errorf("%s.project: not a project id", at)
		}
		if it.Source != "" && it.Source != "nextup" {
			return nil, fmt.Errorf("%s.source: want nextup or empty", at)
		}
		if len(it.NextUpID) > 200 {
			return nil, fmt.Errorf("%s.nextup_id: too long", at)
		}
		if it.ID == "" {
			var b [4]byte
			_, _ = rand.Read(b[:])
			it.ID = "p" + hex.EncodeToString(b[:])
		}
		if !planIDRE.MatchString(it.ID) || seen[it.ID] {
			return nil, fmt.Errorf("%s.id: missing, malformed or repeated", at)
		}
		seen[it.ID] = true
		out = append(out, it)
	}
	return out, nil
}

func (s *Service) writePlan(items []PlanItem, now time.Time) error {
	f := planFile{Version: 1, Updated: now.UTC(), Items: items}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.planPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".plan-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.planPath())
}

// Planner computes the plan view: every account's pace and the saved plan
// with estimates, suggestions and fit verdicts. It reads local records only.
func (s *Service) Planner(hours float64) (*PlanView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.planner(hours)
}

func (s *Service) planner(hours float64) (*PlanView, error) {
	if hours <= 0 {
		hours = DefaultPaceHours
	}
	loc, now := s.Loc, time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if loc == nil {
		loc = time.Local
	}
	f, err := s.readPlan()
	if err != nil {
		return nil, err
	}
	sc := s.scan(now)
	ps := paces(sc, now, hours, loc)
	var avail []string
	if s.Available != nil {
		avail = s.Available()
	}
	items, windows := Plan(PlanInput{
		Items: f.Items, Paces: ps, Runs: planRuns(readOwn(s.DataDir)),
		Team: s.Team, Available: avail, Now: now, Loc: loc,
	})
	v := &PlanView{GeneratedAt: now.UTC(), Hours: hours, Paces: ps, Items: items, Windows: windows}
	if !f.Updated.IsZero() {
		u := f.Updated
		v.Updated = &u
	}
	return v, nil
}

// SavePlan replaces the saved plan and returns the new view.
func (s *Service) SavePlan(items []PlanItem, hours float64) (*PlanView, error) {
	items, err := cleanPlan(items)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errBadPlan, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if err := s.writePlan(items, now); err != nil {
		return nil, err
	}
	return s.planner(hours)
}

var errBadPlan = errors.New("invalid plan")

func paceHours(r *http.Request) (float64, error) {
	v := r.URL.Query().Get("hours")
	if v == "" {
		return DefaultPaceHours, nil
	}
	h, err := strconv.ParseFloat(v, 64)
	if err != nil || h < 0.25 || h > MaxPaceHours {
		return 0, fmt.Errorf("hours must be a number from 0.25 to %d", MaxPaceHours)
	}
	return h, nil
}

// registerPlan adds:
//
//	GET /api/usage/plan?hours=N   paces and the plan, worked out from local records
//	PUT /api/usage/plan           {items} saves the plan (needs X-Lucid-Confirm)
func registerPlan(mux *http.ServeMux, s *Service) {
	mux.HandleFunc("GET /api/usage/plan", func(w http.ResponseWriter, r *http.Request) {
		h, err := paceHours(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		v, err := s.Planner(h)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		apiutil.WriteJSON(w, http.StatusOK, v)
	})
	mux.HandleFunc("PUT /api/usage/plan", func(w http.ResponseWriter, r *http.Request) {
		if !apiutil.Confirmed(w, r) {
			return
		}
		h, err := paceHours(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var in struct {
			Items []PlanItem `json:"items"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		v, err := s.SavePlan(in.Items, h)
		switch {
		case errors.Is(err, errBadPlan):
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		apiutil.WriteJSON(w, http.StatusOK, v)
	})
}
