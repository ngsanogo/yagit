package git_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// searchable builds a repository whose four fields are all distinguishable:
// each commit is the only match for one of them.
//
//	A  "add the parser"        by Ada    touches parser.go, adds "tokenise"
//	B  "fix(api): a paren ("   by Grace  touches api.go,    adds "handler"
//	C  "tidy"                  by Ada    touches parser.go, removes "tokenise"
func searchable(t *testing.T) (string, *git.Runner) {
	t.Helper()
	dir := initRepo(t)
	runner := git.NewRunner(nil)

	write(t, dir, "parser.go", "func tokenise() {}\n")
	runGit(t, runner, dir, "add", "-A")
	runGit(t, runner, dir, "-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "-m", "add the parser")

	write(t, dir, "api.go", "func handler() {}\n")
	runGit(t, runner, dir, "add", "-A")
	runGit(t, runner, dir, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com",
		"commit", "-m", "fix(api): a paren (")

	write(t, dir, "parser.go", "func parse() {}\n")
	runGit(t, runner, dir, "add", "-A")
	runGit(t, runner, dir, "-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "-m", "tidy")

	return dir, runner
}

func searchIn(t *testing.T, runner *git.Runner, dir string, field git.SearchField, query string) git.SearchResult {
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
	found, err := runner.Search(ctx, dir, git.ScopeHead, field, query, refs, head, nil)
	if err != nil {
		t.Fatalf("Search(%s, %q): %v", field, query, err)
	}
	return found
}

func subjects(result git.SearchResult) []string {
	names := make([]string, 0, len(result.Commits))
	for _, commit := range result.Commits {
		names = append(names, commit.Subject)
	}
	return names
}

func TestSearchByMessage(t *testing.T) {
	dir, runner := searchable(t)

	got := subjects(searchIn(t, runner, dir, git.SearchMessage, "parser"))
	if len(got) != 1 || got[0] != "add the parser" {
		t.Fatalf("subjects = %v", got)
	}
}

// The whole reason --fixed-strings is on: `fix(api)` is an unbalanced-looking
// pattern to a regex engine, and a search box is not a regex box.
func TestSearchByMessageTakesTheQueryLiterally(t *testing.T) {
	dir, runner := searchable(t)

	got := subjects(searchIn(t, runner, dir, git.SearchMessage, "fix(api)"))
	if len(got) != 1 || got[0] != "fix(api): a paren (" {
		t.Fatalf("subjects = %v", got)
	}

	// A pattern a regex would refuse outright rather than answer.
	if got := subjects(searchIn(t, runner, dir, git.SearchMessage, "a paren (")); len(got) != 1 {
		t.Fatalf("subjects = %v", got)
	}
}

func TestSearchByMessageIgnoresCase(t *testing.T) {
	dir, runner := searchable(t)

	if got := subjects(searchIn(t, runner, dir, git.SearchMessage, "PARSER")); len(got) != 1 {
		t.Fatalf("subjects = %v", got)
	}
}

func TestSearchByAuthor(t *testing.T) {
	dir, runner := searchable(t)

	got := subjects(searchIn(t, runner, dir, git.SearchAuthor, "grace"))
	if len(got) != 1 || got[0] != "fix(api): a paren (" {
		t.Fatalf("subjects = %v", got)
	}
}

func TestSearchByPath(t *testing.T) {
	dir, runner := searchable(t)

	got := subjects(searchIn(t, runner, dir, git.SearchPath, "parser.go"))
	if len(got) != 2 {
		t.Fatalf("subjects = %v, want the two commits that touched parser.go", got)
	}
}

// The pickaxe: commits where the number of occurrences changed — the one that
// added the word, and the one that took it away.
func TestSearchByContent(t *testing.T) {
	dir, runner := searchable(t)

	got := subjects(searchIn(t, runner, dir, git.SearchContent, "tokenise"))
	if len(got) != 2 {
		t.Fatalf("subjects = %v, want the commit that added it and the one that removed it", got)
	}
}

func TestSearchAnswersWithTheCommandItRan(t *testing.T) {
	dir, runner := searchable(t)

	found := searchIn(t, runner, dir, git.SearchPath, "parser.go")
	if !strings.Contains(found.Command, ":(literal)parser.go") {
		t.Fatalf("command = %q, want a literal pathspec", found.Command)
	}
	// The line shown is the search asked for, not the off-by-one used to find
	// out whether there is more.
	if !strings.Contains(found.Command, "--max-count=200") {
		t.Fatalf("command = %q", found.Command)
	}
}

func TestSearchRefusesAnEmptyQuery(t *testing.T) {
	dir, runner := searchable(t)
	ctx := context.Background()

	refs, err := runner.ForEachRef(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := runner.ReadHEAD(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Search(ctx, dir, git.ScopeHead, git.SearchMessage, "  ", refs, head, nil); !errors.Is(err, git.ErrEmptyQuery) {
		t.Fatalf("err = %v, want ErrEmptyQuery", err)
	}
}

// A repository with no commits is a normal state, and `git log` fails there
// rather than answering with nothing — so which state that is has to be
// decided from the refs, never from git's error text.
func TestSearchOnARepositoryWithNoCommits(t *testing.T) {
	isolateGitConfiguration(t)
	runner := git.NewRunner(nil)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "empty")
	runGit(t, runner, filepath.Dir(dir), "init", "-b", "main", dir)

	found, err := runner.Search(ctx, dir, git.ScopeHead, git.SearchMessage, "anything", nil, git.HEAD{}, nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(found.Commits) != 0 || found.Truncated {
		t.Fatalf("found = %+v", found)
	}
}

func TestParseSearchField(t *testing.T) {
	for _, name := range []string{"message", "author", "path", "content"} {
		if _, err := git.ParseSearchField(name); err != nil {
			t.Errorf("ParseSearchField(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "committer", "Message"} {
		if _, err := git.ParseSearchField(name); err == nil {
			t.Errorf("ParseSearchField(%q) was accepted", name)
		}
	}
}
