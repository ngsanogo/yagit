// Package protect locks a path down so only the process owner can use it.
//
// On Unix that is a mode bit. On Windows access is an ACL, so the same call
// writes an explicit owner-only DACL instead. One function, both platforms,
// and the places that mint the session token call it rather than each
// inventing their own half of the story.
package protect

import (
	"fmt"
	"os"
)

// OwnerOnly makes path readable and writable by the current user alone.
//
// For a directory that means listing and creating inside it; for a file,
// reading and writing it. Call it after every create that holds a secret: the
// session token, the directory that holds it, a released binary's -token-file.
func OwnerOnly(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("locking down %s: %w", path, err)
	}
	if err := ownerOnly(path, info.IsDir()); err != nil {
		return fmt.Errorf("locking down %s: %w", path, err)
	}
	return nil
}

// Check reports whether path is locked down the way OwnerOnly leaves it.
//
// The callers that write the session token assert this after writing, so a
// broken lockdown fails the suite on every platform the same way.
func Check(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("checking lockdown of %s: %w", path, err)
	}
	if err := checkOwnerOnly(path, info.IsDir()); err != nil {
		return fmt.Errorf("checking lockdown of %s: %w", path, err)
	}
	return nil
}
