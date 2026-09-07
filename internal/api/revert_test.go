package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the revert routes answer, and what they refuse.

type wireRevertPlan struct {
	Command string `json:"command"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Into    string `json:"into"`
	Outcome string `json:"outcome"`
}

func revertBody(commit, into, outcome string) string {
	return fmt.Sprintf(`{"commit":%q,"into":%q,"outcome":%q}`, commit, into, outcome)
}

func revertPlanFor(t *testing.T, handler http.Handler, id, commit string) wireRevertPlan {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/revert/plan",
		fmt.Sprintf(`{"commit":%q}`, commit))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireRevertPlan](t, response)
}

// openRevertableRepository is a linear history on main: base → one → two.
// Reverting "one" is a clean apply — and the daemon has an identity to commit
// under.
func openRevertableRepository(t *testing.T) (http.Handler, string, string) {
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

	writeFileIn(t, path, "notes.md", "one\n")
	runGitIn(t, path, "add", "--", "notes.md")
	runGitIn(t, path, withIdentity("commit", "-m", "one")...)

	writeFileIn(t, path, "extra.md", "two\n")
	runGitIn(t, path, "add", "--", "extra.md")
	runGitIn(t, path, withIdentity("commit", "-m", "two")...)

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

func TestPlanningARevertAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, path := openRevertableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := revertPlanFor(t, handler, id, one)
	wantCommand := "git revert --no-edit --no-reference -- " + one
	if planned.Command != wantCommand {
		t.Errorf("command = %q, want %q", planned.Command, wantCommand)
	}
	if planned.Outcome != "revert" {
		t.Errorf("outcome = %q, want revert", planned.Outcome)
	}
	if planned.Into != "main" {
		t.Errorf("into = %q, want main", planned.Into)
	}
	if planned.Commit != one {
		t.Errorf("commit = %s, want the full name", planned.Commit)
	}
	if planned.Subject != "one" {
		t.Errorf("subject = %q, want the commit's", planned.Subject)
	}

	// Planned, not run.
	if got := shaOf(t, path, "HEAD"); got == one {
		t.Error("HEAD moved onto the reverted commit; the plan must change nothing")
	}
}

func TestRevertAppliesTheInverse(t *testing.T) {
	handler, id, path := openRevertableRepository(t)
	one := shaOf(t, path, "main~1")
	before := shaOf(t, path, "HEAD")

	planned := revertPlanFor(t, handler, id, one)
	response := postJSON(t, handler, "/api/repos/"+id+"/revert",
		revertBody(planned.Commit, planned.Into, planned.Outcome))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	head := shaOf(t, path, "HEAD")
	if head == before || head == one {
		t.Errorf("HEAD at %s, want a new commit (was %s, reverted %s)", head, before, one)
	}
	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("answer HEAD = %+v, want main", refs.Head)
	}
}

func TestARevertAimedAtABranchHEADHasLeftIsRefused(t *testing.T) {
	handler, id, path := openRevertableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := revertPlanFor(t, handler, id, one)
	runGitIn(t, path, "switch", "--detach", "HEAD")

	response := postJSON(t, handler, "/api/repos/"+id+"/revert",
		revertBody(planned.Commit, planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	// Detached first: branchUnderfoot refuses before the Into lease is checked.
	if !strings.Contains(response.Body.String(), "detached") &&
		!strings.Contains(response.Body.String(), "HEAD is not on the branch") {
		t.Errorf("body = %s, want a detached or head-moved refusal", response.Body)
	}
}

func TestPlanningARevertOfAMergeIsRefused(t *testing.T) {
	handler, id, path := openRevertableRepository(t)

	runGitIn(t, path, "switch", "-c", "elsewhere")
	writeFileIn(t, path, "extra2.md", "only here\n")
	runGitIn(t, path, "add", "--", "extra2.md")
	runGitIn(t, path, withIdentity("commit", "-m", "elsewhere")...)
	runGitIn(t, path, "switch", "main")
	runGitIn(t, path, withIdentity("merge", "--no-ff", "--no-edit", "-m", "merge elsewhere", "--", "elsewhere")...)
	merge := shaOf(t, path, "HEAD")

	response := postJSON(t, handler, "/api/repos/"+id+"/revert/plan",
		fmt.Sprintf(`{"commit":%q}`, merge))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "mainline") {
		t.Errorf("body = %s, want the merge-commit refusal", response.Body)
	}
}

func TestPlanningARevertOfARootIsRefused(t *testing.T) {
	handler, id, path := openRevertableRepository(t)
	root := shaOf(t, path, "main~2")

	response := postJSON(t, handler, "/api/repos/"+id+"/revert/plan",
		fmt.Sprintf(`{"commit":%q}`, root))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "root") {
		t.Errorf("body = %s, want the root-commit refusal", response.Body)
	}
}

func TestPlanningARevertOfACommitNotOnTheBranchIsRefused(t *testing.T) {
	handler, id, path := openRevertableRepository(t)

	runGitIn(t, path, "switch", "-c", "side", "main~2")
	writeFileIn(t, path, "side.md", "only side\n")
	runGitIn(t, path, "add", "--", "side.md")
	runGitIn(t, path, withIdentity("commit", "-m", "side only")...)
	side := shaOf(t, path, "HEAD")
	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/revert/plan",
		fmt.Sprintf(`{"commit":%q}`, side))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "not on the branch") {
		t.Errorf("body = %s, want the not-on-branch refusal", response.Body)
	}
}

func TestARevertNamingTheCommitDifferentlyFromThePlanIsRefused(t *testing.T) {
	handler, id, path := openRevertableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := revertPlanFor(t, handler, id, one)

	response := postJSON(t, handler, "/api/repos/"+id+"/revert",
		revertBody(planned.Commit[:7], planned.Into, planned.Outcome))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "read the plan again") {
		t.Errorf("body = %s, want the resolved-name refusal", response.Body)
	}
}

func TestRevertingWithAnUnknownOutcomeIsRefused(t *testing.T) {
	handler, id, path := openRevertableRepository(t)
	one := shaOf(t, path, "main~1")

	response := postJSON(t, handler, "/api/repos/"+id+"/revert",
		revertBody(one, "main", "undo"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "revert") {
		t.Errorf("body = %s, want the outcome named", response.Body)
	}
}

func TestPlanningARevertOfARevisionGitWouldReadAsAnOptionIsRefused(t *testing.T) {
	handler, id, _ := openRevertableRepository(t)

	for _, revision := range []string{"", "--output=/tmp/yagit-escaped"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/revert/plan",
			fmt.Sprintf(`{"commit":%q}`, revision))
		if response.Code != http.StatusBadRequest {
			t.Errorf("commit=%q: status = %d, want 400: %s", revision, response.Code, response.Body)
		}
	}
}
