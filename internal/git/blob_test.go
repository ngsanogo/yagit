package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestShowBlobReadsIndexAndHEAD(t *testing.T) {
	t.Parallel()

	dir, runner := workingDirectory(t)
	ctx := context.Background()

	committed := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	staged := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 4, 5, 6}
	writeBytes(t, dir, "logo.png", committed)
	runGit(t, runner, dir, "add", "--", "logo.png")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "first")...)
	writeBytes(t, dir, "logo.png", staged)
	runGit(t, runner, dir, "add", "--", "logo.png")

	fromHead, err := runner.ShowBlob(ctx, dir, "head", "logo.png")
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	if string(fromHead) != string(committed) {
		t.Fatalf("head = %v, want committed bytes", fromHead)
	}

	fromIndex, err := runner.ShowBlob(ctx, dir, "index", "logo.png")
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if string(fromIndex) != string(staged) {
		t.Fatalf("index = %v, want staged bytes", fromIndex)
	}
}

func TestShowBlobRefusesADashRevision(t *testing.T) {
	t.Parallel()

	dir, runner := workingDirectory(t)

	_, err := runner.ShowBlob(context.Background(), dir, "--output=/tmp/x", "a.png")
	if !errors.Is(err, git.ErrBadRevision) {
		t.Fatalf("err = %v, want ErrBadRevision", err)
	}
}

func TestShowBlobRefusesWorktreeAsAnObject(t *testing.T) {
	t.Parallel()

	dir, runner := workingDirectory(t)

	_, err := runner.ShowBlob(context.Background(), dir, "worktree", "a.png")
	if !errors.Is(err, git.ErrBadRevision) {
		t.Fatalf("err = %v, want ErrBadRevision", err)
	}
}

func writeBytes(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
