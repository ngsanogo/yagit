package git_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// Interactive rebase, against the real binary.
//
// The plan is checked without git in interactive_test.go; here the question is
// what git does with the todo list yagit writes — that it is executed at all,
// that each verb means what the interface says it means, and that the two
// things the design leans on are true: a stop at an `edit` is a state the
// existing buttons can finish, and a step that would open an editor fails
// loudly rather than committing something nobody read.

// TestMain makes this test binary the two editors a rebase points git at.
//
// os.Executable is what yagit names, and under `go test` that is this binary
// — so for an interactive rebase to run here at all, this binary has to answer
// the same requests cmd/yagit answers. It answers them by calling the same
// function, which is the point: the thing under test and the thing that ships
// are one definition, and these tests prove it by using it rather than by
// asserting about it.
func TestMain(m *testing.M) {
	if handled, err := git.RunAsEditor(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// planned is a linear history on main: base → one → two → three.
//
// Each commit touches a file of its own, so any order of them applies cleanly
// and a test that wants a conflict has to arrange one on purpose.
func planned(t *testing.T) (string, *git.Runner) {
	t.Helper()
	isolateGitConfiguration(t)

	dir := t.TempDir()
	runner := git.NewRunner(nil)

	runGit(t, runner, dir, "init", "-b", "main")

	// The identity goes in the repository rather than on each commit, which is
	// what every other integration test here does. A rebase makes commits of
	// its own, from inside git, where no `-c` this test wrote can reach them —
	// and git's refusal to guess a committer is the right one: it would
	// otherwise be `ubuntu@somehost` on whoever's machine ran the suite.
	runGit(t, runner, dir, "config", "user.name", "yagit Test")
	runGit(t, runner, dir, "config", "user.email", "test@yagit.local")

	writeWorkFile(t, dir, "base.md", "base\n")
	runGit(t, runner, dir, "add", "--", "base.md")
	runGit(t, runner, dir, commitWith("base")...)

	for _, name := range []string{"one", "two", "three"} {
		writeWorkFile(t, dir, name+".md", name+"\n")
		runGit(t, runner, dir, "add", "--", name+".md")
		runGit(t, runner, dir, commitWith(name)...)
	}

	return dir, runner
}

// planRange reads the range and fails the test if it cannot be read. Every
// test here starts from it, because a plan is only ever built against one.
func planRange(t *testing.T, runner *git.Runner, dir, base string) []git.Commit {
	t.Helper()
	_, _, commits, err := runner.PlanRange(context.Background(), dir, base)
	if err != nil {
		t.Fatalf("PlanRange: %v", err)
	}
	return commits
}

// subjectsOf is how these tests compare histories: by what the commits say,
// since every hash changes when a plan runs. Newest first, as git walks.
func subjectsOf(t *testing.T, runner *git.Runner, dir string) []string {
	t.Helper()
	output, err := runner.Run(context.Background(), dir, "log", "--pretty=format:%s", "--")
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(output)), "\n")
}

// run builds the todo list from a plan and hands it to git, the way the route
// does: check against the range that was read, then run.
func run(t *testing.T, runner *git.Runner, dir, base string, commits []git.Commit, plan []git.RebaseStep) error {
	t.Helper()
	if err := git.CheckPlan(plan, commits); err != nil {
		t.Fatalf("CheckPlan refused a plan the test meant to run: %v", err)
	}
	return runner.RebaseInteractive(
		context.Background(), dir, base, git.TodoList(plan, commits))
}

func TestPlanRangeListsTheCommitsAfterTheBaseOldestFirst(t *testing.T) {
	dir, runner := planned(t)
	base := commitOf(t, runner, dir, "main~3")

	baseSHA, baseSubject, commits, err := runner.PlanRange(context.Background(), dir, "main~3")
	if err != nil {
		t.Fatalf("PlanRange: %v", err)
	}

	if baseSHA != base {
		t.Errorf("base = %s, want the full name %s", baseSHA, base)
	}
	if baseSubject != "base" {
		t.Errorf("base subject = %q, want %q", baseSubject, "base")
	}

	// Oldest first, because that is the order a todo list is executed in. A
	// plan drawn newest-first would be a second convention for one list.
	want := []string{"one", "two", "three"}
	got := make([]string, 0, len(commits))
	for _, commit := range commits {
		got = append(got, commit.Subject)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("range = %v, want %v", got, want)
	}
}

func TestPlanRangeRefusesACommitTheBranchNeverHeld(t *testing.T) {
	dir, runner := planned(t)

	runGit(t, runner, dir, "checkout", "-b", "elsewhere", "main~2")
	writeWorkFile(t, dir, "aside.md", "aside\n")
	runGit(t, runner, dir, "add", "--", "aside.md")
	runGit(t, runner, dir, commitWith("aside")...)
	aside := commitOf(t, runner, dir, "elsewhere")
	runGit(t, runner, dir, "checkout", "main")

	_, _, _, err := runner.PlanRange(context.Background(), dir, aside)
	if !errors.Is(err, git.ErrResetNotOnBranch) {
		t.Errorf("PlanRange over a foreign commit: %v, want ErrResetNotOnBranch", err)
	}
}

func TestPlanRangeRefusesTheTipItself(t *testing.T) {
	dir, runner := planned(t)

	_, _, _, err := runner.PlanRange(context.Background(), dir, "main")
	if !errors.Is(err, git.ErrRangeEmpty) {
		t.Errorf("PlanRange over the tip: %v, want ErrRangeEmpty", err)
	}
}

// A merge in the range is refused rather than flattened. git would drop it
// without a word — a plan is a list of lines and a merge commit is not one.
func TestPlanRangeRefusesAMergeInTheRange(t *testing.T) {
	dir, runner := planned(t)

	runGit(t, runner, dir, "checkout", "-b", "side", "main~1")
	writeWorkFile(t, dir, "side.md", "side\n")
	runGit(t, runner, dir, "add", "--", "side.md")
	runGit(t, runner, dir, commitWith("side")...)
	runGit(t, runner, dir, "checkout", "main")
	runGit(t, runner, dir, append(append([]string{}, identity...),
		"merge", "--no-ff", "side", "-m", "merge side")...)

	_, _, _, err := runner.PlanRange(context.Background(), dir, "main~4")
	if !errors.Is(err, git.ErrRangeHoldsMerge) {
		t.Errorf("PlanRange over a range holding a merge: %v, want ErrRangeHoldsMerge", err)
	}
}

func TestRebaseInteractiveReordersCommits(t *testing.T) {
	dir, runner := planned(t)
	base := commitOf(t, runner, dir, "main~3")
	commits := planRange(t, runner, dir, base)

	// three, one, two — the last commit moved to the front of the range.
	plan := []git.RebaseStep{
		{Commit: commits[2].SHA, Instruction: git.InstructionPick},
		{Commit: commits[0].SHA, Instruction: git.InstructionPick},
		{Commit: commits[1].SHA, Instruction: git.InstructionPick},
	}

	if err := run(t, runner, dir, base, commits, plan); err != nil {
		t.Fatalf("RebaseInteractive: %v", err)
	}

	want := []string{"two", "one", "three", "base"}
	if got := subjectsOf(t, runner, dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("history = %v, want %v", got, want)
	}
}

func TestRebaseInteractiveDropsACommit(t *testing.T) {
	dir, runner := planned(t)
	base := commitOf(t, runner, dir, "main~3")
	commits := planRange(t, runner, dir, base)

	plan := []git.RebaseStep{
		{Commit: commits[0].SHA, Instruction: git.InstructionPick},
		{Commit: commits[1].SHA, Instruction: git.InstructionDrop},
		{Commit: commits[2].SHA, Instruction: git.InstructionPick},
	}

	if err := run(t, runner, dir, base, commits, plan); err != nil {
		t.Fatalf("RebaseInteractive: %v", err)
	}

	want := []string{"three", "one", "base"}
	if got := subjectsOf(t, runner, dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("history = %v, want %v", got, want)
	}

	// The changes go with it. A drop that left the file behind would be a
	// squash under another name.
	if _, err := os.Stat(dir + "/two.md"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("two.md is still in the work tree after its commit was dropped: %v", err)
	}
}

// The two combines differ by whose message survives, and by nothing else. That
// is the question `squash` dodges by joining them and opening an editor over
// the result, which is the question a daemon cannot ask.
func TestRebaseInteractiveCombinesKeepingTheMessageChosen(t *testing.T) {
	for _, combine := range []struct {
		instruction git.Instruction
		// survives is the subject the combined commit ends up with.
		survives string
	}{
		{git.InstructionFixup, "one"},     // the message above survives
		{git.InstructionFixupKeep, "two"}, // this commit's message survives
	} {
		instruction, name := combine.instruction, combine.survives
		t.Run(string(instruction), func(t *testing.T) {
			dir, runner := planned(t)
			base := commitOf(t, runner, dir, "main~3")
			commits := planRange(t, runner, dir, base)

			plan := []git.RebaseStep{
				{Commit: commits[0].SHA, Instruction: git.InstructionPick},
				{Commit: commits[1].SHA, Instruction: instruction},
				{Commit: commits[2].SHA, Instruction: git.InstructionPick},
			}

			if err := run(t, runner, dir, base, commits, plan); err != nil {
				t.Fatalf("RebaseInteractive: %v", err)
			}

			want := []string{"three", name, "base"}
			if got := subjectsOf(t, runner, dir); strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("history = %v, want %v", got, want)
			}

			// Combined, not dropped: both files are in the one commit that is
			// left.
			for _, file := range []string{"one.md", "two.md"} {
				if _, err := os.Stat(dir + "/" + file); err != nil {
					t.Errorf("%s did not survive the combine: %v", file, err)
				}
			}
		})
	}
}

// An `edit` stop is the state the interface already knows how to finish, and
// this is the assertion the whole design rests on: the operation is a rebase,
// and NOTHING is blocked — so the Continue button the banner draws is a button
// that runs.
func TestRebaseInteractiveStopsAtAnEditTheBannerCanFinish(t *testing.T) {
	dir, runner := planned(t)
	base := commitOf(t, runner, dir, "main~3")
	commits := planRange(t, runner, dir, base)

	plan := []git.RebaseStep{
		{Commit: commits[0].SHA, Instruction: git.InstructionPick},
		{Commit: commits[1].SHA, Instruction: git.InstructionEdit},
		{Commit: commits[2].SHA, Instruction: git.InstructionPick},
	}

	// Exit zero, and this is the fact the route is built on: git reports
	// success when it stops at an `edit`, because stopping there is what it
	// was asked to do. A conflict exits non-zero; a stop does not. Nothing in
	// the command's answer distinguishes "finished" from "waiting", so the
	// state is what has to be read afterwards — see RebaseInteractive.
	if err := run(t, runner, dir, base, commits, plan); err != nil {
		t.Fatalf("RebaseInteractive: %v, want the stop at an edit to be reported as success", err)
	}

	state, err := git.ReadState(dir + "/.git")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationRebase {
		t.Errorf("operation = %q, want a rebase", state.Operation)
	}
	if state.Branch != "main" {
		t.Errorf("branch = %q, want main", state.Branch)
	}
	if len(state.Blocked) != 0 {
		t.Errorf("blocked = %v, want nothing: a plan yagit wrote holds no step that opens an editor", state.Blocked)
	}

	// The commit is made and the work tree is clean — an `edit` stops AFTER
	// applying, which is what makes it an amend rather than a conflict.
	if status := statusOf(t, runner, dir); len(status.Files) != 0 {
		t.Errorf("work tree holds %d changed files at an edit stop, want none", len(status.Files))
	}

	if err := runner.ActOnOperation(
		context.Background(), dir, git.OperationRebase, git.ActionContinue); err != nil {
		t.Fatalf("continuing from the edit stop: %v", err)
	}

	want := []string{"three", "two", "one", "base"}
	if got := subjectsOf(t, runner, dir); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("history = %v, want %v", got, want)
	}
}

// The invariant, enforced by an editor that refuses rather than by a second
// check: a step that opens one cannot quietly record a message nobody read.
// CheckPlan refuses `squash` long before this, which is why the todo list here
// is written by hand — this asks what the last line of defence does if the
// first one is ever wrong.
//
// It asks it on every platform, which is the reason the refusal is a program
// rather than an unset variable. Left unset, git answers "Terminal is dumb,
// but EDITOR unset" on Linux and macOS and, on Windows, opens an editor from
// its own bundled environment that waits for a terminal it does not have — so
// this test hung for ten minutes there instead of failing, and so would a
// daemon.
func TestRebaseInteractiveFailsLoudlyOnAStepThatOpensAnEditor(t *testing.T) {
	dir, runner := planned(t)
	base := commitOf(t, runner, dir, "main~3")
	commits := planRange(t, runner, dir, base)

	todo := fmt.Sprintf("pick %s one\nsquash %s two\npick %s three\n",
		commits[0].SHA, commits[1].SHA, commits[2].SHA)

	err := runner.RebaseInteractive(context.Background(), dir, base, todo)
	if err == nil {
		t.Fatal("git accepted a squash and committed a message nobody was shown")
	}

	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want a git failure carrying the command", err)
	}
	// yagit's own sentence, written by the editor git ran and passed through
	// whole. A message about a missing editor would be git's answer to a
	// misconfiguration; this is an answer about what yagit will not do.
	if !strings.Contains(failure.Stderr, "commit one unread") {
		t.Errorf("stderr = %q, want the refusal yagit's own editor writes", failure.Stderr)
	}
}

// No --no-ff, and this is what its absence buys: a plan that changes only its
// last row leaves every commit below that row exactly where it was, hash and
// all. With --no-ff every one of them would be written again.
func TestRebaseInteractiveKeepsTheHashesThePlanDidNotTouch(t *testing.T) {
	dir, runner := planned(t)
	base := commitOf(t, runner, dir, "main~3")
	commits := planRange(t, runner, dir, base)

	plan := []git.RebaseStep{
		{Commit: commits[0].SHA, Instruction: git.InstructionPick},
		{Commit: commits[1].SHA, Instruction: git.InstructionPick},
		{Commit: commits[2].SHA, Instruction: git.InstructionDrop},
	}

	if err := run(t, runner, dir, base, commits, plan); err != nil {
		t.Fatalf("RebaseInteractive: %v", err)
	}

	if tip := commitOf(t, runner, dir, "main"); tip != commits[1].SHA {
		t.Errorf("tip = %s, want the untouched %s", git.ShortSHA(tip), git.ShortSHA(commits[1].SHA))
	}
}

// A conflict stops the same way every other replay does, and the refusal
// travels whole — the command, the exit code, git's own stderr — so the
// resolution screen that already exists takes over.
func TestRebaseInteractiveStopsOnAConflictWithTheCommandWhole(t *testing.T) {
	dir, runner := planned(t)

	// Two commits touching one file, so reordering them cannot apply.
	writeWorkFile(t, dir, "shared.md", "first\n")
	runGit(t, runner, dir, "add", "--", "shared.md")
	runGit(t, runner, dir, commitWith("shared first")...)
	writeWorkFile(t, dir, "shared.md", "second\n")
	runGit(t, runner, dir, commitWith("shared second")...)

	base := commitOf(t, runner, dir, "main~2")
	commits := planRange(t, runner, dir, base)

	plan := []git.RebaseStep{
		{Commit: commits[1].SHA, Instruction: git.InstructionPick},
		{Commit: commits[0].SHA, Instruction: git.InstructionPick},
	}

	err := run(t, runner, dir, base, commits, plan)
	if err == nil {
		t.Fatal("the reordering applied, so there is no conflict to test")
	}

	var failure *git.Error
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want a git failure carrying the command", err)
	}
	if !strings.Contains(failure.CommandLine(), "rebase --interactive") {
		t.Errorf("command = %q, want the interactive rebase", failure.CommandLine())
	}

	state, err := git.ReadState(dir + "/.git")
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationRebase {
		t.Errorf("operation = %q, want a rebase", state.Operation)
	}
}
