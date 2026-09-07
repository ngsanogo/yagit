package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// `git worktree`: the linked checkouts one repository can have.
//
// Not to be confused with staging.go beside it, which is about THE work tree —
// the files and the index of whichever checkout a command runs in. This file
// is about having several of them: one repository, one object database, and a
// directory per branch somebody wants open at the same time.
//
// yagit already understood them before it could make one: repo.Repo keeps the
// common git directory and this checkout's own apart, because refs and objects
// are shared while HEAD, the index and a rebase's state are not.
//
// The list is read with `-z` where git has it. `--porcelain` on its own prints
// paths raw and unquoted — which is exactly why git added `-z` in 2.36 — so a
// directory holding a newline splits one worktree into two unreadable halves.
// That is the same class of bug the NUL record terminator in logFormat exists
// for, and it is answered the same way.
//
// Where git has not got it, the list is read without it rather than refused,
// and that is the one place in this project where a missing flag is worked
// around instead of allowed to fail. The rule it bends to is the rule it comes
// from: a git under the line "fails loudly on that one operation and nowhere
// else". This is not one operation. The worktree panel is drawn beside the
// references of every repository, so `-z` on a git from 2021 would not be a
// refused button — it would be a red panel on every screen, for a repository
// with one checkout and no interest in having two.
//
// What the fallback costs is exactly what `-z` was added to buy: on git before
// 2.36 a worktree whose path holds a newline is read as two, and no output
// from that git can say otherwise. Everything else about the list is
// identical, which is why one parser reads both.

// ErrNoWorktreePath: nothing was named to make or remove.
var ErrNoWorktreePath = errors.New("no worktree path given")

// ErrMainWorktree: the main working tree cannot be removed by `git worktree`.
//
// git refuses it too — "fatal: 'x' is a main working tree" — but the button
// should not have been there, and the list already knows which one it is.
var ErrMainWorktree = errors.New("the main working tree cannot be removed this way")

// Worktree is one checkout of a repository.
type Worktree struct {
	// Path is the directory it lives in — its identity, and what every
	// `git worktree` subcommand takes.
	Path string `json:"path"`

	// HEAD is the commit checked out there. Empty for a bare repository,
	// which has no checkout at all, and for one whose branch is unborn.
	HEAD string `json:"head"`

	// Branch is the short name of the branch checked out there, empty when
	// detached or bare.
	Branch string `json:"branch"`

	Detached bool `json:"detached"`
	Bare     bool `json:"bare"`

	// Locked says somebody marked this worktree as not-to-be-pruned — the
	// usual reason being that it lives on a drive that is not always mounted,
	// where "the directory is gone" and "the directory is unreachable" look
	// the same to git.
	Locked     bool   `json:"locked"`
	LockReason string `json:"lock_reason"`

	// Prunable says git considers the administrative files stale: the
	// directory is gone, or it is no longer a worktree.
	Prunable       bool   `json:"prunable"`
	PrunableReason string `json:"prunable_reason"`

	// Main is the working tree the repository was made in. It is always the
	// first entry git lists, and it is the one `git worktree remove` refuses.
	Main bool `json:"main"`
}

// worktreeListNUL is the git that has `git worktree list -z`.
const worktreeListNULMajor, worktreeListNULMinor = 2, 36

// WorktreeListArgs is the command Worktrees runs. nul says whether this git
// takes `-z`, which GitVersion answers and the caller passes on.
func WorktreeListArgs(nul bool) []string {
	args := []string{"worktree", "list", "--porcelain"}
	if nul {
		args = append(args, "-z")
	}
	return args
}

// Worktrees lists every checkout of the repository, the main one first.
func (r *Runner) Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	nul, err := r.worktreeListTakesNUL(ctx)
	if err != nil {
		return nil, err
	}

	output, err := r.Run(ctx, dir, WorktreeListArgs(nul)...)
	if err != nil {
		return nil, err
	}
	return ParseWorktrees(output, nul)
}

// worktreeListTakesNUL reports whether this git has the flag.
//
// The version failing travels rather than being read as "old": a git that
// cannot say what it is, is a git the very next command will fail on anyway,
// and guessing here would turn one legible error into a silently degraded
// list.
func (r *Runner) worktreeListTakesNUL(ctx context.Context) (bool, error) {
	version, err := r.GitVersion(ctx)
	if err != nil {
		return false, err
	}
	return version.AtLeast(worktreeListNULMajor, worktreeListNULMinor), nil
}

// ParseWorktrees turns `worktree list --porcelain` into worktrees, in either
// of the two shapes git prints it in.
//
// Pure and exported for the reason ParseLog is: a wrong split here is a
// plausible wrong answer — a button that removes the checkout next to the one
// it named — rather than a crash.
//
// The shape is a sequence of terminated attributes, one record ending where an
// empty attribute appears. Each attribute is either a bare keyword (`bare`,
// `detached`) or a keyword, one space, and a value. nul says which terminator
// git used: NUL under `-z`, and a newline without it — the same grammar, and
// the reason the older output can be read at all.
func ParseWorktrees(output []byte, nul bool) ([]Worktree, error) {
	terminator := fieldSeparator
	if !nul {
		terminator = "\n"
	}

	// Without `-z` git ends the last record with a blank line, so the split
	// leaves a trailing empty field — harmless, since an empty field is what
	// finishes a record.
	//
	// Nothing is done about \r\n, and that is deliberate: git writes LF here
	// on every platform, so every \r in this output belongs to the bytes it
	// describes. A directory called `build\r` is legal on every filesystem
	// yagit runs on, and git prints it `worktree /repos/build\r` followed by
	// its LF — so normalising \r\n away would take the last character off its
	// path and leave a row whose remove button names a directory that does
	// not exist.
	fields := strings.Split(string(output), terminator)

	worktrees := make([]Worktree, 0, 4)
	var current *Worktree

	finish := func() {
		if current != nil {
			// The main working tree is the one git lists first, which is
			// documented behaviour and the only way to tell it apart: nothing
			// in the record says so.
			current.Main = len(worktrees) == 0
			worktrees = append(worktrees, *current)
			current = nil
		}
	}

	for _, field := range fields {
		if field == "" {
			finish()
			continue
		}

		keyword, value, _ := strings.Cut(field, " ")
		switch keyword {
		case "worktree":
			// A second `worktree` line without a blank between records would
			// mean git changed the format under us; finishing here rather than
			// overwriting keeps the earlier entry rather than losing it.
			finish()
			if value == "" {
				return nil, fmt.Errorf("worktree list: an entry with no path in %q", field)
			}
			current = &Worktree{Path: value}
		case "HEAD", "branch", "bare", "detached", "locked", "prunable":
			if current == nil {
				return nil, fmt.Errorf("worktree list: %q before any worktree line", field)
			}
			applyWorktreeAttribute(current, keyword, value, nul)
		default:
			// An attribute a later git added. Ignored rather than refused: the
			// fields this reads are the ones it acts on, and a new keyword is
			// not a reason to show nothing.
			continue
		}
	}
	finish()

	return worktrees, nil
}

func applyWorktreeAttribute(worktree *Worktree, keyword, value string, nul bool) {
	switch keyword {
	case "HEAD":
		worktree.HEAD = value
	case "branch":
		// git writes the full ref; the interface names branches short, and so
		// does every other list in this package.
		worktree.Branch = strings.TrimPrefix(value, "refs/heads/")
	case "bare":
		worktree.Bare = true
	case "detached":
		worktree.Detached = true
	case "locked":
		worktree.Locked = true
		worktree.LockReason = worktreeReason(value, nul)
	case "prunable":
		worktree.Prunable = true
		worktree.PrunableReason = worktreeReason(value, nul)
	}
}

// worktreeReason reads the sentence a `locked` or `prunable` attribute carries.
//
// The one place the two output shapes differ in more than their terminator.
// Under `-z` git writes the reason raw. Without it the reason is the only
// field git C-quotes — a lock reason of two lines comes back as
// `"needs a\nrebuild"`, quotes and escape included — because a line-based
// format has no other way to carry a newline. Undone with the package's own
// unquotePath, so a person reads the sentence they wrote rather than the way
// git spelled it for a format yagit is only falling back to.
func worktreeReason(value string, nul bool) string {
	if nul {
		return value
	}
	unquoted, err := unquotePath(value)
	if err != nil {
		// A reason that will not unquote is still a reason, and nothing acts
		// on it: git's own bytes on screen beat a row that lost its
		// explanation over a quote git wrote and this could not read back.
		return value
	}
	return unquoted
}

// AddWorktreeArgs is the command AddWorktree runs.
//
// Three shapes, and which one runs is the caller's to say and the user's to
// see:
//
//   - a branch that exists, checked out in the new directory;
//   - `-b <name>`, making the branch there;
//   - `--detach`, for a tag or a commit, which are not places HEAD can sit on
//     a branch — the same reading branch.go makes for `git switch`.
//
// The path and the commit-ish land after `--`, so a directory named `-f`
// arrives as a directory.
func AddWorktreeArgs(path, ref, newBranch string, detach bool) []string {
	args := []string{"worktree", "add"}
	if detach {
		args = append(args, "--detach")
	}
	if newBranch != "" {
		// The name is the option's own argument, so it cannot be re-read as an
		// option — the same placement CreateAndSwitchArgs relies on.
		args = append(args, "-b", newBranch)
	}
	args = append(args, "--", path)
	if ref != "" {
		args = append(args, ref)
	}
	return args
}

// AddWorktree makes another checkout of this repository at path.
//
// git refuses a branch that is already checked out somewhere else — one branch,
// one working tree — and that refusal travels to the screen whole: it names
// the directory holding it, which is the thing the person needs.
func (r *Runner) AddWorktree(ctx context.Context, dir, path, ref, newBranch string, detach bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return ErrNoWorktreePath
	}
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if err := checkRevision(ref); err != nil {
			return err
		}
	}
	newBranch = strings.TrimSpace(newBranch)
	if newBranch != "" {
		if err := checkBranchName(newBranch); err != nil {
			return err
		}
	}

	_, err := r.Exec(ctx, Command{
		Dir: dir,
		// A worktree add writes a whole checkout, and on a large repository —
		// or one whose files go through a smudge filter — that is minutes of
		// legitimate work. Same reasoning as a hard reset's.
		Args:    AddWorktreeArgs(path, ref, newBranch, detach),
		Timeout: rewriteTimeout,
	})
	return err
}

// RemoveWorktreeArgs is the command RemoveWorktree runs.
//
// `--force` is what gets past a checkout holding uncommitted work, and it is
// therefore never the default: git's refusal names what is in the way, and the
// confirmation shows which of the two lines is about to run.
func RemoveWorktreeArgs(path string, force bool) []string {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	return append(args, "--", path)
}

// RemoveWorktree deletes a linked checkout and the administrative files that
// point at it.
func (r *Runner) RemoveWorktree(ctx context.Context, dir, path string, force bool) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return ErrNoWorktreePath
	}
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    RemoveWorktreeArgs(path, force),
		Timeout: rewriteTimeout,
	})
	return err
}

// PruneWorktreesArgs is the command PruneWorktrees runs.
func PruneWorktreesArgs() []string {
	return []string{"worktree", "prune"}
}

// PruneWorktrees forgets the checkouts whose directories are gone.
//
// It deletes nothing anybody can still reach: what it removes is git's record
// of a directory that is no longer there. A locked worktree is left alone,
// which is what locking is for — a checkout on a drive that is not mounted
// looks exactly like one somebody deleted.
func (r *Runner) PruneWorktrees(ctx context.Context, dir string) error {
	_, err := r.Run(ctx, dir, PruneWorktreesArgs()...)
	return err
}
