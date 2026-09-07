package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/ngsanogo/yagit/internal/git"
)

// Line history over HTTP: the commits that changed one line of one path.
//
// Read-only, beside file history and blame. The line is a 1-based number
// matching what blame shows; a click on a blame gutter asks for that line.

type lineHistoryPayload struct {
	Path     string       `json:"path"`
	Revision string       `json:"revision"`
	Line     int          `json:"line"`
	Limit    int          `json:"limit"`
	Commits  []git.Commit `json:"commits"`
}

// handleLineHistory lists the commits that changed one line of a path.
func (s *Server) handleLineHistory(writer http.ResponseWriter, request *http.Request) {
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

	line, err := strconv.Atoi(query.Get("line"))
	if err != nil || line < 1 {
		writeError(writer, s.logger, http.StatusBadRequest,
			fmt.Errorf("line must be a positive integer, got %q", query.Get("line")))
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

	commits, err := s.runner.LogLine(request.Context(), opened.Path, revision, filePath, line, line)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if commits == nil {
		commits = []git.Commit{}
	}

	answered := revision
	if answered == "" {
		answered = "HEAD"
	}
	writeJSON(writer, s.logger, http.StatusOK, lineHistoryPayload{
		Path:     filePath,
		Revision: answered,
		Line:     line,
		Limit:    git.FileHistoryLimit,
		Commits:  commits,
	})
}
