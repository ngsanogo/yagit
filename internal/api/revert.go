package api

import (
	"fmt"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Reverting one commit onto the branch HEAD is on, over HTTP.
//
// Two routes with the shape cherry-pick uses: one says what would run, the
// other runs it. The plan is not decoration. A revert always records a new
// commit under the user's hooks, and the command answered back is the one that
// pins `--no-edit` so the daemon never looks for a terminal, and
// `--no-reference` so `revert.reference` in somebody's ~/.gitconfig cannot
// commit the placeholder subject that option leaves for an editor. See
// git.PreviewRevert and git.RevertArgs.
//
// The client never assembles a command. It names the commit it clicked and
// echoes back the two facts the dialog showed it — which branch HEAD was on,
// and which outcome was displayed — and both are read again here.
//
// The refusals that come before any of it — HEAD on no branch, a repository
// already busy, HEAD somewhere other than the dialog said, a plan that stopped
// being true — are in branchop.go. A merge, a root, or a commit not on the
// branch is refused by PreviewRevert. A commit field left empty, or holding
// something git would read as an option, is refused by checkRevision on the
// reading below and answered 400: see git.ErrBadRevision.

var revertOperation = branchOperation{gerund: "reverting", preposition: "on"}

// revertPlanRequest names the commit a plan is about.
type revertPlanRequest struct {
	// Commit is a revision. It reaches git after `--`, so anything git accepts
	// as a commit-ish works here — and checkRevision refuses a leading dash
	// before it gets that far.
	Commit string `json:"commit"`
}

// revertRequest is the revert the user was shown, sent back to be carried out.
type revertRequest struct {
	// Commit is the commit to revert. Sent back as the full name the plan
	// resolved, so a short SHA that became ambiguous since cannot silently
	// name a different object.
	Commit string `json:"commit"`

	// Into is the branch the dialog said this was going onto, and it is here
	// to be disagreed with. `git revert` acts on wherever HEAD happens to be,
	// and that is exactly the fact a confirmation cannot keep watching.
	Into string `json:"into"`

	// Outcome is which of the outcomes the dialog displayed, and it is here to
	// be disagreed with as well.
	Outcome git.RevertOutcome `json:"outcome"`
}

// revertPlan is what reverting would do, before it does it.
type revertPlan struct {
	Command string            `json:"command"`
	Commit  string            `json:"commit"`
	Subject string            `json:"subject"`
	Into    string            `json:"into"`
	Outcome git.RevertOutcome `json:"outcome"`
}

// handlePlanRevert says what reverting would run, without running it.
func (s *Server) handlePlanRevert(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body revertPlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"commit": "…"}`) {
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewRevert(request.Context(), opened.Path, body.Commit)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, revertPlan{
		Command: git.CommandLine(git.RevertArgs(preview.Commit)),
		Commit:  preview.Commit,
		Subject: preview.Subject,
		Into:    into,
		Outcome: preview.Outcome,
	})
}

// handleRevert applies the inverse of one commit onto the branch HEAD is on.
//
// A conflict is not an error this hides: git stops, writes the markers into
// the work tree and exits non-zero, and that reaches the interface as the
// failure it is — with git's own account, and with the repository in a state
// the banner names and the conflict screen resolves.
func (s *Server) handleRevert(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body revertRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"commit": "…", "into": "…", "outcome": "revert"}`) {
		return
	}

	approved, err := git.ParseRevertOutcome(string(body.Outcome))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := revertOperation.agreesOnBranch(body.Into, into); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewRevert(request.Context(), opened.Path, body.Commit)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The resolved name, not the one the client typed. A plan answered with a
	// full SHA and a run that accepted a different short form of a different
	// object would be two definitions of what was approved.
	if err := agreesOnCommit(body.Commit, preview.Commit); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := agreesOnPlan(approved, preview.Outcome,
		fmt.Sprintf("reverting %s on %s", git.ShortSHA(preview.Commit), into)); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.Revert(
		uninterrupted(request), opened.Path, preview.Commit, preview.Outcome); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}
