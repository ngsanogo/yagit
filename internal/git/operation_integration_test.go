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

// Real operations, stopped for real, and then finished or called off.
//
// The table next door proves which command each instruction spells. Only these
// prove the commands work from inside the daemon, and that distinction is the
// whole reason the file exists: `git rebase --continue` builds a perfectly
// correct command line and still fails, because it wants to open an editor and
// the daemon's environment has no terminal to open one in. Nothing short of
// running it catches that.

// stoppedRebase builds a repository whose rebase of `side` onto `main` stopped
// on f.txt.
//
//	base:  one two three
//	main:  one MAIN three
//	side:  one SIDE three     ← being replayed onto main
func stoppedRebase(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	// In the repository rather than on each command. These operations commit
	// for themselves — `--continue` records the resolution — and the `-c`
	// flags the fixture's own commits carry do not reach them.
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "f.txt", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, commitWith("base")...)

	runGit(t, runner, dir, "checkout", "-b", "side")
	writeWorkFile(t, dir, "f.txt", "one\nSIDE\nthree\n")
	runGit(t, runner, dir, commitWith("side")...)

	runGit(t, runner, dir, "checkout", "main")
	writeWorkFile(t, dir, "f.txt", "one\nMAIN\nthree\n")
	runGit(t, runner, dir, commitWith("main")...)

	runGit(t, runner, dir, "checkout", "side")
	// Expected to fail: the stopped rebase is the state under test.
	if _, err := runner.Run(context.Background(), dir,
		append(append([]string{}, identity...), "rebase", "main")...); err == nil {
		t.Fatal("the rebase succeeded, so there is nothing to continue")
	}

	return dir, runner
}

func stateOf(t *testing.T, dir string) git.State {
	t.Helper()
	state, err := git.ReadState(gitDirOf(t, dir))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	return state
}

func gitDirOf(t *testing.T, dir string) string {
	t.Helper()
	// The work tree's OWN git directory, which for these fixtures is the
	// ordinary `.git` beside the files.
	return dir + "/.git"
}

// The test the whole feature rests on.
//
// `git rebase --continue` commits the resolved conflict under the message of
// the commit being replayed, and it opens an editor to confirm that message.
// The daemon's environment carries no TERM and no EDITOR, so before GIT_EDITOR
// was pinned this failed with "Terminal is dumb, but EDITOR unset" and the
// rebase stayed exactly where it was — a Continue button that could not
// continue.
func TestContinuingARebaseCommitsTheResolutionWithoutAnEditor(t *testing.T) {
	dir, runner := stoppedRebase(t)

	if state := stateOf(t, dir); state.Operation != git.OperationRebase {
		t.Fatalf("the fixture is in the middle of %q, want a rebase", state.Operation)
	}

	writeWorkFile(t, dir, "f.txt", "one\nRESOLVED\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")

	if err := runner.ActOnOperation(context.Background(), dir,
		git.OperationRebase, git.ActionContinue); err != nil {
		t.Fatalf("continuing the rebase: %v", err)
	}

	if state := stateOf(t, dir); state.InProgress() {
		t.Errorf("still in the middle of %q after the last commit was replayed", state.Operation)
	}
	if got := readFile(t, dir, "f.txt"); got != "one\nRESOLVED\nthree\n" {
		t.Errorf("f.txt holds %q, want the resolved content", got)
	}
	// The replayed commit kept its own message, which is what accepting the
	// prepared one means.
	if subject := headSubject(t, runner, dir); subject != "side" {
		t.Errorf("the replayed commit is called %q, want \"side\"", subject)
	}
}

// Aborting puts the branch back where it was, and says so rather than half
// happening.
func TestAbortingARebaseRestoresTheBranchItStartedFrom(t *testing.T) {
	dir, runner := stoppedRebase(t)

	// HEAD during a rebase is detached at what has been replayed so far,
	// which here is main — the commit being applied is not committed yet.
	if before := headSubject(t, runner, dir); before != "main" {
		t.Fatalf("a stopped rebase has HEAD on %q, want the point it is replaying onto", before)
	}

	if err := runner.ActOnOperation(context.Background(), dir,
		git.OperationRebase, git.ActionAbort); err != nil {
		t.Fatalf("aborting the rebase: %v", err)
	}

	if state := stateOf(t, dir); state.InProgress() {
		t.Errorf("still in the middle of %q after aborting", state.Operation)
	}
	// Back on side, with its own content — the rebase undone, not finished.
	if got := readFile(t, dir, "f.txt"); got != "one\nSIDE\nthree\n" {
		t.Errorf("f.txt holds %q, want side's content back", got)
	}
	if count := commitCount(t, runner, dir); count != 2 {
		t.Errorf("the branch has %d commits, want the 2 it had before the rebase", count)
	}
}

// Skipping drops the commit being replayed. It is the way out git itself
// recommends when a resolution leaves nothing to commit.
func TestSkippingARebaseDropsTheCommitItStoppedOn(t *testing.T) {
	dir, runner := stoppedRebase(t)

	if err := runner.ActOnOperation(context.Background(), dir,
		git.OperationRebase, git.ActionSkip); err != nil {
		t.Fatalf("skipping the commit: %v", err)
	}

	if state := stateOf(t, dir); state.InProgress() {
		t.Errorf("still in the middle of %q after skipping the only commit", state.Operation)
	}
	// main's content, because side's one commit was dropped rather than
	// merged into it.
	if got := readFile(t, dir, "f.txt"); got != "one\nMAIN\nthree\n" {
		t.Errorf("f.txt holds %q, want main's content with side's commit dropped", got)
	}
	if subject := headSubject(t, runner, dir); subject != "main" {
		t.Errorf("HEAD is on %q, want main with the skipped commit gone", subject)
	}
}

// Continuing with the conflict still in place fails, and git's own sentence is
// the one that reaches the screen. It names the file and says to add it, which
// is better than anything this package could write.
func TestContinuingWithAConflictStillOpenRefusesInGitsOwnWords(t *testing.T) {
	dir, runner := stoppedRebase(t)

	err := runner.ActOnOperation(context.Background(), dir, git.OperationRebase, git.ActionContinue)
	if err == nil {
		t.Fatal("continuing a rebase with an unmerged file succeeded")
	}

	// The banner is still true afterwards: a refusal must not leave the
	// repository somewhere neither the user nor the screen expects.
	if state := stateOf(t, dir); state.Operation != git.OperationRebase {
		t.Errorf("a refused continue left the repository in %q, want the rebase untouched", state.Operation)
	}

	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("the refusal is not a git failure, so it carries no stderr: %v", err)
	}
	// git prints this particular refusal on STDOUT and leaves stderr empty.
	// Without the fallback in Exec the user is shown "exit code 1: (no error
	// output)" for the most ordinary mistake this button has.
	if !strings.Contains(gitError.Stderr, "needs merge") &&
		!strings.Contains(gitError.Stderr, "merge conflicts") {
		t.Errorf("git's refusal did not reach the user; it carried %q", gitError.Stderr)
	}
	if strings.Contains(gitError.Error(), "no error output") {
		t.Errorf("the message the interface shows explains nothing: %v", gitError)
	}
}

// A rebase git will not let yagit finish, read off the directory git itself
// wrote.
//
// The unit tests beside this one build the rebase directory by hand; this one
// takes the real thing and puts one line in its todo list. `git rebase -i` is
// not run for it because the sequence editor is a shell command, and the one
// that would rewrite a line is a different program on each of the three
// platforms the suite runs on — while the file it would produce is this.
//
// Continuing here runs git with an editor that accepts whatever it is handed.
// That is honest for the `pick` this rebase is stopped on, whose message
// already exists and belongs to the commit being replayed. It is a silent yes
// for the `squash` behind it, which would commit the two messages joined
// without ever showing them.
func TestARebaseWithASquashAheadOfItRefusesToBeContinued(t *testing.T) {
	dir, _ := stoppedRebase(t)

	// The rebase as git left it: an ordinary stop, and nothing refused.
	if state := stateOf(t, dir); len(state.Blocked) != 0 {
		t.Fatalf("a rebase of plain picks blocks %v", state.Blocked)
	}

	todo := filepath.Join(gitDirOf(t, dir), "rebase-merge", "git-rebase-todo")
	if err := os.WriteFile(todo, []byte("squash 1a2b3c4 another commit\n"), 0o644); err != nil {
		t.Fatalf("writing the todo list: %v", err)
	}

	state := stateOf(t, dir)
	reason, blocked := state.Blocked[git.ActionContinue]
	if !blocked {
		t.Fatal("continuing is still offered over a squash whose message nobody would see")
	}
	if !strings.Contains(reason, "squash") {
		t.Errorf("the reason is %q and does not name the squash", reason)
	}
	if err := state.Refuse(git.ActionContinue); !errors.Is(err, git.ErrActionBlocked) {
		t.Errorf("Refuse gave %v, want ErrActionBlocked", err)
	}

	// The way out is still open. Somebody who cannot continue from here is
	// exactly the person who wants to abort, and neither exit writes a
	// message.
	for _, action := range []git.Action{git.ActionAbort, git.ActionSkip} {
		if err := state.Refuse(action); err != nil {
			t.Errorf("%s is refused as well: %v", action, err)
		}
	}
}

// Aborting a stopped merge throws the resolution away and restores both sides.
func TestAbortingAMergeUndoesIt(t *testing.T) {
	dir, runner := conflicted(t)

	if state := stateOf(t, dir); state.Operation != git.OperationMerge {
		t.Fatalf("the fixture is in the middle of %q, want a merge", state.Operation)
	}

	// Work done since the merge stopped, which is exactly what aborting
	// destroys and what the confirmation on screen has to say it destroys.
	writeWorkFile(t, dir, "f.txt", "one\nHALF RESOLVED\nthree\n")

	if err := runner.ActOnOperation(context.Background(), dir,
		git.OperationMerge, git.ActionAbort); err != nil {
		t.Fatalf("aborting the merge: %v", err)
	}

	if state := stateOf(t, dir); state.InProgress() {
		t.Errorf("still in the middle of %q after aborting", state.Operation)
	}
	if got := readFile(t, dir, "f.txt"); got != "one\nMAIN\nthree\n" {
		t.Errorf("f.txt holds %q, want main's content with the half-done resolution gone", got)
	}
}

// A cherry-pick of several commits, resolved and committed the way yagit's
// own commit box commits — and then continued.
//
// This is the flow the sequencer reader in state.go exists for. Committing by
// hand removes CHERRY_PICK_HEAD, so before that reader the repository looked
// idle here: the banner disappeared with a commit still unapplied, taking the
// Continue button with it.
func TestASequenceCommittedByHandIsStillInProgressAndStillContinues(t *testing.T) {
	dir, runner := cherryPicking(t)

	if state := stateOf(t, dir); state.Operation != git.OperationCherryPick {
		t.Fatalf("the fixture is in the middle of %q, want a cherry-pick", state.Operation)
	}

	// Resolve and commit exactly as the interface does: the file, then `git
	// add`, then an ordinary commit — never `--continue`.
	writeWorkFile(t, dir, "f.txt", "one\nRESOLVED\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, commitWith("resolved by hand")...)

	state := stateOf(t, dir)
	if state.Operation != git.OperationCherryPick {
		t.Fatalf("after committing the conflicted pick the repository reports %q; "+
			"git still calls this a cherry-pick in progress, and one commit is unapplied",
			state.Operation)
	}

	if err := runner.ActOnOperation(context.Background(), dir,
		git.OperationCherryPick, git.ActionContinue); err != nil {
		t.Fatalf("continuing the sequence: %v", err)
	}

	if state := stateOf(t, dir); state.InProgress() {
		t.Errorf("still in the middle of %q after the sequence was finished", state.Operation)
	}
	// The second pick landed, which is the whole point of continuing.
	if subject := headSubject(t, runner, dir); subject != "second" {
		t.Errorf("HEAD is on %q, want the second pick applied", subject)
	}
}

// Aborting a sequence that was committed into ends the sequence and leaves the
// commit standing.
//
// Not what "abort" suggests, and it is git's deliberate behaviour rather than a
// gap: `sequencer/abort-safety` holds where HEAD was left, and when HEAD has
// moved somewhere git did not put it, git refuses to rewind — "You seem to have
// moved HEAD. Not rewinding, check your HEAD!" — and exits 0 having cleaned up
// the sequencer.
//
// It is here because committing by hand is the ORDINARY way out of a conflict
// in yagit, so this is the common case rather than the exotic one, and because
// a confirmation promising to discard the commits would be lying in exactly it.
// What the dialog on screen promises is worded against this.
func TestAbortingASequenceCommittedIntoLeavesTheCommitStanding(t *testing.T) {
	dir, runner := cherryPicking(t)

	writeWorkFile(t, dir, "f.txt", "one\nRESOLVED\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, commitWith("resolved by hand")...)

	if err := runner.ActOnOperation(context.Background(), dir,
		git.OperationCherryPick, git.ActionAbort); err != nil {
		t.Fatalf("aborting the sequence: %v", err)
	}

	// The sequence is over either way, which is what the button is for: the
	// banner goes, and the second pick is not applied.
	if state := stateOf(t, dir); state.InProgress() {
		t.Errorf("still in the middle of %q after aborting", state.Operation)
	}
	if subject := headSubject(t, runner, dir); subject != "resolved by hand" {
		t.Errorf("HEAD is on %q; git does not rewind past a commit it did not make", subject)
	}
}

// cherryPicking builds a repository picking two commits, stopped on the first.
//
//	main:  one MAIN three     ← picking onto here
//	side:  one FIRST three, then one SECOND three
func cherryPicking(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	// In the repository rather than on each command. These operations commit
	// for themselves — `--continue` records the resolution — and the `-c`
	// flags the fixture's own commits carry do not reach them.
	configureIdentity(t, runner, dir)
	writeWorkFile(t, dir, "f.txt", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, commitWith("base")...)

	runGit(t, runner, dir, "checkout", "-b", "side")
	writeWorkFile(t, dir, "f.txt", "one\nFIRST\nthree\n")
	runGit(t, runner, dir, commitWith("first")...)
	// A different file, so the second pick applies cleanly once the first is
	// resolved. A second conflict would be testing something else.
	writeWorkFile(t, dir, "g.txt", "second\n")
	runGit(t, runner, dir, "add", "--", "g.txt")
	runGit(t, runner, dir, commitWith("second")...)

	runGit(t, runner, dir, "checkout", "main")
	writeWorkFile(t, dir, "f.txt", "one\nMAIN\nthree\n")
	runGit(t, runner, dir, commitWith("main")...)

	// Expected to fail on the first pick, which is the state under test.
	if _, err := runner.Run(context.Background(), dir,
		append(append([]string{}, identity...), "cherry-pick", "side~1", "side")...); err == nil {
		t.Fatal("the cherry-pick succeeded, so there is no sequence to finish")
	}

	return dir, runner
}
