package git

import (
	"context"
	"fmt"
	"strings"
)

// HEAD is where the repository's checked-out commit sits.
type HEAD struct {
	// SHA is the commit HEAD points at. Empty when the repository has no
	// commit yet.
	SHA string

	// Name is the short branch name, or "HEAD" when detached, or empty when
	// there is no commit yet.
	Name string

	Detached bool
}

// ReadHEAD returns the commit HEAD points at, the name it is known by, and
// whether it is detached. An empty repository has no HEAD yet and returns
// zero values without error.
func (r *Runner) ReadHEAD(ctx context.Context, dir string) (HEAD, error) {
	// One command for both fields, and --revs-only is what makes it one.
	//
	// It earns its place twice over. HEAD appears twice in the arguments —
	// once raw for the commit, once under --abbrev-ref for the name — so the
	// answer is a single subprocess called once per refresh, beside
	// for-each-ref. And a repository with no commit yet, the state right after
	// git init, is answered with no output and exit 0 rather than with git's
	// "ambiguous argument 'HEAD'" fatal and exit 128. That matters because the
	// Runner hands every execution to its observer and the observer feeds the
	// log panel: the bare form would put a fatal in front of the user on every
	// refresh of a repository where nothing is wrong.
	output, err := r.Run(ctx, dir, "rev-parse", "--revs-only", "HEAD", "--abbrev-ref", "HEAD")
	if err != nil {
		return HEAD{}, err
	}

	// Nothing resolved: there is no commit to point at. --revs-only drops a
	// name it cannot resolve instead of complaining about it, and HEAD is the
	// only name asked for here. A directory that is not a repository at all
	// still fails above, with exit 128, rather than passing for an empty one.
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return HEAD{}, nil
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) != 2 {
		return HEAD{}, fmt.Errorf(
			"git rev-parse answered %d lines about HEAD, 2 expected, in %q", len(lines), dir)
	}

	name := lines[1]
	return HEAD{
		SHA:      lines[0],
		Name:     name,
		Detached: name == "HEAD",
	}, nil
}
