package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The message git starts a commit from.
//
// `git commit` never opens an empty editor. A merge that stopped on a conflict
// leaves MERGE_MSG behind — "Merge branch 'side'", with the conflicted files
// below it as comments — and `--amend` starts from the message of the commit
// being replaced. Both are cases where the user has already said what the
// commit means, once, and should not be asked to say it again from memory.
//
// One function answers both because git answers both the same way, in one
// chain, in builtin/commit.c: amend first, then the prepared files. Splitting
// it into two routes would be two places to get that order wrong.
//
// Nothing here is a suggestion, and the distinction is the whole reason this
// exists separately from what the interface proposes on its own. These are
// git's words. The interface puts them IN the box, the way an editor would;
// what yagit makes up from the file list stays a placeholder until somebody
// accepts it.

// MessageSource says which file a prepared message came from, so the
// interface can say so rather than presenting git's words as its own.
type MessageSource string

const (
	// SourceNone: git has prepared nothing, which is the ordinary case.
	SourceNone MessageSource = ""

	// SourceMerge is MERGE_MSG — written by a merge, and also by a
	// cherry-pick or a revert that stopped on a conflict.
	SourceMerge MessageSource = "merge"

	// SourceSquash is SQUASH_MSG — written by `merge --squash` and by a
	// rebase's squash, and holding every squashed commit's message.
	SourceSquash MessageSource = "squash"

	// SourceHead is the message of the commit an amend would replace.
	SourceHead MessageSource = "head"

	// SourceTemplate is the file `commit.template` names — the user's own
	// skeleton for a message, not something any operation wrote.
	SourceTemplate MessageSource = "template"
)

// PreparedMessage is the message git would open an editor on, or the empty
// string when it would open an empty one.
//
// The order is git's own, from builtin/commit.c. An amend starts from the
// commit being replaced and stops there — it does not fall through to
// MERGE_MSG, because a merge in progress has not been committed and there is
// nothing about it to amend. Otherwise MERGE_MSG comes first and SQUASH_MSG
// second; both can exist at once — a squashing rebase that hits a conflict
// writes both — and git commits the first. `commit.template` is last, for the
// reason templateMessage gives.
//
// dir is where git runs, stateDir is the work tree's own git directory
// (repo.Repo.StateDir). Two paths rather than one because they are genuinely
// two places in a linked worktree, and a message read from the wrong one
// belongs to somebody else's merge.
func (r *Runner) PreparedMessage(ctx context.Context, dir, stateDir string, amend bool) (string, MessageSource, error) {
	if amend {
		text, err := r.HeadMessage(ctx, dir)
		if err != nil {
			return "", SourceNone, err
		}
		return text, SourceHead, nil
	}

	for _, candidate := range []struct {
		name   string
		source MessageSource
	}{
		{"MERGE_MSG", SourceMerge},
		{"SQUASH_MSG", SourceSquash},
	} {
		raw, err := os.ReadFile(filepath.Join(stateDir, candidate.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", SourceNone, fmt.Errorf("reading %s: %w", candidate.name, err)
		}

		text, err := r.StripComments(ctx, dir, raw)
		if err != nil {
			return "", SourceNone, err
		}
		if text == "" {
			// A prepared message that is nothing but comments is not a
			// message. Falling through to the next candidate beats handing
			// the interface an empty string it would have to test for.
			continue
		}
		return text, candidate.source, nil
	}

	// Last, because that is where git puts it: builtin/commit.c reaches the
	// template only when no operation has prepared a message. A merge in
	// progress has already said what the commit means, and a skeleton offered
	// over the top of it would be a form to fill in about work already done.
	return r.templateMessage(ctx, dir)
}

// templateMessage is the message `commit.template` names, or nothing.
//
// The key being unset is the ordinary case and not a failure: `git config
// --get` says so with exit code 1 and an empty stderr, which is the one shape
// read as "no template" here. Every other failure travels — a config file git
// cannot parse is something the user has to know about, and it would otherwise
// look exactly like having configured no template.
//
// A configured file that cannot be read is a failure too, deliberately. It is
// what `git commit` itself does — "fatal: could not read
// '~/.gitmessage'" — and a commit box that silently opened empty would leave
// somebody wondering where their template went for as long as the typo lived.
func (r *Runner) templateMessage(ctx context.Context, dir string) (string, MessageSource, error) {
	output, err := r.Run(ctx, dir, "config", "--get", "commit.template")
	if err != nil {
		var failure *Error
		if errors.As(err, &failure) && failure.ExitCode == 1 && strings.TrimSpace(failure.Stderr) == "" {
			return "", SourceNone, nil
		}
		return "", SourceNone, err
	}

	configured := strings.TrimSpace(string(output))
	if configured == "" {
		return "", SourceNone, nil
	}

	raw, err := os.ReadFile(templatePath(dir, configured))
	if err != nil {
		return "", SourceNone, fmt.Errorf("reading the commit template %q: %w", configured, err)
	}

	text, err := r.StripComments(ctx, dir, raw)
	if err != nil {
		return "", SourceNone, err
	}
	if text == "" {
		// A template that is nothing but comments is a template that proposes
		// nothing — the `#`-only skeletons people write to remind themselves
		// of a convention. Answering "" with no source keeps the interface
		// from labelling an empty box as git's words.
		return "", SourceNone, nil
	}
	return text, SourceTemplate, nil
}

// templatePath resolves what `commit.template` holds against the repository.
//
// Three spellings, because git accepts three: absolute, `~/`-relative, and
// relative to the work tree. The tilde is the one worth the code — the
// documented example is `~/.gitmessage.txt` and git expands it itself, as a
// config value of type path — and a daemon that read that literally would look
// for a directory called `~` beside the repository.
func templatePath(dir, configured string) string {
	if filepath.IsAbs(configured) {
		return configured
	}
	if configured == "~" || strings.HasPrefix(configured, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(configured, "~"))
		}
		// No home to expand against: left as it is, so the read below fails
		// naming the path the user configured rather than one invented here.
		return configured
	}
	return filepath.Join(dir, configured)
}

// HeadMessage is the whole message of the commit HEAD points at, subject and
// body.
//
// What `git commit --amend` starts from. `%B` is the raw body — not `%s` and
// `%b` joined, which would lose the blank line between them on a message whose
// body starts with one, and would drop a subject that wraps.
//
// A branch with no commit yet has no HEAD, and git says so in a sentence that
// names the branch. That reaches the interface whole rather than being turned
// into an empty message here: an amend with nothing to amend is a question
// with no answer, not an answer of "".
func (r *Runner) HeadMessage(ctx context.Context, dir string) (string, error) {
	output, err := r.Run(ctx, dir, "log", "--max-count=1", "--format=%B", "HEAD")
	if err != nil {
		return "", err
	}
	// git ends the format with a newline of its own, on top of whatever the
	// message ended with. Trimming the trailing blank lines is what
	// --cleanup=whitespace would do to it on the way back in anyway, so the
	// box shows what would be committed rather than two blank lines nobody
	// typed.
	return strings.TrimRight(string(output), "\n"), nil
}

// StripComments removes a message's comment lines the way git does.
//
// It has to be git that does it, not a loop over the lines here, and the
// reason is one character: `core.commentChar`. A user who sets it to `;`
// — which people with a shell-script habit do — has MERGE_MSG commented with
// semicolons, and every '#' in it is ordinary text. A stripper that assumed
// '#' would delete their words and keep git's, silently, in the one box where
// the user cannot see what was removed.
//
// `git stripspace --strip-comments` reads the user's configuration and is the
// command git itself documents for this. It also collapses runs of blank lines
// and trims the ends, which is what makes the result a message rather than a
// file.
func (r *Runner) StripComments(ctx context.Context, dir string, message []byte) (string, error) {
	output, err := r.Exec(ctx, Command{
		Dir:   dir,
		Args:  []string{"stripspace", "--strip-comments"},
		Stdin: message,
	})
	if err != nil {
		return "", err
	}
	// stripspace ends its output with a newline, as a file should. This is not
	// going into a file: it is going into a text box, where that newline is a
	// blank line under the message with a cursor sitting on it. Trimmed here
	// rather than in the box, because --cleanup=whitespace would trim it again
	// on the way back in — so what is shown is what would be committed.
	return strings.TrimRight(string(output), "\n"), nil
}
