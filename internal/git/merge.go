package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Merging one local branch into the branch HEAD is on.
//
// The branch name reaches git after `--`, for the same reason every reference
// in branch.go does: `git merge -m subject` is a merge with a message on it,
// not a merge of a branch called `-m`, and the daemon must never be the place
// where a string somebody typed turns into an option. That git-check-ref-format
// refuses to create such a branch is why the guard is never exercised, not a
// reason to leave it out — the day the name comes from somewhere other than a
// row of the sidebar, it is already right.
//
// What the command DOES is never left to configuration. A bare `git merge`
// asks merge.ff, and that is a setting: the same button is a pointer moving on
// one machine, a merge commit on the next, and a refusal on the third — while
// the confirmation shows one line and promises it means one thing. So the
// outcome is read from the two branches first (PreviewMerge) and the flag that
// pins it is passed every time. PullArgs makes the same decision for the same
// reason, and docs/adr/0021 is the rule both follow; the line on screen is then
// the whole of the operation, with nothing read from a file to complete it.
//
// Two more halves of that same promise are pinned here, and docs/adr/0022 is
// why. The branch is named refs/heads/…, because a short name is resolved by
// git's own search order and a tag of the same name wins it. And the message
// of a merge commit is written out, because the one git composes is assembled
// from the name as it was given and from merge.log — a setting again, and one
// that decides what a commit says.

// MergeOutcome is what merging one branch into another would do.
//
// Three, because they are three different things to the person pressing the
// button rather than three readings of one: one writes no object at all, one
// records a commit under their hooks and their signature, and one does nothing
// whatever.
type MergeOutcome string

const (
	// MergeUpToDate: the branch is already contained. git answers "Already up
	// to date" and writes nothing.
	MergeUpToDate MergeOutcome = "up-to-date"

	// MergeFastForward: the branch being merged into has no commit of its
	// own, so the merge is its pointer moving up. Nothing is committed, so no
	// hook runs and nothing is signed.
	MergeFastForward MergeOutcome = "fast-forward"

	// MergeCommit: both branches hold commits the other does not, so the merge
	// is finished by a commit — with the user's hooks and signing, and with a
	// conflict as the ordinary way for it to stop halfway.
	MergeCommit MergeOutcome = "merge-commit"
)

// errUnknownOutcome names the three, because every refusal here is somebody
// being told which words the field takes.
var errUnknownOutcome = errors.New(
	"the merge outcome must be one of up-to-date, fast-forward, merge-commit")

// ParseMergeOutcome reads an outcome sent by a client.
//
// An unknown one is refused rather than read as a default, for the reason
// ParsePullStrategy refuses one: the two commands differ by whether a commit is
// recorded, and running the other would look entirely correct to a client that
// asked for this one.
func ParseMergeOutcome(raw string) (MergeOutcome, error) {
	switch MergeOutcome(raw) {
	case MergeUpToDate, MergeFastForward, MergeCommit:
		return MergeOutcome(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownOutcome)
	default:
		return "", fmt.Errorf("%w: %q is not one of them", errUnknownOutcome, raw)
	}
}

// MergeArgs is the command Merge runs to reach an outcome.
//
// Exported for the reason DeleteBranchArgs is: the line the user is shown and
// the line git receives have one definition between them.
//
// The flag is the lease on what was shown. A fast-forward runs --ff-only, so a
// branch that stopped being strictly behind between the dialog and the click
// is refused rather than quietly recorded as a merge commit nobody saw. A
// merge commit runs --no-ff, which holds the other half of the same promise:
// the dialog said a commit, so a commit is what happens even where a
// fast-forward has become possible since. The flag is the LAST lease, though,
// not the only one — it cannot speak for the sentence beside it, and
// --ff-only says nothing at all about "this changes nothing". What the plan
// promised is checked against the repository before this command is built; see
// the merge route.
//
// The name is spelled refs/heads/… for the reason LocalBranchRef exists: `git
// merge dup` in a repository holding both a branch and a tag called `dup` is a
// merge of the tag. The `--` stays anyway — the two guards cost one argument
// between them, and neither is the one that would be missed.
//
// The message is written here rather than left to git, and only where a commit
// is recorded. Given a full ref git would title the commit "Merge branch
// 'refs/heads/dup'", and given merge.log it would append a summary of every
// commit arriving; both are the message deciding itself out of view of the
// confirmation that showed the command. --no-edit stays beside it because
// GIT_MERGE_AUTOEDIT can still send git looking for an editor the daemon has
// no terminal to open.
func MergeArgs(into, branch string, outcome MergeOutcome) []string {
	// An outcome this does not recognise is not a reason to hand git a bare
	// `merge` and let merge.ff answer: --ff-only is the reading that can
	// neither record a commit nobody asked for nor stop halfway on a conflict.
	// It is unreachable — the route parses the value at the edge of the daemon
	// and Merge is given the one PreviewMerge read — and it is written this
	// way so that a fourth outcome added carelessly cannot reintroduce the
	// setting.
	args := []string{"merge", "--ff-only"}
	if outcome == MergeCommit {
		args = []string{"merge", "--no-ff", "--no-edit", "-m", MergeMessage(into, branch)}
	}

	return append(args, "--", LocalBranchRef(branch))
}

// MergeMessage is the subject a merge commit is recorded under.
//
// git's own form, minus its special cases: git leaves "into main" off when the
// branch merged into is the default one, so the same operation is described
// two ways depending on where it happened. One shape here, always naming both
// branches, and it is on screen in the command before it is in the history.
func MergeMessage(into, branch string) string {
	return fmt.Sprintf("Merge branch '%s' into %s", branch, into)
}

// Merge brings another branch into the one HEAD is on.
//
// `into` is the name of that branch, and it is passed rather than read: git
// merges into wherever HEAD is whatever this says, so the name is here to be
// written into the message and nothing else. Checking that HEAD is still there
// is the caller's, because a caller is the only thing that knows what it
// promised — see the merge route, which refuses when the two disagree.
//
// The outcome is taken as given. Every value produces a command that pins what
// it does (MergeArgs), and the value itself comes from PreviewMerge one line
// earlier on the route rather than from a client, so there is nothing left
// here to validate a second time.
//
// A conflict is not hidden: git stops, writes the markers into the work tree
// and exits non-zero, and that refusal travels whole — command, exit code,
// stderr — to whatever called this. The interface names the state and offers
// to resolve it; this function does not choose on the user's behalf.
//
// Not Run, for the reason ActOnOperation is not: a merge checks out a whole
// tree and can finish by committing, which runs the user's pre-commit and
// commit-msg hooks and may wait on a smartcard or a pinentry. Thirty seconds
// is the deadline for commands that return instantly, and killing this one
// halfway leaves a half-written merge and possibly an index.lock while the
// user is told their command timed out.
//
// The deadline is only half of that promise. Whatever context this is given
// can end the command too, and a browser's request context ends when the tab
// does — which is why the routes that write hand this one a context that
// outlives the request. See internal/api/lifetime.go.
func (r *Runner) Merge(ctx context.Context, dir, into, branch string, outcome MergeOutcome) error {
	into, branch = strings.TrimSpace(into), strings.TrimSpace(branch)
	if into == "" || branch == "" {
		return ErrNoBranchName
	}

	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    MergeArgs(into, branch, outcome),
		Timeout: rewriteTimeout,
	})
	return err
}

// MergePreview is what merging one branch into another would do, read from the
// two branches rather than assumed.
type MergePreview struct {
	Outcome MergeOutcome

	// Ahead is how many commits the branch being merged INTO has that the
	// other does not. Zero is what makes a merge a fast-forward.
	Ahead int

	// Behind is how many commits the merge would bring in. Zero means there is
	// nothing to bring, whatever else the two branches have done.
	Behind int
}

// ErrUnrelatedHistories: the two branches share no commit at all.
//
// git refuses this merge by default — "refusing to merge unrelated histories"
// — and it is worth its own error rather than being met when the command runs.
// The counts say nothing about it: two histories with no fork point report
// every commit on each side, which reads exactly like a wide divergence, and a
// confirmation built from that promises a merge commit git will never make.
var ErrUnrelatedHistories = errors.New("the two branches have no common ancestor")

// PreviewMerge reads what merging branch into `into` would do.
//
// One command for both numbers: `git rev-list --left-right --count a...b`
// counts each side of the symmetric difference in a single walk, and those two
// counts answer everything the confirmation asks — whether this is a
// fast-forward, whether it does nothing at all, and how many commits are
// coming.
//
// The names are spelled in full, refs/heads/…, rather than passed short. See
// LocalBranchRef: `main...dup` is resolved by git's own search order, which
// reaches refs/tags before refs/heads, and a repository holding both a branch
// and a tag called `dup` would have this count against the tag while the
// confirmation named the branch.
//
// The `--` that protects every other reference in this package cannot help
// here: the range is one argument with both names inside it. It is not needed
// either, since the full names cannot start with a dash — git-check-ref-format
// refuses a branch that does.
//
// What this does NOT read is the work tree. A merge is also refused by local
// changes it would overwrite, and finding out which files those are means
// doing the merge; git's own refusal names them and travels whole. The
// boundary is worth stating: this answers what the two BRANCHES make of each
// other, and everything it answers is a promise the run route keeps.
func (r *Runner) PreviewMerge(ctx context.Context, dir, into, branch string) (MergePreview, error) {
	into, branch = strings.TrimSpace(into), strings.TrimSpace(branch)
	if into == "" || branch == "" {
		return MergePreview{}, ErrNoBranchName
	}

	// A name no branch has fails here, with git's own "unknown revision" —
	// which is the honest answer to a plan asked about a branch that has been
	// deleted since the sidebar drew it.
	output, err := r.Run(ctx, dir, "rev-list", "--left-right", "--count",
		fmt.Sprintf("%s...%s", LocalBranchRef(into), LocalBranchRef(branch)))
	if err != nil {
		return MergePreview{}, err
	}

	ahead, behind, err := parseLeftRightCount(string(output))
	if err != nil {
		return MergePreview{}, err
	}

	outcome := outcomeOf(ahead, behind)

	// Only where the two have diverged, because that is the only reading
	// unrelated histories can produce: one contained in the other means they
	// met, so the second walk is spent exactly where it can change the answer.
	if outcome == MergeCommit {
		related, err := r.shareAncestor(ctx, dir, into, branch)
		if err != nil {
			return MergePreview{}, err
		}
		if !related {
			return MergePreview{}, fmt.Errorf(
				"%w: %s and %s were started separately", ErrUnrelatedHistories, into, branch)
		}
	}

	return MergePreview{Outcome: outcome, Ahead: ahead, Behind: behind}, nil
}

// shareAncestor asks whether the two branches ever met.
//
// `git merge-base` grades its own failure: 1 is "there is no merge base", 128
// is "something went wrong", and only the first of those is an answer. So the
// exit code is read rather than passed on, and everything else still travels
// whole — a repository that cannot be read must never be reported as two
// histories that were started separately.
func (r *Runner) shareAncestor(ctx context.Context, dir, into, branch string) (bool, error) {
	output, err := r.Run(ctx, dir, "merge-base",
		LocalBranchRef(into), LocalBranchRef(branch))
	if err == nil {
		return strings.TrimSpace(string(output)) != "", nil
	}

	var failure *Error
	if errors.As(err, &failure) && failure.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// outcomeOf reads the two counts as the three things a merge can be.
//
// Behind first: a branch that brings nothing is up to date however far ahead
// the other one is, and calling that a fast-forward would promise a pointer
// move that never happens.
func outcomeOf(ahead, behind int) MergeOutcome {
	switch {
	case behind == 0:
		return MergeUpToDate
	case ahead == 0:
		return MergeFastForward
	default:
		return MergeCommit
	}
}

// parseLeftRightCount reads the "2\t1" of `rev-list --left-right --count`.
//
// Refused rather than defaulted when it is anything else: zero and zero is
// "already up to date", which is the one answer that must never be invented.
func parseLeftRightCount(output string) (left, right int, err error) {
	fields := strings.Fields(output)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("unreadable rev-list count %q, want two numbers", output)
	}

	if left, err = strconv.Atoi(fields[0]); err != nil {
		return 0, 0, fmt.Errorf("unreadable rev-list count %q: %w", output, err)
	}
	if right, err = strconv.Atoi(fields[1]); err != nil {
		return 0, 0, fmt.Errorf("unreadable rev-list count %q: %w", output, err)
	}

	// Atoi accepts a sign, and `--count` cannot produce one. Refusing here
	// rather than degrading to zero is this function's contract — it already
	// errors on a field that is not a number at all — and it matters because
	// outcomeOf switches on `behind == 0` and then `ahead == 0`: a negative
	// count matches neither and picks an outcome by falling past both.
	if left < 0 || right < 0 {
		return 0, 0, fmt.Errorf("negative rev-list count %q, want two counts", output)
	}
	return left, right, nil
}
