package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Git LFS: large files kept outside the object database, with a pointer
// committed in their place.
//
// Two halves, and only one of them needs the git-lfs program to exist.
//
// What does not: RECOGNISING a pointer. It is a small text file in a format
// the LFS specification pins, so a diff of one is three lines of metadata
// where the user expected their file — and a client that draws them raw has
// shown its user something worse than nothing. Reading it is pure parsing,
// works on a machine with no git-lfs at all, and is what makes the diff pane
// honest about what it is looking at.
//
// What does: tracking a pattern, and everything that moves bytes. `git lfs
// track` is a git-lfs subcommand, and on a machine without it `git lfs`
// answers "'lfs' is not a git command" — a true sentence about the wrong
// problem. So availability is read first and named.
//
// Nothing here touches fetch, pull, push or checkout. LFS installs itself into
// git as a filter, and git runs it; yagit drives git and therefore already
// carries LFS content without a line of code, which is the whole reason those
// three commands are not in this file.

// ErrLFSUnavailable: git-lfs is not installed on this machine.
var ErrLFSUnavailable = errors.New("git-lfs is not installed")

// ErrEmptyLFSPattern: nothing was given to track.
var ErrEmptyLFSPattern = errors.New("no pattern given to track")

// lfsPointerVersionPrefix is the first line of every pointer file, and the
// cheapest way to tell one from a file that happens to start with "version".
//
// The specification pins the URL and pins it as the FIRST line; later versions
// would change the path after the host, which is why the prefix stops at the
// host rather than matching the whole line.
const lfsPointerVersionPrefix = "version https://git-lfs.github.com/spec/"

// lfsPointerMaxBytes bounds what is examined for a pointer.
//
// The specification caps a pointer file at well under a kilobyte; anything
// larger is a real file whose first line happens to look like one, and reading
// further would mean scanning a megabyte of somebody's data to answer no.
const lfsPointerMaxBytes = 1024

// LFSPointer is what a committed LFS pointer says about the file it stands
// for.
type LFSPointer struct {
	// OID is the object identifier, `sha256:<64 hex>` as the file spells it.
	OID string `json:"oid"`

	// Size is the real file's size in bytes — the number the interface shows
	// in place of a diff nobody can read.
	Size int64 `json:"size"`
}

// ParseLFSPointer reads a pointer file, and says whether that is what it was.
//
// Pure and exported for the reason ParseLog is: getting it wrong produces a
// plausible wrong answer — a file described as three hundred megabytes when it
// is a text file about LFS — rather than a crash.
//
// The specification's rules that matter here: the first line is the version,
// every line is `key value` separated by a single space and terminated by \n,
// the keys after the first are in alphabetical order, `oid` and `size` are
// both required, and the only other keys allowed are `ext-*`. Order is not
// enforced; a pointer git-lfs wrote satisfies it anyway, and refusing one that
// does not would be refusing to READ something perfectly clear.
//
// A key that is none of those IS refused, and that strictness is what makes
// this function safe to call on text nobody has established is a whole file.
// Its caller cannot establish that — a unified diff does not carry the length
// of the file it describes (see sideOf) — so the grammar has to be the guard:
// the first seven lines of a large document about Git LFS parse as a pointer
// the moment an unrecognised line is merely skipped, and the pane would then
// report somebody's prose as three hundred megabytes stored elsewhere.
func ParseLFSPointer(content []byte) (LFSPointer, bool) {
	if len(content) > lfsPointerMaxBytes {
		return LFSPointer{}, false
	}
	text := string(content)
	if !strings.HasPrefix(text, lfsPointerVersionPrefix) {
		return LFSPointer{}, false
	}

	var pointer LFSPointer
	var sized bool
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, " ")
		if !found {
			return LFSPointer{}, false
		}
		switch {
		case key == "oid":
			pointer.OID = value
		case key == "size":
			size, err := strconv.ParseInt(value, 10, 64)
			if err != nil || size < 0 {
				return LFSPointer{}, false
			}
			pointer.Size = size
			sized = true
		case key == "version":
			// Already checked as a prefix of the whole text, which is what
			// pins it to the FIRST line. Named here so it is not mistaken for
			// the unknown key below.
		case strings.HasPrefix(key, "ext-"):
			// The specification's extension keys. Read by nothing here and
			// not a reason to refuse a pointer git-lfs itself may have
			// written.
		default:
			return LFSPointer{}, false
		}
	}

	// Both are required by the specification, and a pointer missing either
	// describes no file at all. Size is tracked with a flag rather than read
	// off the field: zero is a legitimate size — an empty file tracked by LFS
	// has a pointer like any other — so an absent `size` and a size of nought
	// are the same number and not the same answer, and the interface would
	// print "0 bytes" for a file it knows nothing about.
	if pointer.OID == "" || !sized {
		return LFSPointer{}, false
	}
	return pointer, true
}

// LFSSupport is what this machine and this repository can do about LFS.
type LFSSupport struct {
	// Installed says `git lfs` answers. Everything that moves bytes needs it;
	// recognising a pointer does not.
	Installed bool `json:"installed"`

	// Version is what git-lfs calls itself, empty when it is not installed.
	Version string `json:"version"`

	// Patterns are the path patterns .gitattributes routes through the LFS
	// filter, in the order they are declared.
	Patterns []string `json:"patterns"`
}

// InUse says this repository has at least one pattern going through LFS.
func (s LFSSupport) InUse() bool { return len(s.Patterns) > 0 }

// LFSVersion asks the machine whether git-lfs is installed.
//
// A failure is the answer rather than something to report: `git lfs version`
// on a machine without it exits non-zero with "'lfs' is not a git command",
// which is true and about the wrong problem.
func (r *Runner) LFSVersion(ctx context.Context) (string, bool) {
	output, err := r.Run(ctx, "", "lfs", "version")
	if err != nil {
		return "", false
	}
	version := strings.TrimSpace(string(output))
	return version, version != ""
}

// ParseLFSPatterns reads the patterns a .gitattributes routes through LFS.
//
// Pure, because the file is a work-tree file and reading one is not this
// package's job — internal/edit owns that, and the route composes the two.
//
// A line is `<pattern> <attr>…`, and the pattern may be quoted when it holds a
// space, which is exactly what `git lfs track "*.psd"` writes. What marks the
// line is `filter=lfs`: `diff=lfs` and `merge=lfs` travel with it, but the
// filter is the one that decides where the bytes live.
func ParseLFSPatterns(content []byte) []string {
	patterns := make([]string, 0, 4)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := splitAttributeLine(line)
		if len(fields) < 2 {
			continue
		}
		for _, attribute := range fields[1:] {
			if attribute == "filter=lfs" {
				patterns = append(patterns, fields[0])
				break
			}
		}
	}
	return patterns
}

// splitAttributeLine splits a .gitattributes line into its pattern and its
// attributes, honouring the quotes git uses for a pattern holding a space.
//
// git's own parser is C-style quoting; this handles the one case it produces
// in practice — a whole field wrapped in double quotes — and treats anything
// else as whitespace-separated. Getting an exotic escape wrong here means one
// pattern reads oddly in a list, not a command that does the wrong thing:
// nothing built from this is ever passed to git.
func splitAttributeLine(line string) []string {
	fields := make([]string, 0, 4)
	var current strings.Builder
	quoted := false

	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}

	for _, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
		case (r == ' ' || r == '\t') && !quoted:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return fields
}

// TrackLFSArgs is the command TrackLFS runs.
//
// The pattern lands after `--` so one beginning with a dash arrives as a
// pattern. `git lfs track` writes .gitattributes and stages nothing: the
// change it makes is a file the user still has to commit.
func TrackLFSArgs(pattern string) []string {
	return []string{"lfs", "track", "--", pattern}
}

// UntrackLFSArgs is the command UntrackLFS runs.
func UntrackLFSArgs(pattern string) []string {
	return []string{"lfs", "untrack", "--", pattern}
}

// TrackLFS routes a path pattern through LFS, by writing .gitattributes.
//
// Availability is checked first rather than left to git: without git-lfs the
// answer is "'lfs' is not a git command", which reads as a broken yagit
// rather than as a program somebody has not installed.
func (r *Runner) TrackLFS(ctx context.Context, dir, pattern string) error {
	return r.runLFSPattern(ctx, dir, pattern, TrackLFSArgs)
}

// UntrackLFS takes a pattern back out of .gitattributes.
//
// Existing files stay pointers until they are checked out again: untracking
// changes where FUTURE content goes, and saying otherwise would be a promise
// git-lfs does not make.
func (r *Runner) UntrackLFS(ctx context.Context, dir, pattern string) error {
	return r.runLFSPattern(ctx, dir, pattern, UntrackLFSArgs)
}

func (r *Runner) runLFSPattern(
	ctx context.Context, dir, pattern string, args func(string) []string,
) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ErrEmptyLFSPattern
	}
	if _, ok := r.LFSVersion(ctx); !ok {
		return fmt.Errorf("%w, so patterns cannot be tracked from here", ErrLFSUnavailable)
	}
	_, err := r.Run(ctx, dir, args(pattern)...)
	return err
}

// DetectLFS reads the two sides of a diff and reports either that is a
// pointer.
//
// The sides are rebuilt from the hunks rather than read again from git: the
// diff in hand IS the content for a file this small — a pointer is three lines
// and always arrives whole — and a second `git show` per file in a changed
// list would be one subprocess per row for an answer that is nearly always no.
//
// Bounded at the pointer size for the same reason ParseLFSPointer is: a real
// file whose first line looks like a pointer must cost a comparison, not a
// copy of its whole content.
func DetectLFS(diff FileDiff) *LFSDiff {
	if diff.Binary {
		// git decided there is nothing to show, so there are no lines to read.
		// A pointer is text and never lands here.
		return nil
	}

	old, oldFromStart := sideOf(diff, LineRemoved)
	current, newFromStart := sideOf(diff, LineAdded)

	var found LFSDiff
	if oldFromStart {
		if pointer, ok := ParseLFSPointer(old); ok {
			found.Old = &pointer
		}
	}
	if newFromStart {
		if pointer, ok := ParseLFSPointer(current); ok {
			found.New = &pointer
		}
	}
	if found.Old == nil && found.New == nil {
		return nil
	}
	return &found
}

// sideOf rebuilds one side of a diff from its hunks, and says whether what
// came back starts at the first line of the file.
//
// fromStart, and NOT "whole", which is the thing this cannot answer: a unified
// diff carries where each hunk begins and how many lines it covers, and never
// how long the file is. A single hunk `@@ -1,7 +1,7 @@` over a five thousand
// line document is one hunk starting at line 1, and the seven lines it hands
// back are seven lines out of five thousand.
//
// So this prunes, and ParseLFSPointer decides. What is ruled out here is
// everything that cannot be a pointer for a structural reason — more than one
// hunk, a side with no lines, a side that does not begin at line 1, more bytes
// than the specification allows a pointer — and what is ruled IN is decided by
// the grammar, which refuses a line that is not one of a pointer's four kinds.
// A prefix of a longer file therefore fails on its first ordinary line of
// prose, which is the guarantee the row on screen actually rests on.
func sideOf(diff FileDiff, changed LineKind) (content []byte, fromStart bool) {
	if len(diff.Hunks) != 1 {
		return nil, false
	}
	hunk := diff.Hunks[0]

	start, count := hunk.OldStart, hunk.OldLines
	if changed == LineAdded {
		start, count = hunk.NewStart, hunk.NewLines
	}
	if count == 0 {
		// The side has no lines at all: the file is being created or deleted,
		// and there is nothing here to be a pointer.
		return nil, false
	}
	if start != 1 {
		return nil, false
	}

	var builder strings.Builder
	for _, line := range hunk.Lines {
		if line.Kind != LineContext && line.Kind != changed {
			continue
		}
		if builder.Len() > lfsPointerMaxBytes {
			return nil, false
		}
		builder.WriteString(line.Text)
		builder.WriteString("\n")
	}
	return []byte(builder.String()), true
}
