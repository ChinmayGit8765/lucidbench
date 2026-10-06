package projects

// Assessments confirmed in the app are kept in <data dir>/assessments/<id>.yaml,
// not written into projects.yaml.
//
// projects.yaml is the user's own file, full of their comments and layout.
// The YAML library keeps comments when it round-trips a document, but it
// re-indents, drops blank lines and rewrites flow lists, and one failed
// write would damage a file Lucidbench does not own. A side file per project
// cannot do either: projects.yaml stays byte-for-byte what the user wrote.
//
// On load the side files are merged into the list. An assessment written by
// hand in projects.yaml still works; when a project has both, the one
// confirmed in the app is newer by construction and wins.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/assess"
	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// AssessDirName is the folder inside the data dir.
const AssessDirName = "assessments"

// AssessDir returns where app-confirmed assessments are kept.
func AssessDir() (string, error) {
	d, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, AssessDirName), nil
}

// MergeAssessments overlays the side files in dir onto l. A file that cannot
// be read or fails validation is reported in l.Errors and ignored; the
// projects.yaml assessment, if any, then stays.
func (l *List) MergeAssessments(dir string) {
	for i := range l.Projects {
		p := &l.Projects[i]
		a, err := readAssessment(dir, p.ID)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			l.Errors = append(l.Errors, fmt.Sprintf("project %q: assessment: %v", p.ID, err))
			continue
		}
		p.Assessment, p.AssessmentFrom = a, "app"
		p.Risk = assess.Suggest(*a).Risk.Level
	}
}

func assessmentPath(dir, id string) (string, error) {
	if !idRE.MatchString(id) {
		return "", fmt.Errorf("bad project id %q", id)
	}
	return filepath.Join(dir, id+".yaml"), nil
}

func readAssessment(dir, id string) (*assess.Assessment, error) {
	path, err := assessmentPath(dir, id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a assess.Assessment
	if err := yaml.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("%s: %s", filepath.Base(path), strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if err := assess.Validate(a); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &a, nil
}

// writeAssessment saves a through a temp file and a rename, so a crash leaves
// the old file or the new one, never half of one.
func writeAssessment(dir, id string, a assess.Assessment) error {
	path, err := assessmentPath(dir, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(a)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".assess-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// AssessmentView is what the assessment routes answer with.
type AssessmentView struct {
	Project string `json:"project"`
	// Assessment is null until the project has been assessed.
	Assessment *assess.Assessment `json:"assessment"`
	// Source is "app" or "projects.yaml".
	Source     string             `json:"source,omitempty"`
	Suggestion *assess.Suggestion `json:"suggestion,omitempty"`
	// SuggestedKind is the kind to preselect, from the project's type.
	SuggestedKind string `json:"suggested_kind,omitempty"`
	// Confidential projects cannot use "Suggest answers from the repo".
	CanSuggest bool `json:"can_suggest"`
}

func view(p *Project) AssessmentView {
	v := AssessmentView{
		Project: p.ID, Assessment: p.Assessment, Source: p.AssessmentFrom,
		SuggestedKind: assess.KindForType(p.Type), CanSuggest: p.Visibility != "confidential" && p.LocalPath != "",
	}
	if p.Assessment != nil {
		s := assess.Suggest(*p.Assessment)
		v.Suggestion = &s
	}
	return v
}
