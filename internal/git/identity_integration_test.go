package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// globalConfigIn points the global configuration at a file the test owns,
// rather than at os.DevNull from isolateGitConfiguration, so --global writes
// land somewhere they can be read back.
func globalConfigIn(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	global := filepath.Join(home, ".gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	return global
}

func TestReadAndSetIdentity(t *testing.T) {
	globalConfigIn(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	before, err := runner.ReadIdentity(ctx)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if before.Name != "" || before.Email != "" {
		t.Fatalf("expected empty identity, got %+v", before)
	}

	if err := runner.SetIdentity(ctx, "Ada Lovelace", "ada@example.com"); err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}

	after, err := runner.ReadIdentity(ctx)
	if err != nil {
		t.Fatalf("ReadIdentity after set: %v", err)
	}
	if after.Name != "Ada Lovelace" || after.Email != "ada@example.com" {
		t.Fatalf("after set = %+v", after)
	}
}

// The daemon reads with no repository of its own, from whatever directory it
// was started in. A checkout there with its own user.name must not become the
// value the form shows, since the save writes the global one.
func TestReadIdentityIgnoresTheRepositoryItRunsIn(t *testing.T) {
	globalConfigIn(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()

	if err := runner.SetIdentity(ctx, "Ada Lovelace", "ada@example.com"); err != nil {
		t.Fatalf("SetIdentity: %v", err)
	}
	repository := t.TempDir()
	runGit(t, runner, repository, "init", "-b", "main")
	runGit(t, runner, repository, "config", "user.name", "Somebody Local")
	t.Chdir(repository)

	read, err := runner.ReadIdentity(ctx)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if read.Name != "Ada Lovelace" {
		t.Fatalf("Name = %q, want the global value, not the repository's", read.Name)
	}
}

// --global alone does not follow include.path, and an identity kept in an
// included file is an ordinary way to arrange one.
func TestReadIdentityFollowsIncludes(t *testing.T) {
	global := globalConfigIn(t)
	included := filepath.Join(filepath.Dir(global), "identity.gitconfig")
	if err := os.WriteFile(included,
		[]byte("[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global,
		[]byte("[include]\n\tpath = "+filepath.ToSlash(included)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	read, err := git.NewRunner(nil).ReadIdentity(context.Background())
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if read.Name != "Ada Lovelace" || read.Email != "ada@example.com" {
		t.Fatalf("identity = %+v, want the included values", read)
	}
}

func TestSetIdentityRefusesABlankOrMultilineValue(t *testing.T) {
	globalConfigIn(t)
	runner := git.NewRunner(nil)

	for _, input := range [][2]string{
		{" ", "ada@example.com"},
		{"Ada Lovelace", ""},
		{"Ada\nLovelace", "ada@example.com"},
	} {
		if err := runner.SetIdentity(context.Background(), input[0], input[1]); !errors.Is(err, git.ErrBadIdentity) {
			t.Errorf("SetIdentity(%q, %q) = %v, want ErrBadIdentity", input[0], input[1], err)
		}
	}
}
