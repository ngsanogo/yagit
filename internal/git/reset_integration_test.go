package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Resetting, against the real binary.
//
// The argument list is the subject of reset_test.go; here the question is
// what git does with each mode — HEAD alone, HEAD and the index, or all three
// trees — and the reading that keeps a foreign commit from being offered.

// resettable is a linear history on main: base → one → two.
func resettable(t *testing.T) (string, *git.Runner) {
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

func TestPreviewResetCountsWhatTheBranchWouldLeave(t *testing.T) {
	dir, runner := resettable(t)
	one := commitOf(t, runner, dir, "main~1")

	preview, err := runner.PreviewReset(context.Background(), dir, one, git.ResetMixed)
	if err != nil {
		t.Fatalf("PreviewReset: %v", err)
	}
	if preview.Mode != git.ResetMixed {
		t.Errorf("mode = %q, want mixed", preview.Mode)
	}
	if preview.Commit != one {
		t.Errorf("commit = %s, want the full name %s", preview.Commit, one)
	}
	if preview.Subject != "one" {
		t.Errorf("subject = %q, want the commit's own", preview.Subject)
	}
	if preview.Dropping != 1 {
		t.Errorf("Dropping = %d, want the one commit past the target", preview.Dropping)
	}
	if preview.DirtyFiles != 0 {
		t.Errorf("DirtyFiles = %d, want a clean tree", preview.DirtyFiles)
	}
}

func TestPreviewResetToHEADDropsNothing(t *testing.T) {
	dir, runner := resettable(t)
	head := commitOf(t, runner, dir, "HEAD")

	preview, err := runner.PreviewReset(context.Background(), dir, head, git.ResetHard)
	if err != nil {
		t.Fatalf("PreviewReset(HEAD): %v", err)
	}
	if preview.Dropping != 0 {
		t.Errorf("Dropping = %d, want none: the target is HEAD", preview.Dropping)
	}
}

// A hard reset puts tracked files back and leaves untracked ones alone, so the
// count the confirmation names under "This will permanently discard" must be
// the tracked ones only.
func TestPreviewResetCountsOnlyTheTrackedFilesAHardResetWouldDiscard(t *testing.T) {
	dir, runner := resettable(t)
	one := commitOf(t, runner, dir, "main~1")
	writeWorkFile(t, dir, "notes.md", "edited\n")
	writeWorkFile(t, dir, "scratch.md", "untracked\n")

	preview, err := runner.PreviewReset(context.Background(), dir, one, git.ResetHard)
	if err != nil {
		t.Fatalf("PreviewReset: %v", err)
	}
	if preview.DirtyFiles != 1 {
		t.Errorf("DirtyFiles = %d, want 1: the edited file, not the untracked one",
			preview.DirtyFiles)
	}

	// And the reason: git leaves scratch.md exactly where it was.
	if err := runner.Reset(context.Background(), dir, one, git.ResetHard); err != nil {
		t.Fatalf("Reset --hard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.md")); err != nil {
		t.Errorf("scratch.md gone after a hard reset: %v", err)
	}
}

func TestPreviewResetRefusesACommitNotOnTheBranch(t *testing.T) {
	dir, runner := resettable(t)

	runGit(t, runner, dir, "switch", "-c", "side", "main~2")
	writeWorkFile(t, dir, "side.md", "only side\n")
	runGit(t, runner, dir, "add", "--", "side.md")
	runGit(t, runner, dir, commitWith("side only")...)
	side := commitOf(t, runner, dir, "HEAD")
	runGit(t, runner, dir, "switch", "main")

	_, err := runner.PreviewReset(context.Background(), dir, side, git.ResetHard)
	if !errors.Is(err, git.ErrResetNotOnBranch) {
		t.Fatalf("PreviewReset(side) = %v, want ErrResetNotOnBranch", err)
	}
}

func TestResetSoftKeepsTheIndexAndTheWorkTree(t *testing.T) {
	dir, runner := resettable(t)
	one := commitOf(t, runner, dir, "main~1")
	two := commitOf(t, runner, dir, "HEAD")

	if err := runner.Reset(context.Background(), dir, one, git.ResetSoft); err != nil {
		t.Fatalf("Reset --soft: %v", err)
	}

	head := headOf(t, runner, dir)
	if head.SHA != one {
		t.Errorf("HEAD at %s, want %s", head.SHA, one)
	}
	// Soft leaves the index at "two"'s tree, so extra.md is still staged.
	status, err := runner.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Files) == 0 {
		t.Fatal("soft reset left nothing staged; want the dropped commit as staged changes")
	}
	if got := commitOf(t, runner, dir, "HEAD"); got == two {
		t.Error("HEAD did not move")
	}
}

func TestResetMixedUnstagesWhatSoftWouldKeep(t *testing.T) {
	dir, runner := resettable(t)
	one := commitOf(t, runner, dir, "main~1")

	if err := runner.Reset(context.Background(), dir, one, git.ResetMixed); err != nil {
		t.Fatalf("Reset --mixed: %v", err)
	}

	if head := headOf(t, runner, dir); head.SHA != one {
		t.Errorf("HEAD at %s, want %s", head.SHA, one)
	}
	// Mixed moves the index with HEAD; the work tree still holds "two"'s files
	// as unstaged changes.
	if _, err := os.Stat(filepath.Join(dir, "extra.md")); err != nil {
		t.Errorf("extra.md missing after mixed reset: %v", err)
	}
	status, err := runner.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	staged := false
	for _, file := range status.Files {
		if file.Staged() {
			staged = true
			break
		}
	}
	if staged {
		t.Error("mixed reset left staged changes; want them unstaged against the new tip")
	}
}

func TestResetHardDiscardsTheWorkTree(t *testing.T) {
	dir, runner := resettable(t)
	one := commitOf(t, runner, dir, "main~1")
	writeWorkFile(t, dir, "notes.md", "edited\n")

	if err := runner.Reset(context.Background(), dir, one, git.ResetHard); err != nil {
		t.Fatalf("Reset --hard: %v", err)
	}

	if head := headOf(t, runner, dir); head.SHA != one {
		t.Errorf("HEAD at %s, want %s", head.SHA, one)
	}
	body, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if err != nil {
		t.Fatalf("reading notes.md: %v", err)
	}
	if string(body) != "one\n" {
		t.Errorf("notes.md = %q, want the target's contents", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "extra.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("extra.md still present after hard reset to before it was added: %v", err)
	}
	status, err := runner.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Files) != 0 {
		t.Errorf("hard reset left %d dirty paths", len(status.Files))
	}
}
