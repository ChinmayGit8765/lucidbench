//go:build !windows

package agentexec

import (
	"os/exec"
	"syscall"
)

// procTree runs a provider CLI in its own process group, so cancelling the
// run ends the whole tree, not only the direct child.
type procTree struct{ pgid int }

func newProcTree() *procTree { return &procTree{} }

func (t *procTree) prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (t *procTree) attach(cmd *exec.Cmd) error {
	t.pgid = cmd.Process.Pid // Setpgid makes the child its own group leader
	return nil
}

// kill sends SIGKILL to the whole group.
func (t *procTree) kill(cmd *exec.Cmd) error {
	pid := cmd.Process.Pid // set before Cancel can run
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return cmd.Process.Kill()
	}
	return nil
}

// close ends any process left in the group.
func (t *procTree) close() {
	if t.pgid != 0 {
		_ = syscall.Kill(-t.pgid, syscall.SIGKILL)
	}
}
