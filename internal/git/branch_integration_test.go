package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Branches, against the real binary.
//
// The argument lists are the whole of this file's subject. Every one of them
// is a claim about how git parses what it is handed — that `--` makes a
// reference a reference, that `--no-guess` stops a switch from creating a
// branch, that `--detach` puts HEAD on a commit rather than on the name that
// led to it — and none of those claims can be checked without running git.

// switchable is a repository with somewhere to switch to: two branches, a tag,
// and a remote-tracking branch that no local branch shadows.
//
//	main    A ─── B
//	side    A ─── S      (tag: v1, on S)
//	origin/upstream-only ─── on A, in refs/remotes only
func switchable(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "A")
	first := headOf(t, runner, dir).SHA

	runGit(t, runner, dir, "switch", "-c", "side")
	commitEmpty(t, runner, dir, "S")
	runGit(t, runner, dir, "tag", "v1")

	runGit(t, runner, dir, "switch", "main")
	commitEmpty(t, runner, dir, "B")

	// Written straight into refs/remotes, because a remote-tracking branch is
	// what a checkout must NOT silently turn into a local one — and setting up
	// a second repository to fetch from would prove the same thing at ten
	// times the cost.
	runGit(t, runner, dir, "update-ref", "refs/remotes/origin/upstream-only", first)

	return dir, runner
}

// headOf is where HEAD is: the commit, the name, and whether it is on one.
//
// The whole reading rather than the SHA, because the tests that move HEAD are
// about all three — a detach leaves the commit and takes the name away, and a
// rename takes the name with it and leaves the commit.
func headOf(t *testing.T, runner *git.Runner, dir string) git.HEAD {
	t.Helper()
	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	return head
}

func TestSwitchMovesHEADOntoABranch(t *testing.T) {
	dir, runner := switchable(t)

	if err := runner.Switch(context.Background(), dir, "side"); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.Name != "side" {
		t.Errorf("HEAD is on %q, expected side", head.Name)
	}
	if head.Detached {
		t.Error("switching to a branch must not detach HEAD")
	}
}

// A switch never creates a branch, which is what --no-guess buys.
//
// Without the flag this is git's "DWIM" case: one remote-tracking branch
// matches the name, so git creates a local branch, sets its upstream and
// checks it out — three things, from a request that named one, and the created
// branch then appears in the sidebar as if somebody had made it.
func TestSwitchRefusesToInventABranchFromARemote(t *testing.T) {
	dir, runner := switchable(t)

	err := runner.Switch(context.Background(), dir, "upstream-only")
	if err == nil {
		t.Fatal("switching to a name with no local branch must fail, not create one")
	}

	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected a git failure, got %T: %v", err, err)
	}
	if !strings.Contains(strings.Join(failure.Args, " "), "--no-guess") {
		t.Errorf("the command that ran was %v, expected --no-guess in it", failure.Args)
	}

	refs, err := runner.ForEachRef(context.Background(), dir)
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	for _, ref := range refs {
		if ref.Name == "refs/heads/upstream-only" {
			t.Fatal("a refused switch left a local branch behind")
		}
	}
}

// The reference reaches git after `--`, so a branch named like an option is a
// branch. git refuses this one — no ref may start with a dash — and what
// matters is WHICH refusal: a sentence about the name, not about an unknown
// option.
func TestSwitchNamesADashedReferenceRatherThanReadingItAsAnOption(t *testing.T) {
	dir, runner := switchable(t)

	err := runner.Switch(context.Background(), dir, "--force")
	if err == nil {
		t.Fatal("expected git to refuse a branch name starting with a dash")
	}
	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected a git failure, got %T: %v", err, err)
	}
	if strings.Contains(failure.Stderr, "unknown option") {
		t.Errorf("git read the reference as an option: %s", failure.Stderr)
	}
	if !strings.Contains(failure.Stderr, "--force") {
		t.Errorf("git's refusal does not name the reference: %s", failure.Stderr)
	}
}

// A tag is not a place HEAD can sit, which is the whole reason detaching is a
// separate operation rather than a flag nobody sets.
func TestDetachPutsHEADOnTheCommitATagNames(t *testing.T) {
	dir, runner := switchable(t)

	if err := runner.Detach(context.Background(), dir, "v1"); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	head := headOf(t, runner, dir)
	if !head.Detached {
		t.Fatalf("HEAD is on %q, expected it detached", head.Name)
	}

	// The commit the tag names, and not merely "some commit": a detach that
	// landed anywhere else would still report itself detached.
	tagged, err := runner.Run(context.Background(), dir, "rev-parse", "v1^{commit}")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if want := strings.TrimSpace(string(tagged)); head.SHA != want {
		t.Errorf("HEAD is at %s, expected %s", head.SHA, want)
	}
}

// Detaching at a branch leaves HEAD on the commit, not on the branch. Which is
// exactly why the interface does not offer it for a local branch: it is the
// one case where "check out" and "detach here" differ in what the user gets.
func TestDetachAtABranchLeavesTheBranchBehind(t *testing.T) {
	dir, runner := switchable(t)
	before := headOf(t, runner, dir).SHA

	if err := runner.Detach(context.Background(), dir, "main"); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	head := headOf(t, runner, dir)
	if !head.Detached {
		t.Error("detaching at a branch must leave HEAD detached")
	}
	if head.SHA != before {
		t.Errorf("HEAD moved to %s, expected it to stay at %s", head.SHA, before)
	}
}

// git refuses a switch that would overwrite uncommitted work, and the refusal
// has to arrive whole: the interface shows the files git named, and there is
// nothing else on screen that could tell the user what is in the way.
func TestSwitchReportsWhatUncommittedWorkWouldBeOverwritten(t *testing.T) {
	dir, runner := switchable(t)

	// The same file, different content on each branch, and a third version on
	// disk: the one shape git cannot carry across.
	writeFile(t, dir, "shared.txt", "from main\n")
	runGit(t, runner, dir, "add", "--", "shared.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "main file")...)

	runGit(t, runner, dir, "switch", "side")
	writeFile(t, dir, "shared.txt", "from side\n")
	runGit(t, runner, dir, "add", "--", "shared.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "side file")...)

	writeFile(t, dir, "shared.txt", "uncommitted\n")

	err := runner.Switch(context.Background(), dir, "main")
	if err == nil {
		t.Fatal("expected git to refuse a switch that would overwrite the work tree")
	}

	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected a git failure, got %T: %v", err, err)
	}
	if !strings.Contains(failure.Stderr, "shared.txt") {
		t.Errorf("git's refusal does not name the file in the way: %s", failure.Stderr)
	}

	// And nothing moved: the file on disk is still what the user wrote.
	content, readErr := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if readErr != nil {
		t.Fatalf("reading back: %v", readErr)
	}
	if string(content) != "uncommitted\n" {
		t.Errorf("the work tree is %q, expected the refused switch to leave it alone", content)
	}
}

func TestSwitchAndDetachRefuseAnEmptyReference(t *testing.T) {
	dir, runner := switchable(t)

	if err := runner.Switch(context.Background(), dir, ""); !errors.Is(err, git.ErrNoRef) {
		t.Errorf("Switch(\"\") = %v, expected ErrNoRef", err)
	}
	if err := runner.Detach(context.Background(), dir, ""); !errors.Is(err, git.ErrNoRef) {
		t.Errorf("Detach(\"\") = %v, expected ErrNoRef", err)
	}
}

// branchable is a repository with two commits and a branch that is not
// checked out.
//
//	main   A ─── B
//	side   A ─── S
func branchable(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "A")

	runGit(t, runner, dir, "switch", "-c", "side")
	commitEmpty(t, runner, dir, "S")

	runGit(t, runner, dir, "switch", "main")
	commitEmpty(t, runner, dir, "B")

	return dir, runner
}

// branchNames is every local branch, sorted, so a test can assert on the whole
// set rather than on the one branch it was thinking about. An operation that
// creates something extra is as wrong as one that creates nothing.
func branchNames(t *testing.T, runner *git.Runner, dir string) []string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir,
		"for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		t.Fatalf("for-each-ref: %v", err)
	}
	names := strings.Fields(string(output))
	slices.Sort(names)
	return names
}

func TestCreateBranchStartsWhereItIsTold(t *testing.T) {
	dir, runner := branchable(t)

	if err := runner.CreateBranch(context.Background(), dir, "from-side", "side"); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	if got, want := branchNames(t, runner, dir), []string{"from-side", "main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want %v", got, want)
	}

	// And it did not move HEAD. Creating a branch and standing on it are two
	// requests, and this is the one that was made.
	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if head.Name != "main" {
		t.Errorf("HEAD on %q after a create, want it left on main", head.Name)
	}
}

func TestCreateBranchWithNoStartPointUsesHEAD(t *testing.T) {
	dir, runner := branchable(t)

	if err := runner.CreateBranch(context.Background(), dir, "here", ""); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Left to git rather than resolved here, so the command in the log panel
	// is the one a person would have typed.
	if args := git.CreateBranchArgs("here", ""); slices.Contains(args, "HEAD") {
		t.Errorf("CreateBranchArgs named HEAD explicitly: %v", args)
	}
}

// The whole point of the file, for creation: a name that is also an option.
//
// Without the separator, `git branch -m release` in a repository on main
// renames main to release. It creates nothing, it reports success, and the
// branch the user was standing on is gone under a name they typed into a
// "new branch" box.
func TestCreateBranchRefusesAnOptionShapedNameByName(t *testing.T) {
	dir, runner := branchable(t)

	err := runner.CreateBranch(context.Background(), dir, "-m", "release")

	if err == nil {
		t.Fatal("CreateBranch(-m) succeeded, want git to refuse the name")
	}
	if !strings.Contains(err.Error(), "not a valid branch name") {
		t.Errorf("error = %v, want git refusing '-m' as a NAME rather than reading it as an option", err)
	}

	// The proof that it was refused rather than obeyed: main is still main.
	if got, want := branchNames(t, runner, dir), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want the two it started with", got)
	}
}

func TestCreateAndSwitchLandsOnTheNewBranch(t *testing.T) {
	dir, runner := branchable(t)

	if err := runner.CreateAndSwitch(context.Background(), dir, "working", "side"); err != nil {
		t.Fatalf("CreateAndSwitch: %v", err)
	}

	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if head.Name != "working" {
		t.Errorf("HEAD on %q, want working", head.Name)
	}
	if head.Detached {
		t.Error("HEAD detached after creating a branch to stand on")
	}
}

// The separator goes in a different place here, and the test is what stops
// somebody moving it: `git switch --create -- feature` reads feature as the
// start point, leaves --create without a name, and fails on a repository where
// no such reference exists — or, worse, succeeds somewhere it does.
func TestCreateAndSwitchRefusesAnOptionShapedName(t *testing.T) {
	dir, runner := branchable(t)

	err := runner.CreateAndSwitch(context.Background(), dir, "-m", "")

	if err == nil {
		t.Fatal("CreateAndSwitch(-m) succeeded, want git to refuse the name")
	}
	if !strings.Contains(err.Error(), "not a valid branch name") {
		t.Errorf("error = %v, want git refusing '-m' as a name", err)
	}
	if got, want := branchNames(t, runner, dir), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want the two it started with", got)
	}
}

func TestRenameBranchKeepsHEADWithIt(t *testing.T) {
	dir, runner := branchable(t)

	if err := runner.RenameBranch(context.Background(), dir, "main", "trunk"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}

	if got, want := branchNames(t, runner, dir), []string{"side", "trunk"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want %v", got, want)
	}

	// Renaming the branch you are standing on is the one operation in this
	// file that moves HEAD, and git is what moves it.
	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if head.Name != "trunk" {
		t.Errorf("HEAD on %q after renaming the branch it was on, want trunk", head.Name)
	}
}

// `-m` and not `-M`, checked by making the collision it refuses.
func TestRenameBranchRefusesToOverwriteAnExistingBranch(t *testing.T) {
	dir, runner := branchable(t)

	err := runner.RenameBranch(context.Background(), dir, "main", "side")

	if err == nil {
		t.Fatal("rename onto an existing branch succeeded, want git to refuse it")
	}
	if got, want := branchNames(t, runner, dir), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want both still there", got)
	}
}

func TestDeleteBranchRefusesUnmergedWorkUntilForced(t *testing.T) {
	dir, runner := branchable(t)

	// side holds a commit main cannot reach, which is exactly what -d exists
	// to refuse.
	if err := runner.DeleteBranch(context.Background(), dir, "side", false); err == nil {
		t.Fatal("DeleteBranch without force succeeded on an unmerged branch")
	}
	if got, want := branchNames(t, runner, dir), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want side still there", got)
	}

	if err := runner.DeleteBranch(context.Background(), dir, "side", true); err != nil {
		t.Fatalf("DeleteBranch(force): %v", err)
	}
	if got, want := branchNames(t, runner, dir), []string{"main"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want only main", got)
	}
}

func TestDeleteBranchRefusesTheBranchHEADIsOn(t *testing.T) {
	dir, runner := branchable(t)

	if err := runner.DeleteBranch(context.Background(), dir, "main", true); err == nil {
		t.Fatal("deleting the checked-out branch succeeded, want git to refuse it")
	}
	if got, want := branchNames(t, runner, dir), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want both still there", got)
	}
}

func TestBranchOperationsRefuseAnEmptyName(t *testing.T) {
	dir, runner := branchable(t)
	ctx := context.Background()

	// Refused here rather than handed to git: `git branch` with no name lists
	// the branches and exits 0, which is a success for an operation that did
	// not happen.
	for name, err := range map[string]error{
		"create":            runner.CreateBranch(ctx, dir, "  ", ""),
		"create-and-switch": runner.CreateAndSwitch(ctx, dir, "", ""),
		"rename-from":       runner.RenameBranch(ctx, dir, "", "new"),
		"rename-to":         runner.RenameBranch(ctx, dir, "main", " "),
		"delete":            runner.DeleteBranch(ctx, dir, "", true),
	} {
		if !errors.Is(err, git.ErrNoBranchName) {
			t.Errorf("%s: error = %v, want ErrNoBranchName", name, err)
		}
	}

	if got, want := branchNames(t, runner, dir), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Fatalf("branches = %v, want nothing touched", got)
	}
}
