package edit

import "strings"

// Whether a file still carries the markers git left in it.
//
// Recognised, not parsed. The daemon has one question about a conflicted file
// — is it finished — and it is asked at one moment: `git add` on a file full
// of `<<<<<<<` succeeds, and git commits it without a word. Choosing between
// the two sides is the browser's job, on a buffer that changes per keystroke,
// and web/src/app/conflict.ts is where that lives; this is the same rule
// written once more on the side that can enforce it.

// The three markers, each seven identical characters.
const (
	markerOurs   = "<<<<<<<"
	markerBase   = "|||||||"
	markerTheirs = ">>>>>>>"
)

// HasConflictMarkers says the text still holds a marker, complete region or
// not.
//
// Half a region counts. Somebody who deleted one end by hand has a file that
// no longer offers a side to take and still commits a `<<<<<<<` — which is the
// case a check for whole regions passes and this one does not.
//
// `=======` is deliberately not among the markers. It is how Markdown
// underlines a heading, so every README with a setext title would be reported
// as unresolved; the other three are seven punctuation characters that occur
// in real text essentially never. Refusing to guess costs the one file whose
// user deleted both ends and left the middle, and buys never crying wolf about
// a document.
func HasConflictMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if isMarker(line, markerOurs) || isMarker(line, markerBase) || isMarker(line, markerTheirs) {
			return true
		}
	}
	return false
}

// isMarker: the seven characters, then the end of the line or a space and
// git's label for the side.
//
// Exact rather than a prefix test, because `>>>>>>>>` is a line of arrows in
// somebody's ASCII diagram and `<<<<<<<foo` is not a marker git writes.
func isMarker(line, marker string) bool {
	rest, found := strings.CutPrefix(line, marker)
	if !found {
		return false
	}
	return rest == "" || strings.HasPrefix(rest, " ")
}
