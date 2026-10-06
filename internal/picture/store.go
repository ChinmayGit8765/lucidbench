// Package picture keeps a project's diagrams and design canvases: Mermaid
// schematics (.mmd) and Excalidraw canvases (.excalidraw).
//
// They live in the project's own repository under docs/picture when the
// project has a local_path that is a folder on this machine. A project
// without one keeps them in Memory, as pages under Picture/<project>/ whose
// body is the source in a fenced block, so the vault stays Markdown.
package picture

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ChinmayGit8765/lucidbench/internal/memory"
	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// Kinds of picture.
const (
	KindMermaid    = "mermaid"
	KindExcalidraw = "excalidraw"
)

// Where a picture is kept.
const (
	WhereRepo   = "repo"
	WhereMemory = "memory"
)

// RepoDir is the folder inside a project repository, with / separators.
const RepoDir = "docs/picture"

// MemoryDir is the Memory folder; each project has a subfolder.
const MemoryDir = "Picture"

// MaxSize is the largest picture accepted or read.
const MaxSize = 4 << 20

var (
	ErrBadPath  = errors.New("bad picture name")
	ErrNotFound = errors.New("picture not found")
	ErrTooBig   = errors.New("the picture is too large")
)

var exts = map[string]string{KindMermaid: ".mmd", KindExcalidraw: ".excalidraw"}

// nameRE is one lower-case path segment: no dots, slashes or spaces, so a
// name can never be a path.
var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// reserved are the Windows device names, which cannot be file names.
var reserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

// ValidName checks a picture name (without its extension).
func ValidName(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%w: %q must be 1-63 characters of a-z, 0-9, - and _", ErrBadPath, name)
	}
	base := strings.SplitN(name, "-", 2)[0]
	base = strings.SplitN(base, "_", 2)[0]
	if reserved[base] || regexp.MustCompile(`^(com|lpt)[1-9]$`).MatchString(base) {
		return fmt.Errorf("%w: %q is a reserved device name", ErrBadPath, name)
	}
	return nil
}

// Ext returns the file extension for a kind, or an error for an unknown kind.
func Ext(kind string) (string, error) {
	e, ok := exts[kind]
	if !ok {
		return "", fmt.Errorf("%w: kind must be mermaid or excalidraw", ErrBadPath)
	}
	return e, nil
}

// Item is one picture in a listing.
type Item struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Where    string    `json:"where"`
	Path     string    `json:"path"` // inside the repo or the vault, with / separators
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

// Location says where a project's pictures are written.
type Location struct {
	Where string `json:"where"`
	// Dir is the absolute folder for a repo, or the Memory folder.
	Dir string `json:"dir"`
}

// Store reads and writes pictures. Vault opens Memory on first use.
type Store struct {
	Vault memory.Opener
}

// repoRoot returns the project's local folder when it has one.
func repoRoot(p projects.Project) string {
	if p.LocalPath == "" {
		return ""
	}
	if st, err := os.Stat(p.LocalPath); err != nil || !st.IsDir() {
		return ""
	}
	return filepath.Clean(p.LocalPath)
}

// Locate says where p's pictures go: docs/picture in its repo, else Memory.
func (s *Store) Locate(p projects.Project) Location {
	if root := repoRoot(p); root != "" {
		return Location{Where: WhereRepo, Dir: filepath.Join(root, filepath.FromSlash(RepoDir))}
	}
	return Location{Where: WhereMemory, Dir: MemoryDir + "/" + p.ID}
}

// Target returns the exact place a picture is written: an absolute file for a
// repo, a vault page path for Memory. It validates the name and kind.
func (s *Store) Target(p projects.Project, name, kind string) (Location, string, error) {
	if err := ValidName(name); err != nil {
		return Location{}, "", err
	}
	ext, err := Ext(kind)
	if err != nil {
		return Location{}, "", err
	}
	loc := s.Locate(p)
	if loc.Where == WhereRepo {
		return loc, filepath.Join(loc.Dir, name+ext), nil
	}
	return loc, loc.Dir + "/" + name + ext + ".md", nil
}

// plainDirs refuses a docs or docs/picture that is a link or junction, so a
// write cannot be redirected out of the repository. Missing folders are fine.
func plainDirs(root string) error {
	cur := root
	for _, seg := range strings.Split(RepoDir, "/") {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return fmt.Errorf("%w: %s is a link or not a folder", ErrBadPath, seg)
		}
	}
	return nil
}

// Write saves a picture. It returns where it went.
func (s *Store) Write(p projects.Project, name, kind, content string) (Item, error) {
	if len(content) > MaxSize {
		return Item{}, ErrTooBig
	}
	loc, target, err := s.Target(p, name, kind)
	if err != nil {
		return Item{}, err
	}
	if loc.Where == WhereMemory {
		v, err := s.vault()
		if err != nil {
			return Item{}, err
		}
		pg := &memory.Page{Path: target, Front: map[string]any{"picture": kind, "project": p.ID}, Body: fence(kind, content)}
		if err := v.Write(pg); err != nil {
			return Item{}, err
		}
		return Item{Name: name, Kind: kind, Where: WhereMemory, Path: target, Size: int64(len(content)), Modified: time.Now()}, nil
	}
	root := repoRoot(p)
	if err := plainDirs(root); err != nil {
		return Item{}, err
	}
	if err := os.MkdirAll(loc.Dir, 0o755); err != nil {
		return Item{}, err
	}
	// MkdirAll may have created folders; check again before writing.
	if err := plainDirs(root); err != nil {
		return Item{}, err
	}
	if fi, err := os.Lstat(target); err == nil && !fi.Mode().IsRegular() {
		return Item{}, fmt.Errorf("%w: %s is not a plain file", ErrBadPath, filepath.Base(target))
	}
	if err := writeAtomic(target, []byte(content)); err != nil {
		return Item{}, err
	}
	return Item{Name: name, Kind: kind, Where: WhereRepo, Path: RepoDir + "/" + filepath.Base(target), Size: int64(len(content)), Modified: time.Now()}, nil
}

// Read returns a picture's source.
func (s *Store) Read(p projects.Project, name, kind string) (string, error) {
	loc, target, err := s.Target(p, name, kind)
	if err != nil {
		return "", err
	}
	if loc.Where == WhereMemory {
		v, err := s.vault()
		if err != nil {
			return "", err
		}
		pg, err := v.Read(target)
		if errors.Is(err, memory.ErrNotFound) {
			return "", ErrNotFound
		}
		if err != nil {
			return "", err
		}
		return unfence(pg.Body), nil
	}
	if err := plainDirs(repoRoot(p)); err != nil {
		return "", err
	}
	fi, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a plain file", ErrBadPath, filepath.Base(target))
	}
	if fi.Size() > MaxSize {
		return "", ErrTooBig
	}
	b, err := os.ReadFile(target)
	return string(b), err
}

// List returns the project's pictures, newest first.
func (s *Store) List(p projects.Project) ([]Item, error) {
	loc := s.Locate(p)
	items := []Item{}
	if loc.Where == WhereRepo {
		if err := plainDirs(repoRoot(p)); err != nil {
			return nil, err
		}
		ents, err := os.ReadDir(loc.Dir)
		if errors.Is(err, fs.ErrNotExist) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			kind, name, ok := splitFile(e.Name())
			if !ok || !e.Type().IsRegular() {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			items = append(items, Item{Name: name, Kind: kind, Where: WhereRepo, Path: RepoDir + "/" + e.Name(), Size: fi.Size(), Modified: fi.ModTime()})
		}
	} else {
		v, err := s.vault()
		if err != nil {
			return nil, err
		}
		ents, err := v.List(loc.Dir)
		if errors.Is(err, memory.ErrNotFound) {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if e.Dir || !strings.HasSuffix(e.Name, ".md") {
				continue
			}
			kind, name, ok := splitFile(strings.TrimSuffix(e.Name, ".md"))
			if !ok {
				continue
			}
			items = append(items, Item{Name: name, Kind: kind, Where: WhereMemory, Path: e.Path, Size: e.Size, Modified: e.Modified})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].Modified.Equal(items[j].Modified) {
			return items[i].Modified.After(items[j].Modified)
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *Store) vault() (*memory.Vault, error) {
	if s.Vault == nil {
		return nil, errors.New("Memory is not available")
	}
	return s.Vault()
}

// splitFile reads "name.mmd" or "name.excalidraw".
func splitFile(file string) (kind, name string, ok bool) {
	for k, e := range exts {
		if strings.HasSuffix(file, e) {
			n := strings.TrimSuffix(file, e)
			if ValidName(n) == nil {
				return k, n, true
			}
		}
	}
	return "", "", false
}

// fence wraps source for a Memory page, so Obsidian shows it as code.
func fence(kind, content string) string {
	lang := "json"
	if kind == KindMermaid {
		lang = "mermaid"
	}
	return "```" + lang + "\n" + strings.TrimRight(content, "\n") + "\n```\n"
}

// unfence is the inverse of fence.
func unfence(body string) string {
	t := strings.TrimSpace(body)
	if !strings.HasPrefix(t, "```") {
		return body
	}
	nl := strings.IndexByte(t, '\n')
	if nl < 0 {
		return body
	}
	t = t[nl+1:]
	t = strings.TrimSuffix(strings.TrimRight(t, " \r\n"), "```")
	return strings.TrimRight(t, "\r\n") + "\n"
}

func writeAtomic(dst string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(dst), ".picture-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
