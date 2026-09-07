package git_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// These tests exist because the diff parser reads a format git decides, and
// git decides it from the user's configuration and from what the file is
// called. A parser tested only on hand-written diffs is a parser tested on
// output git never produces.

// TestDiffOfANonASCIIName covers the case core.quotePath makes ordinary
// everywhere but an English-speaking repository: git C-quotes the name on
// every header line, and a parser that does not read the quoting back answers
// "unreadable file header" for the file, for its diff on all three sides, and
// for every commit that touches it.
func TestDiffOfANonASCIIName(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	const name = "café.txt"
	writeFile(t, dir, name, "un\ndeux\n")

	untracked, err := runner.Diff(ctx, dir, git.DiffRequest{Path: name, Side: git.DiffUntracked})
	if err != nil {
		t.Fatalf("untracked diff of %s: %v", name, err)
	}
	if untracked.Path != name {
		t.Errorf("untracked Path = %q, expected %q", untracked.Path, name)
	}
	if untracked.OldPath != "" {
		t.Errorf("OldPath = %q; a new file was renamed from nothing", untracked.OldPath)
	}

	runGit(t, runner, dir, "add", "--", name)
	staged, err := runner.Diff(ctx, dir, git.DiffRequest{Path: name, Side: git.DiffStaged})
	if err != nil {
		t.Fatalf("staged diff of %s: %v", name, err)
	}
	if staged.Path != name {
		t.Errorf("staged Path = %q, expected %q", staged.Path, name)
	}

	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "accented")...)
	writeFile(t, dir, name, "un\nDEUX\n")
	unstaged, err := runner.Diff(ctx, dir, git.DiffRequest{Path: name, Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("unstaged diff of %s: %v", name, err)
	}
	if unstaged.Path != name || len(unstaged.Hunks) != 1 {
		t.Fatalf("unstaged diff of %s came back as %+v", name, unstaged)
	}

	// The commit details pane reads the same parser through a different
	// command, and a quoted name breaks it there too — for the whole commit,
	// not only for that one file.
	detail, err := runner.Show(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("Show of the commit that added %s: %v", name, err)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != name {
		t.Errorf("Show listed %+v, expected the one accented path", detail.Files)
	}
}

// TestDiffKeepsTheNameGitDisambiguatesWithATab: git terminates the name on a
// `---`/`+++` line with a TAB when it holds a space, so that a traditional
// patch reader can find where the name ends. Kept, the payload names a path
// the status list never mentioned.
func TestDiffKeepsTheNameGitDisambiguatesWithATab(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	const name = "my file.txt"
	writeFile(t, dir, name, "alpha\n")
	runGit(t, runner, dir, "add", "--", name)
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "spaced")...)

	writeFile(t, dir, name, "beta\n")
	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: name, Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if file.Path != name {
		t.Errorf("Path = %q, expected %q with no trailing tab", file.Path, name)
	}

	// A deletion is where a kept tab becomes more than cosmetic: the `+++`
	// line is /dev/null, so nothing overwrites the tabbed name and the file
	// comes back looking like a rename of itself, which the patch builder
	// then refuses with a sentence about a name nobody typed.
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		t.Fatalf("remove %s: %v", name, err)
	}
	deleted, err := runner.Diff(ctx, dir, git.DiffRequest{Path: name, Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff of the deletion: %v", err)
	}
	if deleted.OldPath != "" {
		t.Errorf("OldPath = %q; a deleted file was not renamed", deleted.OldPath)
	}
	if !deleted.Removed {
		t.Error("the deletion was not reported as one")
	}
}

// TestDiffPinsThePathPrefixesAgainstUserConfiguration: diff.mnemonicPrefix and
// diff.noprefix are ordinary things to set in a terminal, and both change the
// `a/`…`b/` this package parses back out. Unpinned, one line in a user's
// ~/.gitconfig makes every diff in the interface unreadable.
func TestDiffPinsThePathPrefixesAgainstUserConfiguration(t *testing.T) {
	for _, setting := range []string{"diff.mnemonicPrefix", "diff.noprefix"} {
		t.Run(setting, func(t *testing.T) {
			dir, runner := workingDirectory(t)
			ctx := context.Background()
			runGit(t, runner, dir, "config", setting, "true")

			file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "a.txt", Side: git.DiffUnstaged})
			if err != nil {
				t.Fatalf("Diff with %s set: %v", setting, err)
			}
			if file.Path != "a.txt" {
				t.Errorf("Path = %q, expected a.txt", file.Path)
			}
			if len(file.Hunks) == 0 {
				t.Error("no hunk came back for a file that differs")
			}
		})
	}
}

// TestDiffOfAStagedRenameKeepsBothNames: rename detection runs AFTER the
// pathspec is applied, so a diff limited to the new name alone can no longer
// pair the two halves and git answers with `new file mode` and every line
// added. The interface then shows a renamed file as a new one, and unstaging
// one of those lines writes an index holding neither HEAD's content nor the
// work tree's.
func TestDiffOfAStagedRenameKeepsBothNames(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "checkout", "--", "a.txt")
	runGit(t, runner, dir, "mv", "a.txt", "b.txt")
	writeFile(t, dir, "b.txt", "one\nX\nthree\n")
	runGit(t, runner, dir, "add", "--all")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{
		Path: "b.txt", OldPath: "a.txt", Side: git.DiffStaged,
	})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if file.Path != "b.txt" || file.OldPath != "a.txt" {
		t.Fatalf("Path = %q, OldPath = %q; expected the rename to be paired", file.Path, file.OldPath)
	}
	if file.Added {
		t.Error("a rename was reported as a new file")
	}

	// With the rename visible, the patch builder refuses to take it line by
	// line instead of writing one that drops HEAD's content.
	if _, err := git.FormatPatch(file, map[int]bool{0: true}, git.PatchReverse); !errors.Is(err, git.ErrNotLineAddressable) {
		t.Errorf("FormatPatch on a rename returned %v, expected ErrNotLineAddressable", err)
	}
}

// TestUntrackedDiffOfAPathGitCannotOpenFails: `git diff --no-index` exits 1
// both for "the files differ" and for a path it could not read, writing the
// reason to stderr and nothing to standard output. Taking the second for an
// empty diff draws "no change here" over git's own explanation.
func TestUntrackedDiffOfAPathGitCannotOpenFails(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	_, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "gone.txt", Side: git.DiffUntracked})
	if err == nil {
		t.Fatal("expected a failure for a path git cannot access")
	}

	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error is %T, expected git's own with its command and stderr", err)
	}
	if !strings.Contains(gitError.Stderr, "Could not access") {
		t.Errorf("stderr = %q, expected git's own sentence", gitError.Stderr)
	}
}

// TestUntrackedDiffOfAnEmptyFileIsNotAFailure guards the check above from
// refusing the one legitimate near-empty answer: git still writes a header for
// a file with no content in it.
func TestUntrackedDiffOfAnEmptyFileIsNotAFailure(t *testing.T) {
	dir, runner := workingDirectory(t)
	writeFile(t, dir, "empty.txt", "")

	file, err := runner.Diff(context.Background(), dir, git.DiffRequest{
		Path: "empty.txt", Side: git.DiffUntracked,
	})
	if err != nil {
		t.Fatalf("Diff of an empty untracked file: %v", err)
	}
	if !file.Added {
		t.Error("an empty new file is still a new file")
	}
}

// TestShowOfAMergeIgnoresLogDiffMerges: `-m` is `--diff-merges=on`, which
// takes its shape from log.diffMerges. Set to `combined` git answers with
// `@@@` hunk headers, which are not a unified diff, and no hand-resolved merge
// commit can be opened at all.
func TestShowOfAMergeIgnoresLogDiffMerges(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "--", "a.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "first")...)

	runGit(t, runner, dir, "checkout", "-b", "side")
	writeFile(t, dir, "a.txt", "one\nSIDE\nthree\n")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-am", "side")...)

	runGit(t, runner, dir, "checkout", "main")
	writeFile(t, dir, "a.txt", "one\nMAIN\nthree\n")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-am", "main")...)

	// The merge conflicts on purpose: a merge resolved by hand is the only
	// one whose combined diff has any content, and so the only one the wrong
	// format breaks.
	if _, err := runner.Run(context.Background(), dir,
		append(append([]string{}, identity...), "merge", "side")...); err == nil {
		t.Fatal("expected the merge to conflict")
	}
	writeFile(t, dir, "a.txt", "one\nRESOLVED\nthree\n")
	runGit(t, runner, dir, "add", "--", "a.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "merge")...)

	runGit(t, runner, dir, "config", "log.diffMerges", "combined")

	detail, err := runner.Show(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatalf("Show of a merge with log.diffMerges=combined: %v", err)
	}
	if !detail.AgainstFirstParent {
		t.Error("a merge's diff must say which comparison it is")
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "a.txt" {
		t.Fatalf("Show listed %+v, expected a.txt against the first parent", detail.Files)
	}
}

// TestADiffTooLargeToHoldIsRefusedRatherThanHeld: a generated file — a
// database dump, a lockfile, a minified bundle — is ordinary text to git, so a
// rewritten one answers with a patch the size of two copies of it. Every byte
// is then held three times over: git's output, the string it is parsed from,
// and one struct per body line. The selected file's diff is re-read on a timer
// while its row stays selected, so an unbounded read is a daemon that grows
// until the kernel kills it, taking every open repository's state with it.
func TestADiffTooLargeToHoldIsRefusedRatherThanHeld(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	// Comfortably past the cap once doubled into a diff, and cheap to build.
	var lines strings.Builder
	for lines.Len() < 6<<20 {
		lines.WriteString("a line of a generated file that nobody will ever read\n")
	}
	writeFile(t, dir, "dump.sql", lines.String())
	runGit(t, runner, dir, "add", "--", "dump.sql")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "generated")...)

	writeFile(t, dir, "dump.sql", strings.ReplaceAll(lines.String(), "a line", "a LINE"))

	_, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "dump.sql", Side: git.DiffUnstaged})
	if !errors.Is(err, git.ErrDiffTooLarge) {
		t.Fatalf("Diff returned %v, expected ErrDiffTooLarge", err)
	}

	// Refused rather than truncated: half a diff is a patch that applies to
	// something other than what it describes, and the error has to say which
	// command produced it like every other git failure here.
	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("error is %T, expected git's own with its command on it", err)
	}
	if !strings.Contains(gitError.Error(), "dump.sql") {
		t.Errorf("the error does not name the command that ran: %v", gitError)
	}
}
