package api

import (
	"errors"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Comparing HEAD against another reference, over HTTP.
//
// One route: the answer is a reading, not an operation. Merge and rebase each
// have their own plan routes; this one only says how far the two tips have
// run.

type compareRequest struct {
	// Ref is a full reference name. CheckRefName refuses a short name and
	// anything git would read as revision syntax — see git.Compare.
	Ref string `json:"ref"`
}

// handleCompare answers how far HEAD and the named reference have diverged.
func (s *Server) handleCompare(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body compareRequest
	if !decodeBody(writer, s.logger, request, &body, `{"ref": "refs/heads/…"}`) {
		return
	}

	result, err := s.runner.Compare(request.Context(), opened.Path, body.Ref)
	if err != nil {
		if errors.Is(err, git.ErrBadRefName) {
			writeError(writer, s.logger, http.StatusBadRequest, err)
			return
		}
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, result)
}
