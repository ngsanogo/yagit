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

// Stashing, against the real binary.
//
// The argument lists are stash_test.go's subject. Here the question is what
// git actually does — and in this family the exit code is not the whole
// answer. A push with nothing tracked to save succeeds and saves nothing; a
// show without --include-untracked prints nothing for a stash that plainly
// holds a file; a conflicting pop fails, keeps the entry, and says both of
// those things on stdout rather than on stderr. Each has a test below, because
// each is a behaviour a future refactor could drop without a single assertion
// going red anywhere else.

// stashable is a repository with one commit and a work tree that differs:
// notes.md changed, and scratch.txt untracked.
func stashable(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "notes.md", "base\n")
	runGit(t, runner, dir, "add", "--", "notes.md")
	runGit(t, runner, dir, commitWith("base")...)

	writeWorkFile(t, dir, "notes.md", "changed\n")
	writeWorkFile(t, dir, "scratch.txt", "untracked\n")

	return dir, runner
}

func stashesOf(t *testing.T, runner *git.Runner, dir string) []git.Stash {
	t.Helper()
	stashes, err := runner.Stashes(context.Background(), dir)
	if err != nil {
		t.Fatalf("Stashes: %v", err)
	}
	return stashes
}

func TestStashesEmptyStack(t *testing.T) {
	dir, runner := stashable(t)

	stashes := stashesOf(t, runner, dir)
	if len(stashes) != 0 {
		t.Fatalf("got %d stashes in a repository that never stashed", len(stashes))
	}
}

func TestPushStashSavesTrackedChangesAndAnswersWithTheStack(t *testing.T) {
	dir, runner := stashable(t)

	stashes, err := runner.PushStash(context.Background(), dir, "keep this", false)
	if err != nil {
		t.Fatalf("PushStash: %v", err)
	}
	if len(stashes) != 1 {
		t.Fatalf("got %d stashes after a push, want 1", len(stashes))
	}

	stash := stashes[0]
	if stash.Index != 0 {
		t.Errorf("Index = %d, want the new stash on top", stash.Index)
	}
	if stash.Message != "keep this" {
		t.Errorf("Message = %q, want the message that was given", stash.Message)
	}
	if stash.Branch != "main" {
		t.Errorf("Branch = %q, want main", stash.Branch)
	}
	if stash.SHA == "" {
		t.Error("SHA is empty, want the stash commit's object name")
	}

	// The tracked change is gone from the work tree; the untracked file is
	// not, because no flag asked for it. That asymmetry is the whole reason
	// StashPushPreview counts the two separately.
	status := statusOf(t, runner, dir)
	if got := readWorkFile(t, dir, "notes.md"); got != "base\n" {
		t.Errorf("notes.md = %q, want the stash to have put HEAD's version back", got)
	}
	if len(status.Files) != 1 || status.Files[0].Path != "scratch.txt" {
		t.Errorf("status = %+v, want the untracked file left where it was", status.Files)
	}
}

func TestPushStashTakesUntrackedFilesWhenAsked(t *testing.T) {
	dir, runner := stashable(t)

	if _, err := runner.PushStash(context.Background(), dir, "everything", true); err != nil {
		t.Fatalf("PushStash: %v", err)
	}

	if status := statusOf(t, runner, dir); !status.Clean() {
		t.Errorf("status = %+v, want a clean work tree", status.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("scratch.txt is still on disk: %v", err)
	}
}

func TestPreviewStashPushCountsTrackedAndUntrackedApart(t *testing.T) {
	dir, runner := stashable(t)

	preview, err := runner.PreviewStashPush(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("PreviewStashPush: %v", err)
	}
	if preview.Tracked != 1 {
		t.Errorf("Tracked = %d, want the one changed file", preview.Tracked)
	}
	if preview.Untracked != 1 {
		t.Errorf("Untracked = %d, want the one new file", preview.Untracked)
	}
	if preview.Branch != "main" {
		t.Errorf("Branch = %q, want main", preview.Branch)
	}
	if preview.IncludeUntracked {
		t.Error("IncludeUntracked is true, want the choice echoed back as it was made")
	}
}

func TestPreviewStashPushDescribesAWorkTreeItWouldSaveNothingFrom(t *testing.T) {
	// A preview describes; it does not refuse. This work tree holds nothing but
	// untracked files, which is a perfectly ordinary thing to be looking at
	// with the box not yet ticked — and refusing to describe it would mean the
	// one dialog that could offer the flag never opens.
	dir, runner := stashable(t)
	runGit(t, runner, dir, "checkout", "--", "notes.md")

	preview, err := runner.PreviewStashPush(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("PreviewStashPush: %v", err)
	}
	if preview.Saving() {
		t.Error("Saving() = true, but nothing tracked has changed and the flag is off")
	}
	if preview.Untracked != 1 {
		t.Errorf("Untracked = %d, want the file the flag would reach", preview.Untracked)
	}

	// Ticking the box is what makes it a stash git would make.
	withFlag, err := runner.PreviewStashPush(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("PreviewStashPush(untracked): %v", err)
	}
	if !withFlag.Saving() {
		t.Error("Saving() = false with the flag on, but there is an untracked file to save")
	}
}

func TestPushStashRefusesAWorkTreeGitWouldSaveNothingFrom(t *testing.T) {
	// The behaviour this refusal exists for, guarded where the command is:
	// `git stash push` with nothing tracked and changed prints "No local
	// changes to save" and exits 0, leaving the untracked files exactly where
	// they were. An interface reading the exit code would report a stash that
	// does not exist.
	dir, runner := stashable(t)
	runGit(t, runner, dir, "checkout", "--", "notes.md")

	_, err := runner.PushStash(context.Background(), dir, "nothing here", false)
	if !errors.Is(err, git.ErrNothingToStash) {
		t.Fatalf("PushStash = %v, want ErrNothingToStash", err)
	}
	if !strings.Contains(err.Error(), "--include-untracked") {
		t.Errorf("refusal %q does not name the flag that would have worked", err)
	}
	if stashes := stashesOf(t, runner, dir); len(stashes) != 0 {
		t.Errorf("got %d stashes, want the refusal to have run nothing", len(stashes))
	}

	// And it is not a refusal at all once the flag is on.
	if _, err := runner.PushStash(context.Background(), dir, "the untracked one", true); err != nil {
		t.Fatalf("PushStash(untracked): %v", err)
	}
}

func TestPushStashRefusesACleanWorkTree(t *testing.T) {
	dir, runner := stashable(t)
	runGit(t, runner, dir, "checkout", "--", "notes.md")
	if err := os.Remove(filepath.Join(dir, "scratch.txt")); err != nil {
		t.Fatalf("remove scratch.txt: %v", err)
	}

	_, err := runner.PushStash(context.Background(), dir, "nothing here", true)
	if !errors.Is(err, git.ErrNothingToStash) {
		t.Fatalf("PushStash = %v, want ErrNothingToStash", err)
	}
	if strings.Contains(err.Error(), "--include-untracked") {
		t.Errorf("refusal %q offers a flag that would not help", err)
	}
}

func TestPreviewStashPushRefusesARepositoryWithNoCommit(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	runGit(t, runner, dir, "init", "-b", "main")
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "notes.md", "first\n")
	runGit(t, runner, dir, "add", "--", "notes.md")

	// git refuses this too — "You do not have the initial commit yet" — but a
	// sentence about the repository beats a sentence about the command.
	_, err := runner.PreviewStashPush(context.Background(), dir, false)
	if !errors.Is(err, git.ErrNoCommits) {
		t.Fatalf("PreviewStashPush = %v, want ErrNoCommits", err)
	}
}

func TestPushStashRefusesAMessageGitWouldRewrite(t *testing.T) {
	// git takes this, stores both lines on the commit, and flattens them into
	// one in the reflog — so the list would show something nobody typed.
	dir, runner := stashable(t)

	_, err := runner.PushStash(context.Background(), dir, "line one\nline two", false)
	if !errors.Is(err, git.ErrStashMessageLine) {
		t.Fatalf("PushStash = %v, want ErrStashMessageLine", err)
	}
	if stashes := stashesOf(t, runner, dir); len(stashes) != 0 {
		t.Errorf("got %d stashes, want the refusal to have run nothing", len(stashes))
	}
}

func TestPushStashKeepsAMessageGitWouldReadAsAnOption(t *testing.T) {
	dir, runner := stashable(t)

	stashes, err := runner.PushStash(context.Background(), dir, "-f --hard", false)
	if err != nil {
		t.Fatalf("PushStash: %v", err)
	}
	if stashes[0].Message != "-f --hard" {
		t.Errorf("Message = %q, want the message that was given", stashes[0].Message)
	}
}

func TestPushStashOnADetachedHEADHasNoBranch(t *testing.T) {
	dir, runner := stashable(t)
	runGit(t, runner, dir, "checkout", "--detach", "HEAD")

	stashes, err := runner.PushStash(context.Background(), dir, "away from a branch", false)
	if err != nil {
		t.Fatalf("PushStash: %v", err)
	}
	// git writes "(no branch)" here. Stashing is a real operation off a
	// branch, so it is not refused; there is simply no branch to name.
	if stashes[0].Branch != "" {
		t.Errorf("Branch = %q, want empty on a detached HEAD", stashes[0].Branch)
	}
	if stashes[0].Message != "away from a branch" {
		t.Errorf("Message = %q", stashes[0].Message)
	}
}

func TestStashesAreNumberedFromTheMostRecent(t *testing.T) {
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "first")
	writeWorkFile(t, dir, "notes.md", "again\n")
	pushStash(t, runner, dir, "second")

	stashes := stashesOf(t, runner, dir)
	if len(stashes) != 2 {
		t.Fatalf("got %d stashes, want 2", len(stashes))
	}
	if stashes[0].Message != "second" || stashes[0].Index != 0 {
		t.Errorf("stash@{0} = %+v, want the most recent", stashes[0])
	}
	if stashes[1].Message != "first" || stashes[1].Index != 1 {
		t.Errorf("stash@{1} = %+v, want the older one", stashes[1])
	}
}

func TestStashTargetRefusesAPositionThatMoved(t *testing.T) {
	// The refusal this file exists for. A plan reads stash@{0}; another window
	// pushes a stash; the run would have dropped somebody else's work under a
	// command that was correct when it was drawn.
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "the one that was approved")
	approved := stashesOf(t, runner, dir)[0]

	writeWorkFile(t, dir, "notes.md", "meanwhile\n")
	pushStash(t, runner, dir, "pushed from somewhere else")

	_, err := runner.StashTarget(context.Background(), dir, 0, approved.SHA)
	if !errors.Is(err, git.ErrStashMoved) {
		t.Fatalf("StashTarget = %v, want ErrStashMoved", err)
	}

	// And it resolves at the position the stash actually moved to.
	found, err := runner.StashTarget(context.Background(), dir, 1, approved.SHA)
	if err != nil {
		t.Fatalf("StashTarget(1): %v", err)
	}
	if found.SHA != approved.SHA {
		t.Errorf("SHA = %s, want %s", found.SHA, approved.SHA)
	}
	if found.Ref() != "stash@{1}" {
		t.Errorf("Ref() = %q, want the position it is at now", found.Ref())
	}
}

func TestStashTargetRefusesAPositionTheStackDoesNotHave(t *testing.T) {
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "only one")
	only := stashesOf(t, runner, dir)[0]

	for name, index := range map[string]int{"past the end": 4, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			_, err := runner.StashTarget(context.Background(), dir, index, only.SHA)
			if !errors.Is(err, git.ErrNoStash) {
				t.Fatalf("StashTarget(%d) = %v, want ErrNoStash", index, err)
			}
		})
	}
}

func TestStashTargetRefusesARequestThatNamesNoStash(t *testing.T) {
	// The empty string is not a wildcard, for the reason agreesOnBranch
	// refuses one: a client that names nothing is a client that did not look.
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "only one")

	_, err := runner.StashTarget(context.Background(), dir, 0, "")
	if !errors.Is(err, git.ErrStashMoved) {
		t.Fatalf("StashTarget = %v, want ErrStashMoved", err)
	}
}

func TestApplyStashKeepsTheEntryAndPopRemovesIt(t *testing.T) {
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "put it back")

	left, err := runner.ApplyStash(context.Background(), dir, 0, git.StashApplyKeep)
	if err != nil {
		t.Fatalf("ApplyStash(apply): %v", err)
	}
	if len(left) != 1 {
		t.Errorf("got %d stashes after an apply, want the entry left in place", len(left))
	}
	if got := readWorkFile(t, dir, "notes.md"); got != "changed\n" {
		t.Errorf("notes.md = %q, want the stashed content back", got)
	}

	// Popping the same entry: the work is already there, so git has nothing to
	// merge and the entry goes.
	runGit(t, runner, dir, "checkout", "--", "notes.md")
	left, err = runner.ApplyStash(context.Background(), dir, 0, git.StashApplyPop)
	if err != nil {
		t.Fatalf("ApplyStash(pop): %v", err)
	}
	if len(left) != 0 {
		t.Errorf("got %d stashes after a pop, want an empty stack", len(left))
	}
}

func TestApplyStashReportsAConflictWithEverythingGitSaid(t *testing.T) {
	// A conflicting pop exits 1, so it travels as the failure it is — the way
	// a conflicting merge does. What makes it worth a test of its own is where
	// git puts the words: "CONFLICT" and "The stash entry is kept in case you
	// need it again" go to STDOUT, and only "Recorded preimage" goes to
	// stderr. A failure carrying stderr alone would tell the user nothing they
	// could act on, which is what Exec borrowing stdout is for.
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "conflicting work")

	writeWorkFile(t, dir, "notes.md", "something else entirely\n")
	runGit(t, runner, dir, "add", "--", "notes.md")

	_, err := runner.ApplyStash(context.Background(), dir, 0, git.StashApplyPop)
	if err == nil {
		t.Fatal("ApplyStash(pop) into a conflict succeeded")
	}
	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error = %v, want git's own account", err)
	}
	if !strings.Contains(gitError.Stderr, "CONFLICT") {
		t.Errorf("the failure says %q, which does not mention the conflict", gitError.Stderr)
	}
	if !strings.Contains(gitError.Stderr, "stash entry is kept") {
		t.Errorf("the failure says %q, which does not say the stash survived", gitError.Stderr)
	}

	// The entry really is still there, and the conflict is on disk in the
	// state the resolution screen already knows how to finish. git records no
	// operation for it — there is no MERGE_HEAD to abort — so the file is what
	// has to say so.
	if stashes := stashesOf(t, runner, dir); len(stashes) != 1 {
		t.Errorf("got %d stashes, want the entry still there", len(stashes))
	}
	if unmerged := fileIn(t, statusOf(t, runner, dir), "notes.md"); unmerged.Kind != git.EntryUnmerged {
		t.Errorf("notes.md kind = %q, want unmerged", unmerged.Kind)
	}
}

func TestApplyStashRefusesToWriteOverAnOverlappingChange(t *testing.T) {
	// git's own refusal, and it is a real failure rather than a silent one:
	// "Your local changes to the following files would be overwritten by
	// merge". Nothing is applied and the entry stays.
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "stashed edit")

	writeWorkFile(t, dir, "notes.md", "a different edit\n")

	_, err := runner.ApplyStash(context.Background(), dir, 0, git.StashApplyKeep)
	if err == nil {
		t.Fatal("ApplyStash succeeded over an overlapping local change")
	}
	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error = %v, want git's own refusal with its stderr", err)
	}
	if stashes := stashesOf(t, runner, dir); len(stashes) != 1 {
		t.Errorf("got %d stashes, want the entry kept after a refused apply", len(stashes))
	}
}

func TestDropStashRemovesOneEntryAndLeavesTheRest(t *testing.T) {
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "older")
	writeWorkFile(t, dir, "notes.md", "again\n")
	pushStash(t, runner, dir, "newer")

	left, err := runner.DropStash(context.Background(), dir, 0)
	if err != nil {
		t.Fatalf("DropStash: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("got %d stashes after a drop, want 1", len(left))
	}
	if left[0].Message != "older" {
		t.Errorf("left %q, want the one that was not dropped", left[0].Message)
	}
	// And it has been renumbered, which is the fact every plan in this family
	// has to be checked against.
	if left[0].Index != 0 {
		t.Errorf("Index = %d, want the survivor renumbered to the top", left[0].Index)
	}
}

func TestShowStashReadsWhatTheStashHolds(t *testing.T) {
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "one file")
	stash := stashesOf(t, runner, dir)[0]

	files, err := runner.ShowStash(context.Background(), dir, stash.SHA)
	if err != nil {
		t.Fatalf("ShowStash: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want the one that was stashed", len(files))
	}
	if files[0].Path != "notes.md" {
		t.Errorf("Path = %q, want notes.md", files[0].Path)
	}
	if len(files[0].Hunks) == 0 {
		t.Error("no hunks, want the change the stash holds")
	}
}

func TestShowStashReadsAStashWhoseOnlyContentIsUntracked(t *testing.T) {
	// The third silent behaviour: without --include-untracked, `git stash
	// show` prints nothing and exits 0 here, because untracked files hang off
	// a third parent it does not look at. An empty screen for a stash that
	// plainly holds a file is the worst answer available, so the flag is not
	// optional.
	dir, runner := stashable(t)
	runGit(t, runner, dir, "checkout", "--", "notes.md")

	if _, err := runner.PushStash(context.Background(), dir, "untracked only", true); err != nil {
		t.Fatalf("PushStash: %v", err)
	}
	stash := stashesOf(t, runner, dir)[0]

	files, err := runner.ShowStash(context.Background(), dir, stash.SHA)
	if err != nil {
		t.Fatalf("ShowStash: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want the untracked file the stash holds", len(files))
	}
	if files[0].Path != "scratch.txt" {
		t.Errorf("Path = %q, want scratch.txt", files[0].Path)
	}
	if !files[0].Added {
		t.Error("Added = false, want the file marked as new")
	}
}

func TestShowStashKeepsGitsOwnPathPrefixes(t *testing.T) {
	// The bug this reading exists to avoid, pinned at the level it broke.
	// `git stash show` re-parses its arguments and does not carry
	// --src-prefix/--dst-prefix through on every git: the header comes back as
	// `diff --git butesnotes.md`, ParseDiff refuses it, and the path that does
	// parse is a fragment of another option's name. Nothing here can reproduce
	// that on a git which behaves — what it can do is assert that the path
	// which comes out is the path that went in.
	dir, runner := stashable(t)
	pushStash(t, runner, dir, "prefixes")
	stash := stashesOf(t, runner, dir)[0]

	files, err := runner.ShowStash(context.Background(), dir, stash.SHA)
	if err != nil {
		t.Fatalf("ShowStash: %v", err)
	}
	if len(files) != 1 || files[0].Path != "notes.md" {
		t.Fatalf("files = %+v, want exactly notes.md", files)
	}
	if files[0].OldPath != "" {
		t.Errorf("OldPath = %q, want none: nothing was renamed", files[0].OldPath)
	}
}

func TestShowStashRefusesACommitThatIsNotAStash(t *testing.T) {
	// These routes take an object name from a client. An ordinary commit would
	// otherwise be drawn as "a stash" — its diff against its own parent, under
	// a heading naming a stack it was never in.
	dir, runner := stashable(t)
	head := commitOf(t, runner, dir, "HEAD")

	if _, err := runner.ShowStash(context.Background(), dir, head); !errors.Is(err, git.ErrNoStash) {
		t.Fatalf("ShowStash(HEAD) = %v, want ErrNoStash", err)
	}
	if _, err := runner.CountStashFiles(context.Background(), dir, head); !errors.Is(err, git.ErrNoStash) {
		t.Fatalf("CountStashFiles(HEAD) = %v, want ErrNoStash", err)
	}
}

func TestCountStashFilesCountsBothHalves(t *testing.T) {
	// One tracked and one untracked, which is the case a single read gets
	// wrong: the untracked file hangs off a third parent, and a count that
	// looked only at the commit would answer one for a stash holding two.
	dir, runner := stashable(t)
	if _, err := runner.PushStash(context.Background(), dir, "both halves", true); err != nil {
		t.Fatalf("PushStash: %v", err)
	}
	stash := stashesOf(t, runner, dir)[0]

	count, err := runner.CountStashFiles(context.Background(), dir, stash.SHA)
	if err != nil {
		t.Fatalf("CountStashFiles: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want the tracked file and the untracked one", count)
	}

	// And it agrees with the patch, which is the only way the confirmation and
	// the panel beside it can name the same number.
	files, err := runner.ShowStash(context.Background(), dir, stash.SHA)
	if err != nil {
		t.Fatalf("ShowStash: %v", err)
	}
	if len(files) != count {
		t.Errorf("ShowStash found %d files, CountStashFiles said %d", len(files), count)
	}
}

func TestShowStashRefusesARevisionGitWouldReadAsAnOption(t *testing.T) {
	dir, runner := stashable(t)

	_, err := runner.ShowStash(context.Background(), dir, "--output=/tmp/written")
	if !errors.Is(err, git.ErrBadRevision) {
		t.Fatalf("ShowStash = %v, want ErrBadRevision", err)
	}
}

// pushStash saves the work tree, failing the test if git refuses.
func pushStash(t *testing.T, runner *git.Runner, dir, message string) {
	t.Helper()
	if _, err := runner.PushStash(context.Background(), dir, message, false); err != nil {
		t.Fatalf("PushStash(%q): %v", message, err)
	}
}

// readWorkFile is how the tests above check that a work tree was written.
func readWorkFile(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}
