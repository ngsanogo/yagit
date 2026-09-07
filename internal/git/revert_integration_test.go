package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Reverting, against the real binary.
//
// The argument list is the subject of revert_test.go; here the question is
// what git does with it — a new commit that undoes one already on the branch,
// a conflict that stops halfway, and the readings that keep a merge, a root
// and a foreign commit from being offered as a command that cannot succeed.

// revertable is a linear history on main: base → one → two.
//
// Reverting "one" onto "two" is a clean apply (the change is still in the
// tree). Reverting a side-branch tip that main never held is the refusal.
func revertable(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "notes.md", "base\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("base")...)

	writeWorkFile(t, dir, "notes.md", "one\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("one")...)

	writeWorkFile(t, dir, "extra.md", "two\n")
	runGit(t, runner, dir, "add", "--", "extra.md")
	runGit(t, runner, dir, commitWith("two")...)

	return dir, runner
}

func TestPreviewRevertReadsAnApply(t *testing.T) {
	dir, runner := revertable(t)
	one := commitOf(t, runner, dir, "main~1")

	preview, err := runner.PreviewRevert(context.Background(), dir, one)
	if err != nil {
		t.Fatalf("PreviewRevert: %v", err)
	}
	if preview.Outcome != git.RevertApply {
		t.Errorf("outcome = %q, want revert", preview.Outcome)
	}
	if preview.Commit != one {
		t.Errorf("commit = %s, want the full name %s", preview.Commit, one)
	}
	if preview.Subject != "one" {
		t.Errorf("subject = %q, want the commit's own", preview.Subject)
	}
}

func TestPreviewRevertAcceptsHEAD(t *testing.T) {
	dir, runner := revertable(t)
	head := commitOf(t, runner, dir, "HEAD")

	preview, err := runner.PreviewRevert(context.Background(), dir, head)
	if err != nil {
		t.Fatalf("PreviewRevert(HEAD): %v", err)
	}
	if preview.Outcome != git.RevertApply {
		t.Errorf("outcome = %q, want revert", preview.Outcome)
	}
}

func TestPreviewRevertRefusesAMerge(t *testing.T) {
	dir, runner := revertable(t)

	runGit(t, runner, dir, "switch", "-c", "elsewhere")
	writeWorkFile(t, dir, "other.md", "only here\n")
	runGit(t, runner, dir, "add", "--", "other.md")
	runGit(t, runner, dir, commitWith("elsewhere")...)
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "-m", "merge elsewhere", "--", "elsewhere")
	merge := commitOf(t, runner, dir, "HEAD")

	_, err := runner.PreviewRevert(context.Background(), dir, merge)
	if !errors.Is(err, git.ErrRevertMerge) {
		t.Fatalf("PreviewRevert(merge) = %v, want ErrRevertMerge", err)
	}
}

func TestPreviewRevertRefusesARoot(t *testing.T) {
	dir, runner := revertable(t)
	root := commitOf(t, runner, dir, "main~2")

	_, err := runner.PreviewRevert(context.Background(), dir, root)
	if !errors.Is(err, git.ErrRevertRoot) {
		t.Fatalf("PreviewRevert(root) = %v, want ErrRevertRoot", err)
	}
}

func TestPreviewRevertRefusesACommitNotOnTheBranch(t *testing.T) {
	dir, runner := revertable(t)

	runGit(t, runner, dir, "switch", "-c", "side", "main~2")
	writeWorkFile(t, dir, "side.md", "only side\n")
	runGit(t, runner, dir, "add", "--", "side.md")
	runGit(t, runner, dir, commitWith("side only")...)
	side := commitOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")

	_, err := runner.PreviewRevert(context.Background(), dir, side)
	if !errors.Is(err, git.ErrRevertNotOnBranch) {
		t.Fatalf("PreviewRevert(side) = %v, want ErrRevertNotOnBranch", err)
	}
}

func TestRevertRecordsANewCommit(t *testing.T) {
	dir, runner := revertable(t)
	one := commitOf(t, runner, dir, "main~1")
	before := commitOf(t, runner, dir, "HEAD")

	if err := runner.Revert(context.Background(), dir, one, git.RevertApply); err != nil {
		t.Fatalf("Revert: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.Name != "main" {
		t.Errorf("HEAD on %q, want main", head.Name)
	}
	if head.SHA == before || head.SHA == one {
		t.Errorf("HEAD at %s, want a new commit (was %s, reverted %s)", head.SHA, before, one)
	}
	if parents := parentsOf(t, runner, dir, head.SHA); len(parents) != 1 || parents[0] != before {
		t.Errorf("parents = %v, want [%s]", parents, before)
	}
	// git's default message names the reverted subject.
	if subject := subjectOf(t, runner, dir, head.SHA); subject != `Revert "one"` {
		t.Errorf("subject = %q, want Revert \"one\"", subject)
	}
}

func TestRevertStopsOnAConflict(t *testing.T) {
	dir, runner := revertable(t)

	// "one" changed notes.md to "one\n". Overwrite that file on top so the
	// inverse of "one" (restoring "base\n") conflicts with the current tree.
	writeWorkFile(t, dir, "notes.md", "changed since\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("overwrite")...)
	one := commitOf(t, runner, dir, "main~2")

	err := runner.Revert(context.Background(), dir, one, git.RevertApply)
	if err == nil {
		t.Fatal("Revert succeeded on a conflicting change")
	}
	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want a git.Error", err)
	}

	if state := stateOf(t, dir); state.Operation != git.OperationRevert {
		t.Errorf("operation = %q, want revert", state.Operation)
	}
}
