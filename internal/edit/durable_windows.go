//go:build windows

package edit

import "os"

// syncDirectory does nothing on Windows, deliberately.
//
// There is no directory flush to ask for: FlushFileBuffers refuses a directory
// handle, so the call the other file makes would fail on every save rather
// than making anything durable. NTFS journals the rename's metadata itself,
// which is the guarantee the flush is bought for elsewhere.
func syncDirectory(*os.Root, string) error { return nil }
