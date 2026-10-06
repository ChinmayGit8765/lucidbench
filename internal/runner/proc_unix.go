//go:build !windows

package runner

import (
	"errors"
	"fmt"
	"syscall"
)

// processState reports whether pid is a running process. An error means the
// answer is unknown.
func processState(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid pid %d", pid)
	}
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil || errors.Is(err, syscall.EPERM):
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	}
	return false, fmt.Errorf("check pid %d: %w", pid, err)
}
