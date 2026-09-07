package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLineHistoryTracksAChangedLine(t *testing.T) {
	handler, id, path := openNamedRepository(t, "line-hist")

	writeFile(t, path, "notes.md", "one\ntwo\nthree\n")
	runGitIn(t, path, "add", "notes.md")
	runGitIn(t, path, "commit", "-m", "add three lines")

	writeFile(t, path, "notes.md", "one\ntwo changed\nthree\n")
	runGitIn(t, path, "add", "notes.md")
	runGitIn(t, path, "commit", "-m", "change line two")

	writeFile(t, path, "notes.md", "one\ntwo changed\nthree\nfour\n")
	runGitIn(t, path, "add", "notes.md")
	runGitIn(t, path, "commit", "-m", "add line four")

	response := get(t, handler, "/api/repos/"+id+"/files/line-history?"+url.Values{
		"path": {"notes.md"},
		"line": {"2"},
	}.Encode())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	body := response.Body.String()
	if !strings.Contains(body, `"subject":"change line two"`) {
		t.Fatalf("missing change commit: %s", body)
	}
	if !strings.Contains(body, `"subject":"add three lines"`) {
		t.Fatalf("missing introducing commit: %s", body)
	}
	if strings.Contains(body, `"subject":"add line four"`) {
		t.Fatalf("included a commit that did not touch line two: %s", body)
	}
	if !strings.Contains(body, `"line":2`) {
		t.Fatalf("line missing: %s", body)
	}
}

func TestLineHistoryRefusesABadLine(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "line-bad")
	response := get(t, handler, "/api/repos/"+id+"/files/line-history?path=a.txt&line=0")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}
