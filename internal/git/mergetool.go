package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrNoMergeTool: merge.tool is not set for this repository.
//
// Refused rather than run. Without it `git mergetool` picks from a list of
// tools it knows, and on a machine where the first one it finds is a terminal
// editor that tool opens on the daemon's closed stdin and the request waits
// out the whole rewrite deadline over a window nobody can see.
var ErrNoMergeTool = errors.New(
	"git has no merge.tool configured for this repository: " +
		"name one with `git config --global merge.tool <tool>`, or resolve the file here")

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

// ConfiguredMergeTool is merge.tool as this repository reads it, or
// ErrNoMergeTool when nothing sets it.
func (r *Runner) ConfiguredMergeTool(ctx context.Context, dir string) (string, error) {
	output, err := r.Exec(ctx, Command{Dir: dir, Args: []string{"config", "--get", "merge.tool"}})
	if err != nil {
		// `git config --get` exits 1 with nothing on stderr for an unset key.
		var failure *Error
		if errors.As(err, &failure) && failure.ExitCode == 1 && strings.TrimSpace(failure.Stderr) == "" {
			return "", ErrNoMergeTool
		}
		return "", err
	}
	tool := strings.TrimSpace(string(output))
	if tool == "" {
		return "", ErrNoMergeTool
	}
	return tool, nil
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
	if _, err := r.ConfiguredMergeTool(ctx, dir); err != nil {
		return err
	}
	_, err := r.Exec(ctx, Command{
		Dir:     dir,
		Args:    MergeToolArgs(path),
		Timeout: rewriteTimeout,
	})
	return err
}
