package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The HEAD reflog: every movement of HEAD, newest first.
//
// Undo reads this rather than inventing a second journal
// (docs/adr/0031-undo-reads-the-head-reflog.md). Positions (HEAD@{n}) are
// display and parse input only; anything that runs leases on the object names
// the entries hold.

// ReflogEntry is one line of a reflog walk.
type ReflogEntry struct {
	// SHA is where HEAD pointed after this entry's action.
	SHA string `json:"sha"`

	// Selector is git's own name for the entry — "HEAD@{0}".
	Selector string `json:"selector"`

	// Subject is the reflog message — "commit: fix the parser".
	Subject string `json:"subject"`

	Date time.Time `json:"date"`
}

// reflogFormat matches stashListFormat's framing so splitRecords applies.
const reflogFormat = "%H%x00%gd%x00%gs%x00%aI%x00%x0a"

const reflogFieldCount = 4

// HeadReflog reads the newest max entries of HEAD's reflog.
//
// max ≤ 0 means a small default: enough for undo to see the tip and its
// predecessor, with a few spare for classification that needs more context
// later. An empty reflog (no commits yet) is not an error — no entries.
//
// An unborn HEAD is answered the same way. `git reflog show HEAD` exits 128
// with "ambiguous argument 'HEAD'" there — the fatal ReadHEAD carefully avoids
// putting on every refresh. Without this guard, Undo on a just-init'd
// repository became a 422 naming a command that was never going to succeed,
// and the log panel showed a failure for a state that is not wrong.
func (r *Runner) HeadReflog(ctx context.Context, dir string, max int) ([]ReflogEntry, error) {
	if max <= 0 {
		max = 20
	}
	head, err := r.ReadHEAD(ctx, dir)
	if err != nil {
		return nil, err
	}
	if head.SHA == "" {
		return nil, nil
	}
	output, err := r.Run(ctx, dir,
		"reflog", "show", "HEAD",
		"--max-count="+strconv.Itoa(max),
		"--pretty=format:"+reflogFormat,
	)
	if err != nil {
		return nil, err
	}
	return ParseReflog(output)
}

// ParseReflog turns reflogFormat output into entries.
//
// Pure and exported for the reason ParseStashList is: a wrong split is a
// plausible wrong undo target rather than a crash.
func ParseReflog(output []byte) ([]ReflogEntry, error) {
	records := splitRecords(output)
	entries := make([]ReflogEntry, 0, len(records))
	for _, record := range records {
		fields := strings.Split(record, fieldSeparator)
		if len(fields) != reflogFieldCount {
			return nil, fmt.Errorf(
				"reflog: expected %d NUL-separated fields, got %d in %q",
				reflogFieldCount, len(fields), record)
		}
		when, err := time.Parse(time.RFC3339, fields[3])
		if err != nil {
			return nil, fmt.Errorf("reflog date %q: %w", fields[3], err)
		}
		entries = append(entries, ReflogEntry{
			SHA:      fields[0],
			Selector: fields[1],
			Subject:  fields[2],
			Date:     when,
		})
	}
	return entries, nil
}
