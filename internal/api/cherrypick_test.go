package api_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// What the cherry-pick routes answer, and what they refuse.

type wireCherryPickPlan struct {
	Command string `json:"command"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Into    string `json:"into"`
	Outcome string `json:"outcome"`
}

func cherryPickBody(commit, into, outcome string) string {
	return fmt.Sprintf(`{"commit":%q,"into":%q,"outcome":%q}`, commit, into, outcome)
}

func cherryPickPlanFor(t *testing.T, handler http.Handler, id, commit string) wireCherryPickPlan {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick/plan",
		fmt.Sprintf(`{"commit":%q}`, commit))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireCherryPickPlan](t, response)
}

// openPickableRepository is main with a side branch that edited a different
// file, so picking side's tip onto main is a clean apply — and the daemon has
// an identity to commit under.
func openPickableRepository(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGitIn(t, path, "init", "-b", "main")
	configureIdentityIn(t, path)
	writeFileIn(t, path, "notes.md", "base\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "base")...)

	runGitIn(t, path, "switch", "-c", "side")
	writeFileIn(t, path, "other.md", "side only\n")
	runGitIn(t, path, "add", "--", "other.md")
	runGitIn(t, path, withIdentity("commit", "-m", "side only")...)

	runGitIn(t, path, "switch", "main")
	writeFileIn(t, path, "notes.md", "main\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "main edit")...)

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

func writeFileIn(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func shaOf(t *testing.T, path, reference string) string {
	t.Helper()
	output, err := git.NewRunner(nil).Run(context.Background(), path, "rev-parse", reference+"^{commit}")
	if err != nil {
		t.Fatalf("rev-parse %s: %v", reference, err)
	}
	return strings.TrimSpace(string(output))
}

func TestPlanningACherryPickAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	side := shaOf(t, path, "side")

	planned := cherryPickPlanFor(t, handler, id, side)
	wantCommand := "git cherry-pick --no-edit --no-ff -- " + side
	if planned.Command != wantCommand {
		t.Errorf("command = %q, want %q", planned.Command, wantCommand)
	}
	if planned.Outcome != "cherry-pick" {
		t.Errorf("outcome = %q, want cherry-pick", planned.Outcome)
	}
	if planned.Into != "main" {
		t.Errorf("into = %q, want main", planned.Into)
	}
	if planned.Commit != side {
		t.Errorf("commit = %s, want the full name", planned.Commit)
	}
	if planned.Subject != "side only" {
		t.Errorf("subject = %q, want the commit's", planned.Subject)
	}

	// Planned, not run.
	if got := shaOf(t, path, "HEAD"); got == side {
		t.Error("HEAD moved onto the picked commit; the plan must change nothing")
	}
}

func TestPlanningACherryPickFastForwardSaysWhatItIs(t *testing.T) {
	handler, id, path := openPickableRepository(t)

	runGitIn(t, path, "switch", "-c", "ahead")
	writeFileIn(t, path, "extra.md", "new\n")
	runGitIn(t, path, "add", "--", "extra.md")
	runGitIn(t, path, withIdentity("commit", "-m", "ahead")...)
	picked := shaOf(t, path, "HEAD")
	runGitIn(t, path, "switch", "main")

	planned := cherryPickPlanFor(t, handler, id, picked)
	if planned.Outcome != "fast-forward" {
		t.Errorf("outcome = %q, want fast-forward", planned.Outcome)
	}
	wantCommand := "git cherry-pick --no-edit --ff -- " + picked
	if planned.Command != wantCommand {
		t.Errorf("command = %q, want %q", planned.Command, wantCommand)
	}
}

func TestPlanningACherryPickAlreadyOnTheBranchHasNoCommand(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	head := shaOf(t, path, "HEAD")

	planned := cherryPickPlanFor(t, handler, id, head)
	if planned.Outcome != "up-to-date" {
		t.Errorf("outcome = %q, want up-to-date", planned.Outcome)
	}
	if planned.Command != "" {
		t.Errorf("command = %q, want empty: there is no no-op cherry-pick to show", planned.Command)
	}
}

func TestCherryPickAppliesTheCommit(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	side := shaOf(t, path, "side")
	before := shaOf(t, path, "HEAD")

	planned := cherryPickPlanFor(t, handler, id, side)
	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick",
		cherryPickBody(planned.Commit, planned.Into, planned.Outcome))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	head := shaOf(t, path, "HEAD")
	if head == before || head == side {
		t.Errorf("HEAD at %s, want a new commit (was %s, picked %s)", head, before, side)
	}
	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("answer HEAD = %+v, want main", refs.Head)
	}
}

func TestACherryPickThatHasBecomeSomethingElseIsRefused(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	side := shaOf(t, path, "side")

	planned := cherryPickPlanFor(t, handler, id, side)

	// Make the commit an ancestor before the run: bring side in.
	runGitIn(t, path, withIdentity("merge", "--no-ff", "--no-edit", "-m", "bring side", "--", "side")...)

	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick",
		cherryPickBody(planned.Commit, planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "no longer what the plan described") {
		t.Errorf("body = %s, want the plan-changed refusal", response.Body)
	}
}

func TestACherryPickAimedAtABranchHEADHasLeftIsRefused(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	side := shaOf(t, path, "side")

	planned := cherryPickPlanFor(t, handler, id, side)
	runGitIn(t, path, "switch", "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick",
		cherryPickBody(planned.Commit, planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "HEAD is not on the branch") {
		t.Errorf("body = %s, want the head-moved refusal", response.Body)
	}
}

func TestPlanningACherryPickOfAMergeIsRefused(t *testing.T) {
	handler, id, path := openPickableRepository(t)

	runGitIn(t, path, "switch", "-c", "elsewhere", "main")
	writeFileIn(t, path, "extra.md", "only here\n")
	runGitIn(t, path, "add", "--", "extra.md")
	runGitIn(t, path, withIdentity("commit", "-m", "elsewhere")...)
	runGitIn(t, path, "switch", "main")
	runGitIn(t, path, withIdentity("merge", "--no-ff", "--no-edit", "-m", "merge elsewhere", "--", "elsewhere")...)
	merge := shaOf(t, path, "HEAD")
	runGitIn(t, path, "switch", "-c", "standing", merge+"^1")

	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick/plan",
		fmt.Sprintf(`{"commit":%q}`, merge))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "mainline") {
		t.Errorf("body = %s, want the merge-commit refusal", response.Body)
	}
}

// The run route takes the plan back, and the commit in it is the full name the
// plan resolved. A short form of the same object is refused rather than
// resolved a second time: two spellings of what was approved are two
// definitions of it.
func TestACherryPickNamingTheCommitDifferentlyFromThePlanIsRefused(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	side := shaOf(t, path, "side")

	planned := cherryPickPlanFor(t, handler, id, side)

	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick",
		cherryPickBody(planned.Commit[:7], planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "read the plan again") {
		t.Errorf("body = %s, want the resolved-name refusal", response.Body)
	}
	if got := shaOf(t, path, "HEAD"); got == side {
		t.Error("HEAD moved; a refused cherry-pick must run nothing")
	}
}

// An outcome the daemon cannot name was never on any plan, so it is the
// request that is wrong — before anything is read from the repository.
func TestCherryPickingWithAnUnknownOutcomeIsRefused(t *testing.T) {
	handler, id, path := openPickableRepository(t)
	side := shaOf(t, path, "side")

	response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick",
		cherryPickBody(side, "main", "merge-commit"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "fast-forward") {
		t.Errorf("body = %s, want the three outcomes named", response.Body)
	}
}

// A revision git would read as an option never reaches git. The plan route
// answers 400 rather than 500: nothing was read from the repository to decide
// it, and reading it again would answer the same way.
func TestPlanningACherryPickOfARevisionGitWouldReadAsAnOptionIsRefused(t *testing.T) {
	handler, id, _ := openPickableRepository(t)

	for _, revision := range []string{"", "--output=/tmp/yagit-escaped"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/cherry-pick/plan",
			fmt.Sprintf(`{"commit":%q}`, revision))
		if response.Code != http.StatusBadRequest {
			t.Errorf("commit=%q: status = %d, want 400: %s", revision, response.Code, response.Body)
		}
	}
}
