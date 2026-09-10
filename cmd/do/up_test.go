package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestParseStackFlags(t *testing.T) {
	options, err := parseStackFlags("up", nil)
	if err != nil || options != (stackOptions{}) {
		t.Fatalf("parseStackFlags(up, nil) = (%+v, %v), want (zero, nil)", options, err)
	}

	options, err = parseStackFlags("up", []string{"--foreground", "--restart"})
	if err != nil || !options.foreground || !options.restart || options.newToken {
		t.Fatalf("parseStackFlags(up, --foreground --restart) = (%+v, %v)", options, err)
	}

	if _, err := parseStackFlags("up", []string{"--wat"}); err == nil {
		t.Fatal("an unknown flag must be refused")
	}
}

// TestNewTokenImpliesARestart pins the one rule the flags carry. Without it a
// daemon already up keeps serving the token that was just replaced, and the
// card prints a secret that opens nothing.
func TestNewTokenImpliesARestart(t *testing.T) {
	for _, command := range []string{"up", "dev"} {
		options, err := parseStackFlags(command, []string{"--new-token"})
		if err != nil {
			t.Fatalf("parseStackFlags(%s, --new-token): %v", command, err)
		}
		if !options.newToken || !options.restart {
			t.Errorf("parseStackFlags(%s, --new-token) = %+v, want restart implied", command, options)
		}
	}
}

// `dev` is `up --foreground` by another name, and takes only the flag that
// makes sense for a foreground stack: it always replaces a background one, so
// --restart would decide nothing, and it cannot be told to be foreground twice.
func TestDevTakesFewerFlagsThanUp(t *testing.T) {
	for _, flag := range []string{"--restart", "--foreground", "-f"} {
		_, err := parseStackFlags("dev", []string{flag})
		if err == nil {
			t.Errorf("dev accepted %s", flag)
			continue
		}
		if !strings.Contains(err.Error(), "dev") {
			t.Errorf("error = %v, want the command it was typed for named", err)
		}
	}
}

// `dev` takes one flag of its own and reads it before bootstrapping anything,
// so a typo costs a sentence rather than a toolchain install.
func TestDevRefusesAnUnknownFlag(t *testing.T) {
	err := runDev(newProject(t), []string{"--restart"})
	if err == nil {
		t.Fatal("dev should refuse a flag it does not take")
	}
	if !strings.Contains(err.Error(), "dev") {
		t.Errorf("error = %v, want the command it was typed for named", err)
	}
}

// A .env the daemon cannot be started on is refused by `./do up` itself,
// before anything is installed or stopped. The case in mind is the migration to
// a reverse proxy: YAGIT_PUBLIC_URL pasted under the YAGIT_PUBLIC_HOST and
// YAGIT_LISTEN_ALL=1 it replaces, then `./do up --restart`. Refused by the
// supervisor instead, that stopped the working stack first and put the reason
// in a log tail.
func TestUpRefusesAConfigurationTheDaemonCannotStartOn(t *testing.T) {
	// Nothing below should reach the port. If it ever does, it reaches this
	// closed one rather than whatever stack the machine running the suite has.
	useNoDaemon(t)
	p := newProject(t)
	writeFile(t, p.path(".env"), "YAGIT_ROOT="+t.TempDir()+"\n"+
		"YAGIT_PUBLIC_HOST=dev-box.local\nYAGIT_LISTEN_ALL=1\n"+
		"YAGIT_PUBLIC_URL=https://yagit.dev-box.local\n")

	err := runUp(p, []string{"--restart"})
	if err == nil {
		t.Fatal("up should refuse a public URL beside the widening")
	}
	if !strings.Contains(err.Error(), "comment YAGIT_PUBLIC_URL out") {
		t.Errorf("error = %v, want the refusal itself rather than a failure further on", err)
	}

	// ensureBootstrapped creates the state directory before anything else it
	// does, so its absence says nothing was installed — and nothing was
	// stopped, since stopping comes after it.
	if _, statErr := os.Stat(p.path(stateDirectory)); !os.IsNotExist(statErr) {
		t.Errorf("%s exists (%v): the refusal came after the bootstrap", stateDirectory, statErr)
	}
}

func TestBrowserURLFollowsConfiguration(t *testing.T) {
	url, err := browserURL(configuration{publicHost: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://127.0.0.1:7420/" {
		t.Errorf("browserURL(loopback) = %q", url)
	}

	url, err = browserURL(configuration{publicHost: "dev-box.local", listenAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://dev-box.local:7420/" {
		t.Errorf("browserURL(remote) = %q", url)
	}

	if _, err := browserURL(configuration{publicHost: "dev-box.local"}); err == nil {
		t.Fatal("browserURL without listen-all should refuse")
	}

	// Behind a proxy the card prints the proxy's address, which carries
	// neither the daemon's scheme nor its port: the loopback URL opens nothing
	// from the machine the browser is on.
	url, err = browserURL(configuration{publicHost: "127.0.0.1", publicURL: "https://yagit.devvm.orb.local"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://yagit.devvm.orb.local/" {
		t.Errorf("browserURL(behind a proxy) = %q", url)
	}

	// A .env the stack refuses to start on is not printed as a place to go.
	if _, err := browserURL(configuration{
		publicHost: "dev-box.local", listenAll: true, publicURL: "https://yagit.devvm.orb.local",
	}); err == nil {
		t.Fatal("browserURL for a proxy beside a widened listen address should refuse")
	}
}

// ---------------------------------------------------------------------------
// What is running, on disk
// ---------------------------------------------------------------------------

func TestStackPIDRoundTrip(t *testing.T) {
	p := newProject(t)

	if err := p.writeStackPID(4242); err != nil {
		t.Fatal(err)
	}
	pid, err := p.readStackPID()
	if err != nil {
		t.Fatal(err)
	}
	if pid != 4242 {
		t.Errorf("readStackPID() = %d, want 4242", pid)
	}
	p.removeStackPID()
	if _, err := p.readStackPID(); err == nil {
		t.Fatal("expected missing pid file after remove")
	}
}

// TestLivingSupervisorSeparatesAliveFromRecorded is the distinction the whole
// lifecycle rests on: a pid file says what was started, never what is running.
func TestLivingSupervisorSeparatesAliveFromRecorded(t *testing.T) {
	p := newProject(t)

	if _, alive := p.livingSupervisor(); alive {
		t.Error("no pid file must mean no supervisor")
	}

	if err := p.writeStackPID(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	pid, alive := p.livingSupervisor()
	if !alive || pid != os.Getpid() {
		t.Errorf("livingSupervisor() = (%d, %v), want this process and true", pid, alive)
	}

	if err := p.writeStackPID(finishedPID(t)); err != nil {
		t.Fatal(err)
	}
	if _, alive := p.livingSupervisor(); alive {
		t.Error("a recorded pid that has exited must not count as alive")
	}
}

// TestLivingStackChildrenKeepsOnlyWhatStillExists covers the recovery path
// `./do down` takes when the supervisor was killed outright: the pids it
// recorded are all that is left to stop, and the dead ones among them must not
// be signalled — those numbers belong to the system again.
func TestLivingStackChildrenKeepsOnlyWhatStillExists(t *testing.T) {
	p := newProject(t)

	if pids := p.livingStackChildren(); pids != nil {
		t.Errorf("no file must mean no children, got %v", pids)
	}

	dead := finishedPID(t)
	if err := p.writeStackChildren([]int{os.Getpid(), dead}); err != nil {
		t.Fatal(err)
	}

	pids := p.livingStackChildren()
	if len(pids) != 1 || pids[0] != os.Getpid() {
		t.Errorf("livingStackChildren() = %v, want only this process", pids)
	}

	p.removeStackChildren()
	if pids := p.livingStackChildren(); pids != nil {
		t.Errorf("children were removed, got %v", pids)
	}
}

// TestStopBackgroundStackIsQuietWhenNothingRuns covers `./do down` on a
// checkout where the last stack died: it reports nothing to stop, and clears
// the record rather than leaving a pid file the next command would believe.
func TestStopBackgroundStackIsQuietWhenNothingRuns(t *testing.T) {
	useNoDaemon(t)
	p := newProject(t)
	if err := p.writeStackPID(finishedPID(t)); err != nil {
		t.Fatal(err)
	}

	stopped, err := p.stopBackgroundStack()
	if err != nil {
		t.Fatalf("stopBackgroundStack: %v", err)
	}
	if stopped {
		t.Error("nothing was running, so nothing was stopped")
	}
	if _, err := p.readStackPID(); err == nil {
		t.Error("the stale pid file must be gone")
	}
}

// TestWaitForStackReadyStopsWhenTheSupervisorDies is why the wait watches the
// process and not only the clock. A stack that failed to start — an
// unreadable YAGIT_ROOT, a missing npm — has nothing left to wait for, and
// two minutes of silence is the worst possible way to say so.
func TestWaitForStackReadyStopsWhenTheSupervisorDies(t *testing.T) {
	useNoDaemon(t)
	p := newProject(t)

	started := time.Now()
	err := p.waitForStackReady(finishedPID(t), stackReadyTimeout)
	if err == nil {
		t.Fatal("a dead supervisor must end the wait")
	}
	if !strings.Contains(err.Error(), "supervisor") {
		t.Errorf("error = %q, want it to name the supervisor", err)
	}
	if waited := time.Since(started); waited > 30*time.Second {
		t.Errorf("waited %s for a process that was already gone", waited)
	}
}

// ---------------------------------------------------------------------------
// The stack log
// ---------------------------------------------------------------------------

// TestTailFileFollowsAcrossATruncation covers `./do logs` surviving a
// `./do up --restart`, which truncates the log underneath it. A reader that
// kept its offset would sit past the end of a file that is now empty, printing
// nothing at all for the rest of the session and never saying why.
func TestTailFileFollowsAcrossATruncation(t *testing.T) {
	p := newProject(t)
	path := p.path(devLogFileName)
	writeFile(t, path, "before the restart\n")

	log, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := log.Close(); err != nil {
			t.Errorf("closing the log: %v", err)
		}
	})

	// Read to the end, exactly as the follow loop does before it sleeps.
	reader := newBufferedReaderAtEnd(t, log)

	if err := os.WriteFile(path, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rewindIfTruncated(log, reader); err != nil {
		t.Fatalf("rewindIfTruncated: %v", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading after the truncation: %v", err)
	}
	if line != "after\n" {
		t.Errorf("read %q, want the first line of the new log", line)
	}
}

// TestTailFileStaysPutWhenTheLogOnlyGrows is the other half: an ordinary
// append must not send the reader back to the top and print the whole file
// again.
func TestTailFileStaysPutWhenTheLogOnlyGrows(t *testing.T) {
	p := newProject(t)
	path := p.path(devLogFileName)
	writeFile(t, path, "first\n")

	log, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := log.Close(); err != nil {
			t.Errorf("closing the log: %v", err)
		}
	})

	reader := newBufferedReaderAtEnd(t, log)

	if err := os.WriteFile(path, []byte("first\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := rewindIfTruncated(log, reader); err != nil {
		t.Fatalf("rewindIfTruncated: %v", err)
	}

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the appended line: %v", err)
	}
	if line != "second\n" {
		t.Errorf("read %q, want the appended line and not the file from the top", line)
	}
}

func TestTailFileWithoutFollowStopsAtTheEnd(t *testing.T) {
	p := newProject(t)
	path := p.path(devLogFileName)
	writeFile(t, path, "one\ntwo\n")

	var out bytes.Buffer
	if err := tailFile(&out, path, false); err != nil {
		t.Fatalf("tailFile: %v", err)
	}
	if out.String() != "one\ntwo\n" {
		t.Errorf("tailFile wrote %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newBufferedReaderAtEnd drains a file the way the follow loop does, leaving
// the reader at end of file with nothing buffered — the one moment at which
// the descriptor's offset is the offset actually consumed.
func newBufferedReaderAtEnd(t *testing.T, file *os.File) *bufio.Reader {
	t.Helper()

	reader := bufio.NewReader(file)
	for {
		_, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return reader
		}
		if err != nil {
			t.Fatalf("draining the log: %v", err)
		}
	}
}

// finishedPID returns the pid of a process that has run and been reaped, which
// is the pid of something that is definitely not alive.
//
// A number picked out of the air would not do: on a busy machine it may well
// belong to somebody, and the tests above would then assert the opposite of
// what they mean.
func finishedPID(t *testing.T) int {
	t.Helper()

	// The test binary itself, asked to run no test at all: it exists on every
	// platform this suite runs on, and it exits immediately.
	command := exec.Command(os.Args[0], "-test.run=^$")
	command.Stdout = nil
	command.Stderr = nil
	if err := command.Start(); err != nil {
		t.Fatalf("starting a throwaway process: %v", err)
	}
	pid := command.Process.Pid
	if err := command.Wait(); err != nil {
		t.Fatalf("waiting for the throwaway process: %v", err)
	}
	return pid
}
