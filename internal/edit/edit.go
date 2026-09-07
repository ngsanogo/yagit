// Package edit reads and writes files inside a repository's work tree.
//
// Nothing here runs git, and that is the point rather than an omission: fixing
// a typo, deleting a conflict marker and saving is what a text editor does,
// and git never sees the file until somebody stages it. Driving `git` for it
// would mean inventing a command git does not have.
//
// It is therefore the one place in the daemon that opens the user's own files,
// which is why both entry points begin by opening the work tree as an os.Root
// and never touch a path outside it again. Depends on nothing.
package edit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Refusals the caller tells apart to pick an HTTP status code. Compared with
// errors.Is, never by their text.
var (
	// ErrOutsideWorkTree: the path resolves somewhere the repository does not
	// reach. The check is on the RESOLVED path, so a symlink planted in the
	// work tree and pointing at /etc is refused here rather than followed.
	ErrOutsideWorkTree = errors.New("this path leaves the work tree")

	// ErrInsideGitDir: the path is part of the repository's own machinery.
	//
	// The one refusal here that is a security boundary rather than a
	// courtesy. `.git/config` sets `core.fsmonitor` and `core.pager`, both of
	// which name a program git runs; a browser able to write that file is a
	// browser able to run anything, on the next git command yagit issues.
	// `.git` itself carries the same weight when it is a file rather than a
	// directory — see resolve, which is where both halves of the rule are.
	ErrInsideGitDir = errors.New("this path is inside the git directory")

	// ErrNotRegular: a directory, a device, a socket. There is no text.
	ErrNotRegular = errors.New("this path is not a regular file")

	// ErrBinary: the bytes are not text, so a text box would not round-trip
	// them. Showing them anyway is how an editor corrupts a PNG.
	ErrBinary = errors.New("this file is not text")

	// ErrTooLarge: past the cap. See MaxFileBytes.
	ErrTooLarge = errors.New("this file is too large to edit")

	// ErrFileMoved: the file changed between being read and being saved.
	//
	// The same guard as a line selection's diff fingerprint, for the same
	// reason: a save is only correct for the content it was started from. A
	// checkout, a rebase or the user's own editor can move the file
	// underneath, and writing anyway would silently throw away whatever
	// arrived in between.
	ErrFileMoved = errors.New("the file changed since it was read")
)

// MaxFileBytes caps what may be opened for editing.
//
// Not the ten megabytes a diff is allowed. That cap protects the daemon's
// memory; this one protects a text box, and a two-megabyte file is already
// tens of thousands of lines rendered into one DOM node. Past it the answer is
// a real editor, which is a better answer than a tab that stops responding —
// and this is a quick-fix pane, not an IDE.
//
// Exported because the route that saves has to size its request body against
// it. Two independent numbers is what produced a pane that opened a file it
// could never write back.
const MaxFileBytes = 2 << 20

// Location is where files may be read and written.
//
// A type rather than two string parameters because the two are checked
// together and neither is optional: a work tree with no forbidden directories
// would happily hand out `.git/config`.
type Location struct {
	// WorkTree is the repository's root, already resolved. Every path is
	// relative to it, and every resolved path must stay under it.
	WorkTree string

	// GitDirs is every directory git keeps its own state in — repo.GitDirs.
	// Nothing under them is editable, whether or not it lies inside the work
	// tree.
	GitDirs []string
}

// EOL is the line ending a file uses on disk.
//
// It has to be recorded, because it cannot survive the trip: a browser's text
// box normalises every newline it is given to a bare LF, so a CRLF file
// round-tripped through one comes back with every line ending changed. That is
// a diff touching every line of a file the user edited one line of. Reading the
// ending here and writing it back is what keeps the save to the lines that
// were actually typed.
type EOL string

const (
	EOLUnix    EOL = "lf"
	EOLWindows EOL = "crlf"
)

// File is a work-tree file as the interface edits it.
type File struct {
	Path string `json:"path"`

	// Text is the content with every line ending normalised to LF. Save puts
	// EOL back.
	Text string `json:"text"`

	// Fingerprint identifies the exact bytes that were on disk — before
	// normalisation, so it describes the file rather than this rendering of
	// it. Sent back with a save; see ErrFileMoved.
	Fingerprint string `json:"fingerprint"`

	EOL EOL `json:"eol"`

	// MixedEOL says the file used both endings. Saving then writes EOL
	// throughout, which changes lines nobody typed in — so the interface says
	// so before the save rather than after it.
	MixedEOL bool `json:"mixed_eol"`

	// Executable is the mode bit, kept across the save. An editor that
	// silently drops it turns a working script into a file with a permission
	// error nobody connects to having fixed a typo.
	Executable bool `json:"executable"`

	Bytes int `json:"bytes"`
}

// Read opens a work-tree file for editing.
func Read(location Location, relPath string) (File, error) {
	root, name, err := location.open(relPath)
	if err != nil {
		return File{}, err
	}
	defer release(root)

	return readThrough(root, name, relPath)
}

// readThrough is the read itself, once the work tree is open and the path is
// allowed.
//
// Its own function so that Save can do both halves through ONE root: the file
// whose fingerprint is checked and the file then written have to be the same
// file, and opening the work tree twice is two chances for them not to be.
func readThrough(root *os.Root, name, relPath string) (File, error) {
	info, err := root.Stat(name)
	if err != nil {
		return File{}, fmt.Errorf("cannot read %s: %w", relPath, err)
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s: %w", relPath, ErrNotRegular)
	}
	// Checked before the read rather than after it: the cap exists so that a
	// gigabyte file is never held in memory, and reading it to measure it
	// would be doing exactly what the cap forbids.
	if info.Size() > MaxFileBytes {
		return File{}, fmt.Errorf("%s is %d bytes, over the %d this pane will open: %w",
			relPath, info.Size(), MaxFileBytes, ErrTooLarge)
	}

	raw, err := root.ReadFile(name)
	if err != nil {
		return File{}, fmt.Errorf("cannot read %s: %w", relPath, err)
	}
	if !isText(raw) {
		return File{}, fmt.Errorf("%s: %w", relPath, ErrBinary)
	}

	text, ending, mixed := normalise(string(raw))

	return File{
		Path:        relPath,
		Text:        text,
		Fingerprint: fingerprint(raw),
		EOL:         ending,
		MixedEOL:    mixed,
		Executable:  info.Mode().Perm()&0o100 != 0,
		Bytes:       len(raw),
	}, nil
}

// Save writes a file back, and refuses if it moved since it was read.
//
// base is the Fingerprint from the Read the edit started at. It is required:
// a save with nothing to compare against is a save that cannot tell an edit
// from an overwrite, and the one moment this pane is most used — the middle of
// a merge — is the one where a checkout is most likely to have run underneath.
//
// Saving does not stage. That separation is deliberate and it is the whole
// shape of the working-directory panel: what is on disk and what is in the
// index are two different things, and an editor that quietly staged would be
// deciding for the user which of the two they meant.
func Save(location Location, relPath, text, base string) (File, error) {
	if base == "" {
		return File{}, fmt.Errorf(
			"%s: no fingerprint was sent, so there is nothing to check the save against: %w",
			relPath, ErrFileMoved)
	}

	root, name, err := location.open(relPath)
	if err != nil {
		return File{}, err
	}
	defer release(root)

	current, err := readThrough(root, name, relPath)
	if err != nil {
		return File{}, err
	}
	if current.Fingerprint != base {
		return File{}, fmt.Errorf(
			"%s: read it again before saving, or the changes made in between are lost: %w",
			relPath, ErrFileMoved)
	}

	// The ending the file already had, put back. current is the authority on
	// it rather than the request: the browser cannot report an ending it
	// normalised away before this code ever saw the text.
	body := []byte(restore(text, current.EOL))

	info, err := root.Stat(name)
	if err != nil {
		return File{}, fmt.Errorf("cannot inspect %s: %w", relPath, err)
	}
	if len(body) > MaxFileBytes {
		return File{}, fmt.Errorf("%s would be %d bytes, over the %d this pane will hold: %w",
			relPath, len(body), MaxFileBytes, ErrTooLarge)
	}

	if err := writeAtomically(root, name, body, info.Mode().Perm()); err != nil {
		return File{}, err
	}

	saved := current
	saved.Text, _, saved.MixedEOL = normalise(string(body))
	saved.Fingerprint = fingerprint(body)
	saved.Bytes = len(body)
	return saved, nil
}

// open opens the work tree, and says what to call the file inside it.
//
// Two boundaries, and they are deliberately not the same one.
//
// os.Root is the outer, and it is the one that holds. It resolves every
// component itself, inside the kernel, and refuses one that leaves the
// directory — so a symlink planted in the repository reaches nothing outside
// the work tree, and neither does one swapped in AFTER a check and before the
// open, which is a window no resolve-then-open can close. Every read and every
// write below goes through it; this package holds no absolute path at all.
//
// checkPath is the inner: what this package refuses to open even though the
// work tree does contain it. `.git/config` is inside the work tree.
func (l Location) open(relPath string) (*os.Root, string, error) {
	name, err := l.checkPath(relPath)
	if err != nil {
		return nil, "", err
	}

	root, err := os.OpenRoot(l.WorkTree)
	if err != nil {
		return nil, "", fmt.Errorf("cannot open the work tree %s: %w", l.WorkTree, err)
	}
	return root, name, nil
}

// release gives the work-tree handle back.
//
// The one error this package drops, and deliberately: os.Root.Close returns a
// descriptor to the kernel, nothing a caller could do differs between the two
// outcomes, and returning it from a deferred call would replace the real
// reason a read failed with noise from the cleanup after it. What makes a save
// durable is the Sync in writeAtomically, never this.
func release(root *os.Root) {
	//nolint:errcheck // see the note above: there is nothing to act on.
	root.Close()
}

// checkPath says whether a repository-relative path may be opened at all, and
// returns the name to open it by.
//
// Symlinks are resolved BEFORE the comparisons, which is the half that
// matters. A repository is a directory anybody can commit into, so `notes.md`
// can perfectly well be a link to `.git/config` — and a check made on the name
// rather than on where the name leads would pass it.
func (l Location) checkPath(relPath string) (string, error) {
	if l.WorkTree == "" {
		return "", errors.New("no work tree given")
	}

	cleaned := filepath.Clean(filepath.FromSlash(relPath))
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("%q names no file", relPath)
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q: %w", relPath, ErrOutsideWorkTree)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(l.WorkTree, cleaned))
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", relPath, err)
	}

	if !within(l.WorkTree, resolved) {
		return "", fmt.Errorf("%q leads to %q: %w", relPath, resolved, ErrOutsideWorkTree)
	}
	// Everything INSIDE a git directory, which for an ordinary repository is
	// everything `.git` contains.
	for _, gitDir := range l.GitDirs {
		if within(gitDir, resolved) {
			return "", fmt.Errorf("%q leads to %q: %w", relPath, resolved, ErrInsideGitDir)
		}
	}

	// And `.git` ITSELF, at any depth, because it is not always a directory.
	// A linked worktree and a submodule each get a `.git` FILE holding one
	// line — `gitdir: /elsewhere` — and that line is where every git command
	// run in this work tree reads its state from. The file sits in the work
	// tree, under no entry in GitDirs, so the loop above never sees it; a
	// browser able to rewrite it points the daemon at a repository outside
	// YAGIT_ROOT, on the next command yagit issues.
	//
	// Nothing editable is lost to the breadth of this: git refuses to track a
	// path with a `.git` component at all, so there is no file behind the
	// rule. The comparison ignores case for the reason git's own protectNTFS
	// does — `.GIT` reaches the same file on a case-insensitive filesystem,
	// and the daemon cannot know which kind it is standing on.
	relative, err := filepath.Rel(l.WorkTree, resolved)
	if err != nil {
		return "", fmt.Errorf("cannot place %s inside the work tree: %w", relPath, err)
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if strings.EqualFold(component, ".git") {
			return "", fmt.Errorf("%q leads to %q: %w", relPath, resolved, ErrInsideGitDir)
		}
	}

	// The name, not where it led. os.Root walks the components again for
	// itself, and handing it a path resolved a moment ago would be handing it
	// an answer that was true a moment ago.
	return cleaned, nil
}

// within reports whether path is root or sits under it. Both must already be
// cleaned and resolved.
//
// The separator appended to the root is what keeps "/srv/data" from counting
// as containing "/srv/database" — the same reasoning, and the same trap, as
// repo.isWithin. Two copies rather than an import, because this package
// depends on nothing and a shared helper would be the whole reason to break
// that.
func within(root, path string) bool {
	if path == root {
		return true
	}
	separator := string(filepath.Separator)
	if !strings.HasSuffix(root, separator) {
		root += separator
	}
	return strings.HasPrefix(path, root)
}

// isText says the bytes can survive a round trip through a text box.
//
// Two questions, and both have to be asked. A NUL byte is git's own test for a
// binary file and catches most of them; valid UTF-8 is what the answer has to
// be, because it leaves as JSON and comes back as a JavaScript string —
// latin-1 bytes would be replaced by U+FFFD on the way out and saved back as
// that, turning "open and close without touching anything" into a file full of
// question marks.
func isText(raw []byte) bool {
	// git reads the first 8000 bytes and so does this. A file that is text
	// for its first eight kilobytes and binary afterwards would still fail
	// the UTF-8 check below, which covers the whole of it.
	head := raw
	if len(head) > 8000 {
		head = head[:8000]
	}
	for _, b := range head {
		if b == 0 {
			return false
		}
	}
	return utf8.Valid(raw)
}

// normalise turns every line ending into LF, and says which endings it found.
func normalise(text string) (normalised string, ending EOL, mixed bool) {
	windows := strings.Count(text, "\r\n")
	if windows == 0 {
		return text, EOLUnix, false
	}

	// Every LF that is not part of a CRLF. A file with both is a file no save
	// can leave alone, which is what MixedEOL exists to announce.
	unix := strings.Count(text, "\n") - windows
	return strings.ReplaceAll(text, "\r\n", "\n"), EOLWindows, unix > 0
}

// restore puts an ending back on text that arrived normalised.
func restore(text string, ending EOL) string {
	if ending != EOLWindows {
		return text
	}
	// The text arrives from a browser, which normalises to LF, but a client
	// that is not one may send CRLF already. Collapsing first makes this
	// function total rather than dependent on who is calling it — without it,
	// a CRLF file saved by such a client grows a \r per line per save.
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
}

func fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// writeAtomically replaces a file's content in one step.
//
// A plain write truncates first, so an interrupted one — the daemon killed,
// the disk full — leaves a half file where the user's source used to be, with
// no copy of the rest anywhere. Writing beside it and renaming over the top
// means the path always names either the old content or the new one.
//
// The rename is within one directory, so it stays on one filesystem, which is
// what makes it atomic. The mode is copied from the file being replaced: a
// rename brings the temporary file's permissions with it, and an executable
// script saved through a default 0600 would stop being one.
//
// Every step goes through the root, so the whole of it is confined to the work
// tree — including the temporary file, which is a file this package creates in
// a directory the user controls and would otherwise be the easiest thing here
// to point somewhere else.
func writeAtomically(root *os.Root, name string, body []byte, mode fs.FileMode) error {
	directory := filepath.Dir(name)

	temporary, temporaryName, err := createTemporary(root, directory, filepath.Base(name))
	if err != nil {
		return fmt.Errorf("cannot open a temporary file beside %s: %w", name, err)
	}

	// Every failure below removes it. A crash-safe write that litters the
	// user's repository with half-written dotfiles is not much of an
	// improvement, and one of them named `.main.go.yagit-1234` is exactly
	// the sort of thing that ends up committed.
	abandon := func(cause error) error {
		if err := root.Remove(temporaryName); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return errors.Join(cause,
				fmt.Errorf("and %s could not be removed: %w", temporaryName, err))
		}
		return cause
	}

	if _, err := temporary.Write(body); err != nil {
		return abandon(errors.Join(
			fmt.Errorf("cannot write %s: %w", temporaryName, err), temporary.Close()))
	}
	// Before the rename, not after. A rename that reaches the disk ahead of
	// the bytes it renames — which is a thing filesystems do — leaves the
	// path naming a file of zeroes after a power cut.
	if err := temporary.Sync(); err != nil {
		return abandon(errors.Join(
			fmt.Errorf("cannot flush %s: %w", temporaryName, err), temporary.Close()))
	}
	if err := temporary.Close(); err != nil {
		return abandon(fmt.Errorf("cannot close %s: %w", temporaryName, err))
	}
	if err := root.Chmod(temporaryName, mode); err != nil {
		return abandon(fmt.Errorf("cannot set the mode of %s: %w", temporaryName, err))
	}
	if err := root.Rename(temporaryName, name); err != nil {
		return abandon(fmt.Errorf("cannot put %s in place: %w", name, err))
	}
	// The rename itself has to reach the disk, and on ext4 and XFS it has not
	// when Rename returns: the directory entry is still only in memory.
	// Without this the promise above holds for a killed daemon and not for a
	// power cut — the directory can come back with neither name in it, the
	// temporary gone and the original entry not restored, which loses the file
	// the write was replacing. Not routed through abandon: the temporary is
	// already gone by here, and there is nothing left to clean up.
	return syncDirectory(root, directory)
}

// createTemporary makes a file beside another one that nothing else holds.
//
// os.CreateTemp cannot do this: it takes a path, and a path is exactly what
// this package no longer has to give. The loop is os.CreateTemp's own — a
// name, an exclusive create, another name if something already had that one —
// and O_EXCL is what makes guessing safe rather than the guess being unguessable.
// The mode is 0600 until the rename: for the moment the content exists under a
// name nobody asked for, it is readable only by the daemon's own user.
func createTemporary(root *os.Root, directory, base string) (*os.File, string, error) {
	const attempts = 10_000

	for range attempts {
		name := filepath.Join(directory,
			"."+base+".yagit-"+strconv.FormatUint(rand.Uint64(), 36))

		handle, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		return handle, name, nil
	}

	return nil, "", fmt.Errorf(
		"no free temporary name beside %s after %d tries", base, attempts)
}
