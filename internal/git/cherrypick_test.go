package git_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The argument list, without git: the separator, the flag that pins the
// outcome, and --no-edit so the daemon never looks for a terminal.

func TestCherryPickArgsPinsWhatThePickWillBe(t *testing.T) {
	cases := map[git.CherryPickOutcome][]string{
		git.CherryPickFastForward: {
			"cherry-pick", "--no-edit", "--ff", "--", "abc1234",
		},
		git.CherryPickApply: {
			"cherry-pick", "--no-edit", "--no-ff", "--", "abc1234",
		},
		// Up-to-date never reaches Exec — see CherryPick — but the helper must
		// still refuse to emit a bare cherry-pick, so a careless caller cannot
		// reintroduce an unpinned command.
		git.CherryPickUpToDate: {
			"cherry-pick", "--no-edit", "--no-ff", "--", "abc1234",
		},
	}

	for outcome, want := range cases {
		if args := git.CherryPickArgs("abc1234", outcome); !slices.Equal(args, want) {
			t.Errorf("CherryPickArgs(%q) = %v, want %v", outcome, args, want)
		}
	}
}

func TestCherryPickArgsPutsTheRevisionWhereItCannotBeAnOption(t *testing.T) {
	for _, outcome := range []git.CherryPickOutcome{
		git.CherryPickUpToDate, git.CherryPickFastForward, git.CherryPickApply,
	} {
		args := git.CherryPickArgs("abc1234", outcome)
		want := []string{"--", "abc1234"}
		if got := args[len(args)-2:]; !slices.Equal(got, want) {
			t.Errorf("CherryPickArgs(%q) ends %v, want %v", outcome, got, want)
		}
	}
}

func TestParseCherryPickOutcomeTakesTheThreeAndNothingElse(t *testing.T) {
	for _, raw := range []string{"up-to-date", "fast-forward", "cherry-pick"} {
		outcome, err := git.ParseCherryPickOutcome(raw)
		if err != nil {
			t.Errorf("ParseCherryPickOutcome(%q): %v", raw, err)
			continue
		}
		if string(outcome) != raw {
			t.Errorf("ParseCherryPickOutcome(%q) = %q", raw, outcome)
		}
	}

	for _, raw := range []string{"", "merge-commit", "ff", "apply"} {
		_, err := git.ParseCherryPickOutcome(raw)
		if err == nil {
			t.Errorf("ParseCherryPickOutcome(%q) succeeded, want a refusal", raw)
			continue
		}
		if !strings.Contains(err.Error(), "up-to-date") ||
			!strings.Contains(err.Error(), "fast-forward") ||
			!strings.Contains(err.Error(), "cherry-pick") {
			t.Errorf("ParseCherryPickOutcome(%q) = %v, want the three named", raw, err)
		}
	}
}
