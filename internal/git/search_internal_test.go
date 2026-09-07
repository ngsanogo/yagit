package git

import (
	"errors"
	"slices"
	"testing"
)

// What the four searches actually send to git, and what a chosen set of refs
// becomes once it is an argument list.
//
// Internal, because the arguments are the whole of what distinguishes them and
// a test that only read the results back would pass on the wrong command: a
// `--grep` for a path finds nothing, which looks exactly like a path nobody
// touched. Three things are worth pinning — where the revisions go, where the
// `--` goes, and that a chosen ref is refused before it can be an option.

func TestSearchArgsPutTheRevisionWhereGitReadsIt(t *testing.T) {
	args := SearchArgs(SearchMessage, "fix the cache", []string{"HEAD"}, SearchLimit)

	// Immediately after the subcommand. `git log --grep=x HEAD` works too, but
	// only by git's own leniency, and the pathspec form below has no such
	// latitude — one position for both is one thing to be right about.
	if args[0] != "log" || args[1] != "HEAD" {
		t.Fatalf("args = %v, want the revision straight after log", args)
	}
	if !slices.Contains(args, "--grep=fix the cache") {
		t.Errorf("args = %v, want a --grep carrying the query whole", args)
	}
	// Literal, not a regexp: somebody searching for "a.b" means "a.b".
	if !slices.Contains(args, "--fixed-strings") {
		t.Errorf("args = %v, want --fixed-strings", args)
	}
	if args[len(args)-1] != "--" {
		t.Errorf("args = %v, want a trailing -- so no query becomes a path", args)
	}
}

func TestSearchArgsForAPathUseAPathspecAfterTheSeparator(t *testing.T) {
	args := SearchArgs(SearchPath, "cache.go", []string{"--all"}, SearchLimit)

	if args[1] != "--all" {
		t.Fatalf("args = %v, want --all as the revision", args)
	}
	separator := slices.Index(args, "--")
	if separator < 0 || separator != len(args)-2 {
		t.Fatalf("args = %v, want exactly one path after a single --", args)
	}
	// `:(literal)`, because git's pathspecs are wildmatch: a file named
	// `app/[id].tsx` is a PATTERN by the time git reads it.
	if args[separator+1] != ":(literal)cache.go" {
		t.Errorf("pathspec = %q", args[separator+1])
	}
}

// The chosen set arrives as several revisions rather than as one joined
// string, which is the shape `git log` takes and the shape a comma-separated
// value is not.
func TestSearchArgsWalkEveryRefThatWasChosen(t *testing.T) {
	chosen := []string{"refs/heads/main", "refs/heads/topic"}
	args := SearchArgs(SearchMessage, "fix", chosen, SearchLimit)

	if args[1] != chosen[0] || args[2] != chosen[1] {
		t.Fatalf("args = %v, want both chosen refs as revisions", args)
	}
}

// revisionsForScope is where a name from a query string becomes an argument,
// and therefore where it is refused. Both walks that take a chosen set come
// through this function; a check only one of them ran would be a check the
// other is one refactor away from not having.
func TestRevisionsForScopeRefuseWhatGitWouldMisread(t *testing.T) {
	cases := map[string]struct {
		scope    Scope
		selected []string
		want     error
	}{
		"a scope nobody defined": {scope: "some"},
		"a ref git would read as an option": {
			scope: ScopeRefs, selected: []string{"-f"}, want: ErrBadRefName,
		},
		"a ref git would read as a range": {
			scope: ScopeRefs, selected: []string{"main..topic"}, want: ErrBadRefName,
		},
		"an option hidden behind an honest ref": {
			scope:    ScopeRefs,
			selected: []string{"refs/heads/main", "--output=/tmp/written"},
			want:     ErrBadRefName,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := revisionsForScope(testCase.scope, nil, HEAD{}, testCase.selected)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if testCase.want != nil && !errors.Is(err, testCase.want) {
				t.Fatalf("err = %v, want %v", err, testCase.want)
			}
		})
	}
}

// A chosen set of none is not an error here: the route above refuses it,
// because an empty walk drawn is indistinguishable from an empty repository,
// and this layer answers the honest "there is nothing to walk".
func TestRevisionsForScopeSayNothingIsWalkableWithNoRefs(t *testing.T) {
	revisions, walkable, err := revisionsForScope(ScopeRefs, nil, HEAD{}, nil)
	if err != nil {
		t.Fatalf("revisionsForScope: %v", err)
	}
	if walkable {
		t.Error("a chosen set of no refs was reported as something to walk")
	}
	if len(revisions) != 0 {
		t.Errorf("revisions = %v, want none", revisions)
	}
}
