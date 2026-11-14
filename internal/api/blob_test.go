package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestBlobServesAnImageFromTheWorkTree(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	png := "\x89PNG\r\n\x1a\n\x00\x00"
	writeInRepo(t, path, "logo.png", png)

	response := get(t, handler, "/api/repos/"+id+"/blob?path=logo.png&at=worktree")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q", got)
	}
	if response.Body.String() != png {
		t.Fatalf("body = %q", response.Body)
	}
}

func TestBlobServesStagedAndHeadSides(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	oldPNG := "\x89PNG\r\n\x1a\nold"
	newPNG := "\x89PNG\r\n\x1a\nnew"
	writeInRepo(t, path, "logo.png", oldPNG)
	runGitIn(t, path, withIdentity("add", "--", "logo.png")...)
	runGitIn(t, path, withIdentity("commit", "-m", "image")...)
	writeInRepo(t, path, "logo.png", newPNG)
	runGitIn(t, path, withIdentity("add", "--", "logo.png")...)

	head := get(t, handler, "/api/repos/"+id+"/blob?path=logo.png&at=head")
	if head.Code != http.StatusOK || head.Body.String() != oldPNG {
		t.Fatalf("head: status = %d body = %q", head.Code, head.Body)
	}

	index := get(t, handler, "/api/repos/"+id+"/blob?path=logo.png&at=index")
	if index.Code != http.StatusOK || index.Body.String() != newPNG {
		t.Fatalf("index: status = %d body = %q", index.Code, index.Body)
	}
}

func TestBlobRefusesANonImage(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/blob?path=a.txt&at=worktree")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestBlobRefusesAPathThatLeavesTheRepository(t *testing.T) {
	handler, id, _ := openWorkingRepository(t)

	response := get(t, handler, "/api/repos/"+id+"/blob?path=../outside.png&at=worktree")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}

func TestBlobRefusesAMissingAt(t *testing.T) {
	handler, id, path := openWorkingRepository(t)
	writeInRepo(t, path, "logo.png", "\x89PNG\r\n\x1a\n")

	response := get(t, handler, "/api/repos/"+id+"/blob?path=logo.png")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "at is required") {
		t.Fatalf("refusal = %s", response.Body)
	}
}
