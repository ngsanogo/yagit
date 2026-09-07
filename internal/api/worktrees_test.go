package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type worktreeAnswer struct {
	Worktrees []struct {
		Path     string `json:"path"`
		Branch   string `json:"branch"`
		Detached bool   `json:"detached"`
		Main     bool   `json:"main"`
		Current  bool   `json:"current"`
		Prunable bool   `json:"prunable"`
	} `json:"worktrees"`
}

func worktreesOf(t *testing.T, handler http.Handler, id string) worktreeAnswer {
	t.Helper()
	response := get(t, handler, "/api/repos/"+id+"/worktrees")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var answer worktreeAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

// A repository under the daemon root, so a worktree can be made beside it.
func worktreeFixture(t *testing.T, name string) (http.Handler, string, string, string) {
	t.Helper()
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "init", "-b", "main", path)
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")
	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "-A")
	runGitIn(t, path, "commit", "-m", "first")
	runGitIn(t, path, "branch", "side")

	opened := postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path))
	if opened.Code != http.StatusCreated && opened.Code != http.StatusOK {
		t.Fatalf("open status = %d: %s", opened.Code, opened.Body)
	}
	var repository struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &repository); err != nil {
		t.Fatal(err)
	}
	return handler, repository.ID, root, path
}

func TestWorktreesListsTheMainTreeFirst(t *testing.T) {
	handler, id, _, path := worktreeFixture(t, "wt-list")

	answer := worktreesOf(t, handler, id)
	if len(answer.Worktrees) != 1 {
		t.Fatalf("worktrees = %+v", answer.Worktrees)
	}
	only := answer.Worktrees[0]
	if !only.Main || !only.Current || only.Branch != "main" {
		t.Fatalf("worktree = %+v, want the main tree of %s on main", only, path)
	}
}

func TestAddWorktreeChecksOutABranchBesideIt(t *testing.T) {
	handler, id, root, _ := worktreeFixture(t, "wt-add")
	destination := filepath.Join(root, "wt-add-side")

	plan := postJSON(t, handler, "/api/repos/"+id+"/worktrees/plan",
		fmt.Sprintf(`{"path":%q,"ref":"side","new_branch":"","detach":false}`, destination))
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	if !strings.Contains(plan.Body.String(), "worktree add") {
		t.Fatalf("plan = %s", plan.Body)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("a plan must make nothing: %v", err)
	}

	added := postJSON(t, handler, "/api/repos/"+id+"/worktrees",
		fmt.Sprintf(`{"path":%q,"ref":"side","new_branch":"","detach":false}`, destination))
	if added.Code != http.StatusOK {
		t.Fatalf("add status = %d: %s", added.Code, added.Body)
	}

	answer := worktreesOf(t, handler, id)
	if len(answer.Worktrees) != 2 {
		t.Fatalf("worktrees = %+v", answer.Worktrees)
	}
	// The main tree stays first, and stays the one this tab is on.
	if !answer.Worktrees[0].Main || !answer.Worktrees[0].Current {
		t.Fatalf("first = %+v", answer.Worktrees[0])
	}
	linked := answer.Worktrees[1]
	if linked.Main || linked.Current || linked.Branch != "side" {
		t.Fatalf("second = %+v, want the linked tree on side", linked)
	}
}

func TestAddWorktreeMakesTheBranchWhenAsked(t *testing.T) {
	handler, id, root, _ := worktreeFixture(t, "wt-new-branch")
	destination := filepath.Join(root, "wt-new-branch-topic")

	added := postJSON(t, handler, "/api/repos/"+id+"/worktrees",
		fmt.Sprintf(`{"path":%q,"ref":"main","new_branch":"topic","detach":false}`, destination))
	if added.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", added.Code, added.Body)
	}

	answer := worktreesOf(t, handler, id)
	if len(answer.Worktrees) != 2 || answer.Worktrees[1].Branch != "topic" {
		t.Fatalf("worktrees = %+v", answer.Worktrees)
	}
}

func TestAddWorktreeDetached(t *testing.T) {
	handler, id, root, _ := worktreeFixture(t, "wt-detach")
	destination := filepath.Join(root, "wt-detach-at")

	added := postJSON(t, handler, "/api/repos/"+id+"/worktrees",
		fmt.Sprintf(`{"path":%q,"ref":"main","new_branch":"","detach":true}`, destination))
	if added.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", added.Code, added.Body)
	}

	answer := worktreesOf(t, handler, id)
	if len(answer.Worktrees) != 2 || !answer.Worktrees[1].Detached {
		t.Fatalf("worktrees = %+v", answer.Worktrees)
	}
}

func TestAddWorktreeRefusesAPathOutsideTheRoot(t *testing.T) {
	handler, id, _, _ := worktreeFixture(t, "wt-outside")

	response := postJSON(t, handler, "/api/repos/"+id+"/worktrees",
		fmt.Sprintf(`{"path":%q,"ref":"side","new_branch":"","detach":false}`,
			filepath.Join(t.TempDir(), "elsewhere")))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}
}

func TestRemoveWorktreeDeletesTheDirectory(t *testing.T) {
	handler, id, root, _ := worktreeFixture(t, "wt-remove")
	destination := filepath.Join(root, "wt-remove-side")

	added := postJSON(t, handler, "/api/repos/"+id+"/worktrees",
		fmt.Sprintf(`{"path":%q,"ref":"side","new_branch":"","detach":false}`, destination))
	if added.Code != http.StatusOK {
		t.Fatalf("add status = %d: %s", added.Code, added.Body)
	}

	plan := postJSON(t, handler, "/api/repos/"+id+"/worktrees/remove/plan",
		fmt.Sprintf(`{"path":%q,"force":false}`, destination))
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	if !strings.Contains(plan.Body.String(), "worktree remove") {
		t.Fatalf("plan = %s", plan.Body)
	}

	removed := postJSON(t, handler, "/api/repos/"+id+"/worktrees/remove",
		fmt.Sprintf(`{"path":%q,"force":false}`, destination))
	if removed.Code != http.StatusOK {
		t.Fatalf("remove status = %d: %s", removed.Code, removed.Body)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("the directory is still there: %v", err)
	}

	// The branch it held is untouched: a worktree is a checkout, not the work.
	runGitIn(t, filepath.Join(root, "wt-remove"), "rev-parse", "--verify", "refs/heads/side")
}

// git says "is a main working tree" and exits 128, which is a correct sentence
// about a button that should not have been there. Refused before the
// confirmation is drawn.
func TestRemoveWorktreeRefusesTheMainTree(t *testing.T) {
	handler, id, _, path := worktreeFixture(t, "wt-main")

	response := postJSON(t, handler, "/api/repos/"+id+"/worktrees/remove/plan",
		fmt.Sprintf(`{"path":%q,"force":false}`, path))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

// A checkout whose directory somebody deleted by hand is reported as prunable
// and forgotten on request. What prune removes is git's record of it.
func TestPruneWorktreesForgetsAMissingDirectory(t *testing.T) {
	handler, id, root, _ := worktreeFixture(t, "wt-prune")
	destination := filepath.Join(root, "wt-prune-side")

	added := postJSON(t, handler, "/api/repos/"+id+"/worktrees",
		fmt.Sprintf(`{"path":%q,"ref":"side","new_branch":"","detach":false}`, destination))
	if added.Code != http.StatusOK {
		t.Fatalf("add status = %d: %s", added.Code, added.Body)
	}
	if err := os.RemoveAll(destination); err != nil {
		t.Fatal(err)
	}

	before := worktreesOf(t, handler, id)
	if len(before.Worktrees) != 2 || !before.Worktrees[1].Prunable {
		t.Fatalf("worktrees = %+v, want the missing one marked prunable", before.Worktrees)
	}

	pruned := postJSON(t, handler, "/api/repos/"+id+"/worktrees/prune", `{}`)
	if pruned.Code != http.StatusOK {
		t.Fatalf("prune status = %d: %s", pruned.Code, pruned.Body)
	}
	if after := worktreesOf(t, handler, id); len(after.Worktrees) != 1 {
		t.Fatalf("worktrees = %+v, want only the main tree", after.Worktrees)
	}
}

// A linked worktree outside YAGIT_ROOT (made in a terminal) must not be
// removable through the API — that would be directory-tree deletion past the
// security boundary. Add is root-bounded; remove has to be the same question.
func TestRemoveWorktreeRefusesAPathOutsideTheRoot(t *testing.T) {
	handler, id, root, path := worktreeFixture(t, "wt-outside-remove")

	outside := filepath.Join(t.TempDir(), "linked")
	runGitIn(t, path, "worktree", "add", outside, "side")

	// The list must not invite a remove this daemon refuses: an outside
	// checkout that still exists is omitted. Prunable outside paths can still
	// appear so prune can forget them.
	listed := worktreesOf(t, handler, id)
	for _, worktree := range listed.Worktrees {
		if worktree.Path == outside || strings.Contains(worktree.Path, "linked") {
			t.Fatalf("list includes outside worktree %+v", worktree)
		}
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/worktrees/remove/plan",
		fmt.Sprintf(`{"path":%q,"force":false}`, outside))
	if response.Code != http.StatusForbidden {
		t.Fatalf("plan status = %d, want 403: %s", response.Code, response.Body)
	}

	removed := postJSON(t, handler, "/api/repos/"+id+"/worktrees/remove",
		fmt.Sprintf(`{"path":%q,"force":false}`, outside))
	if removed.Code != http.StatusForbidden {
		t.Fatalf("remove status = %d, want 403: %s", removed.Code, removed.Body)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside worktree was deleted: %v", err)
	}
	_ = root
}
