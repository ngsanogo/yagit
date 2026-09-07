//go:build windows

package repo

import (
	"errors"
	"fmt"
	"os"
)

// pinGitDir captures the git directory's identity on Windows by opening it, and
// then deliberately lets go of it again.
//
// Both halves are necessary, for opposite reasons.
//
// It must open. os.SameFile on Windows compares a volume serial number and an
// NTFS file reference, and os.Stat does not read them: it stores the path and
// resolves it lazily, inside the comparison. So a FileInfo taken from a path
// before a directory is replaced describes whatever occupies that path when the
// comparison finally happens — which is the replacement, compared against
// itself, reported as unchanged. Measured: the test covering exactly this
// replacement failed here with the check finding nothing to report. Reading the
// identity through a handle fills the numbers in at once; the standard library
// blanks the stored path precisely so the comparison will not go looking again.
//
// It must not hold. Go opens with FILE_SHARE_READ and FILE_SHARE_WRITE but not
// FILE_SHARE_DELETE, so a kept handle on .git makes Windows refuse to delete or
// even rename the repository. Measured too, as six tests failing with "The
// process cannot access the file because it is being used by another process"
// and "Access is denied" on a rename. A git client that stops you moving your
// own project directory is worse than the problem the handle would solve.
//
// And there is nothing left for a kept handle to solve. Unix needs one because
// an inode number is a slot the kernel re-issues; an NTFS file reference is a
// record number plus a sequence number that increments whenever that record is
// reused, so a recreated directory differs from the old one by construction.
// The filesystem already provides what the descriptor buys on Unix — see
// pin_unix.go, where the reasoning runs the other way.
func pinGitDir(path string) (*os.File, os.FileInfo, error) {
	handle, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open the git directory %q: %w", path, err)
	}

	identity, statErr := handle.Stat()
	closeErr := handle.Close()

	if statErr != nil {
		// Joined, not dropped: the close still has to be accounted for, and
		// wrapping a nil error would print %!w(<nil>) into the message.
		return nil, nil, errors.Join(
			fmt.Errorf("cannot inspect the git directory %q: %w", path, statErr),
			closeErr,
		)
	}
	if closeErr != nil {
		// Refusing to open the repository over a failed close looks harsh for
		// a handle whose only job is already done. But closing a handle opened
		// two lines above does not fail for any ordinary reason, and the
		// alternative is a leak this package has no logger to report.
		return nil, nil, fmt.Errorf("cannot release the git directory %q: %w", path, closeErr)
	}
	return nil, identity, nil
}
