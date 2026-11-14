package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Starting a bisect. The verdicts and the reset travel through the operation
// routes once one is in progress; this is the door into that state.

type bisectStartRequest struct {
	// Bad is the known-bad revision, empty to start without naming ends.
	Bad string `json:"bad"`

	// Good is the known-good revision(s). Empty unless Bad is set.
	Good []string `json:"good"`
}

// handlePlanStartBisect says what beginning a bisect would run.
func (s *Server) handlePlanStartBisect(writer http.ResponseWriter, request *http.Request) {
	_, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body bisectStartRequest
	if !decodeBody(writer, s.logger, request, &body, `{"bad": "…", "good": ["…"]}`) {
		return
	}

	args, err := git.BisectStartArgs(body.Bad, body.Good)
	if err != nil {
		writeError(writer, s.logger, statusForBisectError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(args),
	})
}

// handleStartBisect begins a bisect search.
func (s *Server) handleStartBisect(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}

	var body bisectStartRequest
	if !decodeBody(writer, s.logger, request, &body, `{"bad": "…", "good": ["…"]}`) {
		return
	}

	// Refuse when something else is already in progress — starting a bisect
	// over a merge would leave two operations fighting for the work tree.
	state, err := git.ReadState(opened.StateDir())
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}
	if state.Operation != git.OperationNone {
		writeError(writer, s.logger, http.StatusConflict, fmtOperationInProgress(state.Operation))
		return
	}

	if err := s.runner.StartBisect(uninterrupted(request), opened.Path, body.Bad, body.Good); err != nil {
		writeError(writer, s.logger, statusForBisectError(err), err)
		return
	}
	s.answerWithStatus(writer, request, opened)
}

func fmtOperationInProgress(operation git.Operation) error {
	return fmt.Errorf("this repository is already in the middle of a %s", operation)
}

func statusForBisectError(err error) int {
	switch {
	case errors.Is(err, git.ErrBadRevision):
		return http.StatusBadRequest
	default:
		return statusForOperationError(err)
	}
}
