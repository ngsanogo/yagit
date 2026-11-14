package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Two branches that have diverged: main is ahead by one, topic ahead by two
// from the fork — so comparing HEAD=topic against refs/heads/main is
// ahead=2, behind=1.
func TestCompareAheadBehind(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "base")
	runGit(t, runner, dir, "branch", "topic")
	commitEmpty(t, runner, dir, "on main")
	runGit(t, runner, dir, "switch", "topic")
	commitEmpty(t, runner, dir, "topic one")
	commitEmpty(t, runner, dir, "topic two")

	result, err := runner.Compare(context.Background(), dir, "refs/heads/main")
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.Ahead != 2 || result.Behind != 1 {
		t.Fatalf("ahead/behind = %d/%d, want 2/1", result.Ahead, result.Behind)
	}
	if result.Head != "topic" {
		t.Errorf("Head = %q, want topic", result.Head)
	}
	if result.AgainstShort != "main" {
		t.Errorf("AgainstShort = %q, want main", result.AgainstShort)
	}
}

func TestCompareRefusesShortName(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "A")

	_, err := runner.Compare(context.Background(), dir, "main")
	if !errors.Is(err, git.ErrBadRefName) {
		t.Fatalf("Compare(short) = %v, want ErrBadRefName", err)
	}
}

func TestCompareSameTip(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	runGit(t, runner, dir, "init", "-b", "main")
	commitEmpty(t, runner, dir, "A")
	runGit(t, runner, dir, "branch", "also")

	result, err := runner.Compare(context.Background(), dir, "refs/heads/also")
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if result.Ahead != 0 || result.Behind != 0 {
		t.Fatalf("ahead/behind = %d/%d, want 0/0", result.Ahead, result.Behind)
	}
}
