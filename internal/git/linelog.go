package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Line history: the commits that changed one line of one path.
//
// `git log -L` rather than a filter on LogPath. File history answers "which
// commits touched this path"; this answers "which commits changed this line",
// and the commands are not interchangeable — -L walks the line across renames
// and edits, and --follow is refused beside it.
//
// The range is written into the -L argument (`-L2,2:notes.md`). There is no
// `--` separator for the path, so a colon or a newline in the path would be
// read as part of the range syntax; LogLine refuses those before git sees them.

// ErrBadLineRange is a start or end that cannot be a line number.
var ErrBadLineRange = errors.New("line range is not usable")

// LogLine returns the commits that changed lines start through end of path,
// walking back from revision (or HEAD when revision is empty).
//
// start and end are 1-based and inclusive, matching what editors and blame
// show. A single line is start == end.
func (r *Runner) LogLine(ctx context.Context, dir, revision, path string, start, end int) ([]Commit, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrNoPaths
	}
	if strings.ContainsAny(path, ":\n\x00") {
		// -L takes `start,end:path` as one argument. A colon splits the range
		// from the path; a newline would be two arguments to git.
		return nil, fmt.Errorf("%w: %q holds a character -L cannot carry in a path", ErrNoPaths, path)
	}
	if start < 1 || end < 1 || start > end {
		return nil, fmt.Errorf("%w: want 1 ≤ start ≤ end, got %d–%d", ErrBadLineRange, start, end)
	}

	revision = strings.TrimSpace(revision)
	if revision == "" {
		revision = "HEAD"
	}
	if err := checkRevision(revision); err != nil {
		return nil, err
	}

	// -s (--no-patch): -L otherwise prints every hunk between commits, which
	// ParseLog cannot read and which the panel does not show — the commit
	// panel already has the patch.
	//
	// --decorate=short for the reason LogScope pins it: %D answers
	// "refs/heads/main" under a user's log.decorate=full, and a badge whose
	// shape depends on whose machine the daemon runs on is not a badge.
	args := []string{
		"log", revision,
		fmt.Sprintf("-L%d,%d:%s", start, end, path),
		"-s",
		"--decorate=short",
		"--max-count=" + strconv.Itoa(FileHistoryLimit),
		"--pretty=format:" + logFormat,
	}

	output, err := r.Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	commits, err := ParseLog(output)
	if err != nil {
		return nil, fmt.Errorf("line history of %s:%d–%d: %w", path, start, end, err)
	}
	return commits, nil
}
