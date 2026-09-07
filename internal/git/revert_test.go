package git_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The argument list, without git: the separator, --no-edit so the daemon never
// looks for a terminal, and --no-reference so revert.reference cannot commit
// that option's placeholder as the subject.

func TestRevertArgsPinsWhatTheRevertWillBe(t *testing.T) {
	want := []string{"revert", "--no-edit", "--no-reference", "--", "abc1234"}
	if args := git.RevertArgs("abc1234"); !slices.Equal(args, want) {
		t.Errorf("RevertArgs = %v, want %v", args, want)
	}
}

func TestRevertArgsPutsTheRevisionWhereItCannotBeAnOption(t *testing.T) {
	args := git.RevertArgs("abc1234")
	want := []string{"--", "abc1234"}
	if got := args[len(args)-2:]; !slices.Equal(got, want) {
		t.Errorf("RevertArgs ends %v, want %v", got, want)
	}
}

func TestParseRevertOutcomeTakesRevertAndNothingElse(t *testing.T) {
	outcome, err := git.ParseRevertOutcome("revert")
	if err != nil {
		t.Fatalf("ParseRevertOutcome(revert): %v", err)
	}
	if outcome != git.RevertApply {
		t.Errorf("ParseRevertOutcome(revert) = %q", outcome)
	}

	for _, raw := range []string{"", "cherry-pick", "undo", "apply"} {
		_, err := git.ParseRevertOutcome(raw)
		if err == nil {
			t.Errorf("ParseRevertOutcome(%q) succeeded, want a refusal", raw)
			continue
		}
		if !strings.Contains(err.Error(), "revert") {
			t.Errorf("ParseRevertOutcome(%q) = %v, want the outcome named", raw, err)
		}
	}
}
