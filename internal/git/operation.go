package git

import (
	"context"
	"errors"
	"fmt"
)

// The three ways out of an operation that stopped.
//
// state.go reads what a repository is in the middle of; this file is what can
// be done about it. Every one of these is a single git command, and the whole
// of the knowledge is the table in ActionArgs — which pairs are real, and what
// each of them runs.
//
// Two of the three destroy work and the interface asks first, showing the line
// from ActionArgs rather than a sentence about it. Which of them destroys what
// is in the comments on the table, because that is the sentence somebody
// deciding needs and the wrong answer to it is unrecoverable.

// Action is what to do about an operation in progress.
//
// Named for git's own three flags, and it is the whole vocabulary: there is no
// fourth thing to do about a stopped rebase.
type Action string

const (
	// ActionAbort undoes the operation and puts the repository back where it
	// started. It throws away every conflict resolved since it began.
	ActionAbort Action = "abort"

	// ActionContinue records what has been resolved and moves to the next
	// commit, or finishes. The one action here that destroys nothing.
	ActionContinue Action = "continue"

	// ActionSkip drops the commit being applied and moves to the next one.
	// The commit is not applied and its changes are not kept.
	ActionSkip Action = "skip"
)

// ErrNoOperation: nothing is in progress, so there is nothing to act on.
//
// A state rather than a failure, and named so the interface can tell it from a
// git command that refused. It is what a stale banner produces — the button
// was drawn from a status read two seconds ago, and the rebase finished in a
// terminal in between.
var ErrNoOperation = errors.New("this repository is not in the middle of an operation")

// ErrActionUnavailable: the operation is real and the action has no meaning
// for it.
//
// `git merge --skip` does not exist, and neither does continuing a bisect. The
// interface draws only the buttons that apply, so reaching this means the
// repository moved between the drawing and the click — a merge that became a
// rebase — which is exactly the case that must not run a command nobody chose.
var ErrActionUnavailable = errors.New("this operation cannot be given that instruction")

// ActionArgs is the command one action on one operation runs.
//
// Exported for the reason DeleteBranchArgs is: the line the confirmation shows
// and the line git receives have one definition between them. A dialog that
// built its own text would be a second definition, and the stale half of it
// would promise a `git rebase --abort` while the daemon ran something else.
//
// The table is the file. Reading down it:
//
//   - A merge is not continued here, and the omission is the deliberate part.
//     `git merge --continue` commits git's own MERGE_MSG, and yagit already
//     has a way to finish a merge that is strictly better: the commit box,
//     which shows that same message and lets it be edited first. Two buttons
//     for one ending, where one of them silently discards what was typed into
//     the other, is not a choice worth offering.
//
//   - Everything else that commits is continued here anyway, and the
//     difference is that for those the commit box is not an ending. Finishing
//     a cherry-pick of three commits by hand commits the first and leaves the
//     other two unapplied — the sequence needs --continue whatever else
//     happens, so this is the only way out rather than a second one. What it
//     commits is the replayed commit's own message, never a draft, and the
//     button on screen says so instead of leaving it to be discovered.
//
//   - A bisect is ended with `git bisect reset`, which is what leaving one is
//     called. It is not continued either: continuing a bisect means saying
//     whether this commit is good or bad, and yagit does not ask that yet. A
//     button offering to continue would have to guess the answer.
//
//   - Everything that replays commits — a rebase, a cherry-pick, a revert, a
//     mailbox — takes all three, because all three are things git will do to
//     a half-finished sequence.
func ActionArgs(operation Operation, action Action) ([]string, error) {
	// The action is checked against the three before anything is built from
	// it, and this is the line that makes the rest of the file safe. Every
	// branch below spells its command as `--` and the action, so an
	// unrecognised one would not be refused — it would become a flag. A
	// request naming `exec` would run `git rebase --exec`, which takes a
	// shell command as its argument, from a route whose whole vocabulary is
	// meant to be three words.
	//
	// Refusing by name is also the honest answer to a typo: `git rebase
	// --contnue` fails with git explaining its own option parsing, about a
	// request nobody made.
	switch action {
	case ActionAbort, ActionContinue, ActionSkip:
	default:
		return nil, fmt.Errorf("%w: %q is not abort, continue or skip", ErrActionUnavailable, action)
	}

	// Named here rather than repeated in six cases below. The command is the
	// subcommand plus the flag for all but the bisect, which spells its
	// ending differently.
	flag := "--" + string(action)

	switch operation {
	case OperationNone:
		return nil, ErrNoOperation

	case OperationMerge:
		if action != ActionAbort {
			return nil, fmt.Errorf("%w: a merge can only be aborted, or finished by committing", ErrActionUnavailable)
		}
		// Throws away every conflict resolved since the merge began, and puts
		// the work tree and the index back where they were.
		return []string{"merge", flag}, nil

	case OperationBisect:
		if action != ActionAbort {
			return nil, fmt.Errorf("%w: a bisect ends by marking a commit good or bad", ErrActionUnavailable)
		}
		// Not `--abort`, which git does not have here. `reset` checks the
		// original branch back out and forgets every verdict given so far.
		return []string{"bisect", "reset"}, nil

	case OperationRebase:
		// --abort returns to the branch the rebase started from, discarding
		// every commit it has replayed. --skip drops the commit it stopped
		// on entirely.
		return []string{"rebase", flag}, nil

	case OperationCherryPick:
		return []string{"cherry-pick", flag}, nil

	case OperationRevert:
		return []string{"revert", flag}, nil

	case OperationApply:
		// `git am`, whose three flags are spelled exactly like the others'.
		// --abort restores the branch the mailbox was being applied to.
		return []string{"am", flag}, nil
	}

	return nil, fmt.Errorf("%w: %q is not an operation yagit knows", ErrActionUnavailable, operation)
}

// ActOnOperation runs one action on the operation a repository is in the
// middle of.
//
// The operation is passed in rather than read here, and that is the point of
// the argument: the caller has already checked that what it is about to act on
// is what the user was shown. See handleOperation in internal/api, which is
// where that check lives and why.
//
// A git command that refuses travels back whole — exit code, stderr, the line
// that ran. `git rebase --continue` with a file still unmerged is the ordinary
// case, and git's own answer to it names the file and says to add it, which is
// a better sentence than any this package could write.
//
// Not Run, because both of the things Run leaves at their defaults are wrong
// here. These commands replay commits and check out whole trees, so they take
// rewriteTimeout rather than the deadline meant for commands that return
// instantly — a rebase killed halfway through is a repository nobody asked
// for. And --continue commits, under a message git prepares and opens an
// editor over, so it says that it accepts that message; which rebases may be
// continued at all is State.Blocked's answer, not this one's.
func (r *Runner) ActOnOperation(ctx context.Context, dir string, operation Operation, action Action) error {
	args, err := ActionArgs(operation, action)
	if err != nil {
		return err
	}

	_, err = r.Exec(ctx, Command{
		Dir:                    dir,
		Args:                   args,
		Timeout:                rewriteTimeout,
		AcceptsPreparedMessage: true,
	})
	return err
}

// Actions lists what can be done about an operation, in the order the
// interface draws them.
//
// One list rather than a rule the browser reinvents. The buttons on screen and
// the commands the daemon will accept come from the same table in ActionArgs,
// so a pair that is drawn is a pair that runs — and a pair that is not drawn
// is refused rather than quietly doing something else.
//
// Continue first, because it is what somebody who resolved their conflict
// wants and it is the only one of the three that destroys nothing.
func Actions(operation Operation) []Action {
	var available []Action
	for _, action := range []Action{ActionContinue, ActionSkip, ActionAbort} {
		if _, err := ActionArgs(operation, action); err == nil {
			available = append(available, action)
		}
	}
	return available
}

// Destroys says whether an action throws work away, and so whether the
// interface has to ask before running it.
//
// Here rather than in the browser because it is a fact about git, not about a
// screen: continuing records what was resolved and moves on, while the other
// two exist precisely to throw something away. A dialog that decided this for
// itself could be talked out of asking by a refactor; this cannot.
func Destroys(action Action) bool { return action != ActionContinue }
