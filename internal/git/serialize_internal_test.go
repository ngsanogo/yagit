package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWhichCommandsTakeTheIndexLock(t *testing.T) {
	cases := []struct {
		args   []string
		writes bool
		why    string
	}{
		{[]string{"add", "--", ":(literal)a"}, true, "staging writes the index"},
		{[]string{"commit", "--file=-"}, true, "committing writes the index"},
		{[]string{"switch", "main"}, true, "a checkout rewrites the index and the work tree"},
		{[]string{"restore", "--worktree", "--", "a"}, true, "restoring writes files"},
		{[]string{"clean", "--force", "--", "a"}, true, "cleaning deletes files"},
		{[]string{"rebase", "--continue"}, true, "a rebase replays commits through the index"},

		{[]string{"log", "--topo-order"}, false, "the history is a read"},
		{[]string{"status", "--porcelain"}, false, "status is a read, and GIT_OPTIONAL_LOCKS=0 keeps it one"},
		{[]string{"diff", "--cached"}, false, "a diff is a read"},
		{[]string{"rev-parse", "HEAD"}, false, "resolving a revision is a read"},
		{[]string{"fetch", "origin"}, false, "a fetch moves refs and never touches the index"},
		{[]string{"push", "origin"}, false, "a push sends objects and never touches the index"},
		{[]string{"pull", "origin"}, true, "a pull writes the index the way a merge does"},

		// The three commands that are a read or a write depending on what
		// follows. Locking their read form would hold the repository while the
		// side panels render, which is exactly what must keep working during a
		// rebase.
		{[]string{"apply", "--check", "--cached"}, false, "--check asks whether a patch would apply"},
		{[]string{"apply", "--cached"}, true, "applying a patch writes the index"},
		{[]string{"stash", "list"}, false, "the stack can be read"},
		{[]string{"stash", "show"}, false, "one stash can be read"},
		{[]string{"stash", "push"}, true, "pushing a stash writes the index"},
		{[]string{"stash"}, true, "bare `git stash` is a push"},
		{[]string{"submodule", "status"}, false, "submodule status is a read"},
		{[]string{"submodule", "update"}, true, "updating a submodule checks it out"},

		{nil, false, "no arguments is not a command"},
	}

	for _, testCase := range cases {
		t.Run(strings.Join(testCase.args, " "), func(t *testing.T) {
			if got := writesTheIndex(testCase.args); got != testCase.writes {
				t.Errorf("writesTheIndex(%v) = %v, want %v — %s",
					testCase.args, got, testCase.writes, testCase.why)
			}
		})
	}
}

// The subcommand is found, not assumed, so a global flag added in front of a
// command later cannot take it out of the lock in silence.
func TestTheSubcommandIsFoundPastAnyGlobalFlag(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"add", "--"}, "add"},
		{[]string{"--no-pager", "commit"}, "commit"},
		// -c takes its value as the next argument: reading past the flag
		// without knowing that returns `core.pager=cat` as the subcommand.
		{[]string{"-c", "core.pager=cat", "reset", "--hard"}, "reset"},
		{[]string{"-C", "/repo", "clean", "--force"}, "clean"},
		{[]string{"--git-dir", "/repo/.git", "status"}, "status"},
		// A flag carrying its own value needs no skip.
		{[]string{"--git-dir=/repo/.git", "switch", "main"}, "switch"},
		{nil, ""},
		{[]string{"--no-pager"}, ""},
	}

	for _, testCase := range cases {
		if got := subcommandOf(testCase.args); got != testCase.want {
			t.Errorf("subcommandOf(%v) = %q, want %q", testCase.args, got, testCase.want)
		}
	}

	// And the whole point: a write stays a write behind a global flag.
	if !writesTheIndex([]string{"-c", "core.pager=cat", "commit", "--file=-"}) {
		t.Error("a commit behind a global flag was not serialised")
	}
}

func TestTwoWritesToOneRepositoryDoNotOverlap(t *testing.T) {
	locks := newDirLocks()

	release, err := locks.acquire(context.Background(), "/repo")
	if err != nil {
		t.Fatalf("the first write could not take an uncontended lock: %v", err)
	}

	var (
		mutex  sync.Mutex
		second bool
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		releaseSecond, err := locks.acquire(context.Background(), "/repo")
		if err != nil {
			t.Errorf("the second write never got in: %v", err)
			return
		}
		mutex.Lock()
		second = true
		mutex.Unlock()
		releaseSecond()
	}()

	// While the first holds it, the second must not be through.
	time.Sleep(20 * time.Millisecond)
	mutex.Lock()
	got := second
	mutex.Unlock()
	if got {
		t.Fatal("two writes held the same repository at once: this is what index.lock refuses")
	}

	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the second write never woke up after the first released")
	}
}

func TestTwoRepositoriesDoNotWaitOnEachOther(t *testing.T) {
	locks := newDirLocks()

	release, err := locks.acquire(context.Background(), "/one")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// A different directory is a different index. Serialising them would make
	// one slow repository stop every other tab.
	other, err := locks.acquire(context.Background(), "/two")
	if err != nil {
		t.Fatalf("a write to another repository was made to wait: %v", err)
	}
	other()
}

func TestAWriteGivesUpRatherThanHangingForever(t *testing.T) {
	locks := newDirLocks()

	release, err := locks.acquire(context.Background(), "/repo")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	// Its own short deadline stands in for busyWait: what is being checked is
	// that the wait ends in a sentence rather than in a held request.
	waiting, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := locks.acquire(waiting, "/repo"); err == nil {
		t.Fatal("a contended write returned success")
	} else if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, ErrWriteInProgress) {
		t.Errorf("err = %v, want the caller's own deadline or ErrWriteInProgress", err)
	}
}

// The lock is only worth anything if it is actually taken on the path every
// command travels. This runs two writes through Exec itself, against a git
// slow enough that an overlap would be visible.
func TestExecSerialisesTwoWritesToOneRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}

	directory := t.TempDir()
	fake := filepath.Join(directory, "git")
	// Records that it started, waits, records that it finished. If two of
	// these overlap, the file holds "start start", which no lock would allow.
	script := "#!/bin/sh\n" +
		"printf 'start ' >> " + filepath.Join(directory, "order") + "\n" +
		"sleep 0.3\n" +
		"printf 'end ' >> " + filepath.Join(directory, "order") + "\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{binary: fake, timeout: time.Minute, writes: newDirLocks()}

	var group sync.WaitGroup
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			// `add` is a write, so this is the path the lock is on.
			if _, err := runner.Exec(context.Background(), Command{
				Dir:  directory,
				Args: []string{"add", "--", ":(literal)a"},
			}); err != nil {
				t.Errorf("Exec: %v", err)
			}
		}()
	}
	group.Wait()

	order, err := os.ReadFile(filepath.Join(directory, "order"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(order)); got != "start end start end start end" {
		t.Errorf("the writes overlapped: %q", got)
	}
}

// And the other half: a read must go through while a write holds the
// repository, or the side panels would stop answering during a rebase.
func TestExecLetsAReadThroughDuringAWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}

	directory := t.TempDir()
	fake := filepath.Join(directory, "git")
	// Slow for the write and instant for the read: otherwise the read's own
	// duration is what the assertion below would be measuring.
	script := "#!/bin/sh\ncase \"$1\" in add) sleep 0.3 ;; esac\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := &Runner{binary: fake, timeout: time.Minute, writes: newDirLocks()}

	started := make(chan struct{})
	go func() {
		close(started)
		_, _ = runner.Exec(context.Background(), Command{
			Dir:  directory,
			Args: []string{"add", "--", ":(literal)a"},
		})
	}()
	<-started
	time.Sleep(50 * time.Millisecond)

	// `status` is a read. GIT_OPTIONAL_LOCKS=0 means it takes no git lock, and
	// nothing here may add one.
	begun := time.Now()
	if _, err := runner.Exec(context.Background(), Command{
		Dir:  directory,
		Args: []string{"status", "--porcelain"},
	}); err != nil {
		t.Fatalf("a read was refused while a write ran: %v", err)
	}
	if waited := time.Since(begun); waited > 250*time.Millisecond {
		t.Errorf("the read waited %s for the write: reads must not be serialised", waited)
	}
}
