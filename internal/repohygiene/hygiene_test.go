// Package repohygiene holds tests that keep user-specific data out of the
// repository. Nothing personal (home directories, machine paths) may be
// committed; the tests walk the tracked files and fail on any.
package repohygiene

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// homePathRE finds user-home absolute paths on Windows, macOS and Linux and
// captures the user name.
var homePathRE = regexp.MustCompile(`(?i)(?:[a-z]:[\\/]+users[\\/]+|/users/|/home/)([^\\/\s"'<>:;,*?|` + "`" + `]+)`)

// placeholders are user names that are obviously not a real person.
var placeholders = map[string]bool{
	"you": true, "user": true, "username": true, "name": true, "yourname": true,
	"your-name": true, "me": true, "public": true, "default": true, "example": true,
	// service accounts baked into container images or CI runners
	"runner": true, "node": true, "agent": true, "nonroot": true,
}

func isPlaceholder(name string) bool {
	l := strings.ToLower(name)
	return placeholders[l] || strings.ContainsAny(name, "$%{}[]") || strings.HasPrefix(name, "...")
}

// findHomePaths returns the offending matches in text.
func findHomePaths(text string) []string {
	var bad []string
	for _, m := range homePathRE.FindAllStringSubmatch(text, -1) {
		if !isPlaceholder(m[1]) {
			bad = append(bad, m[0])
		}
	}
	return bad
}

func TestFindHomePaths(t *testing.T) {
	for _, s := range []string{`C:\Users\alice\x`, "c:/users/bob/x", "/Users/carol/x", "/home/dave/x"} {
		if len(findHomePaths(s)) == 0 {
			t.Errorf("%q not flagged", s)
		}
	}
	for _, s := range []string{`C:\Users\<you>\x`, "/Users/<you>/x", "/home/<user>/x", "/home/node/.x", "$HOME/x", "/host-home/x", "/home/runner/work"} {
		if got := findHomePaths(s); len(got) != 0 {
			t.Errorf("%q flagged: %v", s, got)
		}
	}
}

func TestNoUserHomePathsInTrackedFiles(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("git not available or not a git checkout")
	}
	top := strings.TrimSpace(string(root))
	out, err := exec.Command("git", "-C", top, "ls-files", "-z").Output()
	if err != nil {
		t.Skip("git ls-files failed")
	}
	self := "internal/repohygiene/hygiene_test.go"
	for _, f := range bytes.Split(out, []byte{0}) {
		name := string(f)
		if name == "" || name == self || skipFile(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(top, filepath.FromSlash(name)))
		if err != nil || bytes.IndexByte(data, 0) >= 0 { // unreadable (deleted) or binary
			continue
		}
		for _, bad := range findHomePaths(string(data)) {
			t.Errorf("%s: user-specific path %q (use a placeholder such as <you>)", name, bad)
		}
	}
}

func skipFile(name string) bool {
	switch filepath.Base(name) {
	case "go.sum", "package-lock.json", "pnpm-lock.yaml", "yarn.lock":
		return true
	}
	return false
}
