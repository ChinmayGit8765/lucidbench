package council

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Summary is one row of the sessions list.
type Summary struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Input        string    `json:"input"` // the first 240 characters
	Project      string    `json:"project,omitempty"`
	Status       string    `json:"status"`
	Stage        string    `json:"stage"`
	Mode         string    `json:"mode"`
	Proposer     string    `json:"proposer"`
	Critics      []string  `json:"critics"`
	Rounds       int       `json:"rounds"`
	StoppedEarly bool      `json:"stopped_early"`
	BriefPath    string    `json:"brief_path,omitempty"`
	Card         string    `json:"card,omitempty"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	Error        string    `json:"error,omitempty"`
	Created      time.Time `json:"created"`
	Updated      time.Time `json:"updated"`
}

func summarise(s *Session) Summary {
	out := Summary{
		ID: s.ID, Title: s.Title, Input: excerpt(oneLine(s.Input), 240), Project: s.Project, Status: s.Status,
		Stage: s.Stage, Mode: s.Mode, Proposer: s.Proposer, Critics: s.Critics, Rounds: len(s.Rounds),
		StoppedEarly: s.StoppedEarly, BriefPath: s.BriefPath, Error: s.Error, Created: s.Created, Updated: s.Updated,
	}
	if out.Critics == nil {
		out.Critics = []string{}
	}
	if s.Card != nil {
		out.Card = s.Card.ID
	}
	for _, u := range s.Usage {
		out.InputTokens += u.InputTokens + u.CacheRead + u.CacheWrite
		out.OutputTokens += u.OutputTokens
		out.CostUSD += u.CostUSD
	}
	return out
}

// List returns every stored session, newest first.
func (s *Service) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Summary{}
	for _, e := range ents {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !idRE.MatchString(id) {
			continue
		}
		sess := s.live[id]
		if sess == nil {
			if sess, err = s.readLocked(id); err != nil {
				continue
			}
			if sess.Status == StatusRunning {
				sess.Status = StatusFailed
				sess.Error = "the run was interrupted: Lucidbench stopped before it finished"
			}
		}
		out = append(out, summarise(sess))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

func (s *Service) file(id string) string { return filepath.Join(s.Dir, id+".json") }

func (s *Service) readLocked(id string) (*Session, error) {
	data, err := os.ReadFile(s.file(id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// persistLocked writes the session record atomically.
func (s *Service) persistLocked(sess *Session) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".council-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, s.file(sess.ID)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// clone is a deep copy, so a snapshot can leave the lock.
func clone(sess *Session) *Session {
	data, err := json.Marshal(sess)
	if err != nil {
		return &Session{ID: sess.ID, Status: sess.Status}
	}
	var out Session
	_ = json.Unmarshal(data, &out)
	return &out
}

// update is one message to an events subscriber.
type update struct {
	data []byte
	done bool // the run is over; close the stream
}

// subscribe returns a channel of session snapshots (JSON) for id, and a
// function that ends the subscription. The channel closes when the run ends.
func (s *Service) subscribe(id string) (<-chan update, func()) {
	ch := make(chan update, 16)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs == nil {
		s.subs = map[string]map[chan update]struct{}{}
	}
	if s.subs[id] == nil {
		s.subs[id] = map[chan update]struct{}{}
	}
	s.subs[id][ch] = struct{}{}
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if set := s.subs[id]; set != nil {
			if _, ok := set[ch]; ok {
				delete(set, ch)
				close(ch)
			}
			if len(set) == 0 {
				delete(s.subs, id)
			}
		}
	}
}

// publishLocked sends a snapshot to every subscriber. Each message is a
// whole session, so a slow reader that misses one loses nothing.
func (s *Service) publishLocked(snap *Session) {
	set := s.subs[snap.ID]
	if len(set) == 0 {
		return
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	for ch := range set {
		select {
		case ch <- update{data: data, done: snap.Status != StatusRunning}:
		default:
		}
	}
}

// closeSubsLocked ends every stream for id once its run is over.
func (s *Service) closeSubsLocked(id string) {
	for ch := range s.subs[id] {
		close(ch)
	}
	delete(s.subs, id)
}
