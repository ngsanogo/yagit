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

// allowFileSubmodules points GIT_CONFIG_GLOBAL at a config that permits the
// file transport, in place of the /dev/null isolateGitConfiguration uses.
//
// git refuses to clone a submodule over `file://` by default — the fix for
// CVE-2022-39253 — and it reads that setting from the global or system config
// rather than from the repository, because the clone runs outside one. yagit
// never sets it and must not: turning a security control off on every user's
// behalf is not a client's decision. A test machine saying so about itself is
// exactly what a user who wants local submodules would do.
func allowFileSubmodules(t *testing.T) {
	t.Helper()
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[protocol \"file\"]\n\tallow = always\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// superproject builds a repository with one submodule pinned inside it.
func superproject(t *testing.T) (super, sub string, runner *git.Runner) {
	t.Helper()
	allowFileSubmodules(t)
	runner = git.NewRunner(nil)
	root := t.TempDir()

	sub = filepath.Join(root, "lib")
	runGit(t, runner, root, "init", "-b", "main", sub)
	runGit(t, runner, sub, "config", "user.name", "yagit Test")
	runGit(t, runner, sub, "config", "user.email", "test@yagit.local")
	write(t, sub, "lib.txt", "one\n")
	runGit(t, runner, sub, "add", "-A")
	runGit(t, runner, sub, "commit", "-m", "the library's first commit")

	super = filepath.Join(root, "super")
	runGit(t, runner, root, "init", "-b", "main", super)
	runGit(t, runner, super, "config", "user.name", "yagit Test")
	runGit(t, runner, super, "config", "user.email", "test@yagit.local")
	write(t, super, "readme.md", "the superproject\n")
	runGit(t, runner, super, "add", "-A")
	runGit(t, runner, super, "commit", "-m", "first")

	if err := runner.AddSubmodule(context.Background(), super, sub, "vendor/lib"); err != nil {
		t.Fatalf("AddSubmodule: %v", err)
	}
	runGit(t, runner, super, "commit", "-m", "pin the library")

	return super, sub, runner
}

func TestSubmodulesReadsWhatIsPinned(t *testing.T) {
	super, sub, runner := superproject(t)
	ctx := context.Background()

	submodules, err := runner.Submodules(ctx, super)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 1 {
		t.Fatalf("submodules = %+v", submodules)
	}

	only := submodules[0]
	if only.Path != "vendor/lib" || only.Name != "vendor/lib" {
		t.Errorf("submodule = %+v", only)
	}
	if !only.Declared || !only.Initialised || !only.Present {
		t.Errorf("submodule = %+v, want it declared, initialised and checked out", only)
	}
	if only.Moved {
		t.Errorf("a fresh add is at the commit it records: %+v", only)
	}
	if only.Recorded != shaOf(t, runner, sub, "HEAD") {
		t.Errorf("recorded = %s, want the library's tip", only.Recorded)
	}
	if only.URL == "" {
		t.Errorf("submodule = %+v, want the URL .gitmodules declares", only)
	}
}

// A repository that pins nothing answers with nothing rather than failing:
// `git config -f .gitmodules` on a missing file exits 1, and that is the
// answer here.
func TestSubmodulesOnARepositoryWithNone(t *testing.T) {
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	submodules, err := runner.Submodules(context.Background(), dir)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 0 {
		t.Fatalf("submodules = %+v", submodules)
	}
}

// The state `git submodule status` marks with `+`, and the one that becomes a
// staged change in the superproject.
func TestSubmodulesReportsACheckoutThatMoved(t *testing.T) {
	super, _, runner := superproject(t)
	ctx := context.Background()
	inside := filepath.Join(super, "vendor", "lib")

	// The checkout is a fresh clone: the identity set in the source repository
	// is local config, and local config is not what a clone copies.
	runGit(t, runner, inside, "config", "user.name", "yagit Test")
	runGit(t, runner, inside, "config", "user.email", "test@yagit.local")
	write(t, inside, "lib.txt", "two\n")
	runGit(t, runner, inside, "add", "-A")
	runGit(t, runner, inside, "commit", "-m", "a commit the superproject does not pin")

	submodules, err := runner.Submodules(ctx, super)
	if err != nil {
		t.Fatalf("Submodules: %v", err)
	}
	if len(submodules) != 1 || !submodules[0].Moved {
		t.Fatalf("submodules = %+v, want the checkout marked as moved", submodules)
	}
	if submodules[0].HEAD == submodules[0].Recorded {
		t.Fatalf("submodule = %+v, want two different commits", submodules[0])
	}
}

// A fresh clone has the gitlink and no checkout. Update is what fills it, and
// --init is what makes that work the first time.
func TestUpdateSubmodulesFillsAnEmptyCheckout(t *testing.T) {
	super, _, runner := superproject(t)
	ctx := context.Background()
	inside := filepath.Join(super, "vendor", "lib")

	// Deinit is what a never-updated clone looks like: the gitlink is there
	// and the directory is empty.
	runGit(t, runner, super, "submodule", "deinit", "--force", "--", "vendor/lib")

	before, err := runner.Submodules(ctx, super)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Present || before[0].Initialised {
		t.Fatalf("submodules = %+v, want one that is neither initialised nor checked out", before)
	}

	if err := runner.UpdateSubmodules(ctx, super, ""); err != nil {
		t.Fatalf("UpdateSubmodules: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inside, "lib.txt")); err != nil {
		t.Fatalf("the checkout was not filled: %v", err)
	}

	after, err := runner.Submodules(ctx, super)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || !after[0].Present || !after[0].Initialised {
		t.Fatalf("submodules = %+v, want it initialised and checked out", after)
	}
}

func TestRemoveSubmoduleUnpinsIt(t *testing.T) {
	super, _, runner := superproject(t)
	ctx := context.Background()

	if err := runner.RemoveSubmodule(ctx, super, "vendor/lib", false); err != nil {
		t.Fatalf("RemoveSubmodule: %v", err)
	}

	submodules, err := runner.Submodules(ctx, super)
	if err != nil {
		t.Fatal(err)
	}
	if len(submodules) != 0 {
		t.Fatalf("submodules = %+v, want none", submodules)
	}

	// git's design, and what the confirmation says out loud: the objects stay.
	if _, err := os.Stat(filepath.Join(super, ".git", "modules", "vendor", "lib")); err != nil {
		t.Fatalf(".git/modules was removed, which git does not do: %v", err)
	}
}

func TestRemoveSubmoduleArgsAreTwoCommands(t *testing.T) {
	argsets := git.RemoveSubmoduleArgs("vendor/lib", true)
	if len(argsets) != 2 {
		t.Fatalf("argsets = %v", argsets)
	}
	if got := strings.Join(argsets[0], " "); got != "submodule deinit --force -- vendor/lib" {
		t.Errorf("first = %q", got)
	}
	if got := strings.Join(argsets[1], " "); got != "rm --force -- vendor/lib" {
		t.Errorf("second = %q", got)
	}
}

func TestSyncSubmodulesCopiesANewURL(t *testing.T) {
	super, sub, runner := superproject(t)
	ctx := context.Background()

	moved := sub + "-moved"
	if err := os.Rename(sub, moved); err != nil {
		t.Fatal(err)
	}
	runGit(t, runner, super, "config", "--file", ".gitmodules", "submodule.vendor/lib.url", moved)

	if err := runner.SyncSubmodules(ctx, super, ""); err != nil {
		t.Fatalf("SyncSubmodules: %v", err)
	}

	output, err := runner.Run(ctx, super, "config", "--get", "submodule.vendor/lib.url")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(output)); got != moved {
		t.Fatalf("config url = %q, want %q", got, moved)
	}
}

func TestAddSubmoduleRefusesEmptyFields(t *testing.T) {
	runner := git.NewRunner(nil)
	ctx := context.Background()

	if err := runner.AddSubmodule(ctx, "", "  ", "vendor/lib"); !errors.Is(err, git.ErrEmptySubmoduleURL) {
		t.Errorf("err = %v, want ErrEmptySubmoduleURL", err)
	}
	if err := runner.AddSubmodule(ctx, "", "https://example.test/x.git", " "); !errors.Is(err, git.ErrNoSubmodulePath) {
		t.Errorf("err = %v, want ErrNoSubmodulePath", err)
	}
}
