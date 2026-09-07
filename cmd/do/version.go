package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Deciding what to release, from what was committed.
//
// The rule is Conventional Commits, which CONTRIBUTING.md already requires of
// every commit — so the version is not a number someone picks, it is a
// consequence of what the changes said about themselves. Nobody has to
// remember to bump anything, and nobody can bump it wrongly.
//
// This lives in Go rather than in the workflow for the reason the whole entry
// point moved here: the rule below has eleven cases and each one is a decision
// about what users are promised. Written as shell in a YAML file it would be
// eleven cases nothing ever checked.

// bump is what one commit does to the version. The order matters: a release
// takes the largest bump any of its commits asks for.
type bump int

const (
	bumpNone bump = iota
	bumpPatch
	bumpMinor
	bumpMajor
)

// version is a semantic version. Only the three numbers: this project does not
// publish pre-releases or build metadata, and parsing what we never produce
// would be inventing a case to get wrong.
type version struct {
	major, minor, patch int
}

func (v version) String() string { return fmt.Sprintf("v%d.%d.%d", v.major, v.minor, v.patch) }

// parseVersion reads a tag of the form v1.2.3.
func parseVersion(tag string) (version, error) {
	fields := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(fields) != 3 {
		return version{}, fmt.Errorf("%q is not a vMAJOR.MINOR.PATCH tag", tag)
	}

	numbers := make([]int, 3)
	for index, field := range fields {
		number, err := strconv.Atoi(field)
		if err != nil || number < 0 {
			return version{}, fmt.Errorf("%q is not a vMAJOR.MINOR.PATCH tag", tag)
		}
		numbers[index] = number
	}
	return version{major: numbers[0], minor: numbers[1], patch: numbers[2]}, nil
}

// classify reads one commit message and says what it asks of the version.
//
// The shape is `type(scope)!: subject`, with an optional body. Only four types
// change anything a user can observe; everything else — docs, test, ci, build,
// chore, refactor, style — changes the repository and not the product, and a
// release for one of those is a release nobody can tell apart from the last.
func classify(message string) bump {
	subject, body, _ := strings.Cut(message, "\n")

	// A breaking change is announced two ways, and both are normative. The
	// footer is the one that survives a subject somebody rewrote.
	if strings.Contains(body, "BREAKING CHANGE:") || strings.Contains(body, "BREAKING-CHANGE:") {
		return bumpMajor
	}

	kind, _, found := strings.Cut(subject, ":")
	if !found {
		// Not a conventional commit at all. Old history, or a merge commit.
		// Silently treating it as a fix would ship a release nobody described.
		return bumpNone
	}

	// The scope is not part of the decision, only the type and the bang.
	// `feat(api)!: …` and `feat!: …` say the same thing.
	if breaking := strings.HasSuffix(kind, "!"); breaking {
		return bumpMajor
	}
	if scope := strings.IndexByte(kind, '('); scope >= 0 {
		kind = kind[:scope]
	}

	switch strings.TrimSpace(kind) {
	case "feat":
		return bumpMinor
	case "fix", "perf", "revert":
		return bumpPatch
	default:
		return bumpNone
	}
}

// nextVersion is the version to release, or false when nothing warrants one.
//
// Below 1.0 a breaking change moves the minor rather than the major. That is
// semver's own rule — "anything MAY change at any time" while the major is
// zero — and the alternative is reaching 4.0.0 before there is a product, which
// tells a reader nothing except that the project changed its mind four times.
func nextVersion(current version, messages []string) (version, bool) {
	largest := bumpNone
	for _, message := range messages {
		if asked := classify(message); asked > largest {
			largest = asked
		}
	}

	preOne := current.major == 0

	switch largest {
	case bumpMajor:
		if preOne {
			return version{major: 0, minor: current.minor + 1}, true
		}
		return version{major: current.major + 1}, true
	case bumpMinor:
		return version{major: current.major, minor: current.minor + 1}, true
	case bumpPatch:
		return version{major: current.major, minor: current.minor, patch: current.patch + 1}, true
	default:
		return version{}, false
	}
}

// ---------------------------------------------------------------------------
// The command
// ---------------------------------------------------------------------------

// runVersion prints the version this checkout would be released as, and
// nothing at all when no commit since the last release asks for one.
//
// Stdout carries the version and nothing else, because a workflow reads it:
//
//	next="$(./do version)"
//	[ -n "$next" ] || exit 0
//
// The reasoning goes to stderr, so a person running it by hand is told why the
// answer is what it is rather than being handed a blank line.
func runVersion(p *project, _ []string) error {
	current, releasedAt, err := p.lastRelease()
	if err != nil {
		return err
	}

	messages, err := p.commitsSince(releasedAt)
	if err != nil {
		return err
	}

	next, warranted := nextVersion(current, messages)
	if !warranted {
		info("%s, and none of the %d commits since asks for a release",
			currentDescription(current, releasedAt), len(messages))
		return nil
	}

	info("%s, and %d commits since ask for %s",
		currentDescription(current, releasedAt), len(messages), next)
	fmt.Println(next)
	return nil
}

func currentDescription(current version, releasedAt string) string {
	if releasedAt == "" {
		return "nothing released yet"
	}
	return "released: " + current.String()
}

// lastRelease returns the newest release tag and the revision it points at.
//
// An empty revision means there is no release yet, in which case the whole
// history counts — that is what the first release is.
func (p *project) lastRelease() (version, string, error) {
	// Sorted by version rather than by date: a tag applied late to an old
	// commit must not become "the latest release".
	listed, err := p.capture("git", "tag", "--list", "v*", "--sort=-version:refname")
	if err != nil {
		return version{}, "", err
	}

	for _, tag := range strings.Fields(listed) {
		parsed, err := parseVersion(tag)
		if err != nil {
			// A tag that is not a version is not this command's business.
			continue
		}
		return parsed, tag, nil
	}
	return version{}, "", nil
}

// commitsSince returns one message per commit after the given revision,
// bodies included.
func (p *project) commitsSince(revision string) ([]string, error) {
	// NUL between records, for the same reason internal/git uses it: a commit
	// message contains newlines, and a newline cannot separate things that
	// contain newlines.
	arguments := []string{"log", "--no-merges", "--format=%B%x00"}
	if revision != "" {
		arguments = append(arguments, revision+"..HEAD")
	}

	output, err := p.capture("git", arguments...)
	if err != nil {
		return nil, err
	}

	messages := make([]string, 0)
	for _, record := range strings.Split(output, "\x00") {
		if trimmed := strings.TrimSpace(record); trimmed != "" {
			messages = append(messages, trimmed)
		}
	}
	return messages, nil
}
