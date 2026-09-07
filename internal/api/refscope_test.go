package api_test

import (
	"net/http"
	"testing"
)

// A repository whose topic branch was never merged: the shape scope=refs
// exists for, and the one neither of the two older scopes answers.
//
//	main:  root ─── "main moved on"
//	          ╲
//	topic:     ─── "the topic tip"
func repositoryWithATopic(t *testing.T, name string) (http.Handler, string, string) {
	t.Helper()
	handler, id, path := openNamedRepository(t, name)

	runGitIn(t, path, "checkout", "-b", "topic")
	commitEmpty(t, path, "the topic tip")
	runGitIn(t, path, "checkout", "main")
	commitEmpty(t, path, "main moved on")

	return handler, id, path
}

func TestCommitsUnderAChosenSetWalkThoseRefsOnly(t *testing.T) {
	handler, id, _ := repositoryWithATopic(t, "chosen")

	main := decode[historyPayload](t, get(t, handler,
		"/api/repos/"+id+"/commits?scope=refs&ref=refs/heads/main"))
	if main.Total != 2 {
		t.Errorf("main alone walked %d commits, expected 2", main.Total)
	}

	both := decode[historyPayload](t, get(t, handler,
		"/api/repos/"+id+"/commits?scope=refs&ref=refs/heads/main&ref=refs/heads/topic"))
	if both.Total != 3 {
		t.Errorf("main with the topic walked %d commits, expected 3", both.Total)
	}
}

// Repeated `ref=` and not one comma-separated value, because a ref name may
// hold a comma — and a separator a name can contain is one that eventually
// splits a reference into two that do not exist.
func TestARefNameHoldingACommaIsOneRef(t *testing.T) {
	handler, id, path := repositoryWithATopic(t, "comma")
	runGitIn(t, path, "branch", "a,b", "main")

	answer := get(t, handler, "/api/repos/"+id+"/commits?scope=refs&ref="+
		"refs%2Fheads%2Fa%2Cb")
	if answer.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", answer.Code, answer.Body)
	}
	if walked := decode[historyPayload](t, answer); walked.Total != 2 {
		t.Errorf("the branch with a comma in its name walked %d commits, expected 2", walked.Total)
	}
}

// An empty walk drawn is indistinguishable from an empty repository, so a
// blank graph would be a correct-looking answer to a question nobody asked.
func TestScopeRefsWithNoRefIsRefused(t *testing.T) {
	handler, id, _ := repositoryWithATopic(t, "norefs")

	for _, target := range []string{
		"/api/repos/" + id + "/commits?scope=refs",
		"/api/repos/" + id + "/search?scope=refs&in=message&q=topic",
	} {
		if response := get(t, handler, target); response.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, expected 400: %s", target, response.Code, response.Body)
		}
	}
}

// A client that thinks it is narrowing a walk which ignores the parameter
// would get the whole repository's graph back and no indication of it — the
// same failure mode as an unknown scope, refused for the same reason.
func TestARefUnderAScopeThatIgnoresItIsRefused(t *testing.T) {
	handler, id, _ := repositoryWithATopic(t, "strayref")

	for _, target := range []string{
		"/api/repos/" + id + "/commits?scope=head&ref=refs/heads/main",
		"/api/repos/" + id + "/commits?scope=all&ref=refs/heads/main",
		"/api/repos/" + id + "/commits?ref=refs/heads/main",
		"/api/repos/" + id + "/search?scope=all&in=message&q=topic&ref=refs/heads/main",
	} {
		if response := get(t, handler, target); response.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, expected 400: %s", target, response.Code, response.Body)
		}
	}
}

// The name reaches `git log` where it takes revisions positionally, so a
// string that is an option is an option. Refused as a bad request, not
// reported as the daemon breaking.
func TestARefNameThatIsAnOptionIsRefused(t *testing.T) {
	handler, id, _ := repositoryWithATopic(t, "optionref")

	response := get(t, handler,
		"/api/repos/"+id+"/commits?scope=refs&ref=--output%3D%2Ftmp%2Fwritten")
	if response.Code != http.StatusBadRequest {
		t.Errorf("status = %d, expected 400: %s", response.Code, response.Body)
	}
}

// A row is a position in one walk. The same commit sits at a different row
// under a different chosen set, and at no row at all under one that does not
// reach it — which the route says in words rather than by scrolling somewhere
// wrong.
func TestOneCommitsRowIsAskedOfTheChosenSet(t *testing.T) {
	handler, id, _ := repositoryWithATopic(t, "locate")

	topic := decode[historyPayload](t, get(t, handler,
		"/api/repos/"+id+"/commits?scope=refs&ref=refs/heads/topic"))
	tip := topic.Commits[0].SHA

	if response := get(t, handler,
		"/api/repos/"+id+"/commits/"+tip+"?scope=refs&ref=refs/heads/topic"); response.Code != http.StatusOK {
		t.Errorf("the topic's tip under its own walk = %d: %s", response.Code, response.Body)
	}

	if response := get(t, handler,
		"/api/repos/"+id+"/commits/"+tip+"?scope=refs&ref=refs/heads/main"); response.Code != http.StatusNotFound {
		t.Errorf("the topic's tip under main's walk = %d, expected 404: %s",
			response.Code, response.Body)
	}
}

// A search says what walk it covered, because "in this history" is most of
// what a search means.
func TestSearchUnderAChosenSetCoversThatSet(t *testing.T) {
	handler, id, _ := repositoryWithATopic(t, "searchchosen")

	found := searchWithin(t, handler, id, "scope=refs&ref=refs/heads/topic", "message", "moved")
	if len(found.Commits) != 0 {
		t.Errorf("searching the topic found %d commits only main reaches", len(found.Commits))
	}

	found = searchWithin(t, handler, id, "scope=refs&ref=refs/heads/topic", "message", "topic")
	if len(found.Commits) != 1 {
		t.Errorf("searching the topic for its own commit found %d", len(found.Commits))
	}
}

func searchWithin(t *testing.T, handler http.Handler, id, walk, field, query string) searchAnswer {
	t.Helper()
	response := get(t, handler,
		"/api/repos/"+id+"/search?"+walk+"&in="+field+"&q="+query)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	return decode[searchAnswer](t, response)
}
