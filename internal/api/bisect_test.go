package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBisectStartPlanNamesTheExactCommand(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/bisect/start/plan", `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var plan struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if plan.Command != "git bisect start" {
		t.Fatalf("command = %q", plan.Command)
	}
}

func TestBisectStartPutsTheRepositoryIntoABisect(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/bisect/start", `{}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	var payload struct {
		State struct {
			Operation string   `json:"operation"`
			Actions   []string `json:"actions"`
		} `json:"state"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.State.Operation != "bisect" {
		t.Fatalf("operation = %q, want bisect", payload.State.Operation)
	}
	want := []string{"good", "bad", "skip", "abort"}
	if !slices.Equal(payload.State.Actions, want) {
		t.Fatalf("actions = %v, want %v", payload.State.Actions, want)
	}
}

func TestBisectGoodAdvancesFromTheBanner(t *testing.T) {
	handler, id, path := openBisectableRepository(t)

	start := postJSON(t, handler, "/api/repos/"+id+"/bisect/start",
		`{"bad":"HEAD","good":["HEAD~2"]}`)
	if start.Code != http.StatusOK {
		t.Fatalf("start: %d: %s", start.Code, start.Body)
	}

	status := get(t, handler, "/api/repos/"+id+"/status")
	if status.Code != http.StatusOK {
		t.Fatalf("status: %d: %s", status.Code, status.Body)
	}
	var payload struct {
		State struct {
			Operation string `json:"operation"`
			Identity  string `json:"identity"`
		} `json:"state"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.State.Operation != "bisect" {
		t.Fatalf("operation = %q after start with ends", payload.State.Operation)
	}

	body, err := json.Marshal(map[string]string{
		"action":    "good",
		"operation": "bisect",
		"identity":  payload.State.Identity,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := postJSON(t, handler, "/api/repos/"+id+"/operation", string(body))
	if response.Code != http.StatusOK {
		t.Fatalf("good: %d: %s", response.Code, response.Body)
	}

	// The path is only here so a reader of the failure can find the fixture.
	_ = path
}

// openBisectableRepository is three commits deep so a start with bad=HEAD and
// good=HEAD~2 has a midpoint to check out.
func openBisectableRepository(t *testing.T) (http.Handler, string, string) {
	t.Helper()

	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "bisect-project")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	runGitIn(t, path, "init", "-b", "main")
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")

	for _, content := range []string{"one\n", "two\n", "three\n"} {
		writeInRepo(t, path, "f.txt", content)
		runGitIn(t, path, "add", "--", "f.txt")
		runGitIn(t, path, withIdentity("commit", "-m", content)...)
	}

	response := postJSON(t, handler, "/api/repos", `{"path":`+mustJSON(path)+`}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}
	return handler, decode[wireRepo](t, response).ID, path
}

func mustJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
