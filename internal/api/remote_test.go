package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/git"
)

// What the network routes answer, and what they refuse.
//
// The refusals carry the weight here, as they do for the checkout. A pull and
// a push are the two operations in this API that change somebody else's
// repository or this one's history, and a request that is wrong about which
// branch it means must fail with a sentence rather than land somewhere
// plausible.
//
// The remote is a directory on the same disk. git treats a path as a remote
// exactly as it treats a URL — the same refspecs, the same fast-forward rules
// — so nothing here is slow and nothing here needs a network.

// openClonedRepository builds a bare repository, a clone of it, and opens the
// clone through the API.
//
//	server.git   A         ← bare, the "other machine"
//	project      A         ← on main, following origin/main
func openClonedRepository(t *testing.T) (handler http.Handler, id, project, server string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	server = filepath.Join(root, "server.git")
	project = filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGitIn(t, root, "init", "--bare", "-b", "main", server)
	runGitIn(t, project, "init", "-b", "main")
	// In the repository's own configuration: a pull that merges writes a
	// commit, and the daemon runs git without the -c identity these tests pass
	// by hand.
	runGitIn(t, project, "config", "user.name", "Ada Lovelace")
	runGitIn(t, project, "config", "user.email", "ada@example.com")
	runGitIn(t, project, "remote", "add", "origin", server)
	commitEmpty(t, project, "A")
	runGitIn(t, project, "push", "--set-upstream", "origin", "main:refs/heads/main")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", project))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, project, server
}

// elsewhere is another checkout of the same server: somebody else's machine,
// where the commits this repository has not seen come from.
func elsewhere(t *testing.T, root, server string) string {
	t.Helper()

	other := filepath.Join(root, "elsewhere")
	runGitIn(t, root, "clone", server, other)
	runGitIn(t, other, "config", "user.name", "Grace Hopper")
	runGitIn(t, other, "config", "user.email", "grace@example.com")
	return other
}

type wireRemotes struct {
	Remotes []struct {
		Name     string `json:"name"`
		FetchURL string `json:"fetch_url"`
		PushURL  string `json:"push_url"`
	} `json:"remotes"`
}

// wireError is the failure shape this project promises: a sentence, and under
// it git's own account of what it ran and what it said.
type wireError struct {
	Error struct {
		Message string `json:"message"`
		Git     *struct {
			Command  string `json:"command"`
			ExitCode int    `json:"exit_code"`
			Stderr   string `json:"stderr"`
		} `json:"git"`
	} `json:"error"`
}

type wirePushPlan struct {
	Command     string `json:"command"`
	Remote      string `json:"remote"`
	Ref         string `json:"ref"`
	LocalBranch string `json:"local_branch"`
	Publishing  bool   `json:"publishing"`
}

func TestRemotesListsWhatTheRepositoryTalksTo(t *testing.T) {
	handler, id, _, server := openClonedRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/remotes")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	payload := decode[wireRemotes](t, response)
	if len(payload.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want one", payload.Remotes)
	}
	if payload.Remotes[0].Name != "origin" {
		t.Errorf("remote is %q, want origin", payload.Remotes[0].Name)
	}
	if payload.Remotes[0].FetchURL != server {
		t.Errorf("fetch URL is %q, want %q", payload.Remotes[0].FetchURL, server)
	}
}

// A nil slice marshals to `null`, and a client asking "are there any remotes"
// of a null gets an answer that is right for the wrong reason — until the day
// it does `remotes.map(…)` and throws.
func TestRemotesInARepositoryWithNoneIsAnEmptyArray(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "solo")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, path, "init", "-b", "main")
	commitEmpty(t, path, "A")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	id := decode[wireRepo](t, response).ID

	body := get(t, handler, "/api/repos/"+id+"/remotes").Body.String()
	if !strings.Contains(body, `"remotes":[]`) {
		t.Errorf("body = %s, want an empty array", body)
	}
}

// The answer is the sidebar's own payload, and that is the point: the route
// that moved the remote-tracking branches sends back where they are now.
func TestFetchAnswersWithTheReferencesItMoved(t *testing.T) {
	handler, id, _, server := openClonedRepository(t)
	root := filepath.Dir(server)

	other := elsewhere(t, root, server)
	commitEmpty(t, other, "B from elsewhere")
	runGitIn(t, other, "push", "origin", "main:refs/heads/main")

	response := postJSON(t, handler, "/api/repos/"+id+"/fetch", `{"remote":"origin"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decodeProgressDone[wireRefs](t, response)
	var tracking string
	for _, ref := range refs.Refs {
		if ref.ShortName == "origin/main" {
			tracking = ref.SHA
		}
		if ref.ShortName == "main" && ref.Behind != 1 {
			t.Errorf("main is %d behind after the fetch, want 1", ref.Behind)
		}
	}
	if tracking == "" {
		t.Error("the answer names no origin/main, so the fetch was not reflected")
	}
}

func TestPullFastForwardsTheCurrentBranch(t *testing.T) {
	handler, id, _, server := openClonedRepository(t)
	root := filepath.Dir(server)

	other := elsewhere(t, root, server)
	commitEmpty(t, other, "B from elsewhere")
	runGitIn(t, other, "push", "origin", "main:refs/heads/main")

	response := postJSON(t, handler, "/api/repos/"+id+"/pull", `{"strategy":"ff-only"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decodeProgressDone[wireRefs](t, response)
	if refs.Head == nil {
		t.Fatal("the answer carries no HEAD")
	}
	for _, ref := range refs.Refs {
		if ref.ShortName == "main" && (ref.Ahead != 0 || ref.Behind != 0) {
			t.Errorf("main is ahead %d behind %d after a pull, want level", ref.Ahead, ref.Behind)
		}
	}
}

// git's refusal, whole, through the JSON shape this project promises: the
// command, the exit code and the raw stderr, so the interface can show what
// actually happened instead of "something went wrong".
func TestPullRefusesDivergedBranchesWithGitsOwnWords(t *testing.T) {
	handler, id, project, server := openClonedRepository(t)
	root := filepath.Dir(server)

	other := elsewhere(t, root, server)
	commitEmpty(t, other, "theirs")
	runGitIn(t, other, "push", "origin", "main:refs/heads/main")
	commitEmpty(t, project, "ours")

	response := postJSON(t, handler, "/api/repos/"+id+"/pull", `{"strategy":"ff-only"}`)
	// Progress already started before git refused, so the refusal rides the
	// stream as an error event — the same shape a failed clone uses (ADR 0030).
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with an error event: %s", response.Code, response.Body)
	}

	failure := decodeProgressError(t, response)
	if failure.Error.Git == nil {
		t.Fatal("the refusal carries no git failure")
	}
	if !strings.Contains(failure.Error.Git.Command, "pull --progress --ff-only") {
		t.Errorf("command is %q, want the pull that was run", failure.Error.Git.Command)
	}
	if failure.Error.Git.Stderr == "" {
		t.Error("the refusal carries no stderr, which is the only thing that explains it")
	}
}

// An unknown strategy is refused rather than read as the default. The two
// differ by a merge commit, and a client that misspelled one would otherwise
// find out by reading the history a week later.
func TestPullRefusesAStrategyItDoesNotKnow(t *testing.T) {
	handler, id, _, _ := openClonedRepository(t)

	for _, body := range []string{`{"strategy":"ours"}`, `{"strategy":""}`, `{}`} {
		response := postJSON(t, handler, "/api/repos/"+id+"/pull", body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", body, response.Code, response.Body)
		}
	}
}

func TestPullOnADetachedHEADIsAConflictRatherThanAFailure(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "switch", "--detach", "HEAD")

	response := postJSON(t, handler, "/api/repos/"+id+"/pull", `{"strategy":"ff-only"}`)
	// 409: the request is well formed and the repository exists; it is in a
	// state where pulling has no meaning, which is a different thing from a
	// bad request and from a git failure.
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "detached") {
		t.Errorf("body = %s, want it to name the detached HEAD", response.Body)
	}
}

func TestPullOnABranchThatFollowsNothingSaysSo(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "switch", "-c", "solo")

	response := postJSON(t, handler, "/api/repos/"+id+"/pull", `{"strategy":"ff-only"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "upstream") {
		t.Errorf("body = %s, want it to name the missing upstream", response.Body)
	}
}

// An empty repository and a detached HEAD look alike from a distance — neither
// has a branch name to read — and only one of them is fixed by making a commit.
// Telling somebody their HEAD is detached in a repository they created a minute
// ago sends them looking for a checkout nobody made.
func TestPushInAnEmptyRepositorySaysThereIsNoCommit(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "fresh")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitIn(t, path, "init", "-b", "main")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	id := decode[wireRepo](t, response).ID

	pushed := postJSON(t, handler, "/api/repos/"+id+"/push", `{"remote":"origin"}`)
	if pushed.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", pushed.Code, pushed.Body)
	}
	body := pushed.Body.String()
	if !strings.Contains(body, "no commit") {
		t.Errorf("body = %s, want it to name the missing commit", body)
	}
	if strings.Contains(body, "detached") {
		t.Errorf("body = %s, which blames a detached HEAD in a repository that has none", body)
	}
}

func TestPushSendsTheCurrentBranchToItsUpstream(t *testing.T) {
	handler, id, project, server := openClonedRepository(t)
	commitEmpty(t, project, "B")

	response := postJSON(t, handler, "/api/repos/"+id+"/push", `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if got, want := revision(t, server, "refs/heads/main"), revision(t, project, "main"); got != want {
		t.Errorf("the server is at %s, want %s", got, want)
	}

	refs := decodeProgressDone[wireRefs](t, response)
	for _, ref := range refs.Refs {
		if ref.ShortName == "main" && ref.Ahead != 0 {
			t.Errorf("main is still %d ahead after the push", ref.Ahead)
		}
	}
}

// The remote the client named is ignored when the branch already follows
// something. A push that took the suggestion would create a second branch on
// the server for one line of work.
func TestPushIgnoresASuggestedRemoteWhenTheBranchFollowsOne(t *testing.T) {
	handler, id, project, server := openClonedRepository(t)
	root := filepath.Dir(server)

	mirror := filepath.Join(root, "mirror.git")
	runGitIn(t, root, "clone", "--bare", server, mirror)
	runGitIn(t, project, "remote", "add", "mirror", mirror)
	commitEmpty(t, project, "B")

	response := postJSON(t, handler, "/api/repos/"+id+"/push", `{"remote":"mirror"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if got, want := revision(t, server, "refs/heads/main"), revision(t, project, "main"); got != want {
		t.Errorf("origin is at %s, want the branch's own upstream to have received it (%s)", got, want)
	}
}

func TestPushPublishesABranchThatFollowsNothing(t *testing.T) {
	handler, id, project, server := openClonedRepository(t)
	runGitIn(t, project, "switch", "-c", "feature")
	commitEmpty(t, project, "F")

	plan := postJSON(t, handler, "/api/repos/"+id+"/push/plan", `{"remote":"origin"}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	answered := decode[wirePushPlan](t, plan)

	body := fmt.Sprintf(`{"remote":"origin","local_branch":%q,"ref":%q}`,
		answered.LocalBranch, answered.Ref)
	response := postJSON(t, handler, "/api/repos/"+id+"/push", body)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if got, want := revision(t, server, "refs/heads/feature"), revision(t, project, "feature"); got != want {
		t.Errorf("feature on the server is %s, want %s", got, want)
	}

	// The publish set the upstream, which is what makes the next push and the
	// next pull need no decision at all.
	refs := decodeProgressDone[wireRefs](t, response)
	for _, ref := range refs.Refs {
		if ref.ShortName == "feature" && ref.Upstream != "refs/remotes/origin/feature" {
			t.Errorf("feature follows %q, want refs/remotes/origin/feature", ref.Upstream)
		}
	}
}

func TestPushRefusesToChooseARemoteForAnUnpublishedBranch(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "switch", "-c", "feature")
	commitEmpty(t, project, "F")

	response := postJSON(t, handler, "/api/repos/"+id+"/push", `{}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

// The plan is the whole content of the confirmation, so it has to be the exact
// line — assembled by the daemon, which is the only side that read the
// upstream.
func TestPushPlanAnswersTheLineThatWouldRun(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "push", "--set-upstream", "origin", "main:refs/heads/trunk")

	response := postJSON(t, handler, "/api/repos/"+id+"/push/plan", `{"force":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	plan := decode[wirePushPlan](t, response)
	want := "git push --progress --force-with-lease --force-if-includes -- origin main:refs/heads/trunk"
	if plan.Command != want {
		t.Errorf("command is %q, want %q", plan.Command, want)
	}
	if plan.Remote != "origin" || plan.Ref != "refs/heads/trunk" || plan.LocalBranch != "main" {
		t.Errorf("plan = %+v, want it to name origin, refs/heads/trunk and main", plan)
	}
	if plan.Publishing {
		t.Error("a branch that already follows something is not being published")
	}
}

func TestPushPlanNamesAPublishAsOne(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "switch", "-c", "feature")
	commitEmpty(t, project, "F")

	response := postJSON(t, handler, "/api/repos/"+id+"/push/plan", `{"remote":"origin"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	plan := decode[wirePushPlan](t, response)
	if !plan.Publishing {
		t.Error("a branch that follows nothing is being published, and the dialog says so")
	}
	if !strings.Contains(plan.Command, "--set-upstream") {
		t.Errorf("command is %q, want the publish to record where it went", plan.Command)
	}
}

// The plan runs nothing. It is asked for while a dialog is opening, and a
// route that pushed on the way to describing a push would be the worst
// possible confirmation.
func TestPushPlanMovesNothing(t *testing.T) {
	handler, id, project, server := openClonedRepository(t)
	commitEmpty(t, project, "B")
	before := revision(t, server, "refs/heads/main")

	if response := postJSON(t, handler, "/api/repos/"+id+"/push/plan", `{}`); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	if got := revision(t, server, "refs/heads/main"); got != before {
		t.Errorf("the server moved to %s while nothing but a plan was asked for", got)
	}
}

func TestNetworkRoutesRefuseAnUnknownField(t *testing.T) {
	// The same rule the staging routes hold to: a client that sends "remotes"
	// where "remote" was expected learns it now rather than by finding that
	// nothing was fetched.
	handler, id, _, _ := openClonedRepository(t)

	for _, route := range []string{
		"fetch", "pull", "push", "push/plan",
		"remotes", "remotes/rename", "remotes/remove", "remotes/remove/plan",
	} {
		response := postJSON(t, handler, "/api/repos/"+id+"/"+route, `{"remotes":"origin"}`)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
	}
}

func TestAddRenameRemoveRemoteThroughTheAPI(t *testing.T) {
	handler, id, project, server := openClonedRepository(t)
	mirror := filepath.Join(filepath.Dir(server), "mirror.git")
	runGitIn(t, filepath.Dir(server), "init", "--bare", "-b", "main", mirror)

	added := postJSON(t, handler, "/api/repos/"+id+"/remotes",
		fmt.Sprintf(`{"name":"mirror","url":%q}`, mirror))
	if added.Code != http.StatusOK {
		t.Fatalf("add: status = %d: %s", added.Code, added.Body)
	}
	if names := remoteNames(t, added); len(names) != 2 {
		t.Fatalf("after add: %v, want origin and mirror", names)
	}

	renamed := postJSON(t, handler, "/api/repos/"+id+"/remotes/rename",
		`{"from":"mirror","to":"backup"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename: status = %d: %s", renamed.Code, renamed.Body)
	}
	if names := remoteNames(t, renamed); !strings.Contains(strings.Join(names, ","), "backup") {
		t.Errorf("after rename: %v, want backup", names)
	}

	plan := postJSON(t, handler, "/api/repos/"+id+"/remotes/remove/plan", `{"name":"backup"}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan: status = %d: %s", plan.Code, plan.Body)
	}
	command := decode[struct {
		Command string `json:"command"`
	}](t, plan).Command
	if command != "git remote remove -- backup" {
		t.Errorf("plan command = %q", command)
	}

	removed := postJSON(t, handler, "/api/repos/"+id+"/remotes/remove", `{"name":"backup"}`)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove: status = %d: %s", removed.Code, removed.Body)
	}
	if names := remoteNames(t, removed); len(names) != 1 || names[0] != "origin" {
		t.Errorf("after remove: %v, want only origin", names)
	}

	// Removing origin also drops its tracking branches — the client invalidates
	// refs for that reason, and the on-disk state is what makes it necessary.
	gone := postJSON(t, handler, "/api/repos/"+id+"/remotes/remove", `{"name":"origin"}`)
	if gone.Code != http.StatusOK {
		t.Fatalf("remove origin: status = %d: %s", gone.Code, gone.Body)
	}
	if _, err := git.NewRunner(nil).Run(context.Background(), project, "rev-parse", "--verify", "refs/remotes/origin/main"); err == nil {
		t.Error("origin/main survived removing origin")
	}
}

func TestSetRemoteURLChangesTheFetchURL(t *testing.T) {
	handler, id, _, server := openClonedRepository(t)
	alternate := filepath.Join(filepath.Dir(server), "alternate.git")
	runGitIn(t, filepath.Dir(server), "init", "--bare", "-b", "main", alternate)

	response := postJSON(t, handler, "/api/repos/"+id+"/remotes/set-url",
		fmt.Sprintf(`{"name":"origin","url":%q}`, alternate))
	if response.Code != http.StatusOK {
		t.Fatalf("set-url: status = %d: %s", response.Code, response.Body)
	}

	listed := get(t, handler, "/api/repos/"+id+"/remotes")
	if listed.Code != http.StatusOK {
		t.Fatalf("remotes: status = %d: %s", listed.Code, listed.Body)
	}
	payload := decode[wireRemotes](t, listed)
	if len(payload.Remotes) != 1 {
		t.Fatalf("remotes = %+v, want one", payload.Remotes)
	}
	if payload.Remotes[0].FetchURL != alternate {
		t.Errorf("fetch URL is %q, want %q", payload.Remotes[0].FetchURL, alternate)
	}
}

func TestPlanSetRemoteURLRedactsCredentials(t *testing.T) {
	handler, id, _, _ := openClonedRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/remotes/set-url/plan",
		`{"name":"origin","url":"https://user:secret@host/repo.git"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("plan: status = %d: %s", response.Code, response.Body)
	}

	command := decode[struct {
		Command string `json:"command"`
	}](t, response).Command
	if !strings.Contains(command, "***") {
		t.Errorf("command = %q, want the password redacted", command)
	}
	if strings.Contains(command, "secret") {
		t.Errorf("command = %q, must not carry the password", command)
	}
}

func TestSetUpstreamAndUnset(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "branch", "solo")

	if upstream := upstreamOf(t, handler, id, "solo"); upstream != "" {
		t.Fatalf("solo tracks %q before anything configured it to track", upstream)
	}

	set := postJSON(t, handler, "/api/repos/"+id+"/upstream",
		`{"branch":"solo","remote":"origin","upstream":"main"}`)
	if set.Code != http.StatusOK {
		t.Fatalf("set upstream: status = %d: %s", set.Code, set.Body)
	}
	if upstream := upstreamOf(t, handler, id, "solo"); upstream != "refs/remotes/origin/main" {
		t.Errorf("solo tracks %q, want refs/remotes/origin/main", upstream)
	}

	unset := postJSON(t, handler, "/api/repos/"+id+"/upstream/unset", `{"branch":"solo"}`)
	if unset.Code != http.StatusOK {
		t.Fatalf("unset upstream: status = %d: %s", unset.Code, unset.Body)
	}
	if upstream := upstreamOf(t, handler, id, "solo"); upstream != "" {
		t.Errorf("solo still tracks %q after unset", upstream)
	}
}

func TestAddRemoteRefusesEmptyFields(t *testing.T) {
	handler, id, _, _ := openClonedRepository(t)

	for name, body := range map[string]string{
		"no name": `{"name":"","url":"https://example.test/x.git"}`,
		"no url":  `{"name":"fork","url":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := postJSON(t, handler, "/api/repos/"+id+"/remotes", body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
			}
		})
	}
}

func remoteNames(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	payload := decode[wireRemotes](t, response)
	names := make([]string, len(payload.Remotes))
	for i, remote := range payload.Remotes {
		names[i] = remote.Name
	}
	return names
}

// revision resolves a name in a repository on disk, so a test can check what
// the server actually received rather than what the answer claimed.
func revision(t *testing.T, dir, name string) string {
	t.Helper()

	runner := git.NewRunner(nil)
	output, err := runner.Run(context.Background(), dir, "rev-parse", "--verify", name)
	if err != nil {
		t.Fatalf("rev-parse %s in %s: %v", name, dir, err)
	}
	return strings.TrimSpace(string(output))
}

func TestForcePushRequiresThePlannedDestination(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "push", "--set-upstream", "origin", "main:refs/heads/trunk")
	commitEmpty(t, project, "rewrite")

	// Force without the lease fields the confirmation would echo.
	response := postJSON(t, handler, "/api/repos/"+id+"/push", `{"force":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestForcePushRefusesWhenTheBranchMovedSinceThePlan(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "push", "--set-upstream", "origin", "main:refs/heads/trunk")
	commitEmpty(t, project, "rewrite")

	plan := postJSON(t, handler, "/api/repos/"+id+"/push/plan", `{"force":true}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	answered := decode[wirePushPlan](t, plan)

	runGitIn(t, project, "switch", "-c", "other")
	commitEmpty(t, project, "on-other")
	runGitIn(t, project, "push", "--set-upstream", "origin", "other:refs/heads/other")

	body := fmt.Sprintf(
		`{"force":true,"local_branch":%q,"ref":%q}`,
		answered.LocalBranch, answered.Ref,
	)
	// Lease is checked before the progress stream starts, so the answer is
	// ordinary JSON — the same 409 reset and merge use when HEAD moved.
	response := postJSON(t, handler, "/api/repos/"+id+"/push", body)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "HEAD") {
		t.Errorf("refusal does not name HEAD: %s", response.Body)
	}
}

func TestUpstreamPlanRoutesAnswerTheCommand(t *testing.T) {
	handler, id, project, _ := openClonedRepository(t)
	runGitIn(t, project, "branch", "solo")

	setPlan := postJSON(t, handler, "/api/repos/"+id+"/upstream/plan",
		`{"branch":"solo","remote":"origin","upstream":"main"}`)
	if setPlan.Code != http.StatusOK {
		t.Fatalf("set plan status = %d: %s", setPlan.Code, setPlan.Body)
	}
	if command := decode[struct {
		Command string `json:"command"`
	}](t, setPlan).Command; !strings.Contains(command, "branch --set-upstream-to") {
		t.Errorf("set plan command = %q", command)
	}

	// Record the follow so unset has something to describe.
	if set := postJSON(t, handler, "/api/repos/"+id+"/upstream",
		`{"branch":"solo","remote":"origin","upstream":"main"}`); set.Code != http.StatusOK {
		t.Fatalf("set: %s", set.Body)
	}

	unsetPlan := postJSON(t, handler, "/api/repos/"+id+"/upstream/unset/plan", `{"branch":"solo"}`)
	if unsetPlan.Code != http.StatusOK {
		t.Fatalf("unset plan status = %d: %s", unsetPlan.Code, unsetPlan.Body)
	}
	if command := decode[struct {
		Command string `json:"command"`
	}](t, unsetPlan).Command; !strings.Contains(command, "branch --unset-upstream") {
		t.Errorf("unset plan command = %q", command)
	}
}

func TestPullRefusesABusyRepository(t *testing.T) {
	handler, id, _ := openStoppedMerge(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/pull", `{"strategy":"ff-only"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
}
