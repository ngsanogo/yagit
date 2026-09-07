package edit_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/edit"
)

// What this package is asked to refuse is most of what it is for.
//
// A pane that opens a file and writes it back is four lines of code. The rest
// is the containment check, the line endings and the fingerprint, and each of
// those is a way to lose somebody's work quietly — which is the only kind of
// bug this package can have.

// workTree builds a directory with a `.git` in it, as a repository has, and
// returns a Location over it.
func workTree(t *testing.T) (edit.Location, string) {
	t.Helper()

	// Resolved, because Location compares resolved paths and macOS hands out
	// temporary directories under a /var that is a symlink to /private/var.
	// Without this every test here would fail on a Mac with "leaves the work
	// tree", about a path that does not.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}

	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	return edit.Location{WorkTree: root, GitDirs: []string{gitDir}}, root
}

func write(t *testing.T, root, name, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return full
}

func read(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func TestReadingAFileAndSavingItBack(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "notes.md", "one\ntwo\n")

	file, err := edit.Read(location, "notes.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if file.Text != "one\ntwo\n" {
		t.Fatalf("Text = %q", file.Text)
	}
	if file.EOL != "lf" || file.MixedEOL {
		t.Fatalf("EOL = %q, mixed = %v", file.EOL, file.MixedEOL)
	}

	saved, err := edit.Save(location, "notes.md", "one\nTWO\n", file.Fingerprint)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := read(t, filepath.Join(root, "notes.md")); got != "one\nTWO\n" {
		t.Fatalf("on disk = %q", got)
	}
	// The fingerprint has to move, or the next save of the same buffer would
	// be refused as stale against content this one wrote.
	if saved.Fingerprint == file.Fingerprint {
		t.Fatal("the fingerprint did not move after a save")
	}
}

// A save built on content the file has moved past is the one failure that
// silently destroys work, so it is a refusal rather than a last-writer-wins.
func TestSavingAgainstAStaleFingerprintIsRefused(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "notes.md", "one\n")

	file, err := edit.Read(location, "notes.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Somebody else — a checkout, another window, the user's own editor.
	write(t, root, "notes.md", "something else entirely\n")

	if _, err := edit.Save(location, "notes.md", "one\ntwo\n", file.Fingerprint); !errors.Is(err, edit.ErrFileMoved) {
		t.Fatalf("Save error = %v, want ErrFileMoved", err)
	}
	if got := read(t, filepath.Join(root, "notes.md")); got != "something else entirely\n" {
		t.Fatalf("the refused save wrote anyway: %q", got)
	}
}

func TestSavingWithNoFingerprintIsRefused(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "notes.md", "one\n")

	if _, err := edit.Save(location, "notes.md", "two\n", ""); !errors.Is(err, edit.ErrFileMoved) {
		t.Fatalf("Save error = %v, want ErrFileMoved", err)
	}
}

// A CRLF file edited through a browser must not come back with every line
// ending rewritten: that is a diff touching the whole file for a one-line fix.
func TestWindowsLineEndingsSurviveARoundTrip(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "notes.txt", "one\r\ntwo\r\n")

	file, err := edit.Read(location, "notes.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if file.EOL != "crlf" {
		t.Fatalf("EOL = %q, want crlf", file.EOL)
	}
	// Normalised on the way out, because a text box would do it anyway and
	// the fingerprint is what guards the file.
	if file.Text != "one\ntwo\n" {
		t.Fatalf("Text = %q, want the LF form", file.Text)
	}

	if _, err := edit.Save(location, "notes.txt", "one\nTWO\n", file.Fingerprint); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := read(t, filepath.Join(root, "notes.txt")); got != "one\r\nTWO\r\n" {
		t.Fatalf("on disk = %q, want CRLF back", got)
	}
}

func TestAFileWithBothEndingsSaysSo(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "mixed.txt", "one\r\ntwo\nthree\r\n")

	file, err := edit.Read(location, "mixed.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !file.MixedEOL {
		t.Fatal("MixedEOL is false for a file that uses both endings")
	}
}

func TestAnExecutableFileStaysExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the execute bit is not a Windows concept")
	}

	location, root := workTree(t)
	full := write(t, root, "run.sh", "#!/bin/sh\necho one\n")
	if err := os.Chmod(full, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	file, err := edit.Read(location, "run.sh")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !file.Executable {
		t.Fatal("Executable is false for a 0755 file")
	}

	if _, err := edit.Save(location, "run.sh", "#!/bin/sh\necho two\n", file.Fingerprint); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(full)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode after save = %v, want 0755", info.Mode().Perm())
	}
}

// The boundary. Each of these is a way out of the work tree, and a check made
// on the name rather than on where the name leads would pass most of them.
func TestPathsThatLeaveTheWorkTreeAreRefused(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "inside.txt", "in\n")

	outside := filepath.Join(filepath.Dir(root), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret\n"), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	for _, candidate := range []string{
		"../outside.txt",
		"a/../../outside.txt",
		"..",
		"",
		".",
	} {
		if _, err := edit.Read(location, candidate); err == nil {
			t.Fatalf("Read(%q) was allowed", candidate)
		}
	}

	if runtime.GOOS != "windows" {
		// The one the name check cannot catch: a legal relative path whose
		// last component is a link pointing out of the tree. A repository is
		// a directory anybody can commit a symlink into.
		if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if _, err := edit.Read(location, "link.txt"); !errors.Is(err, edit.ErrOutsideWorkTree) {
			t.Fatalf("Read through a symlink out of the tree: err = %v, want ErrOutsideWorkTree", err)
		}
	}
}

// `.git/config` names programs git runs — core.pager, core.fsmonitor. A
// browser that can write it can run anything on the next git command.
func TestPathsInsideTheGitDirectoryAreRefused(t *testing.T) {
	location, root := workTree(t)
	write(t, root, ".git/config", "[core]\n")
	write(t, root, ".git/hooks/pre-commit", "#!/bin/sh\n")

	for _, candidate := range []string{".git/config", ".git/hooks/pre-commit", ".git"} {
		if _, err := edit.Read(location, candidate); !errors.Is(err, edit.ErrInsideGitDir) &&
			!errors.Is(err, edit.ErrNotRegular) {
			t.Fatalf("Read(%q): err = %v, want ErrInsideGitDir", candidate, err)
		}
	}

	// And through a link, which is the same escape as above pointed inward.
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, ".git", "config"), filepath.Join(root, "cfg")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if _, err := edit.Read(location, "cfg"); !errors.Is(err, edit.ErrInsideGitDir) {
			t.Fatalf("Read through a link into .git: err = %v, want ErrInsideGitDir", err)
		}
	}
}

// A linked worktree's `.git` is a FILE, and it is the most dangerous file in
// the work tree: one line — `gitdir: /elsewhere` — naming where every git
// command run there reads its state from. It sits under no entry in GitDirs,
// so the containment loop never sees it, and a browser able to rewrite it
// points the daemon at a repository outside YAGIT_ROOT.
func TestTheGitPointerFileOfALinkedWorktreeIsRefused(t *testing.T) {
	// Not the workTree helper: this shape has no `.git` DIRECTORY in it at
	// all, which is exactly what makes it the case the loop misses.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}
	elsewhere := filepath.Join(root, "common.git", "worktrees", "feature")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatalf("mkdir the worktree state directory: %v", err)
	}

	checkout := filepath.Join(root, "feature")
	if err := os.MkdirAll(checkout, 0o755); err != nil {
		t.Fatalf("mkdir the checkout: %v", err)
	}
	if err := os.WriteFile(filepath.Join(checkout, ".git"),
		[]byte("gitdir: "+elsewhere+"\n"), 0o644); err != nil {
		t.Fatalf("write the .git pointer: %v", err)
	}

	location := edit.Location{
		WorkTree: checkout,
		GitDirs:  []string{filepath.Join(root, "common.git"), elsewhere},
	}

	if _, err := edit.Read(location, ".git"); !errors.Is(err, edit.ErrInsideGitDir) {
		t.Fatalf("Read(\".git\"): err = %v, want ErrInsideGitDir", err)
	}
	if _, err := edit.Save(location, ".git", "gitdir: /tmp/evil\n", "whatever"); !errors.Is(err, edit.ErrInsideGitDir) {
		t.Fatalf("Save(\".git\"): err = %v, want ErrInsideGitDir", err)
	}
}

// A submodule's `.git` is the same file one directory down, and the rule has
// to reach it: `sub/.git` names a directory under the SUPERPROJECT's git
// directory, which is in GitDirs — but the pointer itself is in the work tree.
func TestASubmodulesGitPointerIsRefusedAtAnyDepth(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "sub/.git", "gitdir: ../.git/modules/sub\n")

	if _, err := edit.Read(location, "sub/.git"); !errors.Is(err, edit.ErrInsideGitDir) {
		t.Fatalf("Read: err = %v, want ErrInsideGitDir", err)
	}

	// The ordinary file beside it is still editable: the rule is about the
	// name `.git`, not about the directory holding one.
	write(t, root, "sub/notes.md", "hello\n")
	if _, err := edit.Read(location, "sub/notes.md"); err != nil {
		t.Fatalf("Read(sub/notes.md): %v", err)
	}
}

// The write is confined too, and not only the read.
//
// The temporary file is the easiest thing in this package to point somewhere
// else: it is a name this code invents, in a directory the user controls. It
// has to land beside the file being replaced and nowhere else, and it has to
// be gone afterwards whichever way the save went.
func TestTheTemporaryFileStaysBesideTheFile(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "sub/notes.md", "one\n")

	file, err := edit.Read(location, "sub/notes.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, err := edit.Save(location, "sub/notes.md", "one\ntwo\n", file.Fingerprint); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := read(t, filepath.Join(root, "sub", "notes.md")); got != "one\ntwo\n" {
		t.Fatalf("on disk = %q", got)
	}

	// Nothing left over, in the directory that was written to or above it.
	for _, directory := range []string{root, filepath.Join(root, "sub")} {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatalf("read %s: %v", directory, err)
		}
		for _, entry := range entries {
			if strings.Contains(entry.Name(), "yagit-") {
				t.Errorf("%s left behind in %s", entry.Name(), directory)
			}
		}
	}
}

func TestABinaryFileIsRefusedRatherThanMangled(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "logo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

	if _, err := edit.Read(location, "logo.png"); !errors.Is(err, edit.ErrBinary) {
		t.Fatalf("Read: err = %v, want ErrBinary", err)
	}
}

// Bytes that are not UTF-8 cannot survive JSON: they leave as U+FFFD and would
// be saved back as U+FFFD, turning "open and close" into a rewrite.
func TestBytesThatAreNotUTF8AreRefused(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "latin1.txt", "caf\xe9\n")

	if _, err := edit.Read(location, "latin1.txt"); !errors.Is(err, edit.ErrBinary) {
		t.Fatalf("Read: err = %v, want ErrBinary", err)
	}
}

func TestAFileOverTheCapIsRefused(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "huge.txt", strings.Repeat("x", (2<<20)+1))

	if _, err := edit.Read(location, "huge.txt"); !errors.Is(err, edit.ErrTooLarge) {
		t.Fatalf("Read: err = %v, want ErrTooLarge", err)
	}
}

func TestADirectoryIsNotAFile(t *testing.T) {
	location, root := workTree(t)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := edit.Read(location, "src"); !errors.Is(err, edit.ErrNotRegular) {
		t.Fatalf("Read: err = %v, want ErrNotRegular", err)
	}
}

func TestAMissingFileIsReportedAsMissing(t *testing.T) {
	location, _ := workTree(t)

	if _, err := edit.Read(location, "nowhere.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Read: err = %v, want a not-exist error", err)
	}
}

// A failed save must leave the original whole, and must not leave its
// scratch file behind for somebody to commit.
func TestARefusedSaveLeavesNoTemporaryFile(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "notes.md", "one\n")

	if _, err := edit.Save(location, "notes.md", "two\n", "not-the-fingerprint"); err == nil {
		t.Fatal("a save against a wrong fingerprint was allowed")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), "yagit-") {
			t.Fatalf("a temporary file was left behind: %s", entry.Name())
		}
	}
}

// The case this whole package exists for: a file full of conflict markers,
// edited down to the resolution and saved.
func TestResolvingAConflictByHand(t *testing.T) {
	location, root := workTree(t)
	write(t, root, "f.txt", "one\n<<<<<<< HEAD\nMAIN\n=======\nSIDE\n>>>>>>> side\nthree\n")

	file, err := edit.Read(location, "f.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if _, err := edit.Save(location, "f.txt", "one\nMAIN\nSIDE\nthree\n", file.Fingerprint); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := read(t, filepath.Join(root, "f.txt")); got != "one\nMAIN\nSIDE\nthree\n" {
		t.Fatalf("on disk = %q", got)
	}
}

func TestALocationWithNoWorkTreeRefusesEverything(t *testing.T) {
	if _, err := edit.Read(edit.Location{}, "notes.md"); err == nil {
		t.Fatal("a Location with no work tree read a file")
	}
}
