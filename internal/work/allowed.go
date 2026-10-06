package work

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ChinmayGit8765/lucidbench/internal/projects"
)

// A Work session cannot answer a permission prompt, so any command the CLI
// would ask about is refused and the agent cannot run its own checks. The
// allowed commands are the ones it may run without asking.

// baseAllowed is allowed in every project: reading, searching, and git for
// reading and committing. Git is listed by subcommand, never as a whole, so
// that push is not covered.
var baseAllowed = []string{
	"git status", "git diff", "git log", "git show", "git ls-files", "git branch",
	"git add", "git commit", "git restore", "git rev-parse",
	"ls", "cat", "grep", "find",
}

// stackAllowed is added when a marker file is found at the project's root.
var stackAllowed = []struct {
	marker string
	cmds   []string
}{
	{"go.mod", []string{"go", "gofmt"}},
	{"package.json", []string{"npm", "node", "npx"}},
	{"Cargo.toml", []string{"cargo"}},
}

// alwaysDenied is refused whatever the allow list says: pushing, deleting,
// and shells or downloaders that would sidestep the list.
var alwaysDenied = []string{
	"git push", "git remote", "git config", "git clean", "git reset", "git checkout", "git switch",
	"rm", "rmdir", "del", "sudo", "curl", "wget", "ssh", "scp", "gh",
	"sh", "bash", "zsh", "pwsh", "powershell", "cmd", "eval", "env",
}

// DefaultAllowed returns the commands a session on the project at localPath
// may run: the base set plus those of its detected stacks, and cfg, the
// project's own work.allowed_commands, replacing the stack part when set.
func DefaultAllowed(localPath string, cfg []string) []string {
	out := slices.Clone(baseAllowed)
	if len(cfg) > 0 {
		out = append(out, cfg...)
	} else {
		for _, s := range stackAllowed {
			if _, err := os.Stat(filepath.Join(localPath, s.marker)); err == nil {
				out = append(out, s.cmds...)
			}
		}
	}
	clean, _ := CleanAllowed(out)
	return clean
}

var cmdRE = regexp.MustCompile(`^[A-Za-z0-9._+-]+( [A-Za-z0-9._+-]+)*$`)

// CleanAllowed trims and de-duplicates a list. It reports the entries it
// refused: a malformed one, one that is on the always-denied list, or a bare
// "git", which would allow push.
func CleanAllowed(in []string) (clean, refused []string) {
	clean = []string{}
	seen := map[string]bool{}
	for _, c := range in {
		c = strings.Join(strings.Fields(c), " ")
		switch {
		case c == "":
			continue
		case !cmdRE.MatchString(c) || deniedCommand(c):
			refused = append(refused, c)
		case !seen[c]:
			seen[c] = true
			clean = append(clean, c)
		}
	}
	return clean, refused
}

// deniedCommand reports whether c is, or starts with, an always-denied
// command. A bare "git" is denied too: it covers every subcommand.
func deniedCommand(c string) bool {
	if c == "git" {
		return true
	}
	for _, d := range alwaysDenied {
		if c == d || strings.HasPrefix(c, d+" ") {
			return true
		}
	}
	return false
}

// projectAllowed is the default list for a project, honouring its work block.
func projectAllowed(p projects.Project) []string {
	var cfg []string
	if p.Work != nil {
		cfg = p.Work.AllowedCommands
	}
	return DefaultAllowed(p.LocalPath, cfg)
}

// sessionAllowed picks the list a session runs with: the request's own when
// it sent one, else the project's. A refused entry fails the start rather
// than being dropped, so the user sees why a command is not available.
func sessionAllowed(req *StartRequest, p projects.Project) ([]string, error) {
	if req.AllowedCommands == nil {
		return projectAllowed(p), nil
	}
	clean, refused := CleanAllowed(append(slices.Clone(baseAllowed), req.AllowedCommands...))
	if len(refused) > 0 {
		return nil, errf(ErrBadRequest, "these commands cannot be allowed: %s", strings.Join(refused, ", "))
	}
	return clean, nil
}

// Defaults returns the allowed commands a new session on the project would
// start with, for the New Session form.
func (s *Service) Defaults(project string) ([]string, error) {
	if s.Projects == nil {
		return nil, errf(ErrBadRequest, "no projects file")
	}
	list, err := s.Projects()
	if err != nil {
		return nil, err
	}
	for _, p := range list.Projects {
		if p.ID == project {
			return projectAllowed(p), nil
		}
	}
	return nil, errf(ErrNotFound, "no project %q in projects.yaml", project)
}

// denyRules is what the CLI is told never to run, whatever else it is
// allowed: pushing, and deleting a folder tree.
func denyRules() []string {
	return []string{"git push", "git remote", "rm -rf", "rm -r", "sudo", "curl", "wget"}
}
