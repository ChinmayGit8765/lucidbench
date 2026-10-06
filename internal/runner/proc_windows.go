//go:build windows

package runner

import (
	"fmt"
	"syscall"
)

const stillActive = 259

// errInvalidParameter is what OpenProcess returns for a PID with no process.
const errInvalidParameter syscall.Errno = 87

// processState reports whether pid is a running process. An error means the
// answer is unknown.
func processState(pid int) (bool, error) {
	if pid <= 0 {
		return false, fmt.Errorf("invalid pid %d", pid)
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	switch {
	case err == syscall.ERROR_ACCESS_DENIED:
		return true, nil
	case err == errInvalidParameter:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("check pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false, fmt.Errorf("check pid %d: %w", pid, err)
	}
	return code == stillActive, nil
}
