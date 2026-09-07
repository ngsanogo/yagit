package api_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// What POST /switch answers, and what it refuses.
//
// The refusals carry the weight. A checkout is the one operation here that
// changes what every other route in the API is about — the history it walks,
// the files it lists, the message it would prepare — so a request that is
// wrong about what it names must fail loudly rather than land somewhere
// plausible.

// openSwitchableRepository builds a repository with two branches and a tag,
// and opens it through the API.
//
//	main    A ─── B
//	side    A ─── S     (tag: v1, on S)
func openSwitchableRepository(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGitIn(t, path, "init", "-b", "main")
	commitEmpty(t, path, "A")
	runGitIn(t, path, "switch", "-c", "side")
	commitEmpty(t, path, "S")
	runGitIn(t, path, "tag", "v1")
	runGitIn(t, path, "switch", "main")
	commitEmpty(t, path, "B")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}

	return handler, decode[wireRepo](t, response).ID, path
}

// The answer is the sidebar's own payload, and that is the point: the route
// that moves HEAD sends back where HEAD now is, so nothing has to ask again to
// find out what it just did.
func TestSwitchingAnswersWithTheReferencesAndTheNewHEAD(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"ref":"side"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if refs.Head == nil {
		t.Fatal("the answer carries no HEAD")
	}
	if refs.Head.Name != "side" {
		t.Errorf("HEAD is on %q, want side", refs.Head.Name)
	}
	if refs.Head.Detached {
		t.Error("switching to a branch must not detach HEAD")
	}
	if len(refs.Refs) == 0 {
		t.Error("the answer carries no references")
	}
}

func TestDetachingAnswersWithADetachedHEAD(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"ref":"v1","detach":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if refs.Head == nil || !refs.Head.Detached {
		t.Fatalf("HEAD = %+v, want it detached at the tag", refs.Head)
	}
}

// The status route is what the working-directory panel reads, and a checkout
// is the one thing that changes its answer without anybody touching a file.
func TestSwitchingChangesWhatTheStatusRouteReports(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	if response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"ref":"side"}`); response.Code != http.StatusOK {
		t.Fatalf("switching: status = %d: %s", response.Code, response.Body)
	}

	status := decode[wireStatus](t, get(t, handler, "/api/repos/"+id+"/status"))
	if status.Branch != "side" {
		t.Errorf("status says the branch is %q, want side", status.Branch)
	}
	if status.Detached {
		t.Error("status says HEAD is detached after a switch to a branch")
	}
}

// A branch that is not there is git's refusal to report, whole: 422 says the
// request was well formed and git said no, which is a different thing from the
// daemon not understanding it.
func TestSwitchingToAMissingBranchIsGitsRefusal(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"ref":"no-such-branch"}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body)
	}
	if body := response.Body.String(); !strings.Contains(body, "no-such-branch") {
		t.Errorf("the refusal does not name the branch: %s", body)
	}
}

// A tag is not a place HEAD can sit. Without `detach` the request is asking
// for something git cannot do, and the answer has to say so rather than
// quietly detaching instead — the interface would then be showing a branch
// name for a repository that is on none.
func TestSwitchingToATagWithoutDetachingIsRefused(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"ref":"v1"}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, get(t, handler, "/api/repos/"+id+"/refs"))
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("HEAD = %+v, want it left on main", refs.Head)
	}
}

func TestSwitchingWithNoReferenceIsABadRequest(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"ref":""}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

// An unknown field is refused rather than ignored, like everywhere else in
// this API: a client that sent "branch" where "ref" was expected has to find
// out now, not by wondering why nothing was checked out.
func TestSwitchingRefusesAnUnknownField(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/switch", `{"branch":"side"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

// What the branch routes answer, and what they refuse.
//
// The shape of the answer is the subject: every one of them sends back the
// reference list it changed, so a sidebar never has to ask what just happened
// to it — and the delete plan sends back a command rather than a promise about
// one.

// branchNamesOf reads the local branches out of a refs payload, sorted.
func branchNamesOf(payload wireRefs) []string {
	names := make([]string, 0, len(payload.Refs))
	for _, ref := range payload.Refs {
		if ref.Kind == "branch" {
			names = append(names, ref.ShortName)
		}
	}
	slices.Sort(names)
	return names
}

func TestCreatingABranchAnswersWithItInTheList(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches", `{"name":"third","start":"side"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if got, want := branchNamesOf(refs), []string{"main", "side", "third"}; !slices.Equal(got, want) {
		t.Errorf("branches = %v, want %v", got, want)
	}

	// Created, not stood on. The request did not ask to move.
	if refs.Head == nil || refs.Head.Name != "main" {
		t.Errorf("HEAD = %+v, want it left on main", refs.Head)
	}
}

func TestCreatingABranchCanStandOnIt(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches",
		`{"name":"third","switch":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if refs.Head == nil || refs.Head.Name != "third" || refs.Head.Detached {
		t.Errorf("HEAD = %+v, want it on third and attached", refs.Head)
	}
}

// The refusal that matters, at the edge where a name reaches git.
func TestCreatingABranchRefusesAnOptionShapedName(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches", `{"name":"-m","start":"side"}`)
	if response.Code == http.StatusOK {
		t.Fatalf("status = 200 for a branch named -m: %s", response.Body)
	}

	// And nothing happened: main is still main, which is what `git branch -m
	// side` would have taken away.
	refs := decode[wireRefs](t, get(t, handler, "/api/repos/"+id+"/refs"))
	if got, want := branchNamesOf(refs), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Errorf("branches = %v, want the two it started with", got)
	}
}

func TestRenamingABranchAnswersWithTheNewName(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches/rename",
		`{"from":"side","to":"renamed"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if got, want := branchNamesOf(refs), []string{"main", "renamed"}; !slices.Equal(got, want) {
		t.Errorf("branches = %v, want %v", got, want)
	}
}

// The plan is the confirmation's whole content, so it has to be the command
// and not a description of one.
func TestPlanningADeleteAnswersTheCommandItWouldRun(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches/delete/plan",
		`{"name":"side","force":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[struct {
		Command string `json:"command"`
	}](t, response)
	if got, want := planned.Command, "git branch -D -- side"; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}

	// Planned, not run.
	refs := decode[wireRefs](t, get(t, handler, "/api/repos/"+id+"/refs"))
	if got, want := branchNamesOf(refs), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Errorf("branches = %v, want the plan to have changed nothing", got)
	}
}

func TestPlanningADeleteSaysWhichFlagItWouldUse(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches/delete/plan", `{"name":"side"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	planned := decode[struct {
		Command string `json:"command"`
	}](t, response)
	// -d and not -D, because force was not asked for. The dialog shows this
	// line, and showing the wrong one is showing a promise that is not kept.
	if got, want := planned.Command, "git branch -d -- side"; got != want {
		t.Errorf("command = %q, want %q", got, want)
	}
}

func TestDeletingABranchAnswersWithoutIt(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches/delete",
		`{"name":"side","force":true}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body)
	}

	refs := decode[wireRefs](t, response)
	if got, want := branchNamesOf(refs), []string{"main"}; !slices.Equal(got, want) {
		t.Errorf("branches = %v, want only main", got)
	}
}

// git's own refusals reach the client whole, and this is the one a person is
// most likely to meet: side holds a commit main cannot reach.
func TestDeletingAnUnmergedBranchIsRefusedWithGitsWords(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/branches/delete", `{"name":"side"}`)
	if response.Code == http.StatusOK {
		t.Fatalf("status = 200 deleting an unmerged branch without force: %s", response.Body)
	}
	if !strings.Contains(response.Body.String(), "not fully merged") {
		t.Errorf("body = %s, want git's own sentence about the unmerged branch", response.Body)
	}

	refs := decode[wireRefs](t, get(t, handler, "/api/repos/"+id+"/refs"))
	if got, want := branchNamesOf(refs), []string{"main", "side"}; !slices.Equal(got, want) {
		t.Errorf("branches = %v, want side still there", got)
	}
}

func TestBranchRoutesRefuseABodyTheyDoNotUnderstand(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	for _, route := range []string{"/branches", "/branches/rename", "/branches/delete", "/branches/delete/plan"} {
		// An unknown field is a client asking for something this route does
		// not do. Ignoring it silently is how a "force" that was never read
		// becomes a delete somebody thought they had asked to be careful with.
		response := postJSON(t, handler, "/api/repos/"+id+route, `{"nonsense":true}`)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
	}
}

// A name left empty is the request being wrong, not the daemon or the
// repository, and 400 is the code that says so. Every one of these four
// refuses before git is reached, so nothing here has git's own words in it —
// which is exactly why the status is the only thing that can be asserted.
func TestBranchRoutesRefuseAnEmptyName(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	bodies := map[string]string{
		"/branches":             `{"name":"  ","start":"","switch":false}`,
		"/branches/rename":      `{"from":"main","to":"  "}`,
		"/branches/delete":      `{"name":"  ","force":false}`,
		"/branches/delete/plan": `{"name":"  ","force":false}`,
	}

	for route, body := range bodies {
		response := postJSON(t, handler, "/api/repos/"+id+route, body)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", route, response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), "no branch name given") {
			t.Errorf("%s: body = %s, want it to name what is missing", route, response.Body)
		}
	}
}
