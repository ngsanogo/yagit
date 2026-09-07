//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// See process_unix.go for why stopping a *tree* rather than a process is the
// requirement. Windows has no process groups that a signal can reach, so the
// two halves are solved differently.

// createNewProcessGroup is CREATE_NEW_PROCESS_GROUP. It keeps a Ctrl-C in the
// terminal from reaching the children directly, so that this program decides
// when they stop instead of racing them to it.
const createNewProcessGroup = 0x00000200

// detachedProcess keeps a background stack from inheriting this console.
const detachedProcess = 0x00000008

// stillActive is STILL_ACTIVE: what GetExitCodeProcess reports for a process
// that has not exited. Spelled out here because Go's syscall package does not
// declare it — and a program that ships a Windows binary cannot afford a
// constant that only exists on the machine it was written on.
const stillActive = 259

func detachProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}

// processAlive reports whether a pid still exists.
//
// The exit code, not just a handle: a process that has exited keeps its handle
// openable for as long as anything holds one, so OpenProcess succeeding proves
// only that the kernel still remembers the process — not that it is running.
// Taking that for alive is how `./do down` would wait fifteen seconds for
// something that had already stopped.
func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() {
		if err := syscall.CloseHandle(handle); err != nil {
			warn("closing the handle for process %d: %s", pid, err)
		}
	}()

	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

// signalProcess stops a background stack. Graceful shutdown is not reliable on
// Windows console processes, so the whole tree is ended.
func signalProcess(pid int, _ os.Signal) error {
	return stopProcessTree(pid)
}

func isolateProcessTree(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// terminateProcessTree kills the process and everything it spawned.
func terminateProcessTree(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return stopProcessTree(command.Process.Pid)
}

// stopProcessTree kills a pid and everything it spawned.
//
// taskkill /T walks the parent-child chain Windows records, which is the only
// mechanism available without pulling in golang.org/x/sys for job objects.
// /F is not a shortcut: without it taskkill posts WM_CLOSE, which a console
// process like Vite never handles, and the tree survives.
func stopProcessTree(pid int) error {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprint(pid))
	if output, err := kill.CombinedOutput(); err != nil {
		// Exit code 128 is "no such process": the tree is already gone, which
		// is the outcome we wanted.
		if exitCode(err) == 128 {
			return nil
		}
		return fmt.Errorf("stopping process tree %d: %w: %s", pid, err, output)
	}
	return nil
}

// exitCode reports the status a failed command exited with, or -1 when it
// never ran.
func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}
