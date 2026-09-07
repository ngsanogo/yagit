package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrNothingSelected: the selection picks out no added or removed line, so
// there is no patch to build. Refused rather than answered with an empty
// patch, which `git apply` would reject with a message about a corrupt one.
var ErrNothingSelected = errors.New("the selection holds no added or removed line")

// ErrNotLineAddressable: the change has no lines to choose between, so it can
// only be staged or unstaged whole. A binary file and a rename are both this:
// one has no text, the other has no content change at all.
var ErrNotLineAddressable = errors.New("this change cannot be taken line by line")

// PatchDirection says which side of the diff the patch will be applied
// against, which is the one thing that decides how an UNSELECTED line is
// written.
//
// It is not a formatting preference. A patch built for the wrong direction
// applies cleanly about half the time and produces a file the user never
// asked for, which is the worst failure this code could have.
type PatchDirection string

const (
	// PatchForward is applied as written, onto the diff's OLD side, and
	// produces "old plus the selected changes". Staging is this: the index
	// holds the old side.
	PatchForward PatchDirection = "forward"

	// PatchReverse is applied with `git apply --reverse`, onto the diff's NEW
	// side, and produces "new minus the selected changes". Unstaging and
	// discarding are both this: the index and the work tree respectively hold
	// the new side.
	PatchReverse PatchDirection = "reverse"
)

// FormatPatch builds a unified diff holding only the selected lines of a
// file's diff.
//
// This is what makes staging part of a file possible. `git add` takes whole
// paths, so anything finer has to be expressed as a patch and handed to `git
// apply`.
//
// selected holds DiffLine.Index values. Context lines in it are ignored:
// context is not a change, and including or excluding it is not a choice
// anyone can make.
//
// The rules for a line that is NOT selected are the whole of the difficulty,
// and they are mirror images of each other. In both cases the question is the
// same — is this line in the base file, and should it be in the result? —
// and the base file is what the direction names.
//
// Applying forward, onto the old side:
//
//   - An unselected ADDITION is not in the base and must not be in the
//     result. It is dropped: as far as this patch is concerned it never
//     happened.
//   - An unselected REMOVAL is in the base and must stay in the result. It
//     becomes a context line.
//
// Applying in reverse, onto the new side, both answers flip:
//
//   - An unselected ADDITION is in the base and must stay in the result — the
//     user chose not to take it back. It becomes a context line.
//   - An unselected REMOVAL is already gone from the base and must stay gone.
//     It is dropped.
//
// Reversing a forward patch does NOT give the reverse patch. The context
// lines differ, and `git apply` matches on context: a forward patch reversed
// carries the old side's version of every line the user left alone, and the
// file it is applied to has the new side's. That mistake fails loudly the
// first time a hunk holds two changes, and silently the rest of the time.
func FormatPatch(file FileDiff, selected map[int]bool, direction PatchDirection) ([]byte, error) {
	if file.Binary {
		return nil, fmt.Errorf("%w: %s is binary, so stage or unstage the whole file",
			ErrNotLineAddressable, file.Path)
	}
	if file.OldPath != "" {
		// A rename is a change to the index that has no lines in it. A patch
		// carrying half of one would either lose the rename or lose the
		// content, and picking which is a guess about what the user meant.
		return nil, fmt.Errorf(
			"%w: %s was renamed from %s, so stage or unstage the whole file",
			ErrNotLineAddressable, file.Path, file.OldPath)
	}

	var body strings.Builder

	// delta is how far the result has drifted from the base across the hunks
	// written so far. It is what makes each hunk header describe the file
	// this patch actually produces, rather than the one git's diff described.
	delta := 0
	totalOld, totalNew := 0, 0
	wrote := false

	for _, hunk := range file.Hunks {
		lines, baseCount, resultCount, changed := selectHunkLines(hunk, selected, direction)
		if !changed {
			// A hunk reduced to context changes nothing. Writing it would be
			// harmless and pointless, and `git apply` would still have to
			// match its context against the file.
			continue
		}

		baseStart := hunk.OldStart
		if direction == PatchReverse {
			baseStart = hunk.NewStart
		}
		resultStart := startOfResult(baseStart, baseCount, resultCount, delta)

		oldStart, oldCount, newStart, newCount := baseStart, baseCount, resultStart, resultCount
		if direction == PatchReverse {
			// The patch is written old-to-new whichever way it will be
			// applied: `git apply --reverse` reads a normal patch backwards.
			// So under reversal the base is the NEW side of what is written.
			oldStart, oldCount, newStart, newCount = resultStart, resultCount, baseStart, baseCount
		}

		body.WriteString(formatHunkHeader(oldStart, oldCount, newStart, newCount))
		body.WriteString(lines)

		delta += resultCount - baseCount
		totalOld += oldCount
		totalNew += newCount
		wrote = true
	}

	if !wrote {
		return nil, ErrNothingSelected
	}

	return []byte(formatPatchHeader(file, totalOld, totalNew) + body.String()), nil
}

// noNewlineMarker is git's way of saying a file has no trailing newline. It
// describes the line BEFORE it, on the sides that line belongs to.
const noNewlineMarker = "\\ No newline at end of file\n"

// bodyLine is one line of a patch body before the no-newline markers are
// placed. Building the body first is what makes placing them possible: the
// marker says the file ENDS here, and whether it still does depends on every
// line that comes after.
type bodyLine struct {
	// marker is the line's leading character in the patch being written: '-'
	// for the old side, '+' for the new side, ' ' for both. That orientation
	// is the same whichever way the patch will be applied — `git apply
	// --reverse` reads a normal patch backwards.
	marker byte
	text   string

	// endsOld and endsNew say the file ends at this line, without a trailing
	// newline, on the old and on the new side. They come apart because git's
	// marker does: on a removed line it speaks for the old side alone, and a
	// removal that becomes a context line does not thereby acquire the right
	// to speak for the new one.
	endsOld bool
	endsNew bool
}

// selectHunkLines writes one hunk's body under the selection, and counts what
// each side ends up holding.
//
// baseCount always comes out equal to the hunk's own count on that side —
// every context line and every change on the base side is written — which is
// what makes the base's line numbers usable unchanged.
func selectHunkLines(
	hunk Hunk, selected map[int]bool, direction PatchDirection,
) (body string, baseCount, resultCount int, changed bool) {
	// keptKind is the kind of line that, when NOT selected, survives as
	// context; the other kind is dropped. That single value is the whole
	// difference between the two directions.
	keptKind := LineRemoved
	if direction == PatchReverse {
		keptKind = LineAdded
	}

	lines := make([]bodyLine, 0, len(hunk.Lines))
	for _, line := range hunk.Lines {
		switch {
		case line.Kind == LineContext:
			lines = append(lines, bodyLine{
				marker: ' ', text: line.Text,
				endsOld: line.NoNewline, endsNew: line.NoNewline,
			})

		case selected[line.Index]:
			// A selected change is written as it stands, and it exists on
			// exactly one side: an addition on the new side, a removal on the
			// old one.
			if line.Kind == LineAdded {
				lines = append(lines, bodyLine{marker: '+', text: line.Text, endsNew: line.NoNewline})
			} else {
				lines = append(lines, bodyLine{marker: '-', text: line.Text, endsOld: line.NoNewline})
			}
			changed = true

		case line.Kind == keptKind:
			// Present in the base and staying in the result: context, still
			// speaking for the one side its marker came from.
			lines = append(lines, bodyLine{
				marker:  ' ',
				text:    line.Text,
				endsOld: line.NoNewline && line.Kind == LineRemoved,
				endsNew: line.NoNewline && line.Kind == LineAdded,
			})

		default:
			// Absent from the base and staying absent: dropped entirely, and
			// its marker with it. A line that is not in the file cannot be
			// the line the file ends on.
		}
	}

	// The last line on each side is the only one a marker can truthfully
	// follow. A line that used to end the file and no longer does gained a
	// newline, which is exactly what dropping its marker says.
	lastOld, lastNew := -1, -1
	for index, line := range lines {
		if line.marker != '+' {
			lastOld = index
		}
		if line.marker != '-' {
			lastNew = index
		}
	}

	var out strings.Builder
	write := func(marker byte, text string, noNewline bool) {
		out.WriteByte(marker)
		out.WriteString(text)
		out.WriteByte('\n')
		if noNewline {
			out.WriteString(noNewlineMarker)
		}
		// Which of the two written sides is the base is the whole of what the
		// direction decides; reversal makes the written new side the base.
		onOld, onNew := marker != '+', marker != '-'
		if direction == PatchReverse {
			onOld, onNew = onNew, onOld
		}
		if onOld {
			baseCount++
		}
		if onNew {
			resultCount++
		}
	}

	for index, line := range lines {
		endsOld, endsNew := index >= lastOld, index >= lastNew

		// A context line whose two sides disagree about the trailing newline
		// cannot stay one line: the marker after a context line speaks for
		// both. Written as a removal and an addition of the same text it says
		// what actually happened — the line gained a newline because the file
		// no longer ends there. That is what `git add -p` writes, and without
		// it `git apply` accepts a patch that welds two lines into one.
		if line.marker == ' ' &&
			((line.endsOld && endsOld && !endsNew) || (line.endsNew && endsNew && !endsOld)) {
			write('-', line.text, line.endsOld && endsOld)
			write('+', line.text, line.endsNew && endsNew)
			continue
		}

		write(line.marker, line.text,
			(line.endsOld && endsOld && line.marker != '+') ||
				(line.endsNew && endsNew && line.marker != '-'))
	}

	return out.String(), baseCount, resultCount, changed
}

// startOfResult is where a hunk begins in the file the patch produces.
//
// Unified diff numbers a range by its first line — except an EMPTY range,
// which is numbered by the line before it, and is zero when there is nothing
// before it. Both cases occur here: a hunk that only adds has an empty range
// on one side, and one where every change was left out has an empty range on
// the other.
func startOfResult(baseStart, baseCount, resultCount, delta int) int {
	switch {
	case baseCount == 0:
		// git numbered the base side by the line the insertion follows, so
		// the first line of the result is the one after it.
		return baseStart + delta + 1
	case resultCount == 0:
		return baseStart + delta - 1
	default:
		return baseStart + delta
	}
}

func formatHunkHeader(oldStart, oldCount, newStart, newCount int) string {
	return "@@ -" + formatRange(oldStart, oldCount) +
		" +" + formatRange(newStart, newCount) + " @@\n"
}

// formatRange writes `start,count`, or `start` alone when the count is one.
//
// The short form is not decoration: it is what git writes, and a patch that
// round-trips through this code comes back byte for byte the same when every
// line is selected. That property is what the tests check.
func formatRange(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}

// formatPatchHeader writes the two file lines `git apply` reads the paths
// from.
//
// /dev/null is how a unified diff says a file does not exist on one side, and
// it takes BOTH of the conditions below to be true:
//
//   - git said the file is absent on that side of the diff — Added or Removed.
//   - the patch leaves no line there.
//
// Neither is enough alone, and the second one on its own is the trap. A hunk
// carrying only removals has an empty new side and no context — git writes
// `@@ -4 +3,0 @@` for one — while the file around it is untouched and very
// much still there. A patch made of such hunks would be written as a deletion
// and would delete the file.
//
// The first is not enough either: staging two lines of a new file leaves a
// file with two lines in it, which is a creation. Staging two lines of a
// deletion leaves a file with the rest of them, which is not a deletion at
// all.
//
// Getting this wrong does not fail loudly: `--- a/path` on a file that does
// not exist makes `git apply` look for content it will not find, and the
// error talks about the patch rather than about the file.
func formatPatchHeader(file FileDiff, totalOld, totalNew int) string {
	from := "a/" + file.Path
	if file.Added && totalOld == 0 {
		from = "/dev/null"
	}
	to := "b/" + file.Path
	if file.Removed && totalNew == 0 {
		to = "/dev/null"
	}

	var header strings.Builder

	// The mode of a file being created reaches git through `new file mode`,
	// and git reads that line only under a `diff --git` preamble. Left out,
	// `git apply` creates the index entry with its own default of 100644: an
	// executable script staged line by line stops being executable, silently,
	// because the work tree keeps the bit and the interface shows a mode
	// change only once the content stops differing. Staging the file whole
	// goes through `git add` and keeps it, so the loss is on this path alone.
	//
	// Only when the mode is not the default, so that the ordinary patch stays
	// the one shape the round-trip tests pin.
	if from == "/dev/null" && file.NewMode != "" && file.NewMode != defaultFileMode {
		header.WriteString("diff --git a/" + file.Path + " b/" + file.Path + "\n")
		header.WriteString("new file mode " + file.NewMode + "\n")
	}

	header.WriteString("--- " + from + "\n+++ " + to + "\n")
	return header.String()
}

// defaultFileMode is what `git apply` gives an index entry it creates without
// being told otherwise.
const defaultFileMode = "100644"

// ChangedLines is every index a selection could name: the added and removed
// lines of a diff, context excluded.
//
// It is what lets a caller tell "part of this file" from "all of it". The two
// are different operations — one is a patch, the other is `git add` — and the
// difference has to be decided somewhere rather than guessed at by whoever
// looks at the selection last.
func ChangedLines(file FileDiff) map[int]bool {
	indices := make(map[int]bool)
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind != LineContext {
				indices[line.Index] = true
			}
		}
	}
	return indices
}

// CoversEveryChange says the selection names every added and removed line the
// diff holds.
func CoversEveryChange(file FileDiff, selected map[int]bool) bool {
	changed := ChangedLines(file)
	if len(changed) == 0 {
		return false
	}
	for index := range changed {
		if !selected[index] {
			return false
		}
	}
	return true
}
