package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Undo of the most recent HEAD-reflog entry yagit knows how to reverse.
//
// See docs/adr/0031-undo-reads-the-head-reflog.md: classify the tip, lease by
// object names, pin the reverse command. Commit/amend → soft reset.
// Checkout → switch or detach back to where HEAD came from. Reset → soft
// reset to the tip the branch held before it.
//
// Restoring a deleted branch is the one kind that is NOT a reflog reading, and
// it cannot be: `git branch -d` writes nothing to the HEAD reflog and deletes
// the branch's own reflog along with the branch, so afterwards the tip it held
// exists nowhere git will name. The caller passes what it read before running
// the delete (docs/adr/0032-a-deleted-branch-is-remembered-not-recalled.md);
// everything after that — the lease, the pinned command — is the same shape as
// the rest of this file.

// ErrNothingToUndo: the tip entry is not one yagit can reverse.
var ErrNothingToUndo = errors.New("nothing to undo")

// ErrCannotUndoRoot: the tip is the repository's first commit.
var ErrCannotUndoRoot = errors.New("the first commit cannot be undone this way")

// ErrUndoStale: HEAD or the reverse target is no longer what the plan read.
var ErrUndoStale = errors.New("what undo would reverse has moved")

// UndoKind is which tip action would be reversed.
type UndoKind string

const (
	UndoCommit   UndoKind = "commit"
	UndoAmend    UndoKind = "amend"
	UndoCheckout UndoKind = "checkout"
	UndoReset    UndoKind = "reset"

	// UndoBranchDelete puts back a branch yagit deleted, at the object it
	// pointed at. The only kind whose fact comes from outside the reflog.
	UndoBranchDelete UndoKind = "branch-delete"
)

// ErrBranchIsBack: the branch a restore would make already exists again.
//
// Somebody recreated it — in a terminal, or in another tab — and `git branch`
// would refuse the collision anyway. Refused here so the offer disappears
// rather than becoming a button that always fails.
var ErrBranchIsBack = errors.New("a branch of that name exists again")

// ErrDeletedWorkIsGone: the object the deleted branch pointed at is no longer
// in the repository.
//
// Nothing else referenced it and `git gc` collected it. There is no restoring
// a branch onto a commit that does not exist, and saying so is better than
// `git branch` answering "not a valid object name".
var ErrDeletedWorkIsGone = errors.New("the commits that branch held have been collected")

// UndoPreview is what undoing the tip would run.
type UndoPreview struct {
	Kind UndoKind `json:"kind"`

	// Into is the branch HEAD is on now, empty when detached. Sent back so a
	// commit undo can refuse if HEAD left that branch; for checkout it is
	// display ("leave main").
	Into string `json:"into"`

	// Head is the commit HEAD points at now — the lease's near end.
	Head string `json:"head"`

	// To is the commit the reverse lands on.
	To string `json:"to"`

	// ToRef is what reaches git for a checkout undo (branch name or object
	// name). For commit/amend it equals To.
	ToRef string `json:"to_ref"`

	// Detach is set for a checkout undo that must leave HEAD detached
	// (returning to a raw commit, a tag tip, or a remote-tracking name).
	Detach bool `json:"detach"`

	// Branch is the local branch a restore would make. Empty for every other
	// kind: they move a ref that still exists, and this one makes one that
	// does not.
	Branch string `json:"branch"`

	// Subject is what the confirmation names — commit message, or the place
	// checkout returns to.
	Subject string `json:"subject"`

	Command string `json:"command"`
}

// PreviewUndo says what undoing the tip would run, without running it.
func (r *Runner) PreviewUndo(ctx context.Context, dir string) (UndoPreview, error) {
	entries, err := r.HeadReflog(ctx, dir, 3)
	if err != nil {
		return UndoPreview{}, err
	}
	if len(entries) == 0 {
		return UndoPreview{}, ErrNothingToUndo
	}

	head, err := r.ReadHEAD(ctx, dir)
	if err != nil {
		return UndoPreview{}, err
	}
	if head.SHA == "" {
		return UndoPreview{}, ErrNothingToUndo
	}
	if head.SHA != entries[0].SHA {
		return UndoPreview{}, fmt.Errorf(
			"%w: HEAD is %s but the reflog tip is %s",
			ErrUndoStale, shortForUndo(head.SHA), shortForUndo(entries[0].SHA))
	}

	into := ""
	if !head.Detached {
		into = head.Name
	}

	subject := entries[0].Subject
	switch {
	case strings.HasPrefix(subject, "commit (amend): "),
		strings.HasPrefix(subject, "commit (initial): "),
		strings.HasPrefix(subject, "commit: "):
		return r.previewUndoCommit(entries, into, head.SHA)
	case strings.HasPrefix(subject, "checkout: moving from "):
		return r.previewUndoCheckout(ctx, dir, entries[0], into, head.SHA)
	case strings.HasPrefix(subject, "reset: moving to "):
		return r.previewUndoReset(ctx, dir, entries, into, head.SHA)
	default:
		return UndoPreview{}, ErrNothingToUndo
	}
}

// Undo carries out a preview after the caller has checked its lease fields
// against a fresh PreviewUndo.
func (r *Runner) Undo(ctx context.Context, dir string, plan UndoPreview) error {
	head := strings.TrimSpace(plan.Head)
	to := strings.TrimSpace(plan.To)
	toRef := strings.TrimSpace(plan.ToRef)
	if err := checkRevision(head); err != nil {
		return err
	}
	if err := checkRevision(to); err != nil {
		return err
	}
	// toRef is the only name here that did not come from rev-parse: it is read
	// out of a reflog message, and it goes on to `rev-parse --verify` below
	// with no `--` in front of it. Checked for the same reason every other
	// revision reaching git is.
	if toRef == "" {
		return ErrNoRef
	}
	if err := checkRevision(toRef); err != nil {
		return err
	}

	if plan.Kind == UndoBranchDelete {
		if err := checkBranchName(plan.Branch); err != nil {
			return err
		}
	}

	current, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(current)) != head {
		return fmt.Errorf(
			"%w: HEAD is now %s, not %s — read undo again",
			ErrUndoStale, shortForUndo(strings.TrimSpace(string(current))), shortForUndo(head))
	}

	resolved, err := r.Run(ctx, dir, "rev-parse", "--verify", toRef+"^{commit}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(resolved)) != to {
		return fmt.Errorf(
			"%w: %s is now %s, not %s — read undo again",
			ErrUndoStale, toRef, shortForUndo(strings.TrimSpace(string(resolved))), shortForUndo(to))
	}

	switch plan.Kind {
	case UndoCommit, UndoAmend, UndoReset:
		return r.Reset(ctx, dir, to, ResetSoft)
	case UndoCheckout:
		if plan.Detach {
			return r.Detach(ctx, dir, toRef)
		}
		return r.Switch(ctx, dir, toRef)
	case UndoBranchDelete:
		// Re-read rather than trusted: the plan was made before the
		// confirmation was answered, and a branch of that name reappearing in
		// between is exactly the collision `git branch` would refuse — with a
		// message about branch creation, in reply to a button that said undo.
		if _, exists := r.revParse(ctx, dir, "refs/heads/"+plan.Branch); exists {
			return fmt.Errorf("%w: %s", ErrBranchIsBack, plan.Branch)
		}
		return r.CreateBranch(ctx, dir, plan.Branch, to)
	default:
		return fmt.Errorf("%w: %q", ErrNothingToUndo, plan.Kind)
	}
}

// PreviewUndoBranchDeletion says what putting a deleted branch back would run.
//
// name and sha are what the caller read before it ran the delete, and headWhen
// is where HEAD stood at that moment. That last one is how a deletion competes
// with the reflog for the single Undo offer without either of them holding a
// clock: while HEAD has not moved, the deletion is the most recent thing that
// happened to any ref in this repository and it is what Undo should reverse;
// once something has moved HEAD, that something is more recent and the reflog
// has it. Comparing timestamps instead would mean trusting a reflog date that
// git records to the second and writes for the commit rather than for the
// entry.
func (r *Runner) PreviewUndoBranchDeletion(
	ctx context.Context, dir, name, sha, headWhen string,
) (UndoPreview, error) {
	if err := checkBranchName(name); err != nil {
		return UndoPreview{}, err
	}
	if err := checkRevision(sha); err != nil {
		return UndoPreview{}, err
	}

	head, err := r.ReadHEAD(ctx, dir)
	if err != nil {
		return UndoPreview{}, err
	}
	if head.SHA == "" || head.SHA != headWhen {
		return UndoPreview{}, ErrNothingToUndo
	}

	if _, exists := r.revParse(ctx, dir, "refs/heads/"+name); exists {
		return UndoPreview{}, fmt.Errorf("%w: %s", ErrBranchIsBack, name)
	}

	to, ok := r.revParse(ctx, dir, sha+"^{commit}")
	if !ok {
		return UndoPreview{}, fmt.Errorf("%w: %s", ErrDeletedWorkIsGone, shortForUndo(sha))
	}
	_, _, subject, err := r.resolveCommit(ctx, dir, to)
	if err != nil {
		return UndoPreview{}, err
	}

	return UndoPreview{
		Kind:    UndoBranchDelete,
		Head:    head.SHA,
		To:      to,
		ToRef:   to,
		Branch:  name,
		Subject: subject,
		Command: CommandLine(CreateBranchArgs(name, to)),
	}, nil
}

// checkBranchName guards the one name in this file that reaches git as part of
// a concatenated ref path rather than as its own argument — "refs/heads/"+name
// on the way to rev-parse. checkRevision refuses the same three things for the
// same reason; the wrapper exists so the refusal says "branch".
func checkBranchName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoBranchName
	}
	if err := checkRevision(name); err != nil {
		return fmt.Errorf("%w: %w", ErrNoBranchName, err)
	}
	return nil
}

func (r *Runner) previewUndoCommit(entries []ReflogEntry, into, head string) (UndoPreview, error) {
	kind, subject, err := classifyCommitUndo(entries[0].Subject)
	if err != nil {
		return UndoPreview{}, err
	}
	if len(entries) < 2 {
		return UndoPreview{}, ErrNothingToUndo
	}
	to := entries[1].SHA
	return UndoPreview{
		Kind:    kind,
		Into:    into,
		Head:    head,
		To:      to,
		ToRef:   to,
		Subject: subject,
		Command: CommandLine(ResetArgs(to, ResetSoft)),
	}, nil
}

// previewUndoReset reverses `reset: moving to …` by putting the ref back where
// the entry beneath it says it was.
//
// The reverse is a SOFT reset whichever mode the original used, and the mode
// the original used is not knowable: git writes "reset: moving to <what was
// typed>" and records nothing about which of the three trees moved. Guessing
// is what makes that dangerous — mirroring a `--hard` would overwrite whatever
// the work tree holds NOW, which on an undo pressed some minutes late is the
// user's own work. Soft moves the ref and touches nothing else, so an undo can
// never take away more than the reset did. What it therefore cannot do — bring
// back the files a `--hard` discarded — is what the confirmation says out loud.
//
// Undoing an undo falls out of this for free: the soft reset above writes its
// own "reset: moving to <sha>" entry, so the commit undone a moment ago is one
// click from coming back, with the index round-tripping exactly.
func (r *Runner) previewUndoReset(
	ctx context.Context, dir string, entries []ReflogEntry, into, head string,
) (UndoPreview, error) {
	if len(entries) < 2 {
		return UndoPreview{}, ErrNothingToUndo
	}
	to := entries[1].SHA

	// A reset that moved no ref is not something to offer undoing, and two
	// ordinary operations leave one at the tip. `git stash push` resets to
	// HEAD on its way past, so the entry after stashing says "reset" while
	// naming an operation this would reverse wrongly; and a discard typed as
	// `git reset --hard HEAD` leaves the same line having destroyed work no
	// ref move can bring back (0031 keeps discard out of undo). Both are
	// caught by the one fact they share: the reverse target is where HEAD
	// already is, so the command would be a no-op with a promise on it.
	if to == head {
		return UndoPreview{}, ErrNothingToUndo
	}

	_, _, subject, err := r.resolveCommit(ctx, dir, to)
	if err != nil {
		return UndoPreview{}, err
	}

	return UndoPreview{
		Kind:    UndoReset,
		Into:    into,
		Head:    head,
		To:      to,
		ToRef:   to,
		Subject: subject,
		Command: CommandLine(ResetArgs(to, ResetSoft)),
	}, nil
}

func (r *Runner) previewUndoCheckout(
	ctx context.Context, dir string, tip ReflogEntry, into, head string,
) (UndoPreview, error) {
	from, _, ok := parseCheckoutMove(tip.Subject)
	if !ok || from == "" {
		return UndoPreview{}, ErrNothingToUndo
	}
	// The reflog message is text on disk, not something rev-parse produced,
	// and both lookups below concatenate it into an argument.
	if err := checkRevision(from); err != nil {
		return UndoPreview{}, err
	}

	// Prefer a local branch of that name: undoing "left side" means switch
	// back onto side, not detach at whatever commit side happened to hold.
	branchSHA, branchOK := r.revParse(ctx, dir, "refs/heads/"+from)
	if branchOK {
		return UndoPreview{
			Kind:    UndoCheckout,
			Into:    into,
			Head:    head,
			To:      branchSHA,
			ToRef:   from,
			Detach:  false,
			Subject: from,
			Command: CommandLine(SwitchArgs(from)),
		}, nil
	}

	// Otherwise from is a commit, a tag, or a remote-tracking name — detach.
	to, ok := r.revParse(ctx, dir, from+"^{commit}")
	if !ok {
		return UndoPreview{}, fmt.Errorf("%w: %q no longer resolves", ErrNothingToUndo, from)
	}
	return UndoPreview{
		Kind:    UndoCheckout,
		Into:    into,
		Head:    head,
		To:      to,
		ToRef:   from,
		Detach:  true,
		Subject: shortForUndo(from),
		Command: CommandLine(DetachArgs(from)),
	}, nil
}

func (r *Runner) revParse(ctx context.Context, dir, name string) (string, bool) {
	output, err := r.Run(ctx, dir, "rev-parse", "--verify", name)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

func parseCheckoutMove(subject string) (from, to string, ok bool) {
	const prefix = "checkout: moving from "
	if !strings.HasPrefix(subject, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(subject, prefix)
	// git writes exactly one " to " between the two sides.
	at := strings.LastIndex(rest, " to ")
	if at < 0 {
		return "", "", false
	}
	return rest[:at], rest[at+len(" to "):], true
}

func classifyCommitUndo(reflogSubject string) (UndoKind, string, error) {
	switch {
	case strings.HasPrefix(reflogSubject, "commit (amend): "):
		return UndoAmend, strings.TrimPrefix(reflogSubject, "commit (amend): "), nil
	case strings.HasPrefix(reflogSubject, "commit (initial): "):
		return "", "", ErrCannotUndoRoot
	case strings.HasPrefix(reflogSubject, "commit: "):
		return UndoCommit, strings.TrimPrefix(reflogSubject, "commit: "), nil
	default:
		return "", "", ErrNothingToUndo
	}
}

func shortForUndo(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}
