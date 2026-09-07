package git

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
)

// A repository is not concurrent, and a daemon is.
//
// git guards `.git/index` with a lock file and refuses when it is taken:
// "Another git process seems to be running in this repository". yagit serves
// several tabs on one repository, plus whatever the user is doing in their own
// terminal, so two writes landing together is not an edge case — it is two
// quick clicks, or a commit while a checkout is still running.
//
// What the user saw was git's sentence, verbatim, naming a process that had
// already exited and telling them to delete a file inside .git. That is the
// right answer for a lock somebody else holds and the wrong one for a lock
// yagit is holding against itself: the daemon knows what is running, so it can
// wait for it, or say what it is.
//
// So writes are serialised per repository directory here, at the one place
// every git command in this project passes through. Reads are untouched:
// GIT_OPTIONAL_LOCKS=0 already means they take no lock, which is what keeps the
// history, the diffs and the refs answering while a rebase runs.
//
// Safe from deadlocking against itself, and that is a property of Exec rather
// than of this file: Exec never calls Exec. The only project code that runs
// inside it is the progress callback — a non-blocking send on a channel — and
// the execution observer, which logs and publishes. Neither runs git.

// ErrWriteInProgress: another write is holding this repository's index.
//
// Distinct from the API's errRepositoryBusy, which says the repository is in
// the middle of a rebase or a merge — a state that persists on disk and that
// the user finishes or aborts. This one is a lock held for the length of one
// command, and the answer to it is to wait a moment.
//
// Returned instead of waiting indefinitely. A rebase legitimately holds the
// index for minutes, and a user who clicked Commit during one is owed an answer
// rather than a spinner: this says what is true, in yagit's own words, instead
// of git's advice to go and delete a lock file.
var ErrWriteInProgress = errors.New(
	"another operation is already running in this repository; wait for it to finish and try again")

// busyWait is how long a write waits for the repository before saying so.
//
// Ordinary writes finish in well under a second, so this covers the case that
// actually happens — two clicks, or a status poll landing on a commit — without
// making anybody wait on one that will not finish soon. Past it the operation
// is a rebase, a big checkout or a long hook, and the honest answer is to name
// it rather than to hold the request open for the ten minutes those are
// allowed.
const busyWait = 10 * time.Second

// writesTheIndex reports whether a git command takes .git/index.lock.
//
// Read off the arguments rather than declared at each call site, and that is
// deliberate: a hundred call sites is a hundred chances to forget, and the one
// that forgets is discovered as an intermittent failure on somebody else's
// machine. Here it is one list, and a command added to the project is covered
// by it without anyone remembering to.
//
// The subcommand is found rather than assumed to be first. Nothing in this
// package puts a global flag before it today — configuration travels in the
// environment, see pinnedSettings — but a `-c` added in front of one command
// later would take that command out of the lock silently, and a lock that goes
// missing without a word is the failure this exists to prevent.
func writesTheIndex(args []string) bool {
	switch subcommandOf(args) {
	case "add", "commit", "switch", "checkout", "restore", "reset",
		"merge", "rebase", "cherry-pick", "revert", "rm", "mv", "clean", "am",
		// pull integrates into the work tree and takes .git/index.lock the
		// same way merge does. Leaving it out let a commit start on top of a
		// pull and fail on the lock — or worse, interleave — which is the
		// intermittent failure this list exists to prevent.
		"pull":
		return true

	case "apply":
		// `--check` asks whether a patch would apply and writes nothing.
		return !slices.Contains(args, "--check")

	case "stash":
		// The stack can be read as well as pushed to. `git stash` with no
		// subcommand is a push, which is why the empty case writes.
		return len(args) < 2 || (args[1] != "list" && args[1] != "show")

	case "submodule":
		return len(args) < 2 || args[1] != "status"
	}

	return false
}

// subcommandOf is the first argument that is not a global flag.
//
// git takes a handful of options before the subcommand, and five of them take
// their value as the NEXT argument rather than after an equals sign — reading
// past a flag without knowing that would return the value as the subcommand.
// The list is git's own; anything else beginning with a dash carries its value
// with it.
func subcommandOf(args []string) string {
	takesAValue := map[string]bool{
		"-c": true, "-C": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--exec-path": true, "--super-prefix": true,
	}

	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "-") {
			return argument
		}
		if takesAValue[argument] {
			index++
		}
	}
	return ""
}

// dirLocks serialises the writes to each repository directory.
//
// One slot per directory, held in a map that only ever grows — by one entry per
// repository the daemon has written to, which is the number of repositories
// somebody opened. Reclaiming them would mean reference counting a mutex, and
// the leak it would save is a few dozen bytes for the life of a process.
type dirLocks struct {
	mutex sync.Mutex
	slots map[string]chan struct{}
}

func newDirLocks() *dirLocks {
	return &dirLocks{slots: make(map[string]chan struct{})}
}

// acquire takes the directory's slot, or gives up with ErrWriteInProgress.
//
// A buffered channel rather than a sync.Mutex, because a mutex cannot be waited
// on with a deadline: the whole point here is to stop waiting and say so.
func (l *dirLocks) acquire(ctx context.Context, dir string) (release func(), err error) {
	l.mutex.Lock()
	slot, present := l.slots[dir]
	if !present {
		slot = make(chan struct{}, 1)
		l.slots[dir] = slot
	}
	l.mutex.Unlock()

	// Fast path first, so an uncontended write pays nothing for the timer.
	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	default:
	}

	waiting, cancel := context.WithTimeout(ctx, busyWait)
	defer cancel()

	select {
	case slot <- struct{}{}:
		return func() { <-slot }, nil
	case <-waiting.Done():
		// The caller's own context ending is a different thing from the
		// repository being busy, and the user is owed the difference: one
		// means "you went away", the other means "something else is running".
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrWriteInProgress
	}
}
