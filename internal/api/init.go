package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/git"
)

// Making a repository, then opening it.
//
// The same two-step shape as the clone beside it — plan, then run — and the
// same destination check, because "a path under YAGIT_ROOT that is not there
// yet" is one question however the directory comes to be filled.
//
// No NDJSON here. `git init` writes a handful of files and returns; a progress
// stream would be a protocol for one line that always arrives at once
// (docs/adr/0030 is about the commands that do not).

type initRequest struct {
	// Path is the absolute destination under YAGIT_ROOT. The parent must
	// exist; the destination itself must not.
	Path string `json:"path"`

	// Branch is what the first branch is called. Empty means the machine's
	// own answer, which is what the plan proposes.
	Branch string `json:"branch"`
}

// initPlan is what making a repository would run, before it runs.
type initPlan struct {
	Command string `json:"command"`
	Path    string `json:"path"`

	// Branch is echoed because the client may have sent none: the field on
	// screen is filled from this, so what the user then approves is the name
	// the command shows.
	Branch string `json:"branch"`
}

// handlePlanInit says what making a repository would run, without making one.
func (s *Server) handlePlanInit(writer http.ResponseWriter, request *http.Request) {
	destination, branch, ok := s.readInitRequest(writer, request, false)
	if !ok {
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, initPlan{
		Command: git.CommandLine(git.InitArgs(destination, branch)),
		Path:    destination,
		Branch:  branch,
	})
}

// handleInit makes an empty repository and opens it.
func (s *Server) handleInit(writer http.ResponseWriter, request *http.Request) {
	destination, branch, ok := s.readInitRequest(writer, request, true)
	if !ok {
		return
	}

	if err := s.registry.VerifyClaimedDirectory(destination); err != nil {
		if removeErr := s.registry.DiscardIncompleteClone(destination); removeErr != nil {
			s.logger.Warn("could not remove a claimed init destination that failed verification",
				"path", destination, "error", removeErr)
		}
		writeError(writer, s.logger, statusForClonePathError(err), err)
		return
	}

	if err := s.runner.Init(uninterrupted(request), destination, branch); err != nil {
		if removeErr := s.registry.DiscardIncompleteClone(destination); removeErr != nil {
			s.logger.Warn("could not remove what a failed init left behind",
				"path", destination, "error", removeErr)
		}
		writeError(writer, s.logger, statusForInitError(err), err)
		return
	}

	opened, err := s.registry.Open(uninterrupted(request), destination)
	if err != nil {
		// The repository is on disk and the failure is in opening it, so the
		// message says so rather than reading as "init failed" — the directory
		// is there, and a second attempt would now be refused for existing.
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	s.startWatching(opened)

	writeJSON(writer, s.logger, http.StatusCreated, repositoryViewOf(opened, s.unwatched))
}

// readInitRequest decodes the body, checks the destination against the root,
// and settles the branch name. Shared by the plan and the run so the
// confirmation cannot approve something the run would refuse.
//
// claim is true on the run — see readCloneRequest.
func (s *Server) readInitRequest(
	writer http.ResponseWriter,
	request *http.Request,
	claim bool,
) (destination, branch string, ok bool) {
	var body initRequest
	if !decodeBody(writer, s.logger, request, &body, `{"path": "…", "branch": "…"}`) {
		return "", "", false
	}

	body.Path = strings.TrimSpace(body.Path)
	body.Branch = strings.TrimSpace(body.Branch)

	if body.Path == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrEmptyInitPath)
		return "", "", false
	}

	// The machine's own answer when the client sent none. Read here rather
	// than defaulted in the client, because `init.defaultBranch` is on the
	// machine the daemon runs on and the browser cannot see it.
	branch = body.Branch
	if branch == "" {
		branch = s.runner.DefaultBranchName(request.Context())
	}
	if err := git.CheckInitialBranch(branch); err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return "", "", false
	}

	var err error
	if claim {
		destination, err = s.registry.ClaimNewRepositoryPath(body.Path)
	} else {
		destination, err = s.registry.PrepareNewRepositoryPath(body.Path)
	}
	if err != nil {
		writeError(writer, s.logger, statusForClonePathError(err), err)
		return "", "", false
	}

	return destination, branch, true
}

func statusForInitError(err error) int {
	switch {
	case errors.Is(err, git.ErrEmptyInitPath), errors.Is(err, git.ErrBadInitialBranch):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
