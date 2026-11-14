package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Restoring HEAD to a past reflog entry.
//
// Distinct from PreviewReset: that one refuses a commit the branch never held,
// because the confirmation there says "take this branch back along its own
// history". A reflog restore is the opposite promise — HEAD was once here, and
// the user is choosing that moment again, even when the current tip has since
// moved onto a different line. The lease is the object names, not ancestry.

// ErrReflogEntryGone: the selector or SHA is not among the entries just read.
//
// The list on screen and the restore plan can disagree when a new movement of
// HEAD pushes the entry past the window, or when the client invents a
// selector. Refused rather than run: a restore of the wrong moment is worse
// than a refusal that asks the list to be read again.
var ErrReflogEntryGone = errors.New("that reflog entry is no longer in reach")

// ReflogRestorePreview is what restoring HEAD to one entry would run.
type ReflogRestorePreview struct {
	Selector string    `json:"selector"`
	SHA      string    `json:"sha"`
	Subject  string    `json:"subject"`
	Head     string    `json:"head"`
	Into     string    `json:"into"`
	Mode     ResetMode `json:"mode"`
	Command  string    `json:"command"`
}

// PreviewReflogRestore says what restoring HEAD to the named entry would run.
//
// selector is git's own name ("HEAD@{3}") or the full object name the entry
// holds. Matched against a fresh HeadReflog walk so a stale list cannot name a
// moment that has scrolled out of the window.
func (r *Runner) PreviewReflogRestore(
	ctx context.Context, dir, selector string, mode ResetMode, max int,
) (ReflogRestorePreview, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ReflogRestorePreview{}, fmt.Errorf("%w: none was given", ErrNoRef)
	}
	if _, err := ParseResetMode(string(mode)); err != nil {
		return ReflogRestorePreview{}, err
	}

	entries, err := r.HeadReflog(ctx, dir, max)
	if err != nil {
		return ReflogRestorePreview{}, err
	}
	entry, ok := findReflogEntry(entries, selector)
	if !ok {
		return ReflogRestorePreview{}, fmt.Errorf(
			"%w: %s is not among the %d newest HEAD entries",
			ErrReflogEntryGone, selector, len(entries))
	}

	head, err := r.ReadHEAD(ctx, dir)
	if err != nil {
		return ReflogRestorePreview{}, err
	}
	if head.SHA == "" {
		return ReflogRestorePreview{}, ErrNoCommits
	}

	into := ""
	if !head.Detached {
		into = head.Name
	}

	return ReflogRestorePreview{
		Selector: entry.Selector,
		SHA:      entry.SHA,
		Subject:  entry.Subject,
		Head:     head.SHA,
		Into:     into,
		Mode:     mode,
		Command:  CommandLine(ResetArgs(entry.SHA, mode)),
	}, nil
}

// ReflogRestore carries out a preview after the caller has checked its lease.
func (r *Runner) ReflogRestore(ctx context.Context, dir string, plan ReflogRestorePreview) error {
	head := strings.TrimSpace(plan.Head)
	sha := strings.TrimSpace(plan.SHA)
	if err := checkRevision(head); err != nil {
		return err
	}
	if err := checkRevision(sha); err != nil {
		return err
	}
	if _, err := ParseResetMode(string(plan.Mode)); err != nil {
		return err
	}

	current, err := r.Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(current)) != head {
		return fmt.Errorf(
			"%w: HEAD is now %s, not %s — read the reflog again",
			ErrUndoStale, ShortSHA(strings.TrimSpace(string(current))), ShortSHA(head))
	}

	return r.Reset(ctx, dir, sha, plan.Mode)
}

func findReflogEntry(entries []ReflogEntry, needle string) (ReflogEntry, bool) {
	for _, entry := range entries {
		if entry.Selector == needle || entry.SHA == needle {
			return entry, true
		}
	}
	return ReflogEntry{}, false
}
