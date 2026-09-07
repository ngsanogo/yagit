//go:build !windows

package repo

import (
	"errors"
	"fmt"
	"os"
)

// pinGitDir holds the git directory open for as long as the entry that names
// it, and reads its identity through that same descriptor.
//
// The identity is a device and an inode number, and an inode number is only
// unique among LIVE objects: once a directory is gone the kernel may hand its
// number straight to the next one. Delete a repository, create another in its
// place, and os.SameFile answers "same" about two unrelated directories. That
// is not theory — the test covering exactly this replacement passed on one
// filesystem and failed in CI on another.
//
// An open descriptor makes the number durable: an inode is not freed while
// anything still refers to it, so a replacement is guaranteed a different one
// and the comparison becomes exact rather than likely. It costs one descriptor
// per open repository, for exactly as long as the entry: Close releases it, so
// the count follows what is open rather than everything ever opened.
//
// Windows needs none of this and must not do it; see pin_windows.go.
func pinGitDir(path string) (*os.File, os.FileInfo, error) {
	pinned, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open the git directory %q: %w", path, err)
	}

	// Stat through the descriptor, never the path again. Between an os.Stat and
	// an os.Open the directory can be swapped, and the entry would then describe
	// one object while pinning another — the exact confusion this prevents.
	identity, err := pinned.Stat()
	if err != nil {
		// Joined, not dropped: a close that fails here leaks the descriptor,
		// and a leak nobody is told about is how a daemon runs out of them
		// three weeks later with no clue why.
		return nil, nil, errors.Join(
			fmt.Errorf("cannot inspect the git directory %q: %w", path, err),
			pinned.Close(),
		)
	}

	return pinned, identity, nil
}
