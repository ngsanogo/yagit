package watch_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/watch"
)

// The watcher's promise is narrow and worth pinning exactly: something
// changed inside the git directory, so read git again. What these tests check
// is that the announcement arrives when it should, once rather than four
// times, and never for a repository nobody is watching.

// waitFor is how long a test waits for an announcement.
//
// Generous on purpose. The debounce is 100 ms and a loaded CI machine can add
// a great deal to that; a tight bound here would turn a correct watcher into
// a flaky suite, which is the failure that gets tests deleted.
const waitFor = 5 * time.Second

func newWatcher(t *testing.T) *watch.Watcher {
	t.Helper()

	watcher, err := watch.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if err := watcher.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return watcher
}

// gitDirectory builds the shape of a git directory: enough of one for the
// watcher, which never reads its contents.
func gitDirectory(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), ".git")
	for _, sub := range []string{"refs/heads", "refs/tags", "refs/remotes/origin"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	write(t, filepath.Join(dir, "HEAD"), "ref: refs/heads/main\n")
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// awaitChange waits for one announcement and returns the repository it names.
func awaitChange(t *testing.T, watcher *watch.Watcher) string {
	t.Helper()
	select {
	case change := <-watcher.Changes():
		return change.RepositoryID
	case <-time.After(waitFor):
		t.Fatal("no change announced")
		return ""
	}
}

// expectQuiet asserts nothing is announced within a window.
//
// Shorter than waitFor by design: this one is proving a negative, and every
// millisecond of it is time the suite spends doing nothing.
func expectQuiet(t *testing.T, watcher *watch.Watcher, window time.Duration) {
	t.Helper()
	select {
	case change := <-watcher.Changes():
		t.Fatalf("expected silence, got a change for %q", change.RepositoryID)
	case <-time.After(window):
	}
}

func TestAChangeToTheIndexIsAnnounced(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	write(t, filepath.Join(dir, "index"), "not really an index")

	if got := awaitChange(t, watcher); got != "repo-1" {
		t.Errorf("change named %q, expected repo-1", got)
	}
}

func TestARefWrittenIntoANestedDirectoryIsAnnounced(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// refs/remotes/origin exists at watch time, so it is already followed.
	write(t, filepath.Join(dir, "refs/remotes/origin/main"), "aaaa\n")

	if got := awaitChange(t, watcher); got != "repo-1" {
		t.Errorf("change named %q, expected repo-1", got)
	}
}

func TestARefUnderADirectoryCreatedAfterwardsIsAnnounced(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// `git branch feat/x` creates refs/heads/feat and then the ref inside it.
	// Without following the new directory, every branch under it would be
	// invisible — and this is the case a watch set up once and never extended
	// gets wrong.
	nested := filepath.Join(dir, "refs/heads/feat")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The directory's own creation is a change, and it is announced. Drain it
	// before writing the ref, so what the next wait sees is the ref.
	awaitChange(t, watcher)

	write(t, filepath.Join(nested, "x"), "bbbb\n")

	if got := awaitChange(t, watcher); got != "repo-1" {
		t.Errorf("change named %q, expected repo-1", got)
	}
}

func TestSeveralWritesInOneBurstAreAnnouncedOnce(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// What one commit looks like from here: the index, HEAD's reflog, a ref.
	// Announcing each would re-read the whole history four times over.
	write(t, filepath.Join(dir, "index"), "1")
	write(t, filepath.Join(dir, "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(dir, "refs/heads/main"), "cccc\n")

	awaitChange(t, watcher)
	expectQuiet(t, watcher, 300*time.Millisecond)
}

func TestTheIndexLockIsNotAChange(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// It appears and vanishes around every write, yagit's own included.
	// Announcing it would make the interface re-read the state it just
	// changed, twice.
	write(t, filepath.Join(dir, "index.lock"), "")
	if err := os.Remove(filepath.Join(dir, "index.lock")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	expectQuiet(t, watcher, 300*time.Millisecond)
}

func TestAForgottenRepositoryAnnouncesNothing(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	watcher.Forget("repo-1")

	write(t, filepath.Join(dir, "index"), "after forgetting")

	expectQuiet(t, watcher, 300*time.Millisecond)
}

func TestForgettingSomethingNeverWatchedIsFine(t *testing.T) {
	// A browser closes a tab twice. That must not be an error, and it must
	// not be a panic either.
	newWatcher(t).Forget("never-opened")
}

func TestWatchingTheSameRepositoryTwiceReplacesTheFirstWatch(t *testing.T) {
	watcher := newWatcher(t)
	first := gitDirectory(t)
	second := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{first}); err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if err := watcher.Watch("repo-1", []string{second}); err != nil {
		t.Fatalf("Watch again: %v", err)
	}

	// The old directory is no longer followed: a repository re-opened
	// somewhere else must not go on reporting where it used to be.
	write(t, filepath.Join(first, "index"), "old place")
	expectQuiet(t, watcher, 300*time.Millisecond)

	write(t, filepath.Join(second, "index"), "new place")
	if got := awaitChange(t, watcher); got != "repo-1" {
		t.Errorf("change named %q, expected repo-1", got)
	}
}

func TestTwoRepositoriesAreToldApart(t *testing.T) {
	watcher := newWatcher(t)
	one := gitDirectory(t)
	two := gitDirectory(t)

	if err := watcher.Watch("repo-1", []string{one}); err != nil {
		t.Fatalf("Watch one: %v", err)
	}
	if err := watcher.Watch("repo-2", []string{two}); err != nil {
		t.Fatalf("Watch two: %v", err)
	}

	write(t, filepath.Join(two, "index"), "only the second")

	if got := awaitChange(t, watcher); got != "repo-2" {
		t.Errorf("change named %q, expected repo-2", got)
	}
}

func TestWatchingSomethingThatIsNotThereFailsLoudly(t *testing.T) {
	watcher := newWatcher(t)

	// A watch that cannot be established has to be reported. An interface
	// that has silently stopped refreshing is worse than one that never
	// refreshed.
	if err := watcher.Watch("repo-1", []string{filepath.Join(t.TempDir(), "absent")}); err == nil {
		t.Error("expected an error for a git directory that does not exist")
	}
}

func TestARepositoryWithNoRefsDirectoryIsStillWatched(t *testing.T) {
	watcher := newWatcher(t)

	// A bare repository whose refs are all packed can have the tree pruned to
	// nothing. That is not an error, and packed-refs still moves.
	dir := filepath.Join(t.TempDir(), "bare.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	write(t, filepath.Join(dir, "packed-refs"), "# pack-refs with: peeled\n")

	if got := awaitChange(t, watcher); got != "repo-1" {
		t.Errorf("change named %q, expected repo-1", got)
	}
}

func TestClosingTwiceIsFine(t *testing.T) {
	watcher, err := watch.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := watcher.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := watcher.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	// The stream ends rather than hanging: a consumer ranging over it has to
	// be able to stop.
	select {
	case _, open := <-watcher.Changes():
		if open {
			t.Error("a change arrived after Close")
		}
	case <-time.After(waitFor):
		t.Error("the change stream was never closed")
	}
}

func TestAChangeInTheSecondGitDirectoryIsAnnounced(t *testing.T) {
	watcher := newWatcher(t)
	common := gitDirectory(t)

	// A linked worktree's own state lives in <common>/worktrees/<name>: HEAD,
	// the index, ORIG_HEAD, MERGE_HEAD and a rebase's state are all there and
	// none of them is under the common directory. A watch on the common one
	// alone never sees a `git switch` in that worktree — no error, no log
	// line, just a graph that stays wrong.
	linked := filepath.Join(common, "worktrees", "feature")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, filepath.Join(linked, "HEAD"), "ref: refs/heads/feature\n")

	if err := watcher.Watch("repo-1", []string{common, linked}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	write(t, filepath.Join(linked, "HEAD"), "ref: refs/heads/other\n")

	if got := awaitChange(t, watcher); got != "repo-1" {
		t.Errorf("change named %q, expected repo-1", got)
	}
}

func TestWatchRefusesToWatchNothing(t *testing.T) {
	watcher := newWatcher(t)
	if err := watcher.Watch("repo-1", nil); err == nil {
		t.Fatal("expected a refusal: a repository with no git directory has nothing to follow")
	}
}

func TestRefDirectoriesComingAndGoingKeepBeingFollowed(t *testing.T) {
	watcher := newWatcher(t)
	dir := gitDirectory(t)
	heads := filepath.Join(dir, "refs", "heads")

	if err := watcher.Watch("repo-1", []string{dir}); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// `git branch feat/x` creates refs/heads/feat and `git branch -d feat/x`
	// prunes it again. A session of that must not leave the bookkeeping
	// growing behind it — the cap on how many directories one repository may
	// cost counts entries, and entries for directories that no longer exist
	// would eventually refuse a watch there is room for.
	nested := filepath.Join(heads, "feat")
	for round := range 3 {
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatalf("round %d: mkdir: %v", round, err)
		}
		if got := awaitChange(t, watcher); got != "repo-1" {
			t.Fatalf("round %d: change named %q", round, got)
		}

		// The directory has to be watched before it is taken away again, and
		// the watch is added off the event loop.
		write(t, filepath.Join(nested, "x"), "sha\n")
		if got := awaitChange(t, watcher); got != "repo-1" {
			t.Fatalf("round %d: a ref under the new directory was not announced: %q", round, got)
		}

		if err := os.RemoveAll(nested); err != nil {
			t.Fatalf("round %d: remove: %v", round, err)
		}
		if got := awaitChange(t, watcher); got != "repo-1" {
			t.Fatalf("round %d: the removal was not announced: %q", round, got)
		}
	}
}
