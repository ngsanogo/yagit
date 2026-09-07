package git

import (
	"context"
	"errors"
	"strings"
)

// Whether the next commit will be signed.
//
// Read, never written. `commit.gpgsign`, `user.signingkey` and `gpg.format`
// are the user's configuration, and a client that set them would be a client
// deciding whose key a commit carries — which is the one thing about a
// signature that has to stay the person's own decision (ADR 0021 is the same
// rule for a command: what the interface shows, configuration does not
// change).
//
// What is worth saying is that it is about to happen. Signing is the operation
// that fails after everything else succeeded: a locked key, an agent that is
// not running, a smartcard nobody touched. A commit box that said nothing
// about it turns that into "the button did not work", and the fix is somewhere
// the interface never mentioned.

// WillSignCommits reports whether `commit.gpgsign` is on for this repository.
//
// `--type=bool` so git normalises its own spellings — true, yes, on, 1 — into
// one word, rather than this parsing a boolean git already knows how to read.
//
// The key being unset is the ordinary case and not a failure: `git config
// --get` says so with exit code 1 and an empty stderr, which is the one shape
// read as "off" here. Every other failure travels, for the reason
// templateMessage lets its own through — a config file git cannot parse looks
// exactly like an unset key, and only one of the two is worth a person's
// attention.
func (r *Runner) WillSignCommits(ctx context.Context, dir string) (bool, error) {
	output, err := r.Run(ctx, dir, "config", "--get", "--type=bool", "commit.gpgsign")
	if err != nil {
		var failure *Error
		if errors.As(err, &failure) && failure.ExitCode == 1 && strings.TrimSpace(failure.Stderr) == "" {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(output)) == "true", nil
}
