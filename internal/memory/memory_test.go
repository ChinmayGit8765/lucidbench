package memory

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

func open(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func put(t *testing.T, v *Vault, rel, content string) {
	t.Helper()
	abs := filepath.Join(v.Root(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathSafety(t *testing.T) {
	v := open(t)
	outside := filepath.Join(t.TempDir(), "secret.md")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		"../secret.md", "a/../../secret.md", "..", "a/..", "/etc/passwd.md", "\\windows\\x.md",
		"C:/x.md", "c:x.md", "C:\\x.md", "a\\b.md", "//host/share/x.md", ".trash/x.md", "a/.hidden/x.md", "x.md:stream", "a\x00b.md",
		filepath.ToSlash(outside), // absolute
	}
	for _, p := range bad {
		if _, err := v.Read(p); !errors.Is(err, ErrBadPath) {
			t.Errorf("Read(%q) = %v, want ErrBadPath", p, err)
		}
		if err := v.Write(&Page{Path: p, Body: "x"}); !errors.Is(err, ErrBadPath) {
			t.Errorf("Write(%q) = %v, want ErrBadPath", p, err)
		}
		if err := v.Delete(p); !errors.Is(err, ErrBadPath) {
			t.Errorf("Delete(%q) = %v, want ErrBadPath", p, err)
		}
		if err := v.Move("ok.md", p); !errors.Is(err, ErrBadPath) {
			t.Errorf("Move to %q = %v, want ErrBadPath", p, err)
		}
		if _, err := v.List(p); !errors.Is(err, ErrBadPath) {
			t.Errorf("List(%q) = %v, want ErrBadPath", p, err)
		}
	}
	if b, _ := os.ReadFile(outside); string(b) != "secret" {
		t.Errorf("a file outside the vault changed: %q", b)
	}
	if err := v.Write(&Page{Path: "notes.txt", Body: "x"}); !errors.Is(err, ErrBadPath) {
		t.Errorf("non-Markdown write = %v", err)
	}
	if _, err := v.Read("missing.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing = %v", err)
	}
}

// linkDir makes name point at target: a symlink, or on Windows without the
// privilege for one, a junction.
func linkDir(target, name string) error {
	err := os.Symlink(target, name)
	if err != nil && runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", "mklink", "/J", name, target).Run()
	}
	return err
}

func TestSymlinkOutOfVaultIsRefused(t *testing.T) {
	v := open(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.md"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(outside, filepath.Join(v.Root(), "link")); err != nil {
		t.Skipf("cannot create links here: %v", err)
	}
	fileLink := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(v.Root(), "file.md")) == nil
	put(t, v, "real/inside.md", "ok")
	innerLink := os.Symlink(filepath.Join(v.Root(), "real"), filepath.Join(v.Root(), "alias")) == nil

	if _, err := v.Read("link/secret.md"); !errors.Is(err, ErrBadPath) {
		t.Errorf("read through folder link = %v", err)
	}
	if _, err := v.Read("file.md"); fileLink && !errors.Is(err, ErrBadPath) {
		t.Errorf("read through file link = %v", err)
	}
	if err := v.Write(&Page{Path: "link/new.md", Body: "x"}); !errors.Is(err, ErrBadPath) {
		t.Errorf("write through folder link = %v", err)
	}
	if err := v.Write(&Page{Path: "link/deeper/new.md", Body: "x"}); !errors.Is(err, ErrBadPath) {
		t.Errorf("write through folder link, new subfolder = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "new.md")); err == nil {
		t.Error("a page was written outside the vault")
	}
	// A symlink that stays inside is fine (junctions are always refused).
	if p, err := v.Read("alias/inside.md"); innerLink && (err != nil || p.Body != "ok") {
		t.Errorf("inner link = %v %v", p, err)
	}
	// And listings and search do not surface the outside.
	ents, err := v.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name == "link" || e.Name == "file.md" {
			t.Errorf("listing shows the outside link %q", e.Name)
		}
	}
	hits, _ := v.Search("secret", 10)
	if len(hits) != 0 {
		t.Errorf("search reached outside: %+v", hits)
	}
}

func TestWriteReadAtomic(t *testing.T) {
	v := open(t)
	p := &Page{Path: "Inbox/idea.md", Body: "# Idea\n\ntext\n", Front: map[string]any{"status": "draft"}}
	if err := v.Write(p); err != nil { // creates the folder
		t.Fatal(err)
	}
	got, err := v.Read("Inbox/idea.md")
	if err != nil || got.Body != p.Body || got.Front["status"] != "draft" || got.Title != "idea" {
		t.Fatalf("read back = %+v, %v", got, err)
	}
	// An overwrite replaces the content and leaves no temporary files behind.
	p.Body = "second\n"
	if err := v.Write(p); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(filepath.Join(v.Root(), "Inbox"))
	if len(ents) != 1 {
		t.Errorf("leftover files: %v", ents)
	}
	// A failed write (a folder is in the way) keeps the old content and
	// leaves no temporary file.
	if err := os.MkdirAll(filepath.Join(v.Root(), "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := v.Write(&Page{Path: "dir.md", Body: "x"}); err == nil {
		t.Error("writing over a folder succeeded")
	}
	if ents, _ := os.ReadDir(v.Root()); len(ents) != 2 { // Inbox, dir.md
		t.Errorf("root entries after failed write: %v", ents)
	}
	if err := v.Write(&Page{Path: "Inbox/idea.md/child.md", Body: "x"}); err == nil {
		t.Error("writing under a file succeeded")
	}
	if b, _ := os.ReadFile(filepath.Join(v.Root(), "Inbox", "idea.md")); !strings.Contains(string(b), "second") {
		t.Errorf("old content lost: %q", b)
	}
	if ents, _ := os.ReadDir(filepath.Join(v.Root(), "Inbox")); len(ents) != 1 {
		t.Errorf("leftover files after failed write: %v", ents)
	}
}

func TestFrontMatterRoundTrip(t *testing.T) {
	v := open(t)
	src := "---\r\ntitle: My Page\r\ntags: [a, b]\r\nkanban-plugin: basic\r\nnested:\r\n  deep: 1\r\n  list:\r\n    - x\r\ncreated: 2026-10-01\r\ncustom-unknown: {k: v}\r\n---\r\n# Body\r\n\r\n[[Other]]\r\n"
	put(t, v, "p.md", src)
	pg, err := v.Read("p.md")
	if err != nil {
		t.Fatal(err)
	}
	if pg.Title != "My Page" || pg.Body != "# Body\r\n\r\n[[Other]]\r\n" {
		t.Fatalf("page = %+v", pg)
	}
	if !reflect.DeepEqual(pg.Front["tags"], []any{"a", "b"}) || pg.Front["nested"].(map[string]any)["deep"] != 1 || pg.Front["created"] != "2026-10-01" {
		t.Errorf("front = %#v", pg.Front)
	}
	pg.Front["added"] = true
	if err := v.Write(pg); err != nil {
		t.Fatal(err)
	}
	again, err := v.Read("p.md")
	if err != nil {
		t.Fatal(err)
	}
	if again.Body != pg.Body || again.Title != "My Page" {
		t.Errorf("body or title changed: %+v", again)
	}
	want := pg.Front
	if !reflect.DeepEqual(again.Front, want) {
		t.Errorf("unknown keys not preserved:\n got %#v\nwant %#v", again.Front, want)
	}
	// Writing again does not change a byte.
	first, _ := os.ReadFile(filepath.Join(v.Root(), "p.md"))
	if err := v.Write(again); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(v.Root(), "p.md"))
	if string(first) != string(second) {
		t.Errorf("write is not stable:\n%s\n---\n%s", first, second)
	}
	// No front matter stays none; broken front matter stays in the body.
	put(t, v, "plain.md", "just text\n")
	if pl, _ := v.Read("plain.md"); len(pl.Front) != 0 || pl.Body != "just text\n" {
		t.Errorf("plain = %+v", pl)
	}
	if err := v.Write(&Page{Path: "plain2.md", Body: "just text\n"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(v.Root(), "plain2.md")); string(b) != "just text\n" {
		t.Errorf("plain2 = %q", b)
	}
	broken := "---\nkey: [unclosed\n---\nbody\n"
	put(t, v, "broken.md", broken)
	bp, _ := v.Read("broken.md")
	if err := v.Write(bp); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(v.Root(), "broken.md")); string(b) != broken {
		t.Errorf("broken front matter was rewritten: %q", b)
	}
	// A title that differs from the file name is stored.
	if err := v.Write(&Page{Path: "t.md", Title: "Fancy", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if tp, _ := v.Read("t.md"); tp.Title != "Fancy" || tp.Front["title"] != "Fancy" {
		t.Errorf("title = %+v", tp)
	}
}

func TestListMoveDeleteTrash(t *testing.T) {
	v := open(t)
	put(t, v, "b.md", "b")
	put(t, v, "A/one.md", "1")
	put(t, v, "a.md", "a")
	put(t, v, "image.png", "x")
	put(t, v, ".obsidian/app.json", "{}")
	ents, err := v.List("")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"A", "a.md", "b.md"}) { // folders first, no hidden, no images
		t.Errorf("list = %v", names)
	}

	// Title and icon come from front matter; a folder's from its _folder.md.
	put(t, v, "c.md", "---\ntitle: Sea\nicon: \"🌊\"\n---\nbody")
	put(t, v, "A/_folder.md", "---\nicon: 📁\n---\n")
	ents, _ = v.List("")
	meta := map[string][2]string{}
	for _, e := range ents {
		meta[e.Name] = [2]string{e.Title, e.Icon}
	}
	if meta["c.md"] != [2]string{"Sea", "🌊"} || meta["A"] != [2]string{"", "📁"} || meta["b.md"] != [2]string{} {
		t.Errorf("title and icon = %v", meta)
	}
	if err := v.Delete("c.md"); err != nil {
		t.Fatal(err)
	}

	if err := v.Move("a.md", "A/moved.md"); err != nil {
		t.Fatal(err)
	}
	if err := v.Move("b.md", "A/moved.md"); !errors.Is(err, ErrExists) {
		t.Errorf("move onto existing = %v", err)
	}
	if err := v.Move("A", "A/inner"); !errors.Is(err, ErrBadPath) {
		t.Errorf("folder into itself = %v", err)
	}
	if err := v.Move("nope.md", "x.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("move missing = %v", err)
	}

	if err := v.Delete("A/moved.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Read("A/moved.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted page still readable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), ".trash", "A", "moved.md")); err != nil {
		t.Errorf("not in the trash: %v", err)
	}
	// Deleting a second page of the same name keeps both.
	put(t, v, "A/moved.md", "again")
	if err := v.Delete("A/moved.md"); err != nil {
		t.Fatal(err)
	}
	trashed, _ := os.ReadDir(filepath.Join(v.Root(), ".trash", "A"))
	if len(trashed) != 2 {
		t.Errorf("trash = %v", trashed)
	}
	if err := v.Delete("missing.md"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing = %v", err)
	}
	// The trash is invisible to search and listings.
	if hits, _ := v.Search("again", 10); len(hits) != 0 {
		t.Errorf("search found trash: %+v", hits)
	}
}

func TestSearch(t *testing.T) {
	v := open(t)
	put(t, v, "Inbox/Alpha.md", "---\ntitle: Rocket plan\n---\nNothing about it here.\n")
	put(t, v, "Inbox/beta.md", "Some words.\n\nThe ROCKET launches at dawn, after a very long preamble about nothing in particular.\n")
	put(t, v, "gamma.md", "unrelated")
	put(t, v, ".trash/old.md", "rocket")
	hits, err := v.Search("rocket", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Path != "Inbox/Alpha.md" || hits[0].Title != "Rocket plan" || hits[1].Path != "Inbox/beta.md" {
		t.Fatalf("hits = %+v", hits)
	}
	if !strings.Contains(hits[1].Snippet, "ROCKET launches") || strings.Contains(hits[1].Snippet, "\n") {
		t.Errorf("snippet = %q", hits[1].Snippet)
	}
	if hits, _ := v.Search("rocket", 1); len(hits) != 1 {
		t.Errorf("limit not applied: %+v", hits)
	}
	if hits, _ := v.Search("  ", 5); len(hits) != 0 {
		t.Errorf("blank query = %+v", hits)
	}
	if hits, _ := v.Search("zzz", 5); len(hits) != 0 {
		t.Errorf("no match = %+v", hits)
	}
}

func TestBacklinks(t *testing.T) {
	v := open(t)
	put(t, v, "Inbox/target.md", "the target")
	put(t, v, "a.md", "see [[target]] and more")
	put(t, v, "b.md", "alias [[target|the idea]] here")
	put(t, v, "c.md", "path [[Inbox/target]] and heading [[target#Section]] and ext [[target.md]]")
	put(t, v, "d.md", "case [[TARGET]]")
	put(t, v, "e.md", "not it: [[targeted]] [[other/target]] [target](x)")
	put(t, v, "Inbox/target2.md", "[[Target]]")
	put(t, v, "f.md", "embed ![[target]]")
	got, err := v.Backlinks("Inbox/target.md")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Inbox/target2.md", "a.md", "b.md", "c.md", "d.md", "f.md"}
	sort := func(s []string) []string { c := append([]string(nil), s...); sortStrings(c); return c }
	if !reflect.DeepEqual(sort(got), sort(want)) {
		t.Errorf("backlinks = %v, want %v", got, want)
	}
	if links := Links("[[a|b]] [[c#d]] [[e.md]] [[ spaced ]] [[]]"); !reflect.DeepEqual(links, []string{"a", "c", "e", "spaced"}) {
		t.Errorf("links = %v", links)
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func TestConfidentialInheritance(t *testing.T) {
	v := open(t)
	put(t, v, "Private/_folder.md", "---\nconfidential: true\n---\nclients\n")
	put(t, v, "Private/deep/er/page.md", "plain page")
	put(t, v, "Private/note.md", "note")
	put(t, v, "Open/page.md", "---\nconfidential: true\n---\nowned secret\n")
	put(t, v, "Open/fine.md", "fine")
	put(t, v, "Open/flag-string.md", "---\nconfidential: \"true\"\n---\nx")
	put(t, v, "Open/nope.md", "---\nconfidential: false\n---\nx")

	cases := map[string]bool{
		"Private/deep/er/page.md": true,
		"Private/note.md":         true,
		"Private/_folder.md":      true,
		"Open/page.md":            true,
		"Open/flag-string.md":     true,
		"Open/fine.md":            false,
		"Open/nope.md":            false,
	}
	for p, want := range cases {
		pg, err := v.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		if pg.Confidential != want {
			t.Errorf("Read(%s).Confidential = %v, want %v", p, pg.Confidential, want)
		}
		if got, _ := v.IsConfidential(p); got != want {
			t.Errorf("IsConfidential(%s) = %v, want %v", p, got, want)
		}
	}
	// Folders: the folder with the flag, and everything under it.
	for p, want := range map[string]bool{"Private": true, "Private/deep": true, "Open": false, "": false} {
		if got, _ := v.IsConfidential(p); got != want {
			t.Errorf("IsConfidential(%q) = %v, want %v", p, got, want)
		}
	}
	ents, _ := v.List("Private")
	for _, e := range ents {
		if !e.Confidential {
			t.Errorf("listing: %s not marked confidential", e.Path)
		}
	}
	hits, _ := v.Search("clients", 5)
	if len(hits) != 1 || !hits[0].Confidential {
		t.Errorf("search hit = %+v", hits)
	}
	// A root-level _folder.md covers the whole vault.
	put(t, v, "_folder.md", "---\nconfidential: true\n---\n")
	if got, _ := v.IsConfidential("Open/fine.md"); !got {
		t.Error("root _folder.md did not cover the vault")
	}
}

func TestOpenConfigured(t *testing.T) {
	data := t.TempDir()
	t.Setenv("LUCID_DATA_DIR", data)
	v, err := OpenConfigured(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(filepath.Join(data, "memory")); v.Root() != want {
		t.Errorf("root = %q, want %q", v.Root(), want)
	}
	for _, d := range []string{"Inbox", "Boards"} {
		if st, err := os.Stat(filepath.Join(v.Root(), d)); err != nil || !st.IsDir() {
			t.Errorf("%s missing: %v", d, err)
		}
	}
	// vault.path wins and gets no Inbox/Boards of its own.
	chosen := filepath.Join(t.TempDir(), "my-vault")
	cfg := config.Default()
	cfg.Vault.Path = chosen
	v2, err := OpenConfigured(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(chosen); v2.Root() != want {
		t.Errorf("root = %q, want %q", v2.Root(), want)
	}
	if _, err := os.Stat(filepath.Join(chosen, "Inbox")); err == nil {
		t.Error("a chosen vault should not get Inbox/ added")
	}
}
