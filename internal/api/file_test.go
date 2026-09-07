package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Editing a file, and the two ways out of a conflict, over HTTP.
//
// The refusals are again the half worth testing. A save is the only operation
// in the daemon that can destroy work the user has not committed anywhere —
// staging and discarding act on what git already knows about, and this writes
// bytes over somebody's source — so what it refuses to write matters more than
// what it writes.

// wireWorkFile mirrors what GET /api/repos/{id}/file sends. Named apart from
// wireFile beside it, which is a row of the STATUS: one is a file's content
// and the other is git's opinion of it, and a test that confused them would
// assert about the wrong object entirely.
type wireWorkFile struct {
	Path        string `json:"path"`
	Text        string `json:"text"`
	Fingerprint string `json:"fingerprint"`
	EOL         string `json:"eol"`
	MixedEOL    bool   `json:"mixed_eol"`
	Executable  bool   `json:"executable"`
	Bytes       int    `json:"bytes"`
}

type wireSave struct {
	File   wireWorkFile `json:"file"`
	Status wireStatus   `json:"status"`
}

type wirePreparedMessage struct {
	Text              string `json:"text"`
	Source            string `json:"source"`
	Signing           bool   `json:"signing"`
	SigningUnreadable string `json:"signing_unreadable"`
}

func putJSON(t *testing.T, handler http.Handler, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, target, strings.NewReader(body))
	request.Header.Set("X-Yagit-Token", testToken)
	request.Header.Set("Content-Type", "application/json")
	return execute(handler, request)
}

func readFileThrough(t *testing.T, handler http.Handler, id, path string) wireWorkFile {
	t.Helper()
	response := get(t, handler, "/api/repos/"+id+"/file?path="+path)
	if response.Code != http.StatusOK {
		t.Fatalf("reading %s: status = %d: %s", path, response.Code, response.Body)
	}
	return decode[wireWorkFile](t, response)
}

func saveBody(path, text, base string) string {
	body, err := json.Marshal(map[string]string{"path": path, "text": text, "base": base})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func TestReadingAndSavingAFile(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	opened := readFileThrough(t, handler, id, "a.txt")
	if opened.Text != "one\nTWO\nthree\nfour\n" {
		t.Fatalf("Text = %q", opened.Text)
	}
	if opened.Fingerprint == "" {
		t.Fatal("no fingerprint came back, so no save could be checked against it")
	}

	response := putJSON(t, handler, "/api/repos/"+id+"/file",
		saveBody("a.txt", "one\nTWO\nthree\nFOUR\n", opened.Fingerprint))
	if response.Code != http.StatusOK {
		t.Fatalf("saving: status = %d: %s", response.Code, response.Body)
	}

	saved := decode[wireSave](t, response)
	if got := readInRepo(t, path, "a.txt"); got != "one\nTWO\nthree\nFOUR\n" {
		t.Fatalf("on disk = %q", got)
	}
	// The fingerprint has to move, or the next save of the same buffer would
	// be refused as stale against content this one wrote.
	if saved.File.Fingerprint == opened.Fingerprint {
		t.Fatal("the fingerprint did not move")
	}

	// The status comes back with it, so the panel does not draw one frame of
	// the state it just left.
	if !saved.Status.file(t, "a.txt").Unstaged {
		t.Fatalf("the status beside the save does not report a.txt as changed: %+v", saved.Status.Files)
	}
}

// The guard that makes this safe to use in the middle of a merge: a save is
// only correct for the content it started from.
func TestSavingAgainstAFileThatMovedIsRefused(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	opened := readFileThrough(t, handler, id, "a.txt")
	writeInRepo(t, path, "a.txt", "something else entirely\n")

	response := putJSON(t, handler, "/api/repos/"+id+"/file",
		saveBody("a.txt", "one\n", opened.Fingerprint))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if got := readInRepo(t, path, "a.txt"); got != "something else entirely\n" {
		t.Fatalf("the refused save wrote anyway: %q", got)
	}
}

func TestSavingWithNoFingerprintIsRefused(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	response := putJSON(t, handler, "/api/repos/"+id+"/file", saveBody("a.txt", "one\n", ""))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if got := readInRepo(t, path, "a.txt"); got == "one\n" {
		t.Fatal("a save with no fingerprint wrote anyway")
	}
}

// `.git/config` names programs git runs — core.pager, core.fsmonitor. A
// browser that can write it can run anything on the next git command yagit
// issues, so this is a boundary and not a courtesy.
func TestTheGitDirectoryIsNotEditable(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	for _, target := range []string{".git/config", ".git/HEAD", ".git/hooks/pre-commit"} {
		response := get(t, handler, "/api/repos/"+id+"/file?path="+target)
		if response.Code != http.StatusForbidden && response.Code != http.StatusNotFound {
			t.Fatalf("reading %s: status = %d, want 403 or 404: %s", target, response.Code, response.Body)
		}
	}
}

func TestAPathThatLeavesTheRepositoryIsRefused(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/file?path=../outside.txt")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestABinaryFileIsRefusedWithAReason(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	writeInRepo(t, path, "logo.png", "\x89PNG\r\n\x1a\n\x00\x00")

	response := get(t, handler, "/api/repos/"+id+"/file?path=logo.png")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "not text") {
		t.Fatalf("the refusal does not say why: %s", response.Body)
	}
}

func TestAFileThatIsNotThereIsNotFound(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/file?path=nowhere.txt")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", response.Code, response.Body)
	}
}

// conflictedRepository opens a repository whose merge stopped on f.txt.
func conflictedRepository(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "conflicted")

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, path, "init", "-b", "main")
	writeInRepo(t, path, "f.txt", "one\ntwo\nthree\n")
	runGitIn(t, path, "add", "--", "f.txt")
	runGitIn(t, path, commitArgs("base")...)

	runGitIn(t, path, "checkout", "-b", "side")
	writeInRepo(t, path, "f.txt", "one\nSIDE\nthree\n")
	runGitIn(t, path, withIdentity("commit", "-am", "side")...)

	runGitIn(t, path, "checkout", "main")
	writeInRepo(t, path, "f.txt", "one\nMAIN\nthree\n")
	runGitIn(t, path, withIdentity("commit", "-am", "main")...)

	// Expected to fail: the stopped merge is the state under test. runGitIn
	// would fail the test, so this one is run directly.
	mergeConflict(t, path)

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}
	return handler, decode[wireRepo](t, response).ID, path
}

func TestTheStatusNamesTheOperationInProgress(t *testing.T) {
	handler, id, _ := conflictedRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/status")
	if response.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", response.Code, response.Body)
	}

	var payload struct {
		State struct {
			Operation string `json:"operation"`
		} `json:"state"`
		Files []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"files"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// `git status --porcelain` never says this, which is the whole reason the
	// daemon reads the marker files git itself reads.
	if payload.State.Operation != "merge" {
		t.Fatalf("state.operation = %q, want merge: %s", payload.State.Operation, response.Body)
	}
}

// The bug this replaces: git answers an unmerged path with a COMBINED diff,
// which is not a unified diff and cannot be staged line by line. The parser
// used to fail on the `@@@`, and the interface showed a message about a range
// over a file whose real problem was an unfinished merge.
func TestTheDiffOfAConflictedPathIsRefusedWithAReason(t *testing.T) {
	handler, id, _ := conflictedRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/diff?path=f.txt&side=unstaged")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "unmerged") {
		t.Fatalf("the refusal does not name the cause: %s", response.Body)
	}
}

func TestAConflictedFileIsReadWithItsMarkers(t *testing.T) {
	handler, id, _ := conflictedRepository(t)

	opened := readFileThrough(t, handler, id, "f.txt")
	for _, marker := range []string{"<<<<<<<", "=======", ">>>>>>>"} {
		if !strings.Contains(opened.Text, marker) {
			t.Fatalf("%q is not in what was read:\n%s", marker, opened.Text)
		}
	}
}

// The whole loop, end to end: read the conflicted file, edit the markers out,
// save it, stage it, and the repository stops being conflicted.
func TestResolvingAConflictByEditingAndStaging(t *testing.T) {
	handler, id, path := conflictedRepository(t)

	opened := readFileThrough(t, handler, id, "f.txt")

	response := putJSON(t, handler, "/api/repos/"+id+"/file",
		saveBody("f.txt", "one\nMAIN\nSIDE\nthree\n", opened.Fingerprint))
	if response.Code != http.StatusOK {
		t.Fatalf("saving: status = %d: %s", response.Code, response.Body)
	}
	if got := readInRepo(t, path, "f.txt"); got != "one\nMAIN\nSIDE\nthree\n" {
		t.Fatalf("on disk = %q", got)
	}

	staged := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["f.txt"]}`)
	if staged.Code != http.StatusOK {
		t.Fatalf("staging: status = %d: %s", staged.Code, staged.Body)
	}
	if kind := decode[wireStatus](t, staged).file(t, "f.txt").Kind; kind == "unmerged" {
		t.Fatal("still unmerged after the resolved file was staged")
	}
}

// `git add` on a file full of `<<<<<<<` succeeds, and git commits it without a
// word. The editing pane's own button is off while a marker is in the buffer;
// the list beside it offers the same operation in bulk, on files nobody
// opened, so the rule is made here where both of them pass.
func TestStagingAConflictedFileThatStillHasMarkersIsRefused(t *testing.T) {
	handler, id, path := conflictedRepository(t)

	staged := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["f.txt"]}`)
	if staged.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", staged.Code, staged.Body)
	}
	if !strings.Contains(staged.Body.String(), "conflict marker") {
		t.Fatalf("the refusal does not say what is wrong: %s", staged.Body)
	}

	// Refused before anything ran: the index is untouched, so the file is
	// still unmerged and the repository still refuses to commit.
	status := get(t, handler, "/api/repos/"+id+"/status")
	if kind := decode[wireStatus](t, status).file(t, "f.txt").Kind; kind != "unmerged" {
		t.Fatalf("kind = %q after a refused stage, want it left unmerged", kind)
	}

	// And the way through is the same one it always was.
	opened := readFileThrough(t, handler, id, "f.txt")
	saved := putJSON(t, handler, "/api/repos/"+id+"/file",
		saveBody("f.txt", "one\nMAIN\nthree\n", opened.Fingerprint))
	if saved.Code != http.StatusOK {
		t.Fatalf("saving: status = %d: %s", saved.Code, saved.Body)
	}
	if again := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["f.txt"]}`); again.Code != http.StatusOK {
		t.Fatalf("staging the resolved file: status = %d: %s", again.Code, again.Body)
	}
	if got := readInRepo(t, path, "f.txt"); got != "one\nMAIN\nthree\n" {
		t.Fatalf("on disk = %q", got)
	}
}

// A `<<<<<<<` in a file git does NOT call unmerged is text, and this project
// has some: the conflict parser documents the markers in a comment. Refusing
// to stage that would be refusing to commit the documentation.
func TestStagingAnOrdinaryFileHoldingAMarkerIsAllowed(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	writeInRepo(t, path, "docs.md", "git writes\n<<<<<<< HEAD\nand you fix it\n")

	staged := postJSON(t, handler, "/api/repos/"+id+"/stage", `{"paths":["docs.md"]}`)
	if staged.Code != http.StatusOK {
		t.Fatalf("status = %d, want it staged: %s", staged.Code, staged.Body)
	}
}

// The read cap and the save cap used to be independent numbers 32 times apart,
// so every file between them opened in the editor and answered the save with a
// 413 — losing a resolution that existed nowhere but the browser.
func TestAFileTooLargeForTheOldBodyCapCanStillBeSaved(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	// Comfortably over the 64 KiB every other route allows, comfortably under
	// what the pane will open.
	big := strings.Repeat("a line of a file somebody is editing\n", 4_000)
	writeInRepo(t, path, "big.txt", big)

	opened := readFileThrough(t, handler, id, "big.txt")
	if opened.Bytes < 64<<10 {
		t.Fatalf("the fixture is %d bytes, which does not reach the cap it is about", opened.Bytes)
	}

	response := putJSON(t, handler, "/api/repos/"+id+"/file",
		saveBody("big.txt", big+"one more line\n", opened.Fingerprint))
	if response.Code != http.StatusOK {
		t.Fatalf("saving: status = %d: %s", response.Code, response.Body)
	}
	if got := readInRepo(t, path, "big.txt"); got != big+"one more line\n" {
		t.Fatalf("on disk is %d bytes, want %d", len(got), len(big)+14)
	}
}

func TestTakingOneSideOfAConflictWhole(t *testing.T) {
	for _, testCase := range []struct {
		side string
		want string
	}{
		{"ours", "one\nMAIN\nthree\n"},
		{"theirs", "one\nSIDE\nthree\n"},
	} {
		t.Run(testCase.side, func(t *testing.T) {
			handler, id, path := conflictedRepository(t)

			response := postJSON(t, handler, "/api/repos/"+id+"/resolve",
				fmt.Sprintf(`{"paths":["f.txt"],"side":%q}`, testCase.side))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", response.Code, response.Body)
			}
			if got := readInRepo(t, path, "f.txt"); got != testCase.want {
				t.Fatalf("f.txt = %q, want %q", got, testCase.want)
			}

			for _, file := range decode[wireStatus](t, response).Files {
				if file.Path == "f.txt" && file.Kind == "unmerged" {
					t.Fatal("still unmerged after a side was taken")
				}
			}
		})
	}
}

func TestResolveRefusesASideItDoesNotKnow(t *testing.T) {
	handler, id, _ := conflictedRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/resolve", `{"paths":["f.txt"],"side":"mine"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

// Taking a side of a file that has no sides would run `git checkout --ours` on
// a path git reports as ordinary, which fails in a way that explains nothing.
func TestResolveRefusesAPathThatIsNotConflicted(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/resolve", `{"paths":["a.txt"],"side":"ours"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "unmerged") {
		t.Fatalf("the refusal does not say why: %s", response.Body)
	}
}

func TestThePreparedMessageOfAStoppedMergeIsGitsOwn(t *testing.T) {
	handler, id, _ := conflictedRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/prepared-message?amend=false")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	prepared := decode[wirePreparedMessage](t, response)
	if prepared.Source != "merge" {
		t.Fatalf("source = %q, want merge", prepared.Source)
	}
	if !strings.HasPrefix(prepared.Text, "Merge branch 'side'") {
		t.Fatalf("text = %q", prepared.Text)
	}
	// `git stripspace --strip-comments` is what removes the "# Conflicts:"
	// block. yagit commits with --cleanup=whitespace, which keeps '#' lines
	// because in a text box a '#' is text — so a block left in here would be
	// committed verbatim.
	if strings.Contains(prepared.Text, "Conflicts:") {
		t.Fatalf("the comment block survived: %q", prepared.Text)
	}
}

func TestThePreparedMessageOfAnAmendIsTheCommitBeingReplaced(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/prepared-message?amend=true")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	prepared := decode[wirePreparedMessage](t, response)
	if prepared.Source != "head" || prepared.Text != "first" {
		t.Fatalf("prepared = %+v, want the message of HEAD", prepared)
	}
}

func TestAnOrdinaryRepositoryPreparesNoMessage(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/prepared-message?amend=false")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	prepared := decode[wirePreparedMessage](t, response)
	if prepared.Text != "" || prepared.Source != "" {
		t.Fatalf("prepared = %+v, want nothing", prepared)
	}
}

// A decorative flag must not take the message down with it.
//
// `commit.gpgsign = maybe` is not a boolean, so `git config --type=bool` exits
// 128 rather than reporting the key unset. Before this the whole route
// answered 500 with it, and the MERGE_MSG a stopped merge had already read
// successfully went with it — leaving an error where git's own words for the
// commit belong, and no way to get them back.
func TestASigningConfigGitCannotParseStillAnswersWithTheMessage(t *testing.T) {
	handler, id, path := conflictedRepository(t)

	runGitIn(t, path, "config", "commit.gpgsign", "maybe")

	response := get(t, handler, "/api/repos/"+id+"/prepared-message?amend=false")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	prepared := decode[wirePreparedMessage](t, response)
	if !strings.HasPrefix(prepared.Text, "Merge branch 'side'") {
		t.Fatalf("text = %q, want git's own merge message", prepared.Text)
	}
	if prepared.Signing {
		t.Error("signing = true for a value git refused to read")
	}
	// Carried, not swallowed: the command, its exit code and git's own stderr
	// are what make this worth showing instead of "something failed".
	if !strings.Contains(prepared.SigningUnreadable, "commit.gpgsign") {
		t.Errorf("signing_unreadable = %q, want git's own complaint", prepared.SigningUnreadable)
	}
}

// And the ordinary case still answers the question rather than dodging it.
func TestThePreparedMessageSaysWhenTheCommitWillBeSigned(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	runGitIn(t, path, "config", "commit.gpgsign", "true")

	response := get(t, handler, "/api/repos/"+id+"/prepared-message?amend=false")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	prepared := decode[wirePreparedMessage](t, response)
	if !prepared.Signing || prepared.SigningUnreadable != "" {
		t.Errorf("prepared = %+v, want signing with nothing unreadable", prepared)
	}
}
