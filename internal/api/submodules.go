package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Submodules over HTTP: read as a collection, written as four operations.
//
// The destination of an add is NOT checked against YAGIT_ROOT, unlike a
// clone, an init or a worktree. Those three name an absolute path anywhere on
// the machine; this one is a path INSIDE the repository, which is already
// inside the boundary. Absolute paths, `..`, and a leading dash are refused
// here the same way file and staging routes refuse them — git would refuse
// some of those too, and the boundary is worth holding before the subcommand.

type submoduleRequest struct {
	// Path is repository-relative: where the submodule sits in the work tree.
	Path string `json:"path"`
}

type addSubmoduleRequest struct {
	// URL is where git clones from. yagit authenticates nothing; the
	// credentials are git's, exactly as for a clone (ADR 0020).
	URL  string `json:"url"`
	Path string `json:"path"`
}

type removeSubmoduleRequest struct {
	Path  string `json:"path"`
	Force bool   `json:"force"`
}

// handleSubmodules lists what this repository pins.
func (s *Server) handleSubmodules(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}
	s.answerWithSubmodules(writer, request, opened)
}

// handlePlanAddSubmodule says what adding one would run.
func (s *Server) handlePlanAddSubmodule(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body addSubmoduleRequest
	if !decodeBody(writer, s.logger, request, &body, `{"url": "…", "path": "…"}`) {
		return
	}
	if err := checkSubmoduleAdd(body); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	// The URL is redacted on the line the user reads, the way a remote list is:
	// a token in a URL is a password, and this one goes on a screen.
	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.AddSubmoduleArgs(
			git.RedactURL(strings.TrimSpace(body.URL)), strings.TrimSpace(body.Path))),
	})
}

// handleAddSubmodule pins another repository inside this one.
func (s *Server) handleAddSubmodule(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body addSubmoduleRequest
	if !decodeBody(writer, s.logger, request, &body, `{"url": "…", "path": "…"}`) {
		return
	}
	if err := checkSubmoduleAdd(body); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	if err := s.runner.AddSubmodule(
		uninterrupted(request), opened.Path, body.URL, body.Path,
	); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	s.answerWithSubmodules(writer, request, opened)
}

// handleUpdateSubmodules checks out what the superproject records.
//
// An empty path means every submodule, which is what somebody opening a fresh
// clone wants; a path narrows it to one.
func (s *Server) handleUpdateSubmodules(writer http.ResponseWriter, request *http.Request) {
	opened, body, ok := s.readSubmoduleRequest(writer, request)
	if !ok {
		return
	}

	if err := s.runner.UpdateSubmodules(uninterrupted(request), opened.Path, body.Path); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	s.answerWithSubmodules(writer, request, opened)
}

// handleSyncSubmodules copies the URLs from .gitmodules into the local config.
func (s *Server) handleSyncSubmodules(writer http.ResponseWriter, request *http.Request) {
	opened, body, ok := s.readSubmoduleRequest(writer, request)
	if !ok {
		return
	}

	if err := s.runner.SyncSubmodules(uninterrupted(request), opened.Path, body.Path); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	s.answerWithSubmodules(writer, request, opened)
}

// handlePlanRemoveSubmodule answers with BOTH commands a removal runs.
//
// Two lines rather than one, because git has no `submodule remove` and the
// confirmation must not pretend otherwise: the user is approving a deinit and
// a `git rm`, and the dialog shows the pair the way a discard already does.
func (s *Server) handlePlanRemoveSubmodule(writer http.ResponseWriter, request *http.Request) {
	if _, err := s.lookupRepo(request); err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body removeSubmoduleRequest
	if !decodeBody(writer, s.logger, request, &body, `{"path": "…", "force": false}`) {
		return
	}
	path := strings.TrimSpace(body.Path)
	if err := checkSubmodulePath(path); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	argsets := git.RemoveSubmoduleArgs(path, body.Force)
	commands := make([]string, 0, len(argsets))
	for _, args := range argsets {
		commands = append(commands, git.CommandLine(args))
	}
	writeJSON(writer, s.logger, http.StatusOK, map[string]any{"commands": commands})
}

// handleRemoveSubmodule unpins a repository from this one.
func (s *Server) handleRemoveSubmodule(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	var body removeSubmoduleRequest
	if !decodeBody(writer, s.logger, request, &body, `{"path": "…", "force": false}`) {
		return
	}
	if err := checkSubmodulePath(strings.TrimSpace(body.Path)); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	if err := s.runner.RemoveSubmodule(
		uninterrupted(request), opened.Path, body.Path, body.Force,
	); err != nil {
		writeError(writer, s.logger, statusForSubmoduleError(err), err)
		return
	}

	s.answerWithSubmodules(writer, request, opened)
}

func (s *Server) readSubmoduleRequest(
	writer http.ResponseWriter, request *http.Request,
) (*repo.Repo, submoduleRequest, bool) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return nil, submoduleRequest{}, false
	}

	var body submoduleRequest
	if !decodeBody(writer, s.logger, request, &body, `{"path": ""}`) {
		return nil, submoduleRequest{}, false
	}
	body.Path = strings.TrimSpace(body.Path)

	// Empty is allowed here and means every submodule; a non-empty one is
	// checked the way every other path reaching git is.
	if body.Path != "" {
		if err := checkSubmodulePath(body.Path); err != nil {
			writeError(writer, s.logger, statusForSubmoduleError(err), err)
			return nil, submoduleRequest{}, false
		}
	}
	return opened, body, true
}

func (s *Server) answerWithSubmodules(
	writer http.ResponseWriter, request *http.Request, opened *repo.Repo,
) {
	submodules, err := s.runner.Submodules(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, map[string]any{"submodules": submodules})
}

func checkSubmoduleAdd(body addSubmoduleRequest) error {
	if strings.TrimSpace(body.URL) == "" {
		return git.ErrEmptySubmoduleURL
	}
	return checkSubmodulePath(strings.TrimSpace(body.Path))
}

// checkSubmodulePath refuses what `--` cannot save.
//
// The path reaches git after `--`, so it arrives as a path whatever it holds —
// but `git submodule add` takes it as the LAST positional and `git submodule
// deinit` reads it as a pathspec, and neither is a place to discover that the
// field was empty. A leading dash is refused for belt and braces: it is the
// one shape that turns into an option if a future git argument order changes.
//
// Absolute paths and `..` are refused the same way checkedPath refuses them on
// file and staging routes: a submodule checkout outside the work tree is the
// same boundary those routes already hold.
func checkSubmodulePath(path string) error {
	switch {
	case path == "":
		return git.ErrNoSubmodulePath
	case strings.HasPrefix(path, "-"):
		return errSubmodulePathDash
	}

	cleaned, err := checkedPath(path)
	if err != nil {
		return fmt.Errorf("%w: %w", errSubmodulePathEscape, err)
	}
	_ = cleaned
	return nil
}

var (
	errSubmodulePathDash   = errors.New("a submodule path may not start with a dash")
	errSubmodulePathEscape = errors.New("a submodule path may not leave the repository")
)

func statusForSubmoduleError(err error) int {
	switch {
	case errors.Is(err, git.ErrNoSubmodulePath),
		errors.Is(err, git.ErrEmptySubmoduleURL),
		errors.Is(err, errSubmodulePathDash),
		errors.Is(err, errSubmodulePathEscape):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
