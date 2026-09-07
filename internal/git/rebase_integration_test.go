package git_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Rebasing, against the real binary.
//
// The argument list is the subject of rebase_test.go; here the question is what
// git does with it. Every arrangement of two branches is exercised, because
// three of the four are what a one-directional commit count read as each
// other: a branch behind its upstream was called "changes nothing" while git
// moved it, and a branch ahead of it was promised a replay git refuses to
// perform. See docs/adr/0023.

// twoCommits builds a repository whose branches can be arranged afterwards.
//
//	main   base
//	side   base
func twoCommits(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "notes.md", "base\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("base")...)

	return dir, runner
}

// commitOn writes one file on the branch given and commits it, leaving HEAD
// where it found it. The file name is the commit message: every commit in this
// file exists to be counted, and the only thing worth reading in a failure is
// which one it was.
func commitOn(t *testing.T, runner *git.Runner, dir, branch, file, content string) {
	t.Helper()

	was := headOf(t, runner, dir)
	runGit(t, runner, dir, "switch", branch)
	writeWorkFile(t, dir, file, content)
	runGit(t, runner, dir, "add", "--", file)
	runGit(t, runner, dir, commitWith(file)...)
	if was.Name != "" && was.Name != branch {
		runGit(t, runner, dir, "switch", was.Name)
	}
}

// rebaseableClean is a repository where side and main diverged on different
// files, so rebasing side onto main finishes without a conflict.
//
//	main   base ─── main edit (notes.md)
//	side   base ─── side edit (other.md)
func rebaseableClean(t *testing.T) (string, *git.Runner) {
	t.Helper()

	dir, runner := twoCommits(t)
	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "other.md", "only on side\n")
	commitOn(t, runner, dir, "main", "notes.md", "main\n")
	runGit(t, runner, dir, "switch", "side")

	return dir, runner
}

// rebaseableConflict is like mergeable: both branches edited the same file.
func rebaseableConflict(t *testing.T) (string, *git.Runner) {
	t.Helper()

	dir, runner := twoCommits(t)
	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "notes.md", "side\n")
	commitOn(t, runner, dir, "main", "notes.md", "main\n")
	runGit(t, runner, dir, "switch", "side")

	return dir, runner
}

// revList reads a range as a list of SHAs. `filter` may be empty.
func revList(t *testing.T, runner *git.Runner, dir, filter, spec string) []string {
	t.Helper()

	args := []string{"rev-list"}
	if filter != "" {
		args = append(args, filter)
	}
	output, err := runner.Run(context.Background(), dir, append(args, spec)...)
	if err != nil {
		t.Fatalf("git rev-list %v: %v", args, err)
	}
	return strings.Fields(string(output))
}

func countIn(t *testing.T, runner *git.Runner, dir, filter, spec string) int {
	t.Helper()
	return len(revList(t, runner, dir, filter, spec))
}

func previewOf(t *testing.T, runner *git.Runner, dir, from, onto string) git.RebasePreview {
	t.Helper()

	preview, err := runner.PreviewRebase(context.Background(), dir, from, onto)
	if err != nil {
		t.Fatalf("PreviewRebase(%s onto %s): %v", from, onto, err)
	}
	return preview
}

func TestRebaseReplayMovesTheBranch(t *testing.T) {
	dir, runner := rebaseableClean(t)

	before := commitOf(t, runner, dir, "side")

	if err := runner.Rebase(context.Background(), dir, "main", git.RebaseReplay); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.Name != "side" {
		t.Errorf("HEAD on %q, want side", head.Name)
	}
	if head.SHA == before {
		t.Fatal("side did not move, want the replayed commit on top of main")
	}

	mainTip := commitOf(t, runner, dir, "main")
	parents := parentsOf(t, runner, dir, head.SHA)
	if len(parents) != 1 || parents[0] != mainTip {
		t.Errorf("replayed commit's parent = %v, want main's tip %s", parents, mainTip)
	}

	notes, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatalf("reading notes.md: %v", err)
	}
	if string(notes) != "main\n" {
		t.Errorf("notes.md = %q, want main's content from the new base", notes)
	}

	other, err := os.ReadFile(filepath.Join(dir, "other.md"))
	if err != nil {
		t.Fatalf("reading other.md: %v", err)
	}
	if string(other) != "only on side\n" {
		t.Errorf("other.md = %q, want side's replayed file", other)
	}
}

func TestRebaseConflictStopsAndLeavesMarkers(t *testing.T) {
	dir, runner := rebaseableConflict(t)

	if err := runner.Rebase(context.Background(), dir, "main", git.RebaseReplay); err == nil {
		t.Fatal("Rebase succeeded, want a conflict")
	}

	markers, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatalf("reading notes.md: %v", err)
	}
	if !strings.Contains(string(markers), "<<<<<<<") {
		t.Errorf("notes.md = %q, want conflict markers", markers)
	}

	state, err := git.ReadState(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationRebase {
		t.Errorf("Operation = %q, want rebase", state.Operation)
	}
}

func TestPreviewRebaseCountsCommitsToReplay(t *testing.T) {
	dir, runner := rebaseableClean(t)

	preview := previewOf(t, runner, dir, "side", "main")
	if preview.Outcome != git.RebaseReplay {
		t.Errorf("Outcome = %q, want rebase", preview.Outcome)
	}
	if preview.Rewriting != 1 {
		t.Errorf("Rewriting = %d, want the one commit side has past the fork", preview.Rewriting)
	}
	if preview.Flattening != 0 {
		t.Errorf("Flattening = %d, want no merge commit to discard", preview.Flattening)
	}
	if preview.Behind != 1 {
		t.Errorf("Behind = %d, want the one commit main has past the fork", preview.Behind)
	}
}

// The equal arrangement: the branch was created at the upstream's tip. git
// answers "Current branch is up to date" and writes nothing.
func TestPreviewRebaseUpToDateWhereTheBranchSitsOnTheUpstream(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "at-main")

	preview := previewOf(t, runner, dir, "at-main", "main")
	if preview.Outcome != git.RebaseUpToDate {
		t.Errorf("Outcome = %q, want up-to-date", preview.Outcome)
	}
	if preview.Rewriting != 0 || preview.Behind != 0 {
		t.Errorf("preview = %+v, want nothing to replay and nowhere to move", preview)
	}
}

// The arrangement a one-directional count read as a replay. The branch is
// AHEAD of the upstream, so `rev-list --count main..ahead` is 2 and the old
// plan promised two commits replayed — while git answers "Current branch is up
// to date" and writes nothing at all.
func TestPreviewRebaseUpToDateWhereTheBranchIsAheadOfTheUpstream(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "ahead")
	commitOn(t, runner, dir, "ahead", "one.md", "one\n")
	commitOn(t, runner, dir, "ahead", "two.md", "two\n")

	preview := previewOf(t, runner, dir, "ahead", "main")
	if preview.Outcome != git.RebaseUpToDate {
		t.Fatalf("Outcome = %q, want up-to-date: main is an ancestor of ahead", preview.Outcome)
	}
	if preview.Rewriting != 0 {
		t.Errorf("Rewriting = %d, want none: git replays nothing here", preview.Rewriting)
	}

	// And what the plan says is what git does.
	before := commitOf(t, runner, dir, "ahead")
	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	if after := commitOf(t, runner, dir, "ahead"); after != before {
		t.Errorf("ahead moved from %s to %s, want an up-to-date rebase to write nothing",
			before, after)
	}
}

// The arrangement the same count read as "changes nothing". The branch is
// strictly BEHIND, so `rev-list --count main..behind` is 0 — and git moves the
// branch onto the upstream and rewrites every file under it.
func TestPreviewRebaseFastForwardsABranchWithNothingOfItsOwn(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "behind")
	commitOn(t, runner, dir, "main", "notes.md", "moved on\n")

	preview := previewOf(t, runner, dir, "behind", "main")
	if preview.Outcome != git.RebaseFastForward {
		t.Fatalf("Outcome = %q, want fast-forward: git moves the branch here", preview.Outcome)
	}
	if preview.Rewriting != 0 {
		t.Errorf("Rewriting = %d, want none: there is nothing of its own to replay",
			preview.Rewriting)
	}
	if preview.Behind != 1 {
		t.Errorf("Behind = %d, want the one commit it has to move over", preview.Behind)
	}

	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	if got, want := commitOf(t, runner, dir, "behind"), commitOf(t, runner, dir, "main"); got != want {
		t.Errorf("behind = %s, want main's tip %s", got, want)
	}
	notes, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatalf("reading notes.md: %v", err)
	}
	if string(notes) != "moved on\n" {
		t.Errorf("notes.md = %q, want the upstream's content: the work tree was rewritten", notes)
	}
}

// The arrangement the outcome rule got wrong until docs/adr/0023: the branch is
// strictly AHEAD of the upstream, which reads as "nothing to do", and it holds a
// merge commit, which stops git taking its up-to-date shortcut. git flattens the
// branch instead — every commit written again, the merge gone — so the plan has
// to say a replay and not "rebasing changes nothing".
func TestPreviewRebaseReplaysAnAheadBranchThatHoldsAMerge(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "feature")
	commitOn(t, runner, dir, "feature", "a.md", "a\n")
	runGit(t, runner, dir, "switch", "-c", "topic")
	commitOn(t, runner, dir, "topic", "b.md", "b\n")
	runGit(t, runner, dir, "switch", "feature")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic")
	commitOn(t, runner, dir, "feature", "c.md", "c\n")

	preview := previewOf(t, runner, dir, "feature", "main")
	if preview.Behind != 0 {
		t.Fatalf("Behind = %d, want none: main is an ancestor of feature", preview.Behind)
	}
	if preview.Outcome != git.RebaseReplay {
		t.Fatalf("Outcome = %q, want rebase: git cannot skip a range holding a merge",
			preview.Outcome)
	}
	if preview.Rewriting != 3 || preview.Flattening != 1 {
		t.Errorf("preview = %+v, want three commits written again and one merge discarded",
			preview)
	}

	before := commitOf(t, runner, dir, "feature")
	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}
	if commitOf(t, runner, dir, "feature") == before {
		t.Fatal("feature did not move, want the flattening rewrite git performs here")
	}
	if merges := countIn(t, runner, dir, "--merges", "refs/heads/main..refs/heads/feature"); merges != 0 {
		t.Errorf("%d merge commits left, want a straight line", merges)
	}
}

// What --no-ff is for, and it is not only the race. Without it git fast-forwards
// over the commits at the bottom of the range that are unchanged against the new
// base: they keep their hashes and stay on the branch, while the confirmation
// promised new ones in their place. With it, none of the originals survives.
func TestRebaseReplayWritesEveryCommitAgain(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "feature")
	commitOn(t, runner, dir, "feature", "a.md", "a\n")
	runGit(t, runner, dir, "switch", "-c", "topic")
	commitOn(t, runner, dir, "topic", "b.md", "b\n")
	runGit(t, runner, dir, "switch", "feature")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic")

	// Every ordinary commit the branch holds, before anything moves.
	before := revList(t, runner, dir, "--no-merges", "refs/heads/main..refs/heads/feature")

	// The committer has to differ, or a commit replayed onto the parent it
	// already had, by the same person, in the same second, hashes to the object
	// it started as — and the test would pass on a rebase that did nothing.
	runGit(t, runner, dir, "config", "user.name", "Somebody Else")

	if err := runner.Rebase(context.Background(), dir, "main", git.RebaseReplay); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	after := revList(t, runner, dir, "", "refs/heads/feature")
	for _, sha := range before {
		if slices.Contains(after, sha) {
			t.Errorf("%s is still on the branch, want every commit written again", sha)
		}
	}
}

// What --no-rebase-merges costs, counted before it is spent. The range holds
// four commits and git writes three of them again: the merge commit is
// discarded and the branch comes back a straight line.
func TestPreviewRebaseCountsTheMergeCommitsItWouldFlatten(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "a.md", "a\n")
	runGit(t, runner, dir, "switch", "-c", "topic")
	commitOn(t, runner, dir, "topic", "b.md", "b\n")
	runGit(t, runner, dir, "switch", "side")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic")
	commitOn(t, runner, dir, "side", "c.md", "c\n")
	commitOn(t, runner, dir, "main", "notes.md", "main\n")

	preview := previewOf(t, runner, dir, "side", "main")
	if preview.Rewriting != 3 {
		t.Errorf("Rewriting = %d, want the three ordinary commits git writes again",
			preview.Rewriting)
	}
	if preview.Flattening != 1 {
		t.Errorf("Flattening = %d, want the one merge commit that disappears",
			preview.Flattening)
	}

	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	// The proof the count was about git rather than about the range: three new
	// commits, none of them a merge.
	for sha, walked := commitOf(t, runner, dir, "side"), 0; walked < 3; walked++ {
		parents := parentsOf(t, runner, dir, sha)
		if len(parents) != 1 {
			t.Fatalf("commit %s has %d parents, want a straight line", sha, len(parents))
		}
		sha = parents[0]
	}
}

// A branch that holds nothing past the fork but merge commits the upstream
// already contains. Nothing is written again and nothing is recreated: the
// branch lands on the upstream with its merges gone, which is a loss the
// confirmation has to name because no count of replayed commits would.
func TestPreviewRebaseWhereTheBranchHoldsNothingButMerges(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "topic-a")
	commitOn(t, runner, dir, "topic-a", "a.md", "a\n")
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "switch", "-c", "topic-b")
	commitOn(t, runner, dir, "topic-b", "b.md", "b\n")

	// main takes both in, so everything the branch below merges is already
	// there and only its own merge commits are past the fork.
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic-a")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic-b")

	runGit(t, runner, dir, "switch", "-c", "merges-only", "main~2")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic-a")
	runGit(t, runner, dir, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic-b")

	preview := previewOf(t, runner, dir, "merges-only", "main")
	if preview.Outcome != git.RebaseReplay {
		t.Fatalf("Outcome = %q, want rebase: the two have both moved", preview.Outcome)
	}
	if preview.Rewriting != 0 || preview.Flattening != 2 {
		t.Fatalf("preview = %+v, want nothing to write again and two merges discarded", preview)
	}

	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	if got, want := commitOf(t, runner, dir, "merges-only"), commitOf(t, runner, dir, "main"); got != want {
		t.Errorf("merges-only = %s, want main's tip %s: the merges are gone and nothing replaced them",
			got, want)
	}
}

// The count is what the branch holds, not what git's replay loop will keep.
// git drops a commit whose patch is already upstream — "warning: skipped
// previously applied commit" — and it drops one that turns out empty against
// the new base, which nothing can know without doing the rebase. Both leave
// the branch either way, which is what the number and the warning beside it
// are about; erring towards more is the only direction that is safe here.
func TestPreviewRebaseCountsWhatLeavesTheBranchRatherThanWhatGitKeeps(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "a.md", "a\n")
	picked := commitOf(t, runner, dir, "side")
	commitOn(t, runner, dir, "side", "b.md", "b\n")

	// Onto main AFTER a commit of its own, so the cherry-pick lands under a
	// different parent and is a different commit carrying the same patch.
	commitOn(t, runner, dir, "main", "notes.md", "main\n")
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "cherry-pick", picked)
	runGit(t, runner, dir, "switch", "side")

	preview := previewOf(t, runner, dir, "side", "main")
	if preview.Rewriting != 2 {
		t.Errorf("Rewriting = %d, want both commits side holds past the fork: git will "+
			"write one of them again and drop the other, and neither survives as it is",
			preview.Rewriting)
	}
}

// git refuses to MERGE unrelated histories and rebases them without a word, so
// this refuses neither. What the plan answers instead is the whole branch,
// about to be written again somewhere it has never been.
func TestPreviewRebasePlansUnrelatedHistoriesAsAReplay(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "checkout", "--orphan", "orphan")
	writeWorkFile(t, dir, "alone.md", "started separately\n")
	runGit(t, runner, dir, "add", "--", "alone.md")
	runGit(t, runner, dir, commitWith("alone")...)

	preview := previewOf(t, runner, dir, "orphan", "main")
	if preview.Outcome != git.RebaseReplay {
		t.Fatalf("Outcome = %q, want rebase: git performs this one", preview.Outcome)
	}
	if preview.Rewriting != 1 {
		t.Errorf("Rewriting = %d, want the orphan's only commit", preview.Rewriting)
	}

	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v, want git to replay onto the unrelated root", err)
	}
	if got, want := parentsOf(t, runner, dir, commitOf(t, runner, dir, "orphan")),
		commitOf(t, runner, dir, "main"); len(got) != 1 || got[0] != want {
		t.Errorf("orphan's parent = %v, want main's tip %s", got, want)
	}
}

// rebase.updateRefs force-updates every other branch pointing into the range,
// and it does it silently under a dialog that named one branch.
// --no-update-refs is what keeps the rest of the repository where it was.
func TestRebaseLeavesOtherBranchesWhereTheyAre(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "a.md", "a\n")
	runGit(t, runner, dir, "branch", "--", "stacked")
	commitOn(t, runner, dir, "side", "b.md", "b\n")
	commitOn(t, runner, dir, "main", "notes.md", "main\n")
	runGit(t, runner, dir, "config", "rebase.updateRefs", "true")

	stacked := commitOf(t, runner, dir, "stacked")

	if err := runner.Rebase(context.Background(), dir, "main", git.RebaseReplay); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	if after := commitOf(t, runner, dir, "stacked"); after != stacked {
		t.Errorf("stacked moved from %s to %s, want a rebase to touch only the branch it named",
			stacked, after)
	}
}

// rebase.autoStash would stash the work tree, replay, and put it back — under
// a confirmation that named commits and said nothing about uncommitted work.
// --no-autostash is what leaves git's own refusal to reach the screen.
func TestRebaseRefusesADirtyWorkTreeRatherThanStashingIt(t *testing.T) {
	dir, runner := rebaseableClean(t)

	runGit(t, runner, dir, "config", "rebase.autoStash", "true")
	writeWorkFile(t, dir, "notes.md", "uncommitted\n")

	err := runner.Rebase(context.Background(), dir, "main", git.RebaseReplay)
	if err == nil {
		t.Fatal("Rebase succeeded, want git's refusal over the dirty work tree")
	}

	notes, readErr := os.ReadFile(filepath.Join(dir, "notes.md"))
	if readErr != nil {
		t.Fatalf("reading notes.md: %v", readErr)
	}
	if string(notes) != "uncommitted\n" {
		t.Errorf("notes.md = %q, want the uncommitted work untouched", notes)
	}
}

// The second behaviour the flag list leaves alone, and the reason it has to.
//
// A commit whose patch is already upstream is dropped rather than written
// again, and that is git's default with no configuration behind it: `git help
// -c` lists no `rebase.reapplyCherryPicks`, so a `--no-reapply-cherry-picks`
// in RebaseArgs would pin nothing. What holds the drop up is the default, so
// the default is what this tests — and it sets no config, because a `git
// config` line git ignores is a test that passes whatever the flag list says.
//
// It is also where the confirmation's count stops being a prediction. side
// holds two commits past main and one of them is already there, so
// PreviewRebase promises two and git writes one. That is not a bug in either:
// the count is what the branch points at now and stops pointing at, which is
// what rebaseSummary and rebaseLosses are about. Both numbers are asserted
// here so that a change to either has to say which one it meant.
func TestRebaseDropsAlreadyAppliedCommitsByDefault(t *testing.T) {
	dir, runner := twoCommits(t)

	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "a.md", "a\n")
	picked := commitOf(t, runner, dir, "side")
	commitOn(t, runner, dir, "side", "b.md", "b\n")

	commitOn(t, runner, dir, "main", "notes.md", "main\n")
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "cherry-pick", picked)
	runGit(t, runner, dir, "switch", "side")

	preview := previewOf(t, runner, dir, "side", "main")
	if preview.Rewriting != 2 {
		t.Fatalf("Rewriting = %d, want the 2 commits side points at past main", preview.Rewriting)
	}

	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	if replayed := countIn(t, runner, dir, "", "refs/heads/main..refs/heads/side"); replayed != 1 {
		t.Errorf("%d commits on side past main, want 1: the cherry-picked patch should have been dropped",
			replayed)
	}
}

// The one setting the flag list leaves alone, and the reason it is safe to.
//
// `rebase.forkPoint` sends git to the upstream's reflog for a better fork than
// the merge base, and it changes which commits are replayed: `side` below has
// two commits past `main` and one past the fork point git would find, so a
// rebase honouring it replays one and PreviewRebase promises two. What stops
// that is git's own default — an upstream named on the command line is
// --no-fork-point — and the default is what this holds up, because no flag in
// RebaseArgs does.
func TestRebaseIgnoresForkPointWhereAnUpstreamIsNamed(t *testing.T) {
	dir, runner := twoCommits(t)

	// main moves, side is rebased onto it, and then main is rewound and moves
	// somewhere else. That leaves main's old tip in main's reflog and on side,
	// which is the arrangement --fork-point exists for.
	runGit(t, runner, dir, "switch", "-c", "side")
	commitOn(t, runner, dir, "side", "a.md", "a\n")
	commitOn(t, runner, dir, "main", "first.md", "first\n")
	runGit(t, runner, dir, "rebase", "--merge", "--", "refs/heads/main")
	runGit(t, runner, dir, "switch", "main")
	runGit(t, runner, dir, "reset", "--hard", "main~1")
	commitOn(t, runner, dir, "main", "second.md", "second\n")
	runGit(t, runner, dir, "switch", "side")

	// The fork point is main's rewound tip, which side still carries, and it is
	// not the merge base — which is the whole of what makes this test worth
	// running. Where the two agree, --fork-point changes nothing and a passing
	// assertion below would prove nothing.
	forked := mergeBase(t, runner, dir, "--fork-point")
	if base := mergeBase(t, runner, dir); forked == "" || forked == base {
		t.Fatalf("fork point %q and merge base %q, want the arrangement where they differ",
			forked, base)
	}

	runGit(t, runner, dir, "config", "rebase.forkPoint", "true")

	preview := previewOf(t, runner, dir, "side", "main")
	if preview.Rewriting != 2 {
		t.Fatalf("Rewriting = %d, want both commits side holds past main", preview.Rewriting)
	}

	if err := runner.Rebase(context.Background(), dir, "main", preview.Outcome); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	if replayed := countIn(t, runner, dir, "", "refs/heads/main..refs/heads/side"); replayed != 2 {
		t.Errorf("%d commits replayed, want the 2 the plan promised: a fork point git "+
			"honoured would have left one of them behind", replayed)
	}
}

// mergeBase reads where git thinks main and side met, with or without
// --fork-point.
func mergeBase(t *testing.T, runner *git.Runner, dir string, flags ...string) string {
	t.Helper()

	args := append([]string{"merge-base"}, flags...)
	output, err := runner.Run(context.Background(), dir,
		append(args, "refs/heads/main", "refs/heads/side")...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(output))
}
