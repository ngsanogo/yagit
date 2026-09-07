package repo_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// These tests cover the daemon's security boundary. A path coming from the
// network must never give access outside the allowed root, and there are more
// ways out of it than you would expect.

func newRegistry(t *testing.T) (*repo.Registry, string, *git.Runner) {
	t.Helper()

	// The Runner passes HOME through on purpose, so git here reads whatever
	// ~/.gitconfig the machine has. These tests create repositories, which a
	// global init.templateDir or core.hooksPath would quietly change. Point
	// git at no configuration at all and the outcome stops depending on whose
	// machine runs it.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	// EvalSymlinks: on some systems /tmp is itself a symlink. Without that
	// resolution, the root and the resolved paths would never compare equal.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}

	runner := git.NewRunner(nil)
	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry, root, runner
}

func createRepo(t *testing.T, runner *git.Runner, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	if _, err := runner.Run(context.Background(), path, "init", "-b", "main"); err != nil {
		t.Fatalf("git init in %s: %v", path, err)
	}
}

func TestOpenAcceptsRepoUnderRoot(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Path != path {
		t.Errorf("Path = %q, want %q", opened.Path, path)
	}
	if opened.Name != "project" {
		t.Errorf("Name = %q, want project", opened.Name)
	}
	if opened.ID == "" {
		t.Error("the identifier must not be empty")
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	first, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	second, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("different identifiers for the same repository: %q and %q", first.ID, second.ID)
	}
	if len(registry.List()) != 1 {
		t.Errorf("the repository must appear only once, %d entries", len(registry.List()))
	}
}

// Opening a subdirectory of the repository must open the repository, not the
// subdirectory: git is what decides where a repository starts.
func TestOpenWalksUpToRepoRoot(t *testing.T) {
	registry, root, runner := newRegistry(t)
	repoPath := filepath.Join(root, "project")
	createRepo(t, runner, repoPath)

	subdirectory := filepath.Join(repoPath, "src", "internal")
	if err := os.MkdirAll(subdirectory, 0o755); err != nil {
		t.Fatalf("creating the subdirectory: %v", err)
	}

	opened, err := registry.Open(context.Background(), subdirectory)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.Path != repoPath {
		t.Errorf("Path = %q, want the repository root %q", opened.Path, repoPath)
	}
}

func TestOpenRejectsPathOutsideRoot(t *testing.T) {
	registry, _, runner := newRegistry(t)

	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	createRepo(t, runner, outside)

	_, err = registry.Open(context.Background(), outside)
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}

func TestOpenRejectsDotDotEscape(t *testing.T) {
	registry, root, _ := newRegistry(t)

	_, err := registry.Open(context.Background(), filepath.Join(root, "..", "..", "etc"))
	if err == nil {
		t.Fatal("climbing out with .. must be rejected")
	}
	// Depending on whether the target exists, either the root guard or the
	// missing path speaks up first. Both refuse, neither lets anything
	// through, and that is what matters.
	if !errors.Is(err, repo.ErrOutsideRoot) && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The case that string comparison alone would let through: a symlink planted
// inside the root and pointing outside.
func TestOpenRejectsSymlinkEscape(t *testing.T) {
	registry, root, runner := newRegistry(t)

	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	createRepo(t, runner, outside)

	bridge := filepath.Join(root, "bridge")
	if err := os.Symlink(outside, bridge); err != nil {
		t.Fatalf("creating the symlink: %v", err)
	}

	_, err = registry.Open(context.Background(), bridge)
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot — the symlink worked as an escape", err)
	}
}

// "/root-sibling" must not count as contained in "/root".
func TestOpenRejectsSiblingWithSharedPrefix(t *testing.T) {
	registry, root, runner := newRegistry(t)

	sibling := root + "-sibling"
	createRepo(t, runner, sibling)
	t.Cleanup(func() {
		if err := os.RemoveAll(sibling); err != nil {
			t.Errorf("cleaning up %s: %v", sibling, err)
		}
	})

	_, err := registry.Open(context.Background(), sibling)
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}

func TestOpenRejectsDirectoryThatIsNotARepo(t *testing.T) {
	registry, root, _ := newRegistry(t)

	plain := filepath.Join(root, "not-a-repo")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatalf("creating: %v", err)
	}

	_, err := registry.Open(context.Background(), plain)
	if err == nil {
		t.Fatal("a plain directory must not open as a repository")
	}

	// The error must carry git's own detail, not a generic message.
	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error of type %T, want *git.Error as the cause", err)
	}
}

func TestOpenRejectsRelativePath(t *testing.T) {
	registry, _, _ := newRegistry(t)

	_, err := registry.Open(context.Background(), "project/relative")
	if !errors.Is(err, repo.ErrPathNotAbsolute) {
		t.Fatalf("error = %v, want ErrPathNotAbsolute", err)
	}
}

func TestPrepareNewRepositoryPathAcceptsANewPathUnderTheRoot(t *testing.T) {
	registry, root, _ := newRegistry(t)
	destination := filepath.Join(root, "fresh")

	got, err := registry.PrepareNewRepositoryPath(destination)
	if err != nil {
		t.Fatalf("PrepareNewRepositoryPath: %v", err)
	}
	if got != destination {
		t.Fatalf("got %q, want %q", got, destination)
	}
}

func TestPrepareNewRepositoryPathRejectsAnExistingDirectory(t *testing.T) {
	registry, root, _ := newRegistry(t)
	destination := filepath.Join(root, "taken")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := registry.PrepareNewRepositoryPath(destination)
	if !errors.Is(err, repo.ErrDestinationExists) {
		t.Fatalf("error = %v, want ErrDestinationExists", err)
	}
}

func TestPrepareNewRepositoryPathRejectsOutsideRoot(t *testing.T) {
	registry, _, _ := newRegistry(t)
	outside := filepath.Join(t.TempDir(), "escape")

	_, err := registry.PrepareNewRepositoryPath(outside)
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}

func TestPrepareNewRepositoryPathRejectsRelativePath(t *testing.T) {
	registry, _, _ := newRegistry(t)
	_, err := registry.PrepareNewRepositoryPath("relative/clone")
	if !errors.Is(err, repo.ErrPathNotAbsolute) {
		t.Fatalf("error = %v, want ErrPathNotAbsolute", err)
	}
}

// The two ways out of the root, measured rather than argued.
//
// A destination is the one path yagit accepts that does not exist yet, so it
// cannot be resolved the way an opened repository is: the parent is resolved
// and checked, and the leaf is joined onto the RESULT. These two say what that
// buys — `..` cannot climb out, and a symlink under the root cannot carry the
// destination outside it.

func TestPrepareNewRepositoryPathRefusesTraversalOutOfTheRoot(t *testing.T) {
	registry, root, _ := newRegistry(t)
	inside := filepath.Join(root, "sub")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}

	// Not filepath.Join, which would clean the dots away before the call:
	// what a client sends is a string, and the cleaning is what is under test.
	climbing := root + string(filepath.Separator) + "sub" +
		string(filepath.Separator) + ".." + string(filepath.Separator) +
		".." + string(filepath.Separator) + "escape"

	_, err := registry.PrepareNewRepositoryPath(climbing)
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}

func TestPrepareNewRepositoryPathRefusesAParentSymlinkedOutOfTheRoot(t *testing.T) {
	registry, root, _ := newRegistry(t)

	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the outside directory: %v", err)
	}
	link := filepath.Join(root, "way-out")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}

	// The destination itself is under the root by name. Its parent is not,
	// once resolved — which is why the parent is what gets resolved.
	_, err = registry.PrepareNewRepositoryPath(filepath.Join(link, "clone"))
	if !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("error = %v, want ErrOutsideRoot", err)
	}
}

func TestGetOnUnknownIdentifier(t *testing.T) {
	registry, _, _ := newRegistry(t)

	_, err := registry.Get("000000000000")
	if !errors.Is(err, repo.ErrUnknownRepo) {
		t.Fatalf("error = %v, want ErrUnknownRepo", err)
	}
}

// The tests below each stand for a way out of the root that was found by
// reproducing it against the running daemon. They are the reason the boundary
// checks the git directory and not only the work tree.

func TestOpenRejectsAGitDirectoryOutsideTheRoot(t *testing.T) {
	registry, root, runner := newRegistry(t)

	// A repository the boundary is meant to protect, outside the root.
	outside := filepath.Join(t.TempDir(), "private")
	createRepo(t, runner, outside)
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("resolving the outside repository: %v", err)
	}

	// A directory inside the root whose `.git` is a FILE redirecting to it.
	// This is the shape `git worktree` writes, and the shape a hostile
	// repository can carry: the work tree is inside the root, the history is
	// not. Cloning such a repository into the root used to be enough to have
	// yagit serve the target's commits.
	decoy := filepath.Join(root, "decoy")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatalf("creating the decoy: %v", err)
	}
	pointer := []byte("gitdir: " + filepath.Join(resolvedOutside, ".git") + "\n")
	if err := os.WriteFile(filepath.Join(decoy, ".git"), pointer, 0o644); err != nil {
		t.Fatalf("writing the .git file: %v", err)
	}

	if _, err := registry.Open(context.Background(), decoy); !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("Open on a decoy = %v, want ErrOutsideRoot", err)
	}
}

func TestOpenAcceptsABareRepository(t *testing.T) {
	registry, root, runner := newRegistry(t)

	source := filepath.Join(root, "source")
	createRepo(t, runner, source)

	bare := filepath.Join(root, "mirror.git")
	if _, err := runner.Run(context.Background(), root, "clone", "--bare", source, bare); err != nil {
		t.Fatalf("git clone --bare: %v", err)
	}

	// A bare repository has no work tree, so `rev-parse --show-toplevel` fails
	// there. Asking for it was what made every mirror unopenable.
	opened, err := registry.Open(context.Background(), bare)
	if err != nil {
		t.Fatalf("Open on a bare repository: %v", err)
	}
	if !opened.Bare {
		t.Error("the repository should be reported as bare")
	}
	if opened.Path != bare {
		t.Errorf("Path = %q, want %q", opened.Path, bare)
	}
}

func TestOpenKeepsATrailingSpaceInADirectoryName(t *testing.T) {
	// Win32 strips a trailing space from every path component, so the directory
	// this test is about cannot be created there at all. Nothing to assert
	// rather than something wrong.
	if runtime.GOOS == "windows" {
		t.Skip("Windows normalizes a trailing space out of a path component")
	}

	registry, root, runner := newRegistry(t)

	// A directory whose name ends in a space is legal, and git prints it
	// verbatim. Trimming git's output with TrimSpace took the space with the
	// newline and turned a valid path into one that does not exist.
	path := filepath.Join(root, "release ")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open on a directory ending in a space: %v", err)
	}
	if opened.Path != path {
		t.Errorf("Path = %q, want %q", opened.Path, path)
	}
}

func TestGetRefusesARepositoryReplacedSinceItWasOpened(t *testing.T) {
	registry, root, runner := newRegistry(t)

	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Validating a path once is not enough: everything under the root is
	// writable, and the directory can be swapped for a link to a repository
	// outside it between two requests.
	outside := filepath.Join(t.TempDir(), "private")
	createRepo(t, runner, outside)
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatalf("moving the repository aside: %v", err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatalf("planting the symlink: %v", err)
	}

	if _, err := registry.Get(opened.ID); !errors.Is(err, repo.ErrOutsideRoot) {
		t.Fatalf("Get after the swap = %v, want ErrOutsideRoot", err)
	}
}

func TestGetRefusesADifferentRepositoryUnderTheSamePath(t *testing.T) {
	registry, root, runner := newRegistry(t)

	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Swapped for another repository, this one inside the root. The boundary
	// still holds, but the identifier would name a repository nobody opened.
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("removing the repository: %v", err)
	}
	createRepo(t, runner, path)

	if _, err := registry.Get(opened.ID); !errors.Is(err, repo.ErrRepoReplaced) {
		t.Fatalf("Get after the replacement = %v, want ErrRepoReplaced", err)
	}
}

// TestOpenTwiceHoldsOneDescriptor covers the cost of keeping the git directory
// pinned.
//
// Open is idempotent and the repeat call is the common one — a second tab on a
// repository already open. If it took a descriptor before recognising the
// repository, every call would take one and hand it straight back, and the
// error from handing it back would have nowhere to go. A daemon that runs for
// weeks does not get to leak one per call.
func TestOpenTwiceHoldsOneDescriptor(t *testing.T) {
	// Windows holds no descriptor by design — see pin_windows.go — so there is
	// nothing here to leak and nothing to count.
	if runtime.GOOS == "windows" {
		t.Skip("no descriptor is held on Windows")
	}

	descriptors := func() int {
		t.Helper()
		// /proc/self/fd is Linux's answer to "how many do I hold". There is no
		// portable one, and CI is Linux; elsewhere this test has nothing to
		// say rather than something wrong.
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Skipf("no /proc/self/fd on this system: %v", err)
		}
		return len(entries)
	}

	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	if _, err := registry.Open(context.Background(), path); err != nil {
		t.Fatalf("first Open: %v", err)
	}

	// Measured after the first Open: that one is expected to keep a
	// descriptor, and it is every one after it that must not.
	before := descriptors()

	for range 50 {
		if _, err := registry.Open(context.Background(), path); err != nil {
			t.Fatalf("reopening: %v", err)
		}
	}

	if after := descriptors(); after > before {
		t.Errorf("50 repeat Opens took %d more descriptors (%d then %d); "+
			"the idempotent path must take none", after-before, before, after)
	}
}

func TestCloseForgetsARepository(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := registry.Close(opened.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := registry.Get(opened.ID); !errors.Is(err, repo.ErrUnknownRepo) {
		t.Errorf("Get after Close = %v, want ErrUnknownRepo", err)
	}
	if listed := registry.List(); len(listed) != 0 {
		t.Errorf("the repository is still listed: %+v", listed)
	}
}

// TestCloseIsIdempotent covers the two ways a caller reaches it twice: a tab
// shut twice, and a reload racing a click. Neither is an error about a
// repository nobody wanted any more.
func TestCloseIsIdempotent(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for range 3 {
		if err := registry.Close(opened.ID); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	if err := registry.Close("an-identifier-that-names-nothing"); err != nil {
		t.Errorf("closing an unknown identifier reported: %v", err)
	}
}

// TestCloseReleasesTheDescriptor is the half that costs something if it is
// wrong.
//
// The git directory is held open so a recycled inode number cannot pass for
// the same directory. A Close that forgets the entry but keeps the descriptor
// turns a long session into a descriptor leak — the opposite of what closing
// is for.
func TestCloseReleasesTheDescriptor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no descriptor is held on Windows")
	}

	descriptors := func() int {
		t.Helper()
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Skipf("no /proc/self/fd on this system: %v", err)
		}
		return len(entries)
	}

	registry, root, runner := newRegistry(t)

	// One cycle first, so the baseline is taken after whatever the first
	// open-and-close allocates once and keeps.
	first := filepath.Join(root, "warm-up")
	createRepo(t, runner, first)
	warm, err := registry.Open(context.Background(), first)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := registry.Close(warm.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	before := descriptors()

	for index := range 20 {
		path := filepath.Join(root, fmt.Sprintf("project-%d", index))
		createRepo(t, runner, path)

		opened, err := registry.Open(context.Background(), path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if err := registry.Close(opened.ID); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	if after := descriptors(); after > before {
		t.Errorf("20 open-and-close cycles kept %d descriptors (%d then %d)",
			after-before, before, after)
	}
}

// TestAReplacedRepositoryCanBeClosed is why the operation exists at all.
func TestAReplacedRepositoryCanBeClosed(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("removing: %v", err)
	}
	createRepo(t, runner, path)

	if _, err := registry.Get(opened.ID); !errors.Is(err, repo.ErrRepoReplaced) {
		t.Fatalf("Get = %v, want the refusal this test escapes from", err)
	}

	// Refusing is right. Refusing with no way back is not.
	if err := registry.Close(opened.ID); err != nil {
		t.Fatalf("Close after replacement: %v", err)
	}
	if _, err := registry.Open(context.Background(), path); err != nil {
		t.Errorf("reopening after Close: %v", err)
	}
}

func TestTheFilesystemRootContainsEverything(t *testing.T) {
	// The root already ends in a separator, and appending another produced
	// "//", which no absolute path starts with: every path was refused as being
	// outside a root that contains the whole filesystem.
	//
	// The root is discovered rather than written down, so the case is covered
	// on every platform rather than skipped on one. "/" is not an absolute path
	// on Windows — filepath.IsAbs says so — and a hard-coded "C:\\" would be
	// wrong on a runner whose temporary directory lives on another volume. A
	// user who points YAGIT_ROOT at a drive root gets this path either way,
	// which is reason enough not to leave it untested there.
	root := filesystemRootOf(t, t.TempDir())

	registry, err := repo.NewRegistry(root, git.NewRunner(nil))
	if err != nil {
		t.Fatalf("NewRegistry(%q): %v", root, err)
	}

	_, _, runner := newRegistry(t)
	path := filepath.Join(t.TempDir(), "project")
	createRepo(t, runner, path)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}

	if _, err := registry.Open(context.Background(), resolved); err != nil {
		t.Fatalf("Open under a root of %q: %v", root, err)
	}
}

// filesystemRootOf climbs until the path stops changing: "/" on Unix, and the
// volume root the argument sits on — "C:\\" and so on — on Windows.
func filesystemRootOf(t *testing.T, path string) string {
	t.Helper()

	for {
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func TestGitDirsOfAnOrdinaryRepositoryIsTheOneDirectory(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	directories := opened.GitDirs()
	if len(directories) != 1 {
		t.Fatalf("GitDirs = %v, expected one directory", directories)
	}
	if directories[0] != filepath.Join(path, ".git") {
		t.Errorf("GitDirs = %v, expected the repository's own .git", directories)
	}
}

func TestStateDirOfAnOrdinaryRepositoryIsItsGitDirectory(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	opened, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := filepath.Join(path, ".git")
	if got := opened.StateDir(); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}

func TestStateDirOfALinkedWorktreeIsItsOwnStateDirectory(t *testing.T) {
	registry, root, runner := newRegistry(t)
	main := filepath.Join(root, "project")
	createRepo(t, runner, main)

	run := func(arguments ...string) {
		t.Helper()
		if _, err := runner.Run(context.Background(), main, arguments...); err != nil {
			t.Fatalf("git %v: %v", arguments, err)
		}
	}
	run("-c", "user.name=A", "-c", "user.email=a@b.c", "commit", "--allow-empty", "-m", "first")

	linked := filepath.Join(root, "feature")
	run("worktree", "add", linked, "-b", "feature")

	opened, err := registry.Open(context.Background(), linked)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	want := filepath.Join(main, ".git", "worktrees", "feature")
	if got := opened.StateDir(); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(opened.StateDir(), "HEAD")); err != nil {
		t.Errorf("this worktree's own HEAD is not where state would be read: %v", err)
	}
}

func TestGitDirsOfALinkedWorktreeNamesItsOwnStateToo(t *testing.T) {
	registry, root, runner := newRegistry(t)
	main := filepath.Join(root, "project")
	createRepo(t, runner, main)

	run := func(arguments ...string) {
		t.Helper()
		if _, err := runner.Run(context.Background(), main, arguments...); err != nil {
			t.Fatalf("git %v: %v", arguments, err)
		}
	}
	run("-c", "user.name=A", "-c", "user.email=a@b.c", "commit", "--allow-empty", "-m", "first")

	linked := filepath.Join(root, "feature")
	run("worktree", "add", linked, "-b", "feature")

	opened, err := registry.Open(context.Background(), linked)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The two halves are git's own split, and both are load-bearing for the
	// watch: refs, objects and config are shared, while HEAD, the index and
	// the state of a rebase belong to one work tree. Watching only the shared
	// one is silent — a `git switch` in this worktree touches nothing there.
	directories := opened.GitDirs()
	if len(directories) != 2 {
		t.Fatalf("GitDirs = %v, expected the common directory and this worktree's own", directories)
	}
	if directories[0] != filepath.Join(main, ".git") {
		t.Errorf("the first directory is %q, expected the main repository's", directories[0])
	}
	if want := filepath.Join(main, ".git", "worktrees", "feature"); directories[1] != want {
		t.Errorf("the second directory is %q, expected %q", directories[1], want)
	}
	if _, err := os.Stat(filepath.Join(directories[1], "HEAD")); err != nil {
		t.Errorf("this worktree's own HEAD is not where the watch would look: %v", err)
	}
}

// A repository deleted and made again at the same path is a repository, and
// opening it says so.
//
// Every request addressed by identifier refuses one that was replaced — the
// identifier promised a repository and would now serve another — and that
// refusal used to have no way out but closing the tab. Open takes a PATH,
// which it checks against the root before git ever sees it, so it is the one
// call that can honestly say "this one, now".
//
// The case is ordinary rather than exotic: delete a checkout and clone it
// again, or run a test suite that rebuilds its fixtures, against a daemon that
// outlives either.
func TestOpeningARepositoryThatWasReplacedOnDiskOpensTheNewOne(t *testing.T) {
	registry, root, runner := newRegistry(t)
	path := filepath.Join(root, "project")
	createRepo(t, runner, path)

	first, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}

	// Deleted and made again: the same path, a different directory.
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("removing: %v", err)
	}
	createRepo(t, runner, path)

	// The identifier alone still refuses, and must: nothing about that request
	// says which repository the caller meant.
	if _, err := registry.Get(first.ID); !errors.Is(err, repo.ErrRepoReplaced) {
		t.Fatalf("Get after the swap: err = %v, want ErrRepoReplaced", err)
	}

	second, err := registry.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}

	// The identifier is a hash of the path, so it is the same string; what has
	// to have changed is which directory it now resolves to.
	if _, err := registry.Get(second.ID); err != nil {
		t.Fatalf("Get after re-opening: %v", err)
	}

	// And exactly one entry, not two under one identifier.
	if opened := registry.List(); len(opened) != 1 {
		t.Fatalf("List has %d entries after re-opening, want 1", len(opened))
	}
}

func TestClaimNewRepositoryPathCreatesARealDirectory(t *testing.T) {
	registry, root, _ := newRegistry(t)
	destination := filepath.Join(root, "claimed")

	got, err := registry.ClaimNewRepositoryPath(destination)
	if err != nil {
		t.Fatalf("ClaimNewRepositoryPath: %v", err)
	}
	if got != destination && got != filepath.Clean(destination) {
		// EvalSymlinks on the parent can rewrite the path; it must still be
		// under the root and exist as a real directory.
		if _, err := registry.PathWithinRoot(got); err != nil {
			t.Fatalf("claimed path left the root: %v", err)
		}
	}

	info, err := os.Lstat(got)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("claim left %v, want a real directory", info.Mode())
	}

	// A second claim at the same path must refuse — the directory is there.
	if _, err := registry.ClaimNewRepositoryPath(destination); !errors.Is(err, repo.ErrDestinationExists) {
		t.Fatalf("second claim = %v, want ErrDestinationExists", err)
	}
}

func TestClaimNewRepositoryPathRefusesWhenASymlinkAppearsFirst(t *testing.T) {
	registry, root, _ := newRegistry(t)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "symlink-dest")
	if err := os.Symlink(outside, destination); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := registry.ClaimNewRepositoryPath(destination); !errors.Is(err, repo.ErrDestinationExists) {
		t.Fatalf("claim over symlink = %v, want ErrDestinationExists", err)
	}
}
