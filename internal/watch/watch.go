// Package watch reports when a repository's git directory changes on disk.
//
// It exists so the interface does not have to ask. yagit reads repositories
// the user is also working in from their own terminal: a commit made there, a
// branch checked out, a rebase finished, all happen without any request
// passing through the daemon.
//
// It watches a bounded set of directories per repository and never the work
// tree — see docs/adr/0008. Depends on fsnotify and on nothing else in the
// project.
package watch

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Change names a repository whose git directory moved. It carries no detail
// about what moved, deliberately: every consumer re-reads git's state anyway,
// and a "reason" nobody acts on is a field that goes stale without anyone
// noticing.
type Change struct {
	RepositoryID string
}

// debounceWindow is how long a repository is left alone after an event before
// a change is announced.
//
// git writes several files for one operation — a commit touches the index,
// HEAD's reflog and a ref — and each arrives as its own event. Without this,
// one commit becomes four announcements and four full re-reads of the
// history. 100 ms is far below what anyone perceives and far above the gap
// between the writes of a single git command.
const debounceWindow = 100 * time.Millisecond

// maxWatchedDirectories caps how many directories one repository may cost.
//
// Refs live in a directory tree — a branch called `feat/a/b` is a file two
// levels down — so the watch has to follow it, and a repository with tens of
// thousands of loose refs would otherwise exhaust the process's descriptors
// and take every other repository's watch down with it. Reaching the cap is
// reported, never silently accepted: an interface that has quietly stopped
// refreshing is worse than one that never refreshed.
const maxWatchedDirectories = 512

// Watcher follows the git directories of the repositories handed to it.
//
// Safe for concurrent use: repositories are opened and closed from HTTP
// handlers while the watch loop is running.
type Watcher struct {
	inner   *fsnotify.Watcher
	logger  *slog.Logger
	changes chan Change

	// follow carries a directory discovered while events are being consumed
	// to the goroutine that adds it.
	//
	// The hand-off is not tidiness, it is the difference between working and
	// hanging. fsnotify's Windows backend answers Add from the same goroutine
	// that delivers events: Add posts a request and waits for a reply that
	// only the read loop can produce, and the read loop is meanwhile blocked
	// writing an event nobody is reading — because the only reader is the
	// goroutine waiting on Add. Both stop, the watcher is dead for every
	// repository, and every later open or close blocks forever on the mutex
	// the stuck handler is holding.
	follow chan followRequest

	mutex sync.Mutex
	// byDirectory maps a watched directory to the repository it belongs to.
	// fsnotify reports paths, and this is what turns one back into an
	// identifier.
	byDirectory map[string]string
	// directoriesOf is the reverse, so closing a repository can take its
	// watches down without walking the disk again — which would not work
	// anyway once the directory is gone.
	directoriesOf map[string][]string
	// pending holds the debounce timer per repository.
	pending map[string]*time.Timer
	// capped remembers the repositories already told they reached the cap, so
	// that a repository growing past it says so once rather than once per
	// event for the rest of the session.
	capped map[string]bool

	closed bool
}

// followRequest is one directory to start watching, and who it belongs to.
type followRequest struct {
	repositoryID string
	directory    string
}

// followBuffer is how many newly created directories may be waiting to be
// watched. A `git fetch` writing a whole namespace at once is the burst this
// covers; beyond it the walk on the next open catches up.
const followBuffer = 256

// New starts a watcher. Close it when the daemon stops.
func New(logger *slog.Logger) (*Watcher, error) {
	inner, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("cannot watch the filesystem: %w", err)
	}

	watcher := &Watcher{
		inner:  inner,
		logger: logger,
		// Buffered: a burst of events during a rebase must not block the
		// fsnotify loop, and a consumer that is momentarily behind must not
		// lose the announcement that follows.
		changes:       make(chan Change, 64),
		follow:        make(chan followRequest, followBuffer),
		byDirectory:   make(map[string]string),
		directoriesOf: make(map[string][]string),
		pending:       make(map[string]*time.Timer),
		capped:        make(map[string]bool),
	}

	go watcher.run()
	go watcher.followNewDirectories()
	return watcher, nil
}

// Changes is the stream of repositories that moved.
func (w *Watcher) Changes() <-chan Change { return w.changes }

// Watch follows a repository's git directories.
//
// gitDirs are the directories git actually writes this repository's state
// into, and there are two of them for a linked worktree: refs, objects and
// config live in the main repository's directory, while HEAD, the index and
// the state of a rebase or a merge live in `<common>/worktrees/<name>`.
// Watching only one of the two is silent — a `git switch` in a linked worktree
// touches nothing under the common directory, so no event ever arrives.
//
// Watching the same repository twice replaces the previous watch rather than
// adding to it, so re-opening a repository whose directory moved does the
// right thing.
func (w *Watcher) Watch(repositoryID string, gitDirs []string) error {
	if len(gitDirs) == 0 {
		return errors.New("no git directory to watch")
	}

	// The disk walk happens outside the lock: it can take a moment on a
	// repository with many refs, and holding the lock through it would stall
	// every event for every other repository.
	directories, err := watchableDirectories(gitDirs)
	if err != nil {
		return err
	}

	// Replacing rather than adding: re-opening a repository whose directory
	// moved must not leave the old watches behind.
	w.Forget(repositoryID)

	// The kernel calls happen outside the lock too, and that is not an
	// optimisation. On Windows fsnotify answers Add from the goroutine that
	// also delivers events; holding the mutex across hundreds of them locks
	// the event loop out of draining, and once fsnotify's own buffer fills,
	// Add and the read loop wait on each other for good.
	added := make([]string, 0, len(directories))
	for _, directory := range directories {
		if err := w.inner.Add(directory); err != nil {
			// Undo what was added, so a half-watched repository does not
			// report half its changes — which looks like working and is not.
			w.unwatch(added)
			return fmt.Errorf("cannot watch %s: %w", directory, err)
		}
		added = append(added, directory)
	}

	if err := w.record(repositoryID, added); err != nil {
		w.unwatch(added)
		return err
	}
	return nil
}

// record takes the bookkeeping for a set of directories already watched.
func (w *Watcher) record(repositoryID string, added []string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closed {
		return errors.New("watcher is closed")
	}

	for _, directory := range added {
		w.byDirectory[directory] = repositoryID
	}
	w.directoriesOf[repositoryID] = added
	delete(w.capped, repositoryID)
	return nil
}

// unwatch drops kernel watches this package no longer owns. Failing to remove
// one is worth a line and nothing more: the descriptor goes with the process,
// and there is no state left to repair.
func (w *Watcher) unwatch(directories []string) {
	for _, directory := range directories {
		if err := w.inner.Remove(directory); err != nil && !errors.Is(err, fsnotify.ErrNonExistentWatch) {
			w.logger.Warn("cannot stop watching a directory",
				"directory", directory, "error", err)
		}
	}
}

// Forget stops following a repository. Safe to call for one that was never
// watched: closing a tab twice is a normal thing for a browser to do.
func (w *Watcher) Forget(repositoryID string) {
	// A directory that is already gone cannot be un-watched, and that is the
	// ordinary case here: `git gc` and a branch deletion both remove
	// directories the watch was on. fsnotify has already dropped them.
	//
	// Outside the lock, like every other call into fsnotify: Remove waits on
	// the same reply from the same goroutine that Add does, and a DELETE of
	// an open repository holding the mutex across a few hundred of them is
	// the deadlock Watch avoids, arrived at from the other direction.
	w.unwatch(w.takeDirectories(repositoryID))
}

// takeDirectories drops a repository from the bookkeeping and hands back what
// it was watching, so the caller can un-watch it without holding the mutex.
func (w *Watcher) takeDirectories(repositoryID string) []string {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	directories := w.directoriesOf[repositoryID]
	for _, directory := range directories {
		delete(w.byDirectory, directory)
	}
	delete(w.directoriesOf, repositoryID)
	delete(w.capped, repositoryID)

	if timer, waiting := w.pending[repositoryID]; waiting {
		timer.Stop()
		delete(w.pending, repositoryID)
	}
	return directories
}

// forgetDirectoryLocked drops one directory that is gone from the bookkeeping.
//
// fsnotify releases the kernel watch on a removed directory by itself, and
// without this the maps keep the entry: `git branch feat/x` followed by `git
// branch -d feat/x` prunes the directory again, and a session of that grows
// the list without bound — and makes the cap below count directories that no
// longer exist, so a repository would eventually refuse watches it has room
// for.
func (w *Watcher) forgetDirectoryLocked(repositoryID, directory string) {
	if _, watched := w.byDirectory[directory]; !watched {
		return
	}
	delete(w.byDirectory, directory)

	remaining := w.directoriesOf[repositoryID][:0]
	for _, kept := range w.directoriesOf[repositoryID] {
		if kept != directory {
			remaining = append(remaining, kept)
		}
	}
	w.directoriesOf[repositoryID] = remaining
}

// Close stops the watcher and the stream it feeds.
//
// It does not close the change channel: the run loop does that, on its way
// out, under the same mutex every send is made under. That is the only
// arrangement where a debounce timer firing at this exact moment cannot send
// on a channel that has just been closed.
func (w *Watcher) Close() error {
	w.mutex.Lock()
	for _, timer := range w.pending {
		timer.Stop()
	}
	w.mutex.Unlock()

	// Closing the inner watcher closes its event channel, which ends run,
	// which closes the follow queue and ends the goroutine draining it.
	return w.inner.Close()
}

// run turns filesystem events into debounced changes.
func (w *Watcher) run() {
	defer func() {
		w.mutex.Lock()
		w.closed = true
		close(w.changes)
		w.mutex.Unlock()

		// The follow queue is closed by its only writer, which is this
		// goroutine, so the worker draining it ends without a second signal.
		// After the flag above, so that a directory still in the queue is
		// dropped rather than handed to a watcher that is already gone.
		close(w.follow)
	}()

	for {
		select {
		case event, open := <-w.inner.Events:
			if !open {
				return
			}
			w.handle(event)

		case err, open := <-w.inner.Errors:
			if !open {
				return
			}
			// An overflowed event queue is the one that matters: events were
			// dropped, so something changed and nobody will be told. Saying
			// so is the whole of what this package can do about it.
			w.logger.Warn("filesystem watch error", "error", err)
		}
	}
}

func (w *Watcher) handle(event fsnotify.Event) {
	if ignorable(event.Name) {
		return
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	repositoryID, watched := w.byDirectory[filepath.Dir(event.Name)]
	if !watched {
		return
	}

	// A new directory under refs holds refs that would otherwise go
	// unwatched: `git branch feat/x` creates `refs/heads/feat`, and every
	// branch under it afterwards is invisible.
	//
	// Queued rather than added here. This is the goroutine that drains
	// fsnotify's events, and adding a watch from it deadlocks the watcher on
	// Windows — see the follow field.
	if event.Has(fsnotify.Create) && isDirectory(event.Name) {
		select {
		case w.follow <- followRequest{repositoryID: repositoryID, directory: event.Name}:
		default:
			// Dropping is better than blocking the event loop, and saying so
			// is better than a watch that quietly covers less than it says.
			w.logger.Warn("cannot follow a new ref directory, the queue is full",
				"directory", event.Name)
		}
	}

	// A directory that is gone takes its watch with it. fsnotify has already
	// dropped the kernel watch; this is the bookkeeping that would otherwise
	// grow for the life of the daemon.
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		w.forgetDirectoryLocked(repositoryID, event.Name)
	}

	w.debounceLocked(repositoryID)
}

// followNewDirectories adds the watches handle() discovered, off the event
// loop.
func (w *Watcher) followNewDirectories() {
	for request := range w.follow {
		w.followOne(request)
	}
}

func (w *Watcher) followOne(request followRequest) {
	w.mutex.Lock()
	switch {
	case w.closed:
		w.mutex.Unlock()
		return
	case w.byDirectory[request.directory] != "":
		// Already watched. Checked before the cap, so that a directory
		// created, removed and created again does not count twice.
		w.mutex.Unlock()
		return
	case len(w.directoriesOf[request.repositoryID]) >= maxWatchedDirectories:
		alreadyTold := w.capped[request.repositoryID]
		w.capped[request.repositoryID] = true
		w.mutex.Unlock()
		if !alreadyTold {
			// Once per repository. The cap exists so that one repository
			// cannot take every other repository's watch down with it, and
			// reaching it while open is the same fact the initial walk
			// reports — it must not become a line per event for the rest of
			// the session.
			w.logger.Warn("this repository has more ref directories than the watch will follow; it will refresh only when asked",
				"repository", request.repositoryID, "limit", maxWatchedDirectories)
		}
		return
	}
	w.mutex.Unlock()

	if err := w.inner.Add(request.directory); err != nil {
		w.logger.Warn("cannot follow a new ref directory",
			"directory", request.directory, "error", err)
		return
	}

	if !w.recordOne(request) {
		// The repository was closed, or re-opened, while the syscall ran.
		// Leaving the watch on a list that is no longer there would hold it
		// for the life of the daemon, since nothing would ever remove it.
		w.unwatch([]string{request.directory})
	}
}

func (w *Watcher) recordOne(request followRequest) bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closed || w.directoriesOf[request.repositoryID] == nil {
		return false
	}
	w.byDirectory[request.directory] = request.repositoryID
	w.directoriesOf[request.repositoryID] = append(
		w.directoriesOf[request.repositoryID], request.directory)
	return true
}

// debounceLocked restarts the quiet period for a repository. The caller holds
// the mutex.
func (w *Watcher) debounceLocked(repositoryID string) {
	if w.closed {
		return
	}
	if timer, waiting := w.pending[repositoryID]; waiting {
		timer.Reset(debounceWindow)
		return
	}
	w.pending[repositoryID] = time.AfterFunc(debounceWindow, func() {
		w.announce(repositoryID)
	})
}

func (w *Watcher) announce(repositoryID string) {
	// The send happens under the mutex, and so does the close in run's defer.
	// It cannot block: it is a non-blocking send on a buffered channel.
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.closed {
		return
	}
	delete(w.pending, repositoryID)

	select {
	case w.changes <- Change{RepositoryID: repositoryID}:
	default:
		// The consumer is behind by a full buffer. Dropping is right: every
		// change says the same thing — "read git again" — so the ones already
		// queued carry this one's meaning too.
		w.logger.Warn("change dropped, the event consumer is behind",
			"repository", repositoryID)
	}
}

// ignorable filters the files git writes and removes during any operation.
//
// `.git/index.lock` in particular appears and vanishes around every write,
// including the writes yagit itself makes: without this, staging one file
// produces an event that makes the interface re-read the state it just
// changed, twice.
func ignorable(path string) bool {
	name := filepath.Base(path)
	return strings.HasSuffix(name, ".lock") || name == "COMMIT_EDITMSG"
}

// watchableDirectories lists what to watch for one repository: each of its git
// directories, and every directory of the refs tree beneath them.
//
// Directories rather than files, and that is not an optimisation. git replaces
// HEAD, the index and packed-refs by writing a temporary file and renaming it
// over the old one; a watch on the file follows the inode that was replaced
// and never fires again. A watch on the containing directory sees the rename.
func watchableDirectories(gitDirs []string) ([]string, error) {
	var directories []string
	seen := make(map[string]bool)

	add := func(directory string) bool {
		if seen[directory] {
			return true
		}
		seen[directory] = true
		directories = append(directories, directory)
		return len(directories) < maxWatchedDirectories
	}

	for _, gitDir := range gitDirs {
		add(gitDir)

		refs := filepath.Join(gitDir, "refs")
		err := filepath.WalkDir(refs, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				// A directory that vanished mid-walk is `git gc` doing its
				// job. The walk carries on rather than failing the whole
				// watch.
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			if !add(path) {
				return fmt.Errorf(
					"more than %d ref directories under %s; the watch would cost more descriptors than it is worth",
					maxWatchedDirectories, refs)
			}
			return nil
		})

		// A repository with no refs directory at all is not an error: `git
		// init` creates one, but a bare repository packed by `git gc` can have
		// every ref in packed-refs and the tree pruned to nothing.
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	return directories, nil
}

func isDirectory(path string) bool {
	// Stat rather than Lstat: git's refs tree holds no symlink, but a
	// repository is a directory anybody can write into, and following the
	// link is what decides whether there is anything to watch.
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
