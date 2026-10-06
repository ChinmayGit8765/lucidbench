// Package themes holds the UI themes: five built-in presets plus the user's
// own, stored as <data dir>/themes/<id>/theme.json with optional image assets
// in the same folder.
//
// A theme is data, never code. Its tokens may only set the UI's existing CSS
// variables, and each value is checked against the kind of that variable
// (colour, shadow, length or font stack): no url(), no expression(), no
// arbitrary CSS. SVG assets are sanitised before they are written.
package themes

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Version is the theme schema version.
const Version = 1

// Theme is one theme.
type Theme struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     int               `json:"version"`
	Base        string            `json:"base"` // dark | light
	Description string            `json:"description,omitempty"`
	Tokens      map[string]string `json:"tokens"`
	Fonts       *Fonts            `json:"fonts,omitempty"`
	Art         *Art              `json:"art,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`

	// BuiltIn is set on the presets in API responses; it is never stored.
	BuiltIn bool `json:"builtin"`
}

// Fonts are optional font stacks. Only installed fonts can be named; a theme
// cannot load a web font.
type Fonts struct {
	Sans string `json:"sans,omitempty"`
	Mono string `json:"mono,omitempty"`
}

// Art names image files in the theme's folder for the UI's art slots.
type Art struct {
	HeaderImage   string   `json:"headerImage,omitempty"`
	SidebarMascot string   `json:"sidebarMascot,omitempty"`
	EmptyState    string   `json:"emptyState,omitempty"`
	SpriteBoard   []Sprite `json:"spriteBoard,omitempty"`
	// Sprites are the state sprites, keyed by slot (see SpriteSlots): the
	// art the UI shows while loading, working, thinking and so on.
	Sprites map[string]string `json:"sprites,omitempty"`
}

// SpriteSlots are the state sprites a theme may set, in display order,
// with where the UI shows each.
var SpriteSlots = []struct{ Slot, Where string }{
	{"loading", "loading placeholders"},
	{"working", "a Work session running, and the mascot while one runs"},
	{"thinking", "the Council deliberating"},
	{"success", "a CI run that passed"},
	{"failure", "a CI run that failed"},
	{"sleeping", "infrastructure asleep after Sleep everything"},
	{"empty", "empty boards and lists"},
	{"celebrate", "a card reaching Done or a PR merged"},
}

// ValidSlot reports whether s is a state sprite slot.
func ValidSlot(s string) bool {
	for _, x := range SpriteSlots {
		if x.Slot == s {
			return true
		}
	}
	return false
}

// Sprite is one tile of the Overview sprite board.
type Sprite struct {
	File    string `json:"file"`
	Caption string `json:"caption,omitempty"`
}

// Files returns every file the art references.
func (a *Art) Files() []string {
	if a == nil {
		return nil
	}
	var out []string
	for _, f := range []string{a.HeaderImage, a.SidebarMascot, a.EmptyState} {
		if f != "" {
			out = append(out, f)
		}
	}
	for _, s := range a.SpriteBoard {
		out = append(out, s.File)
	}
	for _, x := range SpriteSlots {
		if f := a.Sprites[x.Slot]; f != "" {
			out = append(out, f)
		}
	}
	return out
}

// Kind is the kind of value a token holds.
type Kind int

// Token kinds.
const (
	KindColor Kind = iota
	KindShadow
	KindLength
)

// Tokens are the CSS variables a theme may set, with the kind of each. They
// are exactly the design tokens in web/src/index.css.
var Tokens = func() map[string]Kind {
	m := map[string]Kind{"--radius": KindLength, "--shadow-card": KindShadow, "--shadow-pop": KindShadow}
	for _, n := range []string{
		"background", "sidebar", "foreground", "card", "card-foreground", "elevated",
		"primary", "primary-foreground", "secondary", "secondary-foreground",
		"muted", "muted-foreground", "subtle-foreground", "accent", "accent-foreground",
		"destructive", "border", "border-strong", "input", "ring",
		"brand", "brand-soft", "brand-fg", "brand-2", "glow-1", "glow-2",
		"tint-claude", "tint-codex", "tint-grok", "tint-cursor",
	} {
		m["--"+n] = KindColor
	}
	for _, s := range []string{"success", "warning", "danger", "info", "neutral"} {
		m["--"+s] = KindColor
		m["--"+s+"-soft"] = KindColor
		m["--"+s+"-fg"] = KindColor
	}
	return m
}()

// Labels are the UI strings a theme may rename, with what they default to.
var Labels = map[string]string{
	"overview_title": "the Overview greeting",
	"attention":      "Needs attention",
	"all_clear":      "All clear",
	"sprite_board":   "Sprite board",
	"usage_meter":    "Usage (the live resource meters)",
}

var (
	idRE      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)
	fileRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,59}\.(svg|png|webp|gif)$`)
	valueRE   = regexp.MustCompile(`^[A-Za-z0-9#.,%/ ()+\-]{1,160}$`)
	hexRE     = regexp.MustCompile(`#[0-9A-Fa-f]{3,8}\b`)
	varRE     = regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)\s*\)`)
	wordRE    = regexp.MustCompile(`[A-Za-z][A-Za-z-]*`)
	lengthRE  = regexp.MustCompile(`^(0|\d{1,3}(\.\d{1,3})?(px|rem|em))$`)
	fontRE    = regexp.MustCompile(`^[A-Za-z0-9 ,"'\-]{1,200}$`)
	controlRE = regexp.MustCompile(`[\x00-\x1f\x7f<>{}]`)
)

// words that may appear in colour and shadow values: colour functions and
// spaces, a few keywords, and units.
var words = map[string]bool{
	"oklch": true, "oklab": true, "rgb": true, "rgba": true, "hsl": true, "hsla": true,
	"lab": true, "lch": true, "color-mix": true, "in": true, "srgb": true, "srgb-linear": true,
	"display-p3": true, "transparent": true, "white": true, "black": true, "currentcolor": true,
	"deg": true, "turn": true, "rad": true, "px": true, "rem": true, "em": true, "inset": true, "none": true,
}

// ValidID reports whether id is a valid theme id.
func ValidID(id string) bool { return idRE.MatchString(id) }

// ValidFile reports whether name is a valid asset file name: lowercase,
// no path separators, and an image extension.
func ValidFile(name string) bool { return fileRE.MatchString(name) }

// ValidValue reports whether v is a safe value for a token of kind k.
func ValidValue(k Kind, v string) bool {
	v = strings.TrimSpace(v)
	switch k {
	case KindLength:
		return lengthRE.MatchString(v)
	case KindColor, KindShadow:
		if !valueRE.MatchString(v) || strings.Count(v, "(") != strings.Count(v, ")") {
			return false
		}
		rest := hexRE.ReplaceAllString(v, " ")
		for _, m := range varRE.FindAllStringSubmatch(rest, -1) {
			if _, ok := Tokens[m[1]]; !ok {
				return false
			}
		}
		rest = varRE.ReplaceAllString(rest, " ")
		if strings.Contains(rest, "--") {
			return false
		}
		for _, w := range wordRE.FindAllString(rest, -1) {
			if !words[strings.ToLower(w)] {
				return false
			}
		}
		return true
	}
	return false
}

// ValidColor reports whether v is a safe colour value.
func ValidColor(v string) bool { return ValidValue(KindColor, v) }

// ValidFont reports whether v is a safe font stack.
func ValidFont(v string) bool {
	return fontRE.MatchString(v) && !strings.Contains(strings.ToLower(v), "url")
}

// tokenName normalises a token key to its --name form.
func tokenName(k string) string {
	k = strings.TrimSpace(k)
	if !strings.HasPrefix(k, "--") {
		k = "--" + k
	}
	return k
}

// Validate checks a theme strictly; the first problem is returned.
func (t *Theme) Validate() error {
	if !ValidID(t.ID) {
		return fmt.Errorf("id: must be lowercase letters, digits and dashes, up to 48 characters")
	}
	if n := strings.TrimSpace(t.Name); n == "" || len(n) > 60 || controlRE.MatchString(n) {
		return fmt.Errorf("name: must be 1-60 characters of plain text")
	}
	if t.Version != Version {
		return fmt.Errorf("version: must be %d", Version)
	}
	if t.Base != "dark" && t.Base != "light" {
		return fmt.Errorf("base: must be dark or light")
	}
	if len(t.Description) > 200 || controlRE.MatchString(t.Description) {
		return fmt.Errorf("description: must be at most 200 characters of plain text")
	}
	for k, v := range t.Tokens {
		kind, ok := Tokens[tokenName(k)]
		if !ok {
			return fmt.Errorf("tokens: %q is not a theme token", k)
		}
		if !ValidValue(kind, v) {
			return fmt.Errorf("tokens: %s: value is not an allowed %s", k, kindName(kind))
		}
	}
	if t.Fonts != nil {
		for _, f := range []string{t.Fonts.Sans, t.Fonts.Mono} {
			if f != "" && !ValidFont(f) {
				return fmt.Errorf("fonts: %q is not a plain font stack", f)
			}
		}
	}
	if t.Art != nil {
		if len(t.Art.SpriteBoard) > 12 {
			return fmt.Errorf("art.spriteBoard: at most 12 sprites")
		}
		for slot, f := range t.Art.Sprites {
			if !ValidSlot(slot) {
				return fmt.Errorf("art.sprites: %q is not a sprite slot (loading, working, thinking, success, failure, sleeping, empty, celebrate)", slot)
			}
			if f == "" {
				return fmt.Errorf("art.sprites: %s: names no file", slot)
			}
		}
		for _, f := range t.Art.Files() {
			if !ValidFile(f) {
				return fmt.Errorf("art: %q must be a lowercase file name ending in .svg, .png, .webp or .gif", f)
			}
		}
		for _, s := range t.Art.SpriteBoard {
			if len(s.Caption) > 40 || controlRE.MatchString(s.Caption) {
				return fmt.Errorf("art.spriteBoard: captions must be at most 40 characters of plain text")
			}
		}
	}
	for k, v := range t.Labels {
		if _, ok := Labels[k]; !ok {
			return fmt.Errorf("labels: %q is not a label a theme can rename", k)
		}
		if v == "" || len(v) > 40 || controlRE.MatchString(v) {
			return fmt.Errorf("labels: %s: must be 1-40 characters of plain text", k)
		}
	}
	return nil
}

func kindName(k Kind) string {
	switch k {
	case KindShadow:
		return "shadow"
	case KindLength:
		return "length (px, rem or em)"
	}
	return "colour"
}

// Normalize writes every token key in --name form and trims values.
func (t *Theme) Normalize() {
	if t.Tokens == nil {
		t.Tokens = map[string]string{}
	}
	norm := make(map[string]string, len(t.Tokens))
	for k, v := range t.Tokens {
		norm[tokenName(k)] = strings.TrimSpace(v)
	}
	t.Tokens = norm
	t.Name = strings.TrimSpace(t.Name)
}

// Clean drops whatever would fail validation (unknown tokens, unsafe values,
// unknown labels, bad art references) and reports what it dropped. It is
// used on model output, where one bad token should not waste the whole
// generation; saved themes are validated strictly.
func (t *Theme) Clean(assets map[string]bool) []string {
	t.Normalize()
	var dropped []string
	keys := make([]string, 0, len(t.Tokens))
	for k := range t.Tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		kind, ok := Tokens[k]
		if !ok || !ValidValue(kind, t.Tokens[k]) {
			dropped = append(dropped, "token "+k)
			delete(t.Tokens, k)
		}
	}
	for k, v := range t.Labels {
		if _, ok := Labels[k]; !ok || v == "" || len(v) > 40 || controlRE.MatchString(v) {
			dropped = append(dropped, "label "+k)
			delete(t.Labels, k)
		}
	}
	if t.Fonts != nil {
		if t.Fonts.Sans != "" && !ValidFont(t.Fonts.Sans) {
			t.Fonts.Sans = ""
			dropped = append(dropped, "fonts.sans")
		}
		if t.Fonts.Mono != "" && !ValidFont(t.Fonts.Mono) {
			t.Fonts.Mono = ""
			dropped = append(dropped, "fonts.mono")
		}
	}
	if a := t.Art; a != nil {
		keep := func(f string) string {
			if f == "" || (ValidFile(f) && assets[f]) {
				return f
			}
			dropped = append(dropped, "art "+f)
			return ""
		}
		a.HeaderImage, a.SidebarMascot, a.EmptyState = keep(a.HeaderImage), keep(a.SidebarMascot), keep(a.EmptyState)
		var sprites []Sprite
		for _, s := range a.SpriteBoard {
			if s.File = keep(s.File); s.File != "" && len(sprites) < 12 {
				if len(s.Caption) > 40 || controlRE.MatchString(s.Caption) {
					s.Caption = ""
				}
				sprites = append(sprites, s)
			}
		}
		a.SpriteBoard = sprites
		for slot, f := range a.Sprites {
			if !ValidSlot(slot) {
				dropped = append(dropped, "sprite "+slot)
				delete(a.Sprites, slot)
			} else if keep(f) == "" {
				delete(a.Sprites, slot)
			}
		}
		if len(a.Sprites) == 0 {
			a.Sprites = nil
		}
	}
	if len(t.Description) > 200 || controlRE.MatchString(t.Description) {
		t.Description = ""
	}
	return dropped
}
