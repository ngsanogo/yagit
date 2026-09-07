package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Reverting one commit onto the branch HEAD is on.
//
// The twin of a cherry-pick: where a pick applies a commit's changes, a revert
// applies their inverse. The revision reaches git after `--` for the same
// reason every reference in this package does — checkRevision refuses a
// leading dash before it gets here; the separator is the belt under that brace.
//
// What the command DOES is pinned rather than left to configuration. There is
// no revert.ff and no arrangement of two branches that turns a revert into a
// pointer move: it always records a new commit under the user's hooks and
// signing. So the outcome is one value, and two flags carry the pinning.
//
// `--no-edit` — without it a revert opens an editor on the generated message
// whenever it believes somebody is watching, and the daemon has no terminal.
//
// `--no-reference` — `revert.reference` is a real setting in the user's
// ~/.gitconfig, and the daemon passes HOME so git reads it. It asks for the
// message `--reference` writes, whose FIRST line is the placeholder
// `*** SAY WHY WE ARE REVERTING ON THE TITLE LINE ***` for the editor to
// replace. With `--no-edit` there is no editor, so that placeholder is what
// gets committed as the subject. Pinned here so the line on screen and the
// commit it records cannot disagree because of a setting nobody was shown.
//
// docs/adr/0021 is the rule; the line on screen is then the whole of the
// operation.
//
// A merge commit is refused rather than offered with `-m`, for the same reason
// cherry-pick refuses one: choosing a mainline parent is a second question this
// screen does not ask. A root commit is refused too — git WILL revert one, by
// inverting it against the empty tree, and what that deletes is every file the
// repository started with; the confirmation's sentence names taking one
// commit's changes back out, and for a root that sentence is a wild
// understatement. A commit that is not already on the branch is refused rather
// than offered as an inverse cherry-pick of foreign history: the confirmation
// names what is being taken back out of the branch HEAD is on, and that sentence
// is a lie for a commit the branch never held.

// RevertOutcome is what reverting one commit onto the current branch would do.
//
// One value today: a revert always records a new commit. Kept as a named
// outcome — and echoed back on the run route — so a future reading that finds
// a second thing to say still has a field whose agreement can be checked, and
// so an unknown string from a client is refused rather than defaulted.
type RevertOutcome string

const (
	// RevertApply: the commit's changes are taken back out as a new commit on
	// top of HEAD, under the user's hooks and signing, with a conflict as the
	// ordinary way for it to stop halfway.
	RevertApply RevertOutcome = "revert"
)

// errUnknownRevertOutcome names the one, because every refusal here is
// somebody being told which words the field takes.
var errUnknownRevertOutcome = errors.New("the revert outcome must be revert")

// ErrRevertMerge: the commit has more than one parent.
//
// git needs `-m` to know which parent to invert against, and choosing that is a
// second question. Refused here so a plan never describes a command the run
// route would have to invent a mainline for.
var ErrRevertMerge = errors.New("a merge commit cannot be reverted without choosing a mainline parent")

// ErrRevertRoot: the commit has no parent.
//
// Not git's refusal — git reverts a root commit happily, by inverting it
// against the empty tree, which deletes every file the repository started
// with. It is yagit's: the confirmation offers to take one commit's changes
// back out, and emptying the repository is not that sentence. Named here so
// the plan says so before a dialog promises the smaller operation.
var ErrRevertRoot = errors.New("a root commit is not offered for revert")

// ErrRevertNotOnBranch: the commit is not reachable from HEAD.
//
// A revert takes changes back out of the branch the user is on. Offering one
// for a commit that branch never held would be an inverse cherry-pick of
// foreign history, and the confirmation's sentence would name the wrong thing.
var ErrRevertNotOnBranch = errors.New("the commit is not on the branch HEAD is on")

// ParseRevertOutcome reads an outcome sent by a client.
//
// An unknown one is refused rather than read as a default, for the reason
// ParseCherryPickOutcome refuses one: running the wrong one would look
// entirely correct to a client that asked for another.
func ParseRevertOutcome(raw string) (RevertOutcome, error) {
	switch RevertOutcome(raw) {
	case RevertApply:
		return RevertOutcome(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownRevertOutcome)
	default:
		return "", fmt.Errorf("%w: %q is not it", errUnknownRevertOutcome, raw)
	}
}

// RevertArgs is the command Revert runs.
//
// Exported for the reason CherryPickArgs is: the line the user is shown and
// the line git receives have one definition between them.
//
// Both flags are pins rather than preferences — see the file comment. Without
// `--no-reference` a user with `revert.reference = true` gets a commit whose
// subject is the placeholder that option leaves for an editor `--no-edit`
// never opens.
func RevertArgs(commit string) []string {
	return []string{"revert", "--no-edit", "--no-reference", "--", commit}
}

// Revert applies the inverse of one commit onto the branch HEAD is on.
//
// `into` is the name of that branch, and it is passed rather than read: git
// reverts onto wherever HEAD is whatever this says, so the name is here for
// the caller to have checked and for nothing else. Checking that HEAD is
// still there is the caller's — see the revert route.
//
// A conflict is not hidden: git stops, writes the markers into the work tree
// and exits non-zero, and that refusal travels whole.
//
// Not Run, for the reason CherryPick is not: a revert checks out a tree and
// finishes by committing, which runs the user's hooks and may wait on a
// smartcard. Thirty seconds is the deadline for commands that return
// instantly.
func (r *Runner) Revert(ctx context.Context, dir, commit string, outcome RevertOutcome) error {
	commit = strings.TrimSpace(commit)
	if err := checkRevision(commit); err != nil {
		return err
	}
	if _, err := ParseRevertOutcome(string(outcome)); err != nil {
		return err
	}

	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    RevertArgs(commit),
		Timeout: rewriteTimeout,
	})
	return err
}

// RevertPreview is what reverting one commit onto the current branch would do,
// read from the commit and from HEAD rather than assumed.
type RevertPreview struct {
	Outcome RevertOutcome

	// Commit is the full object name. The client may have sent a short SHA;
	// everything that follows — the command, the lease, the toast — uses the
	// one git resolved, so two forms of the same name cannot disagree.
	Commit string

	// Subject is the commit's first line, for a confirmation that has to name
	// what is being reverted rather than only a hash.
	Subject string
}

// PreviewRevert reads what reverting commit onto HEAD would do.
//
// Four readings, in the order that refuses earliest:
//
//  1. The revision must resolve to a commit.
//  2. A root commit is refused — see ErrRevertRoot.
//  3. A merge commit is refused — see ErrRevertMerge.
//  4. A commit that is not an ancestor of HEAD is refused — see
//     ErrRevertNotOnBranch.
//
// What this does NOT read is the work tree. A revert is also refused by local
// changes it would overwrite, and finding out which files those are means
// doing the revert; git's own refusal names them and travels whole.
func (r *Runner) PreviewRevert(ctx context.Context, dir, commit string) (RevertPreview, error) {
	sha, parents, subject, err := r.resolveCommit(ctx, dir, commit)
	if err != nil {
		return RevertPreview{}, err
	}

	switch {
	case len(parents) == 0:
		return RevertPreview{}, fmt.Errorf(
			"%w: %s has no parent, and inverting it against the empty tree would delete"+
				" every file the repository started with", ErrRevertRoot, ShortSHA(sha))
	case len(parents) > 1:
		return RevertPreview{}, fmt.Errorf(
			"%w: %s has %d parents", ErrRevertMerge, ShortSHA(sha), len(parents))
	}

	head, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return RevertPreview{}, err
	}
	headSHA := strings.TrimSpace(string(head))

	ancestor, err := r.isAncestor(ctx, dir, sha, headSHA)
	if err != nil {
		return RevertPreview{}, err
	}
	if !ancestor {
		return RevertPreview{}, fmt.Errorf(
			"%w: %s is not reachable from HEAD", ErrRevertNotOnBranch, ShortSHA(sha))
	}

	return RevertPreview{Outcome: RevertApply, Commit: sha, Subject: subject}, nil
}
