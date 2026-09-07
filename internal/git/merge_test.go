package git_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The argument list, without git: the separator, the name git cannot misread,
// and the flag that has to be there whatever else is.

func TestMergeArgsNamesTheBranchWhereNothingElseCanAnswerTo(t *testing.T) {
	for _, outcome := range []git.MergeOutcome{
		git.MergeUpToDate, git.MergeFastForward, git.MergeCommit,
	} {
		args := git.MergeArgs("main", "feature", outcome)

		// After `--`, so it cannot be read as an option, and spelled in full,
		// so a tag called `feature` cannot be what gets merged.
		want := []string{"--", "refs/heads/feature"}
		if got := args[len(args)-2:]; !slices.Equal(got, want) {
			t.Errorf("MergeArgs(%q) ends %v, want %v", outcome, got, want)
		}
	}
}

// The whole point of the outcome travelling with the request: the command must
// say what it does rather than let merge.ff say it.
func TestMergeArgsPinsWhatTheMergeWillBe(t *testing.T) {
	cases := map[git.MergeOutcome][]string{
		git.MergeFastForward: {"merge", "--ff-only", "--", "refs/heads/feature"},
		git.MergeUpToDate:    {"merge", "--ff-only", "--", "refs/heads/feature"},
		git.MergeCommit: {
			"merge", "--no-ff", "--no-edit",
			"-m", "Merge branch 'feature' into main",
			"--", "refs/heads/feature",
		},
	}

	for outcome, want := range cases {
		if args := git.MergeArgs("main", "feature", outcome); !slices.Equal(args, want) {
			t.Errorf("MergeArgs(%q) = %v, want %v", outcome, args, want)
		}
	}
}

// The message is written out for the same reason the flag is: git composes one
// from the name as it was given — refs/heads/… , here — and from merge.log,
// which is a setting that decides what a commit says.
func TestMergeArgsWritesTheMessageItselfWhereACommitIsRecorded(t *testing.T) {
	args := git.MergeArgs("main", "feature", git.MergeCommit)

	message, found := messageIn(args)
	if !found {
		t.Fatalf("MergeArgs = %v, want a -m the user can read before pressing the button", args)
	}
	if want := "Merge branch 'feature' into main"; message != want {
		t.Errorf("message = %q, want %q", message, want)
	}

	// And not where none is recorded: --ff-only writes no object, so a message
	// on that line would describe a commit that never happens.
	if _, found := messageIn(git.MergeArgs("main", "feature", git.MergeFastForward)); found {
		t.Error("a fast-forward carries a commit message, want none")
	}
}

// messageIn reads the -m out of an argument list, if there is one.
func messageIn(args []string) (string, bool) {
	index := slices.Index(args, "-m")
	if index < 0 || index+1 >= len(args) {
		return "", false
	}
	return args[index+1], true
}

// A bare `git merge` is the one command this package must never produce: it
// reads merge.ff, and the dialog that showed the line cannot read a setting.
func TestMergeArgsNeverLeavesTheDecisionToConfiguration(t *testing.T) {
	args := git.MergeArgs("main", "feature", git.MergeOutcome("something else entirely"))

	if !slices.Contains(args, "--ff-only") && !slices.Contains(args, "--no-ff") {
		t.Errorf("MergeArgs = %v, want a flag pinning what the merge is", args)
	}
}

func TestParseMergeOutcomeTakesTheThreeAndNothingElse(t *testing.T) {
	for _, raw := range []string{"up-to-date", "fast-forward", "merge-commit"} {
		outcome, err := git.ParseMergeOutcome(raw)
		if err != nil {
			t.Errorf("ParseMergeOutcome(%q): %v", raw, err)
		}
		if string(outcome) != raw {
			t.Errorf("ParseMergeOutcome(%q) = %q", raw, outcome)
		}
	}

	// The empty string is not the default. A client that names no outcome has
	// not read one off a plan, and the two commands differ by a commit.
	for _, raw := range []string{"", "ff", "no-ff", "rebase"} {
		if _, err := git.ParseMergeOutcome(raw); err == nil {
			t.Errorf("ParseMergeOutcome(%q) = nil, want a refusal", raw)
		}
	}
}

// A merge needs two names: the branch coming in, and the one it is going into
// — which is what the commit will say it was merged into.
func TestMergeRefusesAMissingName(t *testing.T) {
	runner := git.NewRunner(nil)

	for _, missing := range []struct{ into, branch string }{
		{into: "main", branch: ""},
		{into: "", branch: "feature"},
		{into: "  ", branch: "feature"},
	} {
		// No repository is touched: the name is refused before anything runs.
		err := runner.Merge(
			context.Background(), t.TempDir(), missing.into, missing.branch, git.MergeFastForward)

		var failure *git.Error
		if !errors.Is(err, git.ErrNoBranchName) || errors.As(err, &failure) {
			t.Errorf("Merge(%q, %q) = %v, want the daemon's own refusal",
				missing.into, missing.branch, err)
		}
	}
}
