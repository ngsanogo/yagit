package git_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Cherry-picking, against the real binary.
//
// The argument list is the subject of cherrypick_test.go; here the question is
// what git does with it — a fast-forward that moves HEAD onto the commit, an
// apply that records a new object under the user's hooks, a conflict that
// stops halfway, and the reading that keeps an ancestor from being offered as
// a command that cannot succeed.

// pickable is a repository on main with a diverged side branch.
//
//	main   base ─── main edit
//	side   base ─── side edit
func pickable(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "notes.md", "base\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("base")...)

	runGit(t, runner, dir, "switch", "-c", "side")
	writeWorkFile(t, dir, "notes.md", "side\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("side edit")...)

	runGit(t, runner, dir, "switch", "main")
	writeWorkFile(t, dir, "notes.md", "main\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("main edit")...)

	return dir, runner
}

func TestPreviewCherryPickReadsAnApply(t *testing.T) {
	dir, runner := pickable(t)
	side := commitOf(t, runner, dir, "side")

	preview, err := runner.PreviewCherryPick(context.Background(), dir, side)
	if err != nil {
		t.Fatalf("PreviewCherryPick: %v", err)
	}
	if preview.Outcome != git.CherryPickApply {
		t.Errorf("outcome = %q, want cherry-pick", preview.Outcome)
	}
	if preview.Commit != side {
		t.Errorf("commit = %s, want the full name %s", preview.Commit, side)
	}
	if preview.Subject != "side edit" {
		t.Errorf("subject = %q, want the commit's own", preview.Subject)
	}
}

func TestPreviewCherryPickReadsAFastForward(t *testing.T) {
	dir, runner := pickable(t)

	runGit(t, runner, dir, "switch", "-c", "ahead")
	writeWorkFile(t, dir, "extra.md", "new\n")
	runGit(t, runner, dir, "add", "--", "extra.md")
	runGit(t, runner, dir, commitWith("ahead")...)
	picked := commitOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")

	preview, err := runner.PreviewCherryPick(context.Background(), dir, picked)
	if err != nil {
		t.Fatalf("PreviewCherryPick: %v", err)
	}
	if preview.Outcome != git.CherryPickFastForward {
		t.Errorf("outcome = %q, want fast-forward", preview.Outcome)
	}
}

func TestPreviewCherryPickReadsAnAncestorAsUpToDate(t *testing.T) {
	dir, runner := pickable(t)
	base := commitOf(t, runner, dir, "main~1")

	preview, err := runner.PreviewCherryPick(context.Background(), dir, base)
	if err != nil {
		t.Fatalf("PreviewCherryPick: %v", err)
	}
	if preview.Outcome != git.CherryPickUpToDate {
		t.Errorf("outcome = %q, want up-to-date", preview.Outcome)
	}

	head := commitOf(t, runner, dir, "HEAD")
	preview, err = runner.PreviewCherryPick(context.Background(), dir, head)
	if err != nil {
		t.Fatalf("PreviewCherryPick(HEAD): %v", err)
	}
	if preview.Outcome != git.CherryPickUpToDate {
		t.Errorf("HEAD outcome = %q, want up-to-date", preview.Outcome)
	}
}

func TestPreviewCherryPickRefusesAMerge(t *testing.T) {
	dir, runner := pickable(t)

	// A merge of a branch that only adds a file — clean, so HEAD is a merge
	// commit. Then stand on a branch that does not already contain it, so the
	// refusal is about the commit being a merge rather than about it already
	// being on the branch.
	runGit(t, runner, dir, "switch", "-c", "elsewhere", "main")
	writeWorkFile(t, dir, "other.md", "only here\n")
	runGit(t, runner, dir, "add", "--", "other.md")
	runGit(t, runner, dir, commitWith("elsewhere")...)
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "-m", "merge elsewhere", "--", "elsewhere")
	merge := commitOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "-c", "standing", merge+"^1")

	_, err := runner.PreviewCherryPick(context.Background(), dir, merge)
	if !errors.Is(err, git.ErrCherryPickMerge) {
		t.Fatalf("PreviewCherryPick(merge) = %v, want ErrCherryPickMerge", err)
	}
}

func TestCherryPickApplyRecordsANewCommit(t *testing.T) {
	dir, runner := pickable(t)

	// Side's edit conflicts with main's on the same file. Pick a commit that
	// touches a different file so the apply is clean.
	runGit(t, runner, dir, "switch", "side")
	writeWorkFile(t, dir, "other.md", "only side\n")
	runGit(t, runner, dir, "add", "--", "other.md")
	runGit(t, runner, dir, commitWith("side only")...)
	picked := commitOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")

	before := commitOf(t, runner, dir, "HEAD")
	if err := runner.CherryPick(context.Background(), dir, picked, git.CherryPickApply); err != nil {
		t.Fatalf("CherryPick: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.Name != "main" {
		t.Errorf("HEAD on %q, want main", head.Name)
	}
	if head.SHA == before || head.SHA == picked {
		t.Errorf("HEAD at %s, want a new commit (was %s, picked %s)", head.SHA, before, picked)
	}
	if parents := parentsOf(t, runner, dir, head.SHA); len(parents) != 1 || parents[0] != before {
		t.Errorf("parents = %v, want [%s]", parents, before)
	}
	// The message is the picked commit's, not a merge-style rewrite.
	if subject := subjectOf(t, runner, dir, head.SHA); subject != "side only" {
		t.Errorf("subject = %q, want the picked commit's", subject)
	}
}

func TestCherryPickFastForwardMovesHEADOntoTheCommit(t *testing.T) {
	dir, runner := pickable(t)

	runGit(t, runner, dir, "switch", "-c", "ahead")
	writeWorkFile(t, dir, "extra.md", "new\n")
	runGit(t, runner, dir, "add", "--", "extra.md")
	runGit(t, runner, dir, commitWith("ahead")...)
	picked := commitOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")

	if err := runner.CherryPick(context.Background(), dir, picked, git.CherryPickFastForward); err != nil {
		t.Fatalf("CherryPick: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.SHA != picked {
		t.Errorf("HEAD at %s, want the picked commit %s", head.SHA, picked)
	}
	if parents := parentsOf(t, runner, dir, head.SHA); len(parents) != 1 {
		t.Errorf("HEAD has %d parents, want the one a fast-forward leaves", len(parents))
	}
}

// Up-to-date runs nothing: the route verifies and returns, because git would
// refuse an empty cherry-pick and a confirmation must never offer that.
func TestCherryPickUpToDateChangesNothing(t *testing.T) {
	dir, runner := pickable(t)
	before := commitOf(t, runner, dir, "HEAD")

	if err := runner.CherryPick(context.Background(), dir, before, git.CherryPickUpToDate); err != nil {
		t.Fatalf("CherryPick(up-to-date): %v", err)
	}
	if got := commitOf(t, runner, dir, "HEAD"); got != before {
		t.Errorf("HEAD moved to %s, want it left at %s", got, before)
	}
}

func TestCherryPickApplyStopsOnAConflict(t *testing.T) {
	dir, runner := pickable(t)
	side := commitOf(t, runner, dir, "side")

	err := runner.CherryPick(context.Background(), dir, side, git.CherryPickApply)
	if err == nil {
		t.Fatal("CherryPick succeeded on a conflicting change")
	}
	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want a git.Error", err)
	}

	if state := stateOf(t, dir); state.Operation != git.OperationCherryPick {
		t.Errorf("operation = %q, want cherry-pick", state.Operation)
	}
}

func subjectOf(t *testing.T, runner *git.Runner, dir, sha string) string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "log", "-1", "--pretty=format:%s", sha)
	if err != nil {
		t.Fatalf("log %s: %v", sha, err)
	}
	return strings.TrimSpace(string(output))
}
