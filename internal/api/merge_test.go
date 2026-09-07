package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// What the merge routes answer, and what they refuse.
//
// The fixture is main and side with one commit each past their fork, so the
// ordinary merge here is a merge commit — and a merge commit is made by the
// daemon, under an identity the daemon has to find. See configureIdentityIn.

// wireMergePlan is the plan as it reaches a browser.
type wireMergePlan struct {
	Command string `json:"command"`
	Branch  string `json:"branch"`
	Into    string `json:"into"`
	Outcome string `json:"outcome"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
}

// mergeBody is the request the interface sends: the branch it clicked, and the
// two facts the dialog showed.
func mergeBody(branch, into, outcome string) string {
	return fmt.Sprintf(`{"branch":%q,"into":%q,"outcome":%q}`, branch, into, outcome)
}

func TestPlanningAMergeAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"side"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[wireMergePlan](t, response)
	// main and side have each moved past the fork, so this cannot be a
	// fast-forward — and the command says so rather than leaving merge.ff to.
	want := wireMergePlan{
		Command: `git merge --no-ff --no-edit -m "Merge branch 'side' into main" -- refs/heads/side`,
		Branch:  "side",
		Into:    "main",
		Outcome: "merge-commit",
		Ahead:   1,
		Behind:  1,
	}
	if planned != want {
		t.Errorf("plan = %+v, want %+v", planned, want)
	}

	// Planned, not run: main is still where it was.
	refs := decode[wireRefs](t, get(t, handler, "/api/repos/"+id+"/refs"))
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("HEAD = %+v, want the plan to have changed nothing", refs.Head)
	}
}

func TestPlanningAFastForwardSaysWhatItIs(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	// A branch one commit ahead of main, with main holding nothing of its own
	// past the fork: the merge is the pointer moving.
	runGitIn(t, path, "switch", "-c", "ahead")
	commitEmpty(t, path, "ahead commit")
	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"ahead"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[wireMergePlan](t, response)
	want := wireMergePlan{
		Command: "git merge --ff-only -- refs/heads/ahead",
		Branch:  "ahead",
		Into:    "main",
		Outcome: "fast-forward",
		Ahead:   0,
		Behind:  1,
	}
	if planned != want {
		t.Errorf("plan = %+v, want %+v", planned, want)
	}
}

// A branch main already holds is not an error and not a merge either. The
// sentence in the dialog is the whole difference, so the outcome carries it.
func TestPlanningAMergeOfABranchAlreadyInSaysNothingIsComing(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "branch", "--", "trailing", "main~1")

	planned := decode[wireMergePlan](t,
		postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"trailing"}`))
	if planned.Outcome != "up-to-date" {
		t.Errorf("outcome = %q, want up-to-date", planned.Outcome)
	}
	if planned.Behind != 0 {
		t.Errorf("behind = %d, want nothing to bring in", planned.Behind)
	}
}

// The lease the flag cannot hold. "Merging changes nothing" is a sentence
// about two branches, and --ff-only does not refuse a fast-forward — so a
// branch that moved between the dialog and the click would be merged under a
// dialog that promised the repository would not move.
func TestAMergeThatHasBecomeSomethingElseIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "branch", "--", "trailing", "main~1")

	planned := decode[wireMergePlan](t,
		postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"trailing"}`))
	if planned.Outcome != "up-to-date" {
		t.Fatalf("outcome = %q, want the plan to promise that nothing happens", planned.Outcome)
	}

	// A second window, a terminal, a pull: trailing catches up and passes main.
	runGitIn(t, path, "switch", "trailing")
	commitEmpty(t, path, "trailing commit")
	runGitIn(t, path, "switch", "main")

	before := commitIn(t, path, "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody(planned.Branch, planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "no longer what the plan described") {
		t.Errorf("body = %s, want it to say the plan is out of date", response.Body)
	}
	if now := commitIn(t, path, "main"); now != before {
		t.Errorf("main moved to %s, want the refusal to have merged nothing", now)
	}
}

// The same lease the other way: the dialog said a merge commit, and by the
// time the button was pressed the branch had become a fast-forward. Two
// different things happen to the history, and only one of them was approved.
func TestAMergeCommitThatBecameAFastForwardIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	planned := decode[wireMergePlan](t,
		postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"side"}`))
	if planned.Outcome != "merge-commit" {
		t.Fatalf("outcome = %q, want a merge commit to be what was approved", planned.Outcome)
	}

	// main is reset back onto side's line, so nothing of its own is left.
	runGitIn(t, path, "reset", "--hard", "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody(planned.Branch, planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "up-to-date") {
		t.Errorf("body = %s, want it to name what the merge would be now", response.Body)
	}
}

// A destination with a newline on it is the branch it names. Comparing it raw
// answers "HEAD is now on main, not main", which reads as a contradiction over
// a repository that never moved.
func TestAMergeDestinationIsTrimmedLikeEveryOtherName(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("side", "main\n", "merge-commit"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
}

// Two histories that were never one. The counts read exactly like a wide
// divergence — each side reports everything it has — so the plan has to ask
// git for the fork point rather than promise a merge commit it cannot make.
func TestPlanningAMergeOfUnrelatedHistoriesIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "checkout", "--orphan", "orphan")
	commitEmpty(t, path, "started separately")
	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"orphan"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "no common ancestor") {
		t.Errorf("body = %s, want it to name why git will not do this", response.Body)
	}
}

// A repository in the middle of something. git refuses a merge on top of a
// stopped one, and it refuses it AFTER a dialog has promised the merge — so
// both routes read the state and say what has to be finished first.
func TestMergingOnTopOfAnUnfinishedOperationIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	// A merge that stops on a conflict, left where it stopped.
	writeInRepo(t, path, "notes.md", "main version\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "main change")...)
	runGitIn(t, path, "switch", "side")
	writeInRepo(t, path, "notes.md", "side version\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "side change")...)
	runGitIn(t, path, "switch", "main")

	if response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("side", "main", "merge-commit")); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want the conflict that leaves the merge open: %s",
			response.Code, response.Body)
	}

	for route, body := range map[string]string{
		"/merge/plan": `{"branch":"side"}`,
		"/merge":      mergeBody("side", "main", "merge-commit"),
	} {
		response := postJSON(t, handler, "/api/repos/"+id+route, body)
		if response.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409: %s", route, response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "in the middle of") {
			t.Errorf("%s: body = %s, want it to name the operation to finish first",
				route, response.Body)
		}
	}
}

func TestMergingABranchFastForwardAnswersWithHEADMoved(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "-c", "ahead")
	commitEmpty(t, path, "ahead commit")
	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("ahead", "main", "fast-forward"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("HEAD = %+v, want it still on main after the fast-forward", refs.Head)
	}
	if want := commitIn(t, path, "ahead"); refs.Head.SHA != want {
		t.Errorf("HEAD at %s, want the tip of ahead %s", refs.Head.SHA, want)
	}
}

// The route's other half: git commits here, which is where the daemon's own
// environment starts to matter.
func TestMergingDivergedBranchesRecordsAMergeCommit(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	before := commitIn(t, path, "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("side", "main", "merge-commit"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Fatalf("HEAD = %+v, want it still on main", refs.Head)
	}
	if refs.Head.SHA == before {
		t.Fatal("main did not move, want the merge commit on top of it")
	}
	if parents := parentsIn(t, path, refs.Head.SHA); len(parents) != 2 {
		t.Errorf("HEAD has %d parents, want the two a merge commit has", len(parents))
	}
}

func TestMergingTheCurrentBranchIsRefused(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	for _, route := range []string{"/merge", "/merge/plan"} {
		body := `{"branch":"main"}`
		if route == "/merge" {
			body = mergeBody("main", "main", "fast-forward")
		}

		response := postJSON(t, handler, "/api/repos/"+id+route, body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "merging into itself does nothing") {
			t.Errorf("%s: body = %s, want the refusal to name merging into itself", route, response.Body)
		}
	}
}

// The window this guard exists for: the dialog says "into main", and HEAD is
// somewhere else by the time the button is pressed.
func TestMergingIntoABranchHEADHasLeftIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	planned := decode[wireMergePlan](t,
		postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"side"}`))

	// A second window, a terminal, the sidebar behind the dialog.
	runGitIn(t, path, "switch", "-c", "release")
	before := commitIn(t, path, "release")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody(planned.Branch, planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "HEAD is now on release") {
		t.Errorf("body = %s, want it to name where HEAD actually is", response.Body)
	}
	if now := commitIn(t, path, "release"); now != before {
		t.Errorf("release moved to %s, want the refusal to have merged nothing", now)
	}
}

// The same window, landed on the branch being merged. "HEAD moved" is the
// answer here rather than "merging into itself does nothing": one names what
// changed under the dialog, the other describes a request nobody made.
func TestAMergeWhoseDestinationBecameItsOwnBranchSaysHEADMoved(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("side", "main", "merge-commit"))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "HEAD is now on side") {
		t.Errorf("body = %s, want it to name where HEAD actually is", response.Body)
	}
}

// An empty destination is not a wildcard: a client that names none is one that
// did not read the plan.
func TestMergingWithoutNamingWhereItGoesIsRefused(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("side", "", "merge-commit"))
	if response.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

func TestMergingWithAConflictIsGitsRefusal(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	// Diverge main and side on the same file.
	writeInRepo(t, path, "notes.md", "main version\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "main change")...)

	runGitIn(t, path, "switch", "side")
	writeInRepo(t, path, "notes.md", "side version\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "side change")...)

	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody("side", "main", "merge-commit"))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "CONFLICT") {
		t.Errorf("body = %s, want git's own account of the conflict", response.Body)
	}
}

// The outcome chooses between two different commands, so a value the daemon
// cannot name is refused rather than read as the safer one.
func TestMergeRefusesAnOutcomeItCannotName(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	for _, outcome := range []string{"", "ff", "rebase"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/merge",
			mergeBody("side", "main", outcome))
		if response.Code != http.StatusBadRequest {
			t.Errorf("outcome %q: status = %d, want 400: %s", outcome, response.Code, response.Body)
		}
	}
}

// A branch name that is only whitespace reaches git as nothing at all. It is
// the request that is wrong, so it is a 400 rather than the 500 an
// unrecognised error would answer with.
func TestMergeRoutesRefuseAnEmptyBranchName(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	for route, body := range map[string]string{
		"/merge/plan": `{"branch":"  "}`,
		"/merge":      mergeBody("  ", "main", "fast-forward"),
	} {
		response := postJSON(t, handler, "/api/repos/"+id+route, body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "no branch name given") {
			t.Errorf("%s: body = %s, want it to name what is missing", route, response.Body)
		}
	}
}

// A detached HEAD has no branch to merge into. The state is the refusal, not
// the request, which is what makes it a 409.
func TestMergingWithADetachedHEADIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "--detach", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge/plan", `{"branch":"side"}`)
	if response.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

func TestMergeRoutesRefuseABodyTheyDoNotUnderstand(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	for _, route := range []string{"/merge", "/merge/plan"} {
		response := postJSON(t, handler, "/api/repos/"+id+route, `{"nonsense":true}`)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
	}
}

// The plan answers what the run route is later checked against, so it must not
// take those fields itself: a client that could name its own destination would
// be describing a merge it had already decided.
func TestThePlanRouteTakesNothingButTheBranch(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/merge/plan",
		mergeBody("side", "main", "fast-forward"))
	if response.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

// commitIn resolves a reference to the commit it points at.
func commitIn(t *testing.T, path, reference string) string {
	t.Helper()

	output, err := git.NewRunner(nil).Run(context.Background(), path, "rev-parse", reference+"^{commit}")
	if err != nil {
		t.Fatalf("rev-parse %s: %v", reference, err)
	}
	return strings.TrimSpace(string(output))
}

// parentsIn is how a fast-forward is told from a merge commit: one parent
// against two.
func parentsIn(t *testing.T, path, sha string) []string {
	t.Helper()

	output, err := git.NewRunner(nil).Run(context.Background(), path, "rev-list", "-1", "--parents", sha)
	if err != nil {
		t.Fatalf("rev-list --parents %s: %v", sha, err)
	}
	return strings.Fields(strings.TrimSpace(string(output)))[1:]
}

func TestMergePlanPrefersAMergeCommitOverAFastForward(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	// A branch one commit ahead of main, with main holding nothing of its own
	// past the fork: the natural reading is a fast-forward.
	runGitIn(t, path, "switch", "-c", "ahead")
	commitEmpty(t, path, "ahead commit")
	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/merge/plan",
		`{"branch":"ahead","merge_commit":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	planned := decode[wireMergePlan](t, response)
	if planned.Outcome != "merge-commit" {
		t.Fatalf("outcome = %q, want merge-commit", planned.Outcome)
	}
	if !strings.Contains(planned.Command, "--no-ff") {
		t.Errorf("command = %q, want --no-ff", planned.Command)
	}

	run := postJSON(t, handler, "/api/repos/"+id+"/merge",
		mergeBody(planned.Branch, planned.Into, planned.Outcome))
	if run.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", run.Code, run.Body)
	}
}
