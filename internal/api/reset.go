package api

import (
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Resetting the branch HEAD is on to a selected commit, over HTTP.
//
// Two routes with the shape revert uses: one says what would run, the other
// runs it. The mode is a choice the client makes — soft, mixed or hard — and
// travels with both requests, for the reason pull's strategy does: nothing in
// the repository decides which of the three trees move, and a bare `git reset`
// would quietly mean mixed. See git.PreviewReset and git.ResetArgs.
//
// The client never assembles a command. It names the commit it clicked, the
// mode it picked, and echoes back the branch the dialog showed it — and all
// three are read again here.
//
// The refusals that come before any of it — HEAD on no branch, a repository
// already busy, HEAD somewhere other than the dialog said — are in branchop.go.
// A commit not on the branch is refused by ResetTarget, which both routes read
// through. A commit field left empty, or holding something git would
// read as an option, is refused by checkRevision on the reading below and
// answered 400: see git.ErrBadRevision.

var resetOperation = branchOperation{gerund: "resetting", preposition: "on"}

// resetPlanRequest names the commit a plan is about, and which mode.
type resetPlanRequest struct {
	// Commit is a revision. checkRevision refuses a leading dash before it
	// reaches git — reset has no `--` pathspec separator (that would turn the
	// revision into a path), so the brace is the whole of the defence.
	Commit string `json:"commit"`

	// Mode is which of the three trees move. Required: see ParseResetMode.
	Mode git.ResetMode `json:"mode"`
}

// resetRequest is the reset the user was shown, sent back to be carried out.
type resetRequest struct {
	// Commit is the commit to reset to. Sent back as the full name the plan
	// resolved, so a short SHA that became ambiguous since cannot silently
	// name a different object.
	Commit string `json:"commit"`

	// Into is the branch the dialog said this was resetting, and it is here
	// to be disagreed with. `git reset` acts on wherever HEAD happens to be,
	// and that is exactly the fact a confirmation cannot keep watching.
	Into string `json:"into"`

	// Mode is which mode the dialog displayed, and it is what builds the
	// command. Not a fact to be disagreed with the way Into is — nothing in
	// the repository has a second opinion about which of the three trees the
	// user meant to move — so what happens to it here is ParseResetMode, and
	// an unknown one is a 400 rather than a silent mixed.
	Mode git.ResetMode `json:"mode"`
}

// resetPlan is what resetting would do, before it does it.
type resetPlan struct {
	Command    string        `json:"command"`
	Commit     string        `json:"commit"`
	Subject    string        `json:"subject"`
	Into       string        `json:"into"`
	Mode       git.ResetMode `json:"mode"`
	Dropping   int           `json:"dropping"`
	DirtyFiles int           `json:"dirty_files"`
}

// handlePlanReset says what resetting would run, without running it.
func (s *Server) handlePlanReset(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body resetPlanRequest
	if !decodeBody(writer, s.logger, request, &body, `{"commit": "…", "mode": "soft|mixed|hard"}`) {
		return
	}

	mode, err := git.ParseResetMode(string(body.Mode))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	preview, err := s.runner.PreviewReset(request.Context(), opened.Path, body.Commit, mode)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, resetPlan{
		Command:    git.CommandLine(git.ResetArgs(preview.Commit, preview.Mode)),
		Commit:     preview.Commit,
		Subject:    preview.Subject,
		Into:       into,
		Mode:       preview.Mode,
		Dropping:   preview.Dropping,
		DirtyFiles: preview.DirtyFiles,
	})
}

// handleReset moves the branch HEAD is on to the named commit, in the named
// mode.
func (s *Server) handleReset(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body resetRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"commit": "…", "into": "…", "mode": "soft|mixed|hard"}`) {
		return
	}

	approved, err := git.ParseResetMode(string(body.Mode))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	into, err := s.branchUnderfoot(request, opened)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := resetOperation.agreesOnBranch(body.Into, into); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// ResetTarget rather than PreviewReset: this route needs the resolved name
	// and the not-on-the-branch refusal, and nothing else. The plan's two
	// counts are for a sentence on a confirmation, and reading them here would
	// walk the whole work tree a second time to throw the answer away.
	//
	// There is no agreesOnPlan beside the two checks below, and its absence is
	// deliberate. Merge and rebase echo back an outcome the daemon READ from
	// the repository, so the reading can come back different and the check has
	// something to refuse. A reset mode is the client's own choice — nothing in
	// the repository decides it — so the daemon has no second opinion to
	// disagree with, and a comparison of the approved mode against itself would
	// be a guard that can never fire pretending to be one that can.
	// ParseResetMode above is what refuses a mode this daemon does not know.
	sha, _, _, err := s.runner.ResetTarget(request.Context(), opened.Path, body.Commit, approved)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// The resolved name, not the one the client typed. A plan answered with a
	// full SHA and a run that accepted a different short form of a different
	// object would be two definitions of what was approved.
	if err := agreesOnCommit(body.Commit, sha); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.Reset(uninterrupted(request), opened.Path, sha, approved); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}
