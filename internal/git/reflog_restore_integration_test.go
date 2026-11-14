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
		context.Background(), dir, entries[1].Selector, git.ResetSoft, 20)
	if err != nil {
		t.Fatalf("PreviewReflogRestore: %v", err)
	}
	if preview.SHA != first {
		t.Fatalf("preview SHA = %s, want %s", preview.SHA, first)
	}

	if err := runner.ReflogRestore(context.Background(), dir, preview); err != nil {
		t.Fatalf("ReflogRestore: %v", err)
	}
	if got := commitOf(t, runner, dir, "HEAD"); got != first {
		t.Fatalf("HEAD = %s, want %s", got, first)
	}
}

func TestReflogRestoreGoneEntry(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "only")

	_, err := runner.PreviewReflogRestore(
		context.Background(), dir, "HEAD@{99}", git.ResetMixed, 5)
	if !errors.Is(err, git.ErrReflogEntryGone) {
		t.Fatalf("PreviewReflogRestore = %v, want ErrReflogEntryGone", err)
	}
}
