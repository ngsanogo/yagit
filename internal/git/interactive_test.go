package git_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// The plan, without running git.
//
// Everything here is a pure function over a plan and the range it claims to
// rearrange: the words the wire accepts, the four ways a plan can be a lie
// about that range, and the file the accepted ones turn into. The integration
// tests beside this ask what git does with the result.

func TestParseInstructionTakesTheFiveAndNothingElse(t *testing.T) {
	for _, raw := range []string{"pick", "fixup", "fixup -C", "edit", "drop"} {
		instruction, err := git.ParseInstruction(raw)
		if err != nil {
			t.Errorf("ParseInstruction(%q): %v", raw, err)
		}
		if string(instruction) != raw {
			t.Errorf("ParseInstruction(%q) = %q, want the verb itself", raw, instruction)
		}
	}

	// squash and reword are the two that matter here. They are real git verbs,
	// and they are refused — not because yagit hides them, but because both
	// open an editor over a message that does not exist yet, which is the
	// state state.go already refuses to continue.
	for _, raw := range []string{"", "squash", "reword", "fixup -c", "exec", "PICK", "s"} {
		if _, err := git.ParseInstruction(raw); err == nil {
			t.Errorf("ParseInstruction(%q) was accepted, want a refusal", raw)
		}
	}
}

// aRange is three commits to plan over, in the order a plan lists them.
func aRange() []git.Commit {
	return []git.Commit{
		{SHA: strings.Repeat("a", 40), Subject: "first"},
		{SHA: strings.Repeat("b", 40), Subject: "second"},
		{SHA: strings.Repeat("c", 40), Subject: "third"},
	}
}

func step(sha string, instruction git.Instruction) git.RebaseStep {
	return git.RebaseStep{Commit: sha, Instruction: instruction}
}

func TestCheckPlanAcceptsARearrangement(t *testing.T) {
	commits := aRange()

	// Reversed, with the last one folded into the one now above it: the shape
	// the whole feature exists for.
	plan := []git.RebaseStep{
		step(commits[2].SHA, git.InstructionPick),
		step(commits[1].SHA, git.InstructionFixup),
		step(commits[0].SHA, git.InstructionEdit),
	}

	if err := git.CheckPlan(plan, commits); err != nil {
		t.Errorf("CheckPlan: %v", err)
	}
}

func TestCheckPlanAcceptsDroppingEveryCommit(t *testing.T) {
	commits := aRange()

	plan := make([]git.RebaseStep, 0, len(commits))
	for _, commit := range commits {
		plan = append(plan, step(commit.SHA, git.InstructionDrop))
	}

	// Not a refusal: the branch ends at the base, git carries it out without
	// complaint, and it is the one plan whose meaning the `drop` verb states
	// exactly.
	if err := git.CheckPlan(plan, commits); err != nil {
		t.Errorf("CheckPlan on an all-drop plan: %v", err)
	}
}

func TestCheckPlanRefusesAPlanThatIsNotTheRange(t *testing.T) {
	commits := aRange()
	foreign := strings.Repeat("f", 40)

	cases := map[string][]git.RebaseStep{
		"empty": {},
		"a commit from outside the range": {
			step(commits[0].SHA, git.InstructionPick),
			step(commits[1].SHA, git.InstructionPick),
			step(foreign, git.InstructionPick),
		},
		"a commit named twice": {
			step(commits[0].SHA, git.InstructionPick),
			step(commits[0].SHA, git.InstructionPick),
			step(commits[1].SHA, git.InstructionPick),
			step(commits[2].SHA, git.InstructionPick),
		},
		"a commit left out": {
			step(commits[0].SHA, git.InstructionPick),
			step(commits[1].SHA, git.InstructionPick),
		},
	}

	for name, plan := range cases {
		err := git.CheckPlan(plan, commits)
		if err == nil {
			t.Errorf("CheckPlan with %s was accepted, want a refusal", name)
			continue
		}
		if !errors.Is(err, git.ErrPlanNotTheRange) && !errors.Is(err, git.ErrEmptyPlan) {
			t.Errorf("CheckPlan with %s: %v, want a plan refusal", name, err)
		}
	}
}

// A fixup at the top of the plan is what git answers AFTER starting the
// rebase, leaving a repository stopped inside a plan that could never have
// run. Both spellings of "nothing above it" are refused: the literal first
// row, and the first row that survives.
func TestCheckPlanRefusesACombineWithNothingAboveIt(t *testing.T) {
	commits := aRange()

	cases := map[string][]git.RebaseStep{
		"first row": {
			step(commits[0].SHA, git.InstructionFixup),
			step(commits[1].SHA, git.InstructionPick),
			step(commits[2].SHA, git.InstructionPick),
		},
		"first row that survives": {
			step(commits[0].SHA, git.InstructionDrop),
			step(commits[1].SHA, git.InstructionFixupKeep),
			step(commits[2].SHA, git.InstructionPick),
		},
	}

	for name, plan := range cases {
		err := git.CheckPlan(plan, commits)
		if !errors.Is(err, git.ErrCombineWithoutTarget) {
			t.Errorf("CheckPlan with a combine on the %s: %v, want ErrCombineWithoutTarget", name, err)
		}
	}
}

func TestCheckPlanRefusesAnInstructionItDoesNotKnow(t *testing.T) {
	commits := aRange()
	plan := []git.RebaseStep{
		step(commits[0].SHA, "squash"),
		step(commits[1].SHA, git.InstructionPick),
		step(commits[2].SHA, git.InstructionPick),
	}

	if err := git.CheckPlan(plan, commits); err == nil {
		t.Error("CheckPlan accepted a squash, want a refusal")
	}
}

// The file git executes. The subjects come from the range rather than from the
// plan, which is what makes the line safe to write unquoted.
func TestTodoListWritesTheVerbTheCommitAndTheSubject(t *testing.T) {
	commits := aRange()
	plan := []git.RebaseStep{
		step(commits[1].SHA, git.InstructionPick),
		step(commits[2].SHA, git.InstructionFixupKeep),
		step(commits[0].SHA, git.InstructionDrop),
	}

	want := "pick " + commits[1].SHA + " second\n" +
		"fixup -C " + commits[2].SHA + " third\n" +
		"drop " + commits[0].SHA + " first\n"

	if got := git.TodoList(plan, commits); got != want {
		t.Errorf("TodoList =\n%q\nwant\n%q", got, want)
	}
}

// InteractiveRebaseArgs pins the four settings RebaseArgs pins, and does NOT
// pin --no-ff. There it makes "every commit is written again" true; here it
// would rewrite the commits the plan left alone.
func TestInteractiveRebaseArgsPinsTheSettingsAndNotTheFastForward(t *testing.T) {
	args := git.InteractiveRebaseArgs("a2801ba")
	line := strings.Join(args, " ")

	for _, flag := range []string{
		"--interactive", "--no-autosquash", "--no-autostash",
		"--no-rebase-merges", "--no-update-refs",
	} {
		if !strings.Contains(line, flag) {
			t.Errorf("InteractiveRebaseArgs = %q, want it to carry %s", line, flag)
		}
	}

	if strings.Contains(line, "--no-ff") {
		t.Errorf("InteractiveRebaseArgs = %q, want no --no-ff: it would rewrite the untouched commits", line)
	}

	// The revision after `--`, like every revision this package passes.
	if want := "-- a2801ba"; !strings.HasSuffix(line, want) {
		t.Errorf("InteractiveRebaseArgs = %q, want it to end %q", line, want)
	}
}

// The two shapes an interactive rebase asks this program to take, and the
// shape an ordinary start must fall through untouched.
func TestRunAsEditorIsRecognisedByTheFlagAndTheCount(t *testing.T) {
	// Writing the todo list: two paths, and the work succeeds.
	handled, err := git.RunAsEditor(
		[]string{"yagit", "--write-rebase-todo", "/nowhere/plan", "/nowhere/todo"})
	if !handled {
		t.Fatal("RunAsEditor refused its own request to write a todo list")
	}
	if err == nil {
		t.Error("reading a plan that is not there succeeded")
	}

	// Refusing to edit a message: handled, and always an error, because being
	// asked at all means a plan reached git that no plan yagit writes ever is.
	handled, err = git.RunAsEditor([]string{"yagit", "--refuse-message-editor", "/nowhere/COMMIT_EDITMSG"})
	if !handled {
		t.Fatal("RunAsEditor refused to be the editor that refuses")
	}
	if err == nil {
		t.Fatal("RunAsEditor agreed to edit a commit message")
	}
	if !strings.Contains(err.Error(), "commit one unread") {
		t.Errorf("refusal = %q, want it to say why", err)
	}

	for name, args := range map[string][]string{
		"a daemon starting": {"yagit", "-root", "/src"},
		"no arguments":      {"yagit"},
		"one path missing":  {"yagit", "--write-rebase-todo", "/tmp/plan"},
		"a refusal with a path too many": {
			"yagit", "--refuse-message-editor", "/tmp/a", "/tmp/b"},
		"an argument too many": {
			"yagit", "--write-rebase-todo", "/tmp/plan", "/tmp/todo", "/tmp/extra"},
	} {
		if handled, _ := git.RunAsEditor(args); handled {
			t.Errorf("RunAsEditor read %s as a request to be an editor", name)
		}
	}
}
