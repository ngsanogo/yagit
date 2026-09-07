package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Searching the history: which commits match, in one of four ways.
//
// Not a filter over the drawn graph, and that is the decision this file rests
// on. Lane assignment draws a picture of how commits connect
// (docs/adr/0012); running it over a filtered set draws connections that are
// not in the repository — two commits side by side that have three others
// between them. So a search answers with a LIST, and following a result takes
// the graph to that commit, which is the same movement clicking a branch in
// the sidebar already makes.
//
// Every field is matched literally rather than as a regex. `--grep` takes a
// POSIX pattern by default, so a search for `foo(bar)` finds nothing and a
// search for `a(b` is an error about parentheses — in reply to a box that
// looked like a search box. `--fixed-strings` makes what was typed what is
// looked for.

// SearchField is where a query is looked for.
type SearchField string

const (
	// SearchMessage matches the commit message — `--grep`.
	SearchMessage SearchField = "message"

	// SearchAuthor matches the author's name or email — `--author`.
	SearchAuthor SearchField = "author"

	// SearchPath matches commits that touched a path — a pathspec after `--`.
	SearchPath SearchField = "path"

	// SearchContent matches commits that changed how many times a string
	// appears — `-S`, git's pickaxe.
	SearchContent SearchField = "content"
)

// ErrEmptyQuery: nothing was typed to look for.
//
// Refused rather than run: `git log --grep=` matches every commit, so an empty
// box would answer with the whole history under the word "results".
var ErrEmptyQuery = errors.New("no search query given")

// errUnknownSearchField names the four, because a refusal here is somebody
// being told which words the field takes.
var errUnknownSearchField = errors.New("the search field must be message, author, path or content")

// ParseSearchField reads a field sent by a client.
func ParseSearchField(raw string) (SearchField, error) {
	switch SearchField(raw) {
	case SearchMessage, SearchAuthor, SearchPath, SearchContent:
		return SearchField(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownSearchField)
	default:
		return "", fmt.Errorf("%w: %q is not one", errUnknownSearchField, raw)
	}
}

// SearchLimit is how many matches one search answers with.
//
// A cap rather than paging, and the number is chosen for what the answer is
// for: a list somebody scans to find one commit. Past a couple of hundred rows
// nobody is scanning — they are refining the query, which is what the "more
// than this matched" line says out loud.
const SearchLimit = 200

// SearchResult is what a search found.
type SearchResult struct {
	Commits []Commit `json:"commits"`

	// Truncated says the repository holds more matches than SearchLimit.
	Truncated bool `json:"truncated"`

	// Command is the line that ran, for the same reason every other operation
	// shows one: a search that finds nothing is indistinguishable from a
	// search that asked the wrong question, unless the question is on screen.
	Command string `json:"command"`
}

// SearchArgs is the command Search runs. Exported for the reason CloneArgs is:
// the line the user is shown and the line git receives have one definition
// between them.
//
// The four fields differ in one argument each, and the differences are not
// interchangeable:
//
//   - `--grep` and `--author` are patterns, made literal by `--fixed-strings`
//     and case-insensitive by `--regexp-ignore-case`. Both flags apply to both.
//
//   - A path is a pathspec, and it goes after `--` as `:(literal)` for the
//     reason staging.go gives at length: everything after `--` is matched with
//     wildmatch, so a file named `app/[id].tsx` is a PATTERN by the time git
//     reads it. A directory still matches everything under it, which is
//     git's own meaning of a pathspec and what somebody searching for `web/src`
//     wants.
//
//   - `-S` is the pickaxe: commits where the number of occurrences of the
//     string CHANGED. Case-sensitive, because making it otherwise means
//     `--pickaxe-regex`, which would turn the same box into a regex box for one
//     of the four fields and not the others. `-G` was the alternative and does
//     something else — it matches any diff that mentions the text, including
//     one that only moved it, which buries the commit that introduced a common
//     word under every commit that shifted it since.
func SearchArgs(field SearchField, query string, revisions []string, limit int) []string {
	args := append([]string{"log"}, revisions...)
	args = append(args,
		"--topo-order", "--decorate=short",
		"--max-count="+strconv.Itoa(limit),
		"--pretty=format:"+logFormat,
	)

	switch field {
	case SearchMessage:
		args = append(args, "--fixed-strings", "--regexp-ignore-case", "--grep="+query)
	case SearchAuthor:
		args = append(args, "--fixed-strings", "--regexp-ignore-case", "--author="+query)
	case SearchContent:
		args = append(args, "-S"+query)
	}

	args = append(args, "--")
	if field == SearchPath {
		args = append(args, literalPathspecs([]string{query})...)
	}
	return args
}

// Search returns the commits matching a query, newest first within the walk's
// topological order.
//
// One more than the cap is asked for, so that "there are more" is a fact read
// from git rather than a guess from a full page.
func (r *Runner) Search(
	ctx context.Context, dir string, scope Scope, field SearchField, query string,
	refs []Ref, head HEAD, selected []string,
) (SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResult{}, ErrEmptyQuery
	}
	if _, err := ParseSearchField(string(field)); err != nil {
		return SearchResult{}, err
	}

	revisions, walkable, err := revisionsForScope(scope, refs, head, selected)
	if err != nil {
		return SearchResult{}, err
	}
	if !walkable {
		// Nothing to walk is a normal state — a repository right after init —
		// and `git log` fails there with "does not have any commits yet".
		// Which state that is has to be decided from the refs, never from
		// git's error text; see LogScope.
		return SearchResult{
			Commits: []Commit{},
			Command: CommandLine(SearchArgs(field, query, revisions, SearchLimit)),
		}, nil
	}

	args := SearchArgs(field, query, revisions, SearchLimit+1)
	output, err := r.Run(ctx, dir, args...)
	if err != nil {
		return SearchResult{}, err
	}

	commits, err := ParseLog(output)
	if err != nil {
		return SearchResult{}, err
	}

	truncated := len(commits) > SearchLimit
	if truncated {
		commits = commits[:SearchLimit]
	}

	return SearchResult{
		Commits:   commits,
		Truncated: truncated,
		// The line without the extra row: what is shown is the search that was
		// asked for, not the off-by-one this function uses to find out whether
		// there is more.
		Command: CommandLine(SearchArgs(field, query, revisions, SearchLimit)),
	}, nil
}

// revisionsForScope is the revision arguments a walk of that scope starts
// from, and whether there is anything to walk at all.
//
// Split out of LogScope so a search covers exactly the commits the graph beside
// it draws. Two readings of "what is checked out" would eventually disagree,
// and the way it would show is a search finding a commit the graph cannot
// scroll to.
//
// A list rather than one string, because the third scope names its own
// revisions: `git log` takes as many as it is given, and one comma-joined
// string is not one of the shapes it takes.
func revisionsForScope(
	scope Scope, refs []Ref, head HEAD, selected []string,
) (revisions []string, walkable bool, err error) {
	switch scope {
	case ScopeHead:
		return []string{"HEAD"}, head.SHA != "", nil
	case ScopeAll:
		return []string{"--all"}, len(refs) > 0 || head.SHA != "", nil
	case ScopeRefs:
		// Checked here rather than only at the route, because this is where
		// the names become arguments and both walks that take a chosen set
		// come through this function. A check only one of them ran would be a
		// check the other is one refactor away from not having.
		for _, name := range selected {
			if err := CheckRefName(name); err != nil {
				return nil, false, err
			}
		}
		return selected, len(selected) > 0, nil
	default:
		return nil, false, fmt.Errorf("unknown history scope %q", scope)
	}
}
