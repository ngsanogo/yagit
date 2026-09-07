package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestLogLineTracksAChangedLine(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "notes.md", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "add three lines")

	write(t, dir, "notes.md", "one\ntwo changed\nthree\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "change line two")

	write(t, dir, "notes.md", "one\ntwo changed\nthree\nfour\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "add line four")

	commits, err := runner.LogLine(ctx, dir, "HEAD", "notes.md", 2, 2)
	if err != nil {
		t.Fatalf("LogLine: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2 (change + add of line two; not the four-line commit)", len(commits))
	}
	if commits[0].Subject != "change line two" {
		t.Errorf("newest subject = %q", commits[0].Subject)
	}
	if commits[1].Subject != "add three lines" {
		t.Errorf("older subject = %q", commits[1].Subject)
	}
}

func TestLogLineFollowsARename(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "notes.md", "one\ntwo\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "add notes")

	runGit(t, runner, dir, "mv", "notes.md", "README.md")
	runGit(t, runner, dir, "commit", "-m", "rename to README")

	commits, err := runner.LogLine(ctx, dir, "HEAD", "README.md", 2, 2)
	if err != nil {
		t.Fatalf("LogLine: %v", err)
	}
	if len(commits) < 1 || commits[len(commits)-1].Subject != "add notes" {
		t.Fatalf("did not follow the rename to the introducing commit: %+v", commits)
	}
}

func TestLogLineRefusesABadRange(t *testing.T) {
	dir := initRepo(t)
	_, err := git.NewRunner(nil).LogLine(context.Background(), dir, "HEAD", "a.txt", 0, 1)
	if !errors.Is(err, git.ErrBadLineRange) {
		t.Fatalf("err = %v, want ErrBadLineRange", err)
	}
}

func TestLogLineRefusesAColonInThePath(t *testing.T) {
	dir := initRepo(t)
	_, err := git.NewRunner(nil).LogLine(context.Background(), dir, "HEAD", "a:b.txt", 1, 1)
	if !errors.Is(err, git.ErrNoPaths) {
		t.Fatalf("err = %v, want ErrNoPaths", err)
	}
}
