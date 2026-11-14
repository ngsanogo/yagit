package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// wireMergetoolPlan mirrors what POST /mergetool/plan sends.
type wireMergetoolPlan struct {
	Command string `json:"command"`
}

func TestMergetoolPlanNamesTheExactCommand(t *testing.T) {
	handler, id, _ := conflictedRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/mergetool/plan", `{"path":"f.txt"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	plan := decode[wireMergetoolPlan](t, response)
	if plan.Command != "git mergetool -y -- ':(literal)f.txt'" {
		t.Fatalf("command = %q", plan.Command)
	}
}

func TestMergetoolRefusesAPathThatIsNotConflicted(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/mergetool", `{"path":"a.txt"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "unmerged") {
		t.Fatalf("the refusal does not say why: %s", response.Body)
	}
}

func TestMergetoolResolvesThroughAConfiguredTool(t *testing.T) {
	handler, id, path := conflictedRepository(t)
	installFakeMergetool(t, path)

	response := postJSON(t, handler, "/api/repos/"+id+"/mergetool", `{"path":"f.txt"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}

	for _, file := range decode[wireStatus](t, response).Files {
		if file.Path == "f.txt" && file.Kind == "unmerged" {
			t.Fatal("still unmerged after the mergetool ran")
		}
	}
	if got := readInRepo(t, path, "f.txt"); !strings.Contains(got, "MERGED") {
		t.Fatalf("f.txt = %q, want the fake tool's content", got)
	}
}

// installFakeMergetool points merge.tool at a script that writes a fixed
// resolution into $MERGED and exits 0 — so the test does not need a display.
func installFakeMergetool(t *testing.T, repo string) {
	t.Helper()

	script := filepath.Join(t.TempDir(), "fake-mergetool")
	if runtime.GOOS == "windows" {
		script += ".cmd"
		content := "@echo MERGED> %~4\r\n"
		if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
			t.Fatalf("write tool: %v", err)
		}
		runGitIn(t, repo, "config", "mergetool.fake.cmd", `"`+script+`" "$BASE" "$LOCAL" "$REMOTE" "$MERGED"`)
	} else {
		content := "#!/bin/sh\nprintf 'MERGED\\n' > \"$4\"\n"
		if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
			t.Fatalf("write tool: %v", err)
		}
		runGitIn(t, repo, "config", "mergetool.fake.cmd", script+` "$BASE" "$LOCAL" "$REMOTE" "$MERGED"`)
	}
	runGitIn(t, repo, "config", "merge.tool", "fake")
	runGitIn(t, repo, "config", "mergetool.fake.trustExitCode", "true")
}
