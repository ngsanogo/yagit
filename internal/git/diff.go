package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// LineKind is what a unified diff's leading character means.
type LineKind string

const (
	LineContext LineKind = "context"
	LineAdded   LineKind = "added"
	LineRemoved LineKind = "removed"
)

// DiffLine is one line of a hunk's body. Hunk headers are not lines: they are
// the Hunk itself.
type DiffLine struct {
	Kind LineKind `json:"kind"`

	// Text is the line without its leading '+', '-' or ' '. Keeping the
	// marker would mean every consumer had to strip it, and one of them would
	// forget.
	Text string `json:"text"`

	// Index numbers every body line of the file's diff, from zero, across
	// every hunk.
	//
	// This is the address a client uses to say which lines to stage. It has
	// to be stable and it has to be unambiguous, which a (hunk, offset) pair
	// is not once hunks are added or removed — and a line number is not
	// either, since an added and a removed line can share one.
	Index int `json:"index"`

	// OldLine and NewLine are the line's number in each side of the file, or
	// zero where it does not exist on that side.
	OldLine int `json:"old_line"`
	NewLine int `json:"new_line"`

	// Truncated counts the runes of Text that were NOT sent, and is zero for
	// every ordinary line.
	//
	// Nothing in this package ever sets it: a FileDiff read here is whole,
	// because it is what a patch is built from and half a line would apply
	// the wrong content. It is set on the copy the HTTP layer answers with —
	// see boundedForDisplay in internal/api — and it exists on this type
	// only because this type is what crosses the wire.
	Truncated int `json:"truncated,omitempty"`

	// NoNewline records git's "\ No newline at end of file" marker, which
	// belongs to the line before it. A patch that drops the marker adds a
	// newline the file never had.
	NoNewline bool `json:"no_newline"`
}

// Hunk is one contiguous run of changes, with its two line ranges.
type Hunk struct {
	OldStart int `json:"old_start"`
	OldLines int `json:"old_lines"`
	NewStart int `json:"new_start"`
	NewLines int `json:"new_lines"`

	// Heading is the function or section name git puts after the second @@.
	// Decoration, never parsed back.
	Heading string `json:"heading"`

	Lines []DiffLine `json:"lines"`
}

// FileDiff is everything git said about one path.
type FileDiff struct {
	// ID fingerprints the exact bytes git produced for this diff.
	//
	// Partial staging builds a patch out of a diff, and that patch is only
	// correct for that diff: if the file moved on in between, the line numbers
	// no longer describe it. The client sends this value back with its
	// selection, and a mismatch is refused rather than applied to whatever is
	// there now. Empty on a diff that was parsed rather than read, since there
	// is then no run of git to fingerprint.
	ID string `json:"id"`

	Path string `json:"path"`

	// OldPath is set only when the two sides have different names — a rename
	// or a copy.
	OldPath string `json:"old_path,omitempty"`

	// Binary says git refused to show the content. There is nothing to stage
	// line by line, and the interface has to say so rather than draw an empty
	// diff.
	Binary bool `json:"binary"`

	// Added and Removed say the file is absent from one side: a creation, a
	// deletion. Both matter to the patch builder, which has to write
	// /dev/null on the side that has no file — and to the interface, which
	// otherwise shows "every line added" without saying the file is new.
	Added   bool `json:"added"`
	Removed bool `json:"removed"`

	// OldMode and NewMode carry a mode change — 100644 to 100755 is a real
	// change with no line in it, and a diff that showed nothing would be
	// telling the user their change does not exist.
	OldMode string `json:"old_mode,omitempty"`
	NewMode string `json:"new_mode,omitempty"`

	// LFS is set when what this diff shows is a Git LFS pointer rather than
	// the file. Absent for everything else, which is nearly every diff.
	//
	// A pointer is three short lines of metadata, so a diff of one is
	// perfectly readable and perfectly useless: it says an OID changed where
	// the user expected to see their picture change. Naming it is what lets
	// the pane say so — see lfs.go, which owns the reading.
	LFS *LFSDiff `json:"lfs,omitempty"`

	Hunks []Hunk `json:"hunks"`
}

// LFSDiff is what the two sides of a diff point at, where either is a pointer.
//
// Both halves are optional and for the same reason a diff has an Added and a
// Removed flag: a file newly tracked by LFS has a pointer on the new side and
// its real content on the old, and one being untracked is the reverse.
type LFSDiff struct {
	Old *LFSPointer `json:"old,omitempty"`
	New *LFSPointer `json:"new,omitempty"`
}

// Empty says the diff carries nothing to draw: no hunk, no mode change, no
// binary marker.
func (d FileDiff) Empty() bool {
	return len(d.Hunks) == 0 && !d.Binary && d.OldMode == d.NewMode
}

// DiffSide says which of a path's two diffs is wanted.
//
// They are two different questions with two different answers, and a file can
// have both at once: `git add`, then edit again. An interface that offers one
// "diff" for such a file is showing the wrong half of it half the time.
type DiffSide string

const (
	// DiffStaged compares HEAD with the index: what a commit would record.
	DiffStaged DiffSide = "staged"

	// DiffUnstaged compares the index with the work tree: what a commit
	// would leave behind.
	DiffUnstaged DiffSide = "unstaged"

	// DiffUntracked is the whole content of a file git does not track yet,
	// presented as one addition per line.
	DiffUntracked DiffSide = "untracked"
)

// contextLines is how many unchanged lines surround each hunk.
//
// git's own default is 3 and this keeps it. It is not only a display choice:
// the patch built for partial staging carries the same context, and `git
// apply` matches on it. Widening the context here makes patches more likely
// to be refused, not less.
const contextLines = 3

// maxDiffBytes caps how much unified diff one read may produce.
//
// A generated file — a database dump, a lockfile, a minified bundle — is
// ordinary text to git, so a rewritten one answers with a patch the size of
// two copies of it. Every byte of that is then held three times over: git's
// output, the string it is parsed from, and one DiffLine per body line. The
// selected file's diff is re-read on a timer while its row stays selected, so
// an unbounded read is a daemon that grows until the kernel kills it, taking
// every open repository's state with it. Refusing is the honest answer: an
// interface draws two thousand lines of a diff, and nobody reads a hundred
// megabytes of one.
const maxDiffBytes = 10 << 20

// ErrDiffTooLarge: git produced more diff than this package will hold. The
// route turns it into a sentence naming the file, the way FileDiff.Binary
// names "nothing to show line by line".
var ErrDiffTooLarge = errors.New("this diff is too large to show")

// ErrCombinedDiff: git answered with a combined diff, which this package does
// not read and which cannot be staged line by line.
//
// `git diff` on an UNMERGED path does not produce a unified diff at all. It
// produces git's combined format — `diff --cc`, hunks headed `@@@`, and two
// columns of markers per line — because a conflicted path has three versions
// and not two. There is no patch to build from one: `git apply` takes a
// two-sided diff, and there is no such thing as staging half a conflict.
//
// Refused by name rather than left to fail on the `@@@`, which is what used to
// happen: the interface showed `range "@@@" does not start with "-"` over a
// file whose real problem was a merge nobody had finished. The named error is
// what lets the route answer with that sentence instead.
var ErrCombinedDiff = errors.New("this path is unmerged, so git shows a combined diff")

// diffArgs are the switches every diff shares.
//
// --no-color: the interface colors the lines itself, and ANSI escapes in the
// middle of a line's text would be shown verbatim as the text.
//
// --no-ext-diff: a user's configured external diff tool answers in its own
// format, which is not a unified diff at all. yagit reads what it asked for.
//
// --no-textconv: a textconv filter shows a rendered version of a binary file.
// Excellent to read, impossible to apply a patch to.
//
// --src-prefix and --dst-prefix pin the `a/` and `b/` this package parses back
// out. They are user configuration otherwise: diff.mnemonicPrefix writes `i/`
// and `w/`, diff.noprefix writes none at all, and either one turns every diff
// in the interface into an unreadable file header. Pinned by name rather than
// with --default-prefix, which git only understands from 2.45.
var diffArgs = []string{
	"--no-color", "--no-ext-diff", "--no-textconv",
	"--src-prefix=a/", "--dst-prefix=b/",
	"--find-renames",
	"-U" + strconv.Itoa(contextLines),
}

// DiffRequest names the change to read.
//
// A struct rather than two more string parameters: Path and OldPath are the
// same shape and mean opposite things, and a call that swapped them would ask
// for a diff of the name the file no longer has.
type DiffRequest struct {
	// Path is the name the file has now — the one the status listed.
	Path string

	// OldPath is the name it had before, for a rename or a copy, and empty
	// for everything else.
	//
	// It has to travel with the request because a pathspec is applied BEFORE
	// rename detection: `git diff --cached -- new.txt` filters the source
	// side out, git can no longer pair the two halves, and it answers with
	// `new file mode` and every line added. The interface then shows a
	// renamed-and-edited file as a whole new one, FileDiff.OldPath is empty
	// so nothing refuses to take it line by line, and unstaging one of those
	// lines writes an index that holds neither HEAD's content nor the work
	// tree's. Naming both sides is what lets git pair them.
	OldPath string

	Side DiffSide
}

// Diff reads one path's diff on one side.
//
// The paths are passed after `--`, which is what keeps a file named `-f` or
// `HEAD` from being read as an option or a revision, and as literal pathspecs,
// which is what keeps one named `*` or `app/[id].tsx` from being read as a
// pattern.
func (r *Runner) Diff(ctx context.Context, dir string, request DiffRequest) (FileDiff, error) {
	var (
		output []byte
		err    error
	)

	switch request.Side {
	case DiffStaged:
		output, err = r.readDiff(ctx, dir, []string{"diff", "--cached"}, request.pathspecs())

	case DiffUnstaged:
		output, err = r.readDiff(ctx, dir, []string{"diff"}, request.pathspecs())

	case DiffUntracked:
		output, err = r.diffUntracked(ctx, dir, request.Path)

	default:
		return FileDiff{}, fmt.Errorf("unknown diff side %q", request.Side)
	}

	if err != nil {
		return FileDiff{}, err
	}

	files, err := ParseDiff(output)
	if err != nil {
		return FileDiff{}, err
	}

	// A path with nothing to show is not an error: it is what git answers for
	// a file whose only change is on the other side, and for one that was put
	// back the way it was between the status read and this one. The
	// fingerprint still travels, so that staging a selection made against an
	// emptied diff is refused like any other stale one.
	if len(files) == 0 {
		return FileDiff{ID: Fingerprint(output), Path: request.Path, Hunks: []Hunk{}}, nil
	}

	// One path was asked for, so one file comes back. Naming a rename's two
	// halves is what can produce a second entry — git pairs them into one
	// when it detects the rename and lists them apart when it does not — and
	// the first is the one the request named.
	file := files[0]
	file.ID = Fingerprint(output)
	return file, nil
}

// pathspecs is what the request names to git.
//
// The old name goes on the command line too, and only there: git needs both
// halves in the pathspec to pair a rename, and it matches nothing at all on a
// side where the file never had that name — `git diff -- new old` for an
// unstaged change is the same answer as `git diff -- new`.
func (request DiffRequest) pathspecs() []string {
	if request.OldPath == "" || request.OldPath == request.Path {
		return literalPathspecs([]string{request.Path})
	}
	return literalPathspecs([]string{request.Path, request.OldPath})
}

func (r *Runner) readDiff(ctx context.Context, dir string, command, pathspecs []string) ([]byte, error) {
	args := append(append(append([]string{}, command...), diffArgs...), "--")
	return r.Exec(ctx, Command{
		Dir:       dir,
		Args:      append(args, pathspecs...),
		MaxOutput: maxDiffBytes,
	})
}

// diffUntracked shows a file git has never seen, as an addition of every one
// of its lines.
//
// `git diff --no-index -- /dev/null <path>` is how git itself does it, and it
// is the command the log panel shows. /dev/null is a name git handles inside
// diff-no-index rather than a path it opens, so it works on Windows too. The
// two names are file names here and not pathspecs — --no-index reads outside
// a repository, where there is nothing to match against — so no literal magic
// goes in front of them.
//
// The exit code needs a word. `--no-index` follows diff(1) rather than git:
// 0 means the two files are identical, 1 means they differ. So the case this
// function exists for is the one git reports as a failure, and that is why 1
// is listed as a success code rather than checked for afterwards.
//
// OutputRequired is the other half of that. git also exits 1 — not 128 — for a
// path it could not open at all: a file deleted since the status listed it, or
// a nested checkout, which `git status -uall` reports as one directory entry.
// It then writes `error: Could not access …` to stderr and nothing to standard
// output, and without this the interface would draw "no change here" over it.
// Even an empty file answers with a header, so empty output is never an answer.
func (r *Runner) diffUntracked(ctx context.Context, dir, path string) ([]byte, error) {
	return r.Exec(ctx, Command{
		Dir:            dir,
		Args:           append(append([]string{"diff", "--no-index"}, diffArgs...), "--", "/dev/null", path),
		SuccessCodes:   []int{1},
		OutputRequired: true,
		MaxOutput:      maxDiffBytes,
	})
}

// Fingerprint identifies the exact bytes git produced for a diff. It is what
// fills FileDiff.ID; see the note there for what it is for.
func Fingerprint(output []byte) string {
	sum := sha256.Sum256(output)
	return hex.EncodeToString(sum[:])
}

// ParseDiff turns unified diff output into one FileDiff per file.
//
// Pure and exported, for the same reason as ParseLog: a bug here is silent,
// and the fix for that is a test that does not need git.
func ParseDiff(output []byte) ([]FileDiff, error) {
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}

	var (
		files   []FileDiff
		current *FileDiff
		hunk    *Hunk
		// lineIndex numbers body lines within the current file, which is the
		// address partial staging uses. It restarts with each file.
		lineIndex int
		oldLine   int
		newLine   int
	)

	// closeHunk attaches the hunk being read, if any, to the current file.
	closeHunk := func() {
		if hunk != nil {
			current.Hunks = append(current.Hunks, *hunk)
			hunk = nil
		}
	}

	for number, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --cc "), strings.HasPrefix(line, "diff --combined "):
			// Before every other case, and before `current` is consulted:
			// the `--- a/f` line two lines below belongs to a combined diff
			// too, and the headerless-patch branch would otherwise adopt it
			// and read the rest as a unified diff it is not.
			return nil, fmt.Errorf("%w: %q", ErrCombinedDiff, line)

		case strings.HasPrefix(line, "diff --git "):
			closeHunk()
			if current != nil {
				files = append(files, *current)
			}
			started, err := parseGitDiffHeader(line)
			if err != nil {
				return nil, fmt.Errorf("diff line %d: %w", number+1, err)
			}
			current = &started
			lineIndex = 0

		case current == nil && strings.HasPrefix(line, "--- "):
			// A unified diff with no `diff --git` preamble. git always writes
			// one; the patches this package builds do not, and being able to
			// read one back is what lets a patch be checked against the diff
			// it came from.
			//
			// Only ever at the start: inside a hunk a line beginning with
			// `---` is a removed line whose text is `-- …`, and the branches
			// below take it as one. That ordering is what makes this safe,
			// and it is also why one headerless diff is read here rather than
			// several — telling the second file's `--- ` from a removed line
			// needs the count git puts in the preamble.
			current = &FileDiff{Hunks: []Hunk{}}
			lineIndex = 0
			if err := applyExtendedHeader(current, line); err != nil {
				return nil, fmt.Errorf("diff line %d: %w", number+1, err)
			}

		case current == nil:
			// Everything before the first `diff --git` line. A configured
			// pager header or a warning on stdout would land here, and
			// skipping is better than refusing to show the diff.

		case strings.HasPrefix(line, "@@"):
			closeHunk()
			started, err := parseHunkHeader(line)
			if err != nil {
				return nil, fmt.Errorf("diff line %d: %w", number+1, err)
			}
			hunk = &started
			oldLine, newLine = started.OldStart, started.NewStart

		case hunk == nil:
			// Between the file header and the first hunk: the extended header
			// lines, which say what happened to the file as a whole.
			if err := applyExtendedHeader(current, line); err != nil {
				return nil, fmt.Errorf("diff line %d: %w", number+1, err)
			}

		case line == "":
			// git writes a context line for an empty line as a single space,
			// but a diff that has been through a mailer or a text field can
			// arrive with that space stripped. Reading it as the end of the
			// hunk would silently drop every line after it.
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: LineContext, Text: "", Index: lineIndex,
				OldLine: oldLine, NewLine: newLine,
			})
			lineIndex++
			oldLine++
			newLine++

		case line[0] == '\\':
			// "\ No newline at end of file" describes the line before it.
			if count := len(hunk.Lines); count > 0 {
				hunk.Lines[count-1].NoNewline = true
			}

		case line[0] == ' ':
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: LineContext, Text: line[1:], Index: lineIndex,
				OldLine: oldLine, NewLine: newLine,
			})
			lineIndex++
			oldLine++
			newLine++

		case line[0] == '+':
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: LineAdded, Text: line[1:], Index: lineIndex,
				NewLine: newLine,
			})
			lineIndex++
			newLine++

		case line[0] == '-':
			hunk.Lines = append(hunk.Lines, DiffLine{
				Kind: LineRemoved, Text: line[1:], Index: lineIndex,
				OldLine: oldLine,
			})
			lineIndex++
			oldLine++

		default:
			return nil, fmt.Errorf(
				"diff line %d: %q starts with none of ' ', '+', '-', '\\'", number+1, line)
		}
	}

	closeHunk()
	if current != nil {
		files = append(files, *current)
	}

	// Every diff a person is shown comes through here, so this is where a
	// pointer is recognised: a route that had to remember to ask would be a
	// route that eventually forgets, and the failure is a pane showing three
	// lines of metadata where somebody expected their picture. Pure, bounded
	// at the pointer size, and nearly always an immediate no — see DetectLFS.
	for index := range files {
		files[index].LFS = DetectLFS(files[index])
	}
	return files, nil
}

// parseGitDiffHeader reads `diff --git a/old b/new`.
//
// The two paths are not authoritative: a path holding " b/" makes this line
// ambiguous, which is exactly why git repeats the names in the `--- ` and
// `+++ ` lines and in `rename from`/`rename to`. Those overwrite what is read
// here. This line is used because it is the one that always exists, including
// for a binary file and for a mode-only change, neither of which has a `---`
// line to correct the guess.
func parseGitDiffHeader(line string) (FileDiff, error) {
	left, right, ok := splitHeaderNames(strings.TrimPrefix(line, "diff --git "))
	if !ok {
		return FileDiff{}, fmt.Errorf("unreadable file header %q", line)
	}

	oldPath, err := headerPath(left)
	if err != nil {
		return FileDiff{}, fmt.Errorf("file header %q: %w", line, err)
	}
	newPath, err := headerPath(right)
	if err != nil {
		return FileDiff{}, fmt.Errorf("file header %q: %w", line, err)
	}

	file := FileDiff{Path: newPath, OldPath: oldPath, Hunks: []Hunk{}}
	if file.OldPath == file.Path {
		// A path unchanged by the diff is not a rename, and carrying OldPath
		// for one would make every consumer test the two for equality.
		file.OldPath = ""
	}
	return file, nil
}

// splitHeaderNames cuts `a/old b/new` into its two names, quoting included.
//
// The separator is a single space and both names may hold one, so the cut has
// to be made on what the shape of the line allows rather than on the first
// space that turns up:
//
//   - A quoted name ends at its closing quote, which is exact.
//   - An unquoted name holds no `"` — a quote is one of the bytes that force
//     quoting — so the first ` "` after it is where the second name starts.
//   - With both names unquoted and equal, which is the overwhelming majority,
//     cutting in the middle is exact whatever the path holds.
//
// Only a rename of two unquoted names holding " b/" is left ambiguous, and
// that one is corrected by the `--- `/`+++ ` lines.
func splitHeaderNames(rest string) (left, right string, ok bool) {
	if strings.HasPrefix(rest, `"`) {
		end, closed := endOfQuoted(rest)
		if !closed || end >= len(rest) || rest[end] != ' ' {
			return "", "", false
		}
		return rest[:end], rest[end+1:], true
	}

	if at := strings.Index(rest, ` "`); at >= 0 {
		return rest[:at], rest[at+1:], true
	}

	if half := len(rest) / 2; len(rest)%2 == 1 && rest[half] == ' ' {
		if candidateLeft, candidateRight := rest[:half], rest[half+1:]; sameNameBothSides(candidateLeft, candidateRight) {
			return candidateLeft, candidateRight, true
		}
	}

	left, right, found := strings.Cut(rest, " b/")
	if !found {
		return "", "", false
	}
	return left, "b/" + right, true
}

// sameNameBothSides says the two halves are one name under the two prefixes
// diffArgs pins. Comparing them whole never matches, since they differ in
// their first byte by construction — which is how the fast path above came to
// be dead code that let a binary file under a directory named `dir b` be
// reported as a rename.
func sameNameBothSides(left, right string) bool {
	return strings.HasPrefix(left, "a/") && strings.HasPrefix(right, "b/") &&
		left[2:] == right[2:]
}

// endOfQuoted returns the index just past the closing quote of the C-style
// quoted string at the start of s.
func endOfQuoted(s string) (int, bool) {
	for index := 1; index < len(s); index++ {
		switch s[index] {
		case '\\':
			// The escaped byte cannot be the closing quote.
			index++
		case '"':
			return index + 1, true
		}
	}
	return 0, false
}

// headerPath turns one name from a header line into the path it stands for:
// git's disambiguating TAB, then its C-style quoting, then the a//b/ prefix.
func headerPath(name string) (string, error) {
	// git terminates the name with a TAB when it holds a space, so that a
	// traditional patch reader can find where the name ends. A file whose
	// name really ends in a tab is quoted, and that tab is written \t inside
	// the quotes — so a raw trailing one is always git's and never the path's.
	unquoted, err := unquotePath(strings.TrimSuffix(name, "\t"))
	if err != nil {
		return "", err
	}
	return stripDiffPrefix(unquoted), nil
}

// unquotePath undoes git's C-style quoting, which it applies to any name it
// cannot write plainly.
//
// There is no configuration that turns all of it off: core.quotePath defaults
// to true and quotes every byte above ASCII, and a control character or a `"`
// is quoted whatever it is set to. Without this, a repository holding one file
// named `café.txt` — ordinary in most of the world — answers "unreadable file
// header" for that file's diff and for every commit that touches it.
func unquotePath(name string) (string, error) {
	if !strings.HasPrefix(name, `"`) {
		return name, nil
	}
	if len(name) < 2 || !strings.HasSuffix(name, `"`) {
		return "", fmt.Errorf("quoted path %q has no closing quote", name)
	}

	body := name[1 : len(name)-1]
	var out strings.Builder
	out.Grow(len(body))

	for index := 0; index < len(body); index++ {
		if body[index] != '\\' {
			out.WriteByte(body[index])
			continue
		}
		index++
		if index >= len(body) {
			return "", fmt.Errorf("quoted path %q ends inside an escape", name)
		}
		switch escaped := body[index]; escaped {
		case 'a':
			out.WriteByte('\a')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'v':
			out.WriteByte('\v')
		case '\\', '"':
			out.WriteByte(escaped)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// Exactly three octal digits, which is what git writes and all it
			// reads back. Every byte above ASCII arrives this way, so one
			// accented character is two of these and an emoji is four.
			if index+2 >= len(body) {
				return "", fmt.Errorf("quoted path %q ends inside an octal escape", name)
			}
			value, err := strconv.ParseUint(body[index:index+3], 8, 8)
			if err != nil {
				return "", fmt.Errorf("quoted path %q holds a bad octal escape: %w", name, err)
			}
			out.WriteByte(byte(value))
			index += 2
		default:
			return "", fmt.Errorf("quoted path %q holds the unknown escape \\%c", name, escaped)
		}
	}
	return out.String(), nil
}

// stripDiffPrefix removes the "a/" or "b/" git puts in front of a path.
func stripDiffPrefix(path string) string {
	if len(path) > 2 && (path[0] == 'a' || path[0] == 'b') && path[1] == '/' {
		return path[2:]
	}
	return path
}

// applyExtendedHeader reads the lines between a file header and its first
// hunk. Anything unrecognized is ignored: git adds header lines, and one it
// adds tomorrow must not stop a diff from being shown today.
//
// A name it cannot read is a different matter and is reported: a path that
// came back wrong is a path the interface shows and the patch builder refuses,
// with a message about a rename nobody performed.
func applyExtendedHeader(file *FileDiff, line string) error {
	switch {
	case strings.HasPrefix(line, "old mode "):
		file.OldMode = strings.TrimPrefix(line, "old mode ")
	case strings.HasPrefix(line, "new mode "):
		file.NewMode = strings.TrimPrefix(line, "new mode ")
	case strings.HasPrefix(line, "new file mode "):
		file.NewMode = strings.TrimPrefix(line, "new file mode ")
		file.Added = true
	case strings.HasPrefix(line, "deleted file mode "):
		file.OldMode = strings.TrimPrefix(line, "deleted file mode ")
		file.Removed = true

	case strings.HasPrefix(line, "rename from "),
		strings.HasPrefix(line, "copy from "):
		name, err := renameName(line, "rename from ", "copy from ")
		if err != nil {
			return err
		}
		file.OldPath = name
	case strings.HasPrefix(line, "rename to "),
		strings.HasPrefix(line, "copy to "):
		name, err := renameName(line, "rename to ", "copy to ")
		if err != nil {
			return err
		}
		file.Path = name

	case strings.HasPrefix(line, "Binary files "), line == "GIT binary patch":
		file.Binary = true

	case strings.HasPrefix(line, "--- "):
		// /dev/null on this side means the file is new, and there is no old
		// path to speak of. `git diff --no-index` writes this line and no
		// "new file mode", so it is the only marker for an untracked file.
		raw := strings.TrimPrefix(line, "--- ")
		if raw == "/dev/null" {
			file.Added = true
			break
		}
		path, err := headerPath(raw)
		if err != nil {
			return err
		}
		file.OldPath = path

	case strings.HasPrefix(line, "+++ "):
		raw := strings.TrimPrefix(line, "+++ ")
		if raw == "/dev/null" {
			file.Removed = true
			break
		}
		path, err := headerPath(raw)
		if err != nil {
			return err
		}
		file.Path = path
	}

	// A path unchanged by the diff is not a rename, and carrying OldPath for
	// one would make every consumer test the two for equality.
	if file.OldPath == file.Path {
		file.OldPath = ""
	}
	return nil
}

// renameName reads the path out of a `rename from`/`copy to` line. git quotes
// these the same way it quotes every other name it writes, and carries no
// disambiguating TAB on them — there is only one name on the line.
func renameName(line string, prefixes ...string) (string, error) {
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return unquotePath(strings.TrimPrefix(line, prefix))
		}
	}
	return "", fmt.Errorf("unreadable rename header %q", line)
}

// parseHunkHeader reads `@@ -oldStart,oldLines +newStart,newLines @@ heading`.
//
// The counts are optional and default to 1 — that is in the format, not a
// convenience: a hunk touching a single line is written `@@ -4 +4 @@`.
func parseHunkHeader(line string) (Hunk, error) {
	body, heading, found := strings.Cut(strings.TrimPrefix(line, "@@ "), " @@")
	if !found {
		return Hunk{}, fmt.Errorf("unreadable hunk header %q", line)
	}

	oldRange, newRange, found := strings.Cut(body, " ")
	if !found {
		return Hunk{}, fmt.Errorf("hunk header %q has one range, expected two", line)
	}

	oldStart, oldLines, err := parseHunkRange(oldRange, '-')
	if err != nil {
		return Hunk{}, fmt.Errorf("hunk header %q: %w", line, err)
	}

	newStart, newLines, err := parseHunkRange(newRange, '+')
	if err != nil {
		return Hunk{}, fmt.Errorf("hunk header %q: %w", line, err)
	}

	return Hunk{
		OldStart: oldStart,
		OldLines: oldLines,
		NewStart: newStart,
		NewLines: newLines,
		Heading:  strings.TrimPrefix(heading, " "),
		Lines:    []DiffLine{},
	}, nil
}

func parseHunkRange(field string, sign byte) (start, count int, err error) {
	if len(field) == 0 || field[0] != sign {
		return 0, 0, fmt.Errorf("range %q does not start with %q", field, string(sign))
	}

	startField, countField, hasCount := strings.Cut(field[1:], ",")

	start, err = strconv.Atoi(startField)
	if err != nil {
		return 0, 0, fmt.Errorf("range start %q: %w", startField, err)
	}

	if !hasCount {
		return start, 1, nil
	}

	count, err = strconv.Atoi(countField)
	if err != nil {
		return 0, 0, fmt.Errorf("range length %q: %w", countField, err)
	}
	return start, count, nil
}
