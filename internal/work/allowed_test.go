package work

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDefaultAllowedByStack(t *testing.T) {
	touch := func(dir, name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	goDir, jsDir, rsDir, bare := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	touch(goDir, "go.mod")
	touch(jsDir, "package.json")
	touch(rsDir, "Cargo.toml")

	for _, c := range []struct {
		dir  string
		want []string
		not  []string
	}{
		{goDir, []string{"go", "gofmt"}, []string{"npm", "cargo"}},
		{jsDir, []string{"npm", "node", "npx"}, []string{"go", "cargo"}},
		{rsDir, []string{"cargo"}, []string{"go", "npm"}},
		{bare, nil, []string{"go", "npm", "cargo"}},
	} {
		got := DefaultAllowed(c.dir, nil)
		for _, w := range append(c.want, "git status", "git commit", "ls", "cat", "grep", "find") {
			if !slices.Contains(got, w) {
				t.Errorf("%s: %q missing from %v", filepath.Base(c.dir), w, got)
			}
		}
		for _, n := range append(c.not, "git", "git push", "rm", "gh", "curl") {
			if slices.Contains(got, n) {
				t.Errorf("%s: %q must not be allowed: %v", filepath.Base(c.dir), n, got)
			}
		}
	}
	// The project's own list replaces the detected stack.
	got := DefaultAllowed(goDir, []string{"make", "git push", "bash"})
	if !slices.Contains(got, "make") || slices.Contains(got, "go") || slices.Contains(got, "git push") || slices.Contains(got, "bash") {
		t.Errorf("work.allowed_commands: %v", got)
	}
}

func TestCleanAllowed(t *testing.T) {
	clean, refused := CleanAllowed([]string{" go ", "go", "git  status", "git", "git push origin", "rm -rf x", "go;ls", "$(x)", "", "sudo make"})
	if got := strings.Join(clean, ","); got != "go,git status" {
		t.Errorf("clean = %q", got)
	}
	if got := strings.Join(refused, "|"); got != "git|git push origin|rm -rf x|go;ls|$(x)|sudo make" {
		t.Errorf("refused = %q", got)
	}
}

func TestAllowedCommandsReachTheCLI(t *testing.T) {
	repo := newRepo(t, true)
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, repo)
	claudeLog, _ := installFakes(t, "edit")

	args := func() []string {
		var rec struct{ Args []string }
		data, _ := os.ReadFile(claudeLog)
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatal(err)
		}
		return rec.Args
	}
	listAfter := func(flag string) []string {
		a := args()
		i := indexOfArg(a, flag)
		if i < 0 {
			return nil
		}
		var out []string
		for _, x := range a[i+1:] {
			if strings.HasPrefix(x, "--") {
				break
			}
			out = append(out, x)
		}
		return out
	}

	// Default: the stack's commands, as claude rules, and never push.
	se, err := f.svc.Start(StartRequest{Project: "demo", Prompt: "run the tests", Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	allow, deny := listAfter("--allowedTools"), listAfter("--disallowedTools")
	for _, w := range []string{"Bash(go:*)", "Bash(gofmt:*)", "Bash(git status:*)", "Bash(git commit:*)", "Bash(ls:*)"} {
		if !slices.Contains(allow, w) {
			t.Errorf("%s missing from --allowedTools %v", w, allow)
		}
	}
	for _, n := range []string{"Bash(git:*)", "Bash(git push:*)", "Bash(rm:*)"} {
		if slices.Contains(allow, n) {
			t.Errorf("%s must not be allowed: %v", n, allow)
		}
	}
	if !slices.Contains(deny, "Bash(git push:*)") || !slices.Contains(deny, "Bash(rm -rf:*)") {
		t.Errorf("--disallowedTools = %v", deny)
	}
	if !slices.Contains(se.AllowedCommands, "go") || slices.Contains(se.AllowedCommands, "git push") {
		t.Errorf("session.allowed_commands = %v", se.AllowedCommands)
	}

	// The request's own list replaces the project's default, as it is.
	se, err = f.svc.Start(StartRequest{Project: "demo", Prompt: "make it", Provider: "claude", AllowedCommands: []string{"make"}})
	if err != nil {
		t.Fatal(err)
	}
	se = wait(t, f.svc, se.ID)
	allow = listAfter("--allowedTools")
	if !slices.Contains(allow, "Bash(make:*)") || slices.Contains(allow, "Bash(go:*)") || slices.Contains(allow, "Bash(git status:*)") || len(allow) != 1 {
		t.Errorf("override --allowedTools = %v", allow)
	}

	// A command that could push or delete is refused, and no worktree is made.
	_, err = f.svc.Start(StartRequest{Project: "demo", Prompt: "x", Provider: "claude", AllowedCommands: []string{"git push"}})
	if !errors.Is(err, ErrBadRequest) || !strings.Contains(err.Error(), "git push") {
		t.Errorf("git push: %v", err)
	}
	if got, _, err := f.svc.Defaults("demo"); err != nil || !slices.Contains(got, "go") {
		t.Errorf("defaults: %v %v", got, err)
	}
	if _, _, err := f.svc.Defaults("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
}

func indexOfArg(args []string, a string) int {
	for i, x := range args {
		if x == a {
			return i
		}
	}
	return -1
}
