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

func TestUndoCommitSoftResetsTheTip(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-commit")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	writeFile(t, path, "f.txt", "two\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "second")

	offer := get(t, handler, "/api/repos/"+id+"/undo")
	if offer.Code != http.StatusOK {
		t.Fatalf("offer status = %d: %s", offer.Code, offer.Body)
	}
	if !strings.Contains(offer.Body.String(), `"available":true`) {
		t.Fatalf("want available: %s", offer.Body)
	}

	plan := postJSON(t, handler, "/api/repos/"+id+"/undo/plan", `{}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	var pending struct {
		Command string `json:"command"`
		Kind    string `json:"kind"`
		Into    string `json:"into"`
		Head    string `json:"head"`
		To      string `json:"to"`
		ToRef   string `json:"to_ref"`
		Subject string `json:"subject"`
		Detach  bool   `json:"detach"`
	}
	if err := json.Unmarshal(plan.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Subject != "second" || pending.Kind != "commit" {
		t.Fatalf("plan = %+v", pending)
	}
	if !strings.Contains(pending.Command, "reset --soft") {
		t.Fatalf("command = %q", pending.Command)
	}

	body, err := json.Marshal(map[string]any{
		"kind":   pending.Kind,
		"into":   pending.Into,
		"head":   pending.Head,
		"to":     pending.To,
		"to_ref": pending.ToRef,
		"detach": pending.Detach,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := postJSON(t, handler, "/api/repos/"+id+"/undo", string(body))
	if run.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", run.Code, run.Body)
	}

	head := revision(t, path, "HEAD")
	if head != pending.To {
		t.Fatalf("HEAD = %s, want %s", head, pending.To)
	}
}

func TestUndoCheckoutSwitchesBack(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-co")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	runGitIn(t, path, "switch", "-c", "side")
	writeFile(t, path, "f.txt", "side\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "on side")
	runGitIn(t, path, "switch", "main")

	offer := get(t, handler, "/api/repos/"+id+"/undo")
	if offer.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", offer.Code, offer.Body)
	}
	if !strings.Contains(offer.Body.String(), `"kind":"checkout"`) {
		t.Fatalf("want checkout offer: %s", offer.Body)
	}

	plan := postJSON(t, handler, "/api/repos/"+id+"/undo/plan", `{}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	var pending struct {
		Command string `json:"command"`
		Kind    string `json:"kind"`
		Into    string `json:"into"`
		Head    string `json:"head"`
		To      string `json:"to"`
		ToRef   string `json:"to_ref"`
		Subject string `json:"subject"`
		Detach  bool   `json:"detach"`
	}
	if err := json.Unmarshal(plan.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.ToRef != "side" || pending.Detach {
		t.Fatalf("plan = %+v", pending)
	}
	if !strings.Contains(pending.Command, "switch --no-guess") {
		t.Fatalf("command = %q", pending.Command)
	}

	body, err := json.Marshal(map[string]any{
		"kind":   pending.Kind,
		"into":   pending.Into,
		"head":   pending.Head,
		"to":     pending.To,
		"to_ref": pending.ToRef,
		"detach": pending.Detach,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := postJSON(t, handler, "/api/repos/"+id+"/undo", string(body))
	if run.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", run.Code, run.Body)
	}

	if got := revision(t, path, "HEAD"); got != pending.To {
		t.Fatalf("HEAD = %s, want %s", got, pending.To)
	}
	if got := revision(t, path, "side"); got != pending.To {
		t.Fatalf("side = %s, want %s", got, pending.To)
	}
	// Still on side: symbolic-ref fails when detached.
	runGitIn(t, path, "symbolic-ref", "-q", "HEAD")
}

func TestUndoResetPutsTheBranchBack(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-reset")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	writeFile(t, path, "f.txt", "two\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "second")
	tip := revision(t, path, "HEAD")
	runGitIn(t, path, "reset", "--hard", "HEAD~1")

	plan := postJSON(t, handler, "/api/repos/"+id+"/undo/plan", `{}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	var pending struct {
		Command string `json:"command"`
		Kind    string `json:"kind"`
		Into    string `json:"into"`
		Head    string `json:"head"`
		To      string `json:"to"`
		ToRef   string `json:"to_ref"`
		Subject string `json:"subject"`
		Detach  bool   `json:"detach"`
	}
	if err := json.Unmarshal(plan.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Kind != "reset" || pending.To != tip {
		t.Fatalf("plan = %+v, want a reset back to %s", pending, tip)
	}
	if !strings.Contains(pending.Command, "reset --soft") {
		t.Fatalf("command = %q", pending.Command)
	}

	body, err := json.Marshal(map[string]any{
		"kind":   pending.Kind,
		"into":   pending.Into,
		"head":   pending.Head,
		"to":     pending.To,
		"to_ref": pending.ToRef,
		"detach": pending.Detach,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := postJSON(t, handler, "/api/repos/"+id+"/undo", string(body))
	if run.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", run.Code, run.Body)
	}
	if head := revision(t, path, "HEAD"); head != tip {
		t.Fatalf("HEAD = %s, want %s", head, tip)
	}
}

func TestUndoOfferAbsentWhenTipIsNotUndoable(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-absent")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	// A stash resets to HEAD on its way past: a reset entry at the tip that
	// moved no ref, and nothing undo can honestly offer to reverse.
	writeFile(t, path, "f.txt", "two\n")
	runGitIn(t, path, "stash", "push", "-m", "aside")

	offer := get(t, handler, "/api/repos/"+id+"/undo")
	if offer.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", offer.Code, offer.Body)
	}
	if !strings.Contains(offer.Body.String(), `"available":false`) {
		t.Fatalf("want unavailable after a stash: %s", offer.Body)
	}
}

func TestUndoBranchDeletionPutsItBack(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-branch-delete")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	runGitIn(t, path, "switch", "-c", "side")
	writeFile(t, path, "f.txt", "side\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "work only side had")
	side := revision(t, path, "HEAD")
	runGitIn(t, path, "switch", "main")

	// Through the route, because the tip is read there and nowhere else: a
	// branch deleted in a terminal leaves nothing for undo to find.
	deleted := postJSON(t, handler, "/api/repos/"+id+"/branches/delete", `{"name":"side","force":true}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body)
	}

	offer := get(t, handler, "/api/repos/"+id+"/undo")
	if !strings.Contains(offer.Body.String(), `"kind":"branch-delete"`) ||
		!strings.Contains(offer.Body.String(), `"branch":"side"`) {
		t.Fatalf("want a branch-delete offer naming side: %s", offer.Body)
	}

	plan := postJSON(t, handler, "/api/repos/"+id+"/undo/plan", `{}`)
	if plan.Code != http.StatusOK {
		t.Fatalf("plan status = %d: %s", plan.Code, plan.Body)
	}
	var pending struct {
		Command string `json:"command"`
		Kind    string `json:"kind"`
		Into    string `json:"into"`
		Head    string `json:"head"`
		To      string `json:"to"`
		ToRef   string `json:"to_ref"`
		Branch  string `json:"branch"`
		Detach  bool   `json:"detach"`
	}
	if err := json.Unmarshal(plan.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Branch != "side" || pending.To != side {
		t.Fatalf("plan = %+v, want side at %s", pending, side)
	}
	if !strings.Contains(pending.Command, "branch -- side") {
		t.Fatalf("command = %q", pending.Command)
	}

	body, err := json.Marshal(map[string]any{
		"kind":   pending.Kind,
		"into":   pending.Into,
		"head":   pending.Head,
		"to":     pending.To,
		"to_ref": pending.ToRef,
		"branch": pending.Branch,
		"detach": pending.Detach,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := postJSON(t, handler, "/api/repos/"+id+"/undo", string(body))
	if run.Code != http.StatusOK {
		t.Fatalf("run status = %d: %s", run.Code, run.Body)
	}
	if got := revision(t, path, "side"); got != side {
		t.Fatalf("side = %s, want %s", got, side)
	}

	// Spent: the same undo is not offered twice.
	again := get(t, handler, "/api/repos/"+id+"/undo")
	if strings.Contains(again.Body.String(), `"kind":"branch-delete"`) {
		t.Fatalf("branch-delete offered again after it ran: %s", again.Body)
	}
}

// The branch is back, so the record is spent and the reflog answers.
//
// This is one of exactly three failures that mean the record went stale — the
// others being HEAD moving on and `git gc` taking the commits. undoPreview
// tells them apart from a git that simply could not run, and only this kind
// drops the record: the tip of a deleted branch exists nowhere git will name,
// so spending the record over a failed subprocess would lose the restore for
// good and quietly replace the offer with whatever the reflog held.
func TestUndoBranchDeletionYieldsWhenTheBranchIsBack(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-delete-branch-back")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	runGitIn(t, path, "switch", "-c", "side")
	writeFile(t, path, "f.txt", "side\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "work only side had")
	side := revision(t, path, "HEAD")
	runGitIn(t, path, "switch", "main")

	deleted := postJSON(t, handler, "/api/repos/"+id+"/branches/delete", `{"name":"side","force":true}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body)
	}
	if offer := get(t, handler, "/api/repos/"+id+"/undo"); !strings.Contains(
		offer.Body.String(), `"kind":"branch-delete"`) {
		t.Fatalf("want a branch-delete offer before the branch returns: %s", offer.Body)
	}

	// Put it back outside yagit, which is what the record competes with.
	runGitIn(t, path, "branch", "side", side)

	after := get(t, handler, "/api/repos/"+id+"/undo")
	if after.Code != http.StatusOK {
		t.Fatalf("offer status = %d: %s", after.Code, after.Body)
	}
	if strings.Contains(after.Body.String(), `"kind":"branch-delete"`) {
		t.Fatalf("a branch that is back was still offered as a restore: %s", after.Body)
	}
}

// A commit after the deletion is more recent than it, and the reflog gets the
// offer back.
func TestUndoBranchDeletionYieldsToALaterCommit(t *testing.T) {
	handler, id, path := openNamedRepository(t, "undo-delete-then-commit")

	writeFile(t, path, "f.txt", "one\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "first")
	runGitIn(t, path, "branch", "side")

	deleted := postJSON(t, handler, "/api/repos/"+id+"/branches/delete", `{"name":"side","force":true}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleted.Code, deleted.Body)
	}

	writeFile(t, path, "f.txt", "two\n")
	runGitIn(t, path, "add", "f.txt")
	runGitIn(t, path, "commit", "-m", "after the delete")

	offer := get(t, handler, "/api/repos/"+id+"/undo")
	if !strings.Contains(offer.Body.String(), `"kind":"commit"`) {
		t.Fatalf("want the commit offer back: %s", offer.Body)
	}
}

// Right after `git init` there is no HEAD and no reflog. Undo must say so
// quietly — available:false — rather than answering with git's
// "ambiguous argument 'HEAD'" fatal as a 422.
func TestUndoOfferOnAnUnbornBranch(t *testing.T) {
	handler, root := serverOnRoot(t)
	path := filepath.Join(root, "undo-unborn")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, root, "init", "-b", "main", path)

	opened := postJSON(t, handler, "/api/repos", fmt.Sprintf(`{"path":%q}`, path))
	if opened.Code != http.StatusCreated {
		t.Fatalf("opening: status = %d: %s", opened.Code, opened.Body)
	}
	id := decode[wireRepo](t, opened).ID

	offer := get(t, handler, "/api/repos/"+id+"/undo")
	if offer.Code != http.StatusOK {
		t.Fatalf("offer status = %d: %s", offer.Code, offer.Body)
	}
	if !strings.Contains(offer.Body.String(), `"available":false`) {
		t.Fatalf("want available:false on an unborn branch, got %s", offer.Body)
	}
}
