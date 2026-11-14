package api_test

import (
	"net/http"
	"testing"
)

type wireCompare struct {
	Head         string `json:"head"`
	AgainstShort string `json:"against_short"`
	Ahead        int    `json:"ahead"`
	Behind       int    `json:"behind"`
}

// main holds B, side holds S: one commit each since they parted.
func TestCompareCountsBothSidesOfTheFork(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/compare", `{"ref":"refs/heads/side"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body)
	}
	got := decode[wireCompare](t, response)
	if got.Head != "main" || got.AgainstShort != "side" || got.Ahead != 1 || got.Behind != 1 {
		t.Fatalf("compare = %+v, want main 1 ahead of side and 1 behind", got)
	}
}

// The repository has a branch and nothing else called side, but a short name
// is still refused: git would resolve it through refs/tags before refs/heads,
// and the answer would count against whichever it found first.
func TestCompareRefusesAShortName(t *testing.T) {
	handler, id, _ := openSwitchableRepository(t)

	response := postJSON(t, handler, "/api/repos/"+id+"/compare", `{"ref":"side"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
}
