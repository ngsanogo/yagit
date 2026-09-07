package api

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/ngsanogo/yagit/internal/edit"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// Git LFS over HTTP: what this machine and this repository can do about it,
// and the two commands that change the second.
//
// The state is composed here rather than read in one place, because it is two
// facts about two different things. Whether git-lfs is installed is a property
// of the MACHINE — one `git lfs version`, the same answer for every repository
// on it. Which patterns go through the filter is a property of a FILE in the
// work tree, which internal/edit owns and internal/git parses. Neither package
// can answer the whole question, and this is the layer whose job that is.
//
// Nothing here fetches, pulls or pushes. LFS installs itself into git as a
// filter and git runs it, so every transfer yagit already drives carries LFS
// content without a line of code — see internal/git/lfs.go, which says the
// same thing from the other side.

// attributesFile is where `git lfs track` writes.
//
// The top-level one, and only it. git reads .gitattributes in every directory
// and in .git/info/attributes with it, so this is not every pattern in force —
// it is every pattern this panel can add to and take away again. Listing rules
// from files it cannot edit would be a list whose rows do not all have the
// same buttons.
const attributesFile = ".gitattributes"

type lfsPatternRequest struct {
	// Pattern is a path pattern as .gitattributes spells it — `*.psd`. It is
	// never a path this daemon resolves: git-lfs writes it into the file
	// verbatim and git matches it there.
	Pattern string `json:"pattern"`
}

// handleLFS answers what LFS can do here and what it is doing.
func (s *Server) handleLFS(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return
	}
	s.answerWithLFS(writer, request, opened)
}

// handlePlanTrackLFS says what tracking a pattern would run.
func (s *Server) handlePlanTrackLFS(writer http.ResponseWriter, request *http.Request) {
	_, pattern, ok := s.readLFSPattern(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.TrackLFSArgs(pattern)),
	})
}

// handleTrackLFS routes a pattern through LFS.
func (s *Server) handleTrackLFS(writer http.ResponseWriter, request *http.Request) {
	opened, pattern, ok := s.readLFSPattern(writer, request)
	if !ok {
		return
	}
	if err := s.runner.TrackLFS(uninterrupted(request), opened.Path, pattern); err != nil {
		writeError(writer, s.logger, statusForLFSError(err), err)
		return
	}
	s.answerWithLFS(writer, request, opened)
}

// handlePlanUntrackLFS says what untracking a pattern would run.
func (s *Server) handlePlanUntrackLFS(writer http.ResponseWriter, request *http.Request) {
	_, pattern, ok := s.readLFSPattern(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, plannedCommand{
		Command: git.CommandLine(git.UntrackLFSArgs(pattern)),
	})
}

// handleUntrackLFS takes a pattern back out of .gitattributes.
func (s *Server) handleUntrackLFS(writer http.ResponseWriter, request *http.Request) {
	opened, pattern, ok := s.readLFSPattern(writer, request)
	if !ok {
		return
	}
	if err := s.runner.UntrackLFS(uninterrupted(request), opened.Path, pattern); err != nil {
		writeError(writer, s.logger, statusForLFSError(err), err)
		return
	}
	s.answerWithLFS(writer, request, opened)
}

// readLFSPattern is the repository and the pattern every write here takes.
//
// A work tree rather than any repository: `git lfs track` writes a file into
// one, and a bare repository has nowhere to put it.
func (s *Server) readLFSPattern(
	writer http.ResponseWriter, request *http.Request,
) (*repo.Repo, string, bool) {
	opened, err := s.workTree(request)
	if err != nil {
		writeError(writer, s.logger, statusForWorkTreeError(err), err)
		return nil, "", false
	}

	var body lfsPatternRequest
	if !decodeBody(writer, s.logger, request, &body, `{"pattern": "*.psd"}`) {
		return nil, "", false
	}
	pattern := strings.TrimSpace(body.Pattern)
	if pattern == "" {
		writeError(writer, s.logger, http.StatusBadRequest, git.ErrEmptyLFSPattern)
		return nil, "", false
	}
	return opened, pattern, true
}

// answerWithLFS composes the state out of the machine's answer and the file's.
//
// Both writes answer with it, for the reason the submodule routes answer with
// their collection: the panel that ran the command is the panel that shows the
// result, and a second request to learn what just happened is a window in
// which the two disagree.
func (s *Server) answerWithLFS(
	writer http.ResponseWriter, request *http.Request, opened *repo.Repo,
) {
	version, installed := s.runner.LFSVersion(request.Context())

	patterns, err := lfsPatterns(opened)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, git.LFSSupport{
		Installed: installed,
		Version:   version,
		Patterns:  patterns,
	})
}

// lfsPatterns reads the top-level .gitattributes, if there is one.
//
// A repository without the file is the ordinary case and not a failure — most
// repositories have never heard of LFS — so a missing file is an empty list.
// Every other read failure travels: a .gitattributes that cannot be read is
// something the user has to know about, and answering "nothing is tracked"
// would be a sentence about a file this daemon never managed to open.
//
// A .gitattributes too large for the editor, or holding bytes that are not
// text, is refused the same way for the same reason.
func lfsPatterns(opened *repo.Repo) ([]string, error) {
	file, err := edit.Read(edit.Location{WorkTree: opened.Path, GitDirs: opened.GitDirs()},
		attributesFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	return git.ParseLFSPatterns([]byte(file.Text)), nil
}

// statusForLFSError tells a program that is not installed from a git failure.
//
// git-lfs missing is not this daemon breaking and not a malformed request: it
// is a machine that cannot do what was asked. 409 is the same reading a bare
// repository gets from statusForWorkTreeError — the request is well formed and
// the thing it asks about does not apply here — and the interface turns it
// into the sentence naming what to install rather than a git error nobody
// asked for.
func statusForLFSError(err error) int {
	switch {
	case errors.Is(err, git.ErrLFSUnavailable):
		return http.StatusConflict
	case errors.Is(err, git.ErrEmptyLFSPattern):
		return http.StatusBadRequest
	default:
		return statusForOperationError(err)
	}
}
