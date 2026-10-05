package usage

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// retention is how far back parsed records are kept. Summaries never look
// further back than MaxDays, so older lines are skipped while parsing.
const retention = (MaxDays + 2) * 24 * time.Hour

// cacheVersion is bumped whenever a cached entry changes shape.
const cacheVersion = 1

// FileState says how far a log file has been read. A file is reused as is
// when its size and mtime match, resumed from Offset when it only grew (the
// logs are append-only; the hash of the first bytes guards that), and read
// again from the start otherwise.
type FileState struct {
	Size    int64
	ModNano int64
	Head    uint64
	Offset  int64
}

// cache holds what was parsed from each log file, keyed by path. It lives in
// the data dir so a restart does not read every log again.
type cache struct {
	Version int
	Claude  map[string]*claudeEntry
	Codex   map[string]*codexEntry
}

func newCache() *cache {
	return &cache{Version: cacheVersion, Claude: map[string]*claudeEntry{}, Codex: map[string]*codexEntry{}}
}

func loadCache(path string) *cache {
	f, err := os.Open(path)
	if err != nil {
		return newCache()
	}
	defer f.Close()
	c := newCache()
	var in cache
	if err := gob.NewDecoder(bufio.NewReader(f)).Decode(&in); err != nil || in.Version != cacheVersion {
		return c
	}
	if in.Claude != nil {
		c.Claude = in.Claude
	}
	if in.Codex != nil {
		c.Codex = in.Codex
	}
	return c
}

func (c *cache) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-cache-*")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(tmp)
	err = gob.NewEncoder(w).Encode(c)
	if err == nil {
		err = w.Flush()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func headHash(f *os.File, size int64) uint64 {
	n := int64(256)
	if size < n {
		n = size
	}
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, 0); err != nil && err != io.EOF {
		return 0
	}
	h := fnv.New64a()
	h.Write(buf)
	return h.Sum64()
}

// plan decides where reading a file should start: -1 means the cached state
// is current, 0 a full read, anything else a resume.
func (st *FileState) plan(info fs.FileInfo, f *os.File) (from int64, fresh bool) {
	if st.Size == info.Size() && st.ModNano == info.ModTime().UnixNano() {
		return -1, false
	}
	if st.Size > 0 && info.Size() >= st.Size && st.Offset > 0 && headHash(f, info.Size()) == st.Head {
		return st.Offset, false
	}
	return 0, true
}

// readLines calls fn for each complete line of f from offset and returns the
// offset after the last line it consumed. A final line without a newline is
// consumed only when it is valid JSON, so a half-written line is read again
// next time.
func readLines(f *os.File, offset int64, fn func(line []byte)) (int64, error) {
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			switch {
			case line[len(line)-1] == '\n':
				offset += int64(len(line))
				fn(bytes.TrimRight(line, "\r\n"))
			case err == io.EOF && json.Valid(line):
				offset += int64(len(line))
				fn(line)
			}
		}
		if err != nil {
			if err == io.EOF {
				return offset, nil
			}
			return offset, err
		}
	}
}

// walkJSONL lists the .jsonl files under root (optionally only names with
// prefix). A missing root is not an error.
func walkJSONL(root, prefix string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") && strings.HasPrefix(d.Name(), prefix) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// parallel runs fn over items on a small worker pool.
func parallel[T any](items []T, fn func(T)) {
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	var wg sync.WaitGroup
	ch := make(chan T)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range ch {
				fn(it)
			}
		}()
	}
	for _, it := range items {
		ch <- it
	}
	close(ch)
	wg.Wait()
}
