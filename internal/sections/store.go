package sections

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store keeps one JSON file per section in Dir (<DataDir>/sections).
type Store struct {
	Dir string
}

// ErrNotFound and ErrFull are mapped to 404 and 409.
var (
	ErrNotFound = errors.New("no such section")
	ErrFull     = fmt.Errorf("at most %d sections", MaxSections)
)

// Broken is a file in the folder that is not a valid section; it is never
// shown, only reported.
type Broken struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// Listing is the body of GET /api/sections.
type Listing struct {
	Sections []Section `json:"sections"`
	Broken   []Broken  `json:"broken"`
}

type dated struct {
	s  Section
	at time.Time
}

// List returns the valid sections, oldest first, so a new one lands last.
func (st *Store) List() Listing {
	out := Listing{Sections: []Section{}, Broken: []Broken{}}
	files, _ := filepath.Glob(filepath.Join(st.Dir, "*.json"))
	var ds []dated
	for _, f := range files {
		name := filepath.Base(f)
		data, err := os.ReadFile(f)
		if err != nil {
			out.Broken = append(out.Broken, Broken{name, err.Error()})
			continue
		}
		s, err := Check(data)
		if err == nil && s.ID+".json" != name {
			err = fmt.Errorf("its id %q does not match the file name", s.ID)
		}
		if err != nil {
			out.Broken = append(out.Broken, Broken{name, err.Error()})
			continue
		}
		info, _ := os.Stat(f)
		var at time.Time
		if info != nil {
			at = info.ModTime()
		}
		ds = append(ds, dated{s, at})
	}
	sort.SliceStable(ds, func(i, j int) bool {
		if !ds[i].at.Equal(ds[j].at) {
			return ds[i].at.Before(ds[j].at)
		}
		return ds[i].s.ID < ds[j].s.ID
	})
	for _, d := range ds {
		out.Sections = append(out.Sections, d.s)
	}
	return out
}

// Save validates s and writes it. Replacing a section keeps its place.
func (st *Store) Save(s Section) (Section, error) {
	s = Normalise(s)
	if err := Validate(s); err != nil {
		return s, err
	}
	path := filepath.Join(st.Dir, s.ID+".json")
	_, statErr := os.Stat(path)
	if statErr != nil && len(st.List().Sections) >= MaxSections {
		return s, ErrFull
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return s, err
	}
	if err := os.MkdirAll(st.Dir, 0o755); err != nil {
		return s, err
	}
	var keep time.Time
	if info, err := os.Stat(path); err == nil {
		keep = info.ModTime()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return s, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return s, err
	}
	if !keep.IsZero() {
		_ = os.Chtimes(path, keep, keep)
	}
	return s, nil
}

// Delete removes a section.
func (st *Store) Delete(id string) error {
	if !idRE.MatchString(id) {
		return ErrNotFound
	}
	err := os.Remove(filepath.Join(st.Dir, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// FreeID returns base, or base-2, base-3… when a section already has it.
func (st *Store) FreeID(base string) string {
	base = strings.Trim(base, "-")
	if !idRE.MatchString(base) {
		base = "section"
	}
	if len(base) > 44 {
		base = strings.TrimRight(base[:44], "-")
	}
	id := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(st.Dir, id+".json")); errors.Is(err, os.ErrNotExist) {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}
