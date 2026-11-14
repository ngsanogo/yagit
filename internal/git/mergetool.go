package git

import (
	"context"
	"fmt"
)

// MergeToolArgs is the command that hands one conflicted path to the user's
// configured merge tool.
//
// `-y` is load-bearing: without it git prompts on the daemon's closed stdin
// before launching the tool and again after it exits, and both prompts hang
// until the rewrite deadline. The path goes through literalPathspecs for the
// same reason every other path does — a file named `app/[id].tsx` is a
// wildmatch pattern after `--`, not a name.
func MergeToolArgs(path string) []string {
	return append([]string{"mergetool", "-y", "--"}, literalPathspecs([]string{path})...)
}

// MergeTool runs the configured mergetool over one conflicted path.
//
// rewriteTimeout rather than the thirty-second default: the tool is interactive
// and the person in front of it is the length of the command. What the wrong
// deadline costs is the same as for a rebase — SIGKILL mid-write, a half-saved
// file, and a repository that still calls the path unmerged.
func (r *Runner) MergeTool(ctx context.Context, dir, path string) error {
	if path == "" {
		return fmt.Errorf("%w: mergetool needs a path", ErrNoPaths)
	}
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    MergeToolArgs(path),
		Timeout: rewriteTimeout,
	})
	return err
}
