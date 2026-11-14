package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Handing one conflicted path to the user's configured merge tool.
//
// Two routes with the shape every other write that leaves the browser uses:
// the plan feeds a confirmation with the exact line, and the run carries it
// out. The in-app resolver stays the ordinary path; this is the escape hatch
// for a file where somebody already has a tool they trust.

type mergetoolRequest struct {
	Path string `json:"path"`
}

// mergetoolPlan is the line, and the tool it will open, for a confirmation
// that names the program about to take over the file.
type mergetoolPlan struct {
	Command string `json:"command"`
	Tool    string `json:"tool"`
}

// handlePlanMergetool says what launching the mergetool would run.
func (s *Server) handlePlanMergetool(writer http.ResponseWriter, request *http.Request) {
	opened, path, ok := s.readMergetoolPath(writer, request)
	if !ok {
		return
	}
	if err := s.requireConflicted(request, opened.Path, path); err != nil {
		writeError(writer, s.logger, statusForMergetoolError(err), err)
		return
	}
	tool, err := s.runner.ConfiguredMergeTool(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, statusForMergetoolError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, mergetoolPlan{
		Command: git.CommandLine(git.MergeToolArgs(path)),
		Tool:    tool,
	})
}

// handleMergetool runs the configured mergetool over one conflicted path.
func (s *Server) handleMergetool(writer http.ResponseWriter, request *http.Request) {
	opened, path, ok := s.readMergetoolPath(writer, request)
	if !ok {
		return
	}
	if err := s.requireConflicted(request, opened.Path, path); err != nil {
		writeError(writer, s.logger, statusForMergetoolError(err), err)
		return
	}
	if err := s.runner.MergeTool(uninterrupted(request), opened.Path, path); err != nil {
		writeError(writer, s.logger, statusForMergetoolError(err), err)
		return
	}
	s.answerWithStatus(writer, request, opened)
}

func (s *Server) readMergetoolPath(
	writer http.ResponseWriter, request *http.Request,
) (opened *repo.Repo, path string, ok bool) {
	var err error
	opened, err = s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return nil, "", false
	}

	var body mergetoolRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, fmt.Errorf(
			"unreadable request body, a {\"path\": \"…\"} object is expected: %w", err))
		return nil, "", false
	}

	path, err = checkedPath(body.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return nil, "", false
	}
	return opened, path, true
}

// requireConflicted refuses a path git does not report as unmerged.
//
// Same guard resolve uses: running the mergetool on an ordinary file launches
// nothing useful and leaves the log panel with a git refusal about a conflict
// that is not there.
func (s *Server) requireConflicted(request *http.Request, dir, path string) error {
	before, err := s.runner.Status(request.Context(), dir)
	if err != nil {
		return err
	}
	record := indexStatus(before).recordOf(path)
	if record.Kind != git.EntryUnmerged {
		return fmt.Errorf("%w: git does not report %s as unmerged", errNotConflicted, path)
	}
	return nil
}

func statusForMergetoolError(err error) int {
	switch {
	case errors.Is(err, errNotConflicted), errors.Is(err, git.ErrNoMergeTool):
		return http.StatusConflict
	default:
		return statusForOperationError(err)
	}
}
