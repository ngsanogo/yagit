package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/ngsanogo/yagit/internal/repo"
)

// maxRequestBody caps how much of a request body is read. The bodies we
// expect fit in a few hundred bytes; this limit keeps a malicious request
// from inflating the daemon's memory.
const maxRequestBody = 64 << 10 // 64 KiB

type reposPayload struct {
	Repos []repositoryView `json:"repos"`
}

type openRepoRequest struct {
	Path string `json:"path"`
}

func (s *Server) handleListRepos(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, s.logger, http.StatusOK, reposPayload{Repos: repositoryViews(s.registry.List(), s.unwatched)})
}

type discoverPayload struct {
	Repos       []repo.DiscoveredRepo `json:"repos"`
	ScannedFrom string                `json:"scanned_from"`
	Root        string                `json:"root"`

	// Depth and DepthLimit come from the scan rather than from this file: the
	// default and the ceiling belong to internal/repo, and the interface has
	// a depth control to draw.
	Depth      int `json:"depth"`
	DepthLimit int `json:"depth_limit"`

	// Skipped is what makes an empty list explicable. Every counter is sent,
	// zero included, so the client never has to tell "none" apart from "not
	// reported".
	Skipped repo.DiscoverSkipped `json:"skipped"`

	// SubmoduleFailures is never null, for the same reason Repos is not.
	SubmoduleFailures []discoverFailure `json:"submodule_failures"`
}

// discoverFailure is a repository the scan could not finish, in the shape the
// interface already knows how to draw: the {message, git} pair every error
// response carries, plus the repository it happened in.
//
// It travels on a 200. The scan succeeded — this is one repository listed
// without its submodules, not a request that failed.
type discoverFailure struct {
	Path    string      `json:"path"`
	Message string      `json:"message"`
	Git     *gitFailure `json:"git,omitempty"`
}

func discoverFailures(failures []repo.DiscoverFailure) []discoverFailure {
	views := make([]discoverFailure, 0, len(failures))
	for _, failure := range failures {
		views = append(views, discoverFailure{
			Path:    failure.Path,
			Message: failure.Err.Error(),
			Git:     gitFailureOf(failure.Err),
		})
	}
	return views
}

func (s *Server) handleDiscoverRepos(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()

	depth := 0
	if raw := query.Get("depth"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeError(writer, s.logger, http.StatusBadRequest,
				fmt.Errorf("depth %q is not a non-negative integer", raw))
			return
		}
		depth = parsed
	}

	opts := repo.DiscoverOptions{
		Dir:               query.Get("dir"),
		MaxDepth:          depth,
		IncludeWorktrees:  queryFlagTrue(query.Get("include_worktrees")),
		IncludeSubmodules: queryFlagTrue(query.Get("include_submodules")),
	}

	found, err := s.registry.Discover(request.Context(), opts)
	if err != nil {
		writeError(writer, s.logger, statusForDiscoverError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, discoverPayload{
		Repos:             found.Repos,
		ScannedFrom:       found.ScannedFrom,
		Root:              s.registry.Root(),
		Depth:             found.Depth,
		DepthLimit:        found.DepthLimit,
		Skipped:           found.Skipped,
		SubmoduleFailures: discoverFailures(found.SubmoduleFailures),
	})
}

func queryFlagTrue(raw string) bool {
	switch strings.ToLower(raw) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func statusForDiscoverError(err error) int {
	switch {
	case errors.Is(err, repo.ErrOutsideRoot):
		return http.StatusForbidden
	case errors.Is(err, repo.ErrPathNotAbsolute):
		return http.StatusBadRequest
	default:
		if errors.Is(err, os.ErrNotExist) {
			return http.StatusNotFound
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
			return http.StatusNotFound
		}
		return http.StatusBadRequest
	}
}

func (s *Server) handleOpenRepo(writer http.ResponseWriter, request *http.Request) {
	var body openRepoRequest

	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxRequestBody))
	// An unknown field is refused rather than ignored: a client that sends
	// "paht" should learn it right away, not find out later that its path was
	// never read.
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(writer, s.logger, http.StatusBadRequest,
			fmt.Errorf("unreadable request body, a {\"path\": \"…\"} object is expected: %w", err))
		return
	}

	if body.Path == "" {
		writeError(writer, s.logger, http.StatusBadRequest,
			errors.New("the path field is empty; give the absolute path of the repository to open"))
		return
	}

	opened, err := s.registry.Open(request.Context(), body.Path)
	if err != nil {
		writeError(writer, s.logger, statusForOpenError(err), err)
		return
	}

	s.startWatching(opened)

	writeJSON(writer, s.logger, http.StatusCreated, repositoryViewOf(opened, s.unwatched))
}

// startWatching follows a repository's git directory, so a commit made in the
// user's own terminal reaches the interface without anybody asking.
//
// A watch that cannot be established is reported and the repository opens
// anyway. Both halves matter. Refusing to open would make an inotify limit
// into a repository nobody can look at; staying quiet would leave an
// interface that has silently stopped refreshing, which is the worse of the
// two — see docs/adr/0008.
func (s *Server) startWatching(opened *repo.Repo) {
	if s.watcher == nil {
		return
	}
	// The git directories, not the work tree. For a linked worktree there are
	// two of them and neither is the work tree; watching the wrong one
	// produces no event ever.
	if err := s.watcher.Watch(opened.ID, opened.GitDirs()); err != nil {
		s.logger.Warn("this repository will not refresh on its own",
			"repository", opened.Name, "error", err)
		// And on the screen, not only in a journal nobody opens. A repository
		// that has stopped refreshing looks exactly like one where nothing is
		// happening, which is the silent staleness this project refuses
		// everywhere else.
		s.unwatched.record(opened.ID, err.Error())
		return
	}
	// A re-open that succeeded clears an older failure: the watch is retried
	// each time a repository is opened, and the warning must not outlive the
	// thing it was about.
	s.unwatched.forget(opened.ID)
}

// handleCloseRepo forgets a repository. The client keeps no path, so an
// identifier is all it can offer and all this needs.
//
// Idempotent, and 204 whether or not anything was open: closing a tab twice is
// a normal thing for a browser to do, and a 404 for the second attempt would
// be an error message about a state the caller already wanted.
func (s *Server) handleCloseRepo(writer http.ResponseWriter, request *http.Request) {
	identifier := request.PathValue("id")
	closeErr := s.registry.Close(identifier)

	// The assigned history goes with it. A tab shut after browsing a large
	// repository would otherwise leave every commit of it held for the life
	// of the daemon, which is the shape of leak nobody notices until the
	// daemon has been running for a week.
	//
	// Dropped even when the close failed, and before the failure is reported:
	// the registry forgets the entry whatever its descriptor did, so nothing
	// can address this history again. Reporting a problem and keeping the
	// memory would be two faults instead of one.
	s.history.Forget(identifier)

	// And so does any branch this repository remembered deleting. The record
	// is only meaningful beside the repository it names, and a reopened
	// repository gets a fresh identifier, so nothing could reach it again.
	s.deleted.forget(identifier)
	s.unwatched.forget(identifier)

	// And so does the watch, which holds a descriptor per directory. Dropped
	// before the failure is reported, for the same reason as the history
	// above: nothing can address this repository again either way.
	if s.watcher != nil {
		s.watcher.Forget(identifier)
	}

	if closeErr != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, closeErr)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// statusForOpenError turns a refusal from the registry into an HTTP code. The
// distinction matters to the interface: a path outside the root is a
// permanent refusal to explain, a missing path is a typo to fix.
func statusForOpenError(err error) int {
	switch {
	case errors.Is(err, repo.ErrOutsideRoot):
		return http.StatusForbidden
	case errors.Is(err, repo.ErrPathNotAbsolute):
		return http.StatusBadRequest
	default:
		// Path not found, a directory that is not a repository: the request
		// is malformed as far as the disk is concerned.
		return http.StatusBadRequest
	}
}
