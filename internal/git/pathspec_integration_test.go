package git_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// What a unit test cannot say: that the form yagit falls back to past the
// command-line limit is one real git accepts, and that it stages exactly the
// files it was given.
//
// The limit is Windows's, and it is reached at roughly eight hundred files of
// ordinary length — while the request body carrying them is still a quarter of
// its own cap. The fallback therefore runs on a machine nobody develops on,
// which is why the threshold is one number for all three platforms and why
// this test builds enough files to cross it here.

// manyFiles writes count files with names long enough that their pathspecs
// pass maxPathspecArgv together, and returns their paths.
func manyFiles(t *testing.T, dir string, count int) []string {
	t.Helper()

	// Forty characters of name plus ten of `:(literal)` — a thousand of them
	// is fifty thousand characters, comfortably past the thirty thousand the
	// command line is allowed.
	const filler = "a-name-long-enough-to-cross-the-limit"

	paths := make([]string, 0, count)
	for index := range count {
		name := fmt.Sprintf("%s-%04d.txt", filler, index)
		writeFile(t, dir, name, "content "+strconv.Itoa(index)+"\n")
		paths = append(paths, name)
	}
	return paths
}

func stagedPaths(t *testing.T, runner *git.Runner, dir string) map[string]bool {
	t.Helper()

	output, err := runner.Run(context.Background(), dir, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		t.Fatalf("listing the index: %v", err)
	}

	staged := map[string]bool{}
	for _, name := range strings.Split(string(output), "\x00") {
		if name != "" {
			staged[name] = true
		}
	}
	return staged
}

func TestStagingMoreFilesThanACommandLineHolds(t *testing.T) {
	dir, runner := workingDirectory(t)

	paths := manyFiles(t, dir, 1000)
	if err := runner.Stage(context.Background(), dir, paths); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	staged := stagedPaths(t, runner, dir)
	for _, path := range paths {
		if !staged[path] {
			t.Fatalf("%s was not staged: the fallback did not reach git", path)
		}
	}
	if len(staged) != len(paths) {
		t.Errorf("staged %d files, asked for %d", len(staged), len(paths))
	}
}

// The fallback still has to mean the names it was given, not patterns matching
// them. `:(literal)` is what makes that true, and it has to survive the move
// from the command line into the file.
func TestTheFallbackStillTakesNamesLiterally(t *testing.T) {
	dir, runner := workingDirectory(t)

	// One file whose name is a glob, among enough others to force the
	// fallback. Staging it must stage that file and nothing else.
	paths := manyFiles(t, dir, 900)
	writeFile(t, dir, "*", "the file actually called star\n")
	writeFile(t, dir, "innocent.txt", "should stay unstaged\n")
	paths = append(paths, "*")

	if err := runner.Stage(context.Background(), dir, paths); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	staged := stagedPaths(t, runner, dir)
	if !staged["*"] {
		t.Error("the file literally called * was not staged")
	}
	if staged["innocent.txt"] {
		t.Error("* was read as a pattern: a file nobody named was staged")
	}
}

func TestUnstagingMoreFilesThanACommandLineHolds(t *testing.T) {
	dir, runner := workingDirectory(t)

	paths := manyFiles(t, dir, 1000)
	if err := runner.Stage(context.Background(), dir, paths); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if err := runner.Unstage(context.Background(), dir, paths, false); err != nil {
		t.Fatalf("Unstage: %v", err)
	}

	if staged := stagedPaths(t, runner, dir); len(staged) != 0 {
		t.Errorf("%d files are still staged after unstaging every one", len(staged))
	}
}

// `git clean` is the one command git never gave a pathspec file, so this list
// reaches it as several commands. What the test pins is that every file goes.
func TestDiscardingMoreUntrackedFilesThanACommandLineHolds(t *testing.T) {
	dir, runner := workingDirectory(t)

	paths := manyFiles(t, dir, 1000)

	batches := git.DiscardUntrackedBatches(paths)
	if len(batches) < 2 {
		t.Fatalf("this many paths produced %d command(s): the test is not exercising the split", len(batches))
	}

	if err := runner.DiscardUntracked(context.Background(), dir, paths); err != nil {
		t.Fatalf("DiscardUntracked: %v", err)
	}

	// Untracked only. The fixture leaves a tracked file modified, and `git
	// clean` is right to leave that alone — discarding it is `git restore`,
	// which the interface asks about separately.
	output, err := runner.Run(context.Background(), dir,
		"ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range strings.Split(string(output), "\x00") {
		if name != "" {
			t.Errorf("an untracked file survived the discard: %q", name)
		}
	}
}
