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

func TestWorktreesAgainstGit(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()
	beside := filepath.Join(filepath.Dir(dir), "linked")

	runGit(t, runner, dir, "branch", "side")
	if err := runner.AddWorktree(ctx, dir, beside, "side", "", false); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	worktrees, err := runner.Worktrees(ctx, dir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
	if len(worktrees) != 2 {
		t.Fatalf("worktrees = %+v", worktrees)
	}
	if !worktrees[0].Main {
		t.Errorf("first = %+v, want the main tree", worktrees[0])
	}
	if worktrees[1].Branch != "side" || worktrees[1].Main {
		t.Errorf("second = %+v, want the linked tree on side", worktrees[1])
	}

	if err := runner.RemoveWorktree(ctx, dir, beside, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(beside); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the directory survived the remove: %v", err)
	}
}

// One branch, one working tree — git's own rule, and the refusal names the
// directory holding it, which is the fact the person needs.
func TestAddWorktreeRefusesABranchAlreadyCheckedOut(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	err := runner.AddWorktree(ctx, dir, filepath.Join(filepath.Dir(dir), "again"), "main", "", false)
	if err == nil {
		t.Fatal("a branch checked out in the main tree was accepted for a second one")
	}
	if !strings.Contains(err.Error(), "already used by worktree") {
		t.Fatalf("err = %v, want git's own sentence", err)
	}
}

// Without --force git refuses a checkout holding uncommitted work, and names
// the files in the way. yagit passes both the refusal and the flag through.
func TestRemoveWorktreeRefusesUncommittedWorkUntilForced(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()
	beside := filepath.Join(filepath.Dir(dir), "dirty")

	runGit(t, runner, dir, "branch", "side")
	if err := runner.AddWorktree(ctx, dir, beside, "side", "", false); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	write(t, beside, "untracked.txt", "not committed\n")

	if err := runner.RemoveWorktree(ctx, dir, beside, false); err == nil {
		t.Fatal("a worktree holding uncommitted work was removed without --force")
	}
	if err := runner.RemoveWorktree(ctx, dir, beside, true); err != nil {
		t.Fatalf("RemoveWorktree --force: %v", err)
	}
}

func TestPruneWorktreesAgainstGit(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()
	beside := filepath.Join(filepath.Dir(dir), "gone")

	runGit(t, runner, dir, "branch", "side")
	if err := runner.AddWorktree(ctx, dir, beside, "side", "", false); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := os.RemoveAll(beside); err != nil {
		t.Fatal(err)
	}

	before, err := runner.Worktrees(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 || !before[1].Prunable {
		t.Fatalf("worktrees = %+v, want the missing one marked prunable", before)
	}

	if err := runner.PruneWorktrees(ctx, dir); err != nil {
		t.Fatalf("PruneWorktrees: %v", err)
	}
	after, err := runner.Worktrees(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 {
		t.Fatalf("worktrees = %+v, want only the main tree", after)
	}
}

func TestAddWorktreeRefusesNoPath(t *testing.T) {
	runner := git.NewRunner(nil)
	if err := runner.AddWorktree(context.Background(), "", "  ", "main", "", false); !errors.Is(err, git.ErrNoWorktreePath) {
		t.Fatalf("err = %v, want ErrNoWorktreePath", err)
	}
}
