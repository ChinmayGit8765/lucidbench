// Package memory is the vault: a folder of Markdown pages with YAML front
// matter and Obsidian-style [[wikilinks]]. It is files all the way down, so
// the same folder opens in Obsidian.
//
// Every path in the API is vault-relative with "/" separators. Paths that
// are absolute, carry a drive letter, contain ".." or leave the vault through
// a symlink are rejected.
package memory

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ChinmayGit8765/lucidbench/internal/config"
)

// Errors returned by the Vault. The HTTP layer maps each to a status.
var (
	ErrBadPath  = errors.New("bad path")
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

// Page is one Markdown file. Front holds every front matter key, including
// the ones Lucidbench does not know, so a write never drops them.
type Page struct {
	Path         string         `json:"path"`
	Title        string         `json:"title"`
	Front        map[string]any `json:"front"`
	Body         string         `json:"body"`
	Modified     time.Time      `json:"modified"`
	Confidential bool           `json:"confidential"`
}

// Entry is one row of a folder listing.
type Entry struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	Dir          bool      `json:"dir"`
	Size         int64     `json:"size"`
	Modified     time.Time `json:"modified"`
	Confidential bool      `json:"confidential"`
}

// Hit is one search result.
type Hit struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Snippet      string `json:"snippet"`
	Confidential bool   `json:"confidential"`
}

// FolderFile is the page whose front matter describes its folder.
const FolderFile = "_folder.md"

// TrashDir is where Delete moves things, inside the vault.
const TrashDir = ".trash"

// Vault is an opened vault folder.
type Vault struct {
	root string // absolute, symlinks resolved
}

// Open opens the vault at root, creating the folder when it is missing.
func Open(root string) (*Vault, error) {
	if root == "" {
		return nil, errors.New("memory: empty vault path")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("memory: create vault folder: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("memory: %s is not a folder", root)
	}
	return &Vault{root: real}, nil
}

// OpenConfigured opens the vault the config points at: vault.path when set,
// else <data dir>/memory, created with Inbox/ and Boards/.
func OpenConfigured(cfg *config.Config) (*Vault, error) {
	if cfg != nil && cfg.Vault.Path != "" {
		return Open(cfg.Vault.Path)
	}
	data, err := config.DataDir()
	if err != nil {
		return nil, err
	}
	v, err := Open(filepath.Join(data, "memory"))
	if err != nil {
		return nil, err
	}
	for _, d := range []string{"Inbox", "Boards"} {
		if err := os.MkdirAll(filepath.Join(v.root, d), 0o755); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// Root returns the absolute vault folder.
func (v *Vault) Root() string { return v.root }

// clean validates a vault-relative path and returns it in canonical form
// ("" for the root).
func clean(rel string) (string, error) {
	if strings.ContainsAny(rel, "\\:\x00") {
		return "", fmt.Errorf("%w: %q (use / separators, no drive letters)", ErrBadPath, rel)
	}
	if strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%w: %q is absolute", ErrBadPath, rel)
	}
	var segs []string
	for _, s := range strings.Split(rel, "/") {
		switch {
		case s == "" || s == ".":
		case s == "..":
			return "", fmt.Errorf("%w: %q leaves the vault", ErrBadPath, rel)
		case strings.HasPrefix(s, "."):
			return "", fmt.Errorf("%w: %q is a hidden folder", ErrBadPath, rel)
		default:
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/"), nil
}

// resolve returns the clean vault-relative path and its absolute location,
// refusing anything that reaches outside the vault through a symlink.
func (v *Vault) resolve(rel string) (string, string, error) {
	c, err := clean(rel)
	if err != nil {
		return "", "", err
	}
	abs := filepath.Join(v.root, filepath.FromSlash(c))
	// Check each existing part of the path. A symlink may only point inside
	// the vault; a Windows junction (which Go reports as irregular and does
	// not resolve) is refused outright.
	cur := v.root
	if c != "" {
		for _, seg := range strings.Split(c, "/") {
			cur = filepath.Join(cur, seg)
			fi, err := os.Lstat(cur)
			if errors.Is(err, fs.ErrNotExist) {
				break // the rest is not there to be a link
			}
			if err != nil {
				return "", "", err
			}
			switch {
			case fi.Mode()&fs.ModeSymlink != 0:
				real, err := filepath.EvalSymlinks(cur)
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						return "", "", fmt.Errorf("%w: %q is a broken link", ErrBadPath, rel)
					}
					return "", "", err
				}
				if !within(v.root, real) {
					return "", "", fmt.Errorf("%w: %q leaves the vault through a link", ErrBadPath, rel)
				}
			case fi.Mode()&fs.ModeIrregular != 0:
				return "", "", fmt.Errorf("%w: %q goes through a junction or special file", ErrBadPath, rel)
			}
		}
	}
	return c, abs, nil
}

// within reports whether p is root or inside it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isMD(name string) bool { return strings.EqualFold(path.Ext(name), ".md") }

func mapErr(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}

// List returns the folder's entries, folders first, then by name. Hidden
// items (.trash, .obsidian) and non-Markdown files are left out.
func (v *Vault) List(dir string) ([]Entry, error) {
	c, abs, err := v.resolve(dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return nil, mapErr(err)
	}
	out := []Entry{}
	for _, e := range ents {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(abs, name)
		isDir := e.IsDir()
		if e.Type()&fs.ModeSymlink != 0 {
			// Follow a link only when it stays in the vault.
			real, err := filepath.EvalSymlinks(full)
			if err != nil || !within(v.root, real) {
				continue
			}
			st, err := os.Stat(real)
			if err != nil {
				continue
			}
			info, isDir = st, st.IsDir()
		}
		if !isDir && !isMD(name) {
			continue
		}
		rel := path.Join(c, name)
		conf, _ := v.IsConfidential(rel)
		ent := Entry{Name: name, Path: rel, Dir: isDir, Modified: info.ModTime(), Confidential: conf}
		if !isDir {
			ent.Size = info.Size()
		}
		out = append(out, ent)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Read returns the page at path.
func (v *Vault) Read(p string) (*Page, error) {
	c, abs, err := v.resolve(p)
	if err != nil {
		return nil, err
	}
	if c == "" || !isMD(c) {
		return nil, fmt.Errorf("%w: %q is not a Markdown page", ErrBadPath, p)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, mapErr(err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, mapErr(err)
	}
	front, body := SplitFront(string(data))
	pg := &Page{Path: c, Front: front, Body: body, Modified: st.ModTime()}
	pg.Title = titleOf(front, c)
	pg.Confidential = truthy(front["confidential"])
	if !pg.Confidential {
		pg.Confidential, _ = v.inheritedConfidential(c)
	}
	return pg, nil
}

func titleOf(front map[string]any, rel string) string {
	if s, ok := front["title"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return strings.TrimSuffix(path.Base(rel), path.Ext(rel))
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	}
	return false
}

// Write saves the page atomically (a temporary file in the same folder, then
// a rename), creating folders as needed.
func (v *Vault) Write(p *Page) error {
	if p == nil {
		return fmt.Errorf("%w: no page", ErrBadPath)
	}
	c, abs, err := v.resolve(p.Path)
	if err != nil {
		return err
	}
	if c == "" || !isMD(c) {
		return fmt.Errorf("%w: %q is not a Markdown page", ErrBadPath, p.Path)
	}
	front := p.Front
	// A title that differs from the file name is kept in the front matter.
	if t := strings.TrimSpace(p.Title); t != "" && t != titleOf(front, c) {
		front = copyMap(front)
		front["title"] = t
	}
	data, err := JoinFront(front, p.Body)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return fmt.Errorf("%w: %q is a folder", ErrBadPath, p.Path)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	// MkdirAll may have followed nothing odd, but check the final location.
	if _, _, err := v.resolve(c); err != nil {
		return err
	}
	return writeAtomic(abs, data)
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// writeAtomic writes data to a temporary file next to dst and renames it
// over dst, so a reader sees the old or the new content, never half.
func writeAtomic(dst string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".lucid-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Move renames a page or folder. The destination must not exist.
func (v *Vault) Move(from, to string) error {
	cf, af, err := v.resolve(from)
	if err != nil {
		return err
	}
	ct, at, err := v.resolve(to)
	if err != nil {
		return err
	}
	if cf == "" || ct == "" {
		return fmt.Errorf("%w: cannot move the vault root", ErrBadPath)
	}
	st, err := os.Stat(af)
	if err != nil {
		return mapErr(err)
	}
	if !st.IsDir() && !isMD(ct) {
		return fmt.Errorf("%w: %q is not a Markdown page", ErrBadPath, to)
	}
	if st.IsDir() && (ct == cf || strings.HasPrefix(ct, cf+"/")) {
		return fmt.Errorf("%w: cannot move a folder into itself", ErrBadPath)
	}
	if _, err := os.Lstat(at); err == nil {
		return fmt.Errorf("%w: %q", ErrExists, to)
	}
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return err
	}
	return os.Rename(af, at)
}

// Delete moves a page or folder to <vault>/.trash/, keeping its path. A name
// already in the trash gets a timestamp suffix.
func (v *Vault) Delete(p string) error {
	c, abs, err := v.resolve(p)
	if err != nil {
		return err
	}
	if c == "" {
		return fmt.Errorf("%w: cannot delete the vault root", ErrBadPath)
	}
	if _, err := os.Lstat(abs); err != nil {
		return mapErr(err)
	}
	dst := filepath.Join(v.root, TrashDir, filepath.FromSlash(c))
	if _, err := os.Lstat(dst); err == nil {
		ext := filepath.Ext(dst)
		dst = strings.TrimSuffix(dst, ext) + "-" + time.Now().UTC().Format("20060102T150405.000000000") + ext
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(abs, dst)
}

// walk calls fn for every visible Markdown page, in path order. Hidden
// folders and symlinks are skipped.
func (v *Vault) walk(fn func(rel string, abs string) error) error {
	return filepath.WalkDir(v.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == v.root {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || !isMD(name) {
			return nil
		}
		rel, err := filepath.Rel(v.root, p)
		if err != nil {
			return nil
		}
		return fn(filepath.ToSlash(rel), p)
	})
}

// Search finds pages whose title or body contains q, ignoring case. Title
// matches come first. limit <= 0 means 20.
func (v *Vault) Search(q string, limit int) ([]Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []Hit{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	needle := strings.ToLower(q)
	var titled, bodied []Hit
	err := v.walk(func(rel, abs string) error {
		if len(titled) >= limit {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil
		}
		front, body := SplitFront(string(data))
		title := titleOf(front, rel)
		inTitle := strings.Contains(strings.ToLower(title), needle)
		i := strings.Index(strings.ToLower(body), needle)
		if !inTitle && i < 0 {
			return nil
		}
		h := Hit{Path: rel, Title: title}
		if i >= 0 {
			h.Snippet = snippet(body, i, len(needle))
		} else {
			h.Snippet = snippet(body, 0, 0)
		}
		h.Confidential = truthy(front["confidential"])
		if !h.Confidential {
			h.Confidential, _ = v.inheritedConfidential(rel)
		}
		if inTitle {
			titled = append(titled, h)
		} else {
			bodied = append(bodied, h)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	hits := append(titled, bodied...)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

// snippet returns about 160 characters around a match, on one line.
func snippet(body string, at, n int) string {
	lo, hi := at-60, at+n+100
	if lo < 0 {
		lo = 0
	}
	if hi > len(body) {
		hi = len(body)
	}
	for lo > 0 && lo < len(body) && body[lo]&0xC0 == 0x80 {
		lo--
	}
	for hi < len(body) && body[hi]&0xC0 == 0x80 {
		hi++
	}
	s := strings.Join(strings.Fields(body[lo:hi]), " ")
	if lo > 0 {
		s = "…" + s
	}
	if hi < len(body) {
		s += "…"
	}
	return s
}

var wikilinkRE = regexp.MustCompile(`\[\[([^\[\]\n]+?)\]\]`)

// Links returns the targets of the [[wikilinks]] in body, without any
// "|alias", "#heading" or ".md".
func Links(body string) []string {
	var out []string
	for _, m := range wikilinkRE.FindAllStringSubmatch(body, -1) {
		t := m[1]
		if i := strings.IndexAny(t, "|#"); i >= 0 {
			t = t[:i]
		}
		t = strings.TrimSuffix(strings.TrimSpace(t), ".md")
		if t != "" {
			out = append(out, filepath.ToSlash(t))
		}
	}
	return out
}

// Backlinks lists the pages that link to path with [[name]], [[name|alias]]
// or [[folder/name]]. Obsidian links by note name, so a bare name matches
// the page wherever it lives.
func (v *Vault) Backlinks(p string) ([]string, error) {
	c, _, err := v.resolve(p)
	if err != nil {
		return nil, err
	}
	if c == "" || !isMD(c) {
		return nil, fmt.Errorf("%w: %q is not a Markdown page", ErrBadPath, p)
	}
	self := strings.ToLower(strings.TrimSuffix(c, path.Ext(c)))
	out := []string{}
	err = v.walk(func(rel, abs string) error {
		if rel == c {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil
		}
		_, body := SplitFront(string(data))
		for _, t := range Links(body) {
			t = strings.ToLower(t)
			if t == self || strings.HasSuffix(self, "/"+t) {
				out = append(out, rel)
				break
			}
		}
		return nil
	})
	return out, err
}

// IsConfidential reports whether the page or folder at path is confidential:
// its own front matter says confidential: true (a folder's is in its
// _folder.md), or any folder above it does.
func (v *Vault) IsConfidential(p string) (bool, error) {
	c, abs, err := v.resolve(p)
	if err != nil {
		return false, err
	}
	if c != "" && isMD(c) {
		if data, err := os.ReadFile(abs); err == nil {
			front, _ := SplitFront(string(data))
			if truthy(front["confidential"]) {
				return true, nil
			}
		}
		return v.inheritedConfidential(c)
	}
	// A folder: its own _folder.md, then its ancestors'.
	if c != "" && v.folderFileSaysConfidential(c) {
		return true, nil
	}
	return v.inheritedConfidential(c)
}

// inheritedConfidential checks the _folder.md of every folder above rel, up
// to and including the vault root.
func (v *Vault) inheritedConfidential(rel string) (bool, error) {
	dir := path.Dir(rel)
	for {
		if dir == "." {
			dir = ""
		}
		if v.folderFileSaysConfidential(dir) {
			return true, nil
		}
		if dir == "" {
			return false, nil
		}
		dir = path.Dir(dir)
	}
}

func (v *Vault) folderFileSaysConfidential(dir string) bool {
	f := filepath.Join(v.root, filepath.FromSlash(path.Join(dir, FolderFile)))
	data, err := os.ReadFile(f)
	if err != nil {
		return false
	}
	front, _ := SplitFront(string(data))
	return truthy(front["confidential"])
}

// SplitFront separates the YAML front matter from the Markdown body. A file
// without a front matter block, or with one that is not valid YAML, comes
// back whole as the body.
func SplitFront(text string) (map[string]any, string) {
	text = strings.TrimPrefix(text, "\ufeff")
	front := map[string]any{}
	if !strings.HasPrefix(text, "---\n") && !strings.HasPrefix(text, "---\r\n") {
		return front, text
	}
	rest := text[strings.IndexByte(text, '\n')+1:]
	var yml strings.Builder
	for len(rest) > 0 {
		line := rest
		next := ""
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			line, next = rest[:i+1], rest[i+1:]
		}
		if t := strings.TrimRight(line, "\r\n"); t == "---" || t == "..." {
			var n yaml.Node
			if err := yaml.Unmarshal([]byte(yml.String()), &n); err != nil {
				return front, text
			}
			if n.Kind == 0 { // an empty block
				return front, next
			}
			if len(n.Content) != 1 || n.Content[0].Kind != yaml.MappingNode {
				return front, text
			}
			m, ok := fromNode(n.Content[0]).(map[string]any)
			if !ok {
				return front, text
			}
			return m, next
		}
		yml.WriteString(line)
		rest = next
	}
	return front, text
}

// fromNode builds plain Go values from YAML, keeping timestamps as the text
// they were written as so a round trip does not reformat them.
func fromNode(n *yaml.Node) any {
	switch n.Kind {
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = fromNode(n.Content[i+1])
		}
		return m
	case yaml.SequenceNode:
		s := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			s = append(s, fromNode(c))
		}
		return s
	case yaml.AliasNode:
		return fromNode(n.Alias)
	case yaml.ScalarNode:
		if n.Tag == "!!timestamp" {
			return n.Value
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return n.Value
		}
		return v
	}
	return nil
}

// JoinFront builds a file from front matter and a body. With no front matter
// the file is just the body. Keys come out sorted.
func JoinFront(front map[string]any, body string) ([]byte, error) {
	var buf bytes.Buffer
	if len(front) > 0 {
		y, err := yaml.Marshal(front)
		if err != nil {
			return nil, fmt.Errorf("front matter: %w", err)
		}
		buf.WriteString("---\n")
		buf.Write(y)
		buf.WriteString("---\n")
	}
	buf.WriteString(body)
	return buf.Bytes(), nil
}
