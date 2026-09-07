package api

import (
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Blame over HTTP: who last touched each line of a path at a revision.
//
// Read-only, beside file history. The path and an optional starting revision
// are query parameters; empty revision means HEAD.

// handleBlame annotates a path as it stood at a revision.
func (s *Server) handleBlame(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	query := request.URL.Query()
	filePath, err := checkedPath(query.Get("path"))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	revision := query.Get("revision")
	if revision != "" {
		revision, err = checkedSHA(revision)
		if err != nil {
			writeError(writer, s.logger, http.StatusBadRequest, err)
			return
		}
	}

	result, err := s.runner.Blame(request.Context(), opened.Path, revision, filePath)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if result.Lines == nil {
		result.Lines = []git.BlameLine{}
	}
	writeJSON(writer, s.logger, http.StatusOK, result)
}
