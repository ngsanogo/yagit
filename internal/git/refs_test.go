package git_test

import (
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// refLine assembles a for-each-ref line: five NUL-separated fields.
func refLine(fields ...string) string {
	return strings.Join(fields, "\x00")
}

func TestParseRefsEmptyOutput(t *testing.T) {
	refs, err := git.ParseRefs(nil)
	if err != nil {
		t.Fatalf("a repository with no ref is not an error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs, got %d", len(refs))
	}
}

func TestParseRefsClassification(t *testing.T) {
	output := []byte(strings.Join([]string{
		refLine("refs/heads/main", "aaaa", "", "refs/remotes/origin/main", ""),
		refLine("refs/remotes/origin/main", "aaaa", "", "", ""),
		refLine("refs/tags/v1.0", "bbbb", "", "", ""),
		refLine("refs/stash", "cccc", "", "", ""),
	}, "\n") + "\n")

	refs, err := git.ParseRefs(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(refs) != 4 {
		t.Fatalf("expected 4 refs, got %d", len(refs))
	}

	cases := []struct {
		kind      git.RefKind
		shortName string
	}{
		{git.RefBranch, "main"},
		{git.RefRemote, "origin/main"},
		{git.RefTag, "v1.0"},
		{git.RefOther, "refs/stash"},
	}
	for index, expected := range cases {
		if refs[index].Kind != expected.kind {
			t.Errorf("refs[%d].Kind = %q, expected %q", index, refs[index].Kind, expected.kind)
		}
		if refs[index].ShortName != expected.shortName {
			t.Errorf("refs[%d].ShortName = %q, expected %q", index, refs[index].ShortName, expected.shortName)
		}
	}

	if refs[0].Upstream != "refs/remotes/origin/main" {
		t.Errorf("upstream of main = %q", refs[0].Upstream)
	}
}

// An annotated tag points at a tag object, not at a commit. Without the
// dereferenced field, its badge would attach to an object absent from the
// graph.
func TestParseRefsAnnotatedTagUsesDereferencedCommit(t *testing.T) {
	output := []byte(refLine("refs/tags/v2.0", "tagobject", "targetcommit", "", ""))

	refs, err := git.ParseRefs(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if refs[0].SHA != "targetcommit" {
		t.Errorf("SHA = %q, expected targetcommit (the tag object must not win)", refs[0].SHA)
	}
}

func TestParseRefsUpstreamTracking(t *testing.T) {
	cases := []struct {
		track  string
		ahead  int
		behind int
		gone   bool
	}{
		{"", 0, 0, false},
		{"[ahead 3]", 3, 0, false},
		{"[behind 2]", 0, 2, false},
		{"[ahead 1, behind 4]", 1, 4, false},
		{"[gone]", 0, 0, true},

		// git cannot emit these. They are here because the counts are read
		// out of a human-readable string, and a branch row renders them as
		// "N commits ahead" — so the only safe reading of a number that is
		// not a count is no count at all.
		{"[ahead -1]", 0, 0, false},
		{"[behind -7]", 0, 0, false},
		{"[ahead -1, behind -2]", 0, 0, false},
		{"[ahead notanumber]", 0, 0, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.track, func(t *testing.T) {
			output := []byte(refLine("refs/heads/main", "aaaa", "", "refs/remotes/origin/main", testCase.track))

			refs, err := git.ParseRefs(output)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if refs[0].Ahead != testCase.ahead {
				t.Errorf("ahead = %d, expected %d", refs[0].Ahead, testCase.ahead)
			}
			if refs[0].Behind != testCase.behind {
				t.Errorf("behind = %d, expected %d", refs[0].Behind, testCase.behind)
			}
			if refs[0].Gone != testCase.gone {
				t.Errorf("gone = %v, expected %v", refs[0].Gone, testCase.gone)
			}
		})
	}
}

func TestParseRefsWrongFieldCount(t *testing.T) {
	_, err := git.ParseRefs([]byte("refs/heads/main\x00aaaa"))
	if err == nil {
		t.Fatal("a truncated line must produce an error")
	}
	if !strings.Contains(err.Error(), "fields") {
		t.Errorf("the error must name the problem, got: %v", err)
	}
}
