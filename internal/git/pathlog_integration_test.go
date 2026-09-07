package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestLogPathFollowsARename(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "notes.md", "first\n")
	runGit(t, runner, dir, "add", "notes.md")
	runGit(t, runner, dir, "commit", "-m", "add notes")

	runGit(t, runner, dir, "mv", "notes.md", "README.md")
	runGit(t, runner, dir, "commit", "-m", "rename to README")

	commits, err := runner.LogPath(ctx, dir, "HEAD", "README.md")
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("commits = %d, want 2 (rename + add, via --follow)", len(commits))
	}
	if commits[0].Subject != "rename to README" {
		t.Errorf("newest subject = %q", commits[0].Subject)
	}
	if commits[1].Subject != "add notes" {
		t.Errorf("older subject = %q", commits[1].Subject)
	}
}

func TestLogPathStartsAtARevision(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	write(t, dir, "a.txt", "one\n")
	runGit(t, runner, dir, "add", "a.txt")
	runGit(t, runner, dir, "commit", "-m", "one")
	first := shaOf(t, runner, dir, "HEAD")

	write(t, dir, "a.txt", "two\n")
	runGit(t, runner, dir, "add", "a.txt")
	runGit(t, runner, dir, "commit", "-m", "two")

	commits, err := runner.LogPath(ctx, dir, first, "a.txt")
	if err != nil {
		t.Fatalf("LogPath: %v", err)
	}
	if len(commits) != 1 || commits[0].Subject != "one" {
		t.Fatalf("from first commit: %+v, want only “one”", commits)
	}
}

func TestLogPathRefusesAnEmptyPath(t *testing.T) {
	dir := initRepo(t)
	_, err := git.NewRunner(nil).LogPath(context.Background(), dir, "HEAD", "  ")
	if !errors.Is(err, git.ErrNoPaths) {
		t.Fatalf("err = %v, want ErrNoPaths", err)
	}
}

func write(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
