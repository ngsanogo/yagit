//go:build unix

package protect

import (
	"fmt"
	"os"
)

// ownerOnly is chmod. The mode is what Unix actually enforces; Windows has
// its own file.
func ownerOnly(path string, directory bool) error {
	return os.Chmod(path, ownerMode(directory))
}

func checkOwnerOnly(path string, directory bool) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	want := ownerMode(directory)
	if got := info.Mode().Perm(); got != want {
		return fmt.Errorf("mode is %04o, want %04o", got, want)
	}
	return nil
}

func ownerMode(directory bool) os.FileMode {
	if directory {
		return 0o700
	}
	return 0o600
}
