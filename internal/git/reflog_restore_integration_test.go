package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestReflogRestoreSoft(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "first")
	first := commitOf(t, runner, dir, "HEAD")
	commitEmpty(t, runner, dir, "second")

	entries, err := runner.HeadReflog(context.Background(), dir, 10)
	if err != nil {
		t.Fatalf("HeadReflog: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("reflog has %d entries, want at least 2", len(entries))
	}

	preview, err := runner.PreviewReflogRestore(
		context.Background(), dir, entries[1].Selector, entries[1].SHA, git.ResetSoft, 20)
	if err != nil {
		t.Fatalf("PreviewReflogRestore: %v", err)
	}
	if preview.SHA != first {
		t.Fatalf("preview SHA = %s, want %s", preview.SHA, first)
	}
	// "second" is on HEAD and not in the entry: that is what leaves the branch.
	if preview.Dropping != 1 {
		t.Errorf("Dropping = %d, want 1", preview.Dropping)
	}

	if err := runner.ReflogRestore(context.Background(), dir, preview); err != nil {
		t.Fatalf("ReflogRestore: %v", err)
	}
	if got := commitOf(t, runner, dir, "HEAD"); got != first {
		t.Fatalf("HEAD = %s, want %s", got, first)
	}
}

// The list was read, then HEAD moved: every selector on screen now names the
// moment after the one it showed. The click names what it saw, and the plan
// refuses rather than restoring whatever HEAD@{1} has become.
func TestReflogRestoreRefusesASelectorThatNowNamesAnotherMoment(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "first")
	commitEmpty(t, runner, dir, "second")

	seen, err := runner.HeadReflog(context.Background(), dir, 10)
	if err != nil {
		t.Fatalf("HeadReflog: %v", err)
	}
	commitEmpty(t, runner, dir, "third")

	_, err = runner.PreviewReflogRestore(
		context.Background(), dir, seen[1].Selector, seen[1].SHA, git.ResetMixed, 20)
	if !errors.Is(err, git.ErrReflogEntryGone) {
		t.Fatalf("PreviewReflogRestore = %v, want ErrReflogEntryGone", err)
	}
}

func TestReflogRestoreGoneEntry(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "only")

	_, err := runner.PreviewReflogRestore(
		context.Background(), dir, "HEAD@{99}", commitOf(t, runner, dir, "HEAD"), git.ResetMixed, 5)
	if !errors.Is(err, git.ErrReflogEntryGone) {
		t.Fatalf("PreviewReflogRestore = %v, want ErrReflogEntryGone", err)
	}
}

// An entry ahead of HEAD — a reset undone through the reflog — can track a
// file the work tree now holds untracked, and a hard restore writes over it.
func TestAHardReflogRestoreCountsTheUntrackedFilesItOverwrites(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "first")
	writeFile(t, dir, "notes.txt", "committed\n")
	runGit(t, runner, dir, "add", "--", "notes.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "notes")...)
	runGit(t, runner, dir, "reset", "--hard", "HEAD~1")
	writeFile(t, dir, "notes.txt", "written since, never added\n")

	entries, err := runner.HeadReflog(context.Background(), dir, 10)
	if err != nil {
		t.Fatalf("HeadReflog: %v", err)
	}
	ahead := entries[1]

	hard, err := runner.PreviewReflogRestore(
		context.Background(), dir, ahead.Selector, ahead.SHA, git.ResetHard, 20)
	if err != nil {
		t.Fatalf("PreviewReflogRestore: %v", err)
	}
	if hard.OverwrittenUntracked != 1 || hard.Dropping != 0 {
		t.Fatalf("overwritten = %d, dropping = %d; want 1 and 0", hard.OverwrittenUntracked, hard.Dropping)
	}

	soft, err := runner.PreviewReflogRestore(
		context.Background(), dir, ahead.Selector, ahead.SHA, git.ResetSoft, 20)
	if err != nil {
		t.Fatalf("PreviewReflogRestore: %v", err)
	}
	if soft.OverwrittenUntracked != 0 {
		t.Fatalf("a soft restore counted %d overwritten files; it touches no work tree", soft.OverwrittenUntracked)
	}
}
