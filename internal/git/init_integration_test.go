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

func TestInitMakesARepositoryOnTheNamedBranch(t *testing.T) {
	isolateGitConfiguration(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "made")

	if err := runner.Init(ctx, dir, "trunk"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	head, err := runner.Run(ctx, dir, "symbolic-ref", "HEAD")
	if err != nil {
		t.Fatalf("symbolic-ref: %v", err)
	}
	if got := strings.TrimSpace(string(head)); got != "refs/heads/trunk" {
		t.Fatalf("HEAD = %q, want refs/heads/trunk", got)
	}
}

// The whole reason the flag is pinned: what a bare `git init` calls the first
// branch is a config key, so the command on screen has to name it.
func TestInitArgsPinTheBranch(t *testing.T) {
	line := git.CommandLine(git.InitArgs("/tmp/made", "trunk"))
	if !strings.Contains(line, "--initial-branch=trunk") {
		t.Fatalf("command = %q", line)
	}
	if !strings.Contains(line, "-- /tmp/made") {
		t.Fatalf("command = %q, want the path after a separator", line)
	}
}

// git checks the name too, and does it AFTER making the directory — leaving a
// path that exists, so the next attempt fails for the wrong reason.
func TestInitRefusesABadBranchWithoutMakingAnything(t *testing.T) {
	isolateGitConfiguration(t)
	runner := git.NewRunner(nil)
	dir := filepath.Join(t.TempDir(), "unmade")

	err := runner.Init(context.Background(), dir, "no spaces allowed")
	if !errors.Is(err, git.ErrBadInitialBranch) {
		t.Fatalf("err = %v, want ErrBadInitialBranch", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the directory was made anyway: %v", err)
	}
}

func TestCheckInitialBranch(t *testing.T) {
	for _, name := range []string{"main", "release/2.0", "feature-1", "a"} {
		if err := git.CheckInitialBranch(name); err != nil {
			t.Errorf("CheckInitialBranch(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{
		"", "-f", "/leading", "trailing/", "double//slash", "ends.", "has..dots",
		"at@{brace", "@", "with space", "tilde~", "caret^", "colon:", "star*",
		"question?", "bracket[", "back\\slash", "control\x01", "x.lock",
		// Every one of these is refused by git AFTER it has made the
		// directory, which is what makes them worth catching here.
		".hidden", "feature/.hidden", "x.lock/y",
	} {
		if err := git.CheckInitialBranch(name); !errors.Is(err, git.ErrBadInitialBranch) {
			t.Errorf("CheckInitialBranch(%q) = %v, want ErrBadInitialBranch", name, err)
		}
	}
}

func TestDefaultBranchNameFallsBackWhenUnset(t *testing.T) {
	isolateGitConfiguration(t)
	runner := git.NewRunner(nil)

	if got := runner.DefaultBranchName(context.Background()); got != git.DefaultInitialBranch {
		t.Fatalf("DefaultBranchName = %q, want %q", got, git.DefaultInitialBranch)
	}
}
