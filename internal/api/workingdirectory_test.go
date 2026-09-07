package api_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// What the working-directory routes answer, and — more to the point — what
// they refuse.
//
// The refusals are the half worth testing. Staging part of a file is the only
// operation in the daemon that can succeed while doing something other than
// what was asked, because a patch built against a diff the file has since
// moved past can still apply. The status code that says so is what the
// interface acts on.

// wireStatus mirrors what GET /api/repos/{id}/status sends. Written out here
// rather than decoded into the internal type, so that a field added to the
// payload does not reach a browser unnoticed.
type wireFile struct {
	Path     string `json:"path"`
	OldPath  string `json:"old_path"`
	Kind     string `json:"kind"`
	Index    string `json:"index"`
	WorkTree string `json:"work_tree"`
	Staged   bool   `json:"staged"`
	Unstaged bool   `json:"unstaged"`
	Conflict string `json:"conflict"`
}

type wireStatus struct {
	Branch   string     `json:"branch"`
	Detached bool       `json:"detached"`
	HeadSHA  string     `json:"head_sha"`
	Unborn   bool       `json:"unborn"`
	Upstream string     `json:"upstream"`
	Ahead    int        `json:"ahead"`
	Behind   int        `json:"behind"`
	Files    []wireFile `json:"files"`
}

// file finds one path in the status, failing the test when it is absent.
func (s wireStatus) file(t *testing.T, path string) wireFile {
	t.Helper()
	for _, file := range s.Files {
		if file.Path == path {
			return file
		}
	}
	t.Fatalf("%s is not in the status: %+v", path, s.Files)
	return wireFile{}
}

type wireDiff struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Added bool   `json:"added"`
	Hunks []struct {
		Lines []struct {
			Kind  string `json:"kind"`
			Text  string `json:"text"`
			Index int    `json:"index"`
		} `json:"lines"`
	} `json:"hunks"`
}

// openWorkingRepository builds a repository with one commit and one modified
// file, opens it through the API, and hands back its identifier and path.
//
//	committed:  one  two   three
//	on disk:    one  TWO   three  four
func openWorkingRepository(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, path, "init", "-b", "main")
	writeInRepo(t, path, "a.txt", "one\ntwo\nthree\n")
	runGitIn(t, path, "add", "--", "a.txt")
	runGitIn(t, path, withIdentity("commit", "-m", "first")...)
	writeInRepo(t, path, "a.txt", "one\nTWO\nthree\nfour\n")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

func writeInRepo(t *testing.T, repoPath, name, content string) {
	t.Helper()
	full := filepath.Join(repoPath, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readInRepo(t *testing.T, repoPath, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repoPath, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func TestStatusReportsWhatDiffersOnEachSide(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	writeInRepo(t, path, "new.txt", "fresh\n")

	response := get(t, handler, "/api/repos/"+id+"/status")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	status := decode[wireStatus](t, response)
	if status.Branch != "main" {
		t.Errorf("Branch = %q", status.Branch)
	}
	if status.Unborn || status.Detached {
		t.Error("a branch with a commit is neither unborn nor detached")
	}

	edited := status.file(t, "a.txt")
	if edited.Staged || !edited.Unstaged {
		t.Errorf("a.txt = %+v, expected unstaged only", edited)
	}
	if edited.Index != "." || edited.WorkTree != "M" {
		t.Errorf("a.txt codes = %q%q, expected .M", edited.Index, edited.WorkTree)
	}

	if kind := status.file(t, "new.txt").Kind; kind != "untracked" {
		t.Errorf("new.txt kind = %q, expected untracked", kind)
	}
}

func TestStatusOfACleanRepositoryIsAnEmptyArray(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	runGitIn(t, path, "restore", "--worktree", "--", "a.txt")

	response := get(t, handler, "/api/repos/"+id+"/status")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	// A nil slice marshals to null, and a panel doing files.map(…) on null
	// throws — on the state most repositories are in most of the time.
	if body := response.Body.String(); !strings.Contains(body, `"files":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", body)
	}
}

func TestTheWorkingDirectoryOfABareRepositoryIsRefusedRatherThanAttempted(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "mirror.git")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, path, "init", "--bare", "-b", "main")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}
	id := decode[wireRepo](t, response).ID

	// 409 rather than a git failure: the request is well formed and the
	// repository exists, and what is asked for cannot apply to it. Reporting
	// git's own complaint about a missing work tree would blame the wrong
	// thing.
	response = get(t, handler, "/api/repos/"+id+"/status")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "bare") {
		t.Errorf("the message does not say why: %s", response.Body)
	}
}

func TestDiffCarriesItsOwnFingerprint(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/diff?path=a.txt&side=unstaged")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	diff := decode[wireDiff](t, response)
	if diff.ID == "" {
		t.Error("the diff must carry the fingerprint a line selection is checked against")
	}
	if diff.Path != "a.txt" {
		t.Errorf("Path = %q", diff.Path)
	}
	if len(diff.Hunks) != 1 || len(diff.Hunks[0].Lines) != 5 {
		t.Fatalf("expected one hunk of five lines, got %+v", diff.Hunks)
	}
}

func TestDiffRefusesASideItDoesNotKnow(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	for _, target := range []string{
		"/api/repos/" + id + "/diff?path=a.txt",
		"/api/repos/" + id + "/diff?path=a.txt&side=sideways",
		"/api/repos/" + id + "/diff?side=unstaged",
	} {
		response := get(t, handler, target)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", target, response.Code, response.Body)
		}
	}
}

func TestDiffRefusesAPathThatLeavesTheRepository(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	// git refuses these too. The check is here as well because the boundary
	// belongs where it can be enforced, not in one subcommand's argument
	// handling.
	for _, path := range []string{"../outside.txt", "/etc/passwd", "a/../../../etc/passwd"} {
		response := get(t, handler,
			"/api/repos/"+id+"/diff?side=unstaged&path="+strings.ReplaceAll(path, "/", "%2F"))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400: %s", path, response.Code, response.Body)
		}
	}
}

func TestStagingAWholePathAnswersWithTheStatusThatFollowed(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["a.txt"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	// The answer is the new status, not 204. Staging a file changes what
	// every other row says about itself, and a client that had to ask again
	// would draw one frame of the state it just left.
	staged := decode[wireStatus](t, response).file(t, "a.txt")
	if !staged.Staged || staged.Unstaged {
		t.Errorf("a.txt = %+v, expected staged only", staged)
	}
}

func TestStagingSomeLinesPutsOnlyThoseInTheIndex(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	diff := decode[wireDiff](t, get(t, handler, "/api/repos/"+id+"/diff?path=a.txt&side=unstaged"))

	// Lines: 0 context, 1 removed "two", 2 added "TWO", 3 context, 4 added
	// "four". Take the substitution and leave "four" behind.
	body := fmt.Sprintf(`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[1,2]}}`, diff.ID)
	response := postJSON(t, handler, "/api/repos/"+id+"/stage", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	// Staged and unstaged at once, which is the state that exists precisely
	// because part of the file was left behind.
	file := decode[wireStatus](t, response).file(t, "a.txt")
	if !file.Staged || !file.Unstaged {
		t.Errorf("a.txt = %+v, expected both staged and unstaged", file)
	}

	if content := readInRepo(t, path, "a.txt"); content != "one\nTWO\nthree\nfour\n" {
		t.Errorf("the work tree was modified: %q", content)
	}
}

func TestStagingEveryLineIsStagingTheFile(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	diff := decode[wireDiff](t, get(t, handler, "/api/repos/"+id+"/diff?path=a.txt&side=unstaged"))

	body := fmt.Sprintf(`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[1,2,4]}}`, diff.ID)
	response := postJSON(t, handler, "/api/repos/"+id+"/stage", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	file := decode[wireStatus](t, response).file(t, "a.txt")
	if !file.Staged || file.Unstaged {
		t.Errorf("a.txt = %+v; every line is the whole file", file)
	}
}

func TestStagingLinesOfADiffThatMovedIsRefused(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	diff := decode[wireDiff](t, get(t, handler, "/api/repos/"+id+"/diff?path=a.txt&side=unstaged"))

	// The user saves in their editor between drawing the diff and clicking.
	// The line numbers now describe something else, and a patch built from
	// them could still apply somewhere.
	writeInRepo(t, path, "a.txt", "one\nTWO\nthree\nfour\nfive\n")

	body := fmt.Sprintf(`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[1,2]}}`, diff.ID)
	response := postJSON(t, handler, "/api/repos/"+id+"/stage", body)

	// 409: the request was correct when it was made and the disk moved under
	// it. Neither a bad request nor a server fault.
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "changed since") {
		t.Errorf("the message does not say what happened: %s", response.Body)
	}
}

func TestStagingLinesRefusesWhatCannotMeanAnything(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)
	diff := decode[wireDiff](t, get(t, handler, "/api/repos/"+id+"/diff?path=a.txt&side=unstaged"))

	cases := map[string]string{
		"no path at all": `{"paths":[]}`,
		"lines across two files": fmt.Sprintf(
			`{"paths":["a.txt","b.txt"],"lines":{"diff":%q,"indices":[1]}}`, diff.ID),
		"lines with no diff to check against": `{"paths":["a.txt"],"lines":{"indices":[1]}}`,
		"lines with nothing chosen": fmt.Sprintf(
			`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[]}}`, diff.ID),
		"a field nobody defined":        `{"path":"a.txt"}`,
		"a path leaving the repository": `{"paths":["../outside.txt"]}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response := postJSON(t, handler, "/api/repos/"+id+"/stage", body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
			}
		})
	}
}

func TestUnstagingPutsTheIndexBack(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	if response := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["a.txt"]}`); response.Code != http.StatusOK {
		t.Fatalf("staging: %d: %s", response.Code, response.Body)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/unstage", `{"paths":["a.txt"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	file := decode[wireStatus](t, response).file(t, "a.txt")
	if file.Staged || !file.Unstaged {
		t.Errorf("a.txt = %+v, expected unstaged only", file)
	}
}

func TestUnstagingOnABranchWithNoCommitYet(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "fresh")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, path, "init", "-b", "main")
	writeInRepo(t, path, "f.txt", "hello\n")
	runGitIn(t, path, "add", "--", "f.txt")

	opened := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	id := decode[wireRepo](t, opened).ID

	// `git restore --staged` has no HEAD to restore from here and exits 128.
	// The daemon reads the status first and picks the command that works.
	response := postJSON(t, handler, "/api/repos/"+id+"/unstage", `{"paths":["f.txt"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	if kind := decode[wireStatus](t, response).file(t, "f.txt").Kind; kind != "untracked" {
		t.Errorf("f.txt kind = %q, expected untracked", kind)
	}
	// Unstaging must not delete the user's work.
	if content := readInRepo(t, path, "f.txt"); content != "hello\n" {
		t.Errorf("the file was changed on disk: %q", content)
	}
}

func TestDiscardingSortsTrackedFromUntracked(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	writeInRepo(t, path, "throwaway.txt", "nothing of value\n")

	// Two different git commands, and the split is made from the status:
	// `git restore` cannot remove a file git has never seen, and `git clean`
	// would refuse a tracked one.
	response := postJSON(t, handler, "/api/repos/"+id+"/discard",
		`{"paths":["a.txt","throwaway.txt"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	if content := readInRepo(t, path, "a.txt"); content != "one\ntwo\nthree\n" {
		t.Errorf("a.txt = %q, expected the committed content back", content)
	}
	if _, err := os.Stat(filepath.Join(path, "throwaway.txt")); !os.IsNotExist(err) {
		t.Errorf("throwaway.txt is still there: %v", err)
	}

	status := decode[wireStatus](t, response)
	if len(status.Files) != 0 {
		t.Errorf("expected a clean status, got %+v", status.Files)
	}
}

// TestTheDiscardPlanIsTheCommandThatRuns is the test the plan route exists
// for.
//
// The confirmation dialog shows what a discard will run. It used to compose
// that line itself, and it was wrong: git receives every path as a
// `:(literal)` pathspec — without which a file named `*` makes "discard this
// one file" delete every untracked file in the work tree — and no browser
// knows that. Reading the plan is only worth anything if the plan is what runs,
// so this asserts the two against the daemon's own log of what it executed.
func TestTheDiscardPlanIsTheCommandThatRuns(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	writeInRepo(t, path, "throwaway.txt", "nothing of value\n")

	// Both kinds at once: the plan is two commands whenever the selection is,
	// which is the case a single composed line cannot express.
	body := `{"paths":["a.txt","throwaway.txt"]}`

	response := postJSON(t, handler, "/api/repos/"+id+"/discard/plan", body)
	if response.Code != http.StatusOK {
		t.Fatalf("plan: status = %d: %s", response.Code, response.Body)
	}
	plan := decode[struct {
		Commands []string `json:"commands"`
	}](t, response)

	want := []string{
		"git restore --worktree -- ':(literal)a.txt'",
		"git clean --force -- ':(literal)throwaway.txt'",
	}
	if !slices.Equal(plan.Commands, want) {
		t.Fatalf("plan = %q, want %q", plan.Commands, want)
	}

	// Nothing ran: a plan that discarded what it described would be a
	// confirmation dialog that acts before it is answered.
	if content := readInRepo(t, path, "a.txt"); content != "one\nTWO\nthree\nfour\n" {
		t.Errorf("a.txt = %q, the plan changed the work tree", content)
	}

	if response := postJSON(t, handler, "/api/repos/"+id+"/discard", body); response.Code != http.StatusOK {
		t.Fatalf("discard: status = %d: %s", response.Code, response.Body)
	}

	// And now the half that cannot drift: every planned line is in the log of
	// what the daemon actually executed.
	log := decode[struct {
		Executions []struct {
			Command string `json:"command"`
		} `json:"executions"`
	}](t, get(t, handler, "/api/log"))

	ran := make([]string, 0, len(log.Executions))
	for _, execution := range log.Executions {
		ran = append(ran, execution.Command)
	}
	for _, command := range plan.Commands {
		if !slices.Contains(ran, command) {
			t.Errorf("the plan promised %q, which the daemon never ran: %q", command, ran)
		}
	}
}

// TestTheDiscardPlanOfALineSelectionIsTheApply covers the third command, which
// takes no path at all: a patch names its own files, so discarding lines is one
// `git apply` whatever was selected.
func TestTheDiscardPlanOfALineSelectionIsTheApply(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	diff := decode[wireDiff](t, get(t, handler, "/api/repos/"+id+"/diff?path=a.txt&side=unstaged"))

	response := postJSON(t, handler, "/api/repos/"+id+"/discard/plan",
		fmt.Sprintf(`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[0]}}`, diff.ID))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	plan := decode[struct {
		Commands []string `json:"commands"`
	}](t, response)
	want := []string{"git apply --reverse -"}
	if !slices.Equal(plan.Commands, want) {
		t.Fatalf("plan = %q, want %q", plan.Commands, want)
	}
}

// TestTheDiscardPlanRefusesWhatTheDiscardRefuses keeps the question and the
// answer on the same terms. A dialog that opened for a selection the daemon
// would reject asks the user to approve something that cannot happen.
func TestTheDiscardPlanRefusesWhatTheDiscardRefuses(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/discard/plan",
		`{"paths":["../outside.txt"]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
}

func TestCommitRecordsTheIndexAndReturnsItsSHA(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	// The identity has to come from somewhere: serverOnRoot points git at
	// empty configuration on purpose.
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")

	if response := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["a.txt"]}`); response.Code != http.StatusOK {
		t.Fatalf("staging: %d: %s", response.Code, response.Body)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/commit",
		`{"message":"second: the subject\n\nand a body"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}

	committed := decode[struct {
		SHA string `json:"sha"`
	}](t, response)
	if len(committed.SHA) != 40 {
		t.Errorf("sha = %q, expected a full object name", committed.SHA)
	}

	status := decode[wireStatus](t, get(t, handler, "/api/repos/"+id+"/status"))
	if len(status.Files) != 0 {
		t.Errorf("expected a clean working directory, got %+v", status.Files)
	}
	if status.HeadSHA != committed.SHA {
		t.Errorf("HEAD is %q, the commit reported %q", status.HeadSHA, committed.SHA)
	}
}

func TestCommitWithNoMessageIsRefusedBeforeGitRuns(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/commit", `{"message":"   "}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestCommitWithNothingStagedCarriesGitsOwnRefusal(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")

	response := postJSON(t, handler, "/api/repos/"+id+"/commit", `{"message":"nothing to say"}`)

	// 422: git refused, and the refusal travels whole. Never "Something went
	// wrong".
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body)
	}

	failure := decode[struct {
		Error struct {
			Git *struct {
				Command  string `json:"command"`
				ExitCode int    `json:"exit_code"`
				Stderr   string `json:"stderr"`
			} `json:"git"`
		} `json:"error"`
	}](t, response)

	if failure.Error.Git == nil {
		t.Fatalf("the git failure did not reach the client: %s", response.Body)
	}
	if !strings.HasPrefix(failure.Error.Git.Command, "git commit") {
		t.Errorf("command = %q", failure.Error.Git.Command)
	}
	if failure.Error.Git.ExitCode == 0 {
		t.Error("a refusal with exit code 0 is not a refusal")
	}
}

func TestTheCommandLogRemembersWhatRan(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	if response := get(t, handler, "/api/repos/"+id+"/status"); response.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", response.Code, response.Body)
	}

	response := get(t, handler, "/api/log")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	log := decode[struct {
		Executions []struct {
			ID       string `json:"id"`
			Command  string `json:"command"`
			ExitCode int    `json:"exit_code"`
		} `json:"executions"`
	}](t, response)

	// The promise: a user must be able to learn git by watching yagit work.
	// That is only true if what ran is what is shown.
	var found bool
	seen := make(map[string]bool)
	for _, execution := range log.Executions {
		if seen[execution.ID] {
			t.Errorf("two executions share the identifier %q", execution.ID)
		}
		seen[execution.ID] = true
		if strings.HasPrefix(execution.Command, "git status --porcelain=v2") {
			found = true
		}
	}
	if !found {
		t.Errorf("the status command is not in the log: %s", response.Body)
	}
}

func TestTheCommandLogOfAnIdleDaemonIsAnEmptyArray(t *testing.T) {
	handler, _ := serverOnRoot(t)

	response := get(t, handler, "/api/log")
	if body := response.Body.String(); !strings.Contains(body, `"executions":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", body)
	}
}

func TestTheEventStreamAnnouncesWhatTheDaemonRuns(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	// A real server rather than httptest.ResponseRecorder: the recorder has
	// no connection to flush to and no way to be read while the handler is
	// still writing, which is the whole behaviour of a stream.
	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("X-Yagit-Token", testToken)

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("closing the stream: %v", err)
		}
	}()

	if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		// A cached event stream plays yesterday's events back once and ends.
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	reader := bufio.NewReader(response.Body)

	// The reconnection delay is written before anything else, and it arriving
	// at all is what proves the handler flushes rather than buffering until
	// it returns.
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if !strings.HasPrefix(first, "retry: ") {
		t.Errorf("the stream does not open with a reconnection delay: %q", first)
	}

	// Something to announce.
	if status := get(t, handler, "/api/repos/"+id+"/status"); status.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", status.Code, status.Body)
	}

	kind, data := readEvent(t, reader)
	if kind != "git" {
		t.Fatalf("event = %q, expected a git command", kind)
	}
	// The payload is compact JSON for one reason: an SSE data field is a
	// single line, and a newline inside it would end the event early and
	// leave the rest to be read as another.
	if strings.Contains(data, "\n") || !json.Valid([]byte(data)) {
		t.Errorf("data is not one line of whole JSON: %q", data)
	}
	if !strings.Contains(data, "git status --porcelain=v2") {
		t.Errorf("the announced command is not the one that ran: %q", data)
	}
}

// readEvent reads one SSE event and returns its kind and its data.
func readEvent(t *testing.T, reader *bufio.Reader) (kind, data string) {
	t.Helper()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the stream: %v", err)
		}
		line = strings.TrimRight(line, "\n")

		switch {
		case strings.HasPrefix(line, "event: "):
			kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "":
			if kind != "" {
				return kind, data
			}
		}
	}
}

func TestReadingOneCommitWhole(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	head := decode[wireStatus](t, get(t, handler, "/api/repos/"+id+"/status")).HeadSHA

	response := get(t, handler, "/api/repos/"+id+"/commits/"+head)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	detail := decode[struct {
		SHA       string `json:"sha"`
		Subject   string `json:"subject"`
		Body      string `json:"body"`
		Committer string `json:"committer"`
		Files     []struct {
			Path  string `json:"path"`
			Added bool   `json:"added"`
		} `json:"files"`
		AgainstFirstParent bool `json:"against_first_parent"`
	}](t, response)

	if detail.SHA != head {
		t.Errorf("sha = %q, expected %q", detail.SHA, head)
	}
	if detail.Subject != "first" {
		t.Errorf("subject = %q", detail.Subject)
	}
	if detail.Committer == "" {
		t.Error("the committer identity did not reach the wire")
	}
	if detail.AgainstFirstParent {
		t.Error("a root commit is not a merge")
	}
	// A root commit's patch is the creation of every file in it: there is no
	// parent to diff against, and a diff against one that does not exist
	// would have shown nothing at all.
	if len(detail.Files) != 1 {
		t.Fatalf("expected 1 file, got %+v", detail.Files)
	}
	if detail.Files[0].Path != "a.txt" || !detail.Files[0].Added {
		t.Errorf("files = %+v", detail.Files)
	}
}

func TestACommitNobodyHasIsNotFound(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	// 404 rather than 500: a commit the walk does not hold is a request to
	// fix, not a daemon that broke. No git command runs — the history is
	// already in memory, and a name it does not carry is answered from that
	// rather than by asking git about an object nobody has.
	response := get(t, handler,
		"/api/repos/"+id+"/commits/0000000000000000000000000000000000000000")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", response.Code, response.Body)
	}
	// And the refusal names the walk, because that is what the reader can
	// change: the same object is often present under every reference.
	if !strings.Contains(response.Body.String(), "is not in the history drawn from") {
		t.Errorf("the refusal does not say what is wrong: %s", response.Body)
	}
}

func TestARevisionGitWouldReadAsAnOptionIsRefused(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	// git has no `--` for revisions, so a leading dash makes the argument an
	// option — and `git log --output=…` writes a file. 400: the request is
	// malformed, and no git command runs at all.
	//
	// Refused on its shape rather than on the history not holding it. This
	// route is only ever given a SHA the daemon put on screen, so anything
	// that is not forty or sixty-four hexadecimal characters is answered
	// before the lookup — a guard that depended on a scan finding nothing
	// would be one refactor away from being absent.
	response := get(t, handler, "/api/repos/"+id+"/commits/"+url.PathEscape("--output=/tmp/written"))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "is not a commit name") {
		t.Errorf("the refusal does not say what is wrong: %s", response.Body)
	}
}
