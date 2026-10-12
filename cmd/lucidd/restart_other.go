//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// inPlace: a restart execs the daemon in place, keeping its PID (PID 1 in
// a container stays PID 1).
const inPlace = true

func startProcess(string, []string, []string) error {
	return errors.New("a restart execs in place here")
}

func execProcess(exe string, args, env []string) error {
	return syscall.Exec(exe, append([]string{exe}, args...), env)
}
