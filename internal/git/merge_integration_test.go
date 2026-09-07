package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Merging, against the real binary.
//
// The argument list is the subject of merge_test.go; here the question is what
// git does with it — a fast-forward that moves HEAD, a clean divergence that
// records a merge commit, a conflict that stops halfway and leaves the markers
// behind, and the one that would otherwise never be noticed: that none of the
// three is decided by the user's configuration.

// mergeable is a repository on main with a branch that diverged on one file.
//
//	main   base ─── main edit
//	side   base ─── side edit
func mergeable(t *testing.T) (string, *git.Runner) {
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
	runGit(t, runner, dir, commitWith("side")...)

	runGit(t, runner, dir, "switch", "main")
	writeWorkFile(t, dir, "notes.md", "main\n")
	runGit(t, runner, dir, commitWith("main")...)

	return dir, runner
}

// ahead adds a branch holding one commit main does not have and nothing main
// has to reconcile: the shape a fast-forward needs.
func ahead(t *testing.T, runner *git.Runner, dir string) {
	t.Helper()

	runGit(t, runner, dir, "switch", "-c", "ahead")
	writeWorkFile(t, dir, "extra.md", "new\n")
	runGit(t, runner, dir, "add", "--", "extra.md")
	runGit(t, runner, dir, commitWith("ahead")...)
	runGit(t, runner, dir, "switch", "main")
}

// diverged adds a branch that touches a file main never touches, so the two
// have commits the other does not and git can merge them cleanly.
func diverged(t *testing.T, runner *git.Runner, dir string) {
	t.Helper()

	runGit(t, runner, dir, "switch", "-c", "elsewhere", "main")
	writeWorkFile(t, dir, "other.md", "only here\n")
	runGit(t, runner, dir, "add", "--", "other.md")
	runGit(t, runner, dir, commitWith("elsewhere")...)

	runGit(t, runner, dir, "switch", "main")
	writeWorkFile(t, dir, "notes.md", "main again\n")
	runGit(t, runner, dir, commitWith("main again")...)
}

func TestMergeFastForwardMovesHEAD(t *testing.T) {
	dir, runner := mergeable(t)
	ahead(t, runner, dir)

	if err := runner.Merge(context.Background(), dir, "main", "ahead", git.MergeFastForward); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.Name != "main" {
		t.Errorf("HEAD on %q, want main", head.Name)
	}
	if want := commitOf(t, runner, dir, "ahead"); head.SHA != want {
		t.Errorf("HEAD at %s, want the tip of ahead %s", head.SHA, want)
	}

	// A fast-forward writes no object, so main's tip is still the commit
	// `ahead` made — one parent, and nobody's signature on a merge.
	if parents := parentsOf(t, runner, dir, head.SHA); len(parents) != 1 {
		t.Errorf("HEAD has %d parents, want the one a fast-forward leaves", len(parents))
	}
}

// The case every open question bites in: git commits, which means the user's
// identity, their hooks and their signing configuration.
func TestMergeOfDivergedBranchesRecordsAMergeCommit(t *testing.T) {
	dir, runner := mergeable(t)
	diverged(t, runner, dir)

	before := commitOf(t, runner, dir, "main")

	if err := runner.Merge(context.Background(), dir, "main", "elsewhere", git.MergeCommit); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.SHA == before {
		t.Fatal("main did not move, want a merge commit on top of it")
	}

	parents := parentsOf(t, runner, dir, head.SHA)
	if len(parents) != 2 {
		t.Fatalf("HEAD has %d parents, want the two a merge commit has", len(parents))
	}
	if parents[0] != before {
		t.Errorf("first parent %s, want the branch merged into %s", parents[0], before)
	}
	if want := commitOf(t, runner, dir, "elsewhere"); parents[1] != want {
		t.Errorf("second parent %s, want the branch brought in %s", parents[1], want)
	}

	// And it did not stop to ask. `git merge` opens an editor on the message
	// it prepared whenever it thinks somebody is watching, and the daemon has
	// no terminal to show one in — --no-edit is what makes this pass rather
	// than fail with "Terminal is dumb, but EDITOR unset".
	if want := "Merge branch 'elsewhere' into main"; headSubject(t, runner, dir) != want {
		t.Errorf("merge commit subject %q, want %q", headSubject(t, runner, dir), want)
	}
}

// The regression test for the whole design: merge.ff decides what a bare
// `git merge` does, and yagit shows a command before it runs one.
func TestMergeIsNotDecidedByTheUsersConfiguration(t *testing.T) {
	t.Run("a fast-forward stays one where merge.ff is false", func(t *testing.T) {
		dir, runner := mergeable(t)
		ahead(t, runner, dir)

		// `git merge ahead` here produces a merge commit — with hooks, with a
		// signature, and with a message nobody was shown.
		runGit(t, runner, dir, "config", "merge.ff", "false")

		if err := runner.Merge(context.Background(), dir, "main", "ahead", git.MergeFastForward); err != nil {
			t.Fatalf("Merge: %v", err)
		}

		head := headOf(t, runner, dir)
		if want := commitOf(t, runner, dir, "ahead"); head.SHA != want {
			t.Errorf("HEAD at %s, want the fast-forward the dialog promised: %s", head.SHA, want)
		}
	})

	t.Run("a merge commit is still recorded where merge.ff is only", func(t *testing.T) {
		dir, runner := mergeable(t)
		diverged(t, runner, dir)

		// `git merge elsewhere` here refuses outright.
		runGit(t, runner, dir, "config", "merge.ff", "only")

		if err := runner.Merge(context.Background(), dir, "main", "elsewhere", git.MergeCommit); err != nil {
			t.Fatalf("Merge: %v", err)
		}

		if parents := parentsOf(t, runner, dir, headOf(t, runner, dir).SHA); len(parents) != 2 {
			t.Errorf("HEAD has %d parents, want the merge commit the dialog promised", len(parents))
		}
	})
}

func TestMergeConflictStopsAndLeavesMarkers(t *testing.T) {
	dir, runner := mergeable(t)

	err := runner.Merge(context.Background(), dir, "main", "side", git.MergeCommit)
	if err == nil {
		t.Fatal("Merge succeeded, want git to stop on a conflict")
	}

	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("expected a git failure, got %T: %v", err, err)
	}

	content, readErr := os.ReadFile(filepath.Join(dir, "notes.md"))
	if readErr != nil {
		t.Fatalf("reading notes.md: %v", readErr)
	}
	if !strings.Contains(string(content), "<<<<<<<") {
		t.Errorf("notes.md = %q, want conflict markers", content)
	}

	// And HEAD is still on main: the merge did not finish.
	if head := headOf(t, runner, dir); head.Name != "main" {
		t.Errorf("HEAD on %q after a stopped merge, want main", head.Name)
	}
}

// The ambiguity every command in this package spells its way out of, met
// where it does the damage: `git merge dup` in a repository holding both a
// branch and a tag called `dup` merges the TAG. The plan is read from
// refs/heads/dup and says a merge commit is coming; the run has to reach the
// same commits or the interface has described one operation and performed
// another.
func TestMergeTakesTheBranchAndNotATagOfTheSameName(t *testing.T) {
	dir, runner := mergeable(t)
	diverged(t, runner, dir)

	// `dup` the branch is `elsewhere`, which holds a commit main does not have
	// and touches no file main touched. `dup` the tag is main itself — so a
	// merge that resolved the tag would answer "Already up to date", exit 0,
	// and leave the interface saying it merged.
	runGit(t, runner, dir, "branch", "--", "dup", "elsewhere")
	runGit(t, runner, dir, "tag", "dup", "main")

	before := commitOf(t, runner, dir, "main")

	err := runner.Merge(context.Background(), dir, "main", "dup", git.MergeCommit)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.SHA == before {
		t.Fatal("main did not move: the merge took the tag, not the branch")
	}

	parents := parentsOf(t, runner, dir, head.SHA)
	if len(parents) != 2 {
		t.Fatalf("HEAD has %d parents, want the two a merge commit has", len(parents))
	}
	if want := commitOf(t, runner, dir, "elsewhere"); parents[1] != want {
		t.Errorf("second parent %s, want the commit the BRANCH dup points at %s", parents[1], want)
	}
}

func TestMergeRefusesAnEmptyBranchName(t *testing.T) {
	dir, runner := mergeable(t)

	err := runner.Merge(context.Background(), dir, "main", "", git.MergeFastForward)
	if !errors.Is(err, git.ErrNoBranchName) {
		t.Errorf("Merge(\"\") = %v, want ErrNoBranchName", err)
	}
}

// What the confirmation is built from: the three shapes, and the counts that
// let a sentence say how many commits are coming.
func TestPreviewMergeReadsWhatTheMergeWouldBe(t *testing.T) {
	dir, runner := mergeable(t)
	ahead(t, runner, dir)
	diverged(t, runner, dir)

	cases := []struct {
		branch  string
		outcome git.MergeOutcome
		ahead   int
		behind  int
	}{
		// main is two commits along its own line; ahead sits one past the
		// first of them.
		{branch: "ahead", outcome: git.MergeCommit, ahead: 1, behind: 1},
		{branch: "elsewhere", outcome: git.MergeCommit, ahead: 1, behind: 1},
		// side forked at the base and has one commit; main has two of its own
		// since.
		{branch: "side", outcome: git.MergeCommit, ahead: 2, behind: 1},
	}

	for _, want := range cases {
		preview, err := runner.PreviewMerge(context.Background(), dir, "main", want.branch)
		if err != nil {
			t.Fatalf("PreviewMerge(%q): %v", want.branch, err)
		}
		if preview.Outcome != want.outcome {
			t.Errorf("PreviewMerge(%q).Outcome = %q, want %q", want.branch, preview.Outcome, want.outcome)
		}
		if preview.Ahead != want.ahead || preview.Behind != want.behind {
			t.Errorf("PreviewMerge(%q) = +%d -%d, want +%d -%d",
				want.branch, preview.Ahead, preview.Behind, want.ahead, want.behind)
		}
	}
}

func TestPreviewMergeTellsAFastForwardFromABranchAlreadyIn(t *testing.T) {
	dir, runner := mergeable(t)

	// main has nothing of its own past this fork, so the merge is the pointer
	// moving.
	runGit(t, runner, dir, "switch", "-c", "trailing", "main~1")

	preview, err := runner.PreviewMerge(context.Background(), dir, "trailing", "main")
	if err != nil {
		t.Fatalf("PreviewMerge: %v", err)
	}
	if preview.Outcome != git.MergeFastForward {
		t.Errorf("Outcome = %q, want a fast-forward", preview.Outcome)
	}
	if preview.Ahead != 0 || preview.Behind != 1 {
		t.Errorf("PreviewMerge = +%d -%d, want +0 -1", preview.Ahead, preview.Behind)
	}

	// And the other way round: main already holds every commit trailing has.
	back, err := runner.PreviewMerge(context.Background(), dir, "main", "trailing")
	if err != nil {
		t.Fatalf("PreviewMerge: %v", err)
	}
	if back.Outcome != git.MergeUpToDate {
		t.Errorf("Outcome = %q, want up to date", back.Outcome)
	}
	if back.Behind != 0 {
		t.Errorf("Behind = %d, want nothing to bring in", back.Behind)
	}
}

// A branch and a tag may share a name, and git's own search order reaches
// refs/tags first. The short name would count against the tag while the dialog
// named the branch — which is why PreviewMerge spells refs/heads/… in full.
func TestPreviewMergeReadsTheBranchAndNotATagOfTheSameName(t *testing.T) {
	dir, runner := mergeable(t)

	// The branch has a commit main does not; the tag is main itself, so a
	// reading that took it would answer "already up to date".
	runGit(t, runner, dir, "branch", "--", "dup", "side")
	runGit(t, runner, dir, "tag", "dup", "main")

	preview, err := runner.PreviewMerge(context.Background(), dir, "main", "dup")
	if err != nil {
		t.Fatalf("PreviewMerge: %v", err)
	}
	if preview.Outcome != git.MergeCommit {
		t.Errorf("Outcome = %q, want the branch's reading rather than the tag's", preview.Outcome)
	}
}

// Two branches that were never one. The counts cannot tell this apart from an
// ordinary divergence — each side reports every commit it has — so a plan
// built from them alone promises a merge commit git refuses to make.
func TestPreviewMergeRefusesHistoriesThatNeverMet(t *testing.T) {
	dir, runner := mergeable(t)

	runGit(t, runner, dir, "checkout", "--orphan", "orphan")
	writeWorkFile(t, dir, "alone.md", "started separately\n")
	runGit(t, runner, dir, "add", "--", "alone.md")
	runGit(t, runner, dir, commitWith("alone")...)
	runGit(t, runner, dir, "switch", "main")

	_, err := runner.PreviewMerge(context.Background(), dir, "orphan", "main")
	if !errors.Is(err, git.ErrUnrelatedHistories) {
		t.Errorf("PreviewMerge = %v, want ErrUnrelatedHistories", err)
	}

	// Both ways round: neither branch contains the other, so the refusal
	// cannot depend on which of the two was asked about.
	if _, err := runner.PreviewMerge(context.Background(), dir, "main", "orphan"); !errors.Is(
		err, git.ErrUnrelatedHistories) {
		t.Errorf("PreviewMerge the other way = %v, want ErrUnrelatedHistories", err)
	}

	// And a branch that DID fork from main still reads as an ordinary
	// divergence, which is what says this is reading the fork point rather
	// than the shape of the counts.
	diverged(t, runner, dir)
	preview, err := runner.PreviewMerge(context.Background(), dir, "main", "elsewhere")
	if err != nil {
		t.Fatalf("PreviewMerge(elsewhere): %v", err)
	}
	if preview.Outcome != git.MergeCommit {
		t.Errorf("Outcome = %q, want a merge commit", preview.Outcome)
	}
}

func TestPreviewMergeRefusesAnEmptyName(t *testing.T) {
	dir, runner := mergeable(t)

	if _, err := runner.PreviewMerge(context.Background(), dir, "main", ""); !errors.Is(err, git.ErrNoBranchName) {
		t.Errorf("PreviewMerge = %v, want ErrNoBranchName", err)
	}
	if _, err := runner.PreviewMerge(context.Background(), dir, "", "side"); !errors.Is(err, git.ErrNoBranchName) {
		t.Errorf("PreviewMerge = %v, want ErrNoBranchName", err)
	}
}

// commitOf resolves a reference the way the tests compare them: as the commit
// it points at, so an annotated tag and a branch answer the same shape.
func commitOf(t *testing.T, runner *git.Runner, dir, reference string) string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "rev-parse", reference+"^{commit}")
	if err != nil {
		t.Fatalf("rev-parse %s: %v", reference, err)
	}
	return strings.TrimSpace(string(output))
}

// parentsOf is how a fast-forward is told from a merge commit: one parent
// against two.
func parentsOf(t *testing.T, runner *git.Runner, dir, sha string) []string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "rev-list", "-1", "--parents", sha)
	if err != nil {
		t.Fatalf("rev-list --parents %s: %v", sha, err)
	}
	return strings.Fields(strings.TrimSpace(string(output)))[1:]
}
