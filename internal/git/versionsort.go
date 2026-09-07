package git

import (
	"slices"
	"strings"
)

// The state machine below is git's versioncmp(), which is glibc's
// strverscmp(). Four states: comparing ordinary text, comparing an integral
// number, comparing a fractional one, and comparing one that so far is
// nothing but leading zeros.
//
// The fractional states are the reason this is a machine and not a loop that
// reads digit runs as integers. git treats a run that starts with a zero as
// the digits after a decimal point: v1.01 sorts *before* v1.1, and v1.09
// *before* v1.0. Reading both runs as numbers makes the first pair equal and
// the second pair backwards, and an order of our own that disagreed with git
// about them would be exactly the bug this file exists to prevent — the same
// tags in two different orders on one screen.
const (
	verNormal   = 0
	verIntegral = 3
	verFraction = 6
	verZeros    = 9

	// Result codes, in the range the ±1 answers below do not occupy.
	verCompare = 2
	verLength  = 3
)

// verNextState is indexed by the current state plus the class of the character
// just read — see digitClass. The three columns are that class in order.
var verNextState = [12]int{
	/* text     */ verNormal, verIntegral, verZeros,
	/* integral */ verNormal, verIntegral, verIntegral,
	/* fraction */ verNormal, verFraction, verFraction,
	/* zeros    */ verNormal, verFraction, verZeros,
}

// verResultType is indexed by (state + class of a's character) * 3 + class of
// b's character. Nine columns per state: the pairs x/x x/d x/0 d/x d/d d/0
// 0/x 0/d 0/0, where x is not a digit, d is 1-9 and 0 is a zero.
var verResultType = [36]int{
	/* text */
	verCompare, verCompare, verCompare,
	verCompare, verLength, verCompare,
	verCompare, verCompare, verCompare,
	/* integral */
	verCompare, -1, -1,
	1, verLength, verLength,
	1, verLength, verLength,
	/* fraction */
	verCompare, verCompare, verCompare,
	verCompare, verCompare, verCompare,
	verCompare, verCompare, verCompare,
	/* zeros */
	verCompare, 1, 1,
	-1, verCompare, verCompare,
	-1, verCompare, verCompare,
}

// versionRefnameCompare orders two ref names the way git's version:refname
// sort does: digit runs as numbers, runs with a leading zero as fractions,
// everything else byte by byte.
//
// Negative means a belongs before b. The sidebar and the decoration badges
// both need this — one list comes from for-each-ref, the other from %D — and
// only a compare that answers what git's own would keeps the two surfaces
// showing one order.
//
// A port of git's versioncmp() rather than something equivalent-looking,
// because the corner it gets right is the corner a rewrite gets wrong. The one
// thing left out is versionsort.suffix: a repository that configures it orders
// its prereleases differently in git than here, and the badges would follow
// the sidebar rather than the config.
func versionRefnameCompare(a, b string) int {
	if a == b {
		return 0
	}

	ia, ib := 0, 0
	c1, c2 := byteAt(a, ia), byteAt(b, ib)
	ia++
	ib++

	state := verNormal + digitClass(c1)

	diff := int(c1) - int(c2)
	for diff == 0 {
		// Both strings ended together, every byte equal.
		if c1 == 0 {
			return 0
		}

		state = verNextState[state]
		c1, c2 = byteAt(a, ia), byteAt(b, ib)
		ia++
		ib++
		state += digitClass(c1)
		diff = int(c1) - int(c2)
	}

	switch result := verResultType[state*3+digitClass(c2)]; result {
	case verCompare:
		return diff
	case verLength:
		// The two numbers agree so far; the longer run of digits is the
		// larger number.
		for isASCIIDigit(byteAt(a, ia)) {
			ia++
			if !isASCIIDigit(byteAt(b, ib)) {
				return 1
			}
			ib++
		}
		if isASCIIDigit(byteAt(b, ib)) {
			return -1
		}
		return diff
	default:
		return result
	}
}

// byteAt reads one byte, answering 0 past the end. git's compare walks
// NUL-terminated strings and the terminator is part of its arithmetic; a ref
// name can hold no NUL of its own, so nothing else can reach this value.
func byteAt(s string, index int) byte {
	if index < len(s) {
		return s[index]
	}
	return 0
}

// digitClass sorts a byte into the three columns the tables above are built
// on: 0 for anything but a digit, 1 for 1-9, 2 for a zero.
func digitClass(b byte) int {
	switch {
	case b == '0':
		return 2
	case b >= '1' && b <= '9':
		return 1
	default:
		return 0
	}
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// tagsLast leaves everything that is not a tag in the order git gave it and
// sends the tags to the end, newest first.
//
// Generic because the same list arrives in two shapes — the sidebar's Ref
// values and the decoration's "tag: v1.0" strings — and the whole point is
// that both surfaces obey one rule. Written twice, it is a rule that can
// disagree with itself.
//
// tagName answers the version to compare and whether the item is a tag at all.
func tagsLast[T any](items []T, tagName func(T) (string, bool)) []T {
	if len(items) < 2 {
		return items
	}

	rest := make([]T, 0, len(items))
	tags := make([]T, 0, len(items))
	for _, item := range items {
		if _, isTag := tagName(item); isTag {
			tags = append(tags, item)
			continue
		}
		rest = append(rest, item)
	}

	slices.SortFunc(tags, func(a, b T) int {
		nameA, _ := tagName(a)
		nameB, _ := tagName(b)
		return versionRefnameCompare(nameB, nameA)
	})
	return append(rest, tags...)
}

// tagDecorationPrefix is how %D spells a tag, and the only decoration that
// carries a prefix at all.
const tagDecorationPrefix = "tag: "

// orderDecorationTags keeps HEAD, the branches and the remotes in the order
// git printed them and sends the tag decorations to the end, newest first.
//
// The tags move past the remotes on the way — git's %D lists them before —
// which is the price of the sidebar and the badges answering with one order.
func orderDecorationTags(refs []string) []string {
	return tagsLast(refs, func(ref string) (string, bool) {
		return strings.CutPrefix(ref, tagDecorationPrefix)
	})
}

// withTagsNewestFirst is the same rule over the ref list for-each-ref
// answered.
func withTagsNewestFirst(refs []Ref) []Ref {
	return tagsLast(refs, func(ref Ref) (string, bool) {
		return ref.ShortName, ref.Kind == RefTag
	})
}
