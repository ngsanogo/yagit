package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Cherry-picking one commit onto the branch HEAD is on.
//
// The revision reaches git after `--`, for the same reason every reference in
// this package does: a string from the network must never be read as an
// option. checkRevision refuses a leading dash before it gets here; the
// separator is the belt under that brace.
//
// What the command DOES is never left to configuration, and a cherry-pick
// reads fewer settings than a merge did — there is no cherry-pick.ff — but it
// still has a flag that changes what happens. `--ff` turns "HEAD is this
// commit's parent" into a pointer moving onto that commit, and without it the
// same arrangement writes a new object with the same tree. So the outcome is
// read first (PreviewCherryPick) and the flag that pins it is passed every
// time. docs/adr/0021 is the rule; the line on screen is then the whole of the
// operation.
//
// `--no-edit` belongs beside it for the same reason merge carries it: a
// cherry-pick opens an editor on the reused message whenever it believes
// somebody is watching, and the daemon has no terminal. Saying `--no-edit`
// makes the outcome a property of the command rather than of the environment.
//
// A merge commit is refused rather than offered with `-m`. Picking a merge
// needs a mainline parent chosen, and that choice is a second question this
// screen does not ask — the commit panel is about one commit's patch, which
// for a merge is already the first-parent reading Show answers with.

// CherryPickOutcome is what picking one commit onto the current branch would
// do.
//
// Three, because they are three different things to the person pressing the
// button: one writes nothing at all, one moves the branch onto the commit
// itself, and one records a new commit under their hooks and signature.
type CherryPickOutcome string

const (
	// CherryPickUpToDate: the commit is already on the branch — HEAD, or an
	// ancestor of it. git would try to apply an empty patch and refuse;
	// reporting that here means the confirmation never offers a command that
	// cannot succeed.
	CherryPickUpToDate CherryPickOutcome = "up-to-date"

	// CherryPickFastForward: HEAD is the commit's first parent, so picking it
	// with --ff is the branch pointer moving onto that commit. Nothing is
	// written, so no hook runs and nothing is signed — but the work tree is
	// checked out underneath it.
	CherryPickFastForward CherryPickOutcome = "fast-forward"

	// CherryPickApply: the commit's changes are applied as a new commit on
	// top of HEAD, under the user's hooks and signing, with a conflict as the
	// ordinary way for it to stop halfway.
	CherryPickApply CherryPickOutcome = "cherry-pick"
)

// errUnknownCherryPickOutcome names the three, because every refusal here is
// somebody being told which words the field takes.
var errUnknownCherryPickOutcome = errors.New(
	"the cherry-pick outcome must be one of up-to-date, fast-forward, cherry-pick")

// ErrCherryPickMerge: the commit has more than one parent.
//
// git needs `-m` to know which parent to diff against, and choosing that is a
// second question. Refused here so a plan never describes a command the run
// route would have to invent a mainline for.
var ErrCherryPickMerge = errors.New("a merge commit cannot be cherry-picked without choosing a mainline parent")

// ParseCherryPickOutcome reads an outcome sent by a client.
//
// An unknown one is refused rather than read as a default, for the reason
// ParseMergeOutcome refuses one: the three differ by whether a commit is
// recorded, and running the wrong one would look entirely correct to a client
// that asked for another.
func ParseCherryPickOutcome(raw string) (CherryPickOutcome, error) {
	switch CherryPickOutcome(raw) {
	case CherryPickUpToDate, CherryPickFastForward, CherryPickApply:
		return CherryPickOutcome(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownCherryPickOutcome)
	default:
		return "", fmt.Errorf("%w: %q is not one of them", errUnknownCherryPickOutcome, raw)
	}
}

// CherryPickArgs is the command CherryPick runs to reach an outcome.
//
// Exported for the reason MergeArgs is: the line the user is shown and the
// line git receives have one definition between them.
//
// Up-to-date has no command that succeeds as a no-op the way `git merge
// --ff-only` does — cherry-picking an ancestor fails on an empty patch — so
// the argument list below is only ever built for the two outcomes that run.
// The plan route still answers up-to-date; it answers without a command, and
// the run route verifies and returns without calling git. See CherryPick.
func CherryPickArgs(commit string, outcome CherryPickOutcome) []string {
	args := []string{"cherry-pick", "--no-edit"}
	switch outcome {
	case CherryPickFastForward:
		args = append(args, "--ff")
	default:
		// Apply, and anything unrecognised. --no-ff is the pin: without it a
		// future git default, or a wrapper that injected --ff, would turn an
		// approved new commit into a pointer move nobody was shown. Unreachable
		// for unknown outcomes on the route — ParseCherryPickOutcome sits at
		// the edge — and written this way so a fourth outcome cannot reintroduce
		// an unpinned cherry-pick.
		args = append(args, "--no-ff")
	}
	return append(args, "--", commit)
}

// CherryPick applies one commit onto the branch HEAD is on.
//
// `into` is the name of that branch, and it is passed rather than read: git
// cherry-picks onto wherever HEAD is whatever this says, so the name is here
// for the caller to have checked and for nothing else. Checking that HEAD is
// still there is the caller's — see the cherry-pick route.
//
// Up-to-date runs nothing. The other two run CherryPickArgs. A conflict is not
// hidden: git stops, writes the markers into the work tree and exits
// non-zero, and that refusal travels whole.
//
// Not Run, for the reason Merge is not: a cherry-pick checks out a tree and
// finishes by committing, which runs the user's hooks and may wait on a
// smartcard. Thirty seconds is the deadline for commands that return
// instantly.
func (r *Runner) CherryPick(ctx context.Context, dir, commit string, outcome CherryPickOutcome) error {
	commit = strings.TrimSpace(commit)
	if err := checkRevision(commit); err != nil {
		return err
	}
	if outcome == CherryPickUpToDate {
		return nil
	}

	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    CherryPickArgs(commit, outcome),
		Timeout: rewriteTimeout,
	})
	return err
}

// CherryPickPreview is what picking one commit onto the current branch would
// do, read from the commit and from HEAD rather than assumed.
type CherryPickPreview struct {
	Outcome CherryPickOutcome

	// Commit is the full object name. The client may have sent a short SHA;
	// everything that follows — the command, the lease, the toast — uses the
	// one git resolved, so two forms of the same name cannot disagree.
	Commit string

	// Subject is the commit's first line, for a confirmation that has to name
	// what is being picked rather than only a hash.
	Subject string
}

// PreviewCherryPick reads what cherry-picking commit onto HEAD would do.
//
// Four readings, in the order that refuses earliest:
//
//  1. The revision must resolve to a commit, and that commit must not be a
//     merge — see ErrCherryPickMerge.
//  2. If it is already an ancestor of HEAD, the outcome is up-to-date.
//  3. If HEAD is its first parent, the outcome is a fast-forward.
//  4. Otherwise it is an apply.
//
// What this does NOT read is the work tree. A cherry-pick is also refused by
// local changes it would overwrite, and finding out which files those are
// means doing the cherry-pick; git's own refusal names them and travels whole.
func (r *Runner) PreviewCherryPick(ctx context.Context, dir, commit string) (CherryPickPreview, error) {
	sha, parents, subject, err := r.resolveCommit(ctx, dir, commit)
	if err != nil {
		return CherryPickPreview{}, err
	}

	if len(parents) > 1 {
		return CherryPickPreview{}, fmt.Errorf(
			"%w: %s has %d parents", ErrCherryPickMerge, ShortSHA(sha), len(parents))
	}

	head, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return CherryPickPreview{}, err
	}
	headSHA := strings.TrimSpace(string(head))

	preview := CherryPickPreview{Commit: sha, Subject: subject}

	// Already on the branch — including "this is HEAD" — before the parent
	// check: a commit that is HEAD is also an ancestor of HEAD, and calling
	// that a fast-forward would promise a pointer move that never happens.
	ancestor, err := r.isAncestor(ctx, dir, sha, headSHA)
	if err != nil {
		return CherryPickPreview{}, err
	}
	if ancestor {
		preview.Outcome = CherryPickUpToDate
		return preview, nil
	}

	if len(parents) == 1 && parents[0] == headSHA {
		preview.Outcome = CherryPickFastForward
		return preview, nil
	}

	preview.Outcome = CherryPickApply
	return preview, nil
}

// resolveCommit reads the three facts both replay previews start from: the
// full object name, how many parents the commit has, and what it says.
//
// One definition because cherry-pick and revert ask for them identically. The
// revision is trimmed and refused if git could read it as an option, and it is
// resolved to the full name FIRST — a short SHA the client held and a different
// commit that became unambiguous since would otherwise be the same string
// describing two objects.
func (r *Runner) resolveCommit(ctx context.Context, dir, revision string) (sha string, parents []string, subject string, err error) {
	revision = strings.TrimSpace(revision)
	if err := checkRevision(revision); err != nil {
		return "", nil, "", err
	}

	resolved, err := r.Run(ctx, dir, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", nil, "", err
	}
	sha = strings.TrimSpace(string(resolved))

	parents, subject, err = r.commitParentsAndSubject(ctx, dir, sha)
	if err != nil {
		return "", nil, "", err
	}
	return sha, parents, subject, nil
}

// commitParentsAndSubject reads the two facts a cherry-pick plan needs from
// one commit: how many parents it has (a merge needs a mainline), and what it
// says (the confirmation names it).
func (r *Runner) commitParentsAndSubject(ctx context.Context, dir, sha string) (parents []string, subject string, err error) {
	output, err := r.Run(ctx, dir, "log", "-1", "--pretty=format:%P%x00%s", sha)
	if err != nil {
		return nil, "", err
	}
	fields := strings.SplitN(string(output), fieldSeparator, 2)
	if len(fields) != 2 {
		return nil, "", fmt.Errorf("unreadable commit summary %q, want parents and subject", output)
	}
	return strings.Fields(fields[0]), fields[1], nil
}

// isAncestor asks whether commit is reachable from tip.
//
// `git merge-base --is-ancestor` grades its own failure: 0 is yes, 1 is no,
// and anything else is something gone wrong. The exit code is read rather than
// passed on, for the same reason shareAncestor reads one: a repository that
// cannot be read must never be reported as "already on the branch".
func (r *Runner) isAncestor(ctx context.Context, dir, commit, tip string) (bool, error) {
	_, err := r.Run(ctx, dir, "merge-base", "--is-ancestor", commit, tip)
	if err == nil {
		return true, nil
	}

	var failure *Error
	if errors.As(err, &failure) && failure.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
