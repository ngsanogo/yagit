package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// What a repository is in the middle of, read off the marker files git leaves.
//
// These build the directory rather than the operation. Producing a real
// stopped rebase takes a repository, a conflict and four commands, and would
// test git's ability to write `rebase-merge/msgnum` rather than this package's
// ability to read it — the integration test beside them does the real merge.
// What matters here is the ORDER the markers are tested in, which is the part
// no single-operation test can catch.

func stateDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestAnIdleRepositoryIsInTheMiddleOfNothing(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{"HEAD": "ref: refs/heads/main\n"}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.InProgress() {
		t.Fatalf("state = %+v, want nothing in progress", state)
	}
}

func TestEachMarkerNamesItsOperation(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		files map[string]string
		want  git.Operation
	}{
		{"merge", map[string]string{"MERGE_HEAD": "abc\n"}, git.OperationMerge},
		{"cherry-pick", map[string]string{"CHERRY_PICK_HEAD": "abc\n"}, git.OperationCherryPick},
		{"revert", map[string]string{"REVERT_HEAD": "abc\n"}, git.OperationRevert},
		{"bisect", map[string]string{"BISECT_LOG": "git bisect start\n"}, git.OperationBisect},
		{"rebase, merge backend", map[string]string{"rebase-merge/head-name": "refs/heads/main\n"}, git.OperationRebase},
		{"rebase, apply backend", map[string]string{"rebase-apply/next": "1\n"}, git.OperationRebase},
		{"am", map[string]string{"rebase-apply/applying": ""}, git.OperationApply},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state, err := git.ReadState(stateDir(t, testCase.files))
			if err != nil {
				t.Fatalf("ReadState: %v", err)
			}
			if state.Operation != testCase.want {
				t.Fatalf("Operation = %q, want %q", state.Operation, testCase.want)
			}
		})
	}
}

// An interactive rebase stopped on a conflict has BOTH a rebase directory and
// CHERRY_PICK_HEAD, because replaying a commit is how a rebase moves. Reading
// the second first would report a cherry-pick nobody started, and offer to
// continue one.
func TestARebaseStoppedOnAConflictIsARebaseAndNotACherryPick(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{
		"CHERRY_PICK_HEAD":         "abc\n",
		"rebase-merge/head-name":   "refs/heads/feature\n",
		"rebase-merge/msgnum":      "3\n",
		"rebase-merge/end":         "7\n",
		"rebase-merge/interactive": "",
	}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}

	if state.Operation != git.OperationRebase {
		t.Fatalf("Operation = %q, want rebase", state.Operation)
	}
	if state.Branch != "feature" {
		t.Fatalf("Branch = %q, want the short name", state.Branch)
	}
	if state.Step != 3 || state.Total != 7 {
		t.Fatalf("progress = %d of %d, want 3 of 7", state.Step, state.Total)
	}
}

// A rebase that cannot say where it is up to is still a rebase. Losing the
// banner over an unreadable counter costs the user the one sentence that
// explains their conflicted files.
func TestAnUnreadableCounterLeavesTheOperationIntact(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{
		"rebase-merge/head-name": "refs/heads/main\n",
		"rebase-merge/msgnum":    "not a number\n",
		"rebase-merge/end":       "7\n",
	}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}

	if state.Operation != git.OperationRebase {
		t.Fatalf("Operation = %q, want rebase", state.Operation)
	}
	if state.Step != 0 || state.Total != 0 {
		t.Fatalf("progress = %d of %d, want neither", state.Step, state.Total)
	}
}

// One counter without the other says nothing: "step 3 of 0" is worse than no
// numbers at all.
func TestHalfAProgressCountIsNoProgressCount(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{
		"rebase-merge/head-name": "refs/heads/main\n",
		"rebase-merge/msgnum":    "3\n",
	}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Step != 0 || state.Total != 0 {
		t.Fatalf("progress = %d of %d, want neither", state.Step, state.Total)
	}
}

// A detached rebase writes no head-name. That is a real state, not a failure.
func TestARebaseWithNoBranchNameIsStillARebase(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{"rebase-merge/msgnum": "1\n"}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationRebase || state.Branch != "" {
		t.Fatalf("state = %+v", state)
	}
}

// A head-name that is there and cannot be read is still a rebase.
//
// GET /status reads this and the panel polls it every two seconds, so an error
// here does not report a branch name — it empties the working directory on
// screen for as long as the condition lasts. The branch decorates a banner;
// the operation is what the banner is for.
func TestAnUnreadableHeadNameStillReportsTheRebase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a mode of 0000 does not stop a read on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode says")
	}

	dir := stateDir(t, map[string]string{
		"rebase-merge/head-name": "refs/heads/main\n",
		"rebase-merge/msgnum":    "2\n",
		"rebase-merge/end":       "5\n",
	})
	if err := os.Chmod(filepath.Join(dir, "rebase-merge", "head-name"), 0o000); err != nil {
		t.Fatalf("chmod head-name: %v", err)
	}

	state, err := git.ReadState(dir)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationRebase {
		t.Fatalf("operation = %q, want a rebase", state.Operation)
	}
	if state.Branch != "" {
		t.Fatalf("branch = %q, want it left unsaid", state.Branch)
	}
	// The rest of the answer survives: nothing about one unreadable file makes
	// the counters it did read any less true.
	if state.Step != 2 || state.Total != 5 {
		t.Fatalf("progress = %d of %d, want 2 of 5", state.Step, state.Total)
	}
}

func TestReadStateWithNoDirectoryIsAnError(t *testing.T) {
	if _, err := git.ReadState(""); err == nil {
		t.Fatal("ReadState(\"\") returned no error")
	}
}

// A sequence outlives the marker for the commit it stopped on.
//
// CHERRY_PICK_HEAD names the one commit being applied and git deletes it the
// moment that commit lands — including when it lands from yagit's own commit
// box, which is the ordinary way out of a conflict here. What is left is the
// todo list, and `git status` in a terminal goes on saying "Cherry-pick
// currently in progress". Reading only the marker made the banner vanish with
// commits still unapplied.
func TestASequenceIsStillInProgressAfterItsMarkerIsGone(t *testing.T) {
	for _, testCase := range []struct {
		name string
		todo string
		want git.Operation
	}{
		{"a cherry-pick", "pick 1a2b3c one\npick 4d5e6f two\n", git.OperationCherryPick},
		{"a revert", "revert 1a2b3c one\nrevert 4d5e6f two\n", git.OperationRevert},

		// git writes the long form and parses the short one.
		{"the short spelling", "p 1a2b3c one\n", git.OperationCherryPick},

		// The instruction is the first LINE THAT IS ONE. git writes comments
		// and blank lines into these files, and a reader that took line one
		// would find a `#` and report nothing in progress.
		{"past blank lines and comments", "\n# a comment\n\npick 1a2b3c one\n", git.OperationCherryPick},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			state, err := git.ReadState(stateDir(t, map[string]string{
				"HEAD":           "ref: refs/heads/main\n",
				"sequencer/todo": testCase.todo,
			}))
			if err != nil {
				t.Fatalf("ReadState: %v", err)
			}
			if state.Operation != testCase.want {
				t.Errorf("reports %q, want %q", state.Operation, testCase.want)
			}
		})
	}
}

// A sequencer directory git left behind with nothing in it is not an
// operation. Claiming one would put a banner over a repository that is idle,
// with buttons offering to abort something that is not happening.
func TestAnEmptyTodoListIsNotAnOperation(t *testing.T) {
	for _, todo := range []string{"", "\n\n", "# every line a comment\n", "noop\n"} {
		state, err := git.ReadState(stateDir(t, map[string]string{
			"HEAD":           "ref: refs/heads/main\n",
			"sequencer/todo": todo,
		}))
		if err != nil {
			t.Fatalf("ReadState: %v", err)
		}
		if state.InProgress() {
			t.Errorf("a todo list of %q reports %q in progress", todo, state.Operation)
		}
	}
}

// The marker wins over the todo list where both exist, because it names the
// commit and the list only names the operation. They agree in practice; the
// order is what makes that guaranteed rather than lucky.
func TestTheCommitMarkerIsPreferredOverTheTodoList(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{
		"HEAD":             "ref: refs/heads/main\n",
		"CHERRY_PICK_HEAD": "1a2b3c\n",
		"sequencer/todo":   "pick 1a2b3c one\n",
	}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationCherryPick {
		t.Errorf("reports %q, want a cherry-pick", state.Operation)
	}
}

// A merge started on top of a leftover sequence is a merge, which is what git
// says about it.
//
// The pair is ordinary rather than exotic: finishing a conflicted cherry-pick
// from yagit's commit box leaves `sequencer/todo` behind, git lets a merge
// start on top of it, and `git status` then prints "You have unmerged paths
// (use git merge --abort)". Reading the sequencer first drew Cherry-picking
// over a conflicted merge and offered an Abort that ran `git cherry-pick
// --abort` — a command aimed at the wrong operation, from a banner nobody
// could have known was lying.
func TestAMergeOnTopOfALeftoverSequenceIsAMerge(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{
		"HEAD":           "ref: refs/heads/main\n",
		"MERGE_HEAD":     "abc\n",
		"sequencer/todo": "pick 1a2b3c one\n",
	}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationMerge {
		t.Errorf("reports %q, want a merge — git's own order tests MERGE_HEAD first", state.Operation)
	}
}

// A rebase that will ask for a commit message is not continued from here.
//
// Continuing runs git with an editor that accepts whatever it is given, which
// is honest for a `pick` — the replayed commit keeps its own message — and a
// silent yes for anything that writes a NEW one. A `squash` would commit the
// two messages joined without showing them; a `reword` would leave the message
// exactly as it was, which is the one thing the user asked it not to do.
func TestARebaseThatWouldWriteAnUnreadMessageWillNotBeContinued(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{
			"a squash still to come",
			map[string]string{
				"rebase-merge/done":            "pick 1a2b3c one\n",
				"rebase-merge/git-rebase-todo": "squash 4d5e6f two\n",
			},
			"squash",
		},
		{
			// The conflict is inside the squash itself: git writes the
			// instruction to `done` and leaves the todo list empty, so a
			// reader that looked only at what was left would find nothing.
			"a squash it stopped inside",
			map[string]string{
				"rebase-merge/done":            "pick 1a2b3c one\nsquash 4d5e6f two\n",
				"rebase-merge/git-rebase-todo": "",
			},
			"squash",
		},
		{
			"a reword, in git's short spelling",
			map[string]string{
				"rebase-merge/done":            "pick 1a2b3c one\n",
				"rebase-merge/git-rebase-todo": "r 4d5e6f two\n",
			},
			"reword",
		},
		{
			// -c edits the combined message; -C takes it whole and opens
			// nothing. One letter, and the whole difference.
			"a fixup that edits the message",
			map[string]string{
				"rebase-merge/done":            "pick 1a2b3c one\n",
				"rebase-merge/git-rebase-todo": "fixup -c 4d5e6f two\n",
			},
			"fixup -c",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			files := map[string]string{"HEAD": "ref: refs/heads/main\n"}
			for name, content := range testCase.files {
				files[name] = content
			}

			state, err := git.ReadState(stateDir(t, files))
			if err != nil {
				t.Fatalf("ReadState: %v", err)
			}
			if state.Operation != git.OperationRebase {
				t.Fatalf("reports %q, want a rebase", state.Operation)
			}

			reason, blocked := state.Blocked[git.ActionContinue]
			if !blocked {
				t.Fatal("continuing is offered, and it would commit a message nobody has seen")
			}
			if !strings.Contains(reason, testCase.want) {
				t.Errorf("the reason is %q, and does not name the %s that caused it", reason, testCase.want)
			}

			// The same sentence reaches the route, which refuses rather than
			// trusting the screen to have drawn the button disabled.
			err = state.Refuse(git.ActionContinue)
			if !errors.Is(err, git.ErrActionBlocked) {
				t.Errorf("Refuse gave %v, want ErrActionBlocked", err)
			}
			if err != nil && !strings.Contains(err.Error(), reason) {
				t.Errorf("the refusal says %q, which is not what the button said", err)
			}

			// Only continuing. Calling the rebase off needs no editor, and a
			// user who cannot continue is exactly the one who wants out.
			for _, action := range []git.Action{git.ActionAbort, git.ActionSkip} {
				if err := state.Refuse(action); err != nil {
					t.Errorf("%s is blocked too: %v", action, err)
				}
			}
		})
	}
}

// Everything else blocks nothing, and that is the common case: a rebase of
// ordinary picks, and every operation that has no plan to read at all.
func TestAnOrdinaryOperationBlocksNothing(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		files map[string]string
	}{
		{"a rebase of picks", map[string]string{
			"rebase-merge/done":            "pick 1a2b3c one\n",
			"rebase-merge/git-rebase-todo": "pick 4d5e6f two\n",
		}},
		// -C takes the commit's message whole and opens no editor, so there is
		// nothing unread about it.
		{"a fixup that keeps a message as it stands", map[string]string{
			"rebase-merge/git-rebase-todo": "fixup -C 4d5e6f two\n",
		}},
		{"a rebase with an exec in it", map[string]string{
			"rebase-merge/git-rebase-todo": "exec make test\npick 4d5e6f two\n",
		}},
		{"an am", map[string]string{"rebase-apply/applying": ""}},
		{"a merge", map[string]string{"MERGE_HEAD": "abc\n"}},
		{"a cherry-pick", map[string]string{"CHERRY_PICK_HEAD": "abc\n"}},
		{"an idle repository", map[string]string{}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			files := map[string]string{"HEAD": "ref: refs/heads/main\n"}
			for name, content := range testCase.files {
				files[name] = content
			}

			state, err := git.ReadState(stateDir(t, files))
			if err != nil {
				t.Fatalf("ReadState: %v", err)
			}
			if len(state.Blocked) != 0 {
				t.Errorf("blocks %v, and nothing here needs an editor", state.Blocked)
			}
		})
	}
}

// A rebase reports as one even with a sequencer directory beside it. An
// interactive rebase keeps its todo list somewhere else entirely, and the
// rebase directory is tested first regardless — but a rebase reported as a
// cherry-pick would offer to continue the wrong thing.
func TestARebaseOutranksALeftoverSequence(t *testing.T) {
	state, err := git.ReadState(stateDir(t, map[string]string{
		"HEAD":                   "ref: refs/heads/main\n",
		"rebase-merge/head-name": "refs/heads/feature",
		"sequencer/todo":         "pick 1a2b3c one\n",
	}))
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if state.Operation != git.OperationRebase {
		t.Errorf("reports %q, want a rebase", state.Operation)
	}
	if state.Branch != "feature" {
		t.Errorf("the rebase is on %q, want feature", state.Branch)
	}
}
