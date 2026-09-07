//go:build !windows

package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A deadline that kills git is not a deadline that ends the request.
//
// os/exec signals the process when the context is done, and then Run waits for
// the pipes it handed out to be closed. git does not hold those alone: `git
// push` starts ssh, a fetch starts the credential helper the user configured,
// and each of them inherits the same standard output. Killing git leaves the
// grandchild holding the write end, so Run goes on blocking long after the
// deadline it was given — the failure mode is a request that never comes back
// on a daemon whose logs say the command timed out ten minutes ago.
//
// process.WaitDelay is what bounds that second wait. This test is the reason
// the field is set, and the shape of the test is the shape of the bug: a fake
// git that starts a background child holding standard output, then hangs
// itself so the deadline is what ends it.
//
// Not on Windows: the fake is a shell script, and the process-group semantics
// this exercises are not the ones Windows has.
func TestExecComesBackWhenAGrandchildHoldsThePipe(t *testing.T) {
	// The grandchild outlives any honest answer, so a Run that waits for it is
	// unambiguous rather than slow.
	const grandchildLifetime = 120

	directory := t.TempDir()
	fake := filepath.Join(directory, "git")
	script := "#!/bin/sh\n" +
		"# Inherits stdout and keeps it open, the way ssh does under a push.\n" +
		"sleep " + strconv.Itoa(grandchildLifetime) + " &\n" +
		"# And this is git itself, hanging until the deadline kills it.\n" +
		"sleep " + strconv.Itoa(grandchildLifetime) + "\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{binary: fake, timeout: 200 * time.Millisecond}

	started := time.Now()
	_, err := runner.Exec(context.Background(), Command{Dir: directory, Args: []string{"status"}})
	elapsed := time.Since(started)

	// The command was killed by its deadline, so it must fail. A nil error here
	// would mean the fake returned successfully, and the test would be measuring
	// nothing.
	if err == nil {
		t.Fatal("a command killed by its deadline reported success")
	}

	// Generous on purpose. What is being told apart is "came back after the
	// wait delay" from "waited for a grandchild with two minutes left", and any
	// bound between the two proves it without turning a busy machine red.
	if limit := 30 * time.Second; elapsed > limit {
		t.Fatalf("Exec took %s, past %s: it waited for the grandchild's pipe rather than for WaitDelay", elapsed, limit)
	}
}

// The field is set at all. The test above proves what it does; this one fails
// the moment somebody removes the line while tidying, which is a different and
// much likelier accident.
func TestWaitDelayIsSetOnEveryCommand(t *testing.T) {
	if waitDelay <= 0 {
		t.Fatal("waitDelay is not positive, which os/exec reads as 'wait forever'")
	}
}

// The other half of WaitDelay, and the one that bites on an ordinary machine.
//
// A pre-commit hook that starts something in the background — a watcher, a
// development server, anything behind `tool &` — leaves that process holding
// git's stderr. git writes the commit and exits 0; five seconds later the wait
// delay expires and os/exec reports ErrWaitDelay for a command that succeeded.
//
// Reading that as a failure is the worst answer available: the commit exists,
// the interface says it does not, and the user makes a second one.
func TestACleanExitSurvivesAGrandchildHoldingThePipe(t *testing.T) {
	const grandchildLifetime = 120

	directory := t.TempDir()
	fake := filepath.Join(directory, "git")
	script := "#!/bin/sh\n" +
		"# What a hook that backgrounds something leaves behind.\n" +
		"sleep " + strconv.Itoa(grandchildLifetime) + " &\n" +
		"# And git itself, doing its job and exiting cleanly.\n" +
		"echo done\n" +
		"exit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{binary: fake, timeout: time.Minute}

	started := time.Now()
	output, err := runner.Exec(context.Background(), Command{Dir: directory, Args: []string{"commit"}})
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("git exited 0 and the command was reported as failed: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "done" {
		t.Errorf("standard output = %q, want %q", got, "done")
	}

	// It still comes back promptly rather than waiting out the grandchild —
	// which is what the wait delay is for. The bound is generous for the same
	// reason as the test above.
	if limit := 30 * time.Second; elapsed > limit {
		t.Errorf("Exec took %s, past %s: it waited for the grandchild", elapsed, limit)
	}
}

// The deadline a transfer runs under measures SILENCE, not elapsed time. A
// clone of a large repository on a slow line is an hour of legitimate work
// that git reports progress through, and a wall clock turned that into a
// repository written most of the way and a message blaming the network.
func TestATransferSurvivesAsLongAsItKeepsReporting(t *testing.T) {
	directory := t.TempDir()
	fake := filepath.Join(directory, "git")

	// Talks for well over its idle allowance, a line at a time, then finishes.
	// Under a wall clock of the same length this could not complete.
	script := "#!/bin/sh\n" +
		"i=0\n" +
		"while [ $i -lt 12 ]; do\n" +
		"  printf 'Receiving objects: %d%%\\r' $i >&2\n" +
		"  sleep 0.05\n" +
		"  i=$((i+1))\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{binary: fake, timeout: time.Minute, writes: newDirLocks()}

	lines := 0
	_, err := runner.Exec(context.Background(), Command{
		Dir:  directory,
		Args: []string{"fetch", "origin"},
		// Shorter than the whole command takes, longer than any one gap in it.
		IdleTimeout: 200 * time.Millisecond,
		OnProgress:  func(string) { lines++ },
	})
	if err != nil {
		t.Fatalf("a transfer that kept reporting was cut off: %v", err)
	}
	if lines < 5 {
		t.Errorf("saw %d progress lines: the test is not exercising the reset", lines)
	}
}

// And the case the deadline exists for: a connection that stopped answering
// without closing.
func TestATransferThatGoesQuietIsGivenUpOn(t *testing.T) {
	directory := t.TempDir()
	fake := filepath.Join(directory, "git")

	// One line, then silence for far longer than the allowance.
	script := "#!/bin/sh\n" +
		"printf 'Receiving objects: 1%%\\r' >&2\n" +
		"sleep 60\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{binary: fake, timeout: time.Minute, writes: newDirLocks()}

	started := time.Now()
	_, err := runner.Exec(context.Background(), Command{
		Dir:         directory,
		Args:        []string{"fetch", "origin"},
		IdleTimeout: 200 * time.Millisecond,
		OnProgress:  func(string) {},
	})
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a transfer that went silent was reported as successful")
	}
	// The sentence has to name silence rather than a deadline nobody set: the
	// user is being told why, and "timed out after 1m0s" is not why.
	if !strings.Contains(err.Error(), "no progress") {
		t.Errorf("err = %v, want it to name the silence", err)
	}
	if limit := 30 * time.Second; elapsed > limit {
		t.Errorf("Exec took %s, past %s", elapsed, limit)
	}
}
