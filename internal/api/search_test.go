package api_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

// searchableRepository: three commits, each the only match for one field.
func searchableRepository(t *testing.T, name string) (http.Handler, string) {
	t.Helper()
	handler, id, path := openNamedRepository(t, name)

	writeFile(t, path, "parser.go", "func tokenise() {}\n")
	runGitIn(t, path, "add", "-A")
	runGitIn(t, path, "-c", "user.name=Ada Lovelace", "-c", "user.email=ada@example.com",
		"commit", "-m", "add the parser")

	writeFile(t, path, "api.go", "func handler() {}\n")
	runGitIn(t, path, "add", "-A")
	runGitIn(t, path, "-c", "user.name=Grace Hopper", "-c", "user.email=grace@example.com",
		"commit", "-m", "fix(api): a paren (")

	return handler, id
}

type searchAnswer struct {
	Commits []struct {
		SHA     string `json:"sha"`
		Subject string `json:"subject"`
		Author  string `json:"author"`
	} `json:"commits"`
	Truncated bool   `json:"truncated"`
	Command   string `json:"command"`
}

func searchFor(t *testing.T, handler http.Handler, id, field, query string) searchAnswer {
	t.Helper()
	target := "/api/repos/" + id + "/search?in=" + field + "&q=" + url.QueryEscape(query)
	response := get(t, handler, target)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var answer searchAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestSearchFindsByEachField(t *testing.T) {
	handler, id := searchableRepository(t, "search-fields")

	for _, probe := range []struct {
		field, query, want string
	}{
		{"message", "the parser", "add the parser"},
		{"author", "grace", "fix(api): a paren ("},
		{"path", "api.go", "fix(api): a paren ("},
		{"content", "handler", "fix(api): a paren ("},
	} {
		answer := searchFor(t, handler, id, probe.field, probe.query)
		if len(answer.Commits) != 1 || answer.Commits[0].Subject != probe.want {
			t.Errorf("%s=%q found %+v, want %q", probe.field, probe.query, answer.Commits, probe.want)
		}
	}
}

// The query is a string and not a pattern: a search box that passed `fix(api)`
// to a regex engine would find nothing, and `a(b` would be an error about
// parentheses.
func TestSearchTakesTheQueryLiterally(t *testing.T) {
	handler, id := searchableRepository(t, "search-literal")

	if answer := searchFor(t, handler, id, "message", "fix(api)"); len(answer.Commits) != 1 {
		t.Fatalf("found %+v", answer.Commits)
	}
	if answer := searchFor(t, handler, id, "message", "a paren ("); len(answer.Commits) != 1 {
		t.Fatalf("found %+v", answer.Commits)
	}
}

func TestSearchAnswersWithTheCommandItRan(t *testing.T) {
	handler, id := searchableRepository(t, "search-command")

	answer := searchFor(t, handler, id, "message", "parser")
	if answer.Command == "" {
		t.Fatal("no command on the answer")
	}
}

func TestSearchRefusesAnEmptyQuery(t *testing.T) {
	handler, id := searchableRepository(t, "search-empty")

	response := get(t, handler, "/api/repos/"+id+"/search?in=message&q=")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestSearchRefusesAnUnknownField(t *testing.T) {
	handler, id := searchableRepository(t, "search-field")

	response := get(t, handler, "/api/repos/"+id+"/search?in=committer&q=ada")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

// The search and the graph beside it have to cover the same commits, or a
// result would be a row the graph cannot scroll to.
func TestSearchIsBoundedByTheScope(t *testing.T) {
	handler, id, path := openNamedRepository(t, "search-scope")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "-A")
	runGitIn(t, path, "commit", "-m", "on main")
	runGitIn(t, path, "switch", "-c", "side")
	writeFile(t, path, "f.txt", "two\n")
	runGitIn(t, path, "add", "-A")
	runGitIn(t, path, "commit", "-m", "only on side")
	runGitIn(t, path, "switch", "main")

	if answer := searchFor(t, handler, id, "message", "only on side"); len(answer.Commits) != 0 {
		t.Fatalf("the default scope reached a branch it does not draw: %+v", answer.Commits)
	}

	response := get(t, handler, "/api/repos/"+id+"/search?in=message&scope=all&q=only+on+side")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var answer searchAnswer
	if err := json.Unmarshal(response.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if len(answer.Commits) != 1 {
		t.Fatalf("scope=all found %+v, want the commit on side", answer.Commits)
	}
}
