package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SweepGrace is how old a run folder must be before Sweep may delete it.
const SweepGrace = 10 * time.Minute

func parseOwner(data []byte) (pid int, lockName string, err error) {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	pid, err = strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || pid <= 0 {
		return 0, "", fmt.Errorf("no owner pid")
	}
	if len(lines) > 1 {
		lockName = strings.TrimSpace(lines[1])
	}
	return pid, lockName, nil
}

var runIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// pidState is processState; a variable so tests can stub it.
var pidState = processState

// sweepBeforeRemove, if set, runs after a folder passes every check and
// before anything in it is deleted. Tests use it to swap paths mid-sweep.
var sweepBeforeRemove func(id string)

// Kept is a run folder Sweep left alone, and why.
type Kept struct{ ID, Reason string }

// SweepFailure is a stale run folder Sweep could not fully delete. The next
// sweep tries again.
type SweepFailure struct {
	ID  string
	Err error
}

// SweepResult is what one Sweep did.
type SweepResult struct {
	Removed []string
	Kept    []Kept
	Failed  []SweepFailure
}

// Lines describes the result, one line per folder, then a summary line.
func (r SweepResult) Lines() []string {
	var out []string
	for _, id := range r.Removed {
		out = append(out, "removed "+id)
	}
	for _, k := range r.Kept {
		out = append(out, "kept "+k.ID+": "+k.Reason)
	}
	for _, f := range r.Failed {
		out = append(out, fmt.Sprintf("failed %s: %v (will retry on the next sweep)", f.ID, f.Err))
	}
	return append(out, fmt.Sprintf("removed %d, kept %d, failed %d", len(r.Removed), len(r.Kept), len(r.Failed)))
}

// Sweep deletes run folders left behind by a crashed run or daemon. A folder
// under <dataDir>/runs is deleted only when its owner record is older than
// grace, its owner PID is dead, and no live lock under <dataDir>/locks names
// the owner PID or the lock the run recorded. Any check that cannot be
// answered keeps the folder.
//
// Every delete goes through an os.Root opened on runs/, so nothing outside
// it can be reached and links are removed, never followed. locks/ is only
// read. Sweep is idempotent: a folder it fails to delete keeps its owner
// record and is judged again next time.
func Sweep(dataDir string, grace time.Duration) (SweepResult, error) {
	var res SweepResult
	runsDir := filepath.Join(dataDir, "runs")
	fi, err := os.Lstat(runsDir)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, err
	}
	if !plainDir(fi) {
		return res, fmt.Errorf("%s is not a plain directory; not sweeping it", runsDir)
	}
	root, err := os.OpenRoot(runsDir)
	if err != nil {
		return res, err
	}
	defer root.Close()

	lockPIDs, lockNames, lockErr := liveLocks(filepath.Join(dataDir, "locks"))
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return res, err
	}
	now := time.Now()
	for _, e := range entries {
		id := e.Name()
		if reason := keepReason(root, id, now, grace, lockPIDs, lockNames, lockErr); reason != "" {
			res.Kept = append(res.Kept, Kept{ID: id, Reason: reason})
			continue
		}
		if sweepBeforeRemove != nil {
			sweepBeforeRemove(id)
		}
		if err := removeRun(root, id); err != nil {
			res.Failed = append(res.Failed, SweepFailure{ID: id, Err: err})
			continue
		}
		res.Removed = append(res.Removed, id)
	}
	return res, nil
}

// keepReason returns why the run folder id must stay, or "" if it is stale.
func keepReason(root *os.Root, id string, now time.Time, grace time.Duration,
	lockPIDs map[int]bool, lockNames map[string]int, lockErr error) string {
	if !runIDRe.MatchString(id) {
		return "not a run folder"
	}
	fi, err := root.Lstat(id)
	if err != nil {
		return "cannot stat: " + err.Error()
	}
	if !plainDir(fi) {
		return "not a plain directory"
	}
	owner := filepath.Join(id, ownerFile)
	ofi, err := root.Lstat(owner)
	if errors.Is(err, os.ErrNotExist) {
		return "no owner record (made by an older version or still being created); delete it by hand if it is stale"
	}
	if err != nil {
		return "owner record unreadable: " + err.Error()
	}
	if !ofi.Mode().IsRegular() {
		return "owner record is not a regular file"
	}
	if age := now.Sub(ofi.ModTime()); age < grace {
		return fmt.Sprintf("younger than the %s grace period", grace)
	}
	data, err := root.ReadFile(owner)
	if err != nil {
		return "owner record unreadable: " + err.Error()
	}
	pid, lockName, err := parseOwner(data)
	if err != nil {
		return "owner record unreadable: " + err.Error()
	}
	alive, err := pidState(pid)
	if err != nil {
		return "cannot tell whether the owner is alive: " + err.Error()
	}
	if alive {
		return fmt.Sprintf("owner pid %d is alive", pid)
	}
	if lockErr != nil {
		return "lock state unknown: " + lockErr.Error()
	}
	if lockPIDs[pid] {
		return fmt.Sprintf("a live lock is held by owner pid %d", pid)
	}
	if holder, ok := lockNames[lockName]; ok && lockName != "" {
		return fmt.Sprintf("lock %s is held by live pid %d", lockName, holder)
	}
	return ""
}

// liveLocks reads every lock under dir and returns the PIDs and file names of
// the ones whose holder is alive. An error means some lock could not be read
// or judged, so no folder can be proven unreferenced.
func liveLocks(dir string) (map[int]bool, map[string]int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	pids, names := map[int]bool{}, map[string]int{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".lock") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue // released since the listing
		}
		if err != nil {
			return nil, nil, fmt.Errorf("lock %s unreadable: %w", name, err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, nil, fmt.Errorf("lock %s holds no pid", name)
		}
		alive, err := pidState(pid)
		if err != nil {
			return nil, nil, fmt.Errorf("lock %s: %w", name, err)
		}
		if alive {
			pids[pid], names[name] = true, pid
		}
	}
	return pids, names, nil
}

// removeRun deletes the run folder id through root. The owner record goes
// last, so a delete that fails partway (a file held open on Windows) leaves a
// folder the next sweep can judge again.
func removeRun(root *os.Root, id string) error {
	children, err := fs.ReadDir(root.FS(), id)
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range children {
		if c.Name() != ownerFile {
			errs = append(errs, root.RemoveAll(filepath.Join(id, c.Name())))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := root.Remove(filepath.Join(id, ownerFile)); err != nil {
		return err
	}
	return root.Remove(id)
}

// plainDir reports whether fi is a directory that is not a symlink, junction
// or other reparse point.
func plainDir(fi fs.FileInfo) bool {
	return fi.IsDir() && fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0
}
