package git_test

import (
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// parseOneFile is the shorthand these tests need: a diff describing a single
// file, parsed, with the failure reported here rather than in every case.
func parseOneFile(t *testing.T, output []byte) git.FileDiff {
	t.Helper()

	files, err := git.ParseDiff(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file in the diff, got %d", len(files))
	}
	return files[0]
}

// selectAll picks every added and removed line, which is the case that has to
// reproduce git's own output.
func selectAll(file git.FileDiff) map[int]bool {
	return git.ChangedLines(file)
}

func TestFormatPatchWithEveryLineSelectedReproducesTheDiffBody(t *testing.T) {
	// The round trip is the property that matters. A patch built from every
	// line of a diff is that diff, so any drift here — a miscounted range, a
	// lost marker — shows up as a difference from git's own bytes.
	file := parseOneFile(t, []byte(modifiedDiff))

	patch, err := git.FormatPatch(file, selectAll(file), git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,3 +1,4 @@\n one\n-two\n+TWO\n three\n+four\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchDropsAnUnselectedAddition(t *testing.T) {
	file := parseOneFile(t, []byte(modifiedDiff))

	// Lines: 0 context, 1 removed "two", 2 added "TWO", 3 context, 4 added
	// "four". Take the substitution, leave "four" behind.
	patch, err := git.FormatPatch(file, map[int]bool{1: true, 2: true}, git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	// "four" is absent from the old file and must be absent from the result,
	// so it is dropped — and the new side is one line shorter than git's.
	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchTurnsAnUnselectedRemovalIntoContext(t *testing.T) {
	file := parseOneFile(t, []byte(modifiedDiff))

	// Take "four" alone. "two" is not being removed, so it is still in the
	// file the patch produces: it becomes context, and both sides count it.
	patch, err := git.FormatPatch(file, map[int]bool{4: true}, git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,3 +1,4 @@\n one\n two\n three\n+four\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchSkipsAHunkWithNothingSelected(t *testing.T) {
	file := parseOneFile(t, diffOutput(
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1,2 +1,2 @@",
		" one",
		"-two",
		"+TWO",
		"@@ -10,2 +10,2 @@",
		" ten",
		"-eleven",
		"+ELEVEN",
	))

	// Only the second hunk's lines. The first collapses to context, which
	// changes nothing, so it is left out entirely.
	patch, err := git.FormatPatch(file, map[int]bool{4: true, 5: true}, git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -10,2 +10,2 @@\n ten\n-eleven\n+ELEVEN\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchShiftsLaterHunksByWhatEarlierOnesChanged(t *testing.T) {
	// Two hunks, the first adding a line. The second hunk begins one line
	// later in the file this patch produces than it did in the old one, and
	// its header has to say so.
	file := parseOneFile(t, diffOutput(
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1,1 +1,2 @@",
		" one",
		"+inserted",
		"@@ -10,1 +11,2 @@",
		" ten",
		"+also inserted",
	))

	patch, err := git.FormatPatch(file, selectAll(file), git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1 +1,2 @@\n one\n+inserted\n" +
		"@@ -10 +11,2 @@\n ten\n+also inserted\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchLeavingOutTheFirstInsertionMovesTheSecondBack(t *testing.T) {
	file := parseOneFile(t, diffOutput(
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1,1 +1,2 @@",
		" one",
		"+inserted",
		"@@ -10,1 +11,2 @@",
		" ten",
		"+also inserted",
	))

	// Only the second insertion. The first hunk is gone, so nothing shifted
	// before the second one and it starts where it did in the old file.
	patch, err := git.FormatPatch(file, map[int]bool{3: true}, git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -10 +10,2 @@\n ten\n+also inserted\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchOfANewFileWritesDevNullOnTheOldSide(t *testing.T) {
	file := parseOneFile(t, diffOutput(
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1,3 @@",
		"+one",
		"+two",
		"+three",
	))

	// Two of the three lines. The result is a file with two lines in it,
	// numbered from one — the old side is empty, so git's rule of numbering
	// an empty range by the line before it puts the new side at 1.
	patch, err := git.FormatPatch(file, map[int]bool{0: true, 2: true}, git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- /dev/null\n+++ b/new.txt\n" +
		"@@ -0,0 +1,2 @@\n+one\n+three\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchKeepsTheNoNewlineMarker(t *testing.T) {
	file := parseOneFile(t, diffOutput(
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"-before",
		"\\ No newline at end of file",
		"+after",
		"\\ No newline at end of file",
	))

	patch, err := git.FormatPatch(file, selectAll(file), git.PatchForward)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	const expected = "--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n" +
		"-before\n\\ No newline at end of file\n" +
		"+after\n\\ No newline at end of file\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchRefusesWhatItCannotExpress(t *testing.T) {
	t.Run("nothing selected", func(t *testing.T) {
		file := parseOneFile(t, []byte(modifiedDiff))
		_, err := git.FormatPatch(file, map[int]bool{}, git.PatchForward)
		if !errors.Is(err, git.ErrNothingSelected) {
			t.Errorf("err = %v, expected ErrNothingSelected", err)
		}
	})

	t.Run("only context selected", func(t *testing.T) {
		// Index 0 is a context line. Choosing it is not a choice, so the
		// answer is the same as choosing nothing.
		file := parseOneFile(t, []byte(modifiedDiff))
		_, err := git.FormatPatch(file, map[int]bool{0: true, 3: true}, git.PatchForward)
		if !errors.Is(err, git.ErrNothingSelected) {
			t.Errorf("err = %v, expected ErrNothingSelected", err)
		}
	})

	t.Run("binary", func(t *testing.T) {
		file := parseOneFile(t, diffOutput(
			"diff --git a/logo.png b/logo.png",
			"Binary files a/logo.png and b/logo.png differ",
		))
		if _, err := git.FormatPatch(file, map[int]bool{0: true}, git.PatchForward); err == nil {
			t.Error("a binary file has no lines to choose between")
		}
	})

	t.Run("rename", func(t *testing.T) {
		// A rename has no lines in it. Half of one is a guess about what the
		// user meant, and this project does not guess.
		file := parseOneFile(t, diffOutput(
			"diff --git a/old.txt b/new.txt",
			"rename from old.txt",
			"rename to new.txt",
			"--- a/old.txt",
			"+++ b/new.txt",
			"@@ -1 +1 @@",
			"-before",
			"+after",
		))
		if _, err := git.FormatPatch(file, selectAll(file), git.PatchForward); err == nil {
			t.Error("expected a refusal naming the rename")
		}
	})
}

func TestChangedLinesCountsOnlyChanges(t *testing.T) {
	file := parseOneFile(t, []byte(modifiedDiff))

	changed := git.ChangedLines(file)
	if len(changed) != 3 {
		t.Errorf("expected 3 changed lines, got %d", len(changed))
	}
	for _, contextIndex := range []int{0, 3} {
		if changed[contextIndex] {
			t.Errorf("index %d is context and must not be selectable", contextIndex)
		}
	}
}

func TestCoversEveryChange(t *testing.T) {
	file := parseOneFile(t, []byte(modifiedDiff))

	if !git.CoversEveryChange(file, selectAll(file)) {
		t.Error("every changed line selected covers the file")
	}
	if git.CoversEveryChange(file, map[int]bool{1: true}) {
		t.Error("one line out of three does not cover the file")
	}

	// An empty diff is covered by nothing: answering true would turn "there
	// is nothing here" into "stage the whole file", which are different
	// operations.
	empty := git.FileDiff{Path: "a.txt"}
	if git.CoversEveryChange(empty, map[int]bool{}) {
		t.Error("a diff with no change is not covered")
	}
}

func TestFormatPatchInReverseKeepsAnUnselectedAdditionAsContext(t *testing.T) {
	// The mirror of the forward rules, and the reason there is a direction at
	// all. The base here is the NEW side: "TWO" and "four" are both in the
	// file being changed. Discarding "four" alone must leave "TWO" exactly
	// where it is — as context, not as the "two" the old side had.
	file := parseOneFile(t, []byte(modifiedDiff))

	patch, err := git.FormatPatch(file, map[int]bool{4: true}, git.PatchReverse)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	// New side: one, TWO, three, four — the file as it stands.
	// Old side: one, TWO, three — the file once "four" is gone.
	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,3 +1,4 @@\n one\n TWO\n three\n+four\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchInReverseDropsAnUnselectedRemoval(t *testing.T) {
	file := parseOneFile(t, []byte(modifiedDiff))

	// Take back the substitution and leave "four" alone. "two" was removed
	// and is not coming back, so it is absent from both sides of this patch.
	patch, err := git.FormatPatch(file, map[int]bool{1: true, 2: true}, git.PatchReverse)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	// Both sides hold four lines: the old one is the file with "two" restored
	// and "four" still there, which is exactly what unstaging the
	// substitution alone leaves behind.
	const expected = "--- a/a.txt\n+++ b/a.txt\n" +
		"@@ -1,4 +1,4 @@\n one\n-two\n+TWO\n three\n four\n"
	if string(patch) != expected {
		t.Errorf("patch =\n%s\nexpected\n%s", patch, expected)
	}
}

func TestFormatPatchReversedIsNotTheForwardPatchTurnedAround(t *testing.T) {
	// Stated as a test because it is the mistake worth pinning: the two
	// patches differ in the lines the user did NOT choose, and `git apply`
	// matches on exactly those.
	file := parseOneFile(t, []byte(modifiedDiff))
	selection := map[int]bool{4: true}

	forward, err := git.FormatPatch(file, selection, git.PatchForward)
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	reverse, err := git.FormatPatch(file, selection, git.PatchReverse)
	if err != nil {
		t.Fatalf("reverse: %v", err)
	}
	if string(forward) == string(reverse) {
		t.Error("the two directions must differ; if they do not, one of them is wrong")
	}
}
