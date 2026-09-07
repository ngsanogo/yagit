package git

import (
	"context"
	"errors"
	"strings"
)

// Tags: making, unmaking, and sending them.
//
// Create and delete are local. Push is the network half: a remote chosen and
// a refspec that names refs/tags/… on both sides — deliberately not the
// branch push path, which writes refs/heads/… and must never resolve a short
// name into a tag.
//
// Annotated is the default. Lightweight is a choice on the dialog: no message,
// no tag object, just a name pointing at a commit. Names reach git after `--`,
// for the same reason branch names do: a tag called `-d` must be refused as a
// name, not read as an option.

// ErrNoTagName: nothing was named to create, delete or push.
var ErrNoTagName = errors.New("no tag name given")

// ErrNoTagMessage: an annotated tag was asked for with nothing to say.
//
// Refused here rather than handed to git with an empty -m, which records a
// tag whose message is blank — a success that looks like the dialog was
// skipped. Lightweight tags carry no message and never reach this check.
var ErrNoTagMessage = errors.New("no tag message given")

// CreateTagArgs is the command CreateTag runs.
//
// Exported so the line the confirmation could show and the line git receives
// stay one definition. Target empty means HEAD, left to git. Annotated takes
// -a and -m; lightweight is the bare form with neither.
func CreateTagArgs(name, message, target string, annotated bool) []string {
	var args []string
	if annotated {
		args = []string{"tag", "-a", "-m", message, "--", name}
	} else {
		args = []string{"tag", "--", name}
	}
	if target != "" {
		args = append(args, target)
	}
	return args
}

// DeleteTagArgs is the command DeleteTag runs.
//
// `-d` only — local. Removing a tag from a remote is a push of a deletion and
// is not offered here.
func DeleteTagArgs(name string) []string {
	return []string{"tag", "-d", "--", name}
}

// PushTagArgs is the command PushTag runs.
//
// The refspec is written out both sides — `refs/tags/v1:refs/tags/v1` —
// rather than `git push origin v1`, which git may read as a branch. Same
// reason DestinationFor writes the full name for a branch push. No force:
// moving a published tag is a different question and is not offered here.
func PushTagArgs(remote, name string) []string {
	ref := TagRef(name)
	return []string{"push", "--", remote, ref + ":" + ref}
}

// CreateTag records a tag on target (or HEAD when target is empty).
//
// Annotated requires a message. Lightweight ignores message: git has nowhere
// to put one without -a, and refusing a non-empty message would turn a
// forgotten SegmentedControl click into a round-trip error for text nobody
// asked to keep.
func (r *Runner) CreateTag(ctx context.Context, dir, name, message, target string, annotated bool) error {
	name = strings.TrimSpace(name)
	message = strings.TrimSpace(message)
	target = strings.TrimSpace(target)
	if name == "" {
		return ErrNoTagName
	}
	if annotated && message == "" {
		return ErrNoTagMessage
	}
	// No blanking of message for a lightweight tag: CreateTagArgs builds the
	// bare form, so the text never reaches an argument. Clearing it here would
	// be a second place saying what the argument builder already says.
	_, err := r.Run(ctx, dir, CreateTagArgs(name, message, target, annotated)...)
	return err
}

// DeleteTag removes a local tag.
func (r *Runner) DeleteTag(ctx context.Context, dir, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoTagName
	}
	_, err := r.Run(ctx, dir, DeleteTagArgs(name)...)
	return err
}

// PushTag sends a local tag to a remote under the same name.
func (r *Runner) PushTag(ctx context.Context, dir, remote, name string) error {
	remote, name = strings.TrimSpace(remote), strings.TrimSpace(name)
	if remote == "" {
		return ErrNoRemote
	}
	if name == "" {
		return ErrNoTagName
	}
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    PushTagArgs(remote, name),
		Timeout: networkTimeout,
	})
	return err
}
