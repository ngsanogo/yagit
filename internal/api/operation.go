package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
)

// Finishing, or calling off, what the repository is in the middle of.
//
// Two routes with the shape the destructive operations in this package already
// use: one says what would run, the other runs it. The plan feeds a
// confirmation and the confirmation shows the exact line, because aborting a
// rebase throws away every conflict resolved since it started.
//
// Both answer with the working directory rather than with the references, and
// that is the opposite of what the branch and remote routes do. The reason is
// what these change: the banner is the whole subject here, the banner is drawn
// from the status, and it has to stop saying "Rebasing" in the same frame the
// button comes back. The references move too — an abort puts a branch back —
// and they are left to the event stream, which is watching `.git/HEAD` and
// `.git/refs` and reports exactly this.
//
// Where the operation is decided is the part worth naming. The client never
// says what to abort; it says which of the three things to do, and names the
// operation it was LOOKING at so the daemon can refuse if that is no longer
// true. See operationRequest.Operation.

// errOperationMoved: the repository is in the middle of something other than
// what the request named.
//
// Its own error rather than git.ErrNoOperation because the two need different
// sentences: one says the thing finished, the other says a different thing
// started. Both are answered by reading the state again.
var errOperationMoved = errors.New("the operation in progress is not the one this request names")

// operationRequest is one instruction for the operation in progress.
type operationRequest struct {
	// Action is abort, continue or skip. Checked against those three inside
	// git.ActionArgs before anything is built from it — every command there
	// is spelled `--` and this string.
	Action git.Action `json:"action"`

	// Operation is what the interface was showing when the button was
	// pressed, and it is here to be disagreed with.
	//
	// The daemon reads the real state itself and refuses when the two differ.
	// Without that the sequence is: the banner says "Rebasing", the rebase
	// finishes in a terminal, a merge starts, the user clicks the Abort they
	// have been looking at — and `git merge --abort` destroys a merge they
	// never saw, after a dialog that promised `git rebase --abort`. The
	// status is polled every two seconds, so that window is real.
	//
	// The same guard as the diff fingerprint a line selection carries and as
	// the lease on a force push, for the same reason: a request that was
	// correct when it was made must not be applied to a repository that has
	// moved under it.
	Operation git.Operation `json:"operation"`

	// Identity is which instance of that operation the dialog described —
	// MERGE_HEAD, the rebase's onto, and so on. Kind alone is not enough:
	// a rebase can finish and another can start while the Abort dialog sits
	// open, and both answer "rebase".
	Identity string `json:"identity"`
}

// operationPlan is the command an instruction would run, before it runs.
type operationPlan struct {
	Command string `json:"command"`

	// Operation is what the daemon found in progress, which is the fact the
	// command was built from. Sent back so the confirmation can name it in a
	// sentence — "Abort the rebase" — from the same reading that produced the
	// line, rather than from what the banner happened to hold.
	Operation git.Operation `json:"operation"`

	// Identity travels with Operation so the run can refuse a different
	// instance of the same kind. See operationRequest.Identity.
	Identity string `json:"identity"`

	Action git.Action `json:"action"`

	// Destroys is whether this throws work away. git.Destroys is the
	// authority; the dialog only chooses its wording from it.
	Destroys bool `json:"destroys"`
}

// handleOperationPlan says what an instruction would run, without running it.
func (s *Server) handleOperationPlan(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body operationRequest
	if !decodeBody(writer, s.logger, request, &body, `{"action": "abort"}`) {
		return
	}

	// The plan does not check the operation the client named. It is asked the
	// instant before a dialog opens, its whole answer is a description of the
	// state as it is now, and refusing to describe it would leave the dialog
	// with nothing to show. The run that follows is where the disagreement
	// matters, and that is where it is caught.
	state, err := git.ReadState(opened.StateDir())
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	args, err := git.ActionArgs(state.Operation, body.Action)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, operationPlan{
		Command:   git.CommandLine(args),
		Operation: state.Operation,
		Identity:  state.Identity,
		Action:    body.Action,
		Destroys:  git.Destroys(body.Action),
	})
}

// handleOperation carries out one instruction on the operation in progress.
func (s *Server) handleOperation(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body operationRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"action": "abort", "operation": "rebase", "identity": "…"}`) {
		return
	}

	state, err := git.ReadState(opened.StateDir())
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	if err := agreesOnOperation(body.Operation, state.Operation, body.Identity, state.Identity); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	// What this state will not be told to do, in the sentence the button
	// already showed. The interface draws the button disabled and this refuses
	// it anyway: the two are the same string from the same reading, and a
	// route that trusted the screen would be trusting a status up to two
	// seconds old.
	if err := state.Refuse(body.Action); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	if err := s.runner.ActOnOperation(uninterrupted(request), opened.Path, state.Operation, body.Action); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	s.answerWithStatus(writer, request, opened)
}

// agreesOnOperation refuses an instruction aimed at a state the repository has
// left.
//
// The empty string is not a wildcard and is refused like any other mismatch: a
// client that sends no operation is one that did not look, and this route
// destroys work. Identity is the same: kind alone would let an Abort aimed at
// one rebase destroy the next.
func agreesOnOperation(claimed, actual git.Operation, claimedID, actualID string) error {
	if claimed != actual {
		if actual == git.OperationNone {
			return fmt.Errorf("%w: it finished, or was ended somewhere else", git.ErrNoOperation)
		}
		return fmt.Errorf(
			"%w: this repository is now in the middle of a %s, not a %s — read the state again before acting on it",
			errOperationMoved, actual, describeClaim(claimed))
	}
	claimedID = strings.TrimSpace(claimedID)
	if claimedID == "" {
		return fmt.Errorf("%w: the request names no identity for the %s",
			errOperationMoved, describeClaim(claimed))
	}
	if claimedID != actualID {
		return fmt.Errorf(
			"%w: that %s finished and another started — read the state again before acting on it",
			errOperationMoved, describeClaim(claimed))
	}
	return nil
}

// describeClaim names what the client thought it was acting on, including when
// it named nothing.
func describeClaim(claimed git.Operation) string {
	if claimed == git.OperationNone {
		return "repository with nothing in progress"
	}
	return string(claimed)
}
