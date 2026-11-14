package api_test

import (
	"net/http"
	"path/filepath"
	"testing"
)

type wireIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// The global configuration under a file the test owns: PUT writes there.
// After serverOnRoot, which points it at the null device; git reads the
// variable when each command starts, so the later setting is the one used.
func ownGlobalConfiguration(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
}

func TestIdentityIsWrittenAndReadBack(t *testing.T) {
	handler, _ := serverOnRoot(t)
	ownGlobalConfiguration(t)

	saved := putJSON(t, handler, "/api/prefs/identity",
		`{"name":"Ada Lovelace","email":"ada@example.com"}`)
	if saved.Code != http.StatusOK {
		t.Fatalf("PUT status = %d: %s", saved.Code, saved.Body)
	}

	read := decode[wireIdentity](t, get(t, handler, "/api/prefs/identity"))
	if read.Name != "Ada Lovelace" || read.Email != "ada@example.com" {
		t.Fatalf("identity = %+v, want what was saved", read)
	}
}

func TestIdentityRefusesABlankName(t *testing.T) {
	handler, _ := serverOnRoot(t)
	ownGlobalConfiguration(t)

	response := putJSON(t, handler, "/api/prefs/identity", `{"name":"  ","email":"ada@example.com"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}
