package git_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestParseReflog(t *testing.T) {
	when := time.Date(2026, 9, 5, 7, 0, 0, 0, time.UTC)
	output := []byte(
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\x00HEAD@{0}\x00commit: second\x00" +
			when.Format(time.RFC3339) + "\x00\n" +
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\x00HEAD@{1}\x00commit: first\x00" +
			when.Format(time.RFC3339) + "\x00\n",
	)
	entries, err := git.ParseReflog(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("len = %d", len(entries))
	}
	if entries[0].Subject != "commit: second" || entries[0].Selector != "HEAD@{0}" {
		t.Fatalf("tip = %+v", entries[0])
	}
}

// An unborn HEAD has no reflog. Asking for one must not surface git's
// "ambiguous argument 'HEAD'" as a failure — Undo treats that state as
// nothing to undo, and the log panel must not show a fatal for it.
func TestHeadReflogOfAnUnbornBranch(t *testing.T) {
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()
	runGit(t, runner, dir, "init", "-b", "main")

	entries, err := runner.HeadReflog(ctx, dir, 3)
	if err != nil {
		t.Fatalf("an unborn branch is a normal state, not an error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries = %d, want none", len(entries))
	}

	if _, err := runner.PreviewUndo(ctx, dir); !errors.Is(err, git.ErrNothingToUndo) {
		t.Fatalf("PreviewUndo = %v, want ErrNothingToUndo", err)
	}
}

func TestPreviewUndoCommit(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")

	write(t, dir, "f.txt", "two\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "second")
	tip := shaOf(t, runner, dir, "HEAD")
	parent := shaOf(t, runner, dir, "HEAD~1")

	preview, err := runner.PreviewUndo(ctx, dir)
	if err != nil {
		t.Fatalf("PreviewUndo: %v", err)
	}
	if preview.Kind != git.UndoCommit {
		t.Errorf("kind = %q", preview.Kind)
	}
	if preview.Head != tip || preview.To != parent {
		t.Errorf("lease head=%s to=%s, want %s → %s", preview.Head, preview.To, tip, parent)
	}
	if preview.Subject != "second" {
		t.Errorf("subject = %q", preview.Subject)
	}
	if !strings.Contains(preview.Command, "reset --soft") {
		t.Errorf("command = %q, want soft reset", preview.Command)
	}
}

func TestUndoCommitRestoresTheIndex(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	parent := shaOf(t, runner, dir, "HEAD")

	write(t, dir, "f.txt", "two\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "second")
	tip := shaOf(t, runner, dir, "HEAD")

	undo := git.UndoPreview{Kind: git.UndoCommit, Head: tip, To: parent, ToRef: parent}
	if err := runner.Undo(ctx, dir, undo); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := shaOf(t, runner, dir, "HEAD"); got != parent {
		t.Fatalf("HEAD = %s, want %s", got, parent)
	}
	status, err := runner.Status(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Files) == 0 {
		t.Fatal("expected the undone commit's tree as staged changes")
	}
}

func TestPreviewUndoCommitRefusesARoot(t *testing.T) {
	isolateGitConfiguration(t)
	dir := filepath.Join(t.TempDir(), "repo")
	runner := git.NewRunner(nil)
	ctx := context.Background()
	runGit(t, runner, filepath.Dir(dir), "init", "-b", "main", dir)
	runGit(t, runner, dir, "config", "user.name", "yagit Test")
	runGit(t, runner, dir, "config", "user.email", "test@yagit.local")

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")

	_, err := runner.PreviewUndo(ctx, dir)
	if !errors.Is(err, git.ErrCannotUndoRoot) {
		t.Fatalf("err = %v, want ErrCannotUndoRoot", err)
	}
}

func TestPreviewUndoCheckout(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	runGit(t, runner, dir, "switch", "-c", "side")
	write(t, dir, "f.txt", "side\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "on side")
	side := shaOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")

	preview, err := runner.PreviewUndo(ctx, dir)
	if err != nil {
		t.Fatalf("PreviewUndo: %v", err)
	}
	if preview.Kind != git.UndoCheckout || preview.ToRef != "side" || preview.Detach {
		t.Fatalf("preview = %+v", preview)
	}
	if preview.To != side {
		t.Errorf("to = %s, want side tip %s", preview.To, side)
	}
	if !strings.Contains(preview.Command, "switch --no-guess") {
		t.Errorf("command = %q", preview.Command)
	}

	if err := runner.Undo(ctx, dir, preview); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if head.Name != "side" || head.Detached {
		t.Fatalf("HEAD = %+v, want on side", head)
	}
}

func TestPreviewUndoReset(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	write(t, dir, "f.txt", "two\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "second")
	tip := shaOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "reset", "--hard", "HEAD~1")
	after := shaOf(t, runner, dir, "HEAD")

	preview, err := runner.PreviewUndo(ctx, dir)
	if err != nil {
		t.Fatalf("PreviewUndo: %v", err)
	}
	if preview.Kind != git.UndoReset {
		t.Errorf("kind = %q, want reset", preview.Kind)
	}
	if preview.Head != after || preview.To != tip {
		t.Errorf("lease head=%s to=%s, want %s → %s", preview.Head, preview.To, after, tip)
	}
	if preview.Subject != "second" {
		t.Errorf("subject = %q, want the commit the branch returns to", preview.Subject)
	}
	if !strings.Contains(preview.Command, "reset --soft") {
		t.Errorf("command = %q, want soft reset", preview.Command)
	}

	if err := runner.Undo(ctx, dir, preview); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := shaOf(t, runner, dir, "HEAD"); got != tip {
		t.Fatalf("HEAD = %s, want %s", got, tip)
	}
}

// Undoing an undo: the soft reset the first one ran is itself a reset entry,
// so the commit comes back — and the index round-trips to clean, which is the
// property that makes the pair a real undo/redo rather than two edits.
func TestUndoOfAnUndoRestoresTheCommit(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	write(t, dir, "f.txt", "two\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "second")
	tip := shaOf(t, runner, dir, "HEAD")

	undoCommit, err := runner.PreviewUndo(ctx, dir)
	if err != nil {
		t.Fatalf("PreviewUndo commit: %v", err)
	}
	if err := runner.Undo(ctx, dir, undoCommit); err != nil {
		t.Fatalf("Undo commit: %v", err)
	}

	redo, err := runner.PreviewUndo(ctx, dir)
	if err != nil {
		t.Fatalf("PreviewUndo reset: %v", err)
	}
	if redo.Kind != git.UndoReset || redo.To != tip {
		t.Fatalf("redo = %+v, want a reset back to %s", redo, tip)
	}
	if err := runner.Undo(ctx, dir, redo); err != nil {
		t.Fatalf("Undo reset: %v", err)
	}
	if got := shaOf(t, runner, dir, "HEAD"); got != tip {
		t.Fatalf("HEAD = %s, want %s", got, tip)
	}
	status, err := runner.Status(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Files) != 0 {
		t.Fatalf("expected a clean tree after the round trip, got %+v", status.Files)
	}
}

// `git stash push` resets to HEAD on its way past, and so does a discard typed
// as `git reset --hard HEAD`. Both leave a reset entry at the tip having moved
// no ref, and undo must not offer a command that does nothing.
func TestPreviewUndoRefusesAResetThatMovedNothing(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	write(t, dir, "f.txt", "dirty\n")
	runGit(t, runner, dir, "stash", "push", "-m", "aside")

	_, err := runner.PreviewUndo(ctx, dir)
	if !errors.Is(err, git.ErrNothingToUndo) {
		t.Fatalf("err = %v, want ErrNothingToUndo after a stash", err)
	}
}

func TestUndoCommitRefusesAMovedHead(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	parent := shaOf(t, runner, dir, "HEAD")
	write(t, dir, "f.txt", "two\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "second")
	tip := shaOf(t, runner, dir, "HEAD")

	write(t, dir, "f.txt", "three\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "third")

	err := runner.Undo(ctx, dir,
		git.UndoPreview{Kind: git.UndoCommit, Head: tip, To: parent, ToRef: parent})
	if !errors.Is(err, git.ErrUndoStale) {
		t.Fatalf("err = %v, want ErrUndoStale", err)
	}
}

func TestPreviewUndoBranchDeletion(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	runGit(t, runner, dir, "switch", "-c", "side")
	write(t, dir, "f.txt", "side\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "work only side had")
	side := shaOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")
	head := shaOf(t, runner, dir, "HEAD")

	tip, err := runner.BranchTip(ctx, dir, "side")
	if err != nil {
		t.Fatalf("BranchTip: %v", err)
	}
	if tip != side {
		t.Fatalf("tip = %s, want %s", tip, side)
	}
	runGit(t, runner, dir, "branch", "-D", "side")

	preview, err := runner.PreviewUndoBranchDeletion(ctx, dir, "side", tip, head)
	if err != nil {
		t.Fatalf("PreviewUndoBranchDeletion: %v", err)
	}
	if preview.Kind != git.UndoBranchDelete || preview.Branch != "side" || preview.To != side {
		t.Fatalf("preview = %+v", preview)
	}
	if preview.Subject != "work only side had" {
		t.Errorf("subject = %q", preview.Subject)
	}
	if !strings.Contains(preview.Command, "branch -- side") {
		t.Errorf("command = %q", preview.Command)
	}

	if err := runner.Undo(ctx, dir, preview); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := shaOf(t, runner, dir, "side"); got != side {
		t.Fatalf("side = %s, want %s", got, side)
	}
	// The restore makes a branch and stands nowhere new.
	if got := shaOf(t, runner, dir, "HEAD"); got != head {
		t.Fatalf("HEAD = %s, want it unmoved at %s", got, head)
	}
}

// The record is only the most recent action while HEAD has not moved. Once it
// has, whatever moved it is more recent and the reflog owns the offer.
func TestPreviewUndoBranchDeletionYieldsOnceHeadMoves(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	runGit(t, runner, dir, "branch", "side")
	tip, err := runner.BranchTip(ctx, dir, "side")
	if err != nil {
		t.Fatal(err)
	}
	head := shaOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "branch", "-D", "side")

	write(t, dir, "f.txt", "two\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "something after the delete")

	_, err = runner.PreviewUndoBranchDeletion(ctx, dir, "side", tip, head)
	if !errors.Is(err, git.ErrNothingToUndo) {
		t.Fatalf("err = %v, want ErrNothingToUndo once HEAD has moved", err)
	}
}

func TestPreviewUndoBranchDeletionRefusesANameThatIsBack(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	runGit(t, runner, dir, "branch", "side")
	tip, err := runner.BranchTip(ctx, dir, "side")
	if err != nil {
		t.Fatal(err)
	}
	head := shaOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "branch", "-D", "side")
	runGit(t, runner, dir, "branch", "side")

	_, err = runner.PreviewUndoBranchDeletion(ctx, dir, "side", tip, head)
	if !errors.Is(err, git.ErrBranchIsBack) {
		t.Fatalf("err = %v, want ErrBranchIsBack", err)
	}
}

func TestPreviewUndoBranchDeletionRefusesACollectedCommit(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "f.txt")
	runGit(t, runner, dir, "commit", "-m", "first")
	head := shaOf(t, runner, dir, "HEAD")

	// An object name of the right shape that this repository has never held.
	gone := "0123456789012345678901234567890123456789"
	_, err := runner.PreviewUndoBranchDeletion(ctx, dir, "side", gone, head)
	if !errors.Is(err, git.ErrDeletedWorkIsGone) {
		t.Fatalf("err = %v, want ErrDeletedWorkIsGone", err)
	}
}
