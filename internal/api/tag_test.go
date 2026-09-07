package api_test

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateTagAppearsInTheRefs(t *testing.T) {
	handler, id := openTestRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":"first release"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), `"short_name":"v1.0"`) {
		t.Fatalf("refs missing the tag: %s", response.Body)
	}
}

func TestCreateTagRefusesAnEmptyMessage(t *testing.T) {
	handler, id := openTestRepository(t)
	response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":""}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestCreateTagLightweightNeedsNoMessage(t *testing.T) {
	handler, id := openTestRepository(t)
	response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":"","annotated":false}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), `"short_name":"v1.0"`) {
		t.Fatalf("refs missing the tag: %s", response.Body)
	}
}

func TestCreateTagDefaultsToAnnotatedWhenTheFlagIsOmitted(t *testing.T) {
	handler, id := openTestRepository(t)
	// Empty message with no annotated field must still be the annotated
	// refusal — not a silent lightweight create.
	response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestDeleteTagPlanShowsTheCommand(t *testing.T) {
	handler, id := openTestRepository(t)
	if response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":"first"}`); response.Code != http.StatusOK {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/tags/delete/plan",
		`{"name":"v1.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "tag -d -- v1.0") {
		t.Fatalf("command = %s", response.Body)
	}
}

func TestDeleteTagRemovesItFromTheRefs(t *testing.T) {
	handler, id := openTestRepository(t)
	if response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":"first"}`); response.Code != http.StatusOK {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/tags/delete",
		`{"name":"v1.0"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), `"short_name":"v1.0"`) {
		t.Fatalf("tag still listed: %s", response.Body)
	}
}

func TestDeleteTagSurfacesGitsRefusal(t *testing.T) {
	handler, id := openTestRepository(t)
	response := postJSON(t, handler, "/api/repos/"+id+"/tags/delete",
		`{"name":"no-such-tag"}`)
	if response.Code == http.StatusOK {
		t.Fatal("deleting a missing tag must fail")
	}
	if !strings.Contains(response.Body.String(), "git") {
		t.Fatalf("error lost the git failure: %s", response.Body)
	}
}

func TestPushTagPlanShowsTheCommand(t *testing.T) {
	handler, id, _, _ := openClonedRepository(t)
	if response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":"first"}`); response.Code != http.StatusOK {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/tags/push/plan",
		`{"name":"v1.0","remote":"origin"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	body := response.Body.String()
	if !strings.Contains(body, "push -- origin refs/tags/v1.0:refs/tags/v1.0") {
		t.Fatalf("command = %s", body)
	}
	if strings.Contains(body, "--force") {
		t.Fatalf("tag push must not force: %s", body)
	}
}

func TestPushTagSendsItToTheRemote(t *testing.T) {
	handler, id, _, server := openClonedRepository(t)
	if response := postJSON(t, handler, "/api/repos/"+id+"/tags",
		`{"name":"v1.0","message":"first"}`); response.Code != http.StatusOK {
		t.Fatalf("create: %d %s", response.Code, response.Body)
	}

	response := postJSON(t, handler, "/api/repos/"+id+"/tags/push",
		`{"name":"v1.0","remote":"origin"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	got := revision(t, server, "refs/tags/v1.0")
	if got == "" {
		t.Fatal("tag missing on the server")
	}
}

func TestPushTagPlanRefusesEmptyFields(t *testing.T) {
	handler, id, _, _ := openClonedRepository(t)

	for name, body := range map[string]string{
		"no name":   `{"name":"","remote":"origin"}`,
		"no remote": `{"name":"v1.0","remote":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := postJSON(t, handler, "/api/repos/"+id+"/tags/push/plan", body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400: %s", response.Code, response.Body)
			}
		})
	}
}

// openTestRepository opens a one-commit repository through the API.
func openTestRepository(t *testing.T) (http.Handler, string) {
	t.Helper()
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "project")
	runGitIn(t, root, "init", "-b", "main", path)
	runGitIn(t, path, "config", "user.name", "Ada Lovelace")
	runGitIn(t, path, "config", "user.email", "ada@example.com")
	commitEmpty(t, path, "A")

	response := postJSON(t, handler, "/api/repos", fmt.Sprintf("{%q:%q}", "path", path))
	if response.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", response.Code, response.Body)
	}
	return handler, decode[wireRepo](t, response).ID
}
