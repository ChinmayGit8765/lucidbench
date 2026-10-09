package nextup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/agentexec"
)

// Settings tune the list. They live in state.json, not in config.
type Settings struct {
	// FocusProject ranks its work higher; Pinned projects a little less so.
	FocusProject string   `json:"focus_project,omitempty"`
	Pinned       []string `json:"pinned"`
	// ScheduleHours re-ranks with the agent every N hours while the app is
	// open; 0 (the default) never does.
	ScheduleHours int `json:"schedule_hours"`
	// Provider and Model are the ranking agent's; "" picks the team's scout,
	// else the first installed CLI on its cheap model.
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// Dismissal is an item the user set aside ("Not now").
type Dismissal struct {
	At      time.Time `json:"at"`
	Reason  string    `json:"reason"`
	Project string    `json:"project,omitempty"`
	Kind    string    `json:"kind,omitempty"`
}

// RankedItem is one entry of an agent ranking, validated, with the real id.
type RankedItem struct {
	ID              string `json:"id"`
	Reason          string `json:"reason,omitempty"`
	SuggestedAction string `json:"suggested_action,omitempty"`
	SuggestedPrompt string `json:"suggested_prompt,omitempty"`
}

// Ranking is the last agent ranking, kept until the next one.
type Ranking struct {
	At            time.Time       `json:"at"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model,omitempty"`
	PromptVersion string          `json:"prompt_version"`
	Usage         agentexec.Usage `json:"usage"`
	Items         []RankedItem    `json:"items"`
}

// State is <DataDir>/nextup/state.json.
type State struct {
	Snoozed   map[string]time.Time `json:"snoozed"`
	Dismissed map[string]Dismissal `json:"dismissed"`
	Settings  Settings             `json:"settings"`
	LastRank  *Ranking             `json:"last_rank,omitempty"`
}

// Store reads and writes state.json.
type Store struct {
	Dir string
	mu  sync.Mutex
}

// StateFile is the state file's name inside Dir.
const StateFile = "state.json"

func (s *Store) path() string { return filepath.Join(s.Dir, StateFile) }

func empty() State {
	return State{Snoozed: map[string]time.Time{}, Dismissed: map[string]Dismissal{}, Settings: Settings{Pinned: []string{}}}
}

// Load reads the state; a missing or unreadable file is an empty state.
func (s *Store) Load() State {
	if s == nil {
		return empty()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() State {
	st := empty()
	b, err := os.ReadFile(s.path())
	if err != nil {
		return st
	}
	if json.Unmarshal(b, &st) != nil {
		return empty()
	}
	if st.Snoozed == nil {
		st.Snoozed = map[string]time.Time{}
	}
	if st.Dismissed == nil {
		st.Dismissed = map[string]Dismissal{}
	}
	if st.Settings.Pinned == nil {
		st.Settings.Pinned = []string{}
	}
	return st
}

// Update changes the state under the lock and writes it atomically.
func (s *Store) Update(fn func(*State) error) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	if err := fn(&st); err != nil {
		return st, err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return st, err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return st, err
	}
	return st, os.Rename(tmp, s.path())
}

// Reasons a user may give for "Not now".
var Reasons = []string{"not-important", "blocked", "someone-else", "later", "other"}

// SnoozeDays are the lengths "Snooze" offers.
var SnoozeDays = []int{1, 7}

var (
	idRE      = regexp.MustCompile(`^(card|brief|work|pr|ci|linear|trello|need):[^\s]{1,200}$`)
	projectRE = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
)

// CheckID refuses ids that no collector makes.
func CheckID(id string) error {
	if !idRE.MatchString(id) {
		return fmt.Errorf("%w: %q is not a Next up id", ErrBadRequest, id)
	}
	return nil
}

// Snooze hides id until now plus days.
func (s *Store) Snooze(id string, days int, now time.Time) (time.Time, error) {
	if err := CheckID(id); err != nil {
		return time.Time{}, err
	}
	if !containsInt(SnoozeDays, days) {
		return time.Time{}, fmt.Errorf("%w: snooze for 1 or 7 days", ErrBadRequest)
	}
	until := now.Add(time.Duration(days) * 24 * time.Hour).UTC()
	_, err := s.Update(func(st *State) error {
		st.Snoozed[id] = until
		delete(st.Dismissed, id)
		return nil
	})
	return until, err
}

// Dismiss sets id aside with a reason, remembered for scoring.
func (s *Store) Dismiss(id, reason, project, kind string, now time.Time) error {
	if err := CheckID(id); err != nil {
		return err
	}
	if !contains(Reasons, reason) {
		return fmt.Errorf("%w: the reason must be one of %v", ErrBadRequest, Reasons)
	}
	_, err := s.Update(func(st *State) error {
		st.Dismissed[id] = Dismissal{At: now.UTC(), Reason: reason, Project: project, Kind: kind}
		delete(st.Snoozed, id)
		return nil
	})
	return err
}

// Restore brings a snoozed or set-aside id back.
func (s *Store) Restore(id string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	_, err := s.Update(func(st *State) error {
		delete(st.Snoozed, id)
		delete(st.Dismissed, id)
		return nil
	})
	return err
}

// MaxScheduleHours is the longest schedule; 0 is off.
const MaxScheduleHours = 168

// SaveSettings checks and saves the settings.
func (s *Store) SaveSettings(in Settings) (Settings, error) {
	if in.FocusProject != "" && !projectRE.MatchString(in.FocusProject) {
		return in, fmt.Errorf("%w: focus_project must be a project id", ErrBadRequest)
	}
	if len(in.Pinned) > 20 {
		return in, fmt.Errorf("%w: pin at most 20 projects", ErrBadRequest)
	}
	pinned := []string{}
	for _, p := range in.Pinned {
		if !projectRE.MatchString(p) {
			return in, fmt.Errorf("%w: %q is not a project id", ErrBadRequest, p)
		}
		if !contains(pinned, p) {
			pinned = append(pinned, p)
		}
	}
	in.Pinned = pinned
	if in.ScheduleHours < 0 || in.ScheduleHours > MaxScheduleHours {
		return in, fmt.Errorf("%w: schedule_hours must be 0 (off) to %d", ErrBadRequest, MaxScheduleHours)
	}
	if in.Provider != "" {
		if _, ok := agentexec.Logins[in.Provider]; !ok {
			return in, fmt.Errorf("%w: provider must be claude, codex or grok", ErrBadRequest)
		}
	}
	if len(in.Model) > 64 {
		return in, fmt.Errorf("%w: the model name is too long", ErrBadRequest)
	}
	st, err := s.Update(func(st *State) error {
		st.Settings = in
		return nil
	})
	return st.Settings, err
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
