package agentexec

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procTree keeps a provider CLI and everything it starts in one Job Object
// with KILL_ON_JOB_CLOSE, so cancelling the run ends the whole tree, not only
// the direct child. If lucidd itself dies, closing the handle ends the tree.
type procTree struct {
	mu     sync.Mutex
	job    windows.Handle
	killed bool
}

// newProcTree creates the job. When no job can be made the tree is empty and
// the run falls back to killing the direct child only.
func newProcTree() *procTree {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return &procTree{}
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return &procTree{}
	}
	return &procTree{job: job}
}

// prepare is called before Start. Nothing to set on Windows.
func (t *procTree) prepare(cmd *exec.Cmd) {}

// attach puts the started process in the job. Go's os/exec cannot start a
// process suspended, so there is a window of a few microseconds between the
// process being created and being assigned; a grandchild spawned inside that
// window would escape the job. A CLI that must load a runtime before it can
// spawn anything does not get there first. A child started after assignment
// joins the job automatically.
func (t *procTree) attach(cmd *exec.Cmd) error {
	if t.job == 0 {
		return nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == 0 {
		return nil
	}
	if err := windows.AssignProcessToJobObject(t.job, h); err != nil {
		return err
	}
	if t.killed { // cancelled before the assignment landed
		return windows.TerminateJobObject(t.job, 1)
	}
	return nil
}

// kill ends every process in the tree.
func (t *procTree) kill(cmd *exec.Cmd) error {
	t.mu.Lock()
	t.killed = true
	job := t.job
	var err error
	if job != 0 {
		err = windows.TerminateJobObject(job, 1)
	}
	t.mu.Unlock()
	if job == 0 || err != nil {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
	}
	return err
}

// close ends any process still in the tree and releases the job.
func (t *procTree) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		windows.CloseHandle(t.job) // KILL_ON_JOB_CLOSE
		t.job = 0
	}
}
