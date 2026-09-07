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

type submoduleAnswer struct {
	Submodules []struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		URL         string `json:"url"`
		Recorded    string `json:"recorded"`
		HEAD        string `json:"head"`
		Initialised bool   `json:"initialised"`
		Present     bool   `json:"present"`
		Moved       bool   `json:"moved"`
		Declared    bool   `json:"declared"`
	} `json:"submodules"`
}

func submodulesOf(t *testing.T, handler http.Handler, id string) submoduleAnswer {
	t.Helper()
	response := get(t, handler, "/api/repos/"+id+"/submodules")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var answer submoduleAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

// superprojectFixture opens a repository that pins one submodule.
//
// The file transport is allowed through GIT_CONFIG_GLOBAL, never by yagit:
// git refuses a submodule clone over `file://` by default (CVE-2022-39253) and
// reads that setting outside the repository, because the clone runs outside
// one. A client that turned it off for every user would be undoing a security
// control on their behalf.
func superprojectFixture(t *testing.T, name string) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)

	// After serverOnRoot, which isolates the configuration to /dev/null: this
	// is the one setting a submodule fixture needs back.
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[protocol \"file\"]\n\tallow = always\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)

	library := filepath.Join(root, name+"-lib")
	if err := os.MkdirAll(library, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "init", "-b", "main", library)
	runGitIn(t, library, "config", "user.name", "Ada Lovelace")
	runGitIn(t, library, "config", "user.email", "ada@example.com")
	writeFile(t, library, "lib.txt", "one\n")
	runGitIn(t, library, "add", "-A")
	runGitIn(t, library, "commit", "-m", "the library")

	super := filepath.Join(root, name)
	if err := os.MkdirAll(super, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "init", "-b", "main", super)
	runGitIn(t, super, "config", "user.name", "Ada Lovelace")
	runGitIn(t, super, "config", "user.email", "ada@example.com")
	writeFile(t, super, "readme.md", "the superproject\n")
	runGitIn(t, super, "add", "-A")
	runGitIn(t, super, "commit", "-m", "first")
	runGitIn(t, super, "submodule", "add", "--", library, "vendor/lib")
	runGitIn(t, super, "commit", "-m", "pin the library")

	opened := postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, super))
	if opened.Code != http.StatusCreated && opened.Code != http.StatusOK {
		t.Fatalf("open status = %d: %s", opened.Code, opened.Body)
	}
	var repository struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(opened.Body.Bytes(), &repository); err != nil {
		t.Fatal(err)
	}
	return handler, repository.ID, super
}

func TestSubmodulesReadsWhatTheIndexPins(t *testing.T) {
	handler, id, _ := superprojectFixture(t, "sm-list")

	answer := submodulesOf(t, handler, id)
	if len(answer.Submodules) != 1 {
		t.Fatalf("submodules = %+v", answer.Submodules)
	}
	only := answer.Submodules[0]
	if only.Path != "vendor/lib" || !only.Declared || !only.Initialised || !only.Present {
		t.Fatalf("submodule = %+v", only)
	}
	if only.Moved {
		t.Fatalf("submodule = %+v, want it at the commit it records", only)
	}
}

// The bug this guards: an empty submodule directory sits inside the
// superproject's work tree, so a plain `rev-parse HEAD` there answers with the
// SUPERPROJECT's HEAD — a commit from another repository, reported as the
// submodule's and marked "moved" for good measure.
func TestSubmodulesDoesNotReadTheSuperprojectHeadForAnEmptyCheckout(t *testing.T) {
	handler, id, super := superprojectFixture(t, "sm-empty")
	runGitIn(t, super, "submodule", "deinit", "--force", "--", "vendor/lib")

	answer := submodulesOf(t, handler, id)
	if len(answer.Submodules) != 1 {
		t.Fatalf("submodules = %+v", answer.Submodules)
	}
	only := answer.Submodules[0]
	if only.Present || only.HEAD != "" || only.Moved {
		t.Fatalf("submodule = %+v, want no checkout and no commit read", only)
	}
}

func TestUpdateSubmodulesFillsTheCheckout(t *testing.T) {
	handler, id, super := superprojectFixture(t, "sm-update")
	runGitIn(t, super, "submodule", "deinit", "--force", "--", "vendor/lib")

	updated := postJSON(t, handler, "/api/repos/"+id+"/submodules/update", `{"path":""}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", updated.Code, updated.Body)
	}

	if _, err := os.Stat(filepath.Join(super, "vendor", "lib", "lib.txt")); err != nil {
		t.Fatalf("the checkout was not filled: %v", err)
	}
	answer := submodulesOf(t, handler, id)
	if len(answer.Submodules) != 1 || !answer.Submodules[0].Present {
		t.Fatalf("submodules = %+v", answer.Submodules)
	}
}

// git has no `submodule remove`: the plan answers with the pair, so the
// confirmation shows what is really about to run.
func TestRemoveSubmodulePlanAnswersWithBothCommands(t *testing.T) {
	handler, id, _ := superprojectFixture(t, "sm-plan")

	plan := postJSON(t, handler, "/api/repos/"+id+"/submodules/remove/plan",
		`{"path":"vendor/lib","force":false}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", plan.Code, plan.Body)
	}
	var answer struct {
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(plan.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Commands) != 2 {
		t.Fatalf("commands = %v", answer.Commands)
	}
	if !strings.Contains(answer.Commands[0], "submodule deinit") ||
		!strings.Contains(answer.Commands[1], "rm --") {
		t.Fatalf("commands = %v", answer.Commands)
	}
}

func TestRemoveSubmoduleUnpinsItThroughTheRoute(t *testing.T) {
	handler, id, super := superprojectFixture(t, "sm-remove")

	removed := postJSON(t, handler, "/api/repos/"+id+"/submodules/remove",
		`{"path":"vendor/lib","force":false}`)
	if removed.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", removed.Code, removed.Body)
	}
	if answer := submodulesOf(t, handler, id); len(answer.Submodules) != 0 {
		t.Fatalf("submodules = %+v", answer.Submodules)
	}
	// git keeps the objects on purpose, and the confirmation says so.
	if _, err := os.Stat(filepath.Join(super, ".git", "modules", "vendor", "lib")); err != nil {
		t.Fatalf(".git/modules was removed, which git does not do: %v", err)
	}
}

func TestAddSubmoduleRefusesEmptyFields(t *testing.T) {
	handler, id, _ := superprojectFixture(t, "sm-empty-fields")

	for _, body := range []string{
		`{"url":"","path":"vendor/other"}`,
		`{"url":"https://example.test/x.git","path":""}`,
		`{"url":"https://example.test/x.git","path":"-f"}`,
	} {
		response := postJSON(t, handler, "/api/repos/"+id+"/submodules", body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", body, response.Code, response.Body)
		}
	}
}

// A URL that carries a password is redacted on the line the user reads, the
// way a remote list is.
func TestAddSubmodulePlanRedactsTheURL(t *testing.T) {
	handler, id, _ := superprojectFixture(t, "sm-redact")

	plan := postJSON(t, handler, "/api/repos/"+id+"/submodules/plan",
		`{"url":"https://ada:hunter2@example.test/lib.git","path":"vendor/other"}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", plan.Code, plan.Body)
	}
	if strings.Contains(plan.Body.String(), "hunter2") {
		t.Fatalf("the plan carries the password: %s", plan.Body)
	}
}

func TestSubmodulesOnARepositoryThatPinsNothing(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "sm-none")

	if answer := submodulesOf(t, handler, id); len(answer.Submodules) != 0 {
		t.Fatalf("submodules = %+v", answer.Submodules)
	}
}

func TestAddSubmoduleRefusesPathsThatLeaveTheRepository(t *testing.T) {
	handler, id, _ := superprojectFixture(t, "sm-escape")

	for _, body := range []string{
		`{"url":"https://example.test/x.git","path":"../outside"}`,
		`{"url":"https://example.test/x.git","path":"/tmp/absolute"}`,
	} {
		response := postJSON(t, handler, "/api/repos/"+id+"/submodules", body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", body, response.Code, response.Body)
		}
	}
}
