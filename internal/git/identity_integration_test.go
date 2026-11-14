package git_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestReadAndSetIdentity(t *testing.T) {
	isolateGitConfiguration(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Point global config at a real file under HOME so --global writes land
	// somewhere the test owns, rather than at os.DevNull from isolate.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))

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

func TestSetIdentityRefusesEmpty(t *testing.T) {
	isolateGitConfiguration(t)
	runner := git.NewRunner(nil)
	err := runner.SetIdentity(context.Background(), " ", "ada@example.com")
	if !errors.Is(err, git.ErrEmptyIdentity) {
		t.Fatalf("SetIdentity empty name = %v, want ErrEmptyIdentity", err)
	}
}
