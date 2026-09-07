package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Cloning a repository onto disk, then opening it.
//
// Progress rides this response as NDJSON — see docs/adr/0030. The session
// event stream is not involved: a clone in flight has no repository id yet.

type cloneRequest struct {
	// URL is where git reads from: an https URL, an ssh URL, or a local path.
	URL string `json:"url"`

	// Path is the absolute destination under YAGIT_ROOT. The parent must
	// exist; the destination itself must not.
	Path string `json:"path"`
}

// clonePlan is what a clone would run, before it runs.
type clonePlan struct {
	Command string `json:"command"`
	Path    string `json:"path"`
}

// NDJSON events on POST /api/repos/clone. One object per line.
type cloneProgressEvent struct {
	Type string `json:"type"` // "progress"
	Line string `json:"line"`
}

type cloneDoneEvent struct {
	Type       string         `json:"type"` // "done"
	Repository repositoryView `json:"repository"`
}

type cloneErrorEvent struct {
	Type  string      `json:"type"` // "error"
	Error errorDetail `json:"error"`
}

// handlePlanClone says what cloning would run, without running it.
//
// The command is the whole of the confirmation: destination checked against
// the root, URL redacted the way a remote list is. Nothing is created.
func (s *Server) handlePlanClone(writer http.ResponseWriter, request *http.Request) {
	body, destination, ok := s.readCloneRequest(writer, request, false)
	if !ok {
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, clonePlan{
		Command: git.ClonePlanCommand(body.URL, destination),
		Path:    destination,
	})
}

// handleClone copies a remote repository onto disk and opens it.
//
// The response is a stream of NDJSON events: progress lines while git runs,
// then either the opened repository or the failure. Validation errors that
// happen before git starts still use the ordinary JSON error shape — there is
// nothing to stream yet.
func (s *Server) handleClone(writer http.ResponseWriter, request *http.Request) {
	body, destination, ok := s.readCloneRequest(writer, request, true)
	if !ok {
		return
	}

	if err := s.registry.VerifyClaimedDirectory(destination); err != nil {
		if removeErr := s.registry.DiscardIncompleteClone(destination); removeErr != nil {
			s.logger.Warn("could not remove a claimed clone destination that failed verification",
				"path", destination, "error", removeErr)
		}
		writeError(writer, s.logger, statusForClonePathError(err), err)
		return
	}

	flusher, streamable := writer.(http.Flusher)
	if !streamable {
		writeError(writer, s.logger, http.StatusInternalServerError,
			fmt.Errorf("this connection cannot stream (%T does not flush)", writer))
		return
	}

	writer.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	encode := json.NewEncoder(writer)
	encode.SetEscapeHTML(false)

	emit := func(event any) bool {
		if err := encode.Encode(event); err != nil {
			s.logger.Warn("clone stream write interrupted", "error", err)
			return false
		}
		flusher.Flush()
		return true
	}

	// Once a write has failed the reader is gone, and every further line would
	// be another failed encode and another identical warning in the log — a
	// ten-minute clone writes thousands of them. The clone itself is
	// deliberately not cancelled: it runs on uninterrupted, and a half-written
	// destination is worse than one nobody is watching.
	streaming := true
	// Through a pump, so the writing happens on a goroutine of ours rather
	// than on the one os/exec copies git's stderr on. A ten-minute clone
	// watched in a tab that gets backgrounded would otherwise stop: the
	// browser stops reading, the write blocks, git's stderr pipe fills, and
	// git waits. See progressPump.
	pump := startProgressPump(func(line string) {
		if !streaming {
			return
		}
		streaming = emit(cloneProgressEvent{Type: "progress", Line: line})
	})
	defer pump.stop()

	err := s.runner.Clone(uninterrupted(request), body.URL, destination, pump.line)
	// Before the terminal event below: until this returns, the pump's own
	// goroutine is still writing to this response.
	pump.stop()
	if err != nil {
		// A clone that stopped partway leaves a directory behind, and until it
		// is gone the user is stuck: pressing Clone again answers "destination
		// already exists", and nothing in the interface can delete it. They
		// need a terminal and an `rm -rf` to recover from a dropped
		// connection.
		//
		// Safe to remove because of what ClaimNewRepositoryPath created
		// before this started — an empty directory we made under the root —
		// so this puts them back exactly where they were. Reported rather
		// than silent if it fails: then the directory IS still there and the
		// next attempt will say so.
		message := err.Error()
		if removeErr := s.registry.DiscardIncompleteClone(destination); removeErr != nil {
			s.logger.Warn("could not remove what a failed clone left behind",
				"path", destination, "error", removeErr)
			message += fmt.Sprintf(
				"\n\n%s was left behind and could not be removed (%s); delete it before cloning again",
				destination, removeErr)
		}

		// The return is ignored on the terminal events for the reason above:
		// there is nothing left to say to a reader that has already gone, and
		// emit has logged the write that failed.
		_ = emit(cloneErrorEvent{
			Type: "error",
			Error: errorDetail{
				Message: message,
				Git:     gitFailureOf(err),
			},
		})
		return
	}

	opened, err := s.registry.Open(uninterrupted(request), destination)
	if err != nil {
		_ = emit(cloneErrorEvent{
			Type:  "error",
			Error: errorDetail{Message: err.Error()},
		})
		return
	}

	s.startWatching(opened)

	_ = emit(cloneDoneEvent{
		Type:       "done",
		Repository: repositoryViewOf(opened, s.unwatched),
	})
}

// readCloneRequest decodes the body and checks the destination against the
// root. Shared by the plan and the run so the confirmation cannot approve a
// path the run would then refuse.
//
// claim is true on the run: the destination directory is created here so a
// symlink cannot be planted between the check and git. The plan only prepares.
func (s *Server) readCloneRequest(
	writer http.ResponseWriter,
	request *http.Request,
	claim bool,
) (cloneRequest, string, bool) {
	var body cloneRequest
	if !decodeBody(writer, s.logger, request, &body, `{"url": "…", "path": "…"}`) {
		return cloneRequest{}, "", false
	}

	// Trimmed before the emptiness test and kept trimmed, the way every other
	// route that takes a name does it: a URL of one space is not a URL, and
	// handing it to git records the refusal as a git failure rather than as
	// the empty field it is.
	body.URL = strings.TrimSpace(body.URL)
	body.Path = strings.TrimSpace(body.Path)

	if body.URL == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrEmptyCloneURL)
		return cloneRequest{}, "", false
	}
	if body.Path == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrEmptyClonePath)
		return cloneRequest{}, "", false
	}

	var destination string
	var err error
	if claim {
		destination, err = s.registry.ClaimNewRepositoryPath(body.Path)
	} else {
		destination, err = s.registry.PrepareNewRepositoryPath(body.Path)
	}
	if err != nil {
		writeError(writer, s.logger, statusForClonePathError(err), err)
		return cloneRequest{}, "", false
	}

	return body, destination, true
}

func statusForClonePathError(err error) int {
	switch {
	case errors.Is(err, repo.ErrOutsideRoot):
		return http.StatusForbidden
	case errors.Is(err, repo.ErrPathNotAbsolute):
		return http.StatusBadRequest
	case errors.Is(err, repo.ErrDestinationExists):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
