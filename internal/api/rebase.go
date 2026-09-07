package api

import (
	"fmt"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Rebasing the branch HEAD is on onto another local branch, over HTTP.
//
// Two routes with the shape merge uses: one says what would run, the other
// runs it. The plan is not decoration. A rebase is one of three different
// things — nothing at all, a branch moving onto a new base, or every commit on
// the branch written again under a new hash — and which one it is cannot be
// worked out in a browser: it is read from the two branches at the moment the
// question is asked, and the command answered back is the one that pins it.
// See git.PreviewRebase and git.RebaseArgs.
//
// The client never assembles a command and never decides what the rebase is.
// It names the branch it clicked and echoes back the two facts the dialog
// showed it — which branch HEAD was on, and which of the outcomes was
// displayed — and both are read again here, because a status two seconds old
// is how a branch gets rewritten under somebody who was told nothing would
// happen.
//
// That re-reading carries more weight here than it does for merge, and the
// difference is worth naming. A merge has `--ff-only` and `--no-ff`, so its
// command refuses two of the three ways the plan could have gone stale. git
// offers a rebase no such flag: `--no-ff` pins the replay and there is nothing
// that pins the other two, so for them the reading below is the whole of the
// lease. See docs/adr/0023.
//
// The working directory changes, and the answer is still the references, for
// the reason the merge route gives: that is what moved, and the changes panel
// polls anyway (ADR 0015).
//
// The refusals that come before any of it — HEAD on no branch, a repository
// already busy, a branch that is the one HEAD is on, HEAD somewhere other than
// the dialog said, a plan that stopped being true — are in branchop.go, where
// merge asks them in the same words.

// rebasePlanRequest names the branch a plan is about.
//
// Its own type rather than the request below with two fields left empty: the
// plan is what ANSWERS those two, so a body that offered them would be asking
// the daemon to describe a rebase the client had already decided.
type rebasePlanRequest struct {
	// Onto is a local branch name. It reaches git after `--`, so anything git
	// accepts as a branch name works here.
	Onto string `json:"onto"`
}

// rebaseRequest is the rebase the user was shown, sent back to be carried out.
type rebaseRequest struct {
	// Onto is the local branch to replay onto.
	Onto string `json:"onto"`

	// From is the branch the dialog said was being rebased, and it is here to
	// be disagreed with. `git rebase` moves whatever HEAD happens to be on,
	// and that is exactly the fact a confirmation cannot keep watching.
	From string `json:"from"`

	// Outcome is which of the three the dialog displayed, and it is here to be
	// disagreed with as well. Required: what the route runs is the outcome it
	// reads for itself a moment later, and this is what that reading is
	// checked against — so a rebase that has become a different operation is
	// refused instead of being carried out under the sentence somebody
	// approved for the old one.
	Outcome git.RebaseOutcome `json:"outcome"`
}

// rebasePlan is what rebasing would do, before it does it.
//
// The command is the part that has to be exact — it is drawn on the
// confirmation — and the rest is the same reading said in the terms a sentence
// and a warning need.
type rebasePlan struct {
	Command string `json:"command"`
	Onto    string `json:"onto"`

	// From is the branch HEAD is on, read here rather than sent. It is what
	// the run route is later checked against, and a name the browser worked
	// out for itself would be a second definition of what is being rebased.
	From string `json:"from"`

	Outcome git.RebaseOutcome `json:"outcome"`

	// Rewriting and Flattening are what the rebase costs: the ordinary commits
	// that stop being what the branch points at, and the merge commits
	// discarded rather than recreated. Both are zero unless the outcome is a
	// replay, and both are on the confirmation because a rebase is the one
	// operation here that takes history away.
	Rewriting  int `json:"rewriting"`
	Flattening int `json:"flattening"`

	// Behind is how many commits the new base holds that the branch does not —
	// how far there is to move.
	Behind int `json:"behind"`
}

// handlePlanRebase says what rebasing would run, without running it.
func (s *Server) handlePlanRebase(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body rebasePlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"onto": "…"}`) {
		return
	}

	from, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	onto, err := rebaseOperation.otherBranch(body.Onto, from)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewRebase(request.Context(), opened.Path, from, onto)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, rebasePlan{
		Command:    git.CommandLine(git.RebaseArgs(onto, preview.Outcome)),
		Onto:       onto,
		From:       from,
		Outcome:    preview.Outcome,
		Rewriting:  preview.Rewriting,
		Flattening: preview.Flattening,
		Behind:     preview.Behind,
	})
}

// handleRebase replays the branch HEAD is on onto another.
//
// A conflict is not an error this hides: git stops partway through the
// sequence, writes the markers into the work tree and exits non-zero, and that
// reaches the interface as the failure it is — with git's own account, and
// with the repository in a state the banner names and the conflict screen
// resolves.
func (s *Server) handleRebase(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body rebaseRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"onto": "…", "from": "…", "outcome": "up-to-date|fast-forward|rebase"}`) {
		return
	}

	// Before anything read from the repository, because this one is about the
	// request: an outcome the daemon cannot name was never on any plan, and
	// saying so is a 400 rather than a report about two branches.
	approved, err := git.ParseRebaseOutcome(string(body.Outcome))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	from, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// Before anything about the branch being replayed onto, because this is
	// the question whose answer changed: a request that named feature and
	// arrived on main must hear that HEAD moved, not something about the
	// branch it asked for — which, by the time HEAD got there, may be the
	// branch it is standing on.
	if err := rebaseOperation.agreesOnBranch(body.From, from); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	onto, err := rebaseOperation.otherBranch(body.Onto, from)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The plan again, from the repository rather than from the request. What
	// comes back chooses the command; what was approved only gets to agree
	// with it.
	preview, err := s.runner.PreviewRebase(request.Context(), opened.Path, from, onto)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if err := agreesOnPlan(approved, preview.Outcome,
		fmt.Sprintf("rebasing %s onto %s", from, onto)); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.Rebase(
		uninterrupted(request), opened.Path, onto, preview.Outcome); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}
