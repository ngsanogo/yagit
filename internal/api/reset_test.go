package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the reset routes answer, and what they refuse.

type wireResetPlan struct {
	Command    string `json:"command"`
	Commit     string `json:"commit"`
	Subject    string `json:"subject"`
	Into       string `json:"into"`
	Mode       string `json:"mode"`
	Dropping   int    `json:"dropping"`
	DirtyFiles int    `json:"dirty_files"`
}

func resetBody(commit, into, mode string) string {
	return fmt.Sprintf(`{"commit":%q,"into":%q,"mode":%q}`, commit, into, mode)
}

func resetPlanFor(t *testing.T, handler http.Handler, id, commit, mode string) wireResetPlan {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/reset/plan",
		fmt.Sprintf(`{"commit":%q,"mode":%q}`, commit, mode))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireResetPlan](t, response)
}

// openResettableRepository is a linear history on main: base → one → two.
func openResettableRepository(t *testing.T) (http.Handler, string, string) {
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

func TestPlanningAResetAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, path := openResettableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := resetPlanFor(t, handler, id, one, "hard")
	wantCommand := "git reset --hard " + one
	if planned.Command != wantCommand {
		t.Errorf("command = %q, want %q", planned.Command, wantCommand)
	}
	if planned.Mode != "hard" {
		t.Errorf("mode = %q, want hard", planned.Mode)
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
	if planned.Dropping != 1 {
		t.Errorf("dropping = %d, want 1", planned.Dropping)
	}

	// Planned, not run.
	if got := shaOf(t, path, "HEAD"); got == one {
		t.Error("HEAD moved onto the target; the plan must change nothing")
	}
}

func TestResetHardMovesTheBranch(t *testing.T) {
	handler, id, path := openResettableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := resetPlanFor(t, handler, id, one, "hard")
	response := postJSON(t, handler, "/api/repos/"+id+"/reset",
		resetBody(planned.Commit, planned.Into, planned.Mode))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if head := shaOf(t, path, "HEAD"); head != one {
		t.Errorf("HEAD at %s, want %s", head, one)
	}
	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("answer HEAD = %+v, want main", refs.Head)
	}
	if refs.Head != nil && refs.Head.SHA != one {
		t.Errorf("answer HEAD sha = %s, want %s", refs.Head.SHA, one)
	}
}

func TestAResetAimedAtABranchHEADHasLeftIsRefused(t *testing.T) {
	handler, id, path := openResettableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := resetPlanFor(t, handler, id, one, "mixed")
	runGitIn(t, path, "switch", "--detach", "HEAD")

	response := postJSON(t, handler, "/api/repos/"+id+"/reset",
		resetBody(planned.Commit, planned.Into, planned.Mode))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "detached") &&
		!strings.Contains(response.Body.String(), "HEAD is not on the branch") {
		t.Errorf("body = %s, want a detached or head-moved refusal", response.Body)
	}
}

func TestPlanningAResetOfACommitNotOnTheBranchIsRefused(t *testing.T) {
	handler, id, path := openResettableRepository(t)

	runGitIn(t, path, "switch", "-c", "side", "main~2")
	writeFileIn(t, path, "side.md", "only side\n")
	runGitIn(t, path, "add", "--", "side.md")
	runGitIn(t, path, withIdentity("commit", "-m", "side only")...)
	side := shaOf(t, path, "HEAD")
	runGitIn(t, path, "switch", "main")

	response := postJSON(t, handler, "/api/repos/"+id+"/reset/plan",
		fmt.Sprintf(`{"commit":%q,"mode":"hard"}`, side))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "not on the branch") {
		t.Errorf("body = %s, want the not-on-branch refusal", response.Body)
	}
}

func TestAResetNamingTheCommitDifferentlyFromThePlanIsRefused(t *testing.T) {
	handler, id, path := openResettableRepository(t)
	one := shaOf(t, path, "main~1")

	planned := resetPlanFor(t, handler, id, one, "soft")

	response := postJSON(t, handler, "/api/repos/"+id+"/reset",
		resetBody(planned.Commit[:7], planned.Into, planned.Mode))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "read the plan again") {
		t.Errorf("body = %s, want the resolved-name refusal", response.Body)
	}
}

func TestResettingWithAnUnknownModeIsRefused(t *testing.T) {
	handler, id, path := openResettableRepository(t)
	one := shaOf(t, path, "main~1")

	response := postJSON(t, handler, "/api/repos/"+id+"/reset",
		resetBody(one, "main", "keep"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "soft, mixed or hard") {
		t.Errorf("body = %s, want the modes named", response.Body)
	}
}

func TestPlanningAResetOfARevisionGitWouldReadAsAnOptionIsRefused(t *testing.T) {
	handler, id, _ := openResettableRepository(t)

	for _, revision := range []string{"", "--output=/tmp/yagit-escaped"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/reset/plan",
			fmt.Sprintf(`{"commit":%q,"mode":"mixed"}`, revision))
		if response.Code != http.StatusBadRequest {
			t.Errorf("commit=%q: status = %d, want 400: %s", revision, response.Code, response.Body)
		}
	}
}
