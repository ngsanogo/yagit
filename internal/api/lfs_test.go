package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
)

type lfsAnswer struct {
	Installed bool     `json:"installed"`
	Version   string   `json:"version"`
	Patterns  []string `json:"patterns"`
}

func lfsState(t *testing.T, handler http.Handler, id string) lfsAnswer {
	t.Helper()
	response := get(t, handler, "/api/repos/"+id+"/lfs")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	return decode[lfsAnswer](t, response)
}

// A repository that has never heard of LFS is the ordinary case: no
// .gitattributes at all. A missing file is an empty list and not a failure.
func TestLFSOfARepositoryWithNoAttributesFile(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "no-attributes")

	state := lfsState(t, handler, id)
	if state.Patterns == nil {
		t.Error("patterns is null; a client declaring it as a list would crash on it")
	}
	if len(state.Patterns) != 0 {
		t.Errorf("patterns = %v, want none", state.Patterns)
	}
}

// The patterns are read out of the file rather than out of git-lfs, which is
// what lets this answer on a machine that has never had git-lfs installed —
// and that machine is exactly the one whose user needs to be told.
func TestLFSReadsThePatternsOutOfGitattributes(t *testing.T) {
	handler, id, path := openNamedRepository(t, "tracked")
	writeFile(t, path, ".gitattributes",
		"*.psd filter=lfs diff=lfs merge=lfs -text\n"+
			"*.md text\n")

	state := lfsState(t, handler, id)
	if len(state.Patterns) != 1 || state.Patterns[0] != "*.psd" {
		t.Errorf("patterns = %v, want [*.psd]", state.Patterns)
	}
}

// The plan is a string, and it is the string that would run. Answered whether
// or not git-lfs is installed: what a command IS does not depend on whether
// this machine can run it.
func TestPlanningAnLFSTrackNamesTheCommand(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "plan-lfs")

	for _, action := range []string{"track", "untrack"} {
		response := postJSON(t, handler,
			"/api/repos/"+id+"/lfs/"+action+"/plan", `{"pattern":"*.psd"}`)
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", action, response.Code, response.Body)
		}

		var planned struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &planned); err != nil {
			t.Fatal(err)
		}
		// The `--` is what makes a pattern beginning with a dash a pattern.
		want := "git lfs " + action + " -- '*.psd'"
		if planned.Command != want {
			t.Errorf("command = %q, want %q", planned.Command, want)
		}
	}
}

// An empty pattern is refused before anything runs. `git lfs track` with no
// argument LISTS the patterns, so sending one through would turn a write into
// a read that answers nothing anybody asked for.
func TestAnEmptyLFSPatternIsRefused(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "empty-pattern")

	for _, target := range []string{"/lfs/track", "/lfs/track/plan", "/lfs/untrack"} {
		response := postJSON(t, handler, "/api/repos/"+id+target, `{"pattern":"   "}`)
		if response.Code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, expected 400: %s", target, response.Code, response.Body)
		}
	}
}

// A bare repository has no work tree, so there is nowhere to put the
// .gitattributes a track would write. Refused with the sentence that says so
// rather than by git failing halfway through.
func TestLFSIsRefusedForABareRepository(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "bare-lfs.git")
	runGitIn(t, root, "init", "--bare", "-b", "main", path)

	opened := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if opened.Code != http.StatusCreated {
		t.Fatalf("opening the bare repository: %d: %s", opened.Code, opened.Body)
	}
	id := decode[wireRepo](t, opened).ID

	if response := get(t, handler, "/api/repos/"+id+"/lfs"); response.Code != http.StatusConflict {
		t.Errorf("GET /lfs on a bare repository = %d, expected 409: %s",
			response.Code, response.Body)
	}
	response := postJSON(t, handler, "/api/repos/"+id+"/lfs/track", `{"pattern":"*.psd"}`)
	if response.Code != http.StatusConflict {
		t.Errorf("POST /lfs/track on a bare repository = %d, expected 409: %s",
			response.Code, response.Body)
	}
}
