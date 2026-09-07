package api_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileHistoryFollowsARename(t *testing.T) {
	handler, id, path := openNamedRepository(t, "history-rename")

	writeFile(t, path, "notes.md", "first\n")
	runGitIn(t, path, "add", "notes.md")
	runGitIn(t, path, "commit", "-m", "add notes")
	runGitIn(t, path, "mv", "notes.md", "README.md")
	runGitIn(t, path, "commit", "-m", "rename to README")

	response := get(t, handler, "/api/repos/"+id+"/files/history?"+url.Values{
		"path": {"README.md"},
	}.Encode())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	body := response.Body.String()
	if !strings.Contains(body, `"subject":"rename to README"`) {
		t.Fatalf("missing rename commit: %s", body)
	}
	if !strings.Contains(body, `"subject":"add notes"`) {
		t.Fatalf("missing followed add: %s", body)
	}
	if !strings.Contains(body, `"limit":100`) {
		t.Fatalf("limit missing: %s", body)
	}
}

func TestFileHistoryStartsAtARevision(t *testing.T) {
	handler, id, path := openNamedRepository(t, "history-rev")

	writeFile(t, path, "a.txt", "one\n")
	runGitIn(t, path, "add", "a.txt")
	runGitIn(t, path, "commit", "-m", "one")
	first := revision(t, path, "HEAD")

	writeFile(t, path, "a.txt", "two\n")
	runGitIn(t, path, "add", "a.txt")
	runGitIn(t, path, "commit", "-m", "two")

	response := get(t, handler, "/api/repos/"+id+"/files/history?"+url.Values{
		"path":     {"a.txt"},
		"revision": {first},
	}.Encode())
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	body := response.Body.String()
	if strings.Contains(body, `"subject":"two"`) {
		t.Fatalf("walked past the revision: %s", body)
	}
	if !strings.Contains(body, `"subject":"one"`) {
		t.Fatalf("missing starting commit: %s", body)
	}
}

func TestFileHistoryRefusesABadPath(t *testing.T) {
	handler, id, _ := openNamedRepository(t, "history-bad")
	response := get(t, handler, "/api/repos/"+id+"/files/history?path=../etc/passwd")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func openNamedRepository(t *testing.T, name string) (http.Handler, string, string) {
	t.Helper()
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "init", "-b", "main", path)
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")
	commitEmpty(t, path, "root")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}
	return handler, decode[wireRepo](t, response).ID, path
}

func writeFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
