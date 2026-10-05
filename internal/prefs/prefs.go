// Package prefs stores the user's UI preferences in <data dir>/ui.json:
// the theme, appearance overrides, the order of core modules in the sidebar,
// and which extensions are added to it.
package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/ChinmayGit8765/lucidbench/internal/apiutil"
	"github.com/ChinmayGit8765/lucidbench/internal/themes"
)

// FileName is the prefs file name inside the data dir.
const FileName = "ui.json"

// Overrides adjust the active theme. Empty fields leave the theme's own value.
type Overrides struct {
	Accent  string `json:"accent,omitempty"`
	Density string `json:"density,omitempty"` // compact | comfortable
	Radius  *int   `json:"radius,omitempty"`  // 0-16 px
	Font    string `json:"font,omitempty"`    // inter | geist | system | mono
	Sidebar string `json:"sidebar,omitempty"` // expanded | rail
}

// ModulePref positions a core module in the sidebar.
type ModulePref struct {
	Order int `json:"order"`
}

// ExtensionPref says whether an extension is in the sidebar, and where.
type ExtensionPref struct {
	Added bool `json:"added"`
	Order int  `json:"order"`
}

// Prefs is the content of ui.json.
type Prefs struct {
	Theme       string                   `json:"theme"`
	Overrides   Overrides                `json:"overrides"`
	Modules     map[string]ModulePref    `json:"modules"`
	Extensions  map[string]ExtensionPref `json:"extensions"`
	SpriteBoard bool                     `json:"sprite_board"`
}

var (
	moduleIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	densities  = map[string]bool{"": true, "compact": true, "comfortable": true}
	fonts      = map[string]bool{"": true, "inter": true, "geist": true, "system": true, "mono": true}
	sidebars   = map[string]bool{"": true, "expanded": true, "rail": true}
)

// Validate checks every field; the first problem is returned.
func (p *Prefs) Validate() error {
	if p.Theme != "" && p.Theme != "system" && !themes.ValidID(p.Theme) {
		return errors.New("theme: must be a theme id")
	}
	o := p.Overrides
	if o.Accent != "" && !themes.ValidColor(o.Accent) {
		return errors.New("overrides.accent: must be a colour such as #3b82f6 or oklch(0.7 0.15 250)")
	}
	if !densities[o.Density] {
		return errors.New("overrides.density: must be compact or comfortable")
	}
	if o.Radius != nil && (*o.Radius < 0 || *o.Radius > 16) {
		return errors.New("overrides.radius: must be 0-16")
	}
	if !fonts[o.Font] {
		return errors.New("overrides.font: must be inter, geist, system or mono")
	}
	if !sidebars[o.Sidebar] {
		return errors.New("overrides.sidebar: must be expanded or rail")
	}
	if len(p.Modules) > 100 || len(p.Extensions) > 100 {
		return errors.New("too many modules")
	}
	for id, m := range p.Modules {
		if !moduleIDRE.MatchString(id) {
			return fmt.Errorf("modules: %q is not a module id", id)
		}
		if m.Order < 0 || m.Order > 1000 {
			return fmt.Errorf("modules: %s: order must be 0-1000", id)
		}
	}
	for id, e := range p.Extensions {
		if !moduleIDRE.MatchString(id) {
			return fmt.Errorf("extensions: %q is not an extension id", id)
		}
		if e.Order < 0 || e.Order > 1000 {
			return fmt.Errorf("extensions: %s: order must be 0-1000", id)
		}
	}
	return nil
}

// Default returns the prefs used before ui.json exists. legacyTheme is the
// config's ui.theme (dark, light or system).
func Default(legacyTheme string) Prefs {
	t := "midnight"
	switch legacyTheme {
	case "light":
		t = "daylight"
	case "system":
		t = "system"
	}
	return Prefs{Theme: t, Modules: map[string]ModulePref{}, Extensions: map[string]ExtensionPref{}}
}

// Store reads and writes ui.json.
type Store struct {
	Path        string
	LegacyTheme string
	mu          sync.Mutex
}

// Load returns the saved prefs, or the defaults when ui.json does not exist.
func (s *Store) Load() (Prefs, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(s.LegacyTheme), nil
	}
	if err != nil {
		return Prefs{}, err
	}
	p := Default(s.LegacyTheme)
	if err := json.Unmarshal(b, &p); err != nil {
		return Prefs{}, fmt.Errorf("%s: %w", FileName, err)
	}
	if p.Modules == nil {
		p.Modules = map[string]ModulePref{}
	}
	if p.Extensions == nil {
		p.Extensions = map[string]ExtensionPref{}
	}
	if err := p.Validate(); err != nil {
		return Prefs{}, fmt.Errorf("%s: %w", FileName, err)
	}
	return p, nil
}

// Save validates p and writes it atomically: to a temporary file in the same
// directory, then renamed over ui.json. An invalid p leaves the file as it
// was.
func (s *Store) Save(p Prefs) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Modules == nil {
		p.Modules = map[string]ModulePref{}
	}
	if p.Extensions == nil {
		p.Extensions = map[string]ExtensionPref{}
	}
	body, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return WriteAtomic(s.Path, append(body, '\n'))
}

// WriteAtomic writes data to path through a temporary file and a rename.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	if err := errors.Join(werr, serr, cerr); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Handler serves GET and PUT /api/prefs. PUT takes the full prefs object and
// needs X-Lucid-Confirm.
func Handler(s *Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p, err := s.Load()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			apiutil.WriteJSON(w, http.StatusOK, p)
		case http.MethodPut:
			if !apiutil.Confirmed(w, r) {
				return
			}
			var p Prefs
			dec := json.NewDecoder(io.LimitReader(r.Body, 256<<10))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&p); err != nil {
				http.Error(w, "invalid prefs JSON: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := s.Save(p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			p, _ = s.Load()
			apiutil.WriteJSON(w, http.StatusOK, p)
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
