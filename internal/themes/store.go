package themes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxRaster is the largest PNG, WebP or GIF asset accepted, in bytes.
const MaxRaster = 1 << 20

// MaxAssets is the most asset files one theme may carry: the three single
// slots, a full sprite board and every state sprite, with room to spare.
const MaxAssets = 32

// Bundle is a theme with its assets inline: the body of POST /api/themes,
// the export format, and the preview a generation returns. SVG assets are
// SVG text; PNG, WebP and GIF assets are base64.
type Bundle struct {
	Theme  Theme             `json:"theme"`
	Assets map[string]string `json:"assets,omitempty"`
}

// Errors returned by the store.
var (
	ErrNotFound = errors.New("no such theme")
	ErrBuiltIn  = errors.New("built-in themes cannot be changed or deleted")
)

// Store keeps user themes under Dir, one folder per theme.
type Store struct {
	Dir string
}

// Listing is the body of GET /api/themes.
type Listing struct {
	Themes []Theme  `json:"themes"`
	Errors []string `json:"errors"`
}

// List returns the presets followed by the user's themes, by name. A user
// theme that fails validation is skipped and reported.
func (s *Store) List() Listing {
	out := Listing{Themes: []Theme{}, Errors: []string{}}
	for _, p := range Presets {
		p.BuiltIn = true
		out.Themes = append(out.Themes, p)
	}
	ents, err := os.ReadDir(s.Dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			out.Errors = append(out.Errors, "themes folder: "+err.Error())
		}
		return out
	}
	var user []Theme
	for _, e := range ents {
		if !e.IsDir() || !ValidID(e.Name()) || IsPreset(e.Name()) {
			continue
		}
		t, err := s.read(e.Name())
		if err != nil {
			out.Errors = append(out.Errors, e.Name()+": "+err.Error())
			continue
		}
		user = append(user, t)
	}
	sort.Slice(user, func(a, b int) bool { return strings.ToLower(user[a].Name) < strings.ToLower(user[b].Name) })
	out.Themes = append(out.Themes, user...)
	return out
}

func (s *Store) read(id string) (Theme, error) {
	var t Theme
	b, err := os.ReadFile(filepath.Join(s.Dir, id, "theme.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return t, ErrNotFound
		}
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("theme.json: %w", err)
	}
	t.Normalize()
	if t.ID != id {
		return t, fmt.Errorf("theme.json: id %q does not match its folder", t.ID)
	}
	if err := t.Validate(); err != nil {
		return t, err
	}
	t.BuiltIn = false
	return t, nil
}

// Get returns one theme, preset or user.
func (s *Store) Get(id string) (Theme, error) {
	for _, p := range Presets {
		if p.ID == id {
			p.BuiltIn = true
			return p, nil
		}
	}
	if !ValidID(id) {
		return Theme{}, ErrNotFound
	}
	return s.read(id)
}

// AssetPath returns the path of one asset of a user theme, or ErrNotFound.
// The file name must be a plain lowercase image name; the resolved path must
// stay inside the theme's folder and be a regular file, not a link.
func (s *Store) AssetPath(id, file string) (string, error) {
	if !ValidID(id) || IsPreset(id) || !ValidFile(file) || filepath.Base(file) != file {
		return "", ErrNotFound
	}
	dir, err := filepath.Abs(filepath.Join(s.Dir, id))
	if err != nil {
		return "", ErrNotFound
	}
	p := filepath.Join(dir, file)
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel != file || strings.HasPrefix(rel, "..") {
		return "", ErrNotFound
	}
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() {
		return "", ErrNotFound
	}
	return p, nil
}

// Export returns a theme with its assets inline.
func (s *Store) Export(id string) (Bundle, error) {
	t, err := s.Get(id)
	if err != nil {
		return Bundle{}, err
	}
	b := Bundle{Theme: t, Assets: map[string]string{}}
	if t.BuiltIn || t.Art == nil {
		return b, nil
	}
	for _, f := range t.Art.Files() {
		p, err := s.AssetPath(id, f)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.HasSuffix(f, ".svg") {
			b.Assets[f] = string(data)
		} else {
			b.Assets[f] = base64.StdEncoding.EncodeToString(data)
		}
	}
	return b, nil
}

var magic = map[string]func([]byte) bool{
	".png": func(b []byte) bool { return bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) },
	".gif": func(b []byte) bool {
		return bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a"))
	},
	".webp": func(b []byte) bool { return len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" },
}

// PrepareAssets validates and decodes inline assets: SVGs are sanitised,
// rasters are base64-decoded and checked by their magic bytes.
func PrepareAssets(in map[string]string) (map[string][]byte, error) {
	if len(in) > MaxAssets {
		return nil, fmt.Errorf("assets: at most %d files", MaxAssets)
	}
	out := map[string][]byte{}
	for name, data := range in {
		if !ValidFile(name) {
			return nil, fmt.Errorf("assets: %q must be a lowercase file name ending in .svg, .png, .webp or .gif", name)
		}
		ext := filepath.Ext(name)
		if ext == ".svg" {
			clean, err := SanitizeSVG([]byte(data))
			if err != nil {
				return nil, fmt.Errorf("assets: %s: %w", name, err)
			}
			out[name] = clean
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, fmt.Errorf("assets: %s: must be base64", name)
		}
		if len(raw) > MaxRaster {
			return nil, fmt.Errorf("assets: %s: larger than %d KB", name, MaxRaster>>10)
		}
		if !magic[ext](raw) {
			return nil, fmt.Errorf("assets: %s: content is not a %s image", name, ext[1:])
		}
		out[name] = raw
	}
	return out, nil
}

// Save validates a bundle and writes it as a user theme, replacing any theme
// with the same id. The folder is written in full next to the old one and
// swapped in by rename, so a failure leaves the previous theme intact.
func (s *Store) Save(b Bundle) (Theme, error) {
	t := b.Theme
	t.Normalize()
	t.BuiltIn = false
	if IsPreset(t.ID) {
		return t, ErrBuiltIn
	}
	if err := t.Validate(); err != nil {
		return t, err
	}
	files, err := PrepareAssets(b.Assets)
	if err != nil {
		return t, err
	}
	final := filepath.Join(s.Dir, t.ID)
	// Art may reference files already saved with the theme.
	for _, f := range t.Art.Files() {
		if _, ok := files[f]; ok {
			continue
		}
		p, err := s.AssetPath(t.ID, f)
		if err != nil {
			return t, fmt.Errorf("art: %s is neither in the upload nor saved with the theme", f)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return t, err
		}
		files[f] = data
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return t, err
	}
	stage, err := os.MkdirTemp(s.Dir, ".stage-"+t.ID+"-")
	if err != nil {
		return t, err
	}
	defer os.RemoveAll(stage)
	body, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return t, err
	}
	if err := os.WriteFile(filepath.Join(stage, "theme.json"), append(body, '\n'), 0o644); err != nil {
		return t, err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(stage, name), data, 0o644); err != nil {
			return t, err
		}
	}
	old := ""
	if _, err := os.Stat(final); err == nil {
		old = stage + ".old"
		if err := os.Rename(final, old); err != nil {
			return t, err
		}
	}
	if err := os.Rename(stage, final); err != nil {
		if old != "" {
			_ = os.Rename(old, final)
		}
		return t, err
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}
	return t, nil
}

// Delete removes a user theme.
func (s *Store) Delete(id string) error {
	if IsPreset(id) {
		return ErrBuiltIn
	}
	if !ValidID(id) {
		return ErrNotFound
	}
	dir := filepath.Join(s.Dir, id)
	if _, err := os.Stat(filepath.Join(dir, "theme.json")); err != nil {
		return ErrNotFound
	}
	return os.RemoveAll(dir)
}
