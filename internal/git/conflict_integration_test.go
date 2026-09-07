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

// A real merge that does not resolve on its own, and the three ways out of it.
//
// Every fact these tests rest on came from running git and reading what it
// said, not from the documentation: that `git diff` on an unmerged path
// answers in a format this package does not read, that `git checkout --ours`
// FAILS on a path our side deleted, that resolving takes two commands and not
// one. Each of those was a bug before it was a test.

// conflicted builds a repository whose merge stopped on f.txt.
//
//	base:  one
//	main:  one MAIN three      ← checked out
//	side:  one SIDE three
func conflicted(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	writeWorkFile(t, dir, "f.txt", "one\ntwo\nthree\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, commitWith("base")...)

	runGit(t, runner, dir, "checkout", "-b", "side")
	writeWorkFile(t, dir, "f.txt", "one\nSIDE\nthree\n")
	runGit(t, runner, dir, commitWith("side")...)

	runGit(t, runner, dir, "checkout", "main")
	writeWorkFile(t, dir, "f.txt", "one\nMAIN\nthree\n")
	runGit(t, runner, dir, commitWith("main")...)

	// The merge is expected to fail; that is the state under test.
	if _, err := runner.Run(context.Background(), dir,
		append(append([]string{}, identity...), "merge", "side")...); err == nil {
		t.Fatal("the merge succeeded, so there is no conflict to test")
	}

	return dir, runner
}

func commitWith(message string) []string {
	return append(append([]string{}, identity...), "commit", "-am", message)
}

func writeWorkFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func statusOf(t *testing.T, runner *git.Runner, dir string) git.Status {
	t.Helper()
	status, err := runner.Status(context.Background(), dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return status
}

func fileIn(t *testing.T, status git.Status, path string) git.FileStatus {
	t.Helper()
	for _, file := range status.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("%s is not in the status: %+v", path, status.Files)
	return git.FileStatus{}
}

func TestAStoppedMergeIsReportedAsOne(t *testing.T) {
	dir, runner := conflicted(t)

	status := statusOf(t, runner, dir)
	if !status.Conflicted() {
		t.Fatalf("status is not conflicted: %+v", status.Files)
	}
	if got := fileIn(t, status, "f.txt").Conflict(); got != git.ConflictBothModified {
		t.Fatalf("Conflict() = %q, want %q", got, git.ConflictBothModified)
	}

	state, err := git.ReadState(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationMerge {
		t.Fatalf("Operation = %q, want merge", state.Operation)
	}
}

// The bug this replaces: `git diff` on an unmerged path answers in git's
// COMBINED format, which is not a unified diff and cannot be staged line by
// line. The parser used to fail on the `@@@`, and the interface showed
// `range "@@@" does not start with "-"` over a file whose real problem was an
// unfinished merge.
func TestTheDiffOfAnUnmergedPathIsRefusedByName(t *testing.T) {
	dir, runner := conflicted(t)

	_, err := runner.Diff(context.Background(), dir, git.DiffRequest{Path: "f.txt", Side: git.DiffUnstaged})
	if !errors.Is(err, git.ErrCombinedDiff) {
		t.Fatalf("Diff error = %v, want ErrCombinedDiff", err)
	}
}

func TestGitLeavesConflictMarkersInTheFile(t *testing.T) {
	dir, _ := conflicted(t)

	content, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
		if !strings.Contains(string(content), marker) {
			t.Fatalf("%q is not in the merged file:\n%s", marker, content)
		}
	}
}

func TestKeepingOneSideResolvesTheConflict(t *testing.T) {
	for _, testCase := range []struct {
		side git.ConflictSide
		want string

		// recorded says the resolution leaves something for the commit to
		// record. Keeping OURS here does not, and that is not a bug: our side
		// is what HEAD already says, so the index ends up matching it and git
		// reports nothing at all for the path. The merge is still unfinished
		// and still commits — a merge commit's job is to join two histories,
		// not to change a file.
		recorded bool
	}{
		{git.SideOurs, "one\nMAIN\nthree\n", false},
		{git.SideTheirs, "one\nSIDE\nthree\n", true},
	} {
		t.Run(string(testCase.side), func(t *testing.T) {
			dir, runner := conflicted(t)

			if err := runner.KeepSide(context.Background(), dir, []string{"f.txt"}, testCase.side); err != nil {
				t.Fatalf("KeepSide: %v", err)
			}

			content, err := os.ReadFile(filepath.Join(dir, "f.txt"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(content) != testCase.want {
				t.Fatalf("f.txt = %q, want %q", content, testCase.want)
			}

			// The half that `git checkout --ours` alone does not do. Without
			// the `git add` that follows it, the path stays unmerged: the work
			// tree looks finished and git still refuses to commit.
			status := statusOf(t, runner, dir)
			if status.Conflicted() {
				t.Fatalf("still unmerged after keeping a side: %+v", status.Files)
			}
			if testCase.recorded && !fileIn(t, status, "f.txt").Staged() {
				t.Fatalf("f.txt is not staged after being resolved: %+v", status.Files)
			}
			if !testCase.recorded && len(status.Files) != 0 {
				t.Fatalf("keeping HEAD's own content left something to record: %+v", status.Files)
			}

			// Unmerged either way, and the merge is still in progress either
			// way: what makes it committable is the index, not the file list.
			state, err := git.ReadState(filepath.Join(dir, ".git"))
			if err != nil {
				t.Fatalf("ReadState: %v", err)
			}
			if state.Operation != git.OperationMerge {
				t.Fatalf("Operation = %q, want the merge still in progress", state.Operation)
			}
		})
	}
}

// After resolving, the merge can be committed — with the message git prepared
// for it, which is the whole point of reading MERGE_MSG.
func TestAResolvedMergeCommitsWithGitsOwnMessage(t *testing.T) {
	dir, runner := conflicted(t)

	text, source, err := runner.PreparedMessage(context.Background(), dir, filepath.Join(dir, ".git"), false)
	if err != nil {
		t.Fatalf("PreparedMessage: %v", err)
	}
	if source != git.SourceMerge {
		t.Fatalf("source = %q, want merge", source)
	}
	if !strings.HasPrefix(text, "Merge branch 'side'") {
		t.Fatalf("prepared message = %q", text)
	}
	// `git stripspace --strip-comments` is what removes the "# Conflicts:"
	// block git writes below the subject. Left in, it would be committed
	// verbatim: yagit commits with --cleanup=whitespace, which keeps '#'
	// lines because in a text box a '#' is text.
	if strings.Contains(text, "# Conflicts:") {
		t.Fatalf("the comment block survived: %q", text)
	}

	if err := runner.KeepSide(context.Background(), dir, []string{"f.txt"}, git.SideTheirs); err != nil {
		t.Fatalf("KeepSide: %v", err)
	}

	configureCommitter(t, runner, dir)
	if _, err := runner.Commit(context.Background(), dir, git.CommitOptions{Message: text}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	state, err := git.ReadState(filepath.Join(dir, ".git"))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.InProgress() {
		t.Fatalf("the merge is still in progress after being committed: %+v", state)
	}
}

// An ordinary repository has prepared nothing, and that is not a failure.
func TestNoOperationPreparesNoMessage(t *testing.T) {
	dir, runner := testRepository(t)

	text, source, err := runner.PreparedMessage(context.Background(), dir, filepath.Join(dir, ".git"), false)
	if err != nil {
		t.Fatalf("PreparedMessage: %v", err)
	}
	if text != "" || source != git.SourceNone {
		t.Fatalf("text = %q, source = %q, want neither", text, source)
	}
}

// "Deleted by us": our side has no version at all. `git checkout --ours` fails
// there with "path 'f.txt' does not have our version", so keeping ours means
// recording the deletion instead — which is what HasSide is read for.
func TestKeepingASideThatHasNoFileRemovesIt(t *testing.T) {
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")
	writeWorkFile(t, dir, "f.txt", "one\n")
	runGit(t, runner, dir, "add", "--", "f.txt")
	runGit(t, runner, dir, commitWith("base")...)

	runGit(t, runner, dir, "checkout", "-b", "side")
	writeWorkFile(t, dir, "f.txt", "one\ntwo\n")
	runGit(t, runner, dir, commitWith("side edits it")...)

	runGit(t, runner, dir, "checkout", "main")
	runGit(t, runner, dir, "rm", "--quiet", "--", "f.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...), "commit", "-m", "main removes it")...)

	if _, err := runner.Run(context.Background(), dir,
		append(append([]string{}, identity...), "merge", "side")...); err == nil {
		t.Fatal("the merge succeeded, so there is no conflict to test")
	}

	record := fileIn(t, statusOf(t, runner, dir), "f.txt")
	if got := record.Conflict(); got != git.ConflictDeletedByUs {
		t.Fatalf("Conflict() = %q, want %q", got, git.ConflictDeletedByUs)
	}
	if record.HasSide(git.SideOurs) {
		t.Fatal("HasSide(ours) is true for a path our side deleted")
	}
	if !record.HasSide(git.SideTheirs) {
		t.Fatal("HasSide(theirs) is false for a path their side kept")
	}

	// The command the interface must run instead, checked against real git:
	// a checkout here fails, and `git rm` is the resolution.
	if err := runner.KeepSide(context.Background(), dir, []string{"f.txt"}, git.SideOurs); err == nil {
		t.Fatal("git checkout --ours succeeded on a path our side deleted")
	}
	if err := runner.RemoveConflicted(context.Background(), dir, []string{"f.txt"}); err != nil {
		t.Fatalf("RemoveConflicted: %v", err)
	}

	if statusOf(t, runner, dir).Conflicted() {
		t.Fatal("still unmerged after recording the deletion")
	}
	if _, err := os.Stat(filepath.Join(dir, "f.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("f.txt is still on disk: %v", err)
	}
}

// The third way out, and the one the editing pane exists for: fix the file by
// hand, stage it, and the conflict is over.
func TestEditingTheFileAndStagingItResolvesTheConflict(t *testing.T) {
	dir, runner := conflicted(t)

	writeWorkFile(t, dir, "f.txt", "one\nMAIN\nSIDE\nthree\n")
	if err := runner.Stage(context.Background(), dir, []string{"f.txt"}); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if statusOf(t, runner, dir).Conflicted() {
		t.Fatal("still unmerged after staging a hand-resolved file")
	}
}

func TestResolvingRefusesAnEmptyPathList(t *testing.T) {
	dir, runner := conflicted(t)

	if err := runner.KeepSide(context.Background(), dir, nil, git.SideOurs); !errors.Is(err, git.ErrNoPaths) {
		t.Fatalf("KeepSide with no paths: err = %v, want ErrNoPaths", err)
	}
	if err := runner.RemoveConflicted(context.Background(), dir, nil); !errors.Is(err, git.ErrNoPaths) {
		t.Fatalf("RemoveConflicted with no paths: err = %v, want ErrNoPaths", err)
	}
	if err := runner.KeepSide(context.Background(), dir, []string{"f.txt"}, "either"); err == nil {
		t.Fatal("KeepSide accepted a side that is neither ours nor theirs")
	}
}

func configureCommitter(t *testing.T, runner *git.Runner, dir string) {
	t.Helper()
	runGit(t, runner, dir, "config", "user.name", "yagit Test")
	runGit(t, runner, dir, "config", "user.email", "test@yagit.local")
}
