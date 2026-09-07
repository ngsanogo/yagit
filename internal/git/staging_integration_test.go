package git_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// These tests run the real git binary against the real operations. What no
// unit test can cover is here: that a patch this package builds is one `git
// apply` accepts, and that applying it puts in the index exactly what the
// user chose and nothing else.
//
// That last part is the whole point. A patch with a miscounted range does not
// crash — it applies, and stages a file the user never asked for.

// workingDirectory builds a repository with one committed file, then changes
// it. Every test below starts from this state.
//
//	committed:  one  two   three
//	on disk:    one  TWO   three  four
func workingDirectory(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "--", "a.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"commit", "-m", "first")...)

	writeFile(t, dir, "a.txt", "one\nTWO\nthree\nfour\n")

	return dir, runner
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

// indexContent is what a commit would record for a path right now.
func indexContent(t *testing.T, runner *git.Runner, dir, path string) string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "show", ":"+path)
	if err != nil {
		t.Fatalf("git show :%s: %v", path, err)
	}
	return string(output)
}

func TestStatusAgainstARealWorkingDirectory(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	writeFile(t, dir, "untracked.txt", "new\n")
	writeFile(t, dir, "sub/deep.txt", "deep\n")

	status, err := runner.Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if status.Branch != "main" {
		t.Errorf("Branch = %q, expected main", status.Branch)
	}
	if status.Unborn || status.Detached {
		t.Error("a repository with one commit on a branch is neither unborn nor detached")
	}
	if status.HeadSHA == "" {
		t.Error("HeadSHA was not read")
	}

	byPath := make(map[string]git.FileStatus, len(status.Files))
	for _, file := range status.Files {
		byPath[file.Path] = file
	}

	edited, found := byPath["a.txt"]
	if !found {
		t.Fatalf("a.txt is missing from %v", status.Files)
	}
	if edited.Staged() || !edited.Unstaged() {
		t.Errorf("a.txt = %+v, expected unstaged only", edited)
	}

	// --untracked-files=all is what puts the file here rather than the
	// directory "sub/". Without it there is nothing to stage but the whole
	// directory, and nothing to show inside it.
	if _, found := byPath["sub/deep.txt"]; !found {
		t.Errorf("sub/deep.txt is missing; git collapsed the directory: %v", status.Files)
	}
	if _, found := byPath["sub/"]; found {
		t.Error("the untracked directory was reported instead of the file inside it")
	}
}

func TestStatusOnAnUnbornBranch(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "f.txt", "hello\n")

	status, err := runner.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Unborn {
		t.Error("a repository with no commit has an unborn branch")
	}
	if status.Branch != "main" {
		t.Errorf("Branch = %q; the name exists before the first commit", status.Branch)
	}
}

func TestDiffOfAnUntrackedFileIsEveryLineAdded(t *testing.T) {
	dir, runner := workingDirectory(t)
	writeFile(t, dir, "untracked.txt", "alpha\nbeta\n")

	// `git diff --no-index` exits 1 when the files differ, which is the case
	// this exists for. A plain run would report every untracked file as a
	// failure.
	file, err := runner.Diff(context.Background(), dir, git.DiffRequest{Path: "untracked.txt", Side: git.DiffUntracked})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !file.Added {
		t.Error("an untracked file has no old side")
	}
	if len(file.Hunks) != 1 || len(file.Hunks[0].Lines) != 2 {
		t.Fatalf("expected one hunk of two lines, got %+v", file.Hunks)
	}
	for _, line := range file.Hunks[0].Lines {
		if line.Kind != git.LineAdded {
			t.Errorf("line %q is %q, expected added", line.Text, line.Kind)
		}
	}
	if file.ID == "" {
		t.Error("the diff must carry the fingerprint a selection is checked against")
	}
}

func TestDiffOfAnUnchangedPathIsEmptyRatherThanAnError(t *testing.T) {
	dir, runner := workingDirectory(t)

	// a.txt has no staged change: git answers nothing, and nothing is a
	// perfectly good answer.
	file, err := runner.Diff(context.Background(), dir, git.DiffRequest{Path: "a.txt", Side: git.DiffStaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !file.Empty() {
		t.Errorf("expected an empty diff, got %+v", file)
	}
	if file.Path != "a.txt" {
		t.Errorf("Path = %q; an empty diff still names what was asked for", file.Path)
	}
}

func TestStagingSomeLinesPutsExactlyThoseLinesInTheIndex(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "a.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	// Take the "two" → "TWO" substitution and leave "four" behind.
	if err := runner.StageLines(ctx, dir, file, map[int]bool{1: true, 2: true}); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	if got := indexContent(t, runner, dir, "a.txt"); got != "one\nTWO\nthree\n" {
		t.Errorf("index holds %q, expected the substitution without the added line", got)
	}

	// --cached changes the index alone. A staging operation that touched the
	// file on disk would silently undo whatever the user was still editing.
	if got := readFile(t, dir, "a.txt"); got != "one\nTWO\nthree\nfour\n" {
		t.Errorf("the work tree was modified: %q", got)
	}
}

func TestUnstagingSomeLinesTakesExactlyThoseBackOut(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "add", "--", "a.txt")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "a.txt", Side: git.DiffStaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	// The staged diff is the same shape: context, -two, +TWO, context, +four.
	// Take "four" back out and leave the substitution staged.
	if err := runner.UnstageLines(ctx, dir, file, map[int]bool{4: true}); err != nil {
		t.Fatalf("UnstageLines: %v", err)
	}

	if got := indexContent(t, runner, dir, "a.txt"); got != "one\nTWO\nthree\n" {
		t.Errorf("index holds %q, expected the added line gone and the substitution kept", got)
	}
	if got := readFile(t, dir, "a.txt"); got != "one\nTWO\nthree\nfour\n" {
		t.Errorf("the work tree was modified: %q", got)
	}
}

func TestDiscardingSomeLinesRemovesThemFromTheWorkTree(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "a.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	// Throw away the added line, keep the substitution.
	if err := runner.DiscardLines(ctx, dir, file, map[int]bool{4: true}); err != nil {
		t.Fatalf("DiscardLines: %v", err)
	}

	if got := readFile(t, dir, "a.txt"); got != "one\nTWO\nthree\n" {
		t.Errorf("work tree holds %q, expected the added line gone", got)
	}
}

func TestStagingPartOfANewFile(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	writeFile(t, dir, "new.txt", "alpha\nbeta\ngamma\n")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "new.txt", Side: git.DiffUntracked})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	// A creation is where the /dev/null on the old side matters: without it
	// git looks for content that is not there and blames the patch.
	if err := runner.StageLines(ctx, dir, file, map[int]bool{0: true, 2: true}); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	if got := indexContent(t, runner, dir, "new.txt"); got != "alpha\ngamma\n" {
		t.Errorf("index holds %q, expected the first and third lines", got)
	}
	if got := readFile(t, dir, "new.txt"); got != "alpha\nbeta\ngamma\n" {
		t.Errorf("the file on disk was modified: %q", got)
	}
}

func TestAPatchBuiltFromAStaleDiffIsRefused(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "a.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	patch, err := git.FormatPatch(file, git.ChangedLines(file), git.PatchForward)
	if err != nil {
		t.Fatalf("FormatPatch: %v", err)
	}

	// The file moves on underneath the selection. git refuses the patch
	// rather than applying it somewhere plausible — which is the last line of
	// defence behind the fingerprint the API checks first.
	writeFile(t, dir, "a.txt", "something else entirely\n")
	runGit(t, runner, dir, "add", "--", "a.txt")

	if err := runner.ApplyPatch(ctx, dir, patch, git.ApplyToIndex, false); err == nil {
		t.Fatal("expected git to refuse a patch that no longer describes the file")
	}
}

func TestStageAndUnstageAWholePath(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	if err := runner.Stage(ctx, dir, []string{"a.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if got := indexContent(t, runner, dir, "a.txt"); got != "one\nTWO\nthree\nfour\n" {
		t.Errorf("index holds %q after staging the whole file", got)
	}

	if err := runner.Unstage(ctx, dir, []string{"a.txt"}, false); err != nil {
		t.Fatalf("Unstage: %v", err)
	}
	if got := indexContent(t, runner, dir, "a.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("index holds %q after unstaging; expected the committed content back", got)
	}
	if got := readFile(t, dir, "a.txt"); got != "one\nTWO\nthree\nfour\n" {
		t.Errorf("unstaging touched the work tree: %q", got)
	}
}

func TestUnstageOnAnUnbornBranch(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "f.txt", "hello\n")
	if err := runner.Stage(ctx, dir, []string{"f.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	// `git restore --staged` has no HEAD to restore from here and exits 128.
	// The unborn flag is what picks the command that works.
	if err := runner.Unstage(ctx, dir, []string{"f.txt"}, true); err != nil {
		t.Fatalf("Unstage on an unborn branch: %v", err)
	}

	status, err := runner.Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Files) != 1 || status.Files[0].Kind != git.EntryUntracked {
		t.Errorf("expected f.txt back to untracked, got %+v", status.Files)
	}
	// Unstaging must not delete the user's work. `git rm` without --cached
	// would have.
	if got := readFile(t, dir, "f.txt"); got != "hello\n" {
		t.Errorf("the file was changed on disk: %q", got)
	}
}

func TestDiscardWholePaths(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	writeFile(t, dir, "untracked.txt", "throwaway\n")

	if err := runner.DiscardTracked(ctx, dir, []string{"a.txt"}); err != nil {
		t.Fatalf("DiscardTracked: %v", err)
	}
	if got := readFile(t, dir, "a.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("a.txt = %q, expected the committed content back", got)
	}

	if err := runner.DiscardUntracked(ctx, dir, []string{"untracked.txt"}); err != nil {
		t.Fatalf("DiscardUntracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "untracked.txt")); !os.IsNotExist(err) {
		t.Errorf("untracked.txt is still there: %v", err)
	}
}

func TestOperationsRefuseAnEmptyPathList(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	// `git clean --force --` with no path is not a no-op, so the guard is
	// here rather than in git's argument parsing.
	operations := map[string]func() error{
		"Stage":            func() error { return runner.Stage(ctx, dir, nil) },
		"Unstage":          func() error { return runner.Unstage(ctx, dir, nil, false) },
		"DiscardTracked":   func() error { return runner.DiscardTracked(ctx, dir, nil) },
		"DiscardUntracked": func() error { return runner.DiscardUntracked(ctx, dir, nil) },
	}
	for name, operation := range operations {
		if err := operation(); err == nil {
			t.Errorf("%s accepted an empty path list", name)
		}
	}
}

func TestCommitRecordsTheIndexAndReturnsItsSHA(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "add", "--", "a.txt")
	// The identity has to come from somewhere, and the isolated configuration
	// deliberately has none.
	runGit(t, runner, dir, "config", "user.name", "yagit Test")
	runGit(t, runner, dir, "config", "user.email", "test@yagit.local")

	sha, err := runner.Commit(ctx, dir, git.CommitOptions{
		Message: "second: a subject\n\nand a body that keeps its # sign\n",
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("SHA = %q, expected a full object name", sha)
	}

	status, err := runner.Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Clean() {
		t.Errorf("expected a clean working directory after the commit, got %+v", status.Files)
	}
	if status.HeadSHA != sha {
		t.Errorf("HEAD is %q, the commit reported %q", status.HeadSHA, sha)
	}

	// --cleanup=whitespace keeps a '#', which in a text box is text and not a
	// comment. The interactive default would have deleted the line.
	message, err := runner.Run(ctx, dir, "log", "-1", "--pretty=format:%B")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(message), "# sign") {
		t.Errorf("message = %q; a line holding '#' was stripped", message)
	}
}

func TestCommitRefusesAnEmptyMessageBeforeRunningGit(t *testing.T) {
	dir, runner := workingDirectory(t)

	_, err := runner.Commit(context.Background(), dir, git.CommitOptions{Message: "  \n\t "})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "message") {
		t.Errorf("err = %v; the message must say what is missing", err)
	}
}

// TestStagingALineAfterALastLineWithNoNewline is the case where the marker
// changes hands.
//
// HEAD holds "one\ntwo" with no trailing newline — ordinary, and what every
// editor without a final-newline setting produces. The work tree adds a line
// after it, so git writes `-two`, the marker, `+two`, `+three`: two lost its
// newline-less-ness because the file no longer ends there. Staging only the
// added line turns the removal into context, and a marker that travels with it
// says BOTH sides end at "two" — which `git apply` accepts, welding "two" and
// "three" into one line in the index. Silently: exit 0, and the panel redraws.
func TestStagingALineAfterALastLineWithNoNewline(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "f.txt", "one\ntwo")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "no newline")...)

	writeFile(t, dir, "f.txt", "one\ntwo\nthree\n")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "f.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	added := -1
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == git.LineAdded && line.Text == "three" {
				added = line.Index
			}
		}
	}
	if added < 0 {
		t.Fatalf("no added line for \"three\" in %+v", file.Hunks)
	}

	if err := runner.StageLines(ctx, dir, file, map[int]bool{added: true}); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	if got := indexContent(t, runner, dir, "f.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("index holds %q, expected one, two and three on three lines", got)
	}
	if got := readFile(t, dir, "f.txt"); got != "one\ntwo\nthree\n" {
		t.Errorf("the work tree was modified: %q", got)
	}
}

// TestStagingEveryLineOfAFileThatKeepsNoTrailingNewline is the other half:
// where the file still ends without a newline afterwards, the marker has to
// survive. Dropping it appends a newline the file never had.
func TestStagingEveryLineOfAFileThatKeepsNoTrailingNewline(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "f.txt", "one\ntwo")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "no newline")...)

	writeFile(t, dir, "f.txt", "one\nTWO")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "f.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	// StageLines rather than Stage: `git add` would take the whole file
	// without ever building a patch, which is the path this is not about.
	if err := runner.StageLines(ctx, dir, file, git.ChangedLines(file)); err != nil {
		t.Fatalf("StageLines: %v", err)
	}
	if got := indexContent(t, runner, dir, "f.txt"); got != "one\nTWO" {
		t.Errorf("index holds %q, expected no trailing newline", got)
	}
}

// TestStagingPartOfANewExecutableFileKeepsTheMode: the mode of a file being
// created reaches git only through `new file mode`, and `git apply` defaults
// to 100644 without it. Staging the file whole goes through `git add` and
// keeps the bit, so the loss happens on the partial path alone — and silently,
// because the work tree keeps it.
func TestStagingPartOfANewExecutableFileKeepsTheMode(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nalpha\nbeta\ngamma\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "run.sh", Side: git.DiffUntracked})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if file.NewMode != "100755" {
		t.Skipf("this filesystem does not carry the executable bit (mode %q)", file.NewMode)
	}

	if err := runner.StageLines(ctx, dir, file, map[int]bool{0: true, 1: true}); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	staged, err := runner.Run(ctx, dir, "ls-files", "--stage", "--", "run.sh")
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if !strings.HasPrefix(string(staged), "100755 ") {
		t.Errorf("index entry is %q, expected mode 100755", strings.TrimSpace(string(staged)))
	}
}

// TestDiscardingAFileNamedLikeAPatternRemovesOnlyIt: `--` ends option parsing;
// it does not make what follows a filename. Everything after it is a pathspec
// matched with wildmatch, so a file legitimately named `page/[id].tsx` — the
// ordinary shape of a routed page, and a name git stores happily — is a
// PATTERN by the time git reads it. `[id]` expands to `i` and to `d`, so `git
// clean --force` deletes three files while the interface named one, and
// nothing it removes was ever committed or is in any reflog.
func TestDiscardingAFileNamedLikeAPatternRemovesOnlyIt(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	writeFile(t, dir, "page/[id].tsx", "chosen\n")
	writeFile(t, dir, "page/i.tsx", "not chosen\n")
	writeFile(t, dir, "page/d.tsx", "not chosen either\n")

	if err := runner.DiscardUntracked(ctx, dir, []string{"page/[id].tsx"}); err != nil {
		t.Fatalf("DiscardUntracked: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "page", "[id].tsx")); !os.IsNotExist(err) {
		t.Error("the file the user chose is still there")
	}
	for _, kept := range []string{"page/i.tsx", "page/d.tsx"} {
		if _, err := os.Stat(filepath.Join(dir, kept)); err != nil {
			t.Errorf("%s was deleted and nobody asked: %v", kept, err)
		}
	}
}

// TestStagingANameThatIsNotThereStagesNothing is the same rule on the
// operations that build the index. `git add` matches a literal name in
// preference to a pattern, so the damage shows only where the named file is
// absent — a row the status listed and something removed in between. Naming
// one file must then be a refusal, never "these three matched instead".
func TestStagingANameThatIsNotThereStagesNothing(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	writeFile(t, dir, "page/i.tsx", "not chosen\n")
	writeFile(t, dir, "page/d.tsx", "not chosen either\n")

	if err := runner.Stage(ctx, dir, []string{"page/[id].tsx"}); err == nil {
		t.Fatal("expected git to refuse a name that is not there")
	}

	status, err := runner.Status(ctx, dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, file := range status.Files {
		if file.Staged() {
			t.Errorf("%s was staged and nobody named it", file.Path)
		}
	}
}

// TestDiscardingOnlyARemovalWhenAdditionsFollowIt is the mirror of the staging
// case above, and it corrupts just as quietly.
//
// HEAD holds "one\ntwo" with no trailing newline; the work tree adds a line
// after it, so git writes `-two`, the marker, `+two`, `+three`. Taking back
// only the removal leaves the additions in place — so "two" is no longer the
// last line of anything, and a marker still travelling with it says the old
// side ends there. `git apply` accepts that and welds the two "two" lines into
// one, exit 0, with the panel redrawing as though nothing happened.
func TestDiscardingOnlyARemovalWhenAdditionsFollowIt(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "f.txt", "one\ntwo")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "no newline")...)

	writeFile(t, dir, "f.txt", "one\ntwo\nthree\n")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "f.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	removal := -1
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == git.LineRemoved && line.Text == "two" {
				removal = line.Index
			}
		}
	}
	if removal < 0 {
		t.Fatalf("no removed line for \"two\" in %+v", file.Hunks)
	}

	if err := runner.DiscardLines(ctx, dir, file, map[int]bool{removal: true}); err != nil {
		t.Fatalf("DiscardLines: %v", err)
	}

	// The removal is undone and the two additions stay, so "two" appears
	// twice. What must not happen is the two of them arriving on one line.
	if got := readFile(t, dir, "f.txt"); got != "one\ntwo\ntwo\nthree\n" {
		t.Errorf("work tree holds %q, expected each line on its own", got)
	}
}

// TestCommitAmendReplacesThePreviousCommit: --amend is the one history-writing
// operation here, and the difference between it and an ordinary commit is a
// single flag. Without this the flag could be dropped, or added where it does
// not belong, and every test would still pass.
func TestCommitAmendReplacesThePreviousCommit(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "add", "--", "a.txt")
	configureIdentity(t, runner, dir)
	before := commitCount(t, runner, dir)

	sha, err := runner.Commit(ctx, dir, git.CommitOptions{Message: "second", Amend: true})
	if err != nil {
		t.Fatalf("Commit --amend: %v", err)
	}
	if sha == "" {
		t.Fatal("an amended commit still has a name")
	}

	if after := commitCount(t, runner, dir); after != before {
		t.Errorf("HEAD has %d commits, expected the %d it started with", after, before)
	}
	if subject := headSubject(t, runner, dir); subject != "second" {
		t.Errorf("subject = %q, expected the amended message", subject)
	}
}

// TestCommitWithoutAmendAddsACommit is the other direction of the same flag: a
// regression that always amended would destroy the previous commit, and would
// pass every assertion the test above makes on its own.
func TestCommitWithoutAmendAddsACommit(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "add", "--", "a.txt")
	configureIdentity(t, runner, dir)
	before := commitCount(t, runner, dir)

	if _, err := runner.Commit(ctx, dir, git.CommitOptions{Message: "second"}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if after := commitCount(t, runner, dir); after != before+1 {
		t.Errorf("HEAD has %d commits, expected %d", after, before+1)
	}
}

// configureIdentity gives the repository an author. The isolated
// configuration deliberately has none, and Commit runs git without one.
func configureIdentity(t *testing.T, runner *git.Runner, dir string) {
	t.Helper()
	runGit(t, runner, dir, "config", "user.name", "yagit Test")
	runGit(t, runner, dir, "config", "user.email", "test@yagit.local")
}

func commitCount(t *testing.T, runner *git.Runner, dir string) int {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("git rev-list --count HEAD: %v", err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		t.Fatalf("unreadable commit count %q: %v", output, err)
	}
	return count
}

func headSubject(t *testing.T, runner *git.Runner, dir string) string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "log", "-1", "--pretty=format:%s")
	if err != nil {
		t.Fatalf("git log -1: %v", err)
	}
	return strings.TrimSpace(string(output))
}

// TestStagingAWholeDeletionThroughAPatchRemovesTheIndexEntry: /dev/null on the
// new side is what tells `git apply` the file is gone. Written as `+++ b/path`
// instead, git stages an empty blob and the commit records a file with no
// content where the user removed one.
func TestStagingAWholeDeletionThroughAPatchRemovesTheIndexEntry(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "checkout", "--", "a.txt")
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("remove a.txt: %v", err)
	}

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "a.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	patch, err := git.FormatPatch(file, git.ChangedLines(file), git.PatchForward)
	if err != nil {
		t.Fatalf("FormatPatch: %v", err)
	}
	if err := runner.ApplyPatch(ctx, dir, patch, git.ApplyToIndex, false); err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}

	listed, err := runner.Run(ctx, dir, "ls-files", "--", "a.txt")
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if strings.TrimSpace(string(listed)) != "" {
		t.Errorf("a.txt is still in the index as %q", strings.TrimSpace(string(listed)))
	}
}

// TestStagingOnlyTheRemovalsOfAContextFreeHunk pins two things a hunk covering
// a whole file gets wrong together.
//
// The result side ends up empty, and a unified diff numbers an empty range by
// the line BEFORE it — arithmetic nothing else exercises. And the patch is
// made only of removals, which is the trap formatPatchHeader exists for: an
// empty new side looks exactly like a deletion, and a header that wrote
// /dev/null for it would remove a file the user still has.
func TestStagingOnlyTheRemovalsOfAContextFreeHunk(t *testing.T) {
	isolateGitConfiguration(t)
	dir := t.TempDir()
	runner := git.NewRunner(nil)
	ctx := context.Background()

	runGit(t, runner, dir, "init", "-b", "main")
	writeFile(t, dir, "f.txt", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "first")...)

	// Every line replaced, so the hunk holds no context at all.
	writeFile(t, dir, "f.txt", "ONE\nTWO\nTHREE\n")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: "f.txt", Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	selected := map[int]bool{}
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == git.LineRemoved {
				selected[line.Index] = true
			}
		}
	}

	if err := runner.StageLines(ctx, dir, file, selected); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	if got := indexContent(t, runner, dir, "f.txt"); got != "" {
		t.Errorf("index holds %q, expected the three lines gone and nothing added", got)
	}
	listed, err := runner.Run(ctx, dir, "ls-files", "--", "f.txt")
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	if strings.TrimSpace(string(listed)) != "f.txt" {
		t.Error("the file was staged as a deletion; only its lines were removed")
	}
	if got := readFile(t, dir, "f.txt"); got != "ONE\nTWO\nTHREE\n" {
		t.Errorf("the work tree was modified: %q", got)
	}
}

// TestStagingSomeLinesOfANonASCIIName closes the loop the diff parser opens.
//
// git writes the name C-quoted and this package reads the quoting back, so the
// patch it builds carries the real name — which `git apply` has to accept in
// turn. Without the round trip, unquoting could be right and staging still
// broken for every accented, CJK or emoji filename in the repository.
func TestStagingSomeLinesOfANonASCIIName(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	const name = "café.txt"
	writeFile(t, dir, name, "un\ndeux\ntrois\n")
	runGit(t, runner, dir, "add", "--", name)
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "accented")...)

	writeFile(t, dir, name, "UN\ndeux\nTROIS\n")

	file, err := runner.Diff(ctx, dir, git.DiffRequest{Path: name, Side: git.DiffUnstaged})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	first := -1
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == git.LineAdded && line.Text == "UN" {
				first = line.Index
			}
		}
	}
	if first < 0 {
		t.Fatalf("no added line for \"UN\" in %+v", file.Hunks)
	}

	removedFirst := -1
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == git.LineRemoved && line.Text == "un" {
				removedFirst = line.Index
			}
		}
	}
	if removedFirst < 0 {
		t.Fatalf("no removed line for \"un\" in %+v", file.Hunks)
	}

	if err := runner.StageLines(ctx, dir, file, map[int]bool{first: true, removedFirst: true}); err != nil {
		t.Fatalf("StageLines: %v", err)
	}

	if got := indexContent(t, runner, dir, name); got != "UN\ndeux\ntrois\n" {
		t.Errorf("index holds %q, expected only the first line changed", got)
	}
}
