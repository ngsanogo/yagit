package api

import (
	"fmt"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Merging one local branch into the branch HEAD is on, over HTTP.
//
// Two routes with the shape the destructive operations in this package already
// use: one says what would run, the other runs it. The plan is not decoration
// here. A merge is one of three different things — a pointer moving, a commit
// recorded under the user's hooks and signature, or nothing at all — and which
// one it is cannot be worked out in a browser: it is read from the two
// branches, at the moment the question is asked, and the command answered back
// is the one that pins it. See git.MergeArgs.
//
// The working directory changes, and the answer is still the references — for
// the reason the pull route gives: that is what moved, and a sidebar that had
// to ask again would draw one frame of the state it just left. The status the
// changes panel reads is not in the answer; the interface polls it anyway
// (ADR 0015) and drops what it holds instead.
//
// Where the operation is decided is worth naming. The client never assembles a
// command and never decides what the merge is; it names the branch it clicked
// and echoes back the two facts the dialog showed it — which branch it was
// merging into, and which of the two commands it displayed. Both are checked
// against the repository as it is now, because a status two seconds old is how
// a merge lands somewhere nobody was looking.
//
// Checked means READ AGAIN. The plan is a sentence about two branches, and
// both of them can move while the dialog is open — from a second tab, a
// terminal, a pull. The flag alone cannot hold that sentence up: --no-ff
// refuses to fast-forward and --ff-only refuses to commit, but nothing in
// `git merge --ff-only` refuses to move a branch that was described as going
// nowhere. So the two branches are read once more, and a merge that has become
// something other than the one on screen is refused rather than run under the
// old description. See docs/adr/0022.

// The refusals that come before any of that — HEAD on no branch, a repository
// already busy, a branch that is the one HEAD is on, HEAD somewhere other than
// the dialog said, a plan that stopped being true — are not merge's own. They
// are in branchop.go, where rebase asks them in the same words.

// mergePlanRequest names the branch a plan is about.
//
// Its own type rather than the request below with two fields left empty: the
// plan is what ANSWERS those two, so a body that offered them would be asking
// the daemon to describe a merge the client had already decided.
type mergePlanRequest struct {
	// Branch is a local branch name. It reaches git after `--`, so anything
	// git accepts as a branch name works here.
	Branch string `json:"branch"`

	// MergeCommit asks for a merge commit even where a fast-forward is
	// possible — the door ADR 0021 left open. Ignored when the reading is
	// already a merge commit or up-to-date: the first needs no preference,
	// and the second has nothing to commit.
	MergeCommit bool `json:"merge_commit"`
}

// mergeRequest is the merge the user was shown, sent back to be carried out.
type mergeRequest struct {
	// Branch is the local branch to bring in.
	Branch string `json:"branch"`

	// Into is the branch the dialog said this was going into, and it is here
	// to be disagreed with. A merge whose destination moved under it is
	// refused rather than run: `git merge` acts on wherever HEAD happens to
	// be, and that is exactly the fact a confirmation cannot keep watching.
	Into string `json:"into"`

	// Outcome is which of the two commands the dialog displayed, and it is
	// here to be disagreed with as well. Required: what the route runs is the
	// outcome it reads for itself a moment later, and this is what that
	// reading is checked against — so a merge that has become a different
	// operation is refused instead of being carried out under the sentence
	// somebody approved for the old one.
	Outcome git.MergeOutcome `json:"outcome"`
}

// mergePlan is what merging would do, before it does it.
//
// The command is the part that has to be exact — it is drawn on the
// confirmation — and the rest is the same reading said in the terms a sentence
// needs. "Brings 3 commits into main, with a merge commit" reads better above
// a button than a flag does, and both have to come from one reading of the two
// branches or one of them will be describing a different merge.
type mergePlan struct {
	Command string `json:"command"`
	Branch  string `json:"branch"`

	// Into is the branch HEAD is on, read here rather than sent. It is what
	// the run route is later checked against, and a destination the browser
	// worked out for itself would be a second definition of where the merge
	// goes.
	Into string `json:"into"`

	Outcome git.MergeOutcome `json:"outcome"`

	// Ahead and Behind are the two sides of the divergence: what the current
	// branch has that the other does not, and how many commits are coming.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
}

// handlePlanMerge says what merging would run, without running it.
func (s *Server) handlePlanMerge(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body mergePlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"branch": "…"}`) {
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	branch, err := mergeOperation.otherBranch(body.Branch, into)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewMerge(request.Context(), opened.Path, into, branch)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	outcome := preview.Outcome
	if body.MergeCommit && outcome == git.MergeFastForward {
		outcome = git.MergeCommit
	}

	writeJSON(writer, s.logger, http.StatusOK, mergePlan{
		Command: git.CommandLine(git.MergeArgs(into, branch, outcome)),
		Branch:  branch,
		Into:    into,
		Outcome: outcome,
		Ahead:   preview.Ahead,
		Behind:  preview.Behind,
	})
}

// handleMerge brings another branch into the one HEAD is on.
//
// A conflict is not an error this hides: git stops, writes the markers into
// the work tree and exits non-zero, and that reaches the interface as the
// failure it is — with git's own account, and with the repository in a state
// the banner names and the conflict screen resolves.
func (s *Server) handleMerge(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body mergeRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"branch": "…", "into": "…", "outcome": "fast-forward|merge-commit|up-to-date"}`) {
		return
	}

	// Before anything read from the repository, because this one is about the
	// request: an outcome the daemon cannot name was never on any plan, and
	// saying so is a 400 rather than a report about two branches.
	approved, err := git.ParseMergeOutcome(string(body.Outcome))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// Before anything about the branch being brought in, because this is the
	// question whose answer changed: a request that named main and arrived at
	// release must hear that HEAD moved, not something about the branch it
	// asked for — which, by the time HEAD got there, may be the branch it is
	// standing on.
	if err := mergeOperation.agreesOnBranch(body.Into, into); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	branch, err := mergeOperation.otherBranch(body.Branch, into)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The plan again, from the repository rather than from the request. What
	// comes back chooses the command; what was approved only gets to agree
	// with it — with one deliberate exception: a merge-commit approval over a
	// fast-forward reading is the "create a merge commit anyway" preference
	// (ADR 0021), and --no-ff is what makes that preference true.
	preview, err := s.runner.PreviewMerge(request.Context(), opened.Path, into, branch)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	runAs := preview.Outcome
	if approved == git.MergeCommit && preview.Outcome == git.MergeFastForward {
		runAs = git.MergeCommit
	} else if err := agreesOnPlan(approved, preview.Outcome,
		fmt.Sprintf("merging %s into %s", branch, into)); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.Merge(
		uninterrupted(request), opened.Path, into, branch, runAs); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}
