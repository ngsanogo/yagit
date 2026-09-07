package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

// The two routes that finish, or call off, what a repository is in the middle
// of.
//
// The plan half is a description and refuses almost nothing. The run half
// destroys work, so what is tested hardest here is what it REFUSES: an
// instruction aimed at an operation the repository has already left must not
// be carried out against whatever it is in the middle of now.

// wireOperationPlan mirrors what POST /operation/plan sends.
type wireOperationPlan struct {
	Command   string `json:"command"`
	Operation string `json:"operation"`
	Identity  string `json:"identity"`
	Action    string `json:"action"`
	Destroys  bool   `json:"destroys"`
}

// wireStatusState is the status, read for its state alone.
type wireStatusState struct {
	State struct {
		Operation string `json:"operation"`
		Identity  string `json:"identity"`
		Branch    string `json:"branch"`
	} `json:"state"`
}

// wireStatusActions is the status, read for the buttons and the reasons one of
// them cannot be pressed.
type wireStatusActions struct {
	State struct {
		Actions []string          `json:"actions"`
		Blocked map[string]string `json:"blocked"`
	} `json:"state"`
}

// openDiverged opens a repository where main and side changed the same line of
// f.txt, which is one command away from any of the stopped states below.
func openDiverged(t *testing.T) (handler http.Handler, id, path string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path = filepath.Join(root, "project")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGitIn(t, path, "init", "-b", "main")
	// In the repository's own configuration: these operations commit for
	// themselves, and the daemon runs git without the -c identity the tests
	// pass by hand.
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")

	write := func(content string) { writeInRepo(t, path, "f.txt", content) }

	write("one\ntwo\nthree\n")
	runGitIn(t, path, "add", "--", "f.txt")
	runGitIn(t, path, withIdentity("commit", "-m", "base")...)

	runGitIn(t, path, "checkout", "-b", "side")
	write("one\nSIDE\nthree\n")
	runGitIn(t, path, withIdentity("commit", "-am", "side")...)

	runGitIn(t, path, "checkout", "main")
	write("one\nMAIN\nthree\n")
	runGitIn(t, path, withIdentity("commit", "-am", "main")...)

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

// stopOn runs a command that is expected to conflict. It cannot go through
// runGitIn, which fails the test on a non-zero exit, and a non-zero exit is
// the state every fixture here is built to be in.
func stopOn(t *testing.T, path string, args ...string) {
	t.Helper()
	if _, err := git.NewRunner(nil).Run(context.Background(), path, withIdentity(args...)...); err == nil {
		t.Fatalf("git %v succeeded, so there is nothing in progress to act on", args)
	}
}

// openStoppedMerge opens a repository whose merge of side into main stopped on
// f.txt.
func openStoppedMerge(t *testing.T) (handler http.Handler, id, path string) {
	t.Helper()

	handler, id, path = openDiverged(t)
	stopOn(t, path, "merge", "side")
	return handler, id, path
}

func operationStateOf(t *testing.T, handler http.Handler, id string) string {
	t.Helper()
	return decode[wireStatusState](t, get(t, handler, "/api/repos/"+id+"/status")).State.Operation
}

// operationLease is the operation and identity a run must echo, read the same
// way the confirmation does — from a plan.
func operationLease(t *testing.T, handler http.Handler, id, action string) (operation, identity string) {
	t.Helper()
	response := postJSON(t, handler, "/api/repos/"+id+"/operation/plan",
		fmt.Sprintf(`{"action": %q}`, action))
	if response.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", response.Code, response.Body)
	}
	plan := decode[wireOperationPlan](t, response)
	if plan.Identity == "" {
		t.Fatal("the plan named no identity; the run lease would never hold")
	}
	return plan.Operation, plan.Identity
}

func operationBody(action, operation, identity string) string {
	return fmt.Sprintf(`{"action": %q, "operation": %q, "identity": %q}`,
		action, operation, identity)
}

// The plan says the exact line, from the state the daemon reads for itself.
// The client never names what to abort — only which of the three things to do.
func TestThePlanNamesTheCommandFromTheStateTheDaemonReads(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/operation/plan", `{"action": "abort"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	plan := decode[wireOperationPlan](t, response)
	if plan.Command != "git merge --abort" {
		t.Errorf("the plan runs %q, want git merge --abort", plan.Command)
	}
	if plan.Operation != "merge" {
		t.Errorf("the plan describes a %q, want a merge", plan.Operation)
	}
	if plan.Identity == "" {
		t.Error("the plan named no identity, so a later Abort cannot tell two merges apart")
	}
	if !plan.Destroys {
		t.Error("aborting is not marked destructive, so the interface would run it without asking")
	}
}

func TestAbortingEndsTheOperationAndAnswersWithTheWorkingDirectory(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)
	operation, identity := operationLease(t, handler, id, "abort")

	response := postJSON(t, handler, "/api/repos/"+id+"/operation",
		operationBody("abort", operation, identity))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	// The answer is the status, because the banner is drawn from it and has to
	// stop saying "Merging" in the same frame the button comes back.
	if state := decode[wireStatusState](t, response).State.Operation; state != "" {
		t.Errorf("the answer still reports %q in progress", state)
	}
	if state := operationStateOf(t, handler, id); state != "" {
		t.Errorf("the repository is still in the middle of %q", state)
	}
}

// The guard that makes the confirmation's promise true.
//
// The banner is drawn from a status read up to two seconds ago. If the merge
// ended and something else started in between, a click on the Abort that is
// still on screen must not abort the something else — the dialog said `git
// merge --abort` and the user read it.
func TestAnInstructionAimedAtAnOperationThatHasPassedIsRefused(t *testing.T) {
	handler, id, path := openStoppedMerge(t)
	_, identity := operationLease(t, handler, id, "abort")

	// The merge ends somewhere else — a terminal — exactly as it would in
	// life.
	runGitIn(t, path, "merge", "--abort")

	response := postJSON(t, handler, "/api/repos/"+id+"/operation",
		operationBody("abort", "merge", identity))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	// The refusal has to say what happened, because the screen still shows a
	// banner that disagrees with it.
	if body := response.Body.String(); !strings.Contains(body, "finished") {
		t.Errorf("the refusal does not explain itself: %s", body)
	}
}

// Naming a different operation is refused, and the message names both.
func TestAnInstructionNamingTheWrongOperationIsRefused(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)
	_, identity := operationLease(t, handler, id, "abort")

	response := postJSON(t, handler, "/api/repos/"+id+"/operation",
		operationBody("abort", "rebase", identity))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	body := response.Body.String()
	if !strings.Contains(body, "merge") || !strings.Contains(body, "rebase") {
		t.Errorf("the refusal names neither what was asked nor what is true: %s", body)
	}

	// And it changed nothing.
	if state := operationStateOf(t, handler, id); state != "merge" {
		t.Errorf("the merge is now %q; a refused request must run no command", state)
	}
}

// Same kind, different instance: a finished rebase must not be aborted as the
// next one that started while the dialog sat open.
func TestAnInstructionAimedAtADifferentInstanceIsRefused(t *testing.T) {
	handler, id, path := openStoppedMerge(t)
	_, firstIdentity := operationLease(t, handler, id, "abort")

	runGitIn(t, path, "merge", "--abort")
	// mtime is part of the identity; without a gap two merges of the same tip
	// can share a nanosecond on coarse filesystems.
	time.Sleep(20 * time.Millisecond)
	stopOn(t, path, "merge", "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/operation",
		operationBody("abort", "merge", firstIdentity))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if body := response.Body.String(); !strings.Contains(body, "another started") {
		t.Errorf("the refusal does not name a new instance: %s", body)
	}
	if state := operationStateOf(t, handler, id); state != "merge" {
		t.Errorf("the new merge is now %q; a refused request must run no command", state)
	}
}

// Naming nothing is not a wildcard. A client that did not look must not be
// allowed to abort whatever happens to be running.
func TestAnInstructionNamingNoOperationIsRefused(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/operation", `{"action": "abort"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if state := operationStateOf(t, handler, id); state != "merge" {
		t.Errorf("the merge is now %q; a refused request must run no command", state)
	}
}

// A merge takes no --skip, and git has no such command to be handed one.
func TestAnInstructionAnOperationDoesNotTakeIsRefusedAsABadRequest(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)
	operation, identity := operationLease(t, handler, id, "abort")

	for _, action := range []string{"skip", "continue"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/operation",
			operationBody(action, operation, identity))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d: %s", action, response.Code, response.Body)
		}
		if state := operationStateOf(t, handler, id); state != "merge" {
			t.Errorf("%s: the merge is now %q; a refused request must run no command", action, state)
		}
	}
}

// An action outside the three never becomes a flag. Every command the daemon
// builds is `--` and this string, so `exec` would be `git rebase --exec`,
// which takes a shell command as its argument.
func TestAnUnknownActionIsRefusedByTheRoute(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)
	operation, identity := operationLease(t, handler, id, "abort")

	for _, action := range []string{"exec", "onto", "--abort", ""} {
		response := postJSON(t, handler, "/api/repos/"+id+"/operation",
			operationBody(action, operation, identity))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d: %s", action, response.Code, response.Body)
		}
	}
}

// Nothing in progress means nothing to instruct, and the interface tells that
// from a git command that refused.
func TestARepositoryInTheMiddleOfNothingRefusesEveryInstruction(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	for _, target := range []string{"/operation", "/operation/plan"} {
		response := postJSON(t, handler, "/api/repos/"+id+target,
			`{"action": "abort", "operation": "merge"}`)
		if response.Code != http.StatusConflict {
			t.Errorf("%s: status = %d: %s", target, response.Code, response.Body)
		}
	}
}

// A rebase yagit will not finish is refused by the route, not only greyed out
// on screen.
//
// Continuing runs git with an editor that accepts whatever it is handed, which
// is honest for a `pick` — the replayed commit keeps its own message — and a
// silent yes for a `squash`, which would commit two messages joined without
// showing them. The button is drawn disabled from the same reading, and this
// is what makes that more than a suggestion: the screen is up to two seconds
// old, and a client is not the thing that decides.
func TestARebaseThatWouldWriteAnUnreadMessageIsRefusedByTheRoute(t *testing.T) {
	handler, id, path := openDiverged(t)
	runGitIn(t, path, "checkout", "side")
	stopOn(t, path, "rebase", "main")

	// What `git rebase -i` with a squash in it leaves behind. Written rather
	// than produced by an editor: the sequence editor is a shell command, and
	// the one that would rewrite a line is a different program on each of the
	// three platforms the suite runs on.
	todo := filepath.Join(path, ".git", "rebase-merge", "git-rebase-todo")
	if err := os.WriteFile(todo, []byte("squash 1a2b3c4 another commit\n"), 0o644); err != nil {
		t.Fatalf("writing the todo list: %v", err)
	}

	_, identity := operationLease(t, handler, id, "abort")

	response := postJSON(t, handler, "/api/repos/"+id+"/operation",
		operationBody("continue", "rebase", identity))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	// The same sentence the disabled button carries. A refusal the screen
	// cannot echo is one the user has to guess at.
	if body := response.Body.String(); !strings.Contains(body, "squash") {
		t.Errorf("the refusal does not name what stopped it: %s", body)
	}

	// And nothing ran: the rebase is exactly where it was.
	if state := operationStateOf(t, handler, id); state != "rebase" {
		t.Errorf("the rebase is now %q; a refused request must run no command", state)
	}
}

// The status carries the reason with the buttons, so the interface can draw
// one refused without inventing a sentence for it.
func TestTheStatusCarriesWhyAnActionIsRefused(t *testing.T) {
	handler, id, path := openDiverged(t)
	runGitIn(t, path, "checkout", "side")
	stopOn(t, path, "rebase", "main")

	todo := filepath.Join(path, ".git", "rebase-merge", "git-rebase-todo")
	if err := os.WriteFile(todo, []byte("reword 1a2b3c4 another commit\n"), 0o644); err != nil {
		t.Fatalf("writing the todo list: %v", err)
	}

	payload := decode[wireStatusActions](t, get(t, handler, "/api/repos/"+id+"/status"))

	// Still offered, and refused: a button that vanished would take its
	// explanation with it.
	if !slices.Contains(payload.State.Actions, "continue") {
		t.Errorf("continue is not among %v, so nothing on screen can say why it is refused", payload.State.Actions)
	}
	if reason := payload.State.Blocked["continue"]; !strings.Contains(reason, "reword") {
		t.Errorf("the status gives %q as the reason, which does not name the reword", reason)
	}
}
