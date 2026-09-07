package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestBlameAttributesLines(t *testing.T) {
	handler, id, path := openNamedRepository(t, "blame-lines")

	writeFile(t, path, "notes.md", "one\n")
	runGitIn(t, path, "add", "notes.md")
	runGitIn(t, path, "commit", "-m", "add one")
	first := revision(t, path, "HEAD")

	writeFile(t, path, "notes.md", "one\ntwo\n")
	runGitIn(t, path, "add", "notes.md")
	runGitIn(t, path, "commit", "-m", "add two")

	response := get(t, handler, "/api/repos/"+id+"/files/blame?"+url.Values{
		"path": {"notes.md"},
	}.Encode())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	body := response.Body.String()
	if !strings.Contains(body, first) {
		t.Fatalf("missing first sha: %s", body)
	}
	if !strings.Contains(body, `"text":"two"`) {
		t.Fatalf("missing second line: %s", body)
	}
	if !strings.Contains(body, `"subject":"add two"`) {
		t.Fatalf("missing subject: %s", body)
	}
}

func TestBlameRefusesABadPath(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "blame-bad")
	response := get(t, handler, "/api/repos/"+id+"/files/blame?path=../etc/passwd")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}
