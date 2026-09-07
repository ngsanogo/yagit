package api

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ngsanogo/yagit/internal/git"
)

func diffWithLine(text string) git.FileDiff {
	return git.FileDiff{
		ID:   "fingerprint",
		Path: "dist/bundle.min.js",
		Hunks: []git.Hunk{{
			Lines: []git.DiffLine{
				{Kind: git.LineContext, Text: "unchanged", Index: 0},
				{Kind: git.LineAdded, Text: text, Index: 1},
			},
		}},
	}
}

func TestBoundedForDisplayCutsALineThatNobodyCanRead(t *testing.T) {
	long := strings.Repeat("x", maxDisplayedLineRunes+500)

	bounded := boundedForDisplay(diffWithLine(long))
	line := bounded.Hunks[0].Lines[1]

	if got := utf8.RuneCountInString(line.Text); got != maxDisplayedLineRunes {
		t.Errorf("kept %d runes, want %d", got, maxDisplayedLineRunes)
	}
	if line.Truncated != 500 {
		t.Errorf("Truncated = %d, want 500: the view has to be able to say how much is missing", line.Truncated)
	}
	if other := bounded.Hunks[0].Lines[0]; other.Truncated != 0 || other.Text != "unchanged" {
		t.Errorf("an ordinary line was altered: %+v", other)
	}
}

// The property everything else depends on.
//
// The FileDiff handed in is the one a patch is built from when somebody stages
// three lines out of a hunk. Mutating it here would write the truncation into
// the user's file — silently, and only for files whose lines are long, which
// are exactly the files nobody reads closely enough to notice.
func TestBoundedForDisplayLeavesTheCallersDiffWhole(t *testing.T) {
	long := strings.Repeat("y", maxDisplayedLineRunes+42)
	original := diffWithLine(long)

	_ = boundedForDisplay(original)

	line := original.Hunks[0].Lines[1]
	if line.Text != long {
		t.Errorf("the caller's line was shortened to %d runes: a patch built from it would truncate the file",
			utf8.RuneCountInString(line.Text))
	}
	if line.Truncated != 0 {
		t.Errorf("the caller's line was flagged as truncated, and it is not")
	}
}

func TestBoundedForDisplayCutsOnARuneBoundary(t *testing.T) {
	// A three-byte rune, repeated past the limit: cutting by bytes would leave
	// a dangling fragment that reaches the browser as a replacement character
	// in the middle of a word.
	long := strings.Repeat("字", maxDisplayedLineRunes+10)

	line := boundedForDisplay(diffWithLine(long)).Hunks[0].Lines[1]

	if !utf8.ValidString(line.Text) {
		t.Fatal("the cut landed inside a rune")
	}
	if got := utf8.RuneCountInString(line.Text); got != maxDisplayedLineRunes {
		t.Errorf("kept %d runes, want %d", got, maxDisplayedLineRunes)
	}
	if line.Truncated != 10 {
		t.Errorf("Truncated = %d, want 10 — it counts runes, not bytes", line.Truncated)
	}
}

func TestBoundedForDisplayLeavesAnOrdinaryDiffAlone(t *testing.T) {
	original := diffWithLine("const answer = 42;")
	bounded := boundedForDisplay(original)

	if bounded.Hunks[0].Lines[1].Text != "const answer = 42;" {
		t.Error("a short line was altered")
	}
	if bounded.Hunks[0].Lines[1].Truncated != 0 {
		t.Error("a short line was flagged as truncated")
	}
	if bounded.ID != original.ID {
		t.Error("the fingerprint changed, and the client sends it back to prove the diff has not moved")
	}
}
