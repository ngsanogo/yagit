package git

import (
	"context"
	"errors"
	"strings"
)

// Branches: standing on one, and making and unmaking them.
//
// The three rules at the top of staging.go hold here too, and the first one
// is what this file leans on hardest: the reference reaches git after `--`, so
// a branch somebody named `-f` arrives as a branch. git then refuses it by
// name — "'-f' is not a valid branch name" — which is the answer, rather than
// an unknown-option error about a request nobody made.
//
// `git switch` throughout, never `git checkout`. The two overlap, and where
// they differ `checkout` is the one that guesses: `git checkout main` switches
// to the branch unless a file called `main` exists in the work tree, in which
// case it restores that file over whatever was written in it — same command,
// same exit code, a different operation. `switch` takes a reference and
// nothing else, so the line in the log panel can only have meant the thing the
// button said.
//
// The rule earns more here than it does anywhere else in the package, because
// `git branch` takes its VERB as an option. `git branch -m release`, in a
// repository on main, does not create a branch called `-m`: it renames main to
// release, reports success, and the branch somebody was standing on is gone
// under a name they typed into a "new branch" box. Every name below reaches
// git where git will read it as a name.
//
// Where that is depends on the command, and the difference is not decoration.
// `git branch` and `git switch <name>` take theirs after `--`. `git switch
// --create` takes its name as the OPTION'S OWN ARGUMENT, so `--create --
// feature` reads feature as the start point and leaves --create empty; there
// the separator goes before the start point instead, and the name is safe
// because a value consumed by an option cannot be re-read as one.

// ErrNoRef: nothing was named to check out.
//
// Refused here rather than handed to git, which answers a missing argument
// with a sentence about its own option parsing — true, and about the wrong
// thing.
var ErrNoRef = errors.New("no reference given")

// SwitchArgs is the command Switch runs. Exported for the reason
// DiscardTrackedArgs is: the line the user is shown and the line git receives
// have one definition between them.
//
// --no-guess is what keeps this from creating anything. Without it, `git
// switch feature` in a repository that has no local `feature` but does have
// `origin/feature` creates a local branch and sets its upstream — a branch
// nobody asked for, from a button that said "check out". Creating one is a
// different operation, with a name to choose and a start point to choose it
// from.
func SwitchArgs(name string) []string {
	return []string{"switch", "--no-guess", "--", name}
}

// DetachArgs is the command Detach runs.
//
// A function of its own rather than a flag on the one above, because they
// answer different requests. `git switch --detach main` leaves HEAD on the
// commit main points at and no longer on main, which is not what "check out
// main" means to anybody — and it is exactly what "check out this commit"
// means, for a tag, a remote-tracking branch or a row of the history, none of
// which HEAD can sit on any other way.
func DetachArgs(revision string) []string {
	return []string{"switch", "--detach", "--", revision}
}

// Switch moves HEAD onto a branch that already exists.
//
// Uncommitted work is git's business and not this function's. git carries a
// change across when the checkout would not overwrite it, and refuses the
// whole switch when it would, naming the files it would have clobbered. Both
// answers are right, and both reach the user whole; a client that quietly
// stashed on their behalf would be making that decision for them, in a
// repository where the stash is then somebody else's to find.
func (r *Runner) Switch(ctx context.Context, dir, name string) error {
	if name == "" {
		return ErrNoRef
	}
	// rewriteTimeout, not the Runner's thirty seconds. A checkout rewrites the
	// whole work tree, runs the clean and smudge filters over every file it
	// touches, and then runs the user's post-checkout hook. None of that is a
	// hang, and a repository routing large assets through LFS spends the time
	// downloading them.
	//
	// The thirty-second deadline is for commands that return instantly, and
	// what it costs here is not a slow answer but a broken work tree: SIGKILL
	// lands mid-checkout, leaving half the files on one branch and half on the
	// other, possibly an index.lock, and a message blaming the daemon's own
	// deadline for an operation the documentation says is supported.
	_, err := r.Exec(ctx, Command{Dir: dir, Args: SwitchArgs(name), Timeout: rewriteTimeout})
	return err
}

// Detach checks out a commit with no branch on it.
//
// The state git calls detached HEAD, and it is a real place to be: it is where
// you look at an old commit, and where a tag or a remote-tracking branch can
// be checked out at all. What it is not is a place to commit from by accident,
// which is why the interface says so on the screen for as long as it lasts.
func (r *Runner) Detach(ctx context.Context, dir, revision string) error {
	if revision == "" {
		return ErrNoRef
	}
	// rewriteTimeout for the reason Switch names: this is a checkout too.
	_, err := r.Exec(ctx, Command{Dir: dir, Args: DetachArgs(revision), Timeout: rewriteTimeout})
	return err
}

// ErrNoBranchName: nothing was named to create, rename or delete.
//
// Refused here rather than handed to git, which answers a missing name by
// listing the branches and exiting 0 — a success for an operation that did
// not happen.
var ErrNoBranchName = errors.New("no branch name given")

// CreateBranchArgs is the command CreateBranch runs.
//
// Exported for the reason DiscardTrackedArgs is: the line the user is shown
// and the line git receives have one definition between them.
//
// The start point is optional and means HEAD when it is absent — git's own
// default, left to git rather than resolved here, so the command in the log
// panel is the one a person would have typed.
func CreateBranchArgs(name, start string) []string {
	args := []string{"branch", "--", name}
	if start != "" {
		args = append(args, start)
	}
	return args
}

// RenameBranchArgs is the command RenameBranch runs.
//
// `-m`, not `-M`: the capital overwrites a branch that already has the new
// name, silently, and the branch it overwrites may be the only thing pointing
// at a line of work. git refuses the collision by name instead, and that
// refusal is worth more than the flag that hides it.
func RenameBranchArgs(from, to string) []string {
	return []string{"branch", "-m", "--", from, to}
}

// DeleteBranchArgs is the command DeleteBranch runs.
//
// `-d` refuses a branch holding commits no other branch can reach; `-D` does
// it anyway. Which one runs is the caller's to say and the user's to see: the
// interface asks first, and what it shows is this line.
func DeleteBranchArgs(name string, force bool) []string {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return []string{"branch", flag, "--", name}
}

// CreateAndSwitchArgs is the command CreateAndSwitch runs.
//
// Note where the `--` is not: `git switch --create -- feature` reads `feature`
// as the START POINT and leaves --create with no name, because the name is the
// option's own argument. That is also what makes it safe — a value consumed by
// --create cannot be read as an option, so a branch called `-m` is refused by
// name here exactly as it is next door. The separator still goes before the
// start point, which git does read as a revision.
//
// One command rather than a create followed by a switch: `git branch` then
// `git switch` can leave a branch made and not stood on when the second half
// is refused — uncommitted work in the way — and the person is then somewhere
// they did not ask to be with a branch they did not know they had.
func CreateAndSwitchArgs(name, start string) []string {
	args := []string{"switch", "--create", name}
	if start != "" {
		args = append(args, "--", start)
	}
	return args
}

// CreateBranch adds a local branch without checking it out.
//
// Making a branch and standing on it are two different requests, and the
// interface asks which one this is. See CreateAndSwitch for the other.
func (r *Runner) CreateBranch(ctx context.Context, dir, name, start string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoBranchName
	}
	_, err := r.Run(ctx, dir, CreateBranchArgs(name, strings.TrimSpace(start))...)
	return err
}

// CreateAndSwitch adds a local branch and moves HEAD onto it.
func (r *Runner) CreateAndSwitch(ctx context.Context, dir, name, start string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoBranchName
	}
	// rewriteTimeout for the reason Switch names: this is a checkout too.
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    CreateAndSwitchArgs(name, strings.TrimSpace(start)),
		Timeout: rewriteTimeout,
	})
	return err
}

// RenameBranch changes a local branch's name.
//
// The branch HEAD is on can be renamed, and git carries HEAD across with it.
// That is the one case where an operation in this file moves HEAD, it is
// git's behaviour rather than this function's, and it is what anybody
// renaming the branch they are working on expects.
func (r *Runner) RenameBranch(ctx context.Context, dir, from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return ErrNoBranchName
	}
	_, err := r.Run(ctx, dir, RenameBranchArgs(from, to)...)
	return err
}

// BranchTip is the object a local branch points at.
//
// Its one caller reads it immediately before deleting that branch, which is
// the only moment it can be read at all: the delete takes the branch's reflog
// with it and leaves nothing in HEAD's, so a tip not read here is a tip no
// undo can find (docs/adr/0032). Named after the fact rather than after
// rev-parse, because a general resolver on this Runner would be an invitation
// to route revisions past the checks the operations do.
func (r *Runner) BranchTip(ctx context.Context, dir, name string) (string, error) {
	if err := checkBranchName(name); err != nil {
		return "", err
	}
	output, err := r.Run(ctx, dir, "rev-parse", "--verify", "refs/heads/"+strings.TrimSpace(name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// DeleteBranch removes a local branch.
//
// git refuses to delete the branch HEAD is on, and refuses an unmerged one
// unless force says otherwise. Both refusals travel to the screen whole: the
// second one is git naming commits that are about to become unreachable, which
// is the sentence the person deciding needs to read.
func (r *Runner) DeleteBranch(ctx context.Context, dir, name string, force bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoBranchName
	}
	_, err := r.Run(ctx, dir, DeleteBranchArgs(name, force)...)
	return err
}
