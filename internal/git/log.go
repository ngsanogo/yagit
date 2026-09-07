package git

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Commit is a history entry as yagit displays it.
type Commit struct {
	SHA     string    `json:"sha"`
	Parents []string  `json:"parents"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Subject string    `json:"subject"`

	// Refs is the decoration git attaches to the commit: "HEAD -> main",
	// "origin/main", "tag: v1.0". It paints the badges on the graph row, and
	// nothing else. The sidebar's branch list comes from ForEachRef, which
	// also carries upstream tracking.
	Refs []string `json:"refs"`
}

// shortSHALength is how much of an object name a person recognises.
//
// Seven is git's own default abbreviation, and the interface shortens to seven
// too — see SHORT_SHA_LENGTH in web/src/lib/format.ts. Both are display, never
// identity: what travels on the wire and what reaches git is always the full
// name.
const shortSHALength = 7

// ShortSHA abbreviates an object name for a sentence somebody reads.
//
// Exported because a refusal is written in two packages — the reading that
// found the merge commit, and the route that answers about it — and one
// definition of "seven characters" between them is the difference between a
// message that matches the interface and one that nearly does.
//
// A name shorter than seven comes back whole rather than panicking. Nothing
// here should ever produce one: every SHA in a refusal came from `rev-parse
// --verify`. That is the reason to write the guard, not a reason to leave it
// out — the day one arrives from somewhere else, this already answers.
func ShortSHA(sha string) string {
	if len(sha) <= shortSHALength {
		return sha
	}
	return sha[:shortSHALength]
}

// logFormat produces one record per commit: six NUL-separated fields,
// terminated by NUL followed by a newline.
//
// None of the six current fields can carry a newline: git forbids one in an
// author name or a ref name, and %s flattens the subject onto a single line.
// So splitting on '\n' would work today.
//
// The terminator is not there for today. It is there for the day someone adds
// %b or %B to this format: splitting by line becomes wrong then, and that is
// the kind of mistake that gets through review precisely because the format
// "worked" until then.
//
// It used to be 0x01, on the belief that git refuses that byte everywhere. It
// does not — `git commit -F` accepts a message holding 0x01 and stores it
// verbatim, and a subject like `Revert "add \x01 handling"` would have split
// one commit into two unparseable halves. NUL is the byte git really does
// refuse in every field, so NUL is what marks the end; the newline that
// follows it makes the sequence impossible to forge, since no field may start
// with one.
const logFormat = "%H%x00%P%x00%an%x00%aI%x00%s%x00%D%x00%x0a"

const (
	fieldSeparator   = "\x00"
	recordTerminator = "\x00\n"

	// logFieldCount must stay in sync with logFormat. A mismatch produces an
	// explicit parse error rather than a silent shift of the fields.
	logFieldCount = 6
)

// Scope names which commits a history walk covers.
//
// Three of them, and the difference is the whole reason this type exists:
// every ref is the widest picture a repository can draw and, on a repository
// with a thousand tags and a hundred topic branches, one nobody can read. The
// checked-out branch is the one nearly everyone is looking at. Between the two
// sits the question people actually arrive with — how far has this topic
// drifted from main — which neither end answers. See docs/adr/0016 and
// docs/adr/0033.
type Scope string

const (
	// ScopeHead walks what is checked out: the current branch, or the commit
	// a detached HEAD sits on. It is the default the interface draws.
	ScopeHead Scope = "head"

	// ScopeAll walks every ref, and HEAD with them — `git log --all`.
	ScopeAll Scope = "all"

	// ScopeRefs walks the refs the caller named, and nothing else. The chosen
	// set IS the scope here: main alone and main with a topic branch are two
	// different histories, not two views of one (docs/adr/0033).
	ScopeRefs Scope = "refs"
)

// Describe names a scope the way the interface does, for a message somebody
// reads. The wire values are "head", "all" and "refs", which say nothing to
// anyone who has not read this file.
func (s Scope) Describe() string {
	switch s {
	case ScopeAll:
		return "every reference"
	case ScopeRefs:
		return "the chosen references"
	default:
		return "what is checked out"
	}
}

// Log returns the repository's full history, across every ref, in topological
// order. It reads the refs and HEAD first; when the caller already holds
// them, use LogScope instead and skip the extra commands.
func (r *Runner) Log(ctx context.Context, dir string) ([]Commit, error) {
	refs, err := r.ForEachRef(ctx, dir)
	if err != nil {
		return nil, err
	}
	head, err := r.ReadHEAD(ctx, dir)
	if err != nil {
		return nil, err
	}
	return r.LogScope(ctx, dir, ScopeAll, refs, head, nil)
}

// LogScope returns the history a scope covers, when the refs and HEAD are
// already known. selected is the set ScopeRefs walks and is read under no
// other scope.
//
// A repository with nothing to walk is a perfectly normal state — the one
// right after git init, and the one a scope can land in on its own — but
// `git log` fails there with "does not have any commits yet". Which state
// means "nothing to walk" is decided from the refs and HEAD the caller
// already read, never from git's error text: that text changes between git
// versions and the exit code 128 is shared with every other bad revision, so
// reading either would swallow the failures this project promises to show.
func (r *Runner) LogScope(
	ctx context.Context, dir string, scope Scope, refs []Ref, head HEAD, selected []string,
) ([]Commit, error) {
	// An unborn branch has no commit to start from, and neither has a
	// repository whose refs are all gone. Nothing else here is empty: a
	// detached HEAD is a commit like any other and walks normally. Which
	// revisions each scope walks — and whether there are any — is
	// revisionsForScope's, shared with search.go so that the list a search
	// answers with and the graph beside it cover the same commits.
	revisions, walkable, err := revisionsForScope(scope, refs, head, selected)
	if err != nil {
		return nil, err
	}
	if !walkable {
		return []Commit{}, nil
	}

	// --decorate=short is not a default worth trusting: with log.decorate=full
	// in a user's config, %D answers "refs/heads/main" where the parser expects
	// "main", and every branch badge in the interface changes shape depending
	// on whose machine the daemon runs on.
	//
	// The trailing -- ends the revisions. Without it a repository holding a
	// file called HEAD makes `git log HEAD` ambiguous, and git refuses the
	// whole walk rather than guessing.
	args := append([]string{"log"}, revisions...)
	args = append(args, "--topo-order", "--decorate=short",
		"--pretty=format:"+logFormat, "--")

	// historyTimeout rather than the Runner's own. See the constant: this walk
	// reads the whole repository, and the thirty seconds meant for commands
	// that return instantly is the wrong deadline for it — the same argument
	// rewriteTimeout and networkTimeout each already make for themselves.
	output, err := r.Exec(ctx, Command{Dir: dir, Args: args, Timeout: historyTimeout})
	if err != nil {
		return nil, err
	}
	return ParseLog(output)
}

// splitRecords cuts the output of a `--pretty=format:` walk into records.
//
// The framing, in one place, because two parsers now depend on it: ParseLog
// and ParseStashList read different fields out of the same shape, and a
// terminator one of them handled and the other did not is a bug that produces
// a plausible wrong answer rather than a crash. It very nearly did — the stash
// list was written against `--format=`, whose extra newline left a record made
// of nothing else.
//
// Two things have to be absorbed. git SEPARATES records with a newline under
// `format:` and TERMINATES them with one under `tformat:`, so every record but
// the first begins with a newline and there may be a chunk after the last
// terminator that is nothing but one. Both are dropped here, which makes this
// correct for either spelling rather than for whichever one the caller wrote.
func splitRecords(output []byte) []string {
	chunks := strings.Split(string(output), recordTerminator)

	records := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimPrefix(chunk, "\n")
		if chunk == "" {
			continue
		}
		records = append(records, chunk)
	}
	return records
}

// ParseLog turns Log's output into commits.
//
// Pure and exported: this is one of the two places in the project where a bug
// would be silent, so it is testable without running git.
func ParseLog(output []byte) ([]Commit, error) {
	records := splitRecords(output)

	commits := make([]Commit, 0, len(records))
	for index, record := range records {
		commit, err := parseLogRecord(record)
		if err != nil {
			return nil, fmt.Errorf("git log record %d: %w", index, err)
		}
		commits = append(commits, commit)
	}

	return commits, nil
}

func parseLogRecord(record string) (Commit, error) {
	fields := strings.Split(record, fieldSeparator)
	if len(fields) != logFieldCount {
		return Commit{}, fmt.Errorf(
			"expected %d NUL-separated fields, got %d in %q",
			logFieldCount, len(fields), record)
	}

	date, err := time.Parse(time.RFC3339, fields[3])
	if err != nil {
		return Commit{}, fmt.Errorf("unreadable author date %q: %w", fields[3], err)
	}

	return Commit{
		SHA:     fields[0],
		Parents: strings.Fields(fields[1]),
		Author:  fields[2],
		Date:    date,
		Subject: fields[4],
		Refs:    parseRefDecoration(fields[5]),
	}, nil
}

// parseRefDecoration splits the decoration from %D, for example
// "HEAD -> main, origin/main, tag: v1.0".
//
// The ", " separator is safe: git forbids spaces in a ref name, so no
// name can contain that sequence.
//
// Tags are then reordered newest-first by version:refname — see
// orderDecorationTags. git's own %D order is deterministic but useless here:
// HEAD, then the tags by refname descending, then the remotes. A row carrying
// v0.9.0, v0.10.0 and v0.25.0 arrives in that order and disagrees with the
// sidebar about which of them is first.
func parseRefDecoration(decoration string) []string {
	decoration = strings.TrimSpace(decoration)
	if decoration == "" {
		return []string{}
	}

	parts := strings.Split(decoration, ", ")
	refs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			refs = append(refs, part)
		}
	}
	return orderDecorationTags(refs)
}
