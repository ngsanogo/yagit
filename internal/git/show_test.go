package git_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Reading one commit whole: the message a person typed, the two identities,
// and the diff it introduced.
//
// The message is the field with no shape at all — it holds newlines, blank
// lines, and anything that looks like the format around it — so it is the
// field the framing has to survive.

func TestShowReadsACommitWhole(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	runGit(t, runner, dir, "add", "--", "a.txt")
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"commit", "-m", "second: the subject", "-m", "The body.\n\nWith a blank line in it.")...)

	sha, err := runner.HeadSHA(ctx, dir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	detail, err := runner.Show(ctx, dir, sha)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if detail.SHA != sha {
		t.Errorf("SHA = %q, expected %q", detail.SHA, sha)
	}
	if detail.Subject != "second: the subject" {
		t.Errorf("Subject = %q", detail.Subject)
	}
	// The blank line inside the body has to survive: it is the paragraph break
	// the author wrote, and the field separator is a NUL for exactly this
	// reason.
	if detail.Body != "The body.\n\nWith a blank line in it." {
		t.Errorf("Body = %q", detail.Body)
	}
	if detail.Author != "yagit Test" || detail.Committer != "yagit Test" {
		t.Errorf("author %q, committer %q", detail.Author, detail.Committer)
	}
	if detail.CommitterDate.IsZero() {
		t.Error("the committer date was not read")
	}
	if detail.AgainstFirstParent {
		t.Error("an ordinary commit's diff is the whole of what it changed")
	}

	if len(detail.Files) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(detail.Files))
	}
	if detail.Files[0].Path != "a.txt" {
		t.Errorf("changed %q", detail.Files[0].Path)
	}
	if len(detail.Files[0].Hunks) == 0 {
		t.Error("the patch did not reach the answer")
	}
}

func TestShowOfACommitWithAOneLineMessage(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	sha, err := runner.HeadSHA(ctx, dir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	detail, err := runner.Show(ctx, dir, sha)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	// Most commits are this. A body invented out of the second line would put
	// half a sentence under a heading.
	if detail.Subject != "first" {
		t.Errorf("Subject = %q", detail.Subject)
	}
	if detail.Body != "" {
		t.Errorf("Body = %q, expected empty", detail.Body)
	}
}

func TestShowOfARootCommitListsWhatItCreated(t *testing.T) {
	dir, runner := workingDirectory(t)
	ctx := context.Background()

	sha, err := runner.HeadSHA(ctx, dir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	detail, err := runner.Show(ctx, dir, sha)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if len(detail.Parents) != 0 {
		t.Errorf("a root commit has no parent, got %v", detail.Parents)
	}
	// git shows a root commit's patch as the creation of every file in it.
	// A diff against a parent that does not exist would have shown nothing.
	if len(detail.Files) != 1 || !detail.Files[0].Added {
		t.Errorf("expected one added file, got %+v", detail.Files)
	}
}

func TestShowOfAMergeSaysWhichDiffItIs(t *testing.T) {
	dir, runner := testRepository(t)
	ctx := context.Background()

	sha, err := runner.HeadSHA(ctx, dir)
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}

	detail, err := runner.Show(ctx, dir, sha)
	if err != nil {
		t.Fatalf("Show: %v", err)
	}

	if len(detail.Parents) != 2 {
		t.Fatalf("expected the merge, got %d parents", len(detail.Parents))
	}
	// A merge has one answer per parent, and git prints none of them by
	// default. Showing the first parent's is what every tool does; saying so
	// is what keeps it from being a lie.
	if !detail.AgainstFirstParent {
		t.Error("a merge's diff has to be labelled as the first parent's")
	}
}

func TestShowRefusesARevisionGitWouldReadAsAnOption(t *testing.T) {
	dir, runner := workingDirectory(t)

	// git has no `--` for revisions the way it has for paths, so a leading
	// dash makes the argument an option — and `git log --output=…` writes a
	// file. Nothing legitimate is refused here, which is the point.
	for _, revision := range []string{"", "-f", "--output=/tmp/written", "HEAD\nHEAD"} {
		if _, err := runner.Show(context.Background(), dir, revision); err == nil {
			t.Errorf("%q was accepted as a revision", revision)
		}
	}
}

func TestShowOfANameGitDoesNotKnow(t *testing.T) {
	dir, runner := workingDirectory(t)

	_, err := runner.Show(context.Background(), dir, "0000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected an error for a commit that does not exist")
	}
	// git's own words reach the caller, whole: the command, the exit code and
	// the raw stderr. Never "Something went wrong".
	var gitError *git.Error
	if !errors.As(err, &gitError) {
		t.Fatalf("err = %v, expected a git failure carrying what git said", err)
	}
	if !strings.HasPrefix(gitError.CommandLine(), "git log") {
		t.Errorf("command = %q", gitError.CommandLine())
	}
	if gitError.Stderr == "" {
		t.Error("git's own message did not travel with the error")
	}
}
