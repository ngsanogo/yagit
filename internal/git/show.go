package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Detail is everything about one commit: what it says, who wrote it, and what
// it changed.
type Detail struct {
	Commit

	// Body is the message below the subject, with the blank line between them
	// removed. Empty for a commit whose message is one line, which most are.
	Body string `json:"body"`

	// Committer is the second identity every commit carries. It differs from
	// the author after a rebase, a cherry-pick, or a patch applied by
	// somebody else — which is exactly when a reader needs to know.
	Committer     string    `json:"committer"`
	CommitterDate time.Time `json:"committer_date"`

	Files []FileDiff `json:"files"`

	// Signature is git's own verdict on the commit's signature — the letter
	// %G? prints. "N" for the unsigned commit most commits are, "G" for a good
	// signature, "B" for a bad one, and five more for the states between them.
	//
	// The letter travels rather than a boolean, because "signed" is not a
	// yes-or-no: a good signature from a key this machine does not trust, an
	// expired key, and a signature git had no key to check at all are three
	// different answers and none of them is a forgery. Collapsing them here
	// would be this package deciding what the interface is allowed to say.
	Signature string `json:"signature"`

	// Signer is who git says signed it, empty when nothing did. git prints the
	// name it read out of the signature, which is NOT the author line — a
	// commit signed by somebody else is exactly the case worth seeing.
	Signer string `json:"signer,omitempty"`

	// AgainstFirstParent says the diff below is a merge's diff against its
	// first parent, and not the whole of what the merge brought in.
	//
	// A merge has no single "what changed": it has one answer per parent. git
	// shows nothing at all for one by default, which is honest and useless.
	// Showing the first parent's is what every tool does; saying so is what
	// keeps it from being a lie.
	AgainstFirstParent bool `json:"against_first_parent"`
}

// detailFormat carries the fields the history list does not: the full message,
// the committer, and git's verdict on the signature. NUL-separated for the
// same reason as everywhere else — %B holds anything a person typed, newlines
// included, which is why it is last.
//
// %G? and %GS are HERE and not in logFormat, and the difference is what they
// cost. Asking for a signature verdict makes git verify the signature, which
// runs gpg once per signed commit; on the history walk — every commit in the
// repository, on every refresh — that is a subprocess per row. On one commit
// somebody clicked, it is one, and it answers the question they clicked to
// ask.
const detailFormat = "%H%x00%P%x00%an%x00%aI%x00%cn%x00%cI%x00%D%x00%G?%x00%GS%x00%B"

const detailFieldCount = 10

// Show reads one commit and the diff it introduced.
//
// Two commands rather than one. `git show` can print both, but its patch
// follows a format string the caller chose, and telling where one ends and the
// other begins means either trusting the message not to look like a diff — it
// can — or picking a separator a message could contain. Two reads have no such
// question in them, and both appear in the log panel, where a reader can see
// exactly what was asked.
func (r *Runner) Show(ctx context.Context, dir, revision string) (Detail, error) {
	// The revision goes after `--`-less options but is still checked: this is
	// a string from the network, and `--output=/etc/passwd` is a revision no
	// more than `-f` is a filename.
	if err := checkRevision(revision); err != nil {
		return Detail{}, err
	}

	output, err := r.Run(ctx, dir,
		"log", "-1", "--decorate=short", "--pretty=format:"+detailFormat, revision)
	if err != nil {
		return Detail{}, err
	}

	detail, err := parseDetail(output)
	if err != nil {
		return Detail{}, err
	}

	patch, err := r.showPatch(ctx, dir, revision, len(detail.Parents) > 1)
	if err != nil {
		return Detail{}, err
	}

	files, err := ParseDiff(patch)
	if err != nil {
		return Detail{}, err
	}

	detail.Files = files
	if detail.Files == nil {
		// A commit that changed nothing is a real thing — `git commit
		// --allow-empty`, and every merge that resolved to its first parent.
		// null in the payload would be an interface that crashes on one.
		detail.Files = []FileDiff{}
	}
	detail.AgainstFirstParent = len(detail.Parents) > 1

	return detail, nil
}

// showPatch reads the diff a commit introduced.
//
// `--format=` empties the header, leaving the patch alone. For a merge, git
// prints nothing without being asked: --diff-merges=first-parent picks the one
// comparison a reader expects, and Detail.AgainstFirstParent says that is what
// they are looking at.
//
// The format is named rather than switched on. `-m` is `--diff-merges=on`,
// which takes its shape from the user's log.diffMerges — set to `combined` it
// answers with `diff --combined` and `@@@` hunk headers, which are not a
// unified diff at all, and every hand-resolved merge commit becomes one this
// daemon cannot read.
func (r *Runner) showPatch(ctx context.Context, dir, revision string, merge bool) ([]byte, error) {
	args := []string{"show", "--format=", "--patch"}
	if merge {
		args = append(args, "--diff-merges=first-parent")
	}
	args = append(args, diffArgs...)
	args = append(args, revision)

	// Capped like every other diff read. A commit that adds a generated file
	// answers with the whole of it, and the details pane would otherwise hold
	// git's output, the parse of it and the encoded payload all at once.
	return r.Exec(ctx, Command{Dir: dir, Args: args, MaxOutput: maxDiffBytes})
}

// ErrBadRevision: the revision in a request is one git must never be given.
//
// Three readings of one fault — nothing at all, a leading dash git would take
// for an option, a byte no object name can hold — under a single sentinel,
// because the answer to all three is the same: the request is wrong, and
// reading the repository again would not change that. It is what lets a route
// tell them from a repository that refused; see statusForOperationError, which
// answers 400 for this and 409 for the state.
var ErrBadRevision = errors.New("unusable revision")

// checkRevision refuses a revision that could be read as an option.
//
// git has no `--` for revisions the way it has for paths: a leading dash makes
// the argument an option, and `git log --output=…` writes a file. The
// interface only ever sends object names it read out of a previous answer, so
// nothing legitimate is refused here — which is the point of checking.
func checkRevision(revision string) error {
	switch {
	case revision == "":
		return fmt.Errorf("%w: none was given", ErrBadRevision)
	case strings.HasPrefix(revision, "-"):
		return fmt.Errorf("%w: %q starts with a dash, which git reads as an option", ErrBadRevision, revision)
	case strings.ContainsAny(revision, "\x00\n"):
		return fmt.Errorf("%w: %q holds a byte no object name can", ErrBadRevision, revision)
	}
	return nil
}

// parseDetail reads one record of detailFormat.
//
// Pure and exported to the package's tests for the same reason as the other
// parsers: the message is the one field with no shape at all, and a separator
// it could contain would split one commit into two.
func parseDetail(output []byte) (Detail, error) {
	// %B ends with whatever the message ended with, so the record has no
	// terminator to strip — there is exactly one, and it runs to the end.
	fields := strings.SplitN(string(output), fieldSeparator, detailFieldCount)
	if len(fields) != detailFieldCount {
		return Detail{}, fmt.Errorf(
			"expected %d NUL-separated fields, got %d in %q",
			detailFieldCount, len(fields), output)
	}

	authorDate, err := time.Parse(time.RFC3339, fields[3])
	if err != nil {
		return Detail{}, fmt.Errorf("unreadable author date %q: %w", fields[3], err)
	}
	committerDate, err := time.Parse(time.RFC3339, fields[5])
	if err != nil {
		return Detail{}, fmt.Errorf("unreadable committer date %q: %w", fields[5], err)
	}

	subject, body := splitMessage(fields[9])

	return Detail{
		Commit: Commit{
			SHA:     fields[0],
			Parents: strings.Fields(fields[1]),
			Author:  fields[2],
			Date:    authorDate,
			Subject: subject,
			Refs:    parseRefDecoration(fields[6]),
		},
		Body:          body,
		Committer:     fields[4],
		CommitterDate: committerDate,
		Signature:     fields[7],
		Signer:        fields[8],
	}, nil
}

// splitMessage cuts a commit message into its subject and its body, at the
// blank line git's own convention puts between them.
//
// A message with no blank line is all subject, however long — that is what git
// does, and inventing a body out of the second line would put half a sentence
// under a heading.
func splitMessage(message string) (subject, body string) {
	message = strings.TrimRight(message, "\n")

	subject, body, found := strings.Cut(message, "\n\n")
	if !found {
		return message, ""
	}
	return subject, strings.TrimLeft(body, "\n")
}
