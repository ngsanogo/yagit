package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The working directory adds two more parsers and one generator, and all
// three are places where a bug is silent.
//
// The parsers are the same shape as the ones beside them: a byte stream cut
// into records and records into fields. The generator is worse. A patch with a
// miscounted range does not crash — `git apply` takes it and stages a file
// nobody asked for — so what is asserted here is not that FormatPatch runs,
// but that what it produces still describes the same change.

// FuzzParseStatusPathsSurviveTheTrip: whatever git puts in a path comes back
// out of the parser byte for byte.
//
// The path is the last field of every record and holds anything a filesystem
// allows — spaces, quotes, a leading dash, a name that looks like another
// status record. -z is what makes that safe, and this is the test that says
// so: without it git would quote and escape, and the interface would offer to
// stage a path that is not the path.
func FuzzParseStatusPathsSurviveTheTrip(f *testing.F) {
	f.Add("a.txt", ".M")
	f.Add("my documents/notes and things.md", "M.")
	f.Add("-leading-dash.txt", "A.")
	f.Add("1 .M N... 100644 100644 100644 aa bb looks-like-a-record", ".D")
	f.Add("# branch.head not-a-header", "MM")
	f.Add("☃/🌳.txt", ".M")

	f.Fuzz(func(t *testing.T, path, code string) {
		if !gitCanEmit(path) || path == "" {
			t.Skip("git cannot emit a NUL or a newline in a path, and never an empty one")
		}
		// The two codes come from a fixed alphabet; anything else is not
		// output git produces, and the parser refusing it is the point of
		// TestParseStatusRefusesRecordsItCannotRead.
		if len(code) != 2 || strings.Trim(code, ".MADRCUT") != "" {
			t.Skip("not a status code git writes")
		}

		const blob = "4cb29ea38f70d7c61b2a3a25b02e3bdf44905402"
		output := statusOutput(
			"1 " + code + " N... 100644 100644 100644 " + blob + " " + blob + " " + path)

		status, err := git.ParseStatus(output)
		if err != nil {
			t.Fatalf("a record git could produce failed to parse: %v", err)
		}
		if len(status.Files) != 1 {
			t.Fatalf("one record parsed as %d files — the framing leaked", len(status.Files))
		}
		if got := status.Files[0].Path; got != path {
			t.Errorf("path = %q, put in %q", got, path)
		}
	})
}

// FuzzParseStatusNeverPanics feeds it what git would never send.
func FuzzParseStatusNeverPanics(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("\x00"))
	f.Add([]byte("\x00\x00\x00"))
	f.Add([]byte("# branch.ab\x00"))
	f.Add([]byte("# branch.ab +\x00"))
	f.Add([]byte("2 R. N... 1 1 1 a b R100 only-one-record\x00"))
	f.Add([]byte("u UU N... 1 1 1 1 a b c\x00"))
	f.Add([]byte("? \x00"))
	f.Add([]byte("\xff\xfe not utf-8 \x00"))

	f.Fuzz(func(t *testing.T, output []byte) {
		status, err := git.ParseStatus(output)
		if err != nil && status.Files != nil {
			t.Errorf("returned %d files alongside an error: %v", len(status.Files), err)
		}
	})
}

// FuzzParseDiffLineTextSurvivesTheTrip: a line of a file comes back as
// itself, marker stripped and nothing else touched.
//
// The seeds are the lines that look like diff syntax, because a line of a
// FILE can be anything — including "@@ -1 +1 @@" or "diff --git a/x b/x".
// Those cannot be told apart from framing by looking at them, which is why
// the parser reads a hunk's body by its declared length rather than by
// hunting for the next marker.
func FuzzParseDiffLineTextSurvivesTheTrip(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Add("@@ -1 +1 @@")
	f.Add("diff --git a/x b/x")
	f.Add("--- a/x")
	f.Add("\\ No newline at end of file")
	f.Add("  leading and trailing  ")
	f.Add("🌳 and \"quotes\" and $vars")

	f.Fuzz(func(t *testing.T, text string) {
		if strings.ContainsAny(text, "\n\x00") {
			t.Skip("a line of a unified diff cannot hold a newline, and git refuses NUL")
		}

		files, err := git.ParseDiff(diffOutput(
			"diff --git a/a.txt b/a.txt",
			"--- a/a.txt",
			"+++ b/a.txt",
			"@@ -1 +1 @@",
			"-"+text,
			"+"+text,
		))
		if err != nil {
			t.Fatalf("a diff git could produce failed to parse: %v", err)
		}
		if len(files) != 1 || len(files[0].Hunks) != 1 {
			t.Fatalf("the framing leaked: %+v", files)
		}

		lines := files[0].Hunks[0].Lines
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d", len(lines))
		}
		for _, line := range lines {
			if line.Text != text {
				t.Errorf("text = %q, put in %q", line.Text, text)
			}
		}
	})
}

// FuzzParseDiffNeverPanics: same contract as the parsers beside it.
func FuzzParseDiffNeverPanics(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte("diff --git"))
	f.Add([]byte("diff --git a/x b/x\n@@"))
	f.Add([]byte("diff --git a/x b/x\n@@ -0 +0 @@\n"))
	f.Add([]byte("diff --git a/x b/x\n@@ -a,b +c,d @@\n"))
	f.Add([]byte("@@ -1 +1 @@\n no file header\n"))
	f.Add([]byte("diff --git a/x b/x\n@@ -1 +1 @@\n\\ marker with no line before it\n"))
	f.Add([]byte("\xff\xfe not utf-8"))

	f.Fuzz(func(t *testing.T, output []byte) {
		files, err := git.ParseDiff(output)
		if err != nil && files != nil {
			t.Errorf("returned %d files alongside an error: %v", len(files), err)
		}
	})
}

// FuzzFormatPatchIsStillTheSameChange is the property the patch builder lives
// or dies by.
//
// Take a diff, choose every line, build the patch, and read it back: the
// result has to describe exactly the change the diff described. Any drift —
// a range off by one, a lost "\ No newline", a hunk quietly skipped — shows
// up as a different set of lines coming back out.
//
// This is stronger than comparing bytes with git's output, because it holds
// for hunks a hand-written test would not think to write.
func FuzzFormatPatchIsStillTheSameChange(f *testing.F) {
	f.Add(3, "-+ +-", 1)
	f.Add(1, "+++", 1)
	f.Add(1, "---", 1)
	f.Add(2, " - + ", 10)
	f.Add(1, "+", 0)
	f.Add(5, "-", 4)

	f.Fuzz(func(t *testing.T, hunkCount int, shape string, firstStart int) {
		file, ok := syntheticDiff(hunkCount, shape, firstStart)
		if !ok {
			t.Skip("not a diff git could produce")
		}

		selection := git.ChangedLines(file)
		if len(selection) == 0 {
			t.Skip("a diff with no change has no patch")
		}

		for _, direction := range []git.PatchDirection{git.PatchForward, git.PatchReverse} {
			patch, err := git.FormatPatch(file, selection, direction)
			if err != nil {
				t.Fatalf("%s: format: %v", direction, err)
			}

			// The patch is a unified diff, so it must parse as one. A patch
			// this package produces and cannot read back is one `git apply`
			// has no reason to accept either.
			reparsed, err := git.ParseDiff(patch)
			if err != nil {
				t.Fatalf("%s: the patch does not parse:\n%s\n%v", direction, patch, err)
			}
			if len(reparsed) != 1 {
				t.Fatalf("%s: patch describes %d files:\n%s", direction, len(reparsed), patch)
			}

			if before, after := changeText(file), changeText(reparsed[0]); before != after {
				t.Errorf("%s: the change was altered\nbefore: %q\nafter:  %q\npatch:\n%s",
					direction, before, after, patch)
			}
		}
	})
}

// syntheticDiff builds a FileDiff out of a shape string: one character per
// line, ' ' for context, '-' for a removal, '+' for an addition. The same
// shape is used for every hunk.
//
// It goes through ParseDiff rather than being built by hand, so the structure
// under test is one the parser really produces — line indices, line numbers
// and hunk counts included.
func syntheticDiff(hunkCount int, shape string, firstStart int) (git.FileDiff, bool) {
	if hunkCount < 1 || hunkCount > 8 || shape == "" || len(shape) > 24 {
		return git.FileDiff{}, false
	}
	if firstStart < 0 || firstStart > 1000 {
		return git.FileDiff{}, false
	}

	lines := []string{"diff --git a/a.txt b/a.txt", "--- a/a.txt", "+++ b/a.txt"}

	oldLine, newLine := max(firstStart, 1), max(firstStart, 1)
	for hunk := range hunkCount {
		var oldCount, newCount int
		var body []string

		for index, marker := range shape {
			text := "line-" + string(rune('a'+index%26))
			switch marker {
			case ' ':
				body = append(body, " "+text)
				oldCount++
				newCount++
			case '-':
				body = append(body, "-"+text)
				oldCount++
			case '+':
				body = append(body, "+"+text)
				newCount++
			default:
				return git.FileDiff{}, false
			}
		}
		if oldCount == 0 && newCount == 0 {
			return git.FileDiff{}, false
		}

		// git numbers an empty range by the line before it.
		oldStart, newStart := oldLine, newLine
		if oldCount == 0 {
			oldStart = oldLine - 1
		}
		if newCount == 0 {
			newStart = newLine - 1
		}

		lines = append(lines, formatSyntheticHunkHeader(oldStart, oldCount, newStart, newCount))
		lines = append(lines, body...)

		// Hunks never touch: git separates them by more than twice the
		// context it carries, or it would have merged them into one.
		_ = hunk
		oldLine += oldCount + 16
		newLine += newCount + 16
	}

	files, err := git.ParseDiff(diffOutput(lines...))
	if err != nil || len(files) != 1 {
		return git.FileDiff{}, false
	}
	return files[0], true
}

func formatSyntheticHunkHeader(oldStart, oldCount, newStart, newCount int) string {
	return "@@ -" + syntheticRange(oldStart, oldCount) +
		" +" + syntheticRange(newStart, newCount) + " @@"
}

func syntheticRange(start, count int) string {
	if count == 1 {
		return itoa(start)
	}
	return itoa(start) + "," + itoa(count)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// changeText is a diff's change with its geometry thrown away: the added and
// removed lines, in order, marker included.
//
// That is what has to survive FormatPatch. The line NUMBERS legitimately move
// — a patch holding part of a change describes a different file — so
// comparing them would fail on correct output.
func changeText(file git.FileDiff) string {
	var builder strings.Builder
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case git.LineAdded:
				builder.WriteString("+" + line.Text + "\n")
			case git.LineRemoved:
				builder.WriteString("-" + line.Text + "\n")
			case git.LineContext:
				// Context is not the change. It moves between the two
				// directions by design — an unselected line becomes context
				// in one of them — so including it here would make the
				// property false for correct output.
			}
		}
	}
	return builder.String()
}
