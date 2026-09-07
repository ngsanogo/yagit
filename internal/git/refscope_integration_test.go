package git_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// topicAndMain builds the shape ScopeRefs exists for: a topic that left main and
// was never merged back, so neither end of the two older scopes answers "how
// far has this drifted".
//
//	main:  A ─── B
//	        ╲
//	topic:   ─── T
func topicAndMain(t *testing.T) (string, *git.Runner) {
	t.Helper()
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "checkout", "-b", "topic")
	commitEmpty(t, runner, dir, "T")
	runGit(t, runner, dir, "checkout", "main")
	commitEmpty(t, runner, dir, "B")

	return dir, runner
}

func walk(t *testing.T, runner *git.Runner, dir string, scope git.Scope, chosen ...string) []git.Commit {
	t.Helper()
	ctx := context.Background()

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := runner.LogScope(ctx, dir, scope, refs, head, chosen)
	if err != nil {
		t.Fatalf("LogScope(%s, %v): %v", scope, chosen, err)
	}
	return commits
}

// The middle of ADR 0033: one ref is narrower than what is checked out only
// when it is a different one, and two refs together are the divergence neither
// of the other scopes can draw on its own.
func TestLogScopeRefsWalksExactlyTheRefsItWasGiven(t *testing.T) {
	dir, runner := topicAndMain(t)

	only := namesOf(walk(t, runner, dir, git.ScopeRefs, "refs/heads/topic"))
	if len(only) != 2 || only[0] != "T" {
		t.Errorf("one ref walked %v, want the topic and the commit it left from", only)
	}

	both := namesOf(walk(t, runner, dir,
		git.ScopeRefs, "refs/heads/main", "refs/heads/topic"))
	if len(both) != 3 {
		t.Fatalf("two refs walked %v, want all three commits", both)
	}

	// And the same three the widest scope finds, because in this repository
	// the chosen set happens to be every ref there is. The scopes differ in
	// what they name, not in how they walk.
	if all := namesOf(walk(t, runner, dir, git.ScopeAll)); len(all) != len(both) {
		t.Errorf("scope=all walked %v and the chosen pair walked %v", all, both)
	}
}

// The check is in revisionsForScope rather than only at the route, so both
// walks that take a chosen set run it. This is the walk half.
func TestLogScopeRefsRefusesANameThatIsAnOption(t *testing.T) {
	dir, runner := topicAndMain(t)
	ctx := context.Background()

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runner.LogScope(ctx, dir, git.ScopeRefs, refs, head,
		[]string{"refs/heads/main", "--output=/tmp/written"})
	if !errors.Is(err, git.ErrBadRefName) {
		t.Errorf("LogScope with an option among the refs = %v, want ErrBadRefName", err)
	}
}

// And this is the search half. A check only one of them ran is a check the
// other route is one refactor away from not having.
func TestSearchUnderScopeRefsRunsTheSameCheck(t *testing.T) {
	dir, runner := topicAndMain(t)
	ctx := context.Background()

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runner.Search(ctx, dir, git.ScopeRefs, git.SearchMessage, "T", refs, head,
		[]string{"-n1"})
	if !errors.Is(err, git.ErrBadRefName) {
		t.Errorf("Search with an option among the refs = %v, want ErrBadRefName", err)
	}

	// The honest set answers the topic's commit and not the one only main can
	// reach: "in this history" is most of what a search means.
	found, err := runner.Search(ctx, dir, git.ScopeRefs, git.SearchMessage, "B", refs, head,
		[]string{"refs/heads/topic"})
	if err != nil {
		t.Fatalf("Search over the topic: %v", err)
	}
	if len(found.Commits) != 0 {
		t.Errorf("searching the topic found %v, which only main reaches", namesOf(found.Commits))
	}
}

// A walk with nothing to walk is not an error and not a guess: git answers
// exit code 128 for an unknown revision and for an empty repository alike, so
// the emptiness is decided from the refs already read.
func TestLogScopeRefsWithNoRefsWalksNothing(t *testing.T) {
	dir, runner := topicAndMain(t)

	if commits := walk(t, runner, dir, git.ScopeRefs); len(commits) != 0 {
		t.Errorf("a chosen set of none walked %v", namesOf(commits))
	}
}

func TestScopeDescribeNamesAllThree(t *testing.T) {
	for scope, want := range map[git.Scope]string{
		git.ScopeHead: "what is checked out",
		git.ScopeAll:  "every reference",
		git.ScopeRefs: "the chosen references",
	} {
		if got := scope.Describe(); got != want {
			t.Errorf("Scope(%q).Describe() = %q, want %q", scope, got, want)
		}
	}
}

// namesOf is what the assertions above compare: the commits by name, in the
// order the walk put them in.
func namesOf(commits []git.Commit) []string {
	names := make([]string, 0, len(commits))
	for _, commit := range commits {
		names = append(names, commit.Subject)
	}
	return names
}
