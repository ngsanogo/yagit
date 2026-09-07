package git

import (
	"slices"
	"testing"
)

func TestVersionRefnameCompareMatchesGitOrder(t *testing.T) {
	// Copied from what `git for-each-ref --sort=version:refname` printed for
	// these tags on git 2.43. Lexicographic order would put v0.10.0 before
	// v0.9.0 and v10 before v2; reading digit runs as plain integers would
	// put v1.09 after v1.0 and make v1.01 and v1.1 equal, because git reads a
	// run with a leading zero as the digits after a decimal point.
	want := []string{
		"beta", "latest", "release-0.9", "release-0.10",
		"v001", "v01", "v0.1", "v0.9.0", "v0.10.0", "v0.25.0",
		"v1", "v1.01", "v1.010", "v1.0100", "v1.09", "v1.0", "v1.0.0", "v1.1",
		"v2", "v10",
	}
	got := slices.Clone(want)
	// Lexicographic first, so the assertion cannot pass on an input that was
	// already in the answer's order.
	slices.Sort(got)
	slices.SortFunc(got, versionRefnameCompare)
	if !slices.Equal(got, want) {
		t.Fatalf("version order = %v, want %v", got, want)
	}
}

// Two names that are not the same string must never compare equal: the sort
// underneath is not stable, so a zero there is an order that can differ
// between the sidebar and the badges on the same screen.
func TestVersionRefnameCompareOnlyEqualForTheSameName(t *testing.T) {
	names := []string{"v1", "v01", "v001", "v1.0", "v1.00", "v1.01", "v1.1", "beta"}
	for _, a := range names {
		for _, b := range names {
			got := versionRefnameCompare(a, b)
			if (got == 0) != (a == b) {
				t.Errorf("compare(%q, %q) = %d", a, b, got)
			}
			if other := versionRefnameCompare(b, a); (got < 0) != (other > 0) {
				t.Errorf("compare(%q, %q) = %d but compare(%q, %q) = %d",
					a, b, got, b, a, other)
			}
		}
	}
}

// A digit run longer than an int can hold used to wrap around and answer with
// the wrong sign. git compares the runs, not their arithmetic value.
func TestVersionRefnameCompareHandlesHugeDigitRuns(t *testing.T) {
	small := "build-99999999999999999999999998"
	large := "build-99999999999999999999999999"
	if got := versionRefnameCompare(small, large); got >= 0 {
		t.Errorf("compare(%q, %q) = %d, want negative", small, large, got)
	}
	if got := versionRefnameCompare(large, small); got <= 0 {
		t.Errorf("compare(%q, %q) = %d, want positive", large, small, got)
	}
}

func TestOrderDecorationTagsNewestFirst(t *testing.T) {
	got := orderDecorationTags([]string{
		"HEAD -> main",
		"tag: v0.9.0",
		"tag: v0.25.0",
		"tag: v0.10.0",
		"origin/main",
		"tag: beta",
	})
	want := []string{
		"HEAD -> main",
		"origin/main",
		"tag: v0.25.0",
		"tag: v0.10.0",
		"tag: v0.9.0",
		"tag: beta",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("decoration = %v, want %v", got, want)
	}
}

func TestWithTagsNewestFirst(t *testing.T) {
	got := withTagsNewestFirst([]Ref{
		{Kind: RefBranch, ShortName: "feature"},
		{Kind: RefTag, ShortName: "v0.9.0"},
		{Kind: RefBranch, ShortName: "main"},
		{Kind: RefTag, ShortName: "v0.10.0"},
		{Kind: RefTag, ShortName: "v0.25.0"},
		{Kind: RefRemote, ShortName: "origin/main"},
	})
	wantKinds := []RefKind{RefBranch, RefBranch, RefRemote, RefTag, RefTag, RefTag}
	wantNames := []string{"feature", "main", "origin/main", "v0.25.0", "v0.10.0", "v0.9.0"}
	if len(got) != len(wantNames) {
		t.Fatalf("refs = %+v, want %v", got, wantNames)
	}
	for index := range wantNames {
		if got[index].Kind != wantKinds[index] || got[index].ShortName != wantNames[index] {
			t.Fatalf("refs = %v, want names %v", shortNames(got), wantNames)
		}
	}
}

func shortNames(refs []Ref) []string {
	names := make([]string, len(refs))
	for index, ref := range refs {
		names[index] = ref.ShortName
	}
	return names
}
