package git_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The table of what each operation can be told to do.
//
// A pure function over two small enumerations, so every pair of them is tested
// rather than a sample. What that buys is the guarantee the whole feature
// rests on: a pair the interface can draw is a pair the daemon will run, and
// every other pair is refused rather than turned into a flag.

var everyOperation = []git.Operation{
	git.OperationNone,
	git.OperationMerge,
	git.OperationRebase,
	git.OperationCherryPick,
	git.OperationRevert,
	git.OperationBisect,
	git.OperationApply,
}

var everyAction = []git.Action{git.ActionAbort, git.ActionContinue, git.ActionSkip}

func TestEveryPairEitherRunsGitOrSaysWhyNot(t *testing.T) {
	for _, operation := range everyOperation {
		for _, action := range everyAction {
			args, err := git.ActionArgs(operation, action)

			if err != nil {
				// A refusal has to be one of the two the interface tells
				// apart. Anything else reaches the screen as a 500.
				if !errors.Is(err, git.ErrNoOperation) && !errors.Is(err, git.ErrActionUnavailable) {
					t.Errorf("%s/%s refused with an error nothing classifies: %v", operation, action, err)
				}
				continue
			}

			// A command that runs must be one — a subcommand and something
			// after it. An empty slice would exec `git` on its own, which
			// prints the manual and exits 1.
			if len(args) < 2 {
				t.Errorf("%s/%s built %q, which is not a command", operation, action, args)
			}
		}
	}
}

// The pairs that must run, and the exact line each one runs.
//
// Written out rather than derived. A test that built the expectation the same
// way the code does would pass just as happily with `--abrot` on both sides.
func TestTheCommandEachInstructionRuns(t *testing.T) {
	for _, testCase := range []struct {
		operation git.Operation
		action    git.Action
		want      string
	}{
		{git.OperationMerge, git.ActionAbort, "git merge --abort"},

		{git.OperationRebase, git.ActionAbort, "git rebase --abort"},
		{git.OperationRebase, git.ActionContinue, "git rebase --continue"},
		{git.OperationRebase, git.ActionSkip, "git rebase --skip"},

		{git.OperationCherryPick, git.ActionAbort, "git cherry-pick --abort"},
		{git.OperationCherryPick, git.ActionContinue, "git cherry-pick --continue"},
		{git.OperationCherryPick, git.ActionSkip, "git cherry-pick --skip"},

		{git.OperationRevert, git.ActionAbort, "git revert --abort"},
		{git.OperationRevert, git.ActionContinue, "git revert --continue"},
		{git.OperationRevert, git.ActionSkip, "git revert --skip"},

		{git.OperationApply, git.ActionAbort, "git am --abort"},
		{git.OperationApply, git.ActionContinue, "git am --continue"},
		{git.OperationApply, git.ActionSkip, "git am --skip"},

		// git has no `git bisect --abort`. Leaving a bisect is a reset, and
		// the button that says Abort has to run the command that does it.
		{git.OperationBisect, git.ActionAbort, "git bisect reset"},
	} {
		args, err := git.ActionArgs(testCase.operation, testCase.action)
		if err != nil {
			t.Errorf("%s/%s: %v", testCase.operation, testCase.action, err)
			continue
		}
		if line := git.CommandLine(args); line != testCase.want {
			t.Errorf("%s/%s runs %q, want %q", testCase.operation, testCase.action, line, testCase.want)
		}
	}
}

// An unknown action must never become a flag.
//
// Every command in the table is spelled `--` and the action, so a string that
// reaches the switch unchecked is an option handed to git by whoever wrote the
// request body. `exec` is the one that matters: `git rebase --exec` takes a
// SHELL COMMAND as its argument.
func TestAnUnknownActionIsRefusedRatherThanSpelledAsAFlag(t *testing.T) {
	for _, action := range []git.Action{"exec", "onto", "", "--abort", "abort --exec rm -rf /"} {
		for _, operation := range everyOperation {
			args, err := git.ActionArgs(operation, action)
			if err == nil {
				t.Fatalf("%s/%q built %q instead of refusing", operation, action, args)
			}
			if operation != git.OperationNone && !errors.Is(err, git.ErrActionUnavailable) {
				t.Errorf("%s/%q refused with %v, want ErrActionUnavailable", operation, action, err)
			}
		}
	}
}

// The pairs the interface draws are exactly the pairs the daemon accepts.
//
// Two sources for one rule is how a button comes to exist for a command that
// is refused, so Actions is derived from ActionArgs; this is the test that
// says it still is.
func TestTheButtonsOfferedAreTheCommandsAccepted(t *testing.T) {
	for _, operation := range everyOperation {
		offered := git.Actions(operation)

		for _, action := range offered {
			if _, err := git.ActionArgs(operation, action); err != nil {
				t.Errorf("%s offers %s, which is then refused: %v", operation, action, err)
			}
		}

		for _, action := range everyAction {
			_, err := git.ActionArgs(operation, action)
			if err == nil && !slices.Contains(offered, action) {
				t.Errorf("%s accepts %s but does not offer it", operation, action)
			}
		}
	}
}

func TestNothingIsOfferedForARepositoryInTheMiddleOfNothing(t *testing.T) {
	if offered := git.Actions(git.OperationNone); len(offered) != 0 {
		t.Errorf("an idle repository offers %v, want nothing", offered)
	}
}

// Continue is first because it is what somebody who just resolved a conflict
// is reaching for, and it is the only one of the three that keeps their work.
func TestContinueIsOfferedFirstWhereItIsOfferedAtAll(t *testing.T) {
	offered := git.Actions(git.OperationRebase)
	if len(offered) == 0 || offered[0] != git.ActionContinue {
		t.Errorf("a rebase offers %v, want continue first", offered)
	}
}

// A merge is finished by committing, in the box that shows the message git
// prepared. `git merge --continue` would commit that message without showing
// it, throwing away anything typed into the box — so the pair does not exist.
func TestAMergeIsAbortedButNotContinued(t *testing.T) {
	if offered := git.Actions(git.OperationMerge); !slices.Equal(offered, []git.Action{git.ActionAbort}) {
		t.Errorf("a merge offers %v, want abort alone", offered)
	}

	_, err := git.ActionArgs(git.OperationMerge, git.ActionContinue)
	if !errors.Is(err, git.ErrActionUnavailable) {
		t.Fatalf("continuing a merge gave %v, want ErrActionUnavailable", err)
	}
	// The refusal is read by a person, so it has to say what to do instead.
	if !strings.Contains(err.Error(), "committing") {
		t.Errorf("the refusal does not name the way out: %v", err)
	}
}

// Continuing a bisect means saying whether this commit is good or bad. yagit
// does not ask that, and a button that continued one would have to guess.
func TestABisectIsOnlyLeft(t *testing.T) {
	if offered := git.Actions(git.OperationBisect); !slices.Equal(offered, []git.Action{git.ActionAbort}) {
		t.Errorf("a bisect offers %v, want abort alone", offered)
	}
}

func TestNothingCanBeDoneToAnOperationThatIsNotHappening(t *testing.T) {
	for _, action := range everyAction {
		if _, err := git.ActionArgs(git.OperationNone, action); !errors.Is(err, git.ErrNoOperation) {
			t.Errorf("%s on an idle repository gave %v, want ErrNoOperation", action, err)
		}
	}
}

// Which instructions need a confirmation, decided here rather than in a
// dialog that could be refactored out of asking.
func TestOnlyContinueKeepsWhatIsThere(t *testing.T) {
	if git.Destroys(git.ActionContinue) {
		t.Error("continue is marked destructive; it records the resolution and moves on")
	}
	for _, action := range []git.Action{git.ActionAbort, git.ActionSkip} {
		if !git.Destroys(action) {
			t.Errorf("%s is not marked destructive, so the interface would run it without asking", action)
		}
	}
}
