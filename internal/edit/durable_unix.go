//go:build !windows

package edit

import (
	"errors"
	"fmt"
	"os"
)

// A rename is atomic; it is not durable until the directory holding it has
// been flushed. Unix and Windows disagree about how to say that strongly
// enough to deserve a file each, rather than a runtime.GOOS branch inside one.

// syncDirectory flushes a directory's own entries to the disk.
//
// The half of writeAtomically's promise that the temporary file's Sync does
// not buy. ext4 and XFS record the new name in memory and write it out on
// their own schedule, so a power cut in between can leave the directory with
// neither the temporary nor the original in it.
//
// Opened through the root like everything else here, so "the directory holding
// the file" is the one the write actually went to.
func syncDirectory(root *os.Root, directory string) error {
	handle, err := root.Open(directory)
	if err != nil {
		return fmt.Errorf("cannot open %s to flush it: %w", directory, err)
	}

	if err := handle.Sync(); err != nil {
		// Closed here as well as below: the error being returned is the
		// interesting one, and losing the descriptor to report it would trade
		// a bad power cut for a daemon that runs out of file handles.
		return fmt.Errorf("cannot flush %s: %w", directory, errors.Join(err, handle.Close()))
	}
	if err := handle.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", directory, err)
	}
	return nil
}
