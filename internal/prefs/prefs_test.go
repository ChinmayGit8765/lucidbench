package prefs

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func store(t *testing.T) *Store {
	return &Store{Path: filepath.Join(t.TempDir(), "ui.json"), LegacyTheme: "light"}
}

func TestDefaultsFollowLegacyTheme(t *testing.T) {
	for legacy, want := range map[string]string{"dark": "midnight", "light": "daylight", "system": "system", "": "midnight"} {
		if got := Default(legacy).Theme; got != want {
			t.Errorf("Default(%q).Theme = %q, want %q", legacy, got, want)
		}
	}
	p, err := store(t).Load()
	if err != nil || p.Theme != "daylight" || p.Modules == nil || p.Extensions == nil {
		t.Fatalf("missing file: %+v, %v", p, err)
	}
}

func TestValidate(t *testing.T) {
	r := func(n int) *int { return &n }
	good := Prefs{
		Theme:      "my-theme",
		Overrides:  Overrides{Accent: "oklch(0.7 0.15 250)", Density: "comfortable", Radius: r(12), Font: "mono", Sidebar: "rail"},
		Modules:    map[string]ModulePref{"overview": {Order: 0}},
		Extensions: map[string]ExtensionPref{"containers": {Added: true, Order: 2}},
	}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Prefs){
		"theme":      func(p *Prefs) { p.Theme = "../x" },
		"accent url": func(p *Prefs) { p.Overrides.Accent = "url(x)" },
		"density":    func(p *Prefs) { p.Overrides.Density = "huge" },
		"radius":     func(p *Prefs) { p.Overrides.Radius = r(40) },
		"font":       func(p *Prefs) { p.Overrides.Font = "comic" },
		"sidebar":    func(p *Prefs) { p.Overrides.Sidebar = "left" },
		"module id":  func(p *Prefs) { p.Modules["Bad Id"] = ModulePref{} },
		"ext order":  func(p *Prefs) { p.Extensions["x"] = ExtensionPref{Order: -1} },
	} {
		p := good
		p.Modules = map[string]ModulePref{}
		p.Extensions = map[string]ExtensionPref{}
		mut(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSaveIsAtomic(t *testing.T) {
	s := store(t)
	if err := s.Save(Prefs{Theme: "aurora", SpriteBoard: true}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path)
	if err := s.Save(Prefs{Theme: "aurora", Overrides: Overrides{Density: "huge"}}); err == nil {
		t.Fatal("invalid prefs saved")
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(before, after) {
		t.Error("failed save changed ui.json")
	}
	ents, _ := os.ReadDir(filepath.Dir(s.Path))
	if len(ents) != 1 {
		t.Errorf("temp files left behind: %v", ents)
	}
	p, err := s.Load()
	if err != nil || p.Theme != "aurora" || !p.SpriteBoard {
		t.Errorf("load = %+v, %v", p, err)
	}
	// Writing over an existing file replaces it in one step.
	if err := s.Save(Prefs{Theme: "paper"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Load(); p.Theme != "paper" {
		t.Errorf("theme = %q", p.Theme)
	}
}

func TestHTTP(t *testing.T) {
	s := store(t)
	h := Handler(s)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("PUT", "/api/prefs", strings.NewReader(`{"theme":"paper"}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PUT without confirm = %d", rec.Code)
	}
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/api/prefs", strings.NewReader(body))
		req.Header.Set("X-Lucid-Confirm", "yes")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := put(`{"theme":"paper","extensions":{"containers":{"added":false,"order":3}}}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"added": false`) && !strings.Contains(rec.Body.String(), `"added":false`) {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body)
	}
	if rec := put(`{"theme":"paper","evil":1}`); rec.Code != 400 {
		t.Errorf("unknown field = %d", rec.Code)
	}
	if rec := put(`{"overrides":{"accent":"red; x: url(y)"}}`); rec.Code != 400 {
		t.Errorf("bad accent = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/prefs", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"theme":"paper"`) {
		t.Errorf("GET = %d %s", rec.Code, rec.Body)
	}
}
