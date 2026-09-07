package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type wireRebasePlan struct {
	Command    string `json:"command"`
	Onto       string `json:"onto"`
	From       string `json:"from"`
	Outcome    string `json:"outcome"`
	Rewriting  int    `json:"rewriting"`
	Flattening int    `json:"flattening"`
	Behind     int    `json:"behind"`
}

func rebaseBody(onto, from, outcome string) string {
	return fmt.Sprintf(`{"onto":%q,"from":%q,"outcome":%q}`, onto, from, outcome)
}

func rebasePlanFor(t *testing.T, handler http.Handler, id, onto string) wireRebasePlan {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase/plan", fmt.Sprintf(`{"onto":%q}`, onto))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireRebasePlan](t, response)
}

// The command is the whole of what the confirmation promises, so it is
// compared character for character — the six flags that stop configuration
// deciding what a rebase means, and --no-ff, which is the lease on the replay.
func TestPlanningARebaseAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "side")

	planned := rebasePlanFor(t, handler, id, "main")
	want := wireRebasePlan{
		Command: "git rebase --merge --no-autosquash --no-autostash " +
			"--no-rebase-merges --no-update-refs --no-ff -- refs/heads/main",
		Onto:      "main",
		From:      "side",
		Outcome:   "rebase",
		Rewriting: 1,
		Behind:    1,
	}
	if planned != want {
		t.Errorf("plan = %+v, want %+v", planned, want)
	}
}

func TestPlanningARebaseAlreadyUpToDateSaysSo(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "-c", "at-main")

	planned := rebasePlanFor(t, handler, id, "main")
	if planned.Outcome != "up-to-date" || planned.Rewriting != 0 || planned.Behind != 0 {
		t.Errorf("plan = %+v, want up-to-date with nothing to replay", planned)
	}
	// No lease on an outcome that writes nothing: --no-ff here would mean
	// "rebase forced", which rewrites the branch the sentence said would not
	// move.
	if strings.Contains(planned.Command, "--no-ff") {
		t.Errorf("command = %q, want no --no-ff on an up-to-date rebase", planned.Command)
	}
}

// The branch is AHEAD of what it would be rebased onto, which a one-directional
// count read as commits to replay. git answers "Current branch is up to date"
// and writes nothing, and so does the plan.
func TestPlanningARebaseOntoAnAncestorSaysUpToDate(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "-c", "ahead")
	commitEmpty(t, path, "one")
	commitEmpty(t, path, "two")

	planned := rebasePlanFor(t, handler, id, "main")
	if planned.Outcome != "up-to-date" {
		t.Fatalf("plan = %+v, want up-to-date: ahead already contains main", planned)
	}
	if planned.Rewriting != 0 || planned.Behind != 0 {
		t.Errorf("plan = %+v, want nothing to replay and nowhere to move", planned)
	}

	// And the route agrees: the rebase runs, and the branch does not move.
	before := commitIn(t, path, "ahead")
	response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
		rebaseBody(planned.Onto, planned.From, planned.Outcome))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	if now := commitIn(t, path, "ahead"); now != before {
		t.Errorf("ahead moved to %s, want an up-to-date rebase to write nothing", now)
	}
}

// The branch has nothing of its own, which the same count read as "changes
// nothing" — while git moves the branch and rewrites the work tree under it.
func TestPlanningARebaseOfABranchWithNothingOfItsOwnSaysFastForward(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "-c", "behind", "main~1")

	planned := rebasePlanFor(t, handler, id, "main")
	if planned.Outcome != "fast-forward" {
		t.Fatalf("plan = %+v, want fast-forward: git moves this branch", planned)
	}
	if planned.Rewriting != 0 || planned.Behind != 1 {
		t.Errorf("plan = %+v, want nothing replayed and one commit to move over", planned)
	}
	if strings.Contains(planned.Command, "--no-ff") {
		t.Errorf("command = %q, want no --no-ff where nothing is replayed", planned.Command)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
		rebaseBody(planned.Onto, planned.From, planned.Outcome))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	if got, want := commitIn(t, path, "behind"), commitIn(t, path, "main"); got != want {
		t.Errorf("behind = %s, want main's tip %s", got, want)
	}
}

func TestRebasingOntoAnotherBranchAnswersWithHEADMoved(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	runGitIn(t, path, "switch", "side")
	before := commitIn(t, path, "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
		rebaseBody("main", "side", "rebase"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "side" {
		t.Errorf("HEAD = %+v, want it still on side after the rebase", refs.Head)
	}
	if refs.Head.SHA == before {
		t.Fatal("side did not move, want the replayed commit")
	}
}

// The lease the argument list cannot hold. The plan said a replay; by the time
// the button is pressed the branch has nothing left to replay, so the sentence
// the user approved is false and the rebase is refused rather than run.
func TestARebaseThatBecameADifferentOperationIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	runGitIn(t, path, "switch", "side")
	planned := rebasePlanFor(t, handler, id, "main")

	// A second window, a terminal, the sidebar behind the dialog: side is
	// reset onto main and now holds nothing of its own.
	runGitIn(t, path, "reset", "--hard", "main")
	before := commitIn(t, path, "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
		rebaseBody(planned.Onto, planned.From, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "no longer what the plan described") {
		t.Errorf("body = %s, want the plan having gone stale", response.Body)
	}
	if now := commitIn(t, path, "side"); now != before {
		t.Errorf("side moved to %s, want the refusal to have rebased nothing", now)
	}
}

func TestRebasingWithAnOutcomeTheDaemonCannotNameIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "side")

	for _, outcome := range []string{"", "merge-commit", "squash"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
			rebaseBody("main", "side", outcome))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400: %s", outcome, response.Code, response.Body)
		}
	}
}

func TestRebasingOntoSelfIsRefused(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	for _, route := range []string{"/rebase", "/rebase/plan"} {
		body := `{"onto":"main"}`
		if route == "/rebase" {
			body = rebaseBody("main", "main", "up-to-date")
		}

		response := postJSON(t, handler, "/api/repos/"+id+route, body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "rebasing onto itself does nothing") {
			t.Errorf("%s: body = %s, want the refusal to name rebasing onto itself", route, response.Body)
		}
	}
}

func TestRebasingWhileDetachedIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "--detach", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase/plan", `{"onto":"side"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

func TestRebasingFromABranchHEADHasLeftIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)

	runGitIn(t, path, "switch", "side")
	planned := rebasePlanFor(t, handler, id, "main")

	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
		rebaseBody(planned.Onto, planned.From, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "HEAD is not on the branch") {
		t.Errorf("body = %s, want HEAD having moved", response.Body)
	}
}

func TestRebasingOnTopOfAnUnfinishedOperationIsRefused(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

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
		"/rebase/plan": `{"onto":"side"}`,
		"/rebase":      rebaseBody("side", "main", "rebase"),
	} {
		response := postJSON(t, handler, "/api/repos/"+id+route, body)
		if response.Code != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409: %s", route, response.Code, response.Body)
		}
	}
}

// The arrangement merge and rebase disagree on. Merge would call this
// up-to-date — main is an ancestor — and a rebase flattens it, because git
// has no shortcut across a merge commit. If the route ever used merge's
// outcomeOf, every other API test would still pass.
func TestPlanningARebaseOfAnAheadBranchThatHoldsAMergeSaysReplay(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	runGitIn(t, path, "switch", "-c", "feature")
	commitEmpty(t, path, "on feature")
	runGitIn(t, path, "switch", "-c", "topic")
	commitEmpty(t, path, "on topic")
	runGitIn(t, path, "switch", "feature")
	runGitIn(t, path, "merge", "--no-ff", "--no-edit", "--", "refs/heads/topic")

	planned := rebasePlanFor(t, handler, id, "main")
	if planned.Outcome != "rebase" {
		t.Fatalf("plan = %+v, want rebase: git flattens a branch that holds a merge", planned)
	}
	if planned.Behind != 0 {
		t.Errorf("behind = %d, want none: main is an ancestor of feature", planned.Behind)
	}
	if planned.Flattening < 1 {
		t.Errorf("flattening = %d, want the merge commit named as discarded", planned.Flattening)
	}
	if !strings.Contains(planned.Command, "--no-ff") {
		t.Errorf("command = %q, want --no-ff on a replay", planned.Command)
	}
}

func TestRebasingWithAConflictIsGitsRefusalAndLeavesTheRebaseOpen(t *testing.T) {
	handler, id, path := openSwitchableRepository(t)
	configureIdentityIn(t, path)

	writeInRepo(t, path, "notes.md", "main version\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "main change")...)
	runGitIn(t, path, "switch", "side")
	writeInRepo(t, path, "notes.md", "side version\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "side change")...)

	response := postJSON(t, handler, "/api/repos/"+id+"/rebase",
		rebaseBody("main", "side", "rebase"))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body)
	}

	// The file, by name. A rebase splits its account across both streams —
	// "CONFLICT (content): Merge conflict in notes.md" on stdout, "error:
	// could not apply …" and the hints on stderr — so a message assembled
	// from stderr alone is progress noise and advice with nothing in it that
	// says WHAT conflicted. Asserting on the word "conflict" alone does not
	// catch that: git's own hint text says "Resolve all conflicts manually",
	// so the assertion passed on a message that named no file at all.
	body := response.Body.String()
	if !strings.Contains(body, "notes.md") {
		t.Errorf("body = %s, want the conflicted file named in it", body)
	}
	if !strings.Contains(strings.ToLower(body), "conflict") {
		t.Errorf("body = %s, want git's own account of the conflict", body)
	}

	if operationStateOf(t, handler, id) != "rebase" {
		t.Errorf("operation = %q, want rebase left open", operationStateOf(t, handler, id))
	}
}
