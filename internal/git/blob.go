package git

import (
	"context"
	"fmt"
	"strings"
)

// MaxBlobBytes caps a single blob this package will hold for a preview.
//
// Five mebibytes is large enough for a typical screenshot and small enough
// that two sides of an image diff stay well under the daemon's ordinary
// memory budget. Past it the answer is an external viewer, not a tab that
// stops responding.
const MaxBlobBytes = 5 << 20

// ShowBlob reads one path as it stands at a named place in the repository.
//
// at is "index" (the staging area), "head" (HEAD), or any revision
// checkRevision accepts — a commit SHA for a commit panel, a stash object
// for a stash panel. The path is the name that place uses for the file.
//
// Served as raw bytes rather than base64: an <img src> against a same-origin
// route is how the interface draws a preview, and encoding every pixel into
// JSON would only inflate the answer.
func (r *Runner) ShowBlob(ctx context.Context, dir, at, path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrNoPaths
	}

	object, err := blobObject(at, path)
	if err != nil {
		return nil, err
	}

	// MaxOutput is the same ceiling MaxBlobBytes names: a gigabyte PNG is
	// never held, and the Cause is ErrDiffTooLarge so the route answers 413
	// the same way an oversized unified diff does.
	//
	// No `--` before the object: it is a tree-ish:path name, not a pathspec.
	// Putting it after `--` would ask show for a patch of that path instead
	// of the blob. checkRevision (and the `:0:` prefix for the index) keep
	// it from being read as an option.
	return r.Exec(ctx, Command{
		Dir:       dir,
		Args:      []string{"show", "--no-textconv", object},
		MaxOutput: MaxBlobBytes,
	})
}

// blobObject builds the tree-ish:path (or :0:path) argument git show takes.
//
// One argument rather than `show -- path`: the path is part of the object
// name, and splitting it would ask git for a commit's patch instead of the
// blob. checkRevision covers every revision form the interface sends.
func blobObject(at, path string) (string, error) {
	switch strings.ToLower(at) {
	case "index":
		return ":0:" + path, nil
	case "head":
		if err := checkRevision("HEAD"); err != nil {
			return "", err
		}
		return "HEAD:" + path, nil
	case "":
		return "", fmt.Errorf("%w: none was given", ErrBadRevision)
	case "worktree":
		// The work tree is not an object name. The route reads the file from
		// disk; reaching here means a caller mixed the two halves.
		return "", fmt.Errorf("%w: %q is the work tree, not a git object", ErrBadRevision, at)
	default:
		if err := checkRevision(at); err != nil {
			return "", err
		}
		return at + ":" + path, nil
	}
}
