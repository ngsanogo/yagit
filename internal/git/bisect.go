package git

import (
	"context"
	"fmt"
	"strings"
)

// Starting a bisect, and the commands that drive one already in progress.
//
// Marking good/bad/skip and resetting travel through ActionArgs like every
// other unfinished operation — the banner draws those buttons from the same
// table. Starting is its own verb: nothing is in progress yet, so there is no
// Action to aim at, and the revisions it takes are not an abort/continue/skip.

// BisectStartArgs is the command that begins a search.
//
// Empty bad and goods is a bare `git bisect start`, after which the banner's
// Good/Bad buttons mark HEAD. Naming both ends up front is `git bisect start
// <bad> <good…>`, which checks out the midpoint immediately.
func BisectStartArgs(bad string, goods []string) ([]string, error) {
	bad = strings.TrimSpace(bad)
	cleaned := make([]string, 0, len(goods))
	for _, good := range goods {
		good = strings.TrimSpace(good)
		if good == "" {
			continue
		}
		if err := checkRevision(good); err != nil {
			return nil, fmt.Errorf("good revision: %w", err)
		}
		cleaned = append(cleaned, good)
	}
	if bad != "" {
		if err := checkRevision(bad); err != nil {
			return nil, fmt.Errorf("bad revision: %w", err)
		}
	}
	if bad == "" && len(cleaned) > 0 {
		return nil, fmt.Errorf("%w: a good revision needs a bad one beside it", ErrBadRevision)
	}

	args := []string{"bisect", "start"}
	if bad != "" {
		args = append(args, bad)
		args = append(args, cleaned...)
		// The end of the revisions, said: git reads what follows them as
		// paths to restrict the search to, and a revision that is also the
		// name of a file is otherwise a guess git makes on its own.
		args = append(args, "--")
	}
	return args, nil
}

// StartBisect begins a bisect, optionally with the bad and good ends named.
func (r *Runner) StartBisect(ctx context.Context, dir, bad string, goods []string) error {
	args, err := BisectStartArgs(bad, goods)
	if err != nil {
		return err
	}
	_, err = r.Exec(ctx, Command{
		Dir:     dir,
		Args:    args,
		Timeout: rewriteTimeout,
	})
	return err
}
