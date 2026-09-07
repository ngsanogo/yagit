package edit_test

import (
	"testing"

	"github.com/ngsanogo/yagit/internal/edit"
)

// What the daemon has to recognise before it lets `git add` end a conflict.
//
// The interesting half is what is NOT a marker: `=======` under a line of text
// is how Markdown underlines a heading, and a rule that counted it would refuse
// to stage every README with a setext title.
func TestHasConflictMarkers(t *testing.T) {
	for _, testCase := range []struct {
		name string
		text string
		want bool
	}{
		{"a whole region", "a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> feature\nb\n", true},
		{"a marker with no label", "<<<<<<<\nours\n=======\ntheirs\n>>>>>>>\n", true},
		{"the base marker diff3 writes", "|||||||| \n", false},
		{"the base marker, exactly seven", "||||||| 8f3a1c2\n", true},
		{"half a region somebody edited by hand", "a\n<<<<<<< HEAD\nours\nb\n", true},
		{"a closing marker on its own", "a\n>>>>>>> feature\n", true},

		{"a resolved file", "one\nSIDE\nthree\n", false},
		{"a setext heading", "Title\n=======\n\nbody\n", false},
		{"a rule of arrows", ">>>>>>>>\n<<<<<<<<\n", false},
		{"a marker not at the start of a line", "see <<<<<<< HEAD in the docs\n", false},
		{"nothing at all", "", false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := edit.HasConflictMarkers(testCase.text); got != testCase.want {
				t.Errorf("HasConflictMarkers(%q) = %v, want %v", testCase.text, got, testCase.want)
			}
		})
	}
}
