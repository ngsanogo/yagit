package git_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The argument list, without git: every mode pins its flag. The revision is
// NOT after `--` — see ResetArgs: that separator turns it into a pathspec.

func TestResetArgsPinsEachMode(t *testing.T) {
	cases := []struct {
		mode git.ResetMode
		flag string
	}{
		{git.ResetSoft, "--soft"},
		{git.ResetMixed, "--mixed"},
		{git.ResetHard, "--hard"},
	}
	for _, testCase := range cases {
		want := []string{"reset", testCase.flag, "abc1234"}
		if args := git.ResetArgs("abc1234", testCase.mode); !slices.Equal(args, want) {
			t.Errorf("ResetArgs(%s) = %v, want %v", testCase.mode, args, want)
		}
	}
}

func TestResetArgsDoesNotPutAPathspecSeparatorBeforeTheRevision(t *testing.T) {
	args := git.ResetArgs("abc1234", git.ResetHard)
	if slices.Contains(args, "--") {
		t.Errorf("ResetArgs = %v, must not contain --: git would read the revision as a path", args)
	}
	if args[len(args)-1] != "abc1234" {
		t.Errorf("ResetArgs ends %q, want the revision", args[len(args)-1])
	}
}

func TestParseResetModeTakesTheThreeAndNothingElse(t *testing.T) {
	for _, raw := range []string{"soft", "mixed", "hard"} {
		mode, err := git.ParseResetMode(raw)
		if err != nil {
			t.Fatalf("ParseResetMode(%q): %v", raw, err)
		}
		if string(mode) != raw {
			t.Errorf("ParseResetMode(%q) = %q", raw, mode)
		}
	}

	for _, raw := range []string{"", "Soft", "keep", "merge", "soft "} {
		_, err := git.ParseResetMode(raw)
		if err == nil {
			t.Errorf("ParseResetMode(%q) succeeded, want a refusal", raw)
			continue
		}
		if !strings.Contains(err.Error(), "soft, mixed or hard") {
			t.Errorf("ParseResetMode(%q) = %v, want the three named", raw, err)
		}
	}
}
