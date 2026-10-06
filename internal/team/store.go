package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Where a team came from.
const (
	SourceRepo    = "repo"    // <local_path>/.lucid/team.yaml
	SourceData    = "data"    // <DataDir>/teams/<project>.yaml
	SourceConfig  = "config"  // config.yaml `team:`
	SourceBuiltin = "builtin" // nothing configured: Lucidbench's defaults
)

// RepoFile is a team's path inside a project's checkout.
const RepoFile = ".lucid/team.yaml"

// Errors the HTTP layer maps to statuses.
var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid team")
	ErrTarget   = errors.New("bad target")
)

// Store finds and saves teams.
type Store struct {
	// DataDir holds teams/<project>.yaml.
	DataDir string
	// Default is the `team:` YAML from config.yaml, "" when unset.
	Default string
	// Projects loads projects.yaml; nil means projects.Load.
	Projects func() (*projects.List, error)
	// Home is shown as "~" in path hints.
	Home string
}

// Resolved is a project's team and where it came from.
type Resolved struct {
	Project string `json:"project"`
	Team    Team   `json:"team"`
	Source  string `json:"source"`
	// PathHint is where the team was read from, home shown as "~"; "" for
	// the built-in default.
	PathHint string `json:"path_hint,omitempty"`
	// Targets are where Save may write: "repo" only when the project has an
	// existing local_path.
	Targets map[string]string `json:"targets"`
	// Problems found while reading: a file that does not parse or validate
	// is reported here and the next source is used.
	Problems     []Problem `json:"problems"`
	Confidential bool      `json:"confidential"`
	ProjectName  string    `json:"project_name"`
}

var projectRE = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

func (s *Store) project(id string) (*projects.Project, error) {
	if !projectRE.MatchString(id) {
		return nil, fmt.Errorf("%w: no project %q", ErrNotFound, id)
	}
	load := s.Projects
	if load == nil {
		load = projects.Load
	}
	l, err := load()
	if err != nil {
		return nil, err
	}
	p := l.Find(id)
	if p == nil {
		return nil, fmt.Errorf("%w: no project %q in projects.yaml", ErrNotFound, id)
	}
	return p, nil
}

func (s *Store) hint(p string) string { return projects.HomeHint(p, s.Home) }

// repoDir is the project's checkout when it exists as a folder.
func repoDir(p *projects.Project) string {
	if p.LocalPath == "" {
		return ""
	}
	if st, err := os.Stat(p.LocalPath); err != nil || !st.IsDir() {
		return ""
	}
	return p.LocalPath
}

func (s *Store) dataPath(id string) string { return filepath.Join(s.DataDir, "teams", id+".yaml") }

// Resolve finds a project's team: the checkout's .lucid/team.yaml, then the
// data dir's teams/<project>.yaml, then the config default, then the
// built-in default. A file that fails to parse or validate is skipped with a
// problem, so a broken file never silently changes who runs.
func (s *Store) Resolve(id string) (*Resolved, error) {
	p, err := s.project(id)
	if err != nil {
		return nil, err
	}
	out := &Resolved{Project: id, ProjectName: p.Name, Confidential: p.Visibility == "confidential",
		Targets: map[string]string{SourceData: s.hint(s.dataPath(id))}, Problems: []Problem{}}
	repo := repoDir(p)
	if repo != "" {
		out.Targets[SourceRepo] = s.hint(filepath.Join(repo, filepath.FromSlash(RepoFile)))
	}
	try := func(source, path string, data []byte) bool {
		t, err := Parse(data)
		if err == nil {
			if ps := Validate(t); len(ps) > 0 {
				err = errors.New(ps[0].Message)
			}
		}
		where := s.hint(path)
		if source == SourceConfig {
			where = "config.yaml team:"
		}
		if err != nil {
			out.Problems = append(out.Problems, Problem{Severity: SevError, Code: "file", Message: fmt.Sprintf("%s was skipped: %v", where, err)})
			return false
		}
		out.Team, out.Source, out.PathHint = Normalise(t), source, where
		return true
	}
	if repo != "" {
		f := filepath.Join(repo, filepath.FromSlash(RepoFile))
		if data, err := os.ReadFile(f); err == nil && try(SourceRepo, f, data) {
			return out, nil
		}
	}
	if data, err := os.ReadFile(s.dataPath(id)); err == nil && try(SourceData, s.dataPath(id), data) {
		return out, nil
	}
	if s.Default != "" && try(SourceConfig, "", []byte(s.Default)) {
		return out, nil
	}
	out.Team, out.Source = Normalise(Default()), SourceBuiltin
	return out, nil
}

// Save writes t for a project to target ("repo" or "data") and returns the
// project's team as it now resolves. A team with a schema error is refused.
func (s *Store) Save(id string, t Team, target string) (*Resolved, error) {
	p, err := s.project(id)
	if err != nil {
		return nil, err
	}
	t = Normalise(t)
	if ps := Validate(t); len(ps) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, ps[0].Message)
	}
	var path string
	switch target {
	case SourceRepo:
		repo := repoDir(p)
		if repo == "" {
			return nil, fmt.Errorf("%w: %s has no checkout at its local_path, so its team can only be kept in the data folder", ErrTarget, p.Name)
		}
		path = filepath.Join(repo, filepath.FromSlash(RepoFile))
	case SourceData:
		path = s.dataPath(id)
	default:
		return nil, fmt.Errorf("%w: target must be repo or data", ErrTarget)
	}
	data, err := Marshal(t)
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(path, data); err != nil {
		return nil, err
	}
	return s.Resolve(id)
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DefaultTeam is the config default, or the built-in one.
func (s *Store) DefaultTeam() (Team, string, []Problem) {
	if s.Default != "" {
		t, err := Parse([]byte(s.Default))
		if err == nil {
			if ps := Validate(t); len(ps) > 0 {
				return Normalise(Default()), SourceBuiltin, ps
			}
			return Normalise(t), SourceConfig, nil
		}
		return Normalise(Default()), SourceBuiltin, []Problem{{Severity: SevError, Code: "file", Message: "config.yaml team: " + err.Error()}}
	}
	return Normalise(Default()), SourceBuiltin, nil
}

// ---- recent costs ----

// MaxEstimateRuns is how many recent runs an estimate averages.
const MaxEstimateRuns = 20

type costRun struct {
	role, provider string
	usd            float64
	at             time.Time
}

// Estimator averages what recent council and Work runs cost, per role and
// provider, from the records under the data dir.
func Estimator(dataDir string) func(role, provider string) (float64, int) {
	runs := readCosts(dataDir)
	return func(role, provider string) (float64, int) {
		var sum float64
		n := 0
		for _, r := range runs {
			if r.role == role && r.provider == provider {
				sum += r.usd
				n++
				if n == MaxEstimateRuns {
					break
				}
			}
		}
		if n == 0 {
			return 0, 0
		}
		return sum / float64(n), n
	}
}

type usageRec struct {
	CostUSD float64 `json:"cost_usd"`
}

type stepRec struct {
	Provider string    `json:"provider"`
	Role     string    `json:"role"`
	Usage    *usageRec `json:"usage"`
}

// readCosts reads council steps (the proposer's draft and revisions are one
// proposer run; each critique is one critic run) and Work sessions (one
// builder run each), newest first. Runs that reported no cost are left out.
func readCosts(dataDir string) []costRun {
	var out []costRun
	files, _ := filepath.Glob(filepath.Join(dataDir, "council", "*.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			Created time.Time `json:"created"`
			Rounds  []struct {
				Proposal  *stepRec   `json:"proposal"`
				Critiques []*stepRec `json:"critiques"`
				Synthesis *stepRec   `json:"synthesis"`
			} `json:"rounds"`
		}
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		proposer := map[string]float64{}
		for _, r := range s.Rounds {
			for _, st := range []*stepRec{r.Proposal, r.Synthesis} {
				if st != nil && st.Usage != nil && st.Usage.CostUSD > 0 {
					proposer[st.Provider] += st.Usage.CostUSD
				}
			}
			for _, st := range r.Critiques {
				if st != nil && st.Usage != nil && st.Usage.CostUSD > 0 {
					out = append(out, costRun{role: RoleCritic, provider: st.Provider, usd: st.Usage.CostUSD, at: s.Created})
				}
			}
		}
		for p, usd := range proposer {
			out = append(out, costRun{role: RoleProposer, provider: p, usd: usd, at: s.Created})
		}
	}
	sessions, _ := filepath.Glob(filepath.Join(dataDir, "work", "sessions", "*", "session.json"))
	for _, f := range sessions {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var s struct {
			Provider string    `json:"provider"`
			Started  time.Time `json:"started"`
			Usage    *usageRec `json:"usage"`
		}
		if json.Unmarshal(b, &s) != nil || s.Usage == nil || s.Usage.CostUSD <= 0 {
			continue
		}
		out = append(out, costRun{role: RoleBuilder, provider: s.Provider, usd: s.Usage.CostUSD, at: s.Started})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at.After(out[j].at) })
	return out
}
