package work

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var lookPath = exec.LookPath

// gitTimeout bounds every git and gh call; a push can be slow.
const gitTimeout = 2 * time.Minute

// run runs a program in dir and returns its trimmed stdout. The error carries
// stderr. Git never prompts: a push that needs credentials fails instead.
func runIn(dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return strings.TrimRight(out.String(), "\r\n"), nil
}

func git(dir string, args ...string) (string, error) { return runIn(dir, "git", args...) }

// worktree is a created session environment.
type worktree struct {
	repo, path, branch, baseRef, baseSHA string
}

// defaultBase returns the branch to start from: the remote's default branch
// (origin/HEAD), as the local branch of that name when there is one, else
// the repo's current branch.
func defaultBase(repo string) (string, error) {
	if ref, err := git(repo, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		name := strings.TrimPrefix(ref, "refs/remotes/origin/")
		if _, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name, nil
		}
		return "origin/" + name, nil
	}
	cur, err := git(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", errf(ErrBadRequest, "cannot read the current branch: %v", err)
	}
	return cur, nil // "HEAD" when detached, which still names a commit
}

// createWorktree makes <repo>/../<repo-name>-lucid-<id> on a new branch
// lucid/<id>-<slug> from the default branch.
func createWorktree(localPath, id, name string) (*worktree, error) {
	st, err := os.Stat(localPath)
	if err != nil || !st.IsDir() {
		return nil, errf(ErrBadRequest, "local_path %s does not exist", localPath)
	}
	top, err := git(localPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errf(ErrBadRequest, "local_path %s is not a git repository", localPath)
	}
	top = longPath(filepath.Clean(filepath.FromSlash(top)))
	base, err := defaultBase(top)
	if err != nil {
		return nil, err
	}
	sha, err := git(top, "rev-parse", "--verify", base+"^{commit}")
	if err != nil {
		return nil, errf(ErrBadRequest, "the repository has no commit to start from on %s", base)
	}
	w := &worktree{
		repo:    top,
		path:    filepath.Join(filepath.Dir(top), filepath.Base(top)+"-lucid-"+id),
		branch:  "lucid/" + id + "-" + name,
		baseRef: base,
		baseSHA: sha,
	}
	if _, err := os.Stat(w.path); err == nil {
		return nil, errf(ErrConflict, "%s already exists", w.path)
	}
	if _, err := git(top, "worktree", "add", "-b", w.branch, w.path, sha); err != nil {
		return nil, fmt.Errorf("cannot create the worktree: %w", err)
	}
	return w, nil
}

// Diff is what a session changed, against the commit it started from.
type Diff struct {
	Base    string       `json:"base"`
	Stat    string       `json:"stat"`
	Files   []FileChange `json:"files"`
	Commits []Commit     `json:"commits"`
	// Uncommitted lists `git status --porcelain` lines: changes the agent
	// made but did not commit. They are not in a PR.
	Uncommitted []string `json:"uncommitted"`
	Added       int      `json:"added"`
	Deleted     int      `json:"deleted"`
}

// FileChange is one changed file. Patch is cut when long.
type FileChange struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary,omitempty"`
	Patch   string `json:"patch,omitempty"`
	Cut     bool   `json:"cut,omitempty"`
}

// Commit is one commit on the session's branch.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
}

const (
	maxFilePatch  = 48 << 10
	maxTotalPatch = 384 << 10
)

// summarise reads the diff of dir's HEAD against base.
func summarise(dir, base string) (*Diff, error) {
	d := &Diff{Base: base, Files: []FileChange{}, Commits: []Commit{}, Uncommitted: []string{}}
	rng := base + "...HEAD"
	stat, err := git(dir, "diff", "--stat", rng)
	if err != nil {
		return nil, err
	}
	d.Stat = stat
	num, err := git(dir, "diff", "--numstat", rng)
	if err != nil {
		return nil, err
	}
	for _, l := range strings.Split(num, "\n") {
		f := strings.SplitN(l, "\t", 3)
		if len(f) != 3 {
			continue
		}
		fc := FileChange{Path: f[2]}
		if f[0] == "-" {
			fc.Binary = true
		} else {
			fc.Added, _ = strconv.Atoi(f[0])
			fc.Deleted, _ = strconv.Atoi(f[1])
		}
		d.Added += fc.Added
		d.Deleted += fc.Deleted
		d.Files = append(d.Files, fc)
	}
	if patch, err := git(dir, "diff", "--no-color", rng); err == nil {
		attachPatches(d.Files, patch)
	}
	if log, err := git(dir, "log", "--format=%H%x09%s", base+"..HEAD"); err == nil {
		for _, l := range strings.Split(log, "\n") {
			if sha, subj, ok := strings.Cut(l, "\t"); ok {
				d.Commits = append(d.Commits, Commit{SHA: sha, Subject: subj})
			}
		}
	}
	if st, err := git(dir, "status", "--porcelain"); err == nil {
		for _, l := range strings.Split(st, "\n") {
			if strings.TrimSpace(l) != "" {
				d.Uncommitted = append(d.Uncommitted, l)
			}
		}
	}
	return d, nil
}

// attachPatches splits a whole patch per file. git prints files in the same
// order for --numstat and the patch; when the counts differ nothing is kept.
func attachPatches(files []FileChange, patch string) {
	if patch == "" {
		return
	}
	parts := strings.Split("\n"+patch, "\ndiff --git ")
	parts = parts[1:]
	if len(parts) != len(files) {
		return
	}
	total := 0
	for i, p := range parts {
		p = "diff --git " + p
		if len(p) > maxFilePatch || total+len(p) > maxTotalPatch {
			files[i].Cut = true
			n := min(len(p), maxFilePatch, max(0, maxTotalPatch-total))
			p = p[:n]
		}
		total += len(p)
		files[i].Patch = p
	}
}

var prURLRE = regexp.MustCompile(`https?://\S+`)

// prBody is the PR description. It leaves the machine, so it names no local
// paths: the brief by its vault path, the commits and the diff stat.
func prBody(se *Session) string {
	var sb strings.Builder
	if se.Brief != "" {
		sb.WriteString("Brief: `" + se.Brief + "` (Lucidbench Memory)\n\n")
	}
	if se.Diff != nil && len(se.Diff.Commits) > 0 {
		sb.WriteString("## Commits\n\n")
		for i := len(se.Diff.Commits) - 1; i >= 0; i-- {
			c := se.Diff.Commits[i]
			sb.WriteString("- " + c.SHA[:min(7, len(c.SHA))] + " " + c.Subject + "\n")
		}
		sb.WriteString("\n")
	}
	if se.Diff != nil && se.Diff.Stat != "" {
		sb.WriteString("## Changes\n\n```\n" + se.Diff.Stat + "\n```\n\n")
	}
	sb.WriteString("Drafted by a " + se.Provider + " agent in a Lucidbench Work session (" + se.ID + "). Review before merging.\n")
	return sb.String()
}

// OpenPR pushes the session's branch to origin and opens a draft PR with gh.
// It never merges. A session that already has a PR returns it.
func (s *Service) OpenPR(id string) (Session, error) {
	e, se, err := s.settled(id)
	if err != nil {
		return se, err
	}
	if se.PR != "" {
		return se, nil
	}
	diff, err := summarise(se.Worktree, se.BaseSHA)
	if err != nil {
		return se, fmt.Errorf("cannot read the worktree: %w", err)
	}
	s.update(e, func(x *Session) { x.Diff = diff })
	se.Diff = diff
	if len(diff.Commits) == 0 {
		msg := "the branch has no commits to open a PR with"
		if len(diff.Uncommitted) > 0 {
			msg += fmt.Sprintf(" (%d uncommitted changes in the worktree: commit them first)", len(diff.Uncommitted))
		}
		return se, errf(ErrConflict, "%s", msg)
	}
	if _, err := git(se.Worktree, "remote", "get-url", "origin"); err != nil {
		return se, errf(ErrConflict, "the repository has no origin remote to push to")
	}
	if _, err := git(se.Worktree, "push", "-u", "origin", se.Branch); err != nil {
		return se, fmt.Errorf("push failed: %w", err)
	}
	s.update(e, func(x *Session) { x.Pushed = true })
	if _, err := lookPath("gh"); err != nil {
		return s.mustGet(id), errf(ErrConflict, "the branch was pushed, but gh is not on PATH to open the PR; install the GitHub CLI and sign in with `gh auth login`")
	}
	title := se.Title
	if title == "" {
		title = se.Branch
	}
	args := []string{"pr", "create", "--draft", "--title", title, "--body", prBody(&se), "--head", se.Branch}
	if b := strings.TrimPrefix(se.BaseRef, "origin/"); b != "" && b != "HEAD" {
		args = append(args, "--base", b)
	}
	out, err := runIn(se.Worktree, "gh", args...)
	if err != nil {
		return s.mustGet(id), fmt.Errorf("gh could not open the PR: %w", err)
	}
	url := ""
	for _, m := range prURLRE.FindAllString(out, -1) {
		url = m
	}
	if url == "" {
		return s.mustGet(id), fmt.Errorf("gh did not print a PR URL: %s", out)
	}
	s.update(e, func(x *Session) { x.PR = url })
	if se.Card != "" {
		s.moveCard(se.Board, se.Card, "", url, false)
	}
	return s.Get(id)
}

// Refresh reads a finished session's diff again, for changes made in the
// worktree after the run ended.
func (s *Service) Refresh(id string) (Session, error) {
	e, se, err := s.settled(id)
	if err != nil {
		return se, err
	}
	diff, err := summarise(se.Worktree, se.BaseSHA)
	if err != nil {
		return se, fmt.Errorf("cannot read the worktree: %w", err)
	}
	s.update(e, func(x *Session) { x.Diff = diff })
	return s.Get(id)
}

func (s *Service) mustGet(id string) Session {
	se, _ := s.Get(id)
	return se
}

// Unpushed counts the worktree's commits that are not on the remote, and its
// uncommitted changes.
func Unpushed(dir, base string) (commits, changes int, err error) {
	rng := base + "..HEAD"
	if _, uerr := git(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); uerr == nil {
		rng = "@{u}..HEAD"
	}
	n, err := git(dir, "rev-list", "--count", rng)
	if err != nil {
		return 0, 0, err
	}
	commits, _ = strconv.Atoi(n)
	st, err := git(dir, "status", "--porcelain")
	if err != nil {
		return 0, 0, err
	}
	for _, l := range strings.Split(st, "\n") {
		if strings.TrimSpace(l) != "" {
			changes++
		}
	}
	return commits, changes, nil
}

// Remove deletes the session's worktree. Unpushed commits or uncommitted
// changes need discard; the branch itself stays in the repository.
func (s *Service) Remove(id string, discard bool) (Session, error) {
	e, se, err := s.settled(id)
	if err != nil {
		return se, err
	}
	if _, serr := os.Stat(se.Worktree); serr == nil {
		commits, changes, err := Unpushed(se.Worktree, se.BaseSHA)
		if err != nil {
			return se, fmt.Errorf("cannot read the worktree: %w", err)
		}
		if (commits > 0 || changes > 0) && !discard {
			return se, errf(ErrConflict, "the worktree has %s and %s; open a PR first, or confirm discarding them", plural(commits, "unpushed commit"), plural(changes, "uncommitted change"))
		}
		args := []string{"worktree", "remove", se.Worktree}
		if discard {
			args = []string{"worktree", "remove", "--force", se.Worktree}
		}
		if _, err := git(se.RepoPath, args...); err != nil {
			return se, fmt.Errorf("cannot remove the worktree: %w", err)
		}
	} else {
		_, _ = git(se.RepoPath, "worktree", "prune")
	}
	s.update(e, func(x *Session) { x.Removed = true })
	return s.Get(id)
}
