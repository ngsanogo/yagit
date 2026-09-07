package protect_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ngsanogo/yagit/internal/protect"
)

func TestOwnerOnlyLocksAFileDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("token\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := protect.OwnerOnly(path); err != nil {
		t.Fatalf("OwnerOnly: %v", err)
	}
	if err := protect.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestOwnerOnlyLocksADirectoryDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := protect.OwnerOnly(path); err != nil {
		t.Fatalf("OwnerOnly: %v", err)
	}
	if err := protect.Check(path); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestOwnerOnlyNamesAMissingPath(t *testing.T) {
	err := protect.OwnerOnly(filepath.Join(t.TempDir(), "nowhere"))
	if err == nil {
		t.Fatal("OwnerOnly on a missing path succeeded")
	}
}

func TestCheckRejectsAWorldReadableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Widening an ACL just to refuse it is a different test; the Windows
		// path of Check is covered by OwnerOnlyLocksAFileDown after a
		// lockdown that started from an inherited ACL.
		t.Skip("world-readable modes are a Unix claim")
	}
	path := filepath.Join(t.TempDir(), "open")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := protect.Check(path); err == nil {
		t.Fatal("Check accepted a 0644 file")
	}
}
