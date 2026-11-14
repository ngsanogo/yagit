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

// ErrReflogEntryGone: no entry just read has both that selector and that SHA.
//
// The list on screen and the restore plan disagree whenever HEAD has moved
// since the list was drawn: every movement renumbers the selectors, so
// HEAD@{3} on screen is HEAD@{4} by the time the click arrives, and the
// selector alone would name a different moment from the one clicked. Refused
// rather than run: a restore of the wrong moment is worse than a refusal that
// asks the list to be read again.
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

	// Dropping and DirtyFiles are ResetPreview's two counts, for the same
	// confirmation sentence: the commits on HEAD the entry does not hold,
	// and the tracked paths a hard restore puts back.
	Dropping   int `json:"dropping"`
	DirtyFiles int `json:"dirty_files"`

	// OverwrittenUntracked is how many untracked files the entry tracks,
	// counted for a hard restore only. `git reset --hard` writes those over
	// without a word, and DirtyFiles leaves untracked files out because a
	// reset to an ancestor almost never meets one; an entry ahead of HEAD,
	// which only a reflog restore reaches, is where it does.
	OverwrittenUntracked int `json:"overwritten_untracked"`
}

// PreviewReflogRestore says what restoring HEAD to the named entry would run.
//
// selector is git's own name ("HEAD@{3}") and sha the object the client saw
// at it; both must match one entry of a fresh HeadReflog walk, so a list drawn
// before HEAD last moved cannot name a moment other than the one clicked.
func (r *Runner) PreviewReflogRestore(
	ctx context.Context, dir, selector, sha string, mode ResetMode, max int,
) (ReflogRestorePreview, error) {
	selector, sha = strings.TrimSpace(selector), strings.TrimSpace(sha)
	if selector == "" || sha == "" {
		return ReflogRestorePreview{}, fmt.Errorf("%w: a reflog entry is named by its selector and its object", ErrNoRef)
	}
	if _, err := ParseResetMode(string(mode)); err != nil {
		return ReflogRestorePreview{}, err
	}

	entries, err := r.HeadReflog(ctx, dir, max)
	if err != nil {
		return ReflogRestorePreview{}, err
	}
	entry, ok := findReflogEntry(entries, selector, sha)
	if !ok {
		return ReflogRestorePreview{}, fmt.Errorf(
			"%w: no entry among the %d newest is %s at %s — read the reflog again",
			ErrReflogEntryGone, len(entries), selector, ShortSHA(sha))
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

	dropping, err := r.countCommits(ctx, dir, entry.SHA+".."+head.SHA)
	if err != nil {
		return ReflogRestorePreview{}, err
	}
	status, err := r.Status(ctx, dir)
	if err != nil {
		return ReflogRestorePreview{}, err
	}
	overwritten := 0
	if mode == ResetHard {
		overwritten, err = r.untrackedTrackedAt(ctx, dir, entry.SHA, status)
		if err != nil {
			return ReflogRestorePreview{}, err
		}
	}

	return ReflogRestorePreview{
		Selector: entry.Selector,
		SHA:      entry.SHA,
		Subject:  entry.Subject,
		Head:     head.SHA,
		Into:     into,
		Mode:     mode,
		Command:  CommandLine(ResetArgs(entry.SHA, mode)),

		Dropping:             dropping,
		DirtyFiles:           status.DirtyTracked(),
		OverwrittenUntracked: overwritten,
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

func findReflogEntry(entries []ReflogEntry, selector, sha string) (ReflogEntry, bool) {
	for _, entry := range entries {
		if entry.Selector == selector && entry.SHA == sha {
			return entry, true
		}
	}
	return ReflogEntry{}, false
}

// untrackedTrackedAt counts the work tree's untracked files that the commit
// tracks: the ones `git reset --hard` to it would write over.
//
// One `cat-file --batch-check` for all of them, asking for the type of
// <commit>:<path> line by line; a path the commit lacks answers "missing".
// A path holding a newline cannot be asked on that line-based input, and is
// left out rather than misread as two.
func (r *Runner) untrackedTrackedAt(ctx context.Context, dir, commit string, status Status) (int, error) {
	var questions strings.Builder
	for _, file := range status.Files {
		if file.Kind != EntryUntracked || strings.Contains(file.Path, "\n") {
			continue
		}
		questions.WriteString(commit + ":" + file.Path + "\n")
	}
	if questions.Len() == 0 {
		return 0, nil
	}

	output, err := r.Exec(ctx, Command{
		Dir:   dir,
		Args:  []string{"cat-file", "--batch-check=%(objecttype)"},
		Stdin: []byte(questions.String()),
	})
	if err != nil {
		return 0, err
	}
	tracked := 0
	for _, answer := range strings.Split(string(output), "\n") {
		if answer == "blob" {
			tracked++
		}
	}
	return tracked, nil
}
