package api

import (
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// File history over HTTP: the commits that touched one path.
//
// Read-only, like show and diff. The path and an optional starting revision
// are query parameters; the answer is the same Commit shape the history list
// uses, so a click on a row opens the commit panel that already exists.

type fileHistoryPayload struct {
	Path     string       `json:"path"`
	Revision string       `json:"revision"`
	Limit    int          `json:"limit"`
	Commits  []git.Commit `json:"commits"`
}

// handleFileHistory lists the commits that touched a path, following renames.
func (s *Server) handleFileHistory(writer http.ResponseWriter, request *http.Request) {
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

	commits, err := s.runner.LogPath(request.Context(), opened.Path, revision, filePath)
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
	writeJSON(writer, s.logger, http.StatusOK, fileHistoryPayload{
		Path:     filePath,
		Revision: answered,
		Limit:    git.FileHistoryLimit,
		Commits:  commits,
	})
}
