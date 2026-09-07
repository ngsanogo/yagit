package git_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

func TestRebaseArgsNamesTheBranchWhereNothingElseCanAnswerTo(t *testing.T) {
	for _, outcome := range []git.RebaseOutcome{
		git.RebaseUpToDate, git.RebaseFastForward, git.RebaseReplay,
	} {
		args := git.RebaseArgs("main", outcome)

		want := []string{"--", "refs/heads/main"}
		if got := args[len(args)-2:]; !slices.Equal(got, want) {
			t.Errorf("RebaseArgs(%q) ends %v, want %v", outcome, got, want)
		}
	}
}

// Every setting git would otherwise read, on every outcome. --no-update-refs
// is the one worth naming: without it, `rebase.updateRefs` force-moves every
// other branch pointing into the replayed range, which no confirmation named.
func TestRebaseArgsPinsWhatConfigurationWouldDecide(t *testing.T) {
	pinned := []string{
		"--merge",
		"--no-autosquash",
		"--no-autostash",
		"--no-rebase-merges",
		"--no-update-refs",
	}

	for _, outcome := range []git.RebaseOutcome{
		git.RebaseUpToDate, git.RebaseFastForward, git.RebaseReplay,
	} {
		args := git.RebaseArgs("main", outcome)
		for _, flag := range pinned {
			if !slices.Contains(args, flag) {
				t.Errorf("RebaseArgs(%q) = %v, want %s among them", outcome, args, flag)
			}
		}
	}
}

// The lease, and the half of it that matters most: --no-ff forces the replay
// the dialog promised, and it must NOT appear on the other two — on an
// up-to-date rebase it means "rebase forced", which rewrites every commit on
// the branch under new hashes while the sentence said nothing would happen.
func TestRebaseArgsForcesOnlyTheReplay(t *testing.T) {
	want := []string{
		"rebase",
		"--merge",
		"--no-autosquash",
		"--no-autostash",
		"--no-rebase-merges",
		"--no-update-refs",
		"--no-ff",
		"--",
		"refs/heads/main",
	}
	if args := git.RebaseArgs("main", git.RebaseReplay); !slices.Equal(args, want) {
		t.Errorf("RebaseArgs(rebase) = %v, want %v", args, want)
	}

	for _, outcome := range []git.RebaseOutcome{git.RebaseUpToDate, git.RebaseFastForward} {
		if args := git.RebaseArgs("main", outcome); slices.Contains(args, "--no-ff") {
			t.Errorf("RebaseArgs(%q) = %v, want no --no-ff on an outcome that writes nothing",
				outcome, args)
		}
	}
}

func TestParseRebaseOutcomeTakesTheThreeAndNothingElse(t *testing.T) {
	for _, raw := range []string{"up-to-date", "fast-forward", "rebase"} {
		outcome, err := git.ParseRebaseOutcome(raw)
		if err != nil {
			t.Errorf("ParseRebaseOutcome(%q): %v", raw, err)
		}
		if string(outcome) != raw {
			t.Errorf("ParseRebaseOutcome(%q) = %q", raw, outcome)
		}
	}

	for _, raw := range []string{"", "merge-commit", "squash", "REBASE"} {
		if _, err := git.ParseRebaseOutcome(raw); err == nil {
			t.Errorf("ParseRebaseOutcome(%q) = nil, want a refusal", raw)
		}
	}
}

func TestRebaseRefusesAMissingName(t *testing.T) {
	runner := git.NewRunner(nil)

	for _, missing := range []string{"", "  ", "\n"} {
		err := runner.Rebase(context.Background(), t.TempDir(), missing, git.RebaseReplay)

		var failure *git.Error
		if !errors.Is(err, git.ErrNoBranchName) || errors.As(err, &failure) {
			t.Errorf("Rebase(%q) = %v, want the daemon's own refusal", missing, err)
		}
	}
}

func TestPreviewRebaseRefusesAMissingName(t *testing.T) {
	runner := git.NewRunner(nil)

	for _, missing := range []struct{ from, onto string }{
		{from: "feature", onto: ""},
		{from: "", onto: "main"},
		{from: "feature", onto: "  "},
	} {
		_, err := runner.PreviewRebase(
			context.Background(), t.TempDir(), missing.from, missing.onto)

		var failure *git.Error
		if !errors.Is(err, git.ErrNoBranchName) || errors.As(err, &failure) {
			t.Errorf("PreviewRebase(%q, %q) = %v, want the daemon's own refusal",
				missing.from, missing.onto, err)
		}
	}
}
