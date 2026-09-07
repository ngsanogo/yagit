package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClonePlanShowsTheCommandWithoutRunningIt(t *testing.T) {
	handler, root := serverOnRoot(t)
	server := filepath.Join(root, "server.git")
	runGitIn(t, root, "init", "--bare", "-b", "main", server)

	destination := filepath.Join(root, "fresh")
	response := postJSON(t, handler, "/api/repos/clone/plan",
		fmt.Sprintf(`{"url":%q,"path":%q}`, server, destination))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	var plan struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.Command, "clone --progress") {
		t.Fatalf("command = %q", plan.Command)
	}
	if plan.Path != destination {
		t.Fatalf("path = %q, want %q", plan.Path, destination)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("plan must not create the destination: %v", err)
	}
}

func TestCloneStreamsProgressAndOpensTheRepository(t *testing.T) {
	handler, root := serverOnRoot(t)
	server := filepath.Join(root, "server.git")
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "init", "--bare", "-b", "main", server)
	runGitIn(t, seed, "init", "-b", "main")
	runGitIn(t, seed, "config", "user.name", "Ada Lovelace")
	runGitIn(t, seed, "config", "user.email", "ada@example.com")
	runGitIn(t, seed, "remote", "add", "origin", server)
	commitEmpty(t, seed, "A")
	runGitIn(t, seed, "push", "--set-upstream", "origin", "main:refs/heads/main")

	destination := filepath.Join(root, "fresh")
	response := postJSON(t, handler, "/api/repos/clone",
		fmt.Sprintf(`{"url":%q,"path":%q}`, server, destination))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "ndjson") {
		t.Fatalf("Content-Type = %q, want ndjson", response.Header().Get("Content-Type"))
	}

	var sawProgress bool
	var repository wireRepo
	for _, line := range strings.Split(strings.TrimSpace(response.Body.String()), "\n") {
		if line == "" {
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &head); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		switch head.Type {
		case "progress":
			sawProgress = true
		case "done":
			var done struct {
				Repository wireRepo `json:"repository"`
			}
			if err := json.Unmarshal([]byte(line), &done); err != nil {
				t.Fatal(err)
			}
			repository = done.Repository
		case "error":
			t.Fatalf("stream error: %s", line)
		default:
			t.Fatalf("unknown event %q", line)
		}
	}

	if !sawProgress {
		t.Fatal("stream carried no progress lines")
	}
	if repository.ID == "" || repository.Path != destination {
		t.Fatalf("repository = %+v", repository)
	}

	listed := get(t, handler, "/api/repos")
	if listed.Code != http.StatusOK {
		t.Fatalf("list: %d", listed.Code)
	}
	if !strings.Contains(listed.Body.String(), repository.ID) {
		t.Fatal("cloned repository was not left open")
	}
}

func TestCloneRefusesAPathOutsideTheRoot(t *testing.T) {
	handler, _ := serverOnRoot(t)
	outside := filepath.Join(t.TempDir(), "escape")
	response := postJSON(t, handler, "/api/repos/clone/plan",
		fmt.Sprintf(`{"url":%q,"path":%q}`, "https://example.test/x.git", outside))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}
}

func TestCloneRefusesAnExistingDestination(t *testing.T) {
	handler, root := serverOnRoot(t)
	destination := filepath.Join(root, "taken")
	if err := os.MkdirAll(destination, 0o755); err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, handler, "/api/repos/clone/plan",
		fmt.Sprintf(`{"url":%q,"path":%q}`, "https://example.test/x.git", destination))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
}

func TestCloneSurfacesAGitFailureOnTheStream(t *testing.T) {
	handler, root := serverOnRoot(t)
	destination := filepath.Join(root, "fresh")
	missing := filepath.Join(root, "missing.git")
	response := postJSON(t, handler, "/api/repos/clone",
		fmt.Sprintf(`{"url":%q,"path":%q}`, missing, destination))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	body := response.Body.String()
	if !strings.Contains(body, `"type":"error"`) {
		t.Fatalf("stream = %s, want an error event", body)
	}
	if !strings.Contains(body, "git") {
		t.Fatalf("error event lost the git failure: %s", body)
	}
}
