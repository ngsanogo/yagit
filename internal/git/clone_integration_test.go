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

func TestCloneArgsPutsURLAndPathAfterTheDashDash(t *testing.T) {
	t.Parallel()

	args := git.CloneArgs("-v", "-o")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-- -v -o") {
		t.Fatalf("CloneArgs = %v; URL and path must sit after --", args)
	}
	if args[0] != "clone" || args[1] != "--progress" {
		t.Fatalf("CloneArgs = %v; want clone --progress first", args)
	}
}

func TestClonePlanCommandRedactsCredentials(t *testing.T) {
	t.Parallel()

	line := git.ClonePlanCommand("https://ada:ghp_secret@example.test/x.git", "/tmp/x")
	if strings.Contains(line, "ghp_secret") {
		t.Fatalf("plan still holds the password: %s", line)
	}
	if !strings.Contains(line, "ada:***") {
		t.Fatalf("plan = %q; want the user kept and the password hidden", line)
	}
}

func TestCloneCopiesALocalRepositoryAndReportsProgress(t *testing.T) {
	t.Parallel()

	server, _, runner := clonedPair(t)
	destination := filepath.Join(t.TempDir(), "fresh")

	var lines []string
	err := runner.Clone(context.Background(), server, destination, func(line string) {
		lines = append(lines, line)
	})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destination, ".git")); err != nil {
		t.Fatalf("cloned repository missing .git: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("OnProgress received no lines; --progress should have written some")
	}
}

func TestCloneRefusesAnEmptyURL(t *testing.T) {
	t.Parallel()

	runner := git.NewRunner(nil)
	err := runner.Clone(context.Background(), "", filepath.Join(t.TempDir(), "x"), nil)
	if !errors.Is(err, git.ErrEmptyCloneURL) {
		t.Fatalf("error = %v, want ErrEmptyCloneURL", err)
	}
}

func TestCloneSurfacesGitFailure(t *testing.T) {
	t.Parallel()

	runner := git.NewRunner(nil)
	destination := filepath.Join(t.TempDir(), "nowhere")
	err := runner.Clone(context.Background(), filepath.Join(t.TempDir(), "missing.git"), destination, nil)
	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error = %v, want a git.Error", err)
	}
	if gitError.Stderr == "" {
		t.Fatal("git failure carried no stderr")
	}
}
