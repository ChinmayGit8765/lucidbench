package themes

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const goodSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><circle cx="5" cy="5" r="4" fill="url(#g)"/></svg>`

func sample(id string) Theme {
	return Theme{
		ID: id, Name: "Ember", Version: 1, Base: "dark",
		Tokens: map[string]string{"--brand": "oklch(0.75 0.17 50)", "background": "#101014"},
		Art:    &Art{HeaderImage: "banner.svg", SpriteBoard: []Sprite{{File: "orb.svg", Caption: "Charge"}}},
		Labels: map[string]string{"usage_meter": "Power level"},
	}
}

func TestPresetsAreValid(t *testing.T) {
	if len(Presets) != 5 {
		t.Fatalf("want 5 presets, got %d", len(Presets))
	}
	for _, p := range Presets {
		p.Normalize()
		if err := p.Validate(); err != nil {
			t.Errorf("preset %s: %v", p.ID, err)
		}
	}
}

func TestTokenAllowlistAndValues(t *testing.T) {
	ok := []struct {
		k Kind
		v string
	}{
		{KindColor, "oklch(0.72 0.14 245 / 13%)"},
		{KindColor, "#3b82f6"},
		{KindColor, "rgb(10, 20, 30)"},
		{KindColor, "color-mix(in oklch, var(--brand) 13%, transparent)"},
		{KindShadow, "0 1px 0 oklch(1 0 0 / 3%) inset, 0 1px 2px oklch(0 0 0 / 30%)"},
		{KindLength, "0.625rem"},
		{KindLength, "8px"},
	}
	for _, c := range ok {
		if !ValidValue(c.k, c.v) {
			t.Errorf("rejected %q", c.v)
		}
	}
	bad := []struct {
		k Kind
		v string
	}{
		{KindColor, "url(https://evil.example/x.png)"},
		{KindColor, "expression(alert(1))"},
		{KindColor, "red; background: url(x)"},
		{KindColor, "var(--not-a-token)"},
		{KindColor, "javascript:alert(1)"},
		{KindColor, "oklch(0.7 0.1 250"},
		{KindColor, "</style><script>"},
		{KindColor, "@import 'x'"},
		{KindColor, "blue"},
		{KindShadow, "0 0 2px red !important"},
		{KindLength, "calc(100vh)"},
		{KindLength, "8vw"},
		{KindColor, ""},
	}
	for _, c := range bad {
		if ValidValue(c.k, c.v) {
			t.Errorf("accepted %q", c.v)
		}
	}
	th := sample("ember")
	th.Tokens["--font-sans"] = "Arial"
	if err := th.Validate(); err == nil || !strings.Contains(err.Error(), "not a theme token") {
		t.Errorf("unknown token: %v", err)
	}
	th = sample("ember")
	th.Labels["sidebar"] = "x"
	if err := th.Validate(); err == nil {
		t.Error("unknown label accepted")
	}
	th = sample("ember")
	th.Fonts = &Fonts{Sans: `x; src: url(evil)`}
	if err := th.Validate(); err == nil {
		t.Error("unsafe font accepted")
	}
}

func TestCleanDropsOnlyTheBadParts(t *testing.T) {
	th := sample("ember")
	th.Tokens["--brand-2"] = "url(x)"
	th.Tokens["--made-up"] = "#fff"
	th.Art.EmptyState = "missing.svg"
	dropped := th.Clean(map[string]bool{"banner.svg": true, "orb.svg": true})
	if len(dropped) != 3 {
		t.Errorf("dropped = %v", dropped)
	}
	if err := th.Validate(); err != nil {
		t.Errorf("cleaned theme invalid: %v", err)
	}
	if th.Tokens["--background"] != "#101014" {
		t.Error("token key not normalised")
	}
}

func TestSanitizeSVG(t *testing.T) {
	in := `<?xml version="1.0"?>
<!DOCTYPE svg [<!ENTITY x "boom">]>
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 10 10" onload="alert(1)">
  <!-- comment -->
  <script>alert(1)</script>
  <foreignObject><div xmlns="http://www.w3.org/1999/xhtml">hi</div></foreignObject>
  <image href="https://evil.example/x.png"/>
  <a href="javascript:alert(1)"><rect width="1" height="1"/></a>
  <style>@import url(https://evil.example/x.css);</style>
  <animate attributeName="href" to="javascript:alert(1)"/>
  <set attributeName="onclick" to="alert(1)"/>
  <defs><radialGradient id="g"><stop offset="0" stop-color="#f90"/></radialGradient></defs>
  <use xlink:href="#g"/>
  <use href="https://evil.example/sprite.svg#a"/>
  <circle cx="5" cy="5" r="4" fill="url(#g)" onclick="alert(1)" style="fill: url(https://evil.example/)"/>
  <path d="M0 0L10 10" stroke="url(javascript:alert(1))" filter="url(#g)"/>
  <text x="1" y="9">Ki &amp; aura</text>
</svg>`
	out, err := SanitizeSVG([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"DOCTYPE", "ENTITY", "boom", "script", "alert", "foreignObject", "<image", "<a ", "<style", "evil.example", "<animate", "<set", "onload", "onclick", "comment", "<?xml", "xlink"} {
		if strings.Contains(s, bad) {
			t.Errorf("sanitised SVG still contains %q:\n%s", bad, s)
		}
	}
	for _, good := range []string{`<use href="#g">`, `fill="url(#g)"`, `filter="url(#g)"`, `Ki &amp; aura`, `<radialGradient id="g">`, `xmlns="http://www.w3.org/2000/svg"`} {
		if !strings.Contains(s, good) {
			t.Errorf("sanitised SVG lost %q:\n%s", good, s)
		}
	}
	// A custom entity is never expanded: strict parsing refuses it.
	if out, err := SanitizeSVG([]byte(`<!DOCTYPE svg [<!ENTITY x "boom">]><svg>&x;</svg>`)); err == nil {
		t.Errorf("entity expanded: %s", out)
	}
	for _, bad := range []string{`<html/>`, `not xml`, `<svg><svg></svg>`, `<svg/><svg/>`} {
		if _, err := SanitizeSVG([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := SanitizeSVG(bytes.Repeat([]byte(" "), MaxSVG+1)); err == nil {
		t.Error("oversized SVG accepted")
	}
}

func bundle(id string) Bundle {
	return Bundle{Theme: sample(id), Assets: map[string]string{"banner.svg": goodSVG, "orb.svg": goodSVG}}
}

func TestSaveListExportDelete(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.Save(bundle("ember")); err != nil {
		t.Fatal(err)
	}
	l := s.List()
	if len(l.Themes) != 6 || l.Themes[5].ID != "ember" || l.Themes[5].BuiltIn || !l.Themes[0].BuiltIn || len(l.Errors) != 0 {
		t.Fatalf("list = %+v", l)
	}
	ex, err := s.Export("ember")
	if err != nil || len(ex.Assets) != 2 || !strings.HasPrefix(ex.Assets["orb.svg"], "<svg") {
		t.Fatalf("export = %+v, %v", ex, err)
	}
	// Re-saving without assets keeps the files already saved.
	if _, err := s.Save(Bundle{Theme: sample("ember")}); err != nil {
		t.Fatalf("resave without assets: %v", err)
	}
	if _, err := s.Save(Bundle{Theme: sample("fresh")}); err == nil {
		t.Error("art without files accepted")
	}
	png := base64.StdEncoding.EncodeToString(append([]byte("\x89PNG\r\n\x1a\n"), 0, 0))
	b := bundle("ember")
	b.Assets["fake.png"] = base64.StdEncoding.EncodeToString([]byte("<svg onload=x>"))
	if _, err := s.Save(b); err == nil {
		t.Error("non-PNG content accepted as .png")
	}
	b.Assets["fake.png"] = png
	if _, err := s.Save(b); err != nil {
		t.Errorf("real PNG header rejected: %v", err)
	}
	if err := s.Delete("midnight"); !errors.Is(err, ErrBuiltIn) {
		t.Errorf("delete preset = %v", err)
	}
	if _, err := s.Save(Bundle{Theme: Theme{ID: "paper", Name: "x", Version: 1, Base: "light"}}); !errors.Is(err, ErrBuiltIn) {
		t.Errorf("overwrite preset = %v", err)
	}
	if err := s.Delete("ember"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("ember"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
}

func TestFailedSaveKeepsPreviousTheme(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.Save(bundle("ember")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.Dir, "ember", "theme.json"))
	b := bundle("ember")
	b.Theme.Tokens["--brand"] = "url(evil)"
	if _, err := s.Save(b); err == nil {
		t.Fatal("bad token accepted")
	}
	b = bundle("ember")
	b.Assets["orb.svg"] = "<html/>"
	if _, err := s.Save(b); err == nil {
		t.Fatal("bad svg accepted")
	}
	after, _ := os.ReadFile(filepath.Join(s.Dir, "ember", "theme.json"))
	if !bytes.Equal(before, after) {
		t.Error("failed save changed theme.json")
	}
	ents, _ := os.ReadDir(s.Dir)
	if len(ents) != 1 {
		t.Errorf("stray entries left behind: %v", ents)
	}
}

func TestAssetPathTraversal(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := s.Save(bundle("ember")); err != nil {
		t.Fatal(err)
	}
	// A file next to the themes folder that traversal would reach.
	if err := os.WriteFile(filepath.Join(filepath.Dir(s.Dir), "secret.svg"), []byte(goodSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "ember", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := s.AssetPath("ember", "orb.svg"); err != nil || filepath.Base(p) != "orb.svg" {
		t.Fatalf("good asset: %q %v", p, err)
	}
	for _, c := range [][2]string{
		{"ember", "../secret.svg"},
		{"ember", `..\secret.svg`},
		{"ember", "..%2Fsecret.svg"},
		{"..", "secret.svg"},
		{"ember/..", "secret.svg"},
		{"ember", "C:secret.svg"},
		{"ember", `C:\secret.svg`},
		{"ember", "/etc/passwd"},
		{"ember", "orb.svg:stream"},
		{"ember", "ORB.SVG"},
		{"ember", "notes.txt"},
		{"ember", "theme.json"},
		{"ember", "missing.svg"},
		{"midnight", "orb.svg"},
		{"ember", ""},
	} {
		if p, err := s.AssetPath(c[0], c[1]); err == nil {
			t.Errorf("AssetPath(%q, %q) = %q, want refusal", c[0], c[1], p)
		}
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(s.Dir, "ember", "link.svg")
		if err := os.Symlink(filepath.Join(filepath.Dir(s.Dir), "secret.svg"), link); err == nil {
			if _, err := s.AssetPath("ember", "link.svg"); err == nil {
				t.Error("symlink followed")
			}
		}
	}
}

func TestHTTP(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	mux := http.NewServeMux()
	Register(mux, s)
	body, _ := json.Marshal(bundle("ember"))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/themes", bytes.NewReader(body)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST without confirm = %d", rec.Code)
	}
	req := httptest.NewRequest("POST", "/api/themes", bytes.NewReader(body))
	req.Header.Set("X-Lucid-Confirm", "yes")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/themes/ember/assets/orb.svg", nil))
	h := rec.Header()
	if rec.Code != 200 || h.Get("Content-Type") != "image/svg+xml" || h.Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("asset = %d %v", rec.Code, h)
	}
	for _, p := range []string{
		"/api/themes/ember/assets/..%2F..%2Fsecret.svg",
		"/api/themes/ember/assets/..%5Csecret.svg",
		"/api/themes/ember/assets/theme.json",
		"/api/themes/..%2Fember/assets/orb.svg",
	} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}

	req = httptest.NewRequest("DELETE", "/api/themes/midnight", nil)
	req.Header.Set("X-Lucid-Confirm", "yes")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("DELETE preset = %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/api/themes", strings.NewReader(`{"theme":{},"script":"x"}`))
	req.Header.Set("X-Lucid-Confirm", "yes")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field = %d", rec.Code)
	}
}
