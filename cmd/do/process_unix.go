//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// Stopping the development stack means stopping process *trees*, not
// processes.
//
// `npm run dev` does not forward SIGTERM to the Vite it spawned (measured):
// killing npm's own pid leaves Vite orphaned, its port taken, and the next
// `./do dev` failing on an inexplicable strictPort error. The same holds for
// air and the daemon binary it rebuilds.
//
// Unix and Windows solve this differently enough to deserve a file each,
// rather than a runtime.GOOS branch inside one.

// detachProcess starts a command in a new session so it survives the
// terminal that launched `./do up`.
func detachProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// processAlive reports whether a pid still exists. Signal 0 does no work
// beyond the permission and existence checks.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

// signalProcess asks a process to stop. The supervisor traps this and stops
// air and Vite before it exits.
func signalProcess(pid int, sig os.Signal) error {
	syscallSig, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("signal %v is not a syscall.Signal", sig)
	}
	if err := syscall.Kill(pid, syscallSig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signalling process %d: %w", pid, err)
	}
	return nil
}

// isolateProcessTree puts the command in a process group of its own, so the
// whole tree can be signalled at once.
func isolateProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcessTree sends SIGTERM to the whole group.
func terminateProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return stopProcessTree(command.Process.Pid)
}

// stopProcessTree ends the process group a pid leads.
//
// The negative pid is the point: it targets the process GROUP, which is what
// takes Vite and the rebuilt daemon down along with their parents. It is also
// what makes signalling a recorded pid safe after the fact — a number the
// system has since reused leads no group of its own, so the call finds nothing
// rather than ending a stranger's process.
func stopProcessTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		// ESRCH means the group is already gone, which is the outcome we
		// wanted. Anything else is worth reporting.
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return fmt.Errorf("stopping process group %d: %w", pid, err)
	}
	return nil
}
