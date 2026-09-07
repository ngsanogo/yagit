package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// diffOutput joins lines the way git writes them: separated by newlines, with
// a trailing one.
func diffOutput(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

const modifiedDiff = `diff --git a/a.txt b/a.txt
index 4cb29ea..6addb9b 100644
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,4 @@
 one
-two
+TWO
 three
+four
`

func TestParseDiffEmptyOutput(t *testing.T) {
	files, err := git.ParseDiff(nil)
	if err != nil {
		t.Fatalf("empty output must not be an error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no file, got %d", len(files))
	}
}

func TestParseDiffModifiedFile(t *testing.T) {
	files, err := git.ParseDiff([]byte(modifiedDiff))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	file := files[0]
	if file.Path != "a.txt" {
		t.Errorf("Path = %q", file.Path)
	}
	if file.OldPath != "" {
		t.Errorf("OldPath = %q; a path the diff did not change is not a rename", file.OldPath)
	}
	if file.Added || file.Removed || file.Binary {
		t.Errorf("a modified file is none of added, removed, binary: %+v", file)
	}
	if len(file.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(file.Hunks))
	}

	hunk := file.Hunks[0]
	if hunk.OldStart != 1 || hunk.OldLines != 3 || hunk.NewStart != 1 || hunk.NewLines != 4 {
		t.Errorf("hunk range = -%d,%d +%d,%d", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines)
	}

	expected := []struct {
		kind    git.LineKind
		text    string
		oldLine int
		newLine int
	}{
		{git.LineContext, "one", 1, 1},
		{git.LineRemoved, "two", 2, 0},
		{git.LineAdded, "TWO", 0, 2},
		{git.LineContext, "three", 3, 3},
		{git.LineAdded, "four", 0, 4},
	}
	if len(hunk.Lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d", len(expected), len(hunk.Lines))
	}
	for index, want := range expected {
		line := hunk.Lines[index]
		if line.Kind != want.kind || line.Text != want.text {
			t.Errorf("line %d = %q %q, expected %q %q", index, line.Kind, line.Text, want.kind, want.text)
		}
		if line.OldLine != want.oldLine || line.NewLine != want.newLine {
			t.Errorf("line %d numbers = %d/%d, expected %d/%d",
				index, line.OldLine, line.NewLine, want.oldLine, want.newLine)
		}
		if line.Index != index {
			t.Errorf("line %d has Index %d; the index is the address a selection uses", index, line.Index)
		}
	}
}

func TestParseDiffNewFile(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/new.txt b/new.txt",
		"new file mode 100644",
		"index 0000000..3e75765",
		"--- /dev/null",
		"+++ b/new.txt",
		"@@ -0,0 +1,2 @@",
		"+first",
		"+second",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	file := files[0]
	if !file.Added {
		t.Error("a file whose old side is /dev/null is added")
	}
	if file.Removed {
		t.Error("Removed must stay false for a creation")
	}
	if file.NewMode != "100644" {
		t.Errorf("NewMode = %q", file.NewMode)
	}
	if file.OldPath != "" {
		t.Errorf("OldPath = %q; /dev/null is not a path", file.OldPath)
	}
	if got := file.Hunks[0].OldStart; got != 0 {
		t.Errorf("OldStart = %d; git numbers an empty range by the line before it", got)
	}
}

func TestParseDiffDeletedFile(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/gone.txt b/gone.txt",
		"deleted file mode 100644",
		"index 3e75765..0000000",
		"--- a/gone.txt",
		"+++ /dev/null",
		"@@ -1,2 +0,0 @@",
		"-first",
		"-second",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	file := files[0]
	if !file.Removed || file.Added {
		t.Errorf("expected removed and not added, got %+v", file)
	}
	if file.Path != "gone.txt" {
		t.Errorf("Path = %q; the path survives a /dev/null on the new side", file.Path)
	}
}

func TestParseDiffRename(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/old name.txt b/new name.txt",
		"similarity index 87%",
		"rename from old name.txt",
		"rename to new name.txt",
		"index 4cb29ea..6addb9b 100644",
		"--- a/old name.txt",
		"+++ b/new name.txt",
		"@@ -1 +1 @@",
		"-before",
		"+after",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	file := files[0]
	// The `diff --git` line cannot be cut reliably when the name changed AND
	// holds a space. The rename headers are what settle it, and they are why
	// they overwrite what that line suggested.
	if file.Path != "new name.txt" || file.OldPath != "old name.txt" {
		t.Errorf("%q from %q, expected \"new name.txt\" from \"old name.txt\"", file.Path, file.OldPath)
	}

	hunk := file.Hunks[0]
	if hunk.OldLines != 1 || hunk.NewLines != 1 {
		t.Errorf("a range with no comma means one line, got %d and %d", hunk.OldLines, hunk.NewLines)
	}
}

func TestParseDiffPathWithSpacesAndNoRename(t *testing.T) {
	// The halves of the header are equal, so cutting in the middle is exact
	// whatever the name holds. Cutting on " b/" would have split this one
	// inside the filename.
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/my b/dir.txt b/my b/dir.txt",
		"index 4cb29ea..6addb9b 100644",
		"--- a/my b/dir.txt",
		"+++ b/my b/dir.txt",
		"@@ -1 +1 @@",
		"-x",
		"+y",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if files[0].Path != "my b/dir.txt" {
		t.Errorf("Path = %q, expected \"my b/dir.txt\"", files[0].Path)
	}
	if files[0].OldPath != "" {
		t.Errorf("OldPath = %q, expected empty: nothing was renamed", files[0].OldPath)
	}
}

func TestParseDiffBinaryFile(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/logo.png b/logo.png",
		"index 4cb29ea..6addb9b 100644",
		"Binary files a/logo.png and b/logo.png differ",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !files[0].Binary {
		t.Error("expected the file to be marked binary")
	}
	if len(files[0].Hunks) != 0 {
		t.Error("a binary file has no hunk")
	}
}

func TestParseDiffModeChangeWithNoContent(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/run.sh b/run.sh",
		"old mode 100644",
		"new mode 100755",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	file := files[0]
	if file.OldMode != "100644" || file.NewMode != "100755" {
		t.Errorf("modes = %q → %q", file.OldMode, file.NewMode)
	}
	// A change with no line in it is still a change. An interface that called
	// this empty would be telling the user their change does not exist.
	if file.Empty() {
		t.Error("a mode change is not an empty diff")
	}
}

func TestParseDiffNoNewlineMarkerAttachesToTheLineBefore(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/a.txt b/a.txt",
		"index 4cb29ea..6addb9b 100644",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"-before",
		"\\ No newline at end of file",
		"+after",
		"\\ No newline at end of file",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	lines := files[0].Hunks[0].Lines
	if len(lines) != 2 {
		t.Fatalf("the marker is not a line; expected 2 lines, got %d", len(lines))
	}
	if !lines[0].NoNewline || !lines[1].NoNewline {
		t.Error("both lines end without a newline")
	}
}

func TestParseDiffSeveralHunksNumberLinesAcrossAllOfThem(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
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
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files[0].Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(files[0].Hunks))
	}

	// The index is the address a selection uses, so it has to be unique
	// across the file rather than restart with each hunk.
	seen := map[int]bool{}
	for _, hunk := range files[0].Hunks {
		for _, line := range hunk.Lines {
			if seen[line.Index] {
				t.Errorf("index %d appears twice", line.Index)
			}
			seen[line.Index] = true
		}
	}
	if len(seen) != 6 {
		t.Errorf("expected 6 distinct indices, got %d", len(seen))
	}
}

func TestParseDiffSeveralFilesRestartTheirOwnNumbering(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/a.txt b/a.txt",
		"--- a/a.txt",
		"+++ b/a.txt",
		"@@ -1 +1 @@",
		"-a",
		"+A",
		"diff --git a/b.txt b/b.txt",
		"--- a/b.txt",
		"+++ b/b.txt",
		"@@ -1 +1 @@",
		"-b",
		"+B",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	for _, file := range files {
		if file.Hunks[0].Lines[0].Index != 0 {
			t.Errorf("%s: numbering must restart with each file", file.Path)
		}
	}
}

func TestParseDiffHunkHeading(t *testing.T) {
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/a.go b/a.go",
		"--- a/a.go",
		"+++ b/a.go",
		"@@ -10,3 +10,3 @@ func Example() {",
		" one",
		"-two",
		"+TWO",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := files[0].Hunks[0].Heading; got != "func Example() {" {
		t.Errorf("Heading = %q", got)
	}
}

func TestParseDiffEmptyContextLine(t *testing.T) {
	// git writes a blank context line as a single space. A diff that has been
	// through something that strips trailing whitespace arrives with the
	// space gone, and reading that as the end of the hunk would drop every
	// line after it without a word.
	files, err := git.ParseDiff([]byte(
		"diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n one\n\n-three\n+THREE\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	lines := files[0].Hunks[0].Lines
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	if lines[1].Kind != git.LineContext || lines[1].Text != "" {
		t.Errorf("line 1 = %q %q, expected an empty context line", lines[1].Kind, lines[1].Text)
	}
}

func TestParseDiffRefusesALineItCannotRead(t *testing.T) {
	cases := map[string][]byte{
		"body line with no marker": diffOutput(
			"diff --git a/a.txt b/a.txt", "--- a/a.txt", "+++ b/a.txt",
			"@@ -1 +1 @@", "no marker here"),
		"hunk header with one range": diffOutput(
			"diff --git a/a.txt b/a.txt", "@@ -1,3 @@"),
		"hunk range that is not a number": diffOutput(
			"diff --git a/a.txt b/a.txt", "@@ -x,3 +1,3 @@"),
		"file header with no second path": diffOutput("diff --git a/only.txt"),
	}

	for name, output := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := git.ParseDiff(output); err == nil {
				t.Error("expected an error rather than a plausible answer")
			}
		})
	}
}

func TestFingerprintChangesWithTheBytes(t *testing.T) {
	first := git.Fingerprint([]byte(modifiedDiff))
	again := git.Fingerprint([]byte(modifiedDiff))
	other := git.Fingerprint([]byte(modifiedDiff + " "))

	if first != again {
		t.Error("the same bytes must fingerprint the same")
	}
	if first == other {
		t.Error("different bytes must fingerprint differently: this is what makes a stale selection refusable")
	}
}

func TestParseDiffOfABinaryUnderADirectoryHoldingTheSeparator(t *testing.T) {
	// A binary file has no `---`/`+++` pair to correct a bad guess, and a
	// mode-only change has none either — which is exactly what the equal-
	// halves check above exists for. Comparing the halves whole never matched,
	// since they differ in their first byte by construction, so every header
	// fell through to cutting on the FIRST " b/" and this one came back as a
	// rename of `dir` into `x.png b/dir b/x.png`.
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/dir b/x.png b/dir b/x.png",
		"index 5e951e4..ecda2e8 100644",
		"Binary files a/dir b/x.png and b/dir b/x.png differ",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if files[0].Path != "dir b/x.png" {
		t.Errorf("Path = %q, expected \"dir b/x.png\"", files[0].Path)
	}
	if files[0].OldPath != "" {
		t.Errorf("OldPath = %q, expected empty: nothing was renamed", files[0].OldPath)
	}
	if !files[0].Binary {
		t.Error("the file was not reported as binary")
	}
}

func TestParseDiffUnquotesAPathGitCouldNotWritePlainly(t *testing.T) {
	// core.quotePath defaults to true, so every byte above ASCII arrives as
	// three octal digits behind a backslash, inside quotes. Without reading
	// that back, one accented filename makes the whole diff unreadable — and
	// the whole commit with it, since one header stops the parse.
	files, err := git.ParseDiff(diffOutput(
		`diff --git "a/caf\303\251.txt" "b/caf\303\251.txt"`,
		"index 814f4a4..879de50 100644",
		`--- "a/caf\303\251.txt"`,
		`+++ "b/caf\303\251.txt"`,
		"@@ -1 +1 @@",
		"-un",
		"+deux",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if files[0].Path != "café.txt" {
		t.Errorf("Path = %q, expected %q", files[0].Path, "café.txt")
	}
	if files[0].OldPath != "" {
		t.Errorf("OldPath = %q, expected empty: nothing was renamed", files[0].OldPath)
	}
}

func TestParseDiffUnquotesARenameWithOneQuotedHalf(t *testing.T) {
	// git quotes each name on its own, so a rename from a plain name to an
	// accented one has one quoted half and one that is not — and the space
	// between them is the only separator.
	files, err := git.ParseDiff(diffOutput(
		`diff --git a/plain.txt "b/caf\303\251.txt"`,
		"similarity index 100%",
		"rename from plain.txt",
		`rename to "caf\303\251.txt"`,
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if files[0].Path != "café.txt" || files[0].OldPath != "plain.txt" {
		t.Errorf("%q from %q, expected %q from \"plain.txt\"",
			files[0].Path, files[0].OldPath, "café.txt")
	}
}

func TestParseDiffUnquotesTheEscapesGitWrites(t *testing.T) {
	// A tab, a quote and a backslash are quoted whatever core.quotePath is
	// set to, since none of them can be written plainly on a header line.
	files, err := git.ParseDiff(diffOutput(
		`diff --git "a/od\td\"\\name.txt" "b/od\td\"\\name.txt"`,
		"index 814f4a4..879de50 100644",
		`--- "a/od\td\"\\name.txt"`,
		`+++ "b/od\td\"\\name.txt"`,
		"@@ -1 +1 @@",
		"-x",
		"+y",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := "od\td\"\\name.txt"; files[0].Path != want {
		t.Errorf("Path = %q, expected %q", files[0].Path, want)
	}
}

func TestParseDiffStripsTheTabGitAddsAfterASpacedName(t *testing.T) {
	// git terminates the name on a `---`/`+++` line with a TAB when it holds
	// a space, so a traditional patch reader can find where it ends. Kept, the
	// payload names a path the status list never mentioned — and on a deletion
	// it survives on the old side alone, which reads as a rename of the file
	// into itself.
	files, err := git.ParseDiff(diffOutput(
		"diff --git a/my file.txt b/my file.txt",
		"deleted file mode 100644",
		"index 814f4a4..0000000",
		"--- a/my file.txt\t",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-gone",
	))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if files[0].Path != "my file.txt" {
		t.Errorf("Path = %q, expected \"my file.txt\"", files[0].Path)
	}
	if files[0].OldPath != "" {
		t.Errorf("OldPath = %q, expected empty: a deleted file was not renamed", files[0].OldPath)
	}
	if !files[0].Removed {
		t.Error("the deletion was not reported as one")
	}
}

func TestParseDiffRefusesAPathItCannotUnquote(t *testing.T) {
	// A name that cannot be read back is not a name to guess at: it would be
	// shown as a path nothing on disk answers to, and refused by the patch
	// builder with a sentence about a rename nobody performed.
	_, err := git.ParseDiff(diffOutput(
		`diff --git "a/bad\q.txt" "b/bad\q.txt"`,
		"index 814f4a4..879de50 100644",
	))
	if err == nil {
		t.Fatal("expected a refusal for an unknown escape")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("err = %v; the message must say what could not be read", err)
	}
}
