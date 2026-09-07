package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// headRefIn is what HEAD points at, which on a repository with no commits is
// the only place the first branch's name is written down.
func headRefIn(t *testing.T, dir string) string {
	t.Helper()

	runner := git.NewRunner(nil)
	output, err := runner.Run(context.Background(), dir, "symbolic-ref", "HEAD")
	if err != nil {
		t.Fatalf("symbolic-ref HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(output))
}

func TestInitPlanShowsTheCommandWithoutMakingAnything(t *testing.T) {
	handler, root := serverOnRoot(t)
	destination := filepath.Join(root, "fresh")

	response := postJSON(t, handler, "/api/repos/init/plan",
		fmt.Sprintf(`{"path":%q,"branch":"trunk"}`, destination))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	var plan struct {
		Command string `json:"command"`
		Path    string `json:"path"`
		Branch  string `json:"branch"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Command, "init --initial-branch=trunk") {
		t.Fatalf("command = %q", plan.Command)
	}
	if plan.Path != destination || plan.Branch != "trunk" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("a plan must not make the destination: %v", err)
	}
}

// The branch name is a setting on the daemon's machine, so a client that sends
// none is answered with the name that will actually be used rather than left
// to guess.
func TestInitPlanFillsTheBranchTheClientLeftEmpty(t *testing.T) {
	handler, root := serverOnRoot(t)

	response := postJSON(t, handler, "/api/repos/init/plan",
		fmt.Sprintf(`{"path":%q,"branch":""}`, filepath.Join(root, "fresh")))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	var plan struct {
		Command string `json:"command"`
		Branch  string `json:"branch"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Branch == "" {
		t.Fatalf("plan = %+v, want a settled branch name", plan)
	}
	if !strings.Contains(plan.Command, "--initial-branch="+plan.Branch) {
		t.Fatalf("command %q does not pin the branch it names", plan.Command)
	}
}

func TestInitMakesTheRepositoryAndOpensIt(t *testing.T) {
	handler, root := serverOnRoot(t)
	destination := filepath.Join(root, "fresh")

	response := postJSON(t, handler, "/api/repos/init",
		fmt.Sprintf(`{"path":%q,"branch":"trunk"}`, destination))
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	var made struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	if made.ID == "" {
		t.Fatalf("no identifier for the repository just opened: %s", response.Body)
	}

	// symbolic-ref rather than rev-parse: the branch has no commit on it yet,
	// and HEAD is the only thing that knows its name.
	if head := headRefIn(t, destination); head != "refs/heads/trunk" {
		t.Fatalf("HEAD = %q, want refs/heads/trunk", head)
	}

	// It is open: the reference list answers for it, on a branch with no
	// commits on it yet.
	refs := get(t, handler, "/api/repos/"+made.ID+"/refs")
	if refs.Code != http.StatusOK {
		t.Fatalf("refs status = %d: %s", refs.Code, refs.Body)
	}
}

func TestInitRefusesADestinationOutsideTheRoot(t *testing.T) {
	handler, _ := serverOnRoot(t)

	response := postJSON(t, handler, "/api/repos/init",
		fmt.Sprintf(`{"path":%q,"branch":"main"}`, filepath.Join(t.TempDir(), "elsewhere")))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}
}

func TestInitRefusesAPathThatExists(t *testing.T) {
	handler, root := serverOnRoot(t)
	taken := filepath.Join(root, "taken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}

	response := postJSON(t, handler, "/api/repos/init",
		fmt.Sprintf(`{"path":%q,"branch":"main"}`, taken))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

func TestInitRefusesABranchNameGitWouldNot(t *testing.T) {
	handler, root := serverOnRoot(t)
	destination := filepath.Join(root, "fresh")

	response := postJSON(t, handler, "/api/repos/init",
		fmt.Sprintf(`{"path":%q,"branch":"no spaces"}`, destination))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("a refused name must leave nothing behind: %v", err)
	}
}
