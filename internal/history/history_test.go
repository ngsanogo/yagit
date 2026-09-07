package history_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/graph"
	"github.com/ngsanogo/yagit/internal/history"
	"github.com/ngsanogo/yagit/internal/repo"
)

// identity is passed to every command that writes an object, so the commits
// these tests produce never depend on who runs them.
var identity = []string{"-c", "user.name=yagit Test", "-c", "user.email=test@yagit.local"}

// ranCommands counts the git subcommands a Runner executed.
//
// It is how these tests tell a reused assignment from a repeated one. Asking
// the store for a counter would prove only that the store believes it cached;
// counting the subprocesses proves git was not run, which is the whole point
// of the cache.
type ranCommands struct {
	mutex sync.Mutex
	count map[string]int
}

func (r *ranCommands) observe(execution git.Execution) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, argument := range execution.Args {
		// Skip the -c pairs the test identity adds, and reach the subcommand.
		if argument == "-c" || argument == identity[1] || argument == identity[3] {
			continue
		}
		r.count[argument]++
		return
	}
}

func (r *ranCommands) of(subcommand string) int {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	return r.count[subcommand]
}

// fixture builds a repository with a real merge, opens it, and returns
// everything the tests need to talk to it.
//
//	main:     A ─── C ─── M
//	            ╲       ╱
//	feature:      B ───
func fixture(t *testing.T) (*history.Store, *repo.Repo, *git.Runner, *ranCommands) {
	t.Helper()

	// git here reads whatever ~/.gitconfig the machine has, because the
	// Runner passes HOME through on purpose. A global commit.gpgsign would
	// make these commits depend on an unlocked signing key.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	// On some systems /tmp is itself a symlink, and the registry compares
	// resolved paths.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}

	ran := &ranCommands{count: make(map[string]int)}
	runner := git.NewRunner(ran.observe)

	dir := filepath.Join(root, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	runGit(t, runner, dir, "init", "-b", "main")
	commit(t, runner, dir, "A")
	runGit(t, runner, dir, "checkout", "-b", "feature")
	commit(t, runner, dir, "B")
	runGit(t, runner, dir, "checkout", "main")
	commit(t, runner, dir, "C")
	// merge writes a commit too, so it needs the identity just as commit does.
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"merge", "--no-ff", "-m", "M", "feature")...)

	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	opened, err := registry.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return history.NewStore(runner), opened, runner, ran
}

func runGit(t *testing.T, runner *git.Runner, dir string, args ...string) {
	t.Helper()
	if _, err := runner.Run(context.Background(), dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}

func commit(t *testing.T, runner *git.Runner, dir, message string) {
	t.Helper()
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"commit", "--allow-empty", "-m", message)...)
}

// headSHA reads the commit HEAD points at, through the same runner the store
// uses, so the count of what ran stays a count of what the store did.
func headSHA(t *testing.T, runner *git.Runner, dir string) string {
	t.Helper()
	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	return head.SHA
}

// headDecoration returns the "HEAD -> branch" badge git attached to a commit,
// or the empty string when HEAD is on no branch. It is the visible half of
// this bug: the badge the interface paints comes from the walk, so a reused
// walk paints a stale one.
func headDecoration(commits []git.Commit) string {
	for _, commit := range commits {
		for _, ref := range commit.Refs {
			if strings.HasPrefix(ref, "HEAD -> ") {
				return ref
			}
		}
	}
	return ""
}

func TestWindowDescribesTheWholeHistory(t *testing.T) {
	store, opened, _, _ := fixture(t)

	window, err := store.Window(context.Background(), opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}

	if window.Total != 4 {
		t.Fatalf("Total = %d, expected 4 (A, B, C, M)", window.Total)
	}
	if len(window.Commits) != 4 {
		t.Fatalf("got %d rows, expected 4", len(window.Commits))
	}
	if window.First != 0 {
		t.Errorf("First = %d, expected 0", window.First)
	}
	if window.Width != 2 {
		t.Errorf("Width = %d: one branch beside the trunk needs two columns", window.Width)
	}
	if subject := window.Commits[0].Subject; subject != "M" {
		t.Errorf("the newest row is %q, expected the merge M", subject)
	}
	if lane := window.LaneOf(0); lane != 0 {
		t.Errorf("the merge sits in column %d, expected 0", lane)
	}

	// The merge has two parents, so two lines leave its dot, and one of them
	// runs down a column of its own.
	leaving := 0
	for _, edge := range window.Edges {
		if edge.From == 0 {
			leaving++
		}
	}
	if leaving != 2 {
		t.Errorf("%d lines leave the merge, expected 2", leaving)
	}
}

func TestWindowIsClampedToTheHistory(t *testing.T) {
	store, opened, _, _ := fixture(t)
	ctx := context.Background()

	past, err := store.Window(ctx, opened, 1000, 10, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window past the end must not fail: %v", err)
	}
	if len(past.Commits) != 0 {
		t.Errorf("expected no row past the end, got %d", len(past.Commits))
	}
	if past.Total != 4 {
		t.Errorf("Total = %d, expected 4 even past the end", past.Total)
	}

	tail, err := store.Window(ctx, opened, 3, 200, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if len(tail.Commits) != 1 {
		t.Errorf("a window of 200 starting at row 3 of 4 holds 1 row, got %d", len(tail.Commits))
	}
	if tail.First != 3 {
		t.Errorf("First = %d, expected 3", tail.First)
	}

	// A negative index is the interface asking for the top of the list, which
	// is where clamping lands it.
	head, err := store.Window(ctx, opened, -5, 2, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if head.First != 0 || len(head.Commits) != 2 {
		t.Errorf("First = %d with %d rows, expected 0 and 2", head.First, len(head.Commits))
	}
}

func TestRefreshDoesNotAskForRefsTwice(t *testing.T) {
	store, opened, _, ran := fixture(t)
	ctx := context.Background()

	if _, err := store.Window(ctx, opened, 0, 2, git.ScopeHead, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}
	if count := ran.of("for-each-ref"); count != 1 {
		t.Fatalf("the first window ran git for-each-ref %d times, expected 1", count)
	}
}

func TestASecondWindowReusesTheAssignment(t *testing.T) {
	store, opened, _, ran := fixture(t)
	ctx := context.Background()

	if _, err := store.Window(ctx, opened, 0, 2, git.ScopeHead, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}
	after := ran.of("log")
	if after != 1 {
		t.Fatalf("the first window ran git log %d times, expected 1", after)
	}

	for range 5 {
		if _, err := store.Window(ctx, opened, 2, 2, git.ScopeHead, nil); err != nil {
			t.Fatalf("Window: %v", err)
		}
	}
	if again := ran.of("log"); again != 1 {
		t.Errorf("git log ran %d times over six windows: the history is being re-read", again)
	}
}

func TestACommitInvalidatesTheAssignment(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	before, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}

	commit(t, runner, opened.Path, "D")

	after, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if after.Total != before.Total+1 {
		t.Errorf("Total = %d after a commit, expected %d", after.Total, before.Total+1)
	}
	if after.Commits[0].Subject != "D" {
		t.Errorf("the newest row is %q, expected the new commit D", after.Commits[0].Subject)
	}
	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times, expected 2: once before the commit and once after", runs)
	}
}

// TestABranchDeletionInvalidatesTheAssignment covers the case a fingerprint of
// the tip alone would miss: nothing was added, and the history is smaller.
func TestABranchDeletionInvalidatesTheAssignment(t *testing.T) {
	store, opened, runner, _ := fixture(t)
	ctx := context.Background()

	runGit(t, runner, opened.Path, "checkout", "-b", "spare")
	commit(t, runner, opened.Path, "E")
	runGit(t, runner, opened.Path, "checkout", "main")

	with, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if with.Total != 5 {
		t.Fatalf("Total = %d with the spare branch, expected 5 (A, B, C, M, E)", with.Total)
	}

	runGit(t, runner, opened.Path, "branch", "-D", "spare")

	without, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if without.Total != 4 {
		t.Errorf("Total = %d once the branch is gone, expected 4: E is unreachable", without.Total)
	}
}

func TestForgetReleasesTheHistory(t *testing.T) {
	store, opened, _, ran := fixture(t)
	ctx := context.Background()

	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}
	store.Forget(opened.ID)
	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}

	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times, expected 2: a forgotten history is read again", runs)
	}
}

func TestARepositoryWithNoCommitYet(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}
	runner := git.NewRunner(nil)
	dir := filepath.Join(root, "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	runGit(t, runner, dir, "init", "-b", "main")

	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	opened, err := registry.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Both scopes, because they read the emptiness from different state:
	// one from an unborn HEAD, the other from a repository with no ref. A
	// scope that got it wrong would answer git's "does not have any commits
	// yet" as a 500 on the most ordinary repository there is.
	for _, scope := range []git.Scope{git.ScopeHead, git.ScopeAll} {
		window, err := history.NewStore(runner).Window(context.Background(), opened, 0, 200, scope, nil)
		if err != nil {
			t.Fatalf("%s: a repository with no commit is a normal state, not an error: %v", scope, err)
		}
		if window.Total != 0 || len(window.Commits) != 0 || window.Width != 0 {
			t.Errorf("%s: expected an empty picture, got %+v", scope, window)
		}
	}
}

// TestACheckoutRedrawsTheHeadDecoration is the case a fingerprint of the refs
// cannot see under --all: every commit the walk covers is the same one, and
// the only thing that moved is which of them git writes `HEAD ->` beside.
//
// Its pair is TestACheckoutChangesWhatTheDefaultScopeWalks, where the commits
// themselves change. Two tests because they fail for different reasons: this
// one on a stale decoration, that one on a stale set of commits.
func TestACheckoutRedrawsTheHeadDecoration(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	before, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if decoration := headDecoration(before.Commits); decoration != "HEAD -> main" {
		t.Fatalf("the fixture leaves HEAD on main, got %q", decoration)
	}

	runGit(t, runner, opened.Path, "checkout", "feature")

	after, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times, expected 2: a checkout writes no ref, so the refs alone cannot see it", runs)
	}
	if decoration := headDecoration(after.Commits); decoration != "HEAD -> feature" {
		t.Errorf("decoration = %q, expected HEAD -> feature", decoration)
	}
}

// TestACheckoutOntoAnotherNameForTheSameCommitInvalidates is the case a
// fingerprint of refs cannot see at all: every ref points exactly where it
// pointed, and the only thing that moved is the name HEAD is known by.
func TestACheckoutOntoAnotherNameForTheSameCommitInvalidates(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	runGit(t, runner, opened.Path, "branch", "also-here", "main")
	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}

	runGit(t, runner, opened.Path, "checkout", "also-here")

	after, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times, expected 2: the ref list is identical and the history still has to be walked again", runs)
	}
	if decoration := headDecoration(after.Commits); decoration != "HEAD -> also-here" {
		t.Errorf("decoration = %q, expected HEAD -> also-here", decoration)
	}
}

// TestDetachingHEADInvalidatesTheAssignment is the same case with nothing at
// all left to compare: the same commit, under no branch name.
func TestDetachingHEADInvalidatesTheAssignment(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}

	runGit(t, runner, opened.Path, "checkout", "--detach", "HEAD")

	after, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times, expected 2: detaching changes the decoration and no ref", runs)
	}
	if decoration := headDecoration(after.Commits); decoration != "" {
		t.Errorf("decoration = %q, expected none: HEAD is on no branch any more", decoration)
	}
	if refs := after.Commits[0].Refs; !slices.Contains(refs, "HEAD") {
		t.Errorf("decoration on the tip = %v, expected a bare HEAD beside main", refs)
	}
}

// TestAnUnbornHEADDoesNotThrashTheCache covers the repository whose HEAD
// points at no commit: ReadHEAD answers zero values there. Treating that as a
// new value on every refresh would walk the whole history again for every page
// of rows the interface asks for.
func TestAnUnbornHEADDoesNotThrashTheCache(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	// An orphan branch is the reachable version of that state: HEAD names a
	// ref that does not exist yet, while every other ref stays put.
	runGit(t, runner, opened.Path, "checkout", "--orphan", "unborn")

	for range 3 {
		if _, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil); err != nil {
			t.Fatalf("a repository with no HEAD is a normal state, not an error: %v", err)
		}
	}
	if runs := ran.of("log"); runs != 1 {
		t.Errorf("git log ran %d times over three windows, expected 1: an unreadable HEAD is a stable value", runs)
	}
}

// TestHEADIsReadWithTheRefs pins where the extra command lives. Reading HEAD
// costs a subprocess, and it belongs in the refresh beside for-each-ref.
//
// How many commands ReadHEAD spends per refresh is internal/git's business and
// may change there. What this test holds is the shape: the cost tracks the
// refreshes, and neither the rows a window asked for nor the commits the walk
// returned. A read that had drifted into the paging path or the commit loop
// would scale with one of those, and two measurements taken under different
// conditions would then disagree.
func TestHEADIsReadWithTheRefs(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	// Opening a repository runs rev-parse of its own, and so does every
	// measurement before this one, so each is a growth from the count at the
	// time rather than an absolute.
	perRefresh := func(windows, rows int) int {
		t.Helper()
		refsAtStart, revParseAtStart := ran.of("for-each-ref"), ran.of("rev-parse")

		for range windows {
			if _, err := store.Window(ctx, opened, 0, rows, git.ScopeAll, nil); err != nil {
				t.Fatalf("Window: %v", err)
			}
		}

		refreshes := ran.of("for-each-ref") - refsAtStart
		if refreshes != windows {
			t.Fatalf("git for-each-ref ran %d times over %d windows, expected %d",
				refreshes, windows, windows)
		}
		revParses := ran.of("rev-parse") - revParseAtStart
		if revParses%refreshes != 0 {
			t.Fatalf("git rev-parse ran %d times over %d refreshes: HEAD is not read once per refresh",
				revParses, refreshes)
		}
		return revParses / refreshes
	}

	narrow := perRefresh(5, 2)
	if narrow == 0 {
		t.Fatal("no git rev-parse ran during a refresh: HEAD is not being read at all")
	}

	if wide := perRefresh(3, 100); wide != narrow {
		t.Errorf("a refresh cost %d rev-parse calls for a two-row window and %d for the whole history: the read follows the rows, not the refresh",
			narrow, wide)
	}

	// Twice the history, same question: the cost cannot follow the commits
	// either.
	for _, message := range []string{"D", "E", "F", "G"} {
		commit(t, runner, opened.Path, message)
	}
	if longer := perRefresh(3, 100); longer != narrow {
		t.Errorf("a refresh cost %d rev-parse calls over four commits and %d over eight: the read is inside the commit loop",
			narrow, longer)
	}
}

// TestLocateFindsACommitByItsSHA covers the read behind following a reference:
// the row is what the list scrolls to, and a location that ignored the order
// would still pass on the tip alone.
func TestLocateFindsACommitByItsSHA(t *testing.T) {
	store, opened, _, _ := fixture(t)
	ctx := context.Background()

	window, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}

	for row, commit := range window.Commits {
		located, found, err := store.Locate(ctx, opened, commit.SHA, git.ScopeAll, nil)
		if err != nil {
			t.Fatalf("Locate %s: %v", commit.SHA, err)
		}
		if !found {
			t.Fatalf("%s is in the history and was not found", commit.SHA)
		}
		if located.Row != row {
			t.Errorf("row = %d, expected %d", located.Row, row)
		}
		if located.Commit.SHA != commit.SHA {
			t.Errorf("sha = %q, expected %q", located.Commit.SHA, commit.SHA)
		}
		if located.Lane != window.LaneOf(row) {
			t.Errorf("lane = %d, expected %d", located.Lane, window.LaneOf(row))
		}
	}
}

// TestLocateOfACommitOutsideTheHistoryIsNotFound covers a reference pointing
// at something the walk never reached. Not an error: the answer is that there
// is no row to scroll to, and the interface has to be able to say so.
func TestLocateOfACommitOutsideTheHistoryIsNotFound(t *testing.T) {
	store, opened, _, _ := fixture(t)

	located, found, err := store.Locate(context.Background(), opened,
		"0000000000000000000000000000000000000000", git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if found {
		t.Errorf("a SHA outside the history was located at %+v", located)
	}
}

// TestLocateIsAnsweredWithinTheScopeItWasAsked is the case the interface has
// to be able to explain: a branch nobody merged is reachable from a ref and
// from no checkout, so the sidebar lists it and the default walk does not hold
// it. Answering from the other scope's assignment would scroll the list to a
// row holding a different commit.
func TestLocateIsAnsweredWithinTheScopeItWasAsked(t *testing.T) {
	store, opened, runner, _ := fixture(t)
	ctx := context.Background()

	// A commit on a branch that is left behind: reachable from refs/heads/spare
	// and from nothing main can see.
	runGit(t, runner, opened.Path, "checkout", "-b", "spare")
	commit(t, runner, opened.Path, "E")
	unmerged := headSHA(t, runner, opened.Path)
	runGit(t, runner, opened.Path, "checkout", "main")

	if _, found, err := store.Locate(ctx, opened, unmerged, git.ScopeAll, nil); err != nil {
		t.Fatalf("Locate under every ref: %v", err)
	} else if !found {
		t.Error("a commit on a ref was not found under the scope that walks every ref")
	}

	if located, found, err := store.Locate(ctx, opened, unmerged, git.ScopeHead, nil); err != nil {
		t.Fatalf("Locate under the checkout: %v", err)
	} else if found {
		t.Errorf("a commit no checkout reaches was located at %+v", located)
	}
}

func TestLaneOfOutsideTheHistoryLeavesThePicture(t *testing.T) {
	store, opened, _, _ := fixture(t)

	window, err := store.Window(context.Background(), opened, 0, 1, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}

	// Inside the history but outside the window: an edge leaving the window
	// bends onto these, so they have to answer.
	if lane := window.LaneOf(3); lane == graph.Absent {
		t.Errorf("row 3 is in the history and its column must be known")
	}
	for _, row := range []int{-1, 4, 1000} {
		if lane := window.LaneOf(row); lane != graph.Absent {
			t.Errorf("row %d is outside the history: column %d, expected Absent", row, lane)
		}
	}
}

// TestTheDefaultScopeDrawsWhatIsCheckedOut: the picture is the current
// branch unless somebody asks for every ref.
func TestTheDefaultScopeDrawsWhatIsCheckedOut(t *testing.T) {
	store, opened, runner, _ := fixture(t)
	ctx := context.Background()

	runGit(t, runner, opened.Path, "checkout", "-b", "spare")
	commit(t, runner, opened.Path, "E")
	runGit(t, runner, opened.Path, "checkout", "main")

	checkedOut, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if checkedOut.Total != 4 {
		t.Errorf("Total = %d on main, expected 4: E lives only on spare", checkedOut.Total)
	}

	everything, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if everything.Total != 5 {
		t.Errorf("Total = %d across every ref, expected 5 (A, B, C, M, E)", everything.Total)
	}
}

// TestEachScopeKeepsItsOwnAssignment is what the cache key is for. Held under
// the repository alone, the second call would find a fingerprint that had not
// changed — no ref moved, HEAD did not move — and answer the first scope's
// walk to a caller that asked for the other one.
func TestEachScopeKeepsItsOwnAssignment(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	runGit(t, runner, opened.Path, "checkout", "-b", "spare")
	commit(t, runner, opened.Path, "E")
	runGit(t, runner, opened.Path, "checkout", "main")

	first, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeAll, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}

	again, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if again.Total != first.Total {
		t.Errorf("Total = %d back on the first scope, expected %d", again.Total, first.Total)
	}

	// Two walks, not three: the answer to the first scope was still held when
	// the caller came back to it, which is what makes the control on screen a
	// switch rather than a reload.
	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times over three windows, expected 2", runs)
	}
}

// TestACheckoutChangesWhatTheDefaultScopeWalks covers what a fingerprint of
// the refs alone would miss: a checkout moves no ref, and the default scope
// walks from HEAD, so the picture would stay on the branch you just left.
func TestACheckoutChangesWhatTheDefaultScopeWalks(t *testing.T) {
	store, opened, runner, _ := fixture(t)
	ctx := context.Background()

	runGit(t, runner, opened.Path, "checkout", "-b", "spare")
	commit(t, runner, opened.Path, "E")
	runGit(t, runner, opened.Path, "checkout", "main")

	onMain, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if onMain.Commits[0].Subject != "M" {
		t.Fatalf("the newest row on main is %q, expected the merge M", onMain.Commits[0].Subject)
	}

	runGit(t, runner, opened.Path, "checkout", "spare")

	onSpare, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("Window: %v", err)
	}
	if onSpare.Commits[0].Subject != "E" {
		t.Errorf("the newest row after the checkout is %q, expected E",
			onSpare.Commits[0].Subject)
	}
}

// TestADetachedHEADIsAHistory: nothing is checked out but a commit, which is
// the state `git checkout <sha>` and every rebase in progress leave behind.
func TestADetachedHEADIsAHistory(t *testing.T) {
	store, opened, runner, _ := fixture(t)
	ctx := context.Background()

	runGit(t, runner, opened.Path, "checkout", "--detach", "HEAD~1")

	detached, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil)
	if err != nil {
		t.Fatalf("a detached HEAD is a normal state, not an error: %v", err)
	}
	if detached.Total != 2 {
		t.Errorf("Total = %d one commit back from the merge, expected 2 (A, C)", detached.Total)
	}
}

// Under git.ScopeRefs the chosen set IS the scope: main alone and main with a
// topic branch are two histories, and a key that ignored the choice would
// answer the second from the first's assignment — every column wrong, and
// wrong in a way that looks right.
func TestTwoChoicesOfRefsAreTwoAssignments(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	// The fixture's feature branch was merged, so main already reaches it.
	// A branch that was never merged is the case the two older scopes cannot
	// tell apart, and the one somebody opens the picker for.
	runGit(t, runner, opened.Path, "checkout", "-b", "topic")
	commit(t, runner, opened.Path, "E")
	runGit(t, runner, opened.Path, "checkout", "main")

	main, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs, []string{"refs/heads/main"})
	if err != nil {
		t.Fatalf("Window over main: %v", err)
	}

	both, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs,
		[]string{"refs/heads/main", "refs/heads/topic"})
	if err != nil {
		t.Fatalf("Window over main and topic: %v", err)
	}

	if both.Total != main.Total+1 {
		t.Errorf("main walked %d commits and main with the topic walked %d; "+
			"the topic adds exactly its own", main.Total, both.Total)
	}
	if runs := ran.of("log"); runs != 2 {
		t.Errorf("git log ran %d times, expected 2: one walk per chosen set", runs)
	}
}

// The order the boxes were ticked in is nobody's business, and keying on it
// would pay for the whole log again to answer with identical rows.
func TestTheOrderRefsWereChosenInIsNotPartOfTheKey(t *testing.T) {
	store, opened, _, ran := fixture(t)
	ctx := context.Background()

	one := []string{"refs/heads/main", "refs/heads/feature"}
	other := []string{"refs/heads/feature", "refs/heads/main", "refs/heads/main"}

	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs, one); err != nil {
		t.Fatalf("Window: %v", err)
	}
	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs, other); err != nil {
		t.Fatalf("Window: %v", err)
	}

	if runs := ran.of("log"); runs != 1 {
		t.Errorf("git log ran %d times, expected 1: the same refs in another sequence, "+
			"and a repeat among them, are the same walk", runs)
	}
}

// A row is a position in one walk. Locate under a chosen set that does not
// reach a commit has to answer "not there" rather than the row it holds under
// another set — which would scroll the list to a different commit.
func TestLocateUnderAChosenSetAnswersForThatSetOnly(t *testing.T) {
	store, opened, runner, _ := fixture(t)
	ctx := context.Background()

	feature, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs, []string{"refs/heads/feature"})
	if err != nil {
		t.Fatalf("Window over feature: %v", err)
	}

	// The tip of the branch that was chosen: the first row of its own walk,
	// and a row further down under any wider one.
	tip := feature.Commits[0].SHA

	located, found, err := store.Locate(ctx, opened, tip, git.ScopeRefs,
		[]string{"refs/heads/feature"})
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if !found {
		t.Fatal("the tip of the chosen branch was not in the walk over it")
	}
	if located.Row != 0 {
		t.Errorf("row = %d, expected the tip of the only chosen ref to be the first row",
			located.Row)
	}

	// The same commit, under a set that does not reach it. Not an error: the
	// answer is that there is no row to scroll to, and the interface says so.
	runGit(t, runner, opened.Path, "checkout", "-b", "elsewhere", "main~2")
	if _, found, err := store.Locate(ctx, opened, tip, git.ScopeRefs,
		[]string{"refs/heads/elsewhere"}); err != nil {
		t.Fatalf("Locate: %v", err)
	} else if found {
		t.Error("a commit outside the chosen walk was given a row in it")
	}
}

// The picked sets are the one part of the key a person can invent, and each
// entry holds a whole assigned history. Without a ceiling, a minute in the
// picker on a repository with twenty references holds every walk it produced
// until the tab is closed.
func TestPickedWalksAreBounded(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	// Enough branches to overrun the ceiling several times over. They all
	// point at the same commit, so every walk is cheap and the only thing
	// that differs between them is the key.
	names := make([]string, 0, 12)
	for index := range 12 {
		name := fmt.Sprintf("topic-%d", index)
		runGit(t, runner, opened.Path, "branch", name)
		names = append(names, "refs/heads/"+name)
	}

	for _, name := range names {
		if _, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs, []string{name}); err != nil {
			t.Fatalf("Window over %s: %v", name, err)
		}
	}

	if held := store.Held(opened.ID); held > 4 {
		t.Errorf("the store holds %d walks of this repository after twelve choices; "+
			"the picked ones are supposed to have a ceiling", held)
	}

	// And the ceiling is a ceiling, not a wall: the walk just asked for is
	// still there, so pressing the same choice twice does not read git twice.
	before := ran.of("log")
	last := []string{names[len(names)-1]}
	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeRefs, last); err != nil {
		t.Fatalf("Window: %v", err)
	}
	if again := ran.of("log"); again != before {
		t.Errorf("git log ran again for the choice that was just made: %d then %d", before, again)
	}
}

// What must survive the ceiling: the two scopes the switch on screen switches
// between. They are two keys, they cannot grow, and paying for either walk
// again on every press is the thing this package exists to avoid.
func TestTheUnpickedScopesAreNotEvicted(t *testing.T) {
	store, opened, runner, ran := fixture(t)
	ctx := context.Background()

	for _, scope := range []git.Scope{git.ScopeHead, git.ScopeAll} {
		if _, err := store.Window(ctx, opened, 0, 100, scope, nil); err != nil {
			t.Fatalf("Window under %v: %v", scope, err)
		}
	}
	after := ran.of("log")

	for index := range 12 {
		name := fmt.Sprintf("topic-%d", index)
		runGit(t, runner, opened.Path, "branch", name)
		if _, err := store.Window(
			ctx, opened, 0, 100, git.ScopeRefs, []string{"refs/heads/" + name}); err != nil {
			t.Fatalf("Window over %s: %v", name, err)
		}
	}

	// Every branch made above moved the refs, so both walks are stale and
	// re-read once each — which is the fingerprint doing its job. What would
	// say they had been evicted is a THIRD read, of a walk that was dropped
	// and rebuilt from nothing.
	before := ran.of("log")
	for _, scope := range []git.Scope{git.ScopeHead, git.ScopeAll} {
		if _, err := store.Window(ctx, opened, 0, 100, scope, nil); err != nil {
			t.Fatalf("Window under %v: %v", scope, err)
		}
	}
	if walks := ran.of("log") - before; walks != 2 {
		t.Errorf("head and all cost %d walks after the picker was used, expected 2", walks)
	}
	if after == 0 {
		t.Fatal("the first two windows read nothing")
	}
}

// Closing a tab still drops everything, ceiling or no ceiling.
func TestForgetDropsThePickedWalksToo(t *testing.T) {
	store, opened, _, _ := fixture(t)
	ctx := context.Background()

	if _, err := store.Window(
		ctx, opened, 0, 100, git.ScopeRefs, []string{"refs/heads/main"}); err != nil {
		t.Fatalf("Window: %v", err)
	}
	if _, err := store.Window(ctx, opened, 0, 100, git.ScopeHead, nil); err != nil {
		t.Fatalf("Window: %v", err)
	}

	store.Forget(opened.ID)

	if held := store.Held(opened.ID); held != 0 {
		t.Errorf("the store holds %d walks after the repository was closed", held)
	}
}
