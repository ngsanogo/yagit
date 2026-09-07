package git

import (
	"context"
	"errors"
)

// Cloning a repository from somewhere else onto this disk.
//
// Like fetch, pull and push, this waits on another machine and carries
// networkTimeout. Unlike them, it has no open repository to run inside: the
// destination is a path under YAGIT_ROOT, checked by the registry before this
// is called. Progress rides the request — see docs/adr/0030.

// ErrEmptyCloneURL: nothing was given to clone from.
var ErrEmptyCloneURL = errors.New("no URL given to clone")

// ErrEmptyClonePath: nowhere was given to put the clone.
var ErrEmptyClonePath = errors.New("no path given to clone into")

// CloneArgs is the command Clone runs. Exported so the plan the interface
// shows and the line git receives have one definition between them.
//
// `--progress` is not optional: without it git writes nothing to stderr while
// it transfers, and the stream the interface reads would be silent for the
// whole of a ten-minute clone. The URL and the path land after `--` so a
// destination named `-v` cannot become an option.
func CloneArgs(url, path string) []string {
	return []string{"clone", "--progress", "--", url, path}
}

// ClonePlanCommand is the line shown on the confirmation, with credentials
// stripped from the URL the way Remotes strips them from a remote list.
//
// The real command still carries the URL the user typed — git needs it — and
// that string never goes back onto a screen through this helper.
func ClonePlanCommand(url, path string) string {
	return CommandLine(CloneArgs(RedactURL(url), path))
}

// Clone copies a remote repository onto path.
//
// onProgress receives each stderr segment as git writes it; nil is fine for
// callers that only care about the result. The destination's parent must
// already exist, and path itself must not — those checks belong to the
// registry, which owns the root boundary, rather than here.
func (r *Runner) Clone(ctx context.Context, url, path string, onProgress func(line string)) error {
	if url == "" {
		return ErrEmptyCloneURL
	}
	if path == "" {
		return ErrEmptyClonePath
	}

	_, err := r.Exec(ctx, Command{
		// Dir is empty on purpose: the destination is an argument, and a
		// working directory would only matter for a relative path — which the
		// registry has already refused.
		Args:        CloneArgs(url, path),
		IdleTimeout: networkIdle,
		OnProgress:  onProgress,
	})
	return err
}
