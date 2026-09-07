package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Setting the working directory aside, and putting it back.
//
// A stash is a commit that is not on any branch, held in a reflog under
// refs/stash. That is what makes this file different from every other
// operation in this package: a stash has no name of its own. It is addressed
// as stash@{0}, stash@{1}, … — a POSITION in a stack — and the position of a
// given stash changes whenever another is pushed onto the stack or dropped out
// of the middle of it.
//
// So the confirmation problem the rest of this package solves with
// agreesOnCommit does not work here. A dialog that read stash@{2} and comes
// back to drop stash@{2} may be dropping something else entirely, and the
// command it showed would have been perfectly correct at the moment it was
// drawn. StashTarget is the answer: the plan resolves the position to the
// object name at it, the run sends that name back, and the position is read
// again and refused if it no longer holds it. See docs/adr/0028.
//
// Two of git's own behaviours here succeed while doing nothing, and each is
// refused or worked around rather than passed on:
//
//   - `git stash push` with nothing but untracked files, and no
//     --include-untracked, prints "No local changes to save" and exits 0. The
//     files stay where they are and a caller reading only the exit code
//     believes they were saved. PushStash refuses that combination and names
//     the flag that would have worked — on the command rather than on the
//     preview, so that the one dialog able to offer the flag still opens.
//
//   - `git stash show` on a stash whose only content is untracked files prints
//     nothing and exits 0, because those files hang off a third parent it does
//     not look at without --include-untracked. That parent is read explicitly
//     here, so no stash can be inspected into an empty screen — and the
//     wrapper is not used at all, for a second reason given above ShowStash.
//
// A conflicting apply is neither of those. It exits 1, and the failure is the
// answer: git wrote the markers, kept the entry, and said so — on stdout,
// which a failed command borrows into its stderr for exactly this reason (see
// Exec). So the refusal travels whole, the way a conflicting merge's does, and
// the interface resolves it. What it does NOT come with is an operation: git
// records no MERGE_HEAD for a stash, so there is nothing to abort or continue
// and the conflict is finished by editing the file and staging it.

// Stash is one entry of the stack, as the interface draws it.
type Stash struct {
	// Index is the position in the stack: stash@{Index}, counted from the top,
	// where 0 is the most recent. It is both what a person reads and what git
	// takes as an argument — which is exactly the problem this file is about,
	// since the same number names a different stash after any push or drop.
	Index int `json:"index"`

	// SHA is the full object name of the stash commit. The identity the index
	// is not: it does not move, and it is what a plan is checked against
	// before the command it described is run.
	SHA string `json:"sha"`

	// Message is what the user typed, or the one git wrote for them.
	//
	// Git's reflog subject carries the branch as a prefix — "On main: fix the
	// parser", "WIP on main: 5956208 feat(cherry-pick): …" — and this is what
	// is left after Branch below has been taken off the front, because a list
	// of rows all starting with "On main:" is a list nobody can scan.
	Message string `json:"message"`

	// Branch is the branch the stash was made on, or empty where it was made
	// on a detached HEAD — which git records as the literal "(no branch)".
	// Empty rather than that string, so the interface can decide whether to
	// draw anything at all instead of drawing a branch nobody can check out.
	Branch string `json:"branch"`

	// Date is when the stash was made, from the commit's author date.
	Date time.Time `json:"date"`
}

// Ref is the argument git takes for this stash: stash@{2}.
//
// A method rather than a field, because it is the index said another way and
// two copies of one fact drift. Every command in this file that names a stash
// goes through it.
func (s Stash) Ref() string { return stashRef(s.Index) }

// stashRef writes a position the way git reads one.
//
// The index is an int and cannot carry a dash, a NUL or anything else
// checkRevision exists to refuse, which is why nothing here checks it: the
// only value that could reach git is a number, and a negative one is refused
// by checkStashIndex before it is ever formatted.
func stashRef(index int) string { return "stash@{" + strconv.Itoa(index) + "}" }

// ErrNoStash: there is no stash at that position.
//
// A stack of two has no stash@{5}, and a stack that was emptied in another
// terminal has no stash@{0} either. git answers "stash@{5} is not a valid
// reference", which is true and says nothing about the list the user is
// looking at — so the reading happens here, against the list, before any
// command is assembled.
var ErrNoStash = errors.New("there is no stash at that position")

// ErrStashMoved: the position no longer holds the stash the plan described.
//
// The one refusal this file exists for. A stash pushed or dropped in another
// window renumbers every entry below it, so a confirmation showing
// `git stash drop stash@{1}` can come back to a stack where stash@{1} is
// somebody else's work. Told apart from ErrNoStash because the answer differs:
// this one means read the list again, and the stash is still there.
var ErrStashMoved = errors.New("this stash is no longer at that position")

// ErrNothingToStash: there is nothing in the work tree to set aside.
//
// Refused rather than run, because `git stash push` with nothing to save
// exits 0 and does nothing — the one outcome an interface cannot tell from
// success. The reason travels with it: a work tree holding only untracked
// files is the case where the command succeeds, saves nothing, and looks
// exactly like a stash that worked.
var ErrNothingToStash = errors.New("there is nothing to stash")

// ErrStashMessageLine: the message would not survive being written down.
//
// A stash's description lives in a reflog entry, and a reflog entry is one
// line. git takes a message with a newline in it, stores the whole thing on
// the commit, and flattens it for the reflog — so the list comes back saying
// something the user did not type, with no error anywhere. Refused here
// instead.
var ErrStashMessageLine = errors.New("a stash message must be a single line")

// errUnknownStashApplyMode names the two, because a refusal here is somebody
// being told which words the field takes.
var errUnknownStashApplyMode = errors.New("the stash apply mode must be apply or pop")

// StashApplyMode is whether the entry survives being put back.
//
// Two words that are one git subcommand each, and the difference is not a
// detail: `pop` removes the stash from the stack and `apply` leaves it there.
// A choice the client makes rather than a fact the daemon reads — the shape
// ResetMode and PullStrategy have — so it is always pinned on the command.
type StashApplyMode string

const (
	// StashApplyKeep is `git stash apply`: the work comes back and the entry
	// stays in the stack, so the same stash can be put on another branch.
	StashApplyKeep StashApplyMode = "apply"

	// StashApplyPop is `git stash pop`: the work comes back and the entry is
	// removed. A pop that ends in a conflict keeps it instead — and fails, so
	// there is no success in which the entry unexpectedly survives.
	StashApplyPop StashApplyMode = "pop"
)

// ParseStashApplyMode reads a mode sent by a client.
//
// An unknown one is refused rather than read as apply, for the reason
// ParseResetMode refuses one: running the other command would look entirely
// correct to a client that asked for this one, and it would remove a stash
// nobody agreed to remove.
func ParseStashApplyMode(raw string) (StashApplyMode, error) {
	switch StashApplyMode(raw) {
	case StashApplyKeep, StashApplyPop:
		return StashApplyMode(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownStashApplyMode)
	default:
		return "", fmt.Errorf("%w: %q is not one", errUnknownStashApplyMode, raw)
	}
}

// StashPushArgs is the command PushStash runs.
//
// Exported for the reason ResetArgs is: the line the user is shown and the
// line git receives have one definition between them.
//
// `push` is written out. `git stash` on its own is `git stash push`, and a
// line on screen that leaves the verb off is a line that says less than the
// one that runs.
//
// The message goes in as `--message=…`, one argument, rather than as two.
// git's option parser takes the next argv element as the value of a separate
// `--message`, so `--message -x` does store "-x" — but the joined form is the
// one that cannot be read any other way, and a stash called "-f" is a thing a
// person can type.
//
// No `--`. It would only be needed to separate pathspecs, and this never sends
// any: a stash of selected paths is a different feature with a different
// confirmation, and adding an empty separator now would put a token on screen
// that explains nothing.
func StashPushArgs(message string, untracked bool) []string {
	args := []string{"stash", "push"}
	if untracked {
		args = append(args, "--include-untracked")
	}
	if message != "" {
		args = append(args, "--message="+message)
	}
	return args
}

// StashApplyArgs is the command ApplyStash runs: `git stash apply stash@{2}`
// or `git stash pop stash@{2}`.
//
// The mode is the subcommand, so there is no flag to forget and no default to
// inherit — `git stash pop` and `git stash apply` are two names for two
// different promises about whether the entry is still there afterwards.
func StashApplyArgs(mode StashApplyMode, index int) []string {
	return []string{"stash", string(mode), stashRef(index)}
}

// StashDropArgs is the command DropStash runs.
//
// The position, not the object name. `git stash drop` refuses a raw SHA — it
// takes a stash reference, because dropping is an edit of the reflog rather
// than of the object database — which is the whole reason StashTarget has to
// check the position against the name before this line is allowed to run.
func StashDropArgs(index int) []string {
	return []string{"stash", "drop", stashRef(index)}
}

// stashListFormat produces one record per entry: four NUL-separated fields,
// terminated by NUL and a newline, exactly as logFormat is.
//
// `git stash list` is `git log` over a reflog, so it takes the same --format.
// %gd is the reflog selector — "stash@{2}" — and %gs its subject, the line
// git wrote when the entry was made.
//
// %gs cannot hold a newline: a reflog is a line-based file and git flattens
// anything that would break it. The NUL separators are here anyway, for the
// reason logFormat gives — the day somebody adds %B to this list, splitting by
// line becomes wrong, and that is the kind of change that passes review
// because everything still worked the day before.
const stashListFormat = "%H%x00%gd%x00%gs%x00%aI%x00%x0a"

// stashFieldCount must stay in sync with stashListFormat. A mismatch is an
// explicit parse error rather than a silent shift of the fields.
const stashFieldCount = 4

// Stashes reads the stack, newest first.
//
// An empty stack is not an error and not a special case: `git stash list` in a
// repository that never stashed prints nothing and exits 0, which parses to no
// entries. A repository with no commits yet answers the same way — there is
// nothing to say about a stack that cannot exist — so this is safe to call
// before anything else has been read.
func (r *Runner) Stashes(ctx context.Context, dir string) ([]Stash, error) {
	// --pretty=format: rather than --format=, so the framing is character for
	// character the one Log produces and splitRecords is written against.
	// They differ by a newline, which is exactly the sort of difference that
	// costs an afternoon.
	output, err := r.Run(ctx, dir, "stash", "list", "--pretty=format:"+stashListFormat)
	if err != nil {
		return nil, err
	}
	return ParseStashList(output)
}

// ParseStashList reads stashListFormat's records.
//
// Pure and exported for the reason ParseLog and ParseStatus are: the message
// is a field with no shape at all, a separator it could contain would split
// one entry into two, and that is the kind of bug which produces a plausible
// wrong answer rather than a crash. It is tested and fuzzed on its own.
//
// The position is read from %gd rather than counted off the loop. Both would
// be right today — `git stash list` walks the reflog in order — but only one
// of them stays right if git ever prints the list any other way, and an index
// that silently drifts by one is a drop of the wrong stash.
func ParseStashList(output []byte) ([]Stash, error) {
	records := splitRecords(output)

	stashes := make([]Stash, 0, len(records))
	for position, record := range records {
		fields := strings.Split(record, fieldSeparator)
		if len(fields) != stashFieldCount {
			return nil, fmt.Errorf(
				"expected %d NUL-separated fields, got %d in stash record %q",
				stashFieldCount, len(fields), record)
		}

		index, err := parseStashSelector(fields[1])
		if err != nil {
			return nil, err
		}
		if index != position {
			return nil, fmt.Errorf(
				"stash list is out of order: %q is entry %d of the output", fields[1], position)
		}

		date, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			return nil, fmt.Errorf("unreadable stash date %q: %w", fields[3], err)
		}

		branch, message := splitStashSubject(fields[2])
		stashes = append(stashes, Stash{
			Index:   index,
			SHA:     fields[0],
			Message: message,
			Branch:  branch,
			Date:    date,
		})
	}
	return stashes, nil
}

// parseStashSelector reads the number out of git's "stash@{2}".
//
// The whole shape is required rather than the digits pulled out of the middle:
// %gd abbreviates only for refs/stash, and a selector that came back as
// "refs/stash@{2}" — or as anything else — means this is not the list this
// code thinks it is reading.
func parseStashSelector(selector string) (int, error) {
	digits, found := strings.CutPrefix(selector, "stash@{")
	if !found {
		return 0, fmt.Errorf("unreadable stash selector %q: expected stash@{N}", selector)
	}
	digits, found = strings.CutSuffix(digits, "}")
	if !found {
		return 0, fmt.Errorf("unreadable stash selector %q: expected stash@{N}", selector)
	}

	index, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("unreadable stash selector %q: %w", selector, err)
	}
	if index < 0 {
		return 0, fmt.Errorf("unreadable stash selector %q: a position cannot be negative", selector)
	}
	return index, nil
}

// stashSubjectPrefixes are the two openings git writes, longest first.
//
// "WIP on main: 5956208 subject" is the message it invents; "On main: fix the
// parser" is the one wrapped around a message the user gave. Longest first
// because "WIP on " contains no "On " at its start but the naive order would
// still matter the day a third prefix arrives.
var stashSubjectPrefixes = []string{"WIP on ", "On "}

// detachedStashBranch is what git records where there is no branch.
const detachedStashBranch = "(no branch)"

// splitStashSubject takes git's own prefix off a reflog subject.
//
// The prefix is "On <branch>: " or "WIP on <branch>: ", and the branch is
// everything up to the first colon after it. That cut is exact rather than
// hopeful: git-check-ref-format forbids both ':' and ' ' in a branch name, so
// the first ": " after the prefix cannot fall inside one — and a user message
// containing a colon, "fix: the parser", keeps it, because the cut has already
// happened before that colon is reached.
//
// A subject in neither shape comes back whole, with no branch. That is not a
// failure worth an error: a reflog can be written by anything, this is display
// text, and a row that shows exactly what git said is more honest than one
// that refuses to draw.
func splitStashSubject(subject string) (branch, message string) {
	for _, prefix := range stashSubjectPrefixes {
		rest, found := strings.CutPrefix(subject, prefix)
		if !found {
			continue
		}
		name, tail, found := strings.Cut(rest, ": ")
		if !found {
			continue
		}
		if name == detachedStashBranch {
			// Not a branch and not a name anything can be done with. Empty is
			// what lets the interface say nothing rather than draw a row
			// offering to go to a branch called "(no branch)".
			return "", tail
		}
		return name, tail
	}
	return "", subject
}

// StashPushPreview is what setting the work tree aside would save, read from
// the repository rather than assumed.
type StashPushPreview struct {
	// Branch is the branch the stash would be filed under, or empty on a
	// detached HEAD — which git allows and records as "(no branch)". Stashing
	// there is a real operation and is not refused; the confirmation just has
	// no branch to name.
	Branch string

	// IncludeUntracked is the choice the client made, echoed back so the
	// command and the counts below are read as one answer.
	IncludeUntracked bool

	// Tracked is how many tracked paths differ from HEAD or from the index —
	// what `git stash push` saves with no flag at all.
	Tracked int

	// Untracked is how many paths git does not track yet. They are saved only
	// with --include-untracked, and counted separately for exactly that
	// reason: a single number would let a confirmation promise to save files
	// the command leaves behind.
	//
	// Ignored files are not among them. `git status` is read without
	// --ignored, and --include-untracked does not take them either, so the
	// count and the command agree.
	Untracked int
}

// Saving reports whether this preview describes a stash git would actually
// make.
//
// False is not an error here, and that placement is deliberate. A preview
// DESCRIBES — it is what a dialog draws before anybody has decided anything —
// and a work tree holding nothing but untracked files is a perfectly ordinary
// thing to be looking at with the box not yet ticked. Refusing to describe it
// would mean the one dialog that could offer the flag never opens.
//
// The refusal lives on PushStash, which is where the command is. See
// ErrNothingToStash.
func (p StashPushPreview) Saving() bool {
	if p.Tracked > 0 {
		return true
	}
	return p.IncludeUntracked && p.Untracked > 0
}

// PreviewStashPush reads what a stash would save.
//
// The only refusal is a repository with no commit at all, because there is
// then nothing to describe: a stash is a commit whose first parent is HEAD.
// Everything else — including a work tree this flag combination would save
// nothing from — comes back as counts, and Saving above is what says which.
func (r *Runner) PreviewStashPush(
	ctx context.Context, dir string, untracked bool,
) (StashPushPreview, error) {
	status, err := r.Status(ctx, dir)
	if err != nil {
		return StashPushPreview{}, err
	}
	if status.Unborn {
		// git refuses this too — "You do not have the initial commit yet" —
		// but saying that there is no commit to hang a stash off is a sentence
		// about the repository rather than about the command.
		return StashPushPreview{}, fmt.Errorf(
			"%w: a stash is a commit, and this repository has none to make one from", ErrNoCommits)
	}

	return StashPushPreview{
		Branch:           status.Branch,
		IncludeUntracked: untracked,
		Tracked:          countStashable(status.Files, false),
		Untracked:        countStashable(status.Files, true),
	}, nil
}

// nothingToStash says which of the two empty work trees this is.
//
// The distinction is the whole value of the refusal. A clean work tree has
// nothing to offer and the button should say so; a work tree holding four
// untracked files has plenty to offer and one unticked box between it and
// being saved — and the command that would run there is the one that exits 0
// having done nothing.
//
// Neither sentence carries a count, though the preview has one. The number
// belongs on screen beside the tick box, where it changes as the box is
// ticked; here it would only be a plural this package has no way to write —
// "1 untracked files" — for a fact the flag itself already names.
func nothingToStash(preview StashPushPreview) error {
	if preview.Untracked > 0 {
		return fmt.Errorf(
			"%w: nothing tracked has changed, and untracked files are left where they are without --include-untracked",
			ErrNothingToStash)
	}
	return fmt.Errorf("%w: the work tree matches HEAD", ErrNothingToStash)
}

// countStashable counts one side of what a push would save.
//
// Untracked and tracked are counted by the same walk because they are one
// question asked twice, and the alternative — two loops that must agree on
// which entries belong to neither — is where an unmerged path ends up counted
// in both or in nothing. An unmerged path is tracked: `git stash push` saves
// it, and the conflict comes back with it.
func countStashable(files []FileStatus, untracked bool) int {
	count := 0
	for _, file := range files {
		if (file.Kind == EntryUntracked) == untracked {
			count++
		}
	}
	return count
}

// PushStash sets the work tree aside and answers with the stack it made.
//
// rewriteTimeout rather than the deadline for commands that return instantly:
// a push writes every changed file back to its HEAD version, and on a large
// repository — or one whose files go through a smudge filter — that is minutes
// of legitimate work. The reasoning is Reset's, and so is the consequence of
// getting it wrong: killed at thirty seconds this leaves a half-written work
// tree and reports a timeout.
//
// The message is checked rather than trusted. Its own refusal exists because
// git accepts a newline here and then writes something else down; see
// ErrStashMessageLine.
//
// The work tree is read again here rather than taken from a plan, and this is
// where the no-op refusal lives. `git stash push` with nothing to save prints
// "No local changes to save" and exits 0 — the one outcome an interface cannot
// tell from success — so the guard belongs against the command that would run,
// not against the preview that only describes.
func (r *Runner) PushStash(
	ctx context.Context, dir, message string, untracked bool,
) ([]Stash, error) {
	message = strings.TrimSpace(message)
	if err := checkStashMessage(message); err != nil {
		return nil, err
	}

	preview, err := r.PreviewStashPush(ctx, dir, untracked)
	if err != nil {
		return nil, err
	}
	if !preview.Saving() {
		return nil, nothingToStash(preview)
	}

	if _, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    StashPushArgs(message, untracked),
		Timeout: rewriteTimeout,
	}); err != nil {
		return nil, err
	}

	return r.Stashes(ctx, dir)
}

// checkStashMessage refuses a description git would write down differently.
//
// Only the newline, and only because git is silent about it. Everything else a
// person can type — a leading dash, a quote, a colon — reaches the reflog
// intact and comes back out of parseStashList intact, and refusing any of it
// would be this file deciding what a stash may be called.
func checkStashMessage(message string) error {
	if strings.ContainsAny(message, "\n\r") {
		return fmt.Errorf(
			"%w: git stores the whole message on the commit and flattens it in the list, so the list would show something you did not write",
			ErrStashMessageLine)
	}
	return nil
}

// StashTarget resolves a position to the stash sitting at it, and refuses one
// that has moved.
//
// This is the check that makes every write in this file safe, and it is not
// the check the rest of the package uses. Elsewhere a plan resolves a revision
// to an object name and the run sends that name straight to git, so the name
// IS the agreement — see agreesOnCommit. A stash has no such name to send:
// `git stash drop` takes a position and refuses an object name, so the
// position is what runs, and the position is the part that moves.
//
// Two readings, in the order that refuses earliest:
//
//  1. The position must be a real one in the stack as it stands now.
//  2. The stash at it must be the one the plan described.
//
// The second is the one that matters. A stash pushed in another window shifts
// every entry down by one, so stash@{1} approved on screen is stash@{2} by the
// time the button is pressed — and the command in the confirmation would run,
// succeed, and take the wrong work.
//
// `expected` empty is refused like any other mismatch, for the reason
// agreesOnBranch refuses an empty branch: a client that names no stash is a
// client that did not look at one.
func (r *Runner) StashTarget(ctx context.Context, dir string, index int, expected string) (Stash, error) {
	if err := checkStashIndex(index); err != nil {
		return Stash{}, err
	}

	stashes, err := r.Stashes(ctx, dir)
	if err != nil {
		return Stash{}, err
	}
	if index >= len(stashes) {
		return Stash{}, fmt.Errorf("%w: %s, in a stack of %d",
			ErrNoStash, stashRef(index), len(stashes))
	}

	found := stashes[index]
	if expected == "" {
		return Stash{}, fmt.Errorf("%w: the request names none, and %s holds %s",
			ErrStashMoved, found.Ref(), ShortSHA(found.SHA))
	}
	if found.SHA != expected {
		return Stash{}, fmt.Errorf(
			"%w: %s now holds %s, not %s — read the list again",
			ErrStashMoved, found.Ref(), ShortSHA(found.SHA), ShortSHA(expected))
	}
	return found, nil
}

// checkStashIndex refuses a position no stack can have.
//
// A negative index would reach git as "stash@{-1}", which it reads as counting
// from the other end of the reflog — a real syntax, answering with a stash
// nobody named. Refused here rather than left to be surprising.
func checkStashIndex(index int) error {
	if index < 0 {
		return fmt.Errorf("%w: %d is not a position in the stack", ErrNoStash, index)
	}
	return nil
}

// ApplyStash puts one stash back into the work tree, and answers with the
// stack as it stands afterwards.
//
// The position is what git takes; StashTarget is what has already checked that
// the position still holds the stash the caller means.
//
// Whether the entry survives is the mode and nothing else, which is worth
// saying because it looks as though it might not be. A pop that runs into a
// conflict keeps the entry — git prints "The stash entry is kept in case you
// need it again" — but it also exits 1, so it never reaches this line. On the
// success path apply always keeps and pop always removes, and there is nothing
// here for a caller to read back to find out which.
//
// rewriteTimeout for the reason PushStash takes one: this writes the work
// tree.
func (r *Runner) ApplyStash(
	ctx context.Context, dir string, index int, mode StashApplyMode,
) ([]Stash, error) {
	if err := checkStashIndex(index); err != nil {
		return nil, err
	}
	if _, err := ParseStashApplyMode(string(mode)); err != nil {
		return nil, err
	}

	if _, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    StashApplyArgs(mode, index),
		Timeout: rewriteTimeout,
	}); err != nil {
		return nil, err
	}

	return r.Stashes(ctx, dir)
}

// DropStash removes one entry from the stack and answers with what is left.
//
// The commit itself is not deleted: nothing points at it afterwards, so it
// becomes unreachable and waits for git's own garbage collection. That is the
// difference between "gone" and "gone from the list", and it is what the
// confirmation is able to say because this returns without pretending
// otherwise.
func (r *Runner) DropStash(ctx context.Context, dir string, index int) ([]Stash, error) {
	if err := checkStashIndex(index); err != nil {
		return nil, err
	}
	if _, err := r.Run(ctx, dir, StashDropArgs(index)...); err != nil {
		return nil, err
	}
	return r.Stashes(ctx, dir)
}

// What a stash holds is read with `git diff` and `git show`, never with
// `git stash show`.
//
// The wrapper looks like the obvious command and cannot be used here. It
// re-parses its arguments and hands the remainder on, and the `--src-prefix=a/`
// and `--dst-prefix=b/` that diffArgs pins do not survive the trip on every
// git: they come back out as fragments of other option names, so the header is
// `diff --git butesnotes.md` instead of `diff --git a/notes.md b/notes.md`, and
// ParseDiff refuses it. The prefixes cannot simply be dropped, because pinning
// them is what stops `diff.mnemonicPrefix` and `diff.noprefix` from doing the
// same thing on a user's machine (see diffArgs).
//
// It went unnoticed locally and failed on all three CI platforms, which is the
// shape of a version-dependent bug in somebody else's program. So the reading
// is done with the two commands diffArgs was written for, against the parents
// a stash commit already carries.

// stashSources are the two halves of what a stash holds.
//
// A stash commit is a merge with an unusual arrangement of parents, and the
// arrangement is the whole reason two reads are needed. Its own tree is the
// tracked work; the untracked files it took along hang off a third parent,
// which is why `git stash show` needs telling to look at them and why anything
// reading only the commit finds an empty patch for a stash that plainly holds
// a file.
type stashSources struct {
	// Base is stash^1, the commit the stash was made on. The tracked half is
	// the diff from it to the stash commit.
	Base string

	// Untracked is stash^3, a parentless commit whose tree is the files git
	// was not tracking — or empty where the stash was made without them.
	Untracked string
}

// stashSources reads a stash commit's parents, and refuses a commit that is
// not one.
//
// Two or three parents, never one and never none. That check is not
// bookkeeping: these routes take an object name from a client, and an ordinary
// commit passed to them would otherwise be shown as "a stash" — its diff
// against its own parent, under a heading naming a stack it was never in.
func (r *Runner) stashSources(ctx context.Context, dir, sha string) (stashSources, error) {
	if err := checkRevision(sha); err != nil {
		return stashSources{}, err
	}

	output, err := r.Run(ctx, dir, "rev-list", "-1", "--parents", sha)
	if err != nil {
		return stashSources{}, err
	}

	// The line is the commit followed by its parents.
	fields := strings.Fields(strings.TrimSpace(string(output)))
	parents := fields[1:]
	switch len(parents) {
	case 2:
		return stashSources{Base: parents[0]}, nil
	case 3:
		return stashSources{Base: parents[0], Untracked: parents[2]}, nil
	default:
		return stashSources{}, fmt.Errorf(
			"%w: %s has %d parents, and a stash commit has two or three",
			ErrNoStash, ShortSHA(sha), len(parents))
	}
}

// stashPatch reads both halves of what a stash holds, in the shape asked for.
//
// `shape` is what the two commands are told to produce — the content, or the
// names alone — and it is the only thing that differs between this file's two
// readers. The commands themselves cannot be shared: `git diff` takes two
// revisions and prints no header, `git show` takes one and needs `--format=`
// to be told not to.
//
// Each half is capped on its own. Two reads means the pair can come to twice
// maxDiffBytes, and that is the honest cap for two commands — the limit is
// there so no single answer can be unbounded, not so a stash of two enormous
// halves is refused for being two.
func (r *Runner) stashPatch(ctx context.Context, dir, sha string, shape []string) ([]byte, error) {
	sources, err := r.stashSources(ctx, dir, sha)
	if err != nil {
		return nil, err
	}

	tracked := append([]string{"diff"}, shape...)
	tracked = append(tracked, sources.Base, sha)

	patch, err := r.Exec(ctx, Command{Dir: dir, Args: tracked, MaxOutput: maxDiffBytes})
	if err != nil {
		return nil, err
	}
	if sources.Untracked == "" {
		return patch, nil
	}

	// `--format=` empties the header, leaving the patch alone — the same
	// reading showPatch makes of a commit. No --diff-merges is needed: this
	// parent has no parents of its own, so git prints its whole tree as
	// additions, which is exactly what those files were.
	untracked := append([]string{"show", "--format="}, shape...)
	untracked = append(untracked, sources.Untracked)

	extra, err := r.Exec(ctx, Command{Dir: dir, Args: untracked, MaxOutput: maxDiffBytes})
	if err != nil {
		return nil, err
	}
	return append(patch, extra...), nil
}

// CountStashFiles is how many paths one stash holds.
//
// Separate from ShowStash, and the difference is what a plan can afford. A
// confirmation has to name what is at stake — "the changes it holds, in 3
// files" — and reading the whole patch to count its files would pull a
// regenerated lockfile through the daemon to answer with a single integer.
// --name-only is the same walk without the content.
//
// -z because a path is bytes: a filename with a newline in it is legal on
// every platform yagit runs on, and the line-based form would count it twice.
func (r *Runner) CountStashFiles(ctx context.Context, dir, sha string) (int, error) {
	names, err := r.stashPatch(ctx, dir, sha, []string{"--name-only", "-z"})
	if err != nil {
		return 0, err
	}

	// Every path is terminated by a NUL, so the count is the number of
	// terminators. Splitting would answer one too many on any non-empty
	// output, which is the sort of off-by-one that reads as plausible.
	return bytes.Count(names, []byte{0}), nil
}

// ShowStash reads what one stash holds.
//
// By object name rather than by position, and that is the one place in this
// file where the position is not used. Reading is not writing: nothing is at
// stake if the stack shifted between the click and the answer, and the object
// name cannot come back as somebody else's work. What the caller draws is
// whatever this describes, so a stash dropped in another window is still
// readable here for as long as its commit survives — which is more useful than
// a 404 for a screen that is only looking.
func (r *Runner) ShowStash(ctx context.Context, dir, sha string) ([]FileDiff, error) {
	content := append([]string{}, diffArgs...)
	// `git diff` prints a patch by default and `git show` does not, so the
	// flag is named rather than left to either one's habit.
	content = append(content, "--patch")

	patch, err := r.stashPatch(ctx, dir, sha, content)
	if err != nil {
		return nil, err
	}

	files, err := ParseDiff(patch)
	if err != nil {
		return nil, err
	}
	if files == nil {
		// A stash whose content resolves to nothing is not something git makes
		// — push refuses an empty work tree — but null in a payload is an
		// interface that crashes on whatever produced it.
		return []FileDiff{}, nil
	}
	return files, nil
}
