package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/api"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
	"github.com/ngsanogo/yagit/internal/watch"
)

// auth_test.go pins who may call these routes. This file pins what they
// answer — including the part that decides which HTTP status a refusal from
// the security boundary becomes, which was covered by nothing.
//
// A status code is not decoration here. 403 says "this path is outside the
// root, and it will still be tomorrow"; 400 says "you typed it wrong". An
// interface that cannot tell those apart either nags the user to fix
// something that is not broken, or hides a boundary refusal behind a typo.

// wireRepo is the repository shape the HTTP API exposes.
//
// Written out here rather than decoded into repo.Repo, so that a field added
// to the registry does not reach a browser because somebody forgot to think
// about it: this type is the wire, and the test fails the day the two differ.
type wireRepo struct {
	ID       string `json:"id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Bare     bool   `json:"bare"`
	OpenedAt string `json:"opened_at"`
}

// serverOnRoot builds a server whose registry is rooted at a real directory,
// so repositories can actually be opened through the API.
func serverOnRoot(t *testing.T) (handler http.Handler, root string) {
	t.Helper()
	handler, root, _ = serverOnWatchedRoot(t, false)
	return handler, root
}

// serverOnWatchedRoot is serverOnRoot with the filesystem watch wired the way
// cmd/yagit wires it, and the stream it publishes into.
//
// A test server without a Watcher leaves everything the watch does unprovable
// above internal/watch: the daemon could watch the work tree instead of the
// git directory, or never start the watch at all, and every test would still
// pass while a commit made in the user's own terminal never reached the
// interface.
func serverOnWatchedRoot(t *testing.T, watching bool) (http.Handler, string, *api.EventStream) {
	t.Helper()

	// git here must read no configuration of the machine's own: a global
	// init.templateDir or commit.gpgsign would change what these repositories
	// come out as, and the test would pass or fail depending on whose laptop
	// ran it.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}

	// The observer is wired the way cmd/yagit wires it, and it has to be:
	// the promise that the log panel shows every command yagit runs is a
	// property of that wiring, not of the server, and a test server with a
	// silent Runner would let it break without a word.
	events := api.NewEventStream(slog.New(slog.DiscardHandler))
	runner := git.NewRunner(events.PublishExecution)

	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	var watcher *watch.Watcher
	if watching {
		watcher, err = watch.New(slog.New(slog.DiscardHandler))
		if err != nil {
			t.Fatalf("watch.New: %v", err)
		}
		t.Cleanup(func() {
			if err := watcher.Close(); err != nil {
				t.Errorf("closing the watcher: %v", err)
			}
		})
		// The same goroutine cmd/yagit runs: without it a change is seen and
		// never announced, which is half the wiring and looks like all of it.
		go func() {
			for change := range watcher.Changes() {
				events.PublishRepositoryChanged(change.RepositoryID)
			}
		}()
	}

	server, err := api.NewServer(api.Options{
		Registry:       registry,
		Runner:         runner,
		Token:          testToken,
		AllowedOrigins: []string{allowedOrigin},
		Logger:         slog.New(slog.DiscardHandler),
		Frontend:       http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Events:         events,
		Watcher:        watcher,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server.Handler(), root, events
}

// makeRepo creates a repository with one commit, so `git log` has something to
// report and `for-each-ref` has a branch to find.
func makeRepo(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}

	runner := git.NewRunner(nil)
	run := func(arguments ...string) {
		t.Helper()
		if _, err := runner.Run(context.Background(), path, arguments...); err != nil {
			t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
		}
	}

	run("init", "-b", "main")
	run(commitArgs("first commit")...)
}

// commitEmpty adds a commit to a repository a test already made.
func commitEmpty(t *testing.T, path, message string) {
	t.Helper()
	runGitIn(t, path, commitArgs(message)...)
}

// identityArgs pins the author, so the objects a test produces never depend on
// who ran it. Needed by every command that writes one, merge included.
//
// Unexported and used only through withIdentity: appending to a shared slice
// is how two tests come to overwrite each other's arguments, and it fails
// silently — this one happens to have no spare capacity today, so every append
// copies, and the day somebody adds a fifth element is the day that stops
// being true.
var identityArgs = []string{
	"-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
}

// withIdentity is a git command line with the pinned author in front of it.
func withIdentity(args ...string) []string {
	return append(slices.Clone(identityArgs), args...)
}

// configureIdentityIn writes the author into the repository's own config,
// which withIdentity above cannot do for the commands that matter here.
//
// The daemon builds its own command line. A test where the DAEMON commits —
// merging two diverged branches is the case — cannot put `-c user.name=…` in
// front of it, and the server's git reads no global configuration (see
// serverOnWatchedRoot), so there is no identity anywhere to find. git then
// exits 128 with "Committer identity unknown" before it does any of the work
// the test is about, which reads as the route being broken.
func configureIdentityIn(t *testing.T, path string) {
	t.Helper()

	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")
}

func commitArgs(message string) []string {
	return withIdentity("commit", "--allow-empty", "-m", message)
}

// mergeConflict runs a merge that is expected to stop, which runGitIn cannot
// do: it fails the test on a non-zero exit, and a non-zero exit is the state
// the conflicted fixtures are built to be in.
func mergeConflict(t *testing.T, path string) {
	t.Helper()

	command := exec.Command("git", withIdentity("merge", "side")...)
	command.Dir = path
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("git merge succeeded in %s, so there is no conflict to test:\n%s", path, output)
	}
}

// runGitIn runs a command in a repository a test already made.
func runGitIn(t *testing.T, path string, arguments ...string) {
	t.Helper()

	runner := git.NewRunner(nil)
	if _, err := runner.Run(context.Background(), path, arguments...); err != nil {
		t.Fatalf("git %s: %v", strings.Join(arguments, " "), err)
	}
}

func get(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("X-Yagit-Token", testToken)
	return execute(handler, request)
}

func postJSON(t *testing.T, handler http.Handler, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.Header.Set("X-Yagit-Token", testToken)
	request.Header.Set("Content-Type", "application/json")
	return execute(handler, request)
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatalf("decoding %q: %v", response.Body.String(), err)
	}
	return value
}

// decodeProgressDone reads the terminal "done" event from an NDJSON network
// response (fetch, pull, push). Progress lines are skipped; an error event
// fails the test the way a non-200 would.
func decodeProgressDone[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	if !strings.Contains(response.Header().Get("Content-Type"), "ndjson") {
		t.Fatalf("Content-Type = %q, want ndjson; body = %s",
			response.Header().Get("Content-Type"), response.Body.String())
	}

	var last T
	found := false
	for _, line := range strings.Split(strings.TrimSpace(response.Body.String()), "\n") {
		if line == "" {
			continue
		}
		var envelope struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("decoding progress line %q: %v", line, err)
		}
		switch envelope.Type {
		case "progress":
			continue
		case "error":
			t.Fatalf("progress stream ended in error: %s", line)
		case "done":
			if err := json.Unmarshal([]byte(line), &last); err != nil {
				t.Fatalf("decoding done event %q: %v", line, err)
			}
			found = true
		default:
			t.Fatalf("unknown progress event type %q in %s", envelope.Type, line)
		}
	}
	if !found {
		t.Fatalf("progress stream carried no done event: %s", response.Body.String())
	}
	return last
}

// decodeProgressError reads the terminal "error" event from an NDJSON network
// response. Used when git refused after the stream had already begun.
func decodeProgressError(t *testing.T, response *httptest.ResponseRecorder) wireError {
	t.Helper()
	if !strings.Contains(response.Header().Get("Content-Type"), "ndjson") {
		t.Fatalf("Content-Type = %q, want ndjson; body = %s",
			response.Header().Get("Content-Type"), response.Body.String())
	}

	var last wireError
	found := false
	for _, line := range strings.Split(strings.TrimSpace(response.Body.String()), "\n") {
		if line == "" {
			continue
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("decoding progress line %q: %v", line, err)
		}
		switch envelope.Type {
		case "progress":
			continue
		case "done":
			t.Fatalf("progress stream ended in done, want error: %s", line)
		case "error":
			if err := json.Unmarshal([]byte(line), &last); err != nil {
				t.Fatalf("decoding error event %q: %v", line, err)
			}
			found = true
		default:
			t.Fatalf("unknown progress event type %q in %s", envelope.Type, line)
		}
	}
	if !found {
		t.Fatalf("progress stream carried no error event: %s", response.Body.String())
	}
	return last
}

// ---------------------------------------------------------------------------
// GET /api/repos
// ---------------------------------------------------------------------------

// TestListingNoRepositoriesIsAnEmptyArray guards a JSON detail with real
// consequences. A nil slice marshals to `null`, and a client doing
// `repos.map(…)` on null throws — so the empty list has to stay an empty list
// all the way out.
func TestListingNoRepositoriesIsAnEmptyArray(t *testing.T) {
	handler, _ := serverOnRoot(t)

	response := get(t, handler, "/api/repos")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if body := strings.TrimSpace(response.Body.String()); !strings.Contains(body, `"repos":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", body)
	}
}

func TestDiscoverEmptyListIsAnEmptyArray(t *testing.T) {
	handler, _ := serverOnRoot(t)

	response := get(t, handler, "/api/repos/discover")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}
	if body := strings.TrimSpace(response.Body.String()); !strings.Contains(body, `"repos":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", body)
	}
}

func TestDiscoverListsRepositoriesUnderRoot(t *testing.T) {
	handler, root := serverOnRoot(t)

	pathA := filepath.Join(root, "alpha")
	pathB := filepath.Join(root, "nested", "beta")
	makeRepo(t, pathA)
	makeRepo(t, pathB)

	response := get(t, handler, "/api/repos/discover")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	payload := decode[struct {
		Repos       []repo.DiscoveredRepo `json:"repos"`
		ScannedFrom string                `json:"scanned_from"`
		Root        string                `json:"root"`
	}](t, response)

	if payload.Root != root {
		t.Errorf("root = %q, want %q", payload.Root, root)
	}
	names := make([]string, len(payload.Repos))
	for index, found := range payload.Repos {
		names[index] = found.Path
	}
	if !slices.Contains(names, pathA) || !slices.Contains(names, pathB) {
		t.Fatalf("repos = %v, want %q and %q", names, pathA, pathB)
	}
}

// The scan's account of itself, key by key.
//
// These names are written out a second time in web/src/api/types.ts, by hand,
// and a hint the interface never shows is as silent as the no-hint-at-all this
// route exists to replace. So the counters are decoded into a map and compared
// whole: a tag renamed on the Go side arrives as a key nothing asked for and a
// key nothing sent, and both halves fail here rather than in a browser. Field
// by field, a renamed tag was a zero, and a zero is what six of the seven
// counters legitimately are.
func TestDiscoverReportsWhyItFoundNothing(t *testing.T) {
	handler, root := serverOnRoot(t)

	makeRepo(t, filepath.Join(root, ".hidden", "project"))
	makeRepo(t, filepath.Join(root, "node_modules", "project"))
	// Four levels is the default, and this repository is on the fifth.
	makeRepo(t, filepath.Join(root, "deep", "a", "b", "c", "project"))
	// A `.git` nothing wrote into: git knows the name and then refuses to
	// call the directory a repository.
	if err := os.MkdirAll(filepath.Join(root, "broken", ".git"), 0o755); err != nil {
		t.Fatalf("creating the broken repository: %v", err)
	}

	response := get(t, handler, "/api/repos/discover")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	payload := decode[struct {
		Repos      []repo.DiscoveredRepo `json:"repos"`
		Depth      int                   `json:"depth"`
		DepthLimit int                   `json:"depth_limit"`
		Skipped    map[string]int        `json:"skipped"`
	}](t, response)

	if len(payload.Repos) != 0 {
		t.Fatalf("repos = %v, want none: every one of them sits behind a skip", payload.Repos)
	}

	// The whole object, with a count of its own for every reason the fixture
	// can actually produce. Unreadable is the one it cannot: a directory the
	// daemon may not open is a permission the test suite would have to be
	// able to take away, and it cannot on every platform it runs on.
	skipped := map[string]int{
		"unreadable":       0,
		"too_deep":         1,
		"ignored_name":     1,
		"dotted":           1,
		"worktrees":        0,
		"submodules":       0,
		"not_a_repository": 1,
	}
	if !maps.Equal(payload.Skipped, skipped) {
		t.Errorf("skipped = %v, want %v", payload.Skipped, skipped)
	}

	if payload.Depth <= 0 || payload.DepthLimit < payload.Depth {
		t.Errorf("depth = %d, depth_limit = %d, want a depth the control can draw",
			payload.Depth, payload.DepthLimit)
	}

	// Iterated without a check on the other side, like every other list here.
	if body := strings.TrimSpace(response.Body.String()); !strings.Contains(body, `"submodule_failures":[]`) {
		t.Errorf("body = %s, want an empty array rather than null", body)
	}
}

// A superproject the scan could not finish, in the shape the interface draws.
//
// It travels on a 200, beside the repositories that were found, and what it
// carries is the whole of the point: without the command, the exit code and
// git's own stderr this is "Something went wrong" with a path beside it.
func TestDiscoverReportsASubmoduleFailureOnTheWire(t *testing.T) {
	handler, root := serverOnRoot(t)

	child := filepath.Join(root, "child")
	makeRepo(t, child)

	superPath := filepath.Join(root, "super")
	makeRepo(t, superPath)
	addSubmodule(t, superPath, child, "sub")

	// git parses .gitmodules before it runs foreach over an initialised
	// submodule, and refuses a file it cannot parse.
	if err := os.WriteFile(filepath.Join(superPath, ".gitmodules"), []byte("nonsense\n"), 0o600); err != nil {
		t.Fatalf("writing .gitmodules: %v", err)
	}

	response := get(t, handler, "/api/repos/discover?include_submodules=true")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: a broken .gitmodules is not a failed request: %s",
			response.Code, response.Body)
	}

	payload := decode[struct {
		Repos             []repo.DiscoveredRepo `json:"repos"`
		SubmoduleFailures []struct {
			Path    string `json:"path"`
			Message string `json:"message"`
			Git     *struct {
				Command  string   `json:"command"`
				Args     []string `json:"args"`
				ExitCode int      `json:"exit_code"`
				Stderr   string   `json:"stderr"`
			} `json:"git"`
		} `json:"submodule_failures"`
	}](t, response)

	if len(payload.Repos) == 0 {
		t.Error("repos = none: one broken .gitmodules must not hide every repository on the disk")
	}
	if len(payload.SubmoduleFailures) != 1 {
		t.Fatalf("submodule_failures = %+v, want exactly the superproject", payload.SubmoduleFailures)
	}

	failure := payload.SubmoduleFailures[0]
	if failure.Path != superPath {
		t.Errorf("path = %q, want %q", failure.Path, superPath)
	}
	if failure.Message == "" {
		t.Error("message is empty, and it is the line shown wherever git is not what refused")
	}
	if failure.Git == nil {
		t.Fatal("git is absent, so nothing on the screen can say what ran or what it answered")
	}
	if !strings.Contains(failure.Git.Command, "submodule foreach") {
		t.Errorf("command = %q, want the submodule listing that failed", failure.Git.Command)
	}
	if len(failure.Git.Args) == 0 {
		t.Error("args is empty, and it is what the command line is built from")
	}
	if failure.Git.ExitCode == 0 {
		t.Error("exit_code = 0 for a command that failed")
	}
	if strings.TrimSpace(failure.Git.Stderr) == "" {
		t.Error("stderr is empty, which is the half of a git failure a user can act on")
	}
}

// addSubmodule adds one repository inside another and commits the result.
//
// protocol.file.allow: git has refused a file:// submodule by default since
// CVE-2022-39253, and every submodule in these tests is one.
func addSubmodule(t *testing.T, parent, child, at string) {
	t.Helper()
	runGitIn(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", child, at)
	runGitIn(t, parent, withIdentity("commit", "-am", "track "+at)...)
}

// ?depth= has been on the route since the scan existed. It is worth one test
// that it reaches the walk, now that something sets it.
func TestDiscoverHonoursTheDepthAsked(t *testing.T) {
	handler, root := serverOnRoot(t)
	makeRepo(t, filepath.Join(root, "one", "two", "project"))

	payload := decode[struct {
		Repos []repo.DiscoveredRepo `json:"repos"`
		Depth int                   `json:"depth"`
	}](t, get(t, handler, "/api/repos/discover?depth=1"))

	if payload.Depth != 1 {
		t.Errorf("depth = %d, want the 1 that was asked for", payload.Depth)
	}
	if len(payload.Repos) != 0 {
		t.Errorf("repos = %v, want none at depth 1", payload.Repos)
	}
}

func TestDiscoverRefusesDirectoryOutsideRoot(t *testing.T) {
	handler, _ := serverOnRoot(t)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	makeRepo(t, outside)

	response := get(t, handler, "/api/repos/discover?dir="+url.QueryEscape(outside))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}
}

func TestOpeningARepositoryThenListingIt(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path))
	if opened.Code != http.StatusCreated {
		t.Fatalf("open: status = %d, want 201: %s", opened.Code, opened.Body)
	}

	created := decode[wireRepo](t, opened)
	if created.ID == "" {
		t.Error("the opened repository has no identifier, which is all the client gets to address it by")
	}
	if created.Name != "project" {
		t.Errorf("name = %q, want the directory's", created.Name)
	}
	// The tab bar has nothing else to tell two repositories called "project"
	// apart, and it is the same string /api/repos/discover reports.
	if created.Path != path {
		t.Errorf("path = %q, want %q", created.Path, path)
	}

	listed := decode[struct {
		Repos []wireRepo `json:"repos"`
	}](t, get(t, handler, "/api/repos"))

	if len(listed.Repos) != 1 || listed.Repos[0].ID != created.ID {
		t.Errorf("the listing does not contain the repository just opened: %+v", listed.Repos)
	}
}

// ---------------------------------------------------------------------------
// POST /api/repos — every refusal, and the status it becomes
// ---------------------------------------------------------------------------

func TestOpenRefusalsCarryTheRightStatus(t *testing.T) {
	handler, root := serverOnRoot(t)

	insideButNotARepo := filepath.Join(root, "plain-directory")
	if err := os.MkdirAll(insideButNotARepo, 0o755); err != nil {
		t.Fatalf("creating: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	makeRepo(t, outside)

	cases := []struct {
		name string
		body string
		want int
		why  string
	}{
		{
			name: "outside the allowed root",
			body: fmt.Sprintf(`{"path":%q}`, outside),
			want: http.StatusForbidden,
			why:  "a permanent refusal to explain, not a typo to fix",
		},
		{
			name: "a relative path",
			body: `{"path":"some/relative/path"}`,
			want: http.StatusBadRequest,
			why:  "relative to what? the daemon's working directory is not the client's",
		},
		{
			name: "a path that does not exist",
			body: fmt.Sprintf(`{"path":%q}`, filepath.Join(root, "absent")),
			want: http.StatusBadRequest,
		},
		{
			name: "a directory that is not a repository",
			body: fmt.Sprintf(`{"path":%q}`, insideButNotARepo),
			want: http.StatusBadRequest,
		},
		{
			name: "no path at all",
			body: `{}`,
			want: http.StatusBadRequest,
		},
		{
			name: "an empty path",
			body: `{"path":""}`,
			want: http.StatusBadRequest,
		},
		{
			// A client that sends "paht" learns it here rather than wondering
			// later why its path was never read.
			name: "a misspelt field",
			body: `{"paht":"/somewhere"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "not JSON at all",
			body: `this is not json`,
			want: http.StatusBadRequest,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := postJSON(t, handler, "/api/repos", testCase.body)

			if response.Code != testCase.want {
				t.Errorf("status = %d, want %d (%s): %s",
					response.Code, testCase.want, testCase.why, response.Body)
			}

			// Every refusal is JSON with a message. An empty body, or HTML,
			// leaves the interface with nothing to show but the number.
			failure := decode[struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}](t, response)
			if failure.Error.Message == "" {
				t.Errorf("the refusal carries no message: %s", response.Body)
			}
		})
	}
}

// TestOpeningTheSameRepositoryTwiceIsIdempotent covers what a second tab on
// the same repository does: the same entry, not a second one under a new id.
func TestOpeningTheSameRepositoryTwiceIsIdempotent(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	body := fmt.Sprintf(`{"path":%q}`, path)
	first := decode[wireRepo](t, postJSON(t, handler, "/api/repos", body))
	second := decode[wireRepo](t, postJSON(t, handler, "/api/repos", body))

	if first.ID != second.ID {
		t.Errorf("two ids for one repository: %q then %q", first.ID, second.ID)
	}

	listed := decode[struct {
		Repos []wireRepo `json:"repos"`
	}](t, get(t, handler, "/api/repos"))
	if len(listed.Repos) != 1 {
		t.Errorf("the repository was opened %d times", len(listed.Repos))
	}
}

// TestOpeningARepositoryBySubdirectory covers letting git decide where a
// repository starts: someone pastes a path from deep inside their tree, and
// the answer is the repository, not a refusal.
func TestOpeningARepositoryBySubdirectory(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	nested := filepath.Join(path, "internal", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("creating: %v", err)
	}

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, nested)))
	if opened.Name != "project" {
		t.Errorf("name = %q, want the repository directory name", opened.Name)
	}
}

// ---------------------------------------------------------------------------
// GET /api/repos/{id}/commits and /refs
// ---------------------------------------------------------------------------

func TestCommitsAndRefsOfAnOpenRepository(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	commitsResponse := get(t, handler, "/api/repos/"+opened.ID+"/commits")
	if commitsResponse.Code != http.StatusOK {
		t.Fatalf("commits: status = %d: %s", commitsResponse.Code, commitsResponse.Body)
	}
	commits := decode[struct {
		Commits []git.Commit `json:"commits"`
	}](t, commitsResponse)

	if len(commits.Commits) != 1 {
		t.Fatalf("expected the one commit made, got %d", len(commits.Commits))
	}
	if commits.Commits[0].Subject != "first commit" {
		t.Errorf("subject = %q", commits.Commits[0].Subject)
	}
	if commits.Commits[0].Author != "Ada Lovelace" {
		t.Errorf("author = %q", commits.Commits[0].Author)
	}
	if commits.Commits[0].Date.IsZero() {
		t.Error("the commit has no date; the whole graph is ordered by it")
	}

	refsResponse := get(t, handler, "/api/repos/"+opened.ID+"/refs")
	if refsResponse.Code != http.StatusOK {
		t.Fatalf("refs: status = %d: %s", refsResponse.Code, refsResponse.Body)
	}
	refs := decode[wireRefs](t, refsResponse)

	if len(refs.Refs) != 1 {
		t.Fatalf("expected the one branch, got %+v", refs.Refs)
	}
	if refs.Refs[0].ShortName != "main" || refs.Refs[0].Kind != git.RefBranch {
		t.Errorf("ref = %+v, want the branch main", refs.Refs[0])
	}

	// `for-each-ref` lists no HEAD, so without this the sidebar cannot mark
	// the branch that is checked out — the one fact about a ref list that
	// changes under the user's feet.
	if refs.Head == nil {
		t.Fatal("the refs carry no HEAD, and the sidebar has nothing to mark")
	}
	if refs.Head.Name != "main" || refs.Head.Detached {
		t.Errorf("head = %+v, want main, attached", *refs.Head)
	}
	if refs.Head.SHA != commits.Commits[0].SHA {
		t.Errorf("head sha = %q, want the tip %q", refs.Head.SHA, commits.Commits[0].SHA)
	}
}

// wireRefs is the answer of the refs route, HEAD included. Written out rather
// than decoded into the daemon's own types, for the same reason wireRepo is.
type wireRefs struct {
	Refs []git.Ref `json:"refs"`
	Head *struct {
		SHA      string `json:"sha"`
		Name     string `json:"name"`
		Detached bool   `json:"detached"`
	} `json:"head"`
}

// wireCommit is the answer of the single-commit route.
// wireCommit is the answer of the one-commit route: the commit's own fields at
// the top level, plus where it sits in the walk that was asked for.
type wireCommit struct {
	git.Commit
	Body string `json:"body"`
	Row  int    `json:"row"`
	Lane int    `json:"lane"`
}

// TestADetachedHEADIsReportedAsDetached covers the state a CI checkout leaves a
// repository in. No branch is current there, and marking one would name a
// branch nobody is on.
func TestADetachedHEADIsReportedAsDetached(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)
	runGitIn(t, path, "checkout", "--detach", "HEAD")

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))
	refs := decode[wireRefs](t, get(t, handler, "/api/repos/"+opened.ID+"/refs"))

	if refs.Head == nil {
		t.Fatal("a detached HEAD is still a HEAD, and the sidebar has to show it")
	}
	if !refs.Head.Detached || refs.Head.Name != "HEAD" {
		t.Errorf("head = %+v, want a detached HEAD", *refs.Head)
	}
}

// TestARepositoryWithNoCommitHasNoHEAD pins the state right after git init.
// An object with an empty name would read as a branch called nothing.
func TestARepositoryWithNoCommitHasNoHEAD(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "fresh")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	runGitIn(t, path, "init", "-b", "main")

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))
	response := get(t, handler, "/api/repos/"+opened.ID+"/refs")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if refs := decode[wireRefs](t, response); refs.Head != nil {
		t.Errorf("head = %+v, expected none at all", *refs.Head)
	}
}

// ---------------------------------------------------------------------------
// GET /api/repos/{id}/commits/{sha}
// ---------------------------------------------------------------------------

// TestACommitIsLocatedAndCarriesTheRestOfItsMessage covers the read behind two
// things the interface could not do: follow a reference to its commit, and
// show more of a commit than the row has room for.
//
// The row is asserted for every commit, not only for the tip: a location that
// ignored the order would pass on a one-commit history and send every click to
// the top of the list.
func TestACommitIsLocatedAndCarriesTheRestOfItsMessage(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)
	runGitIn(t, path, withIdentity("commit", "--allow-empty", "-m", "second commit", "-m", "why it was made")...)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))
	history := decode[historyPayload](t, get(t, handler, "/api/repos/"+opened.ID+"/commits"))
	if len(history.Commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(history.Commits))
	}

	for row, commit := range history.Commits {
		located := decode[wireCommit](t, get(t, handler, "/api/repos/"+opened.ID+"/commits/"+commit.SHA))
		if located.Row != row {
			t.Errorf("%s: row = %d, want %d", commit.Subject, located.Row, row)
		}
		if located.SHA != commit.SHA {
			t.Errorf("sha = %q, want %q", located.SHA, commit.SHA)
		}
		if located.Lane != commit.Lane {
			t.Errorf("lane = %d, want %d", located.Lane, commit.Lane)
		}
	}

	newest := decode[wireCommit](t, get(t, handler, "/api/repos/"+opened.ID+"/commits/"+history.Commits[0].SHA))
	if newest.Body != "why it was made" {
		t.Errorf("body = %q, want the paragraph under the subject", newest.Body)
	}

	// A message that is a subject and nothing else has no body, and the empty
	// string is the answer — not the subject repeated.
	oldest := decode[wireCommit](t, get(t, handler, "/api/repos/"+opened.ID+"/commits/"+history.Commits[1].SHA))
	if oldest.Body != "" {
		t.Errorf("body = %q, want nothing", oldest.Body)
	}
}

// TestACommitOutsideTheHistoryIs404 covers a reference pointing at an object
// the walk never reached. The interface has to be able to say that the commit
// is not in the picture being drawn, rather than scrolling to a row that does
// not exist — and the sentence names the walk, because under the default scope
// the answer is often "switch to every reference".
func TestACommitOutsideTheHistoryIs404(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))
	response := get(t, handler,
		"/api/repos/"+opened.ID+"/commits/0000000000000000000000000000000000000000")

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "is not in the history drawn from") {
		t.Errorf("the refusal does not say what is wrong: %s", response.Body)
	}
}

// TestTheRefsRouteFollowsTheRepository pins what the sidebar is allowed to
// show: what the repository holds now, not what it held when something else
// was read.
//
// It exists because the route has already been through one implementation
// that could fall behind. The refs came from the history store for a while,
// and the store keeps them only as long as its fingerprint holds — names and
// SHAs — so the second half of this test, where nothing moves at all, is the
// case that version had to be written around. Reading through the runner
// makes it true by construction, and this is what says so out loud.
func TestTheRefsRouteFollowsTheRepository(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	shortNames := func() []string {
		t.Helper()
		response := get(t, handler, "/api/repos/"+opened.ID+"/refs")
		if response.Code != http.StatusOK {
			t.Fatalf("refs: status = %d: %s", response.Code, response.Body)
		}
		payload := decode[struct {
			Refs []git.Ref `json:"refs"`
		}](t, response)

		names := make([]string, 0, len(payload.Refs))
		for _, ref := range payload.Refs {
			names = append(names, ref.ShortName)
		}
		slices.Sort(names)
		return names
	}

	if names := shortNames(); !slices.Equal(names, []string{"main"}) {
		t.Fatalf("refs = %v, want the one branch", names)
	}

	runGitIn(t, path, "branch", "feature")

	if names := shortNames(); !slices.Equal(names, []string{"feature", "main"}) {
		t.Errorf("refs = %v after a branch was made, want both", names)
	}

	// And the change that moves nothing. Configuring a branch's upstream
	// writes no ref and touches no object: every name and every SHA is what it
	// was a moment ago, so a fingerprint over those two sees an unchanged
	// repository — and the sidebar draws the tracking branch the user has just
	// replaced. The remote-tracking ref and the refspec are put in place
	// before the first read, so the only thing that differs between the two is
	// the configuration.
	runGitIn(t, path, "update-ref", "refs/remotes/origin/main", "refs/heads/main")
	runGitIn(t, path, "config", "remote.origin.url", ".")
	runGitIn(t, path, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")

	if upstream := upstreamOf(t, handler, opened.ID, "main"); upstream != "" {
		t.Fatalf("main tracks %q before anything configured it to track", upstream)
	}

	runGitIn(t, path, "config", "branch.main.remote", "origin")
	runGitIn(t, path, "config", "branch.main.merge", "refs/heads/main")

	if upstream := upstreamOf(t, handler, opened.ID, "main"); upstream != "refs/remotes/origin/main" {
		t.Errorf("main tracks %q, want refs/remotes/origin/main", upstream)
	}
}

// upstreamOf reads one branch's upstream out of the refs route.
func upstreamOf(t *testing.T, handler http.Handler, identifier, shortName string) string {
	t.Helper()

	response := get(t, handler, "/api/repos/"+identifier+"/refs")
	if response.Code != http.StatusOK {
		t.Fatalf("refs: status = %d: %s", response.Code, response.Body)
	}
	payload := decode[struct {
		Refs []git.Ref `json:"refs"`
	}](t, response)

	for _, ref := range payload.Refs {
		if ref.ShortName == shortName && ref.Kind == git.RefBranch {
			return ref.Upstream
		}
	}
	t.Fatalf("no branch %q among %+v", shortName, payload.Refs)
	return ""
}

// TestHistoryOfAnUnknownRepositoryIs404 covers both history routes at once:
// an identifier that designates nothing grants access to nothing, and the
// message says what to do about it.
func TestHistoryOfAnUnknownRepositoryIs404(t *testing.T) {
	handler, _ := serverOnRoot(t)

	for _, route := range []string{
		"/api/repos/deadbeef/commits",
		"/api/repos/deadbeef/commits/f1e2d3c",
		"/api/repos/deadbeef/refs",
	} {
		response := get(t, handler, route)

		if response.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", route, response.Code)
		}
		if !strings.Contains(response.Body.String(), "POST /api/repos") {
			t.Errorf("%s: the refusal does not say how to open it: %s", route, response.Body)
		}
	}
}

// ---------------------------------------------------------------------------
// The graph, and the paging that carries it
// ---------------------------------------------------------------------------

// historyPayload is the shape of an answer from the commits route. Declared
// here rather than reached for through a map so that a test reads like the
// contract it is checking.
type historyPayload struct {
	Commits []struct {
		git.Commit
		Lane int `json:"lane"`
	} `json:"commits"`
	Edges []struct {
		From     int `json:"from"`
		FromLane int `json:"from_lane"`
		To       int `json:"to"`
		ToLane   int `json:"to_lane"`
		Lane     int `json:"lane"`
	} `json:"edges"`
	First    int `json:"first"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
	Width    int `json:"width"`
}

// TestCommitsCarryTheGraph checks that the picture travels with the rows it
// describes, on a history that actually has a shape:
//
//	main:     A ─── C ─── M
//	            ╲       ╱
//	feature:      B ───
func TestCommitsCarryTheGraph(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path) // A

	runGitIn(t, path, "checkout", "-b", "feature")
	commitEmpty(t, path, "B")
	runGitIn(t, path, "checkout", "main")
	commitEmpty(t, path, "C")
	runGitIn(t, path, withIdentity("merge", "--no-ff", "-m", "M", "feature")...)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))
	history := decode[historyPayload](t, get(t, handler, "/api/repos/"+opened.ID+"/commits"))

	if history.Total != 4 {
		t.Fatalf("total = %d, expected 4 (A, B, C, M)", history.Total)
	}
	if history.Width != 2 {
		t.Errorf("width = %d: one branch beside the trunk needs two columns", history.Width)
	}
	if history.First != 0 {
		t.Errorf("first = %d, expected 0", history.First)
	}
	if history.PageSize <= 0 {
		t.Errorf("page_size = %d: the interface reads it to know how to page", history.PageSize)
	}

	// The merge is the newest row, in the leftmost column, and two lines
	// leave its dot — one of them down a column of its own.
	if subject := history.Commits[0].Subject; subject != "M" {
		t.Fatalf("the newest row is %q, expected the merge M", subject)
	}
	if lane := history.Commits[0].Lane; lane != 0 {
		t.Errorf("the merge sits in column %d, expected 0", lane)
	}

	branch := 0
	for _, edge := range history.Edges {
		if edge.From != 0 {
			continue
		}
		if edge.FromLane != 0 {
			t.Errorf("a line leaving the merge starts in column %d, expected 0", edge.FromLane)
		}
		if edge.Lane != 0 {
			branch++
		}
	}
	if branch != 1 {
		t.Errorf("%d lines leave the merge down a column of their own, expected 1", branch)
	}

	// Every line ends on a row of the history, and none points back up the
	// screen: an edge that did would draw a child below its own parent.
	for _, edge := range history.Edges {
		if edge.To == -1 {
			t.Errorf("this history is complete, so no line may leave the bottom: %+v", edge)
			continue
		}
		if edge.To <= edge.From {
			t.Errorf("%+v points back up the screen", edge)
		}
		if edge.ToLane != history.Commits[edge.To].Lane {
			t.Errorf("%+v lands in column %d, but row %d sits in column %d",
				edge, edge.ToLane, edge.To, history.Commits[edge.To].Lane)
		}
	}
}

// TestAPagePastTheEndIsEmptyRatherThanAnError: the history shrinks whenever a
// branch is deleted, so a client asking for a page that existed a moment ago
// is not making a mistake. It gets an empty page and the new total, which is
// what it needs to correct itself.
func TestAPagePastTheEndIsEmptyRatherThanAnError(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	response := get(t, handler, "/api/repos/"+opened.ID+"/commits?page=9")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	history := decode[historyPayload](t, response)
	if len(history.Commits) != 0 {
		t.Errorf("expected no row past the end, got %d", len(history.Commits))
	}
	if history.Total != 1 {
		t.Errorf("total = %d, expected 1 even past the end", history.Total)
	}
	// A list, never null: an interface handed null where it expected a list
	// is an interface that crashes on an empty page.
	if !bytes.Contains(response.Body.Bytes(), []byte(`"commits":[]`)) {
		t.Errorf("an empty page must send an empty list, got %s", response.Body)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"edges":[]`)) {
		t.Errorf("an empty page must send an empty edge list, got %s", response.Body)
	}
}

// TestAPageThatIsNotAPageIsRefused. Rounding a nonsense page to the first one
// would answer a plausible list to a client that computed its request wrong,
// and hide the defect for good.
func TestAPageThatIsNotAPageIsRefused(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	for _, query := range []string{"?page=abc", "?page=-1", "?page=", "?page=1.5"} {
		response := get(t, handler, "/api/repos/"+opened.ID+"/commits"+query)
		if query == "?page=" {
			// An empty value is a client that built a URL without one, which
			// is the first page — not a number it got wrong.
			if response.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want 200", query, response.Code)
			}
			continue
		}
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", query, response.Code, response.Body)
		}
	}
}

// A page number so large that page*pageSize wraps.
//
// It is the one wrong answer worth a test of its own: the product overflows to
// a negative row, the store clamps a negative row to zero, and the daemon
// answers the FIRST page — the newest commits, which look exactly like a
// correct answer. Every other bad page number is visibly refused; this one was
// silently agreed with.
func TestAPageNumberThatWouldOverflowIsRefused(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[repo.Repo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	response := get(t, handler,
		fmt.Sprintf("/api/repos/%s/commits?page=%d", opened.ID, math.MaxInt64/2))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if bytes.Contains(response.Body.Bytes(), []byte(`"first":0`)) {
		t.Errorf("an overflowing page must be refused, not answered with the first: %s", response.Body)
	}
}

// TestTheDefaultScopeIsWhatIsCheckedOut: the graph used to be
// `git log --all` and nothing else, which is what made a repository with a
// thousand tags too wide to draw.
func TestTheDefaultScopeIsWhatIsCheckedOut(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path) // A, on main

	// A branch that was never merged: reachable from a ref, not from HEAD.
	runGitIn(t, path, "checkout", "-b", "spare")
	commitEmpty(t, path, "B")
	runGitIn(t, path, "checkout", "main")

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	checkedOut := decode[historyPayload](t, get(t, handler, "/api/repos/"+opened.ID+"/commits"))
	if checkedOut.Total != 1 {
		t.Errorf("total = %d with no scope asked for, expected 1: B lives only on spare",
			checkedOut.Total)
	}

	everything := decode[historyPayload](t,
		get(t, handler, "/api/repos/"+opened.ID+"/commits?scope=all"))
	if everything.Total != 2 {
		t.Errorf("total = %d with scope=all, expected 2 (A, B)", everything.Total)
	}

	// Naming the default explicitly is the same request, so the interface can
	// send what it holds rather than deciding when to leave the parameter out.
	named := decode[historyPayload](t,
		get(t, handler, "/api/repos/"+opened.ID+"/commits?scope=head"))
	if named.Total != checkedOut.Total {
		t.Errorf("total = %d with scope=head, expected %d", named.Total, checkedOut.Total)
	}
}

// TestAScopeThatNamesNoRefsIsRefused. Reading an unknown scope as the default
// would answer a history that looks entirely correct to a client that asked
// for a different one — the same reason a nonsense page number is refused
// rather than rounded to the first.
func TestAScopeThatNamesNoRefsIsRefused(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	for _, query := range []string{"?scope=everything", "?scope=--all", "?scope=HEAD"} {
		response := get(t, handler, "/api/repos/"+opened.ID+"/commits"+query)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", query, response.Code, response.Body)
		}
	}
}

// TestHistoryOfARepositoryReplacedUnderneathIs409 covers the one refusal that
// is neither "you asked for the wrong thing" nor "you may not have it".
//
// The identifier still resolves, but not to the repository it was opened on.
// 409 is the difference between "that never existed" and "that is not what it
// was" — and the second is the one worth interrupting the user for.
func TestHistoryOfARepositoryReplacedUnderneathIs409(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("removing: %v", err)
	}
	makeRepo(t, path)

	response := get(t, handler, "/api/repos/"+opened.ID+"/commits")
	if response.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

// TestTheWireFormatIsWhatTheClientDeclares pins the exact set of keys each
// route sends.
//
// web/src/api/types.ts is a hand-written description of these objects, and
// nothing made the two agree. They had already drifted: the daemon sends
// `bare` on a repository and the TypeScript did not mention it — found by
// pointing the running daemon at four real repositories and diffing the
// answers against the file.
//
// A missing field is not a compile error on the client, it is a property that
// silently reads as undefined. So this test fails when a field is added or
// removed, and says which file has to move with it. It is a reminder rather
// than a proof — nothing here can read the TypeScript — but a reminder that
// fires at the moment of the change beats a discovery six months later.
func TestTheWireFormatIsWhatTheClientDeclares(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	// A second commit, so the history has a line in it: the edge list of a
	// lone root commit is empty, and an empty list pins nothing.
	commitEmpty(t, path, "second commit")

	repos := get(t, handler, "/api/repos").Body.Bytes()
	commits := get(t, handler, "/api/repos/"+opened.ID+"/commits").Body.Bytes()
	refs := get(t, handler, "/api/repos/"+opened.ID+"/refs").Body.Bytes()

	cases := []struct {
		name     string
		body     []byte
		payload  []string // every key of the object the route answers with
		envelope string   // the key holding the list this case is about
		expected []string // every key of one item of that list
	}{
		{
			name:     "Repository",
			body:     repos,
			payload:  []string{"repos"},
			envelope: "repos",
			// watch_failure is absent here on purpose: it is omitempty, and
			// this repository is watched. What the shape has to carry either
			// way is `watched` — a client that never sees the key cannot draw
			// the case where it is false.
			expected: []string{"bare", "id", "name", "opened_at", "path", "watched"},
		},
		{
			name:    "Commit",
			body:    commits,
			payload: []string{"commits", "edges", "first", "page_size", "total", "width"},
			// `lane` is the commit's column in the graph. It rides on the
			// commit rather than in a parallel array, so it belongs to this
			// list of keys and moves with it.
			envelope: "commits",
			expected: []string{"author", "date", "lane", "parents", "refs", "sha", "subject"},
		},
		{
			name:     "Edge",
			body:     commits,
			payload:  []string{"commits", "edges", "first", "page_size", "total", "width"},
			envelope: "edges",
			expected: []string{"from", "from_lane", "lane", "to", "to_lane"},
		},
		{
			name:     "Ref",
			body:     refs,
			payload:  []string{"head", "refs"},
			envelope: "refs",
			// `upstream` is absent here on purpose: it is omitempty, and this
			// repository has no remote. The client declares it optional, which
			// is the only correct way to read a key that may not arrive.
			expected: []string{"ahead", "behind", "gone", "kind", "name", "sha", "short_name"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(testCase.body, &payload); err != nil {
				t.Fatalf("decoding: %v", err)
			}

			if keys := slices.Sorted(maps.Keys(payload)); !slices.Equal(keys, testCase.payload) {
				t.Errorf("the payload changed shape: %v, expected %v.\n"+
					"web/src/api/types.ts describes these objects by hand — move it too.",
					keys, testCase.payload)
			}

			var items []map[string]json.RawMessage
			if err := json.Unmarshal(payload[testCase.envelope], &items); err != nil {
				t.Fatalf("decoding %s: %v", testCase.envelope, err)
			}
			if len(items) == 0 {
				t.Fatalf("no %s in %s", testCase.envelope, testCase.body)
			}

			keys := slices.Sorted(maps.Keys(items[0]))
			if !slices.Equal(keys, testCase.expected) {
				t.Errorf("the wire format changed: %v, expected %v.\n"+
					"web/src/api/types.ts describes these objects by hand — move it too.",
					keys, testCase.expected)
			}
		})
	}
}

// TestTheCommitWireFormatIsWhatTheClientDeclares does for the single-commit
// route what the table above does for the lists. A route answering one object
// rather than a list of them does not fit that harness, and the drift it
// guards against is exactly the same.
//
// The keys are flat: a commit's own fields at the top level, plus the two that
// say where it sits in the walk that was asked for. One object rather than a
// commit nested inside a locator, because it is one answer to one click — and
// a nested shape would have meant a second type on the client for the same
// commit the history list already describes.
func TestTheCommitWireFormatIsWhatTheClientDeclares(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))
	history := decode[historyPayload](t, get(t, handler, "/api/repos/"+opened.ID+"/commits"))
	body := get(t, handler, "/api/repos/"+opened.ID+"/commits/"+history.Commits[0].SHA).Body.Bytes()

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// "signer" is not in this list and that is correct: it is omitempty, and
	// the commit this fixture makes is unsigned. The verdict itself is always
	// sent — "N" for an unsigned commit is an answer, not an absence.
	expected := []string{
		"against_first_parent", "author", "body", "committer", "committer_date", "date",
		"files", "lane", "parents", "refs", "row", "sha", "signature", "subject",
	}
	if keys := slices.Sorted(maps.Keys(payload)); !slices.Equal(keys, expected) {
		t.Errorf("the payload changed shape: %v, expected %v.\n"+
			"web/src/api/types.ts describes these objects by hand — move it too.", keys, expected)
	}

	// The seven a row of the history carries are all in there, under the same
	// names. A commit has one shape on the wire, whichever route sent it.
	for _, key := range []string{"author", "date", "lane", "parents", "refs", "sha", "subject"} {
		if _, present := payload[key]; !present {
			t.Errorf("the answer has no %q, which every row of the history carries", key)
		}
	}
}

// ---------------------------------------------------------------------------
// The middleware every response passes through
// ---------------------------------------------------------------------------
//
// These three exist because each was once broken in a way that produced no
// error at all: a panic that closed the connection with no status and no log
// line, and a ResponseWriter wrapper that swallowed the capabilities hot
// reload and event streams are built on. None of them was covered.

// serverWithFrontend puts a handler of the test's choosing behind the
// middleware. Frontend is reached for any path outside /api, which makes it
// the way to exercise the wrapper without inventing a route.
func serverWithFrontend(t *testing.T, frontend http.Handler) http.Handler {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}
	runner := git.NewRunner(nil)
	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	server, err := api.NewServer(api.Options{
		Registry: registry,
		Runner:   runner,
		Token:    testToken,
		Logger:   slog.New(slog.DiscardHandler),
		Frontend: frontend,
		Events:   api.NewEventStream(slog.New(slog.DiscardHandler)),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server.Handler()
}

// TestAPanickingHandlerStillAnswers reproduces what net/http does on its own,
// and what this middleware is here to prevent.
//
// net/http recovers a panic by closing the connection: no status, no body, and
// no log line — the request vanishes from the journal as if it had never
// arrived. Measured over a real socket, because httptest.ResponseRecorder
// cannot show a connection being dropped.
func TestAPanickingHandlerStillAnswers(t *testing.T) {
	handler := serverWithFrontend(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("something nobody thought of")
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("X-Yagit-Token", testToken)

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("the connection was dropped instead of answered: %v", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("closing: %v", err)
		}
	}()

	if response.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", response.StatusCode)
	}

	body := decodeBody(t, response)
	if body.Error.Message == "" {
		t.Error("the answer carries no message")
	}
	// Nothing from the panic itself: whatever was in scope when it happened is
	// not the client's business. The journal has the details.
	if strings.Contains(body.Error.Message, "something nobody thought of") {
		t.Errorf("the panic value reached the client: %q", body.Error.Message)
	}
}

// TestAnAbortedHandlerIsNotTurnedIntoA500 covers net/http's own way of saying
// "drop this connection, deliberately". Swallowing it would turn an intentional
// abort into a 500 and a log line about a failure that did not happen.
func TestAnAbortedHandlerIsNotTurnedIntoA500(t *testing.T) {
	handler := serverWithFrontend(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("X-Yagit-Token", testToken)

	response, err := server.Client().Do(request)
	if err == nil {
		defer func() { _ = response.Body.Close() }()
		t.Fatalf("status = %d, want the connection dropped as asked", response.StatusCode)
	}
}

// TestTheWrapperHandsBackTheConnection covers the capability hot reload needs.
//
// Wrapping a ResponseWriter hides the interfaces the original implemented, and
// anything needing more than "write bytes" then stops working — with error
// messages that never point at the middleware to blame. This is the WebSocket
// upgrade behind Vite's hot reload, in miniature.
func TestTheWrapperHandsBackTheConnection(t *testing.T) {
	handler := serverWithFrontend(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hijacker, capable := writer.(http.Hijacker)
		if !capable {
			t.Error("the wrapper does not expose http.Hijacker")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		defer func() { _ = connection.Close() }()

		if _, err := buffered.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n"); err != nil {
			t.Errorf("writing: %v", err)
			return
		}
		if err := buffered.Flush(); err != nil {
			t.Errorf("flushing: %v", err)
		}
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("X-Yagit-Token", testToken)

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("the upgrade never completed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want 101", response.StatusCode)
	}
}

// TestTheWrapperFlushes covers the other half: an event stream is worth
// nothing unless what is written arrives as it happens.
func TestTheWrapperFlushes(t *testing.T) {
	handler := serverWithFrontend(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if _, err := writer.Write([]byte("event: hello\n\n")); err != nil {
			t.Errorf("writing: %v", err)
			return
		}
		flusher, capable := writer.(http.Flusher)
		if !capable {
			t.Error("the wrapper does not expose http.Flusher")
			return
		}
		flusher.Flush()
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("X-Yagit-Token", testToken)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if !recorder.Flushed {
		t.Error("the flush did not reach the underlying writer")
	}
}

// TestNewServerRefusesToStartHalfAssembled covers the constructor's guards.
//
// Each of these is a dependency without which the daemon would answer
// something wrong rather than nothing: no token is no authentication at all,
// and no frontend is a daemon serving an interface it does not have.
func TestNewServerRefusesToStartHalfAssembled(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	runner := git.NewRunner(nil)
	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	complete := api.Options{
		Registry: registry,
		Runner:   runner,
		Token:    testToken,
		Logger:   slog.New(slog.DiscardHandler),
		Frontend: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Events:   api.NewEventStream(slog.New(slog.DiscardHandler)),
	}

	cases := map[string]func(*api.Options){
		"registry":     func(o *api.Options) { o.Registry = nil },
		"runner":       func(o *api.Options) { o.Runner = nil },
		"token":        func(o *api.Options) { o.Token = "" },
		"logger":       func(o *api.Options) { o.Logger = nil },
		"frontend":     func(o *api.Options) { o.Frontend = nil },
		"event stream": func(o *api.Options) { o.Events = nil },
	}

	for missing, remove := range cases {
		t.Run("without a "+missing, func(t *testing.T) {
			options := complete
			remove(&options)

			_, err := api.NewServer(options)
			if err == nil {
				t.Fatalf("a server with no %s must not be built", missing)
			}
			// The message names the missing piece. "invalid options" would
			// send the reader to the constructor to find out which.
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("the error does not name %q: %v", missing, err)
			}
		})
	}
}

func decodeBody(t *testing.T, response *http.Response) struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
} {
	t.Helper()

	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}
	return body
}

// ---------------------------------------------------------------------------
// DELETE /api/repos/{id}
// ---------------------------------------------------------------------------

// TestClosingARepositoryIsIdempotent covers the operation that makes a broken
// repository recoverable.
//
// Without it an entry lives for the life of the process, and one case makes
// that actively harmful: a repository deleted and re-cloned — which people do —
// is refused afterwards with 409 forever, correctly and with no way back.
func TestClosingARepositoryIsIdempotent(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path)))

	closed := execute(handler, authenticated(httptest.NewRequest(http.MethodDelete, "/api/repos/"+opened.ID, nil)))
	if closed.Code != http.StatusNoContent {
		t.Fatalf("close: status = %d, want 204: %s", closed.Code, closed.Body)
	}

	listed := decode[struct {
		Repos []wireRepo `json:"repos"`
	}](t, get(t, handler, "/api/repos"))
	if len(listed.Repos) != 0 {
		t.Errorf("the repository is still listed: %+v", listed.Repos)
	}

	// Closing a tab twice is a normal thing for a browser to do, and a reload
	// racing a click does it too. A 404 for the second attempt would be an
	// error about a state the caller already wanted.
	again := execute(handler, authenticated(httptest.NewRequest(http.MethodDelete, "/api/repos/"+opened.ID, nil)))
	if again.Code != http.StatusNoContent {
		t.Errorf("closing twice: status = %d, want 204", again.Code)
	}

	unknown := execute(handler, authenticated(httptest.NewRequest(http.MethodDelete, "/api/repos/deadbeef", nil)))
	if unknown.Code != http.StatusNoContent {
		t.Errorf("closing an unknown id: status = %d, want 204", unknown.Code)
	}
}

// TestAReplacedRepositoryCanBeClosedAndReopened is the whole point of the
// route: the refusal is right, and it must not be permanent.
func TestAReplacedRepositoryCanBeClosedAndReopened(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	makeRepo(t, path)

	body := fmt.Sprintf(`{"path":%q}`, path)
	opened := decode[wireRepo](t, postJSON(t, handler, "/api/repos", body))

	// Deleted and made again — a re-clone, as far as the daemon can tell.
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("removing: %v", err)
	}
	makeRepo(t, path)

	if stuck := get(t, handler, "/api/repos/"+opened.ID+"/commits"); stuck.Code != http.StatusConflict {
		t.Fatalf("status = %d, want the 409 that this test exists to escape", stuck.Code)
	}

	execute(handler, authenticated(httptest.NewRequest(http.MethodDelete, "/api/repos/"+opened.ID, nil)))

	reopened := postJSON(t, handler, "/api/repos", body)
	if reopened.Code != http.StatusCreated {
		t.Fatalf("reopening: status = %d, want 201: %s", reopened.Code, reopened.Body)
	}
	if commits := get(t, handler, "/api/repos/"+decode[wireRepo](t, reopened).ID+"/commits"); commits.Code != http.StatusOK {
		t.Errorf("after reopening: status = %d, want 200: %s", commits.Code, commits.Body)
	}
}

// authenticated adds the token to a request built by hand.
func authenticated(request *http.Request) *http.Request {
	request.Header.Set("X-Yagit-Token", testToken)
	return request
}

// ---------------------------------------------------------------------------
// The door
// ---------------------------------------------------------------------------

// TestAnUnauthenticatedBrowserGetsAPageAndAClientGetsJSON covers the answer to
// a missing token, which depends on who asked.
//
// What a person browsing to the daemon's address used to get was the JSON
// refusal below, rendered as text in a browser window — instructions written
// for a client, on a screen offering no way to follow them. It is also the
// first thing anyone browsing from another machine sees, which is the whole
// case YAGIT_PUBLIC_HOST and YAGIT_PUBLIC_URL exist for.
func TestAnUnauthenticatedBrowserGetsAPageAndAClientGetsJSON(t *testing.T) {
	handler := testServer(t)

	cases := []struct {
		name        string
		path        string
		accept      string
		wantHTML    bool
		wantContent string
	}{
		{
			name:        "a browser navigating to the root",
			path:        "/",
			accept:      "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			wantHTML:    true,
			wantContent: "text/html",
		},
		{
			name:        "a browser deep-linking into the application",
			path:        "/repositories/abc123",
			accept:      "text/html",
			wantHTML:    true,
			wantContent: "text/html",
		},
		{
			// A client parses. Handing it a page where it expects an object is
			// how a useful refusal becomes an unreadable one.
			name:        "an API call from the application",
			path:        "/api/repos",
			accept:      "text/html",
			wantHTML:    false,
			wantContent: "application/json",
		},
		{
			name:        "curl, which sends no Accept at all",
			path:        "/",
			accept:      "",
			wantHTML:    false,
			wantContent: "application/json",
		},
		{
			// fetch() defaults to */*. It is a client, and it gets JSON.
			name:        "fetch, which accepts anything",
			path:        "/",
			accept:      "*/*",
			wantHTML:    false,
			wantContent: "application/json",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, testCase.path, nil)
			if testCase.accept != "" {
				request.Header.Set("Accept", testCase.accept)
			}

			response := execute(handler, request)

			// 401 either way. The page is a courtesy to the reader, not a
			// claim that the request succeeded: a proxy, a probe or a `curl -f`
			// still has to see the refusal for what it is.
			if response.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", response.Code)
			}
			if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, testCase.wantContent) {
				t.Errorf("Content-Type = %q, want %s", contentType, testCase.wantContent)
			}

			body := response.Body.String()
			if testCase.wantHTML {
				// The page has to carry both ways in: the line the daemon
				// prints, and the command that recovers it once that line has
				// scrolled away.
				for _, needed := range []string{"<!doctype html>", "Session token:", "./do token", `name="token"`, `action="/api/session"`} {
					if !strings.Contains(body, needed) {
						t.Errorf("the page does not mention %q", needed)
					}
				}
				// And it must never carry the answer.
				if strings.Contains(body, testToken) {
					t.Error("the page leaks the session token")
				}
				return
			}

			if !strings.Contains(body, `"error"`) {
				t.Errorf("a client got %q, which is not the JSON refusal", body)
			}
		})
	}
}

// TestTheClientRefusalNamesAWayIn guards the only instruction curl ever gets.
// The announced address carries no token, so a refusal that sent the reader to
// look in it would describe a place the token has never been.
func TestTheClientRefusalNamesAWayIn(t *testing.T) {
	handler := testServer(t)

	response := execute(handler, httptest.NewRequest(http.MethodGet, "/api/repos", nil))
	body := response.Body.String()

	// Both ways in: the file the token is written to, and the route that
	// trades it for a cookie.
	for _, needed := range []string{"./do token", "X-Yagit-Token", "/api/session"} {
		if !strings.Contains(body, needed) {
			t.Errorf("the refusal does not mention %q: %s", needed, body)
		}
	}

	// Capitalised, because `curl` contains the same three letters in lower
	// case. What must not come back is the prose word for the address the
	// caller used.
	if strings.Contains(body, "URL") {
		t.Errorf("the refusal points at a URL again: %s", body)
	}
}

// TestSessionExchangeSetsACookie closes the loop: POST /api/session is how a
// browser gets in without putting the secret in the URL.
func TestSessionExchangeSetsACookie(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader("token="+testToken))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := execute(handler, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/" {
		t.Errorf("Location = %q, want /", location)
	}
	if len(response.Result().Cookies()) != 1 {
		t.Error("no session cookie was set")
	}
}

// TestLegacyTokenInURLStillWorks keeps the query-string exchange for tools
// that already pass ?token=, while steering browsers toward POST /api/session.
func TestLegacyTokenInURLStillWorks(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/?token="+testToken, nil)
	request.Header.Set("Accept", "text/html")
	response := execute(handler, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/" {
		t.Errorf("Location = %q, want the token gone from the address bar", location)
	}
	if len(response.Result().Cookies()) != 1 {
		t.Error("no session cookie was set, so the next request starts over")
	}
}
