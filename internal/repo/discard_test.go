package repo_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ngsanogo/yagit/internal/repo"
)

// A clone that stops partway leaves a directory behind, and until it is gone
// the user cannot try again from inside yagit: the destination check refuses
// a path that exists, and nothing in the interface deletes one.
//
// Removing it is safe for one reason and only that reason — the destination
// did not exist when the clone started, so everything there was written by the
// clone. These tests pin the boundary that makes it safe, because this is the
// one place in the project that removes a directory tree the user did not name
// file by file.

func registryAt(t *testing.T, root string) *repo.Registry {
	t.Helper()
	registry, err := repo.NewRegistry(root, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func TestDiscardRemovesWhatAFailedCloneWrote(t *testing.T) {
	root := t.TempDir()
	registry := registryAt(t, root)

	destination := filepath.Join(root, "half-cloned")
	if err := os.MkdirAll(filepath.Join(destination, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := registry.DiscardIncompleteClone(destination); err != nil {
		t.Fatalf("DiscardIncompleteClone: %v", err)
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Error("the half-written clone is still there, so the next attempt still fails")
	}
}

func TestDiscardSaysNothingWhenThereIsNothing(t *testing.T) {
	root := t.TempDir()
	registry := registryAt(t, root)

	// A clone that failed before creating anything is the ordinary case — a
	// URL that does not resolve — and it must not be reported as a failure to
	// clean up.
	if err := registry.DiscardIncompleteClone(filepath.Join(root, "never-made")); err != nil {
		t.Errorf("removing nothing reported an error: %v", err)
	}
}

func TestDiscardRefusesAPathOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	registry := registryAt(t, root)

	keep := filepath.Join(outside, "somebody-elses-work")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := registry.DiscardIncompleteClone(keep); err == nil {
		t.Error("a path outside the root was accepted for removal")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a directory outside the root was removed: %v", err)
	}
}

func TestDiscardRefusesSomethingThatIsNotADirectory(t *testing.T) {
	root := t.TempDir()
	registry := registryAt(t, root)

	// A clone leaves a directory. Finding a file or a symbolic link at the
	// destination means something else made it, and refusing is the answer
	// that cannot destroy what somebody else was doing.
	file := filepath.Join(root, "a-file")
	if err := os.WriteFile(file, []byte("not a clone"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := registry.DiscardIncompleteClone(file); err == nil {
		t.Error("a plain file was accepted for removal")
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("a file that was not a clone was removed: %v", err)
	}
}

func TestDiscardRefusesARelativePath(t *testing.T) {
	registry := registryAt(t, t.TempDir())

	if err := registry.DiscardIncompleteClone("half-cloned"); err == nil {
		t.Error("a relative path was accepted: what it names depends on the working directory")
	}
}
