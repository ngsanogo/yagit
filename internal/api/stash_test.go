package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What the stash routes answer, and what they refuse.
//
// The refusals are most of this file, and they are not decoration. Two of them
// stand in front of a command that would otherwise succeed while doing nothing
// — a push with only untracked files, a message git would rewrite — and one
// stands in front of a command that would succeed while doing the wrong thing:
// a position that has come to hold somebody else's work.

type wireStash struct {
	Index   int    `json:"index"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Branch  string `json:"branch"`
	Date    string `json:"date"`
}

type wireStashes struct {
	Stashes []wireStash `json:"stashes"`
}

type wireStashDetail struct {
	wireStash
	Files []struct {
		Path  string `json:"path"`
		Added bool   `json:"added"`
	} `json:"files"`
}

type wireStashPushPlan struct {
	Branch           string `json:"branch"`
	IncludeUntracked bool   `json:"include_untracked"`
	Tracked          int    `json:"tracked"`
	Untracked        int    `json:"untracked"`
}

type wireStashApplyPlan struct {
	Command    string `json:"command"`
	Index      int    `json:"index"`
	SHA        string `json:"sha"`
	Message    string `json:"message"`
	Branch     string `json:"branch"`
	Mode       string `json:"mode"`
	Files      int    `json:"files"`
	DirtyFiles int    `json:"dirty_files"`
}

type wireStashDropPlan struct {
	Command string `json:"command"`
	Index   int    `json:"index"`
	SHA     string `json:"sha"`
	Message string `json:"message"`
	Files   int    `json:"files"`
}

// openStashableRepository has one commit, one changed tracked file and one
// untracked file — the smallest work tree in which every count differs.
func openStashableRepository(t *testing.T) (http.Handler, string, string) {
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

	writeFileIn(t, path, "notes.md", "changed\n")
	writeFileIn(t, path, "scratch.txt", "untracked\n")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

// pushStashThrough saves the work tree over the API and answers with the stack.
func pushStashThrough(t *testing.T, handler http.Handler, id, message string, untracked bool) []wireStash {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/push",
		fmt.Sprintf(`{"message":%q,"untracked":%t}`, message, untracked))
	if response.Code != http.StatusOK {
		t.Fatalf("stash push: status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireStashes](t, response).Stashes
}

func stashesThrough(t *testing.T, handler http.Handler, id string) []wireStash {
	t.Helper()

	response := get(t, handler, "/api/repos/"+id+"/stashes")
	if response.Code != http.StatusOK {
		t.Fatalf("stashes: status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireStashes](t, response).Stashes
}

func TestListingStashesIsAnEmptyArray(t *testing.T) {
	// The same JSON detail TestListingNoRepositoriesIsAnEmptyArray guards: a
	// nil slice marshals to null, and a client doing `stashes.map(…)` on null
	// throws.
	handler, id, _ := openStashableRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/stashes")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), `"stashes":[]`) {
		t.Errorf("body = %s, want an empty array", response.Body)
	}
}

func TestPlanningAStashSaysWhatItWouldSave(t *testing.T) {
	handler, id, path := openStashableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/push/plan", `{"untracked":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[wireStashPushPlan](t, response)
	if planned.Branch != "main" {
		t.Errorf("branch = %q, want main", planned.Branch)
	}
	if planned.Tracked != 1 {
		t.Errorf("tracked = %d, want the one changed file", planned.Tracked)
	}
	if planned.Untracked != 1 {
		t.Errorf("untracked = %d, want the one new file", planned.Untracked)
	}
	if planned.IncludeUntracked {
		t.Error("include_untracked = true, want the question echoed as it was asked")
	}

	// Planned, not run.
	if stashes := stashesThrough(t, handler, id); len(stashes) != 0 {
		t.Errorf("got %d stashes; the plan must change nothing", len(stashes))
	}
	if got := readFileIn(t, path, "notes.md"); got != "changed\n" {
		t.Errorf("notes.md = %q; the plan must not touch the work tree", got)
	}
}

func TestStashingSavesTheWorkTreeAndAnswersWithTheStack(t *testing.T) {
	handler, id, path := openStashableRepository(t)

	stashes := pushStashThrough(t, handler, id, "keep this", false)
	if len(stashes) != 1 {
		t.Fatalf("got %d stashes, want 1: %+v", len(stashes), stashes)
	}
	if stashes[0].Message != "keep this" {
		t.Errorf("message = %q, want the one that was given", stashes[0].Message)
	}
	if stashes[0].Branch != "main" {
		t.Errorf("branch = %q, want main", stashes[0].Branch)
	}
	if stashes[0].Index != 0 {
		t.Errorf("index = %d, want the top of the stack", stashes[0].Index)
	}

	if got := readFileIn(t, path, "notes.md"); got != "base\n" {
		t.Errorf("notes.md = %q, want HEAD's version back", got)
	}
	// The untracked file stays, because no flag asked for it.
	if _, err := os.Stat(filepath.Join(path, "scratch.txt")); err != nil {
		t.Errorf("scratch.txt: %v, want it left where it was", err)
	}
}

func TestPlanningAStashDescribesAWorkTreeItWouldSaveNothingFrom(t *testing.T) {
	// The plan describes rather than refuses, and it has to: this is the work
	// tree whose only route to being stashed is the box on the dialog, and a
	// refusal here would mean the dialog never opens.
	handler, id, path := openStashableRepository(t)
	runGitIn(t, path, "checkout", "--", "notes.md")

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/push/plan", `{"untracked":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[wireStashPushPlan](t, response)
	if planned.Tracked != 0 {
		t.Errorf("tracked = %d, want none", planned.Tracked)
	}
	if planned.Untracked != 1 {
		t.Errorf("untracked = %d, want the file the box would reach", planned.Untracked)
	}
}

func TestStashingRefusesAWorkTreeItWouldSaveNothingFrom(t *testing.T) {
	// git's own version of this exits 0 having done nothing, which is the one
	// outcome an interface cannot tell from success. The guard is on the run,
	// where the command is.
	handler, id, path := openStashableRepository(t)
	runGitIn(t, path, "checkout", "--", "notes.md")

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/push",
		`{"message":"nothing here","untracked":false}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "include-untracked") {
		t.Errorf("body = %s, does not name the flag that would have worked", response.Body)
	}
	if stashes := stashesThrough(t, handler, id); len(stashes) != 0 {
		t.Errorf("got %d stashes, want the refusal to have run nothing", len(stashes))
	}

	// With the box ticked there is something to save, and it is saved.
	if stashes := pushStashThrough(t, handler, id, "the untracked one", true); len(stashes) != 1 {
		t.Fatalf("got %d stashes, want 1", len(stashes))
	}
	if _, err := os.Stat(filepath.Join(path, "scratch.txt")); !os.IsNotExist(err) {
		t.Errorf("scratch.txt is still there: %v", err)
	}
}

func TestStashingRefusesAMessageGitWouldRewrite(t *testing.T) {
	handler, id, _ := openStashableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/push",
		`{"message":"line one\nline two","untracked":false}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if stashes := stashesThrough(t, handler, id); len(stashes) != 0 {
		t.Errorf("got %d stashes, want the refusal to have run nothing", len(stashes))
	}
}

func TestStashingRefusesARepositoryInTheMiddleOfSomething(t *testing.T) {
	// git refuses this too, with "notes.md: needs merge", which says nothing
	// about the merge that is in the way.
	handler, id, path := openStashableRepository(t)
	runGitIn(t, path, "checkout", "--", "notes.md")
	runGitIn(t, path, "checkout", "-b", "side")
	writeFileIn(t, path, "notes.md", "theirs\n")
	runGitIn(t, path, withIdentity("commit", "-am", "theirs")...)
	runGitIn(t, path, "checkout", "main")
	writeFileIn(t, path, "notes.md", "mine\n")
	runGitIn(t, path, withIdentity("commit", "-am", "mine")...)
	// Stops on the conflict and leaves MERGE_HEAD behind, which is the state
	// the refusal is about.
	mergeConflict(t, path)

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/push/plan", `{"untracked":false}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "merge") {
		t.Errorf("body = %s, does not name what is in the way", response.Body)
	}
}

func TestPlanningAnApplyAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "put it back", false)[0]

	for mode, want := range map[string]string{
		"apply": "git stash apply 'stash@{0}'",
		"pop":   "git stash pop 'stash@{0}'",
	} {
		t.Run(mode, func(t *testing.T) {
			planned := applyPlanFor(t, handler, id, stash, mode)
			if planned.Command != want {
				t.Errorf("command = %q, want %q", planned.Command, want)
			}
			if planned.Mode != mode {
				t.Errorf("mode = %q, want %q", planned.Mode, mode)
			}
			if planned.SHA != stash.SHA {
				t.Errorf("sha = %q, want %q", planned.SHA, stash.SHA)
			}
			if planned.Message != "put it back" {
				t.Errorf("message = %q", planned.Message)
			}
			if planned.Files != 1 {
				t.Errorf("files = %d, want the one file the stash holds", planned.Files)
			}
			if planned.DirtyFiles != 0 {
				t.Errorf("dirty_files = %d, want a clean tree", planned.DirtyFiles)
			}
		})
	}
}

func TestApplyingKeepsTheStashAndPoppingRemovesIt(t *testing.T) {
	handler, id, path := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "put it back", false)[0]

	left := applyThrough(t, handler, id, stash, "apply")
	if len(left) != 1 {
		t.Errorf("got %d stashes after an apply, want it left in place", len(left))
	}
	if got := readFileIn(t, path, "notes.md"); got != "changed\n" {
		t.Errorf("notes.md = %q, want the stashed content back", got)
	}

	runGitIn(t, path, "checkout", "--", "notes.md")
	left = applyThrough(t, handler, id, stash, "pop")
	if len(left) != 0 {
		t.Errorf("got %d stashes after a pop, want an empty stack", len(left))
	}
}

func TestApplyingRefusesAPositionThatNowHoldsAnotherStash(t *testing.T) {
	// The refusal this whole family is built around. The dialog read
	// stash@{0}; something pushed another stash; the command it showed would
	// still run, and it would put back the wrong work.
	handler, id, path := openStashableRepository(t)
	approved := pushStashThrough(t, handler, id, "the one that was approved", false)[0]

	writeFileIn(t, path, "notes.md", "meanwhile\n")
	pushStashThrough(t, handler, id, "pushed from somewhere else", false)

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/apply",
		fmt.Sprintf(`{"index":0,"sha":%q,"mode":"pop"}`, approved.SHA))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if stashes := stashesThrough(t, handler, id); len(stashes) != 2 {
		t.Errorf("got %d stashes, want both still there", len(stashes))
	}

	// And at the position it actually moved to, the same request works.
	left := applyThrough(t, handler, id, wireStash{Index: 1, SHA: approved.SHA}, "pop")
	if len(left) != 1 {
		t.Errorf("got %d stashes after the pop, want 1", len(left))
	}
}

func TestApplyingRefusesARequestThatNamesNoStash(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	pushStashThrough(t, handler, id, "only one", false)

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/apply",
		`{"index":0,"sha":"","mode":"apply"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

func TestApplyingRefusesAModeTheDaemonDoesNotKnow(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "only one", false)[0]

	for _, mode := range []string{"", "drop", "--index"} {
		response := postJSON(t, handler, "/api/repos/"+id+"/stash/apply",
			fmt.Sprintf(`{"index":0,"sha":%q,"mode":%q}`, stash.SHA, mode))
		if response.Code != http.StatusBadRequest {
			t.Errorf("mode %q: status = %d, want 400: %s", mode, response.Code, response.Body)
		}
	}
}

func TestApplyingOverAConflictReportsWhatGitSaid(t *testing.T) {
	// A conflicting pop exits non-zero, and everything worth reading is on
	// stdout: which file conflicted, and that the entry was kept. The failure
	// has to carry both.
	handler, id, path := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "conflicting work", false)[0]

	writeFileIn(t, path, "notes.md", "something else entirely\n")
	runGitIn(t, path, "add", "--", "notes.md")

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/apply",
		fmt.Sprintf(`{"index":0,"sha":%q,"mode":"pop"}`, stash.SHA))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body)
	}

	body := response.Body.String()
	for _, want := range []string{"CONFLICT", "stash entry is kept", "git stash pop"} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %s, does not mention %q", body, want)
		}
	}
	if stashes := stashesThrough(t, handler, id); len(stashes) != 1 {
		t.Errorf("got %d stashes, want the entry kept", len(stashes))
	}
}

func TestPlanningADropNamesWhatGoesWithIt(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "throw away", false)[0]

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/drop",
		fmt.Sprintf(`{"index":0,"sha":%q}`, stash.SHA))
	if response.Code != http.StatusOK {
		t.Fatalf("drop: status = %d: %s", response.Code, response.Body)
	}
	if stashes := decode[wireStashes](t, response).Stashes; len(stashes) != 0 {
		t.Errorf("got %d stashes after a drop, want none", len(stashes))
	}
}

func TestPlanningADropAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "throw away", false)[0]

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/drop/plan",
		fmt.Sprintf(`{"index":0,"sha":%q}`, stash.SHA))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[wireStashDropPlan](t, response)
	if planned.Command != "git stash drop 'stash@{0}'" {
		t.Errorf("command = %q", planned.Command)
	}
	if planned.Files != 1 {
		t.Errorf("files = %d, want the one file that goes with it", planned.Files)
	}
	if planned.Message != "throw away" {
		t.Errorf("message = %q", planned.Message)
	}

	// Planned, not run.
	if stashes := stashesThrough(t, handler, id); len(stashes) != 1 {
		t.Errorf("got %d stashes; the plan must change nothing", len(stashes))
	}
}

func TestDroppingRefusesAPositionThatMoved(t *testing.T) {
	handler, id, path := openStashableRepository(t)
	approved := pushStashThrough(t, handler, id, "the one that was approved", false)[0]

	writeFileIn(t, path, "notes.md", "meanwhile\n")
	pushStashThrough(t, handler, id, "pushed from somewhere else", false)

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/drop",
		fmt.Sprintf(`{"index":0,"sha":%q}`, approved.SHA))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if stashes := stashesThrough(t, handler, id); len(stashes) != 2 {
		t.Errorf("got %d stashes, want nothing dropped", len(stashes))
	}
}

func TestDroppingRefusesAPositionPastTheEnd(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	stash := pushStashThrough(t, handler, id, "only one", false)[0]

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/drop",
		fmt.Sprintf(`{"index":7,"sha":%q}`, stash.SHA))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if stashes := stashesThrough(t, handler, id); len(stashes) != 1 {
		t.Errorf("got %d stashes, want nothing dropped", len(stashes))
	}
}

func TestReadingOneStashAnswersWhatItHolds(t *testing.T) {
	handler, id, _ := openStashableRepository(t)
	pushStashThrough(t, handler, id, "one file", false)

	response := get(t, handler, "/api/repos/"+id+"/stashes/0")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	detail := decode[wireStashDetail](t, response)
	if detail.Message != "one file" {
		t.Errorf("message = %q", detail.Message)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "notes.md" {
		t.Fatalf("files = %+v, want the one path the stash holds", detail.Files)
	}
}

func TestReadingAStashOfUntrackedFilesIsNotEmpty(t *testing.T) {
	// Without --include-untracked, `git stash show` answers with nothing at
	// all here and exits 0 — an empty screen for a stash that plainly holds a
	// file.
	handler, id, path := openStashableRepository(t)
	runGitIn(t, path, "checkout", "--", "notes.md")
	pushStashThrough(t, handler, id, "untracked only", true)

	response := get(t, handler, "/api/repos/"+id+"/stashes/0")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	detail := decode[wireStashDetail](t, response)
	if len(detail.Files) != 1 || detail.Files[0].Path != "scratch.txt" {
		t.Fatalf("files = %+v, want the untracked file", detail.Files)
	}
	if !detail.Files[0].Added {
		t.Error("the file is not marked as added, but the stash created it")
	}
}

func TestReadingAStashThatIsNotThere(t *testing.T) {
	handler, id, _ := openStashableRepository(t)

	for name, target := range map[string]int{
		"/api/repos/" + id + "/stashes/3":   http.StatusNotFound,
		"/api/repos/" + id + "/stashes/-1":  http.StatusBadRequest,
		"/api/repos/" + id + "/stashes/top": http.StatusBadRequest,
	} {
		response := get(t, handler, name)
		if response.Code != target {
			t.Errorf("%s: status = %d, want %d: %s", name, response.Code, target, response.Body)
		}
	}
}

func readFileIn(t *testing.T, dir, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}

func applyPlanFor(t *testing.T, handler http.Handler, id string, stash wireStash, mode string) wireStashApplyPlan {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/apply/plan",
		fmt.Sprintf(`{"index":%d,"sha":%q,"mode":%q}`, stash.Index, stash.SHA, mode))
	if response.Code != http.StatusOK {
		t.Fatalf("apply plan: status = %d, want 200: %s", response.Code, response.Body)
	}
	return decode[wireStashApplyPlan](t, response)
}

func applyThrough(t *testing.T, handler http.Handler, id string, stash wireStash, mode string) []wireStash {
	t.Helper()

	response := postJSON(t, handler, "/api/repos/"+id+"/stash/apply",
		fmt.Sprintf(`{"index":%d,"sha":%q,"mode":%q}`, stash.Index, stash.SHA, mode))
	if response.Code != http.StatusOK {
		t.Fatalf("apply %s: status = %d, want 200: %s", mode, response.Code, response.Body)
	}
	return decode[wireStashes](t, response).Stashes
}
