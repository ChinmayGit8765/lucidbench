package main

import (
	"os"
	"os/exec"
	"syscall"
)

// inPlace: Windows has no exec, so a restart starts a new process.
const inPlace = false

var getConsoleWindow = syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleWindow")

// startProcess starts the new daemon with this one's standard handles. A
// daemon without a console (the desktop app's sidecar) starts the new one
// without a console window too.
func startProcess(exe string, args, env []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if h, _, _ := getConsoleWindow.Call(); h == 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func execProcess(string, []string, []string) error { return syscall.EWINDOWS }
