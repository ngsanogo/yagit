package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// File history: the commits that touched one path, following renames.
//
// A walk of its own rather than a filter on LogScope. The full history is
// every commit under a scope; this is the commits that changed a file, which
// is a different question and a different command — `git log --follow` —
// and combining them would either drop --follow or invent a scope that does
// not exist.
//
// --follow only works for a single path. That is git's rule, and it is why
// this takes one path rather than a list: a request for two files' histories
// is two requests, not one with two pathspecs that silently drop --follow.

// FileHistoryLimit is how many commits one file-history answer carries.
//
// Smaller than the graph page on purpose: a panel listing every change to one
// file is read top to bottom, and two hundred rows of it bury the ones that
// matter. Raising it is a product decision; the constant is the one place.
const FileHistoryLimit = 100

// LogPath returns the commits that touched path, walking back from revision
// (or HEAD when revision is empty), following renames.
func (r *Runner) LogPath(ctx context.Context, dir, revision, path string) ([]Commit, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrNoPaths
	}

	revision = strings.TrimSpace(revision)
	if revision == "" {
		revision = "HEAD"
	}
	if err := checkRevision(revision); err != nil {
		return nil, err
	}

	args := []string{
		"log", revision,
		"--follow",
		"--topo-order",
		"--decorate=short",
		"--max-count=" + strconv.Itoa(FileHistoryLimit),
		"--pretty=format:" + logFormat,
		"--",
	}
	args = append(args, literalPathspecs([]string{path})...)

	output, err := r.Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	commits, err := ParseLog(output)
	if err != nil {
		return nil, fmt.Errorf("file history of %s: %w", path, err)
	}
	return commits, nil
}
