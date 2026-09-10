//go:build !windows

package main

import (
	"bufio"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestTerminateProcessTreeTakesTheChildrenToo covers the reason this function
// signals a group rather than a process.
//
// `npm run dev` does not forward SIGTERM to the Vite it spawned. Killing npm's
// own pid leaves Vite orphaned, its port held, and the next `./do dev`
// failing on a strictPort error that names nothing useful. The shape below is
// that one: a parent that ignores the signal, and a child that would outlive
// it.
func TestTerminateProcessTreeTakesTheChildrenToo(t *testing.T) {
	// A parent and a long-lived child, in one process group.
	//
	// The parent is deliberately not made to ignore SIGTERM. An earlier draft
	// of this test used `trap '' TERM` to model npm, and it hung: a
	// disposition of SIG_IGN is inherited across fork and exec, so the child
	// ignored the signal too and nothing ever died. What distinguishes a group
	// kill from a pid kill needs no trap — signalling the parent's pid alone
	// never reaches the child at all, which is the whole failure being
	// guarded against.
	command := exec.Command("/bin/sh", "-c", `sleep 300 & echo $!; wait`)

	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	isolateProcessTree(command)

	if err := command.Start(); err != nil {
		t.Fatalf("starting the parent: %v", err)
	}
	t.Cleanup(func() {
		// Whatever this test concludes, nothing it started may outlive it.
		_ = terminateProcessTree(command)
		_ = command.Wait()
	})

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the child's pid: %v", err)
	}
	child, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("parsing the child's pid from %q: %v", line, err)
	}

	if !processExists(child) {
		t.Fatalf("the child %d was not running to begin with", child)
	}

	if err := terminateProcessTree(command); err != nil {
		t.Fatalf("terminateProcessTree: %v", err)
	}
	// The parent died of the signal, so Wait reports it. That is the expected
	// outcome, not a failure of the test.
	_ = command.Wait()

	// Reaping is the kernel's business and takes a moment: the child is
	// reparented before it disappears from the table.
	deadline := time.Now().Add(10 * time.Second)
	for processExists(child) {
		if time.Now().After(deadline) {
			t.Fatalf("the child %d outlived its parent — this is Vite holding its port", child)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestTerminateProcessTreeIsQuietAboutAGroupThatIsAlreadyGone covers the path
// taken on every ordinary exit: `do dev` stops the stack after one of the two
// processes has already died on its own.
func TestTerminateProcessTreeIsQuietAboutAGroupThatIsAlreadyGone(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "exit 0")
	isolateProcessTree(command)

	if err := command.Start(); err != nil {
		t.Fatalf("starting: %v", err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("waiting: %v", err)
	}

	// ESRCH is the outcome we wanted, not a failure to report. Treating it as
	// one would print a warning on every clean shutdown, and a warning that
	// always appears is one nobody reads.
	if err := terminateProcessTree(command); err != nil {
		t.Errorf("terminating an already-finished process reported: %v", err)
	}
}

// TestTerminateProcessTreeToleratesAProcessThatNeverStarted covers the error
// path in startDevStack: the first command starts, the second fails, and the
// stack is stopped with one of its two entries holding no process at all.
func TestTerminateProcessTreeToleratesAProcessThatNeverStarted(t *testing.T) {
	if err := terminateProcessTree(exec.Command("/bin/sh", "-c", "true")); err != nil {
		t.Errorf("terminating an unstarted command reported: %v", err)
	}
}

// processExists reports whether the kernel still knows this pid. Signal 0 does
// no work beyond the permission and existence checks.
func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}
