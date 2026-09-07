package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// RefKind classifies a ref by its namespace.
type RefKind string

const (
	RefBranch RefKind = "branch"
	RefRemote RefKind = "remote"
	RefTag    RefKind = "tag"
	RefOther  RefKind = "other"
)

// Ref is a git ref with, for local branches, its tracking state against the
// upstream.
type Ref struct {
	Name      string  `json:"name"`       // refs/heads/main
	ShortName string  `json:"short_name"` // main
	Kind      RefKind `json:"kind"`
	SHA       string  `json:"sha"`

	Upstream string `json:"upstream,omitempty"` // refs/remotes/origin/main
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`

	// Gone flags an upstream that is configured but gone from the remote. The
	// user has to see that state: their branch tracks something that no longer
	// exists.
	Gone bool `json:"gone"`
}

// refFormat describes one ref per line, NUL-separated fields.
//
// A ref name can contain neither a newline nor a NUL — git refuses both —
// so the line is enough as a record separator, without the terminator git
// log requires.
//
// %(*objectname) deserves a word: for an annotated tag, %(objectname) is the
// tag object, not the commit it points at. Without the dereferenced field,
// every annotated tag would attach to an object absent from the graph and
// their badges would vanish from the history.
const refFormat = "%(refname)%00%(objectname)%00%(*objectname)%00%(upstream)%00%(upstream:track)"

const refFieldCount = 5

// ForEachRef returns every ref in the repository.
//
// Sorted by version:refname so release tags read in version order rather than
// lexicographic refname order, which puts v0.10.0 before v0.9.0 and made the
// sidebar unreadable for version tags. Tags are then reversed in process, by
// the compare that reorders the decoration badges — one rule on both surfaces,
// and no second for-each-ref: `--sort=-version:refname` would reverse the
// branches with them, and the history cache pays for one invocation per
// refresh.
func (r *Runner) ForEachRef(ctx context.Context, dir string) ([]Ref, error) {
	output, err := r.Run(ctx, dir, "for-each-ref", "--format="+refFormat, "--sort=version:refname")
	if err != nil {
		return nil, err
	}
	refs, err := ParseRefs(output)
	if err != nil {
		return nil, err
	}
	return withTagsNewestFirst(refs), nil
}

// ParseRefs turns ForEachRef's output into refs. Pure function, testable
// without git.
func ParseRefs(output []byte) ([]Ref, error) {
	lines := strings.Split(strings.TrimRight(string(output), "\n"), "\n")

	refs := make([]Ref, 0, len(lines))
	for index, line := range lines {
		if line == "" {
			continue
		}

		ref, err := parseRefLine(line)
		if err != nil {
			return nil, fmt.Errorf("git for-each-ref line %d: %w", index+1, err)
		}
		refs = append(refs, ref)
	}

	return refs, nil
}

func parseRefLine(line string) (Ref, error) {
	fields := strings.Split(line, fieldSeparator)
	if len(fields) != refFieldCount {
		return Ref{}, fmt.Errorf(
			"expected %d NUL-separated fields, got %d in %q",
			refFieldCount, len(fields), line)
	}

	name := fields[0]

	// For an annotated tag, the second field carries the commit it points at;
	// it is empty everywhere else. So we always prefer the dereferenced one
	// when it exists: that is the one in the graph.
	sha := fields[1]
	if dereferenced := fields[2]; dereferenced != "" {
		sha = dereferenced
	}

	kind, shortName := classifyRef(name)

	ahead, behind, gone := parseUpstreamTrack(fields[4])

	return Ref{
		Name:      name,
		ShortName: shortName,
		Kind:      kind,
		SHA:       sha,
		Upstream:  fields[3],
		Ahead:     ahead,
		Behind:    behind,
		Gone:      gone,
	}, nil
}

// The three namespaces this package spells out, in one place.
//
// They are here rather than typed where they are needed because the same
// strings are read one way and written the other: a ref is classified by its
// prefix, and a branch is handed to git with the prefix put back on. A
// literal in each of those places is a literal that can disagree with the
// others by one character.
const (
	headsPrefix   = "refs/heads/"
	remotesPrefix = "refs/remotes/"
	tagsPrefix    = "refs/tags/"
)

// LocalBranchRef spells a branch name the way git cannot read as anything
// else.
//
// A short name is not a reference: git resolves it through the search order in
// gitrevisions, which reaches refs/tags before refs/heads. A repository
// holding both a branch and a tag called `release` — one release process away
// from ordinary — answers `release` with the tag, and every command given the
// short name then acts on something other than the row the user clicked.
//
// So every command in this package that acts on a local branch the user named
// spells it in full. The full name cannot be misread, and it cannot start with
// a dash either, which is the other thing a name reaching git must never do.
func LocalBranchRef(branch string) string { return headsPrefix + branch }

// TagRef is the full name of a tag: "refs/tags/" + name.
//
// Same reason LocalBranchRef exists: a short name reaches tags before heads in
// git's search order, and a push destination that resolved to a branch would
// be a push of the wrong kind of thing.
func TagRef(name string) string { return tagsPrefix + name }

// ErrBadRefName: a string that must not reach git where git reads a revision.
var ErrBadRefName = errors.New("unusable reference name")

// forbiddenInRefName are the bytes git-check-ref-format rules out, minus the
// ones checked as sequences below.
//
// Every one of them is revision syntax rather than a name: `main^` is a
// parent, `main:path` is a blob, `main*` is a glob. A chosen reference
// carrying any of them would walk something other than the reference somebody
// chose.
const forbiddenInRefName = "~^:?*[\\"

// CheckRefName refuses anything that is not a reference name.
//
// ScopeRefs is the first walk in this package whose revisions come off a query
// string. Everywhere else the revision is an object name checked as
// hexadecimal, or a name the daemon spelled in full itself — see
// LocalBranchRef. Here the client names the refs, and `git log` takes its
// revisions BEFORE any `--`, so there is no separator to hide them behind: a
// `--output=/etc/passwd` in that position is an option and not a ref.
//
// Refused on the name's shape rather than on a list of tricks, because git's
// own rules are narrow enough to check outright. Nothing legitimate is turned
// away: every ref the interface sends came out of ForEachRef, which lists only
// names git itself accepted.
func CheckRefName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%w: none was given", ErrBadRefName)
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("%w: %q starts with a dash, which git reads as an option",
			ErrBadRefName, name)
	case strings.Contains(name, ".."):
		return fmt.Errorf("%w: %q holds \"..\", which git reads as a range of commits",
			ErrBadRefName, name)
	case strings.Contains(name, "@{"):
		return fmt.Errorf("%w: %q holds \"@{\", which git reads as a reflog entry",
			ErrBadRefName, name)
	}

	for _, letter := range name {
		// Space and everything under it, DEL with them: git forbids the
		// control characters, and a NUL or a newline would break the framing
		// every parser in this package depends on as well.
		if letter <= ' ' || letter == 0x7f || strings.ContainsRune(forbiddenInRefName, letter) {
			return fmt.Errorf("%w: %q holds %q, which no reference name may",
				ErrBadRefName, name, letter)
		}
	}
	return nil
}

func classifyRef(name string) (RefKind, string) {
	switch {
	case strings.HasPrefix(name, headsPrefix):
		return RefBranch, strings.TrimPrefix(name, headsPrefix)
	case strings.HasPrefix(name, remotesPrefix):
		return RefRemote, strings.TrimPrefix(name, remotesPrefix)
	case strings.HasPrefix(name, tagsPrefix):
		return RefTag, strings.TrimPrefix(name, tagsPrefix)
	default:
		return RefOther, name
	}
}

// parseUpstreamTrack reads the %(upstream:track) field, which is "[ahead 3]",
// "[ahead 1, behind 2]", "[gone]" or the empty string.
//
// The format is English because commandEnvironment forces LC_ALL=C. Without
// that it would be translated and this parsing would break from one machine
// to the next — exactly the kind of reason the locale is pinned.
func parseUpstreamTrack(track string) (ahead, behind int, gone bool) {
	track = strings.TrimSpace(track)
	if track == "" {
		return 0, 0, false
	}

	track = strings.TrimPrefix(track, "[")
	track = strings.TrimSuffix(track, "]")

	for _, part := range strings.Split(track, ", ") {
		switch {
		case part == "gone":
			gone = true
		case strings.HasPrefix(part, "ahead "):
			ahead = parseCountOrZero(strings.TrimPrefix(part, "ahead "))
		case strings.HasPrefix(part, "behind "):
			behind = parseCountOrZero(strings.TrimPrefix(part, "behind "))
		}
		// An unknown fragment is ignored on purpose: git could add one, and
		// one extra ref in the list beats a history that refuses to render
		// at all.
	}

	return ahead, behind, gone
}

// parseCountOrZero returns 0 for anything that is not a count. git always
// produces a non-negative integer here; if it did not, showing "0 commits
// ahead" still beats failing the load of every branch.
//
// A negative number is unreadable in exactly the same sense, and saying so
// takes a line of its own because Atoi does not: it parses "-1" happily, and
// the -1 travelled all the way into the JSON a branch row renders as "N
// commits ahead". A fuzz seed found it, which is what the round-trip target
// exists for.
func parseCountOrZero(text string) int {
	count, err := strconv.Atoi(text)
	if err != nil || count < 0 {
		return 0
	}
	return count
}
