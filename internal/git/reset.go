package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Resetting the branch HEAD is on to a selected commit.
//
// Three modes, three different promises about the three trees a reset can
// touch: HEAD, the index, and the work tree. Soft moves only HEAD. Mixed moves HEAD and
// the index. Hard moves all three. The mode is a choice the client makes, not a
// fact the daemon reads — the same shape pull's strategy has — so every command
// pins `--soft`, `--mixed` or `--hard` rather than leaving git's default of
// mixed to be what the line on screen "meant". See docs/adr/0021.
//
// The revision reaches git as the commit argument of `git reset --<mode>`,
// not after `--`. That separator would turn it into a pathspec — "Cannot do
// hard reset with paths" — which is the opposite of what every other command
// in this package uses `--` for. checkRevision still refuses a leading dash
// before the argument is assembled.
//
// A commit that is not an ancestor of HEAD is refused rather than offered as a
// move onto foreign history. Resetting the branch to a tip it never held is a
// real operation, and a different sentence from "take this branch back to a
// commit it already passed through" — which is what the confirmation says.
// HEAD itself is an ancestor of HEAD, so resetting to the tip is offered: soft
// and mixed become ways to clear the index, and hard becomes a discard of
// uncommitted work.

// ResetMode is which of the three trees a reset moves.
type ResetMode string

const (
	// ResetSoft: HEAD moves; the index and the work tree stay where they are.
	// The commits past the target become staged changes against the new tip.
	ResetSoft ResetMode = "soft"

	// ResetMixed: HEAD and the index move; the work tree stays. The commits
	// past the target become unstaged changes against the new tip. git's own
	// default when no flag is given — pinned here so the dialog never means
	// something else on a machine that changed that default.
	ResetMixed ResetMode = "mixed"

	// ResetHard: HEAD, the index and the work tree all move. Commits past the
	// target leave the branch, and any uncommitted change the work tree held
	// is discarded with them.
	ResetHard ResetMode = "hard"
)

// errUnknownResetMode names the three, because every refusal here is somebody
// being told which words the field takes.
var errUnknownResetMode = errors.New("the reset mode must be soft, mixed or hard")

// ErrResetNotOnBranch: the commit is not reachable from HEAD.
//
// A reset of the current branch to a selected commit is taking that branch
// back along its own history. Offering one for a commit the branch never held
// would move it onto foreign history, and the confirmation's sentence would
// name the wrong thing.
var ErrResetNotOnBranch = errors.New("the commit is not on the branch HEAD is on")

// ParseResetMode reads a mode sent by a client.
//
// An unknown one is refused rather than read as mixed, for the reason
// ParsePullStrategy refuses one: running the wrong mode would look entirely
// correct to a client that asked for another.
func ParseResetMode(raw string) (ResetMode, error) {
	switch ResetMode(raw) {
	case ResetSoft, ResetMixed, ResetHard:
		return ResetMode(raw), nil
	case "":
		return "", fmt.Errorf("%w: none was given", errUnknownResetMode)
	default:
		return "", fmt.Errorf("%w: %q is not one", errUnknownResetMode, raw)
	}
}

// ResetArgs is the command Reset runs.
//
// Exported for the reason RevertArgs is: the line the user is shown and the
// line git receives have one definition between them. The mode flag is always
// present — never a bare `git reset`, whose meaning is mixed only by default.
//
// No `--` before the revision. Unlike revert and cherry-pick, `git reset`
// reads everything after `--` as pathspecs: `git reset --hard -- abc` is
// "hard-reset the path named abc", which is why git answers "Cannot do hard
// reset with paths." The leading-dash refusal is checkRevision's, and the
// mode flag has already consumed the option slot.
func ResetArgs(commit string, mode ResetMode) []string {
	return []string{"reset", "--" + string(mode), commit}
}

// Reset moves the branch HEAD is on to commit, in the named mode.
//
// `into` is the name of that branch, and it is passed rather than read: git
// resets wherever HEAD is whatever this says, so the name is here for the
// caller to have checked and for nothing else.
//
// rewriteTimeout rather than the deadline for commands that return instantly,
// because `--hard` is one of the commands that comment is about: it writes the
// whole work tree, and on a large repository — or one whose files go through a
// smudge filter — that is minutes of legitimate work. Killed at thirty seconds
// it would leave a half-written tree and an index.lock, and tell the user their
// reset timed out. Soft and mixed finish in milliseconds either way: the
// deadline is a ceiling, not a wait.
func (r *Runner) Reset(ctx context.Context, dir, commit string, mode ResetMode) error {
	commit = strings.TrimSpace(commit)
	if err := checkRevision(commit); err != nil {
		return err
	}
	if _, err := ParseResetMode(string(mode)); err != nil {
		return err
	}

	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    ResetArgs(commit, mode),
		Timeout: rewriteTimeout,
	})
	return err
}

// ResetPreview is what resetting to one commit would do, read from HEAD and
// from the work tree rather than assumed.
type ResetPreview struct {
	Mode ResetMode

	// Commit is the full object name. The client may have sent a short SHA;
	// everything that follows — the command, the lease, the toast — uses the
	// one git resolved, so two forms of the same name cannot disagree.
	Commit string

	// Subject is the commit's first line, for a confirmation that has to name
	// what the branch is moving back to rather than only a hash.
	Subject string

	// Dropping is how many commits sit on HEAD past the target
	// (`rev-list --count target..HEAD`). Zero means the target is HEAD itself:
	// soft and mixed then act on the index alone, and hard discards only
	// uncommitted work.
	Dropping int

	// DirtyFiles is how many TRACKED paths differ from HEAD or from the index
	// — what a hard reset actually puts back. Untracked files are left out
	// because `git reset --hard` leaves them exactly where they are, and a
	// confirmation that counted them would name a loss that does not happen,
	// which is how people learn to read past the one that does. Counted rather
	// than listed: the confirmation names the count, and the changes panel
	// already shows which.
	DirtyFiles int
}

// ResetTarget resolves the commit a reset would move to, and refuses one the
// branch never held.
//
// Three readings, in the order that refuses earliest:
//
//  1. The mode must be one of the three.
//  2. The revision must resolve to a commit.
//  3. A commit that is not an ancestor of HEAD is refused — see
//     ErrResetNotOnBranch.
//
// Separate from PreviewReset because the run route needs exactly this and
// nothing else: the counts below exist for a sentence on a confirmation, and
// reading them again to throw them away costs a second walk of the whole work
// tree on every reset. headSHA comes back so PreviewReset does not ask for it
// twice.
func (r *Runner) ResetTarget(
	ctx context.Context, dir, commit string, mode ResetMode,
) (sha, subject, headSHA string, err error) {
	if _, err := ParseResetMode(string(mode)); err != nil {
		return "", "", "", err
	}

	sha, _, subject, err = r.resolveCommit(ctx, dir, commit)
	if err != nil {
		return "", "", "", err
	}

	head, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", "", err
	}
	headSHA = strings.TrimSpace(string(head))

	ancestor, err := r.isAncestor(ctx, dir, sha, headSHA)
	if err != nil {
		return "", "", "", err
	}
	if !ancestor {
		return "", "", "", fmt.Errorf(
			"%w: %s is not reachable from HEAD", ErrResetNotOnBranch, ShortSHA(sha))
	}

	return sha, subject, headSHA, nil
}

// PreviewReset reads what resetting HEAD to commit in the named mode would do.
//
// The target and its refusals are ResetTarget's. What this adds is the two
// counts a confirmation needs: how many commits the branch would leave behind,
// and how many tracked paths currently differ. Neither is a refusal — a clean
// reset to HEAD is a real (if empty) operation, and a dirty soft reset is the
// ordinary way to keep work while moving the tip.
func (r *Runner) PreviewReset(ctx context.Context, dir, commit string, mode ResetMode) (ResetPreview, error) {
	sha, subject, headSHA, err := r.ResetTarget(ctx, dir, commit, mode)
	if err != nil {
		return ResetPreview{}, err
	}

	dropping, err := r.countCommits(ctx, dir, sha+".."+headSHA)
	if err != nil {
		return ResetPreview{}, err
	}

	status, err := r.Status(ctx, dir)
	if err != nil {
		return ResetPreview{}, err
	}

	return ResetPreview{
		Mode:       mode,
		Commit:     sha,
		Subject:    subject,
		Dropping:   dropping,
		DirtyFiles: status.DirtyTracked(),
	}, nil
}

// countCommits is `git rev-list --count [filters] range`. Zero is a real
// answer — the target is HEAD — and an unreadable number is a broken git, not
// a zero.
//
// The filters are what let rebase's merge count share this: `--merges` narrows
// the same walk, and one parse of one number is enough for both.
func (r *Runner) countCommits(ctx context.Context, dir, revRange string, filters ...string) (int, error) {
	args := append([]string{"rev-list", "--count"}, filters...)
	args = append(args, revRange)

	output, err := r.Run(ctx, dir, args...)
	if err != nil {
		return 0, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("unreadable rev-list count %q: %w", output, err)
	}
	return count, nil
}
