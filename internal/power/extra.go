package power

import (
	"context"
	"sync"
)

// Extra is something outside this package that the supervisor stops once it
// has been idle, such as the on-demand database managers. It logs its own
// starts and stops to the supervisor's activity log (kind "manager").
type Extra interface {
	// Tick stops what has been idle long enough. It runs on every check.
	Tick(ctx context.Context)
	// Sleep stops everything that is up, for "Sleep everything idle".
	Sleep(ctx context.Context) []Outcome
}

type extras struct {
	mu   sync.Mutex
	list []Extra
}

// AddExtra registers an Extra. It is safe to call while the supervisor runs.
func (s *Supervisor) AddExtra(e Extra) {
	s.extras.mu.Lock()
	defer s.extras.mu.Unlock()
	s.extras.list = append(s.extras.list, e)
}

func (s *Supervisor) extraList() []Extra {
	s.extras.mu.Lock()
	defer s.extras.mu.Unlock()
	return append([]Extra(nil), s.extras.list...)
}
