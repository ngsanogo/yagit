package api_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

// The half of the working directory that destroys something, and the half
// that decides what a route is allowed to read.
//
// Discarding by line is the one operation on this branch that removes work no
// git command can bring back, and it was the one with no test at all: it read
// the tracked diff whatever the status said, so every attempt on a new file
// compared a fingerprint against a diff that could never match — a conflict
// blaming the user's editor, on a request nothing was wrong with.

// changedIndices are the addressable lines of a diff, in order.
func changedIndices(diff wireDiff) []int {
	var indices []int
	for _, hunk := range diff.Hunks {
		for _, line := range hunk.Lines {
			if line.Kind != "context" {
				indices = append(indices, line.Index)
			}
		}
	}
	return indices
}

func readDiff(t *testing.T, handler http.Handler, id, path, side string) wireDiff {
	t.Helper()
	response := get(t, handler,
		"/api/repos/"+id+"/diff?side="+side+"&path="+strings.ReplaceAll(path, "/", "%2F"))
	if response.Code != http.StatusOK {
		t.Fatalf("diff of %s on %s: status = %d: %s", path, side, response.Code, response.Body)
	}
	return decode[wireDiff](t, response)
}

func TestDiscardingSomeLinesOfATrackedFile(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	diff := readDiff(t, handler, id, "a.txt", "unstaged")
	indices := changedIndices(diff)
	// The last change is the added "four"; throw that away and keep the
	// substitution the user still wants.
	last := indices[len(indices)-1]

	response := postJSON(t, handler, "/api/repos/"+id+"/discard", fmt.Sprintf(
		`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[%d]}}`, diff.ID, last))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if got := readInRepo(t, path, "a.txt"); got != "one\nTWO\nthree\n" {
		t.Errorf("work tree holds %q, expected the added line gone and the substitution kept", got)
	}
}

func TestDiscardingSomeLinesOfAnUntrackedFile(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	writeInRepo(t, path, "new.txt", "alpha\nbeta\ngamma\n")

	// The side the interface drew it on, which is the side the daemon has to
	// read it back on. Reading the tracked diff instead answers with nothing
	// for a file git has never seen, and nothing has a fingerprint of its own
	// that no selection can ever match.
	diff := readDiff(t, handler, id, "new.txt", "untracked")
	indices := changedIndices(diff)
	if len(indices) != 3 {
		t.Fatalf("expected three addable lines, got %v", indices)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/discard", fmt.Sprintf(
		`{"paths":["new.txt"],"lines":{"diff":%q,"indices":[%d]}}`, diff.ID, indices[1]))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if got := readInRepo(t, path, "new.txt"); got != "alpha\ngamma\n" {
		t.Errorf("work tree holds %q, expected the middle line gone", got)
	}
}

func TestDiscardingLinesAgainstAStaleDiffIsRefused(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	diff := readDiff(t, handler, id, "a.txt", "unstaged")
	indices := changedIndices(diff)

	// The file moves on under the selection: the line numbers no longer
	// describe it, and applying anyway is how a patch lands somewhere
	// plausible instead of where it was meant to.
	writeInRepo(t, path, "a.txt", "something else entirely\n")

	response := postJSON(t, handler, "/api/repos/"+id+"/discard", fmt.Sprintf(
		`{"paths":["a.txt"],"lines":{"diff":%q,"indices":[%d]}}`, diff.ID, indices[0]))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if got := readInRepo(t, path, "a.txt"); got != "something else entirely\n" {
		t.Errorf("the work tree was changed by a refused request: %q", got)
	}
}

func TestDiffRefusesAWindowsDriveLetter(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	// path.IsAbs knows only a leading slash, and `C:/…` is relative to it —
	// while a Windows daemon resolves it as an absolute path and hands git a
	// file anywhere on the machine. The daemon that reads the path is not
	// necessarily the one that wrote it, so the check cannot wait for GOOS.
	for _, candidate := range []string{
		"C:/Windows/win.ini",
		`C:\Windows\win.ini`,
		"C:evil",
		"c:/Users/victim/.ssh/id_rsa",
	} {
		target := "/api/repos/" + id + "/diff?side=untracked&path=" + urlEscape(candidate)
		if response := get(t, handler, target); response.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400: %s", candidate, response.Code, response.Body)
		}
	}
}

func TestTheUntrackedDiffOnlyReadsWhatGitCallsUntracked(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	// `git diff --no-index` deliberately ignores the index and the work-tree
	// registry: it opens the two files it is given and prints them. Ungated,
	// this route reads anything the daemon can reach — .git/config and the
	// remote URL in it, which routinely carries a token — and follows a
	// symlink the repository checked out straight past YAGIT_ROOT, which
	// SECURITY.md says only the open call can cross.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "id_ed25519"), []byte("TOP-SECRET-KEY\n"), 0o600); err != nil {
		t.Fatalf("writing the secret: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "keys")); err != nil {
		t.Skipf("this platform will not make a symlink: %v", err)
	}

	for _, candidate := range []string{".git/config", "keys/id_ed25519"} {
		target := "/api/repos/" + id + "/diff?side=untracked&path=" + urlEscape(candidate)
		response := get(t, handler, target)
		if response.Code == http.StatusOK {
			t.Errorf("%q was served: %s", candidate, response.Body)
			continue
		}
		if response.Code != http.StatusNotFound {
			t.Errorf("%q: status = %d, want 404: %s", candidate, response.Code, response.Body)
		}
	}
}

func TestDiscardingANestedRepositoryIsRefusedRatherThanAttempted(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	// `--untracked-files=all` expands untracked directories into their files,
	// with one exception: a directory holding its own `.git`. That arrives as
	// a single `nested/` entry, `git clean --force` on it exits 0 having done
	// nothing, and the flags that would make it work delete somebody else's
	// repository.
	nested := filepath.Join(path, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, nested, "init", "-b", "main")

	status := decode[wireStatus](t, get(t, handler, "/api/repos/"+id+"/status"))
	entry := status.file(t, "nested/")
	if entry.Kind != "untracked" {
		t.Fatalf("nested/ kind = %q, expected untracked", entry.Kind)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/discard", `{"paths":["nested/"]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Errorf("the nested repository was removed anyway: %v", err)
	}
}

func TestASelectionLargerThanARequestMaySaySoRatherThanLookMalformed(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	// One integer per changed line, so a single very large hunk goes over the
	// cap. "Unreadable request body" sends the reader looking for a typo in a
	// body whose only fault is its size.
	var indices strings.Builder
	for index := range 40_000 {
		if index > 0 {
			indices.WriteByte(',')
		}
		indices.WriteString(strconv.Itoa(index))
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/stage", fmt.Sprintf(
		`{"paths":["a.txt"],"lines":{"diff":"whatever","indices":[%s]}}`, indices.String()))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "whole file") {
		t.Errorf("the message does not say what to do instead: %s", response.Body)
	}
}

func TestAmendingReplacesTheCommitRatherThanAddingOne(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	runGitIn(t, path, "config", "user.name", "yagit Test")
	runGitIn(t, path, "config", "user.email", "test@yagit.local")
	runGitIn(t, path, "add", "--", "a.txt")

	before := commitCountIn(t, path)

	response := postJSON(t, handler, "/api/repos/"+id+"/commit",
		`{"message":"first, said again","amend":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}

	if after := commitCountIn(t, path); after != before {
		t.Errorf("HEAD has %d commits, expected the %d it started with", after, before)
	}
	if subject := headSubjectIn(t, path); subject != "first, said again" {
		t.Errorf("subject = %q, expected the amended message", subject)
	}
}

func TestCommittingWithoutAmendAddsACommit(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	runGitIn(t, path, "config", "user.name", "yagit Test")
	runGitIn(t, path, "config", "user.email", "test@yagit.local")
	runGitIn(t, path, "add", "--", "a.txt")

	before := commitCountIn(t, path)

	response := postJSON(t, handler, "/api/repos/"+id+"/commit", `{"message":"second"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body)
	}
	if after := commitCountIn(t, path); after != before+1 {
		t.Errorf("HEAD has %d commits, expected %d", after, before+1)
	}
}

// TestAChangeOnDiskReachesTheStream is the whole of what the filesystem watch
// promises: a commit made in the user's own terminal, with no request passing
// through the daemon, reaches the interface anyway.
//
// It goes through the HTTP routes rather than internal/watch directly, because
// what is worth proving is the wiring — that opening a repository starts a
// watch, that the watch follows the git directory and not the work tree beside
// it, and that what it sees is published. Watching the wrong directory is
// silent: no error, no log line, just an interface that never refreshes.
func TestAChangeOnDiskReachesTheStream(t *testing.T) {
	handler, root, _ := serverOnWatchedRoot(t, true)

	path := filepath.Join(root, "project")
	makeRepo(t, path)

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}
	id := decode[wireRepo](t, response).ID

	server := httptest.NewServer(handler)
	defer server.Close()

	reader, stream := openStream(t, server, "")
	// Closing the body is what ends the stream. The error is dropped because
	// a stream this test has finished reading may already be gone.
	defer func() { _ = stream.Body.Close() }()

	// A commit nobody asked the daemon for.
	commitEmpty(t, path, "made in a terminal")

	for {
		kind, data, err := nextEvent(reader)
		if err != nil {
			t.Fatalf("the commit made on disk was never announced: %v", err)
		}
		if kind != "repository" {
			continue
		}
		if !strings.Contains(data, id) {
			t.Fatalf("a repository event named something else: %q", data)
		}
		return
	}
}

// TestAReconnectingStreamIsGivenWhatItMissed pins the other half of ADR 0007's
// promise. A stream that falls behind is hung up on purpose so the browser
// reconnects with Last-Event-ID, and the replay is what makes that a recovery
// rather than a silent gap.
func TestAReconnectingStreamIsGivenWhatItMissed(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	server := httptest.NewServer(handler)
	defer server.Close()

	// Something to have missed, published while nothing was listening.
	if response := get(t, handler, "/api/repos/"+id+"/status"); response.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", response.Code, response.Body)
	}

	// A first connection replays nothing: it has just fetched the state it
	// needs through the ordinary routes, and the whole buffer would only make
	// it fetch that state again.
	fresh, freshResponse := openStream(t, server, "")
	defer func() { _ = freshResponse.Body.Close() }()
	if response := get(t, handler, "/api/repos/"+id+"/refs"); response.Code != http.StatusOK {
		t.Fatalf("refs: %d: %s", response.Code, response.Body)
	}
	_, first, err := nextEvent(fresh)
	if err != nil {
		t.Fatalf("a first connection was given nothing at all: %v", err)
	}
	if !strings.Contains(first, "for-each-ref") {
		t.Errorf("a first connection was replayed a backlog: %q", first)
	}
	// Hanging up, which is what the browser does before it comes back with
	// Last-Event-ID.
	if err := freshResponse.Body.Close(); err != nil {
		t.Fatalf("closing the first stream: %v", err)
	}

	// A reconnection from the very beginning asks for everything after event
	// zero, which is the whole buffer.
	resumed, resumedResponse := openStream(t, server, "0")
	defer func() { _ = resumedResponse.Body.Close() }()

	kind, data, err := nextEvent(resumed)
	if err != nil {
		t.Fatalf("nothing was replayed: %v", err)
	}
	if kind != "git" {
		t.Fatalf("event = %q, expected a replayed git command", kind)
	}

	// The very first event the daemon ever published, so nothing between it
	// and the reconnection was skipped. The id is what the browser sends back
	// next time, and it has to be the one the stream numbered the event with.
	var replayed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(data), &replayed); err != nil {
		t.Fatalf("unreadable event payload %q: %v", data, err)
	}
	if replayed.ID != "1" {
		t.Errorf("the replay begins at event %q, expected the first one: %q", replayed.ID, data)
	}
}

// streamDeadline bounds how long a test will wait on a stream. A stream that
// never delivers is exactly the failure these tests are for, and waiting for
// the whole test binary's timeout would report it as "panic: test timed out"
// with no clue which stream went quiet.
const streamDeadline = 20 * time.Second

// openStream opens /api/events, optionally resuming from an event id.
//
// The response comes back with the reader: closing the body is what ends the
// stream, and it belongs to the test that decides when that is.
func openStream(t *testing.T, server *httptest.Server, lastEventID string) (*bufio.Reader, *http.Response) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), streamDeadline)
	t.Cleanup(cancel)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("X-Yagit-Token", testToken)
	if lastEventID != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "retry: ") {
		t.Fatalf("the stream does not open with a reconnection delay: %q, %v", line, err)
	}

	return reader, response
}

func commitCountIn(t *testing.T, path string) int {
	t.Helper()
	return atoi(t, gitOutput(t, path, "rev-list", "--count", "HEAD"))
}

func headSubjectIn(t *testing.T, path string) string {
	t.Helper()
	return gitOutput(t, path, "log", "-1", "--pretty=format:%s")
}

func atoi(t *testing.T, text string) int {
	t.Helper()
	value, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("unreadable number %q: %v", text, err)
	}
	return value
}

// gitOutput runs git in a repository a test made and hands back what it said.
func gitOutput(t *testing.T, path string, arguments ...string) string {
	t.Helper()
	output, err := git.NewRunner(nil).Run(context.Background(), path, arguments...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output))
}

// urlEscape puts a path in a query string, slashes and all.
func urlEscape(path string) string { return url.QueryEscape(path) }

// nextEvent reads one SSE event, or says why it could not. Unlike readEvent it
// hands the failure back rather than ending the test, so a caller waiting for
// one particular kind of event can say what it was waiting for.
func nextEvent(reader *bufio.Reader) (kind, data string, err error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		line = strings.TrimRight(line, "\n")

		switch {
		case strings.HasPrefix(line, "event: "):
			kind = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && kind != "":
			return kind, data, nil
		}
	}
}

// TestTheStagedDiffOfARenameCarriesBothNames is the wiring between the status
// and the diff. A pathspec is applied before rename detection, so a diff
// limited to the new name alone cannot pair the two halves and git answers
// with `new file mode` and every line added: the row says "renamed" while the
// pane says "new file", nothing refuses to take it line by line, and unstaging
// one of those lines writes an index holding neither HEAD's content nor the
// work tree's. The old name has to come from the status, which is where git
// already reported it.
func TestTheStagedDiffOfARenameCarriesBothNames(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	runGitIn(t, path, "checkout", "--", "a.txt")
	runGitIn(t, path, "mv", "a.txt", "b.txt")
	writeInRepo(t, path, "b.txt", "one\nX\nthree\n")
	runGitIn(t, path, "add", "--all")

	status := decode[wireStatus](t, get(t, handler, "/api/repos/"+id+"/status"))
	if entry := status.file(t, "b.txt"); entry.OldPath != "a.txt" {
		t.Fatalf("the status reports b.txt as coming from %q, expected a.txt", entry.OldPath)
	}

	response := get(t, handler, "/api/repos/"+id+"/diff?side=staged&path=b.txt")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	diff := decode[struct {
		Path    string `json:"path"`
		OldPath string `json:"old_path"`
		Added   bool   `json:"added"`
	}](t, response)

	if diff.OldPath != "a.txt" {
		t.Errorf("old_path = %q, expected a.txt", diff.OldPath)
	}
	if diff.Added {
		t.Error("a renamed file was served as a new one")
	}
}

// TestUnstagingLinesOfARenameIsRefusedRatherThanApplied: a rename is a change
// to the index with no lines in it, and a patch carrying half of one either
// loses the rename or loses the content. Refusing is the only answer that is
// not a guess about what the user meant — and it only happens when the daemon
// can still see that the file was renamed, which is what re-reading the diff
// with both names is for.
func TestUnstagingLinesOfARenameIsRefusedRatherThanApplied(t *testing.T) {
	handler, id, path := openWorkingRepository(t)

	runGitIn(t, path, "checkout", "--", "a.txt")
	runGitIn(t, path, "mv", "a.txt", "b.txt")
	writeInRepo(t, path, "b.txt", "one\nX\nthree\n")
	runGitIn(t, path, "add", "--all")

	diff := readDiff(t, handler, id, "b.txt", "staged")
	indices := changedIndices(diff)
	if len(indices) == 0 {
		t.Fatalf("the staged rename has no addressable line: %+v", diff)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/unstage", fmt.Sprintf(
		`{"paths":["b.txt"],"lines":{"diff":%q,"indices":[%d]}}`, diff.ID, indices[0]))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "renamed") {
		t.Errorf("the message does not say why: %s", response.Body)
	}

	// And the index still holds what was staged: HEAD's line 3 among it.
	if staged := gitOutput(t, path, "show", ":b.txt"); staged != "one\nX\nthree" {
		t.Errorf("index holds %q, expected the staged rename untouched", staged)
	}
}
