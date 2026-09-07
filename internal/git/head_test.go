package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestReadHEADOnABranch(t *testing.T) {
	dir, runner := testRepository(t)

	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}

	// Checked against the log rather than against a literal: --topo-order puts
	// the tip of main first, and comparing the two is what would catch a SHA
	// read from the wrong line of output.
	commits, err := runner.Log(context.Background(), dir)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if head.SHA != commits[0].SHA {
		t.Errorf("SHA = %q, want the tip of main %q", head.SHA, commits[0].SHA)
	}
	if head.Name != "main" {
		t.Errorf("Name = %q, want main", head.Name)
	}
	if head.Detached {
		t.Error("a branch checkout is not detached")
	}
}

func TestReadHEADWhenDetached(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	onBranch, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}

	runGit(t, runner, dir, "checkout", "--detach", "HEAD")

	detached, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatalf("ReadHEAD: %v", err)
	}
	if !detached.Detached {
		t.Error("expected a detached HEAD")
	}
	if detached.Name != "HEAD" {
		t.Errorf("Name = %q, want HEAD: a detached HEAD is known by no branch name", detached.Name)
	}
	// Detaching moves no commit, so the two states differ by the name alone.
	// That is the difference a caller watching HEAD has to be able to see.
	if detached.SHA != onBranch.SHA {
		t.Errorf("SHA = %q after detaching, want the same commit %q", detached.SHA, onBranch.SHA)
	}
}

// TestReadHEADWithNoCommitYet covers the repository right after git init, and
// covers it twice over: the answer is the zero value, and getting there costs
// no failed command. The observer behind the Runner feeds the log panel, so a
// git command that fails here would put a fatal in front of the user on every
// refresh of a repository where nothing is wrong.
func TestReadHEADWithNoCommitYet(t *testing.T) {
	isolateGitConfiguration(t)

	var failures []git.Execution
	runner := git.NewRunner(func(execution git.Execution) {
		if execution.ExitCode != 0 {
			failures = append(failures, execution)
		}
	})

	dir := t.TempDir()
	runGit(t, runner, dir, "init", "-b", "main")

	head, err := runner.ReadHEAD(context.Background(), dir)
	if err != nil {
		t.Fatalf("a repository with no commit is a normal state, not an error: %v", err)
	}
	if head != (git.HEAD{}) {
		t.Errorf("head = %+v, want the zero value: there is no commit to point at", head)
	}
	for _, failure := range failures {
		t.Errorf("%s exited %d with %q: an unborn HEAD must be read without failing a command",
			failure.CommandLine(), failure.ExitCode, strings.TrimSpace(failure.Stderr))
	}
}

// TestReadHEADOutsideARepository is the other half of the same branch: an
// unborn HEAD is answered with zero values, and everything else git calls
// fatal has to travel. A directory that is not a repository reading as a
// repository without commits would hide the real problem behind an empty
// history.
func TestReadHEADOutsideARepository(t *testing.T) {
	isolateGitConfiguration(t)

	// A .git file pointing nowhere, rather than a bare directory: git walks up
	// to the parent directories, and a temporary directory that happens to sit
	// inside somebody's checkout would answer that checkout's HEAD.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"),
		[]byte("gitdir: "+filepath.Join(dir, "nowhere")+"\n"), 0o600); err != nil {
		t.Fatalf("writing the .git file: %v", err)
	}

	head, err := git.NewRunner(nil).ReadHEAD(context.Background(), dir)
	if err == nil {
		t.Fatalf("head = %+v with no error: a directory that is not a repository is not a repository without commits", head)
	}
	if head != (git.HEAD{}) {
		t.Errorf("head = %+v alongside an error, want the zero value", head)
	}
}
