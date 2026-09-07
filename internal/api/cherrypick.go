package api

import (
	"fmt"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Cherry-picking one commit onto the branch HEAD is on, over HTTP.
//
// Two routes with the shape merge and rebase use: one says what would run, the
// other runs it. The plan is not decoration. A cherry-pick is one of three
// different things — nothing at all, a pointer moving onto the commit, or a
// new commit recorded under the user's hooks — and which one it is cannot be
// worked out in a browser: it is read from the commit and from HEAD at the
// moment the question is asked, and the command answered back is the one that
// pins it. See git.PreviewCherryPick and git.CherryPickArgs.
//
// The client never assembles a command and never decides what the cherry-pick
// is. It names the commit it clicked and echoes back the two facts the dialog
// showed it — which branch HEAD was on, and which of the outcomes was
// displayed — and both are read again here.
//
// Up-to-date is the one outcome with no command. Cherry-picking an ancestor
// fails on an empty patch, unlike `git merge --ff-only` which succeeds as a
// no-op, so the plan answers without a command and the run route verifies and
// returns without calling git. The other two run the line the plan showed.
//
// The refusals that come before any of it — HEAD on no branch, a repository
// already busy, HEAD somewhere other than the dialog said, a plan that stopped
// being true — are in branchop.go. A commit field left empty, or holding
// something git would read as an option, is refused by checkRevision on the
// reading below and answered 400: see git.ErrBadRevision.

var cherryPickOperation = branchOperation{gerund: "cherry-picking", preposition: "onto"}

// cherryPickPlanRequest names the commit a plan is about.
type cherryPickPlanRequest struct {
	// Commit is a revision. It reaches git after `--`, so anything git accepts
	// as a commit-ish works here — and checkRevision refuses a leading dash
	// before it gets that far.
	Commit string `json:"commit"`
}

// cherryPickRequest is the cherry-pick the user was shown, sent back to be
// carried out.
type cherryPickRequest struct {
	// Commit is the commit to pick. Sent back as the full name the plan
	// resolved, so a short SHA that became ambiguous since cannot silently
	// name a different object.
	Commit string `json:"commit"`

	// Into is the branch the dialog said this was going onto, and it is here
	// to be disagreed with. `git cherry-pick` acts on wherever HEAD happens to
	// be, and that is exactly the fact a confirmation cannot keep watching.
	Into string `json:"into"`

	// Outcome is which of the three the dialog displayed, and it is here to be
	// disagreed with as well.
	Outcome git.CherryPickOutcome `json:"outcome"`
}

// cherryPickPlan is what cherry-picking would do, before it does it.
type cherryPickPlan struct {
	// Command is the exact line, empty when the outcome is up-to-date — there
	// is no cherry-pick that succeeds as a no-op, and inventing one would put
	// a lie on the confirmation.
	Command string `json:"command"`

	Commit  string                `json:"commit"`
	Subject string                `json:"subject"`
	Into    string                `json:"into"`
	Outcome git.CherryPickOutcome `json:"outcome"`
}

// handlePlanCherryPick says what cherry-picking would run, without running it.
func (s *Server) handlePlanCherryPick(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body cherryPickPlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"commit": "…"}`) {
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewCherryPick(request.Context(), opened.Path, body.Commit)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	plan := cherryPickPlan{
		Commit:  preview.Commit,
		Subject: preview.Subject,
		Into:    into,
		Outcome: preview.Outcome,
	}
	if preview.Outcome != git.CherryPickUpToDate {
		plan.Command = git.CommandLine(git.CherryPickArgs(preview.Commit, preview.Outcome))
	}

	writeJSON(writer, s.logger, http.StatusOK, plan)
}

// handleCherryPick applies one commit onto the branch HEAD is on.
//
// A conflict is not an error this hides: git stops, writes the markers into
// the work tree and exits non-zero, and that reaches the interface as the
// failure it is — with git's own account, and with the repository in a state
// the banner names and the conflict screen resolves.
func (s *Server) handleCherryPick(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body cherryPickRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"commit": "…", "into": "…", "outcome": "up-to-date|fast-forward|cherry-pick"}`) {
		return
	}

	approved, err := git.ParseCherryPickOutcome(string(body.Outcome))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := cherryPickOperation.agreesOnBranch(body.Into, into); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewCherryPick(request.Context(), opened.Path, body.Commit)
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
		fmt.Sprintf("cherry-picking %s onto %s", git.ShortSHA(preview.Commit), into)); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.CherryPick(
		uninterrupted(request), opened.Path, preview.Commit, preview.Outcome); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}
