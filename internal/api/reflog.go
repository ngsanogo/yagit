package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/ngsanogo/yagit/internal/git"
)

// The HEAD reflog over HTTP: list the newest entries, then restore one with a
// plan.
//
// Restore is a reset leased by object names. It is not PreviewReset: ancestry
// is not required — see git.PreviewReflogRestore. Detached HEAD is allowed, for
// the reason undo allows it: the tip may already be detached when somebody
// reaches for an older moment.

const defaultReflogLimit = 50
const maxReflogLimit = 200

type reflogPayload struct {
	Entries []git.ReflogEntry `json:"entries"`
}

type reflogRestorePlanRequest struct {
	Selector string        `json:"selector"`
	SHA      string        `json:"sha"`
	Mode     git.ResetMode `json:"mode"`
}

type reflogRestoreRequest struct {
	Selector string        `json:"selector"`
	SHA      string        `json:"sha"`
	Head     string        `json:"head"`
	Into     string        `json:"into"`
	Mode     git.ResetMode `json:"mode"`
}

func (s *Server) handleReflog(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	limit := defaultReflogLimit
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(writer, s.logger, http.StatusBadRequest,
				errors.New("limit must be a positive integer"))
			return
		}
		if parsed > maxReflogLimit {
			parsed = maxReflogLimit
		}
		limit = parsed
	}

	entries, err := s.runner.HeadReflog(request.Context(), opened.Path, limit)
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	if entries == nil {
		entries = []git.ReflogEntry{}
	}
	writeJSON(writer, s.logger, http.StatusOK, reflogPayload{Entries: entries})
}

func (s *Server) handlePlanReflogRestore(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}
	if err := s.repositoryIdle(opened); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	var body reflogRestorePlanRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"selector":"HEAD@{n}","sha":"…","mode":"soft|mixed|hard"}`) {
		return
	}
	mode, err := git.ParseResetMode(string(body.Mode))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	preview, err := s.runner.PreviewReflogRestore(
		request.Context(), opened.Path, body.Selector, body.SHA, mode, defaultReflogLimit)
	if err != nil {
		writeError(writer, s.logger, statusForReflogError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, preview)
}

func (s *Server) handleReflogRestore(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}
	if err := s.repositoryIdle(opened); err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}

	var body reflogRestoreRequest
	if !decodeBody(writer, s.logger, request, &body,
		`{"selector":"…","sha":"…","head":"…","into":"…","mode":"soft|mixed|hard"}`) {
		return
	}
	mode, err := git.ParseResetMode(string(body.Mode))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	fresh, err := s.runner.PreviewReflogRestore(
		request.Context(), opened.Path, body.Selector, body.SHA, mode, defaultReflogLimit)
	if err != nil {
		writeError(writer, s.logger, statusForReflogError(err), err)
		return
	}
	// Into too: a dialog left open while HEAD switched to another branch at
	// the same commit would otherwise reset a branch it never named.
	if fresh.SHA != body.SHA || fresh.Head != body.Head || fresh.Into != body.Into ||
		fresh.Mode != mode {
		writeError(writer, s.logger, http.StatusConflict, git.ErrUndoStale)
		return
	}

	if err := s.runner.ReflogRestore(uninterrupted(request), opened.Path, fresh); err != nil {
		writeError(writer, s.logger, statusForReflogError(err), err)
		return
	}

	s.answerWithRefs(writer, request, opened)
}

func statusForReflogError(err error) int {
	switch {
	case errors.Is(err, git.ErrReflogEntryGone),
		errors.Is(err, git.ErrUndoStale):
		return http.StatusConflict
	case errors.Is(err, git.ErrNoRef),
		errors.Is(err, git.ErrBadRevision):
		return http.StatusBadRequest
	default:
		return statusForOperationError(err)
	}
}
