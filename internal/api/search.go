package api

import (
	"errors"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Searching the history over HTTP.
//
// A GET, because a search is a reading and asking the same question twice has
// to be the same request — the browser's cache, the back button and TanStack
// Query's key all depend on that.
//
// Not routed through the history store. The store holds an ASSIGNED walk per
// scope, and an assignment is a picture of how commits connect; a search wants
// the subset that matches and nothing about how it is drawn. Following a
// result is what puts the graph on that commit, and the store already answers
// that question through Locate.

func (s *Server) handleSearch(writer http.ResponseWriter, request *http.Request) {
	opened, err := s.lookupRepo(request)
	if err != nil {
		writeError(writer, s.logger, statusForLookupError(err), err)
		return
	}

	scope, selected, err := requestedWalk(request)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	field, err := git.ParseSearchField(request.URL.Query().Get("in"))
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	// The refs and HEAD say whether there is anything to walk, which is a
	// question `git log` answers with a failure rather than an empty list.
	// Read here and passed down for the reason LogScope takes them: one
	// reading, so the search and the graph cannot disagree about it.
	refs, err := s.runner.ForEachRef(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}
	head, err := s.runner.ReadHEAD(request.Context(), opened.Path)
	if err != nil {
		writeError(writer, s.logger, http.StatusInternalServerError, err)
		return
	}

	found, err := s.runner.Search(
		request.Context(), opened.Path, scope, field, request.URL.Query().Get("q"),
		refs, head, selected)
	if err != nil {
		writeError(writer, s.logger, statusForSearchError(err), err)
		return
	}

	writeJSON(writer, s.logger, http.StatusOK, found)
}

func statusForSearchError(err error) int {
	// A ref name that must not reach git is the same client mistake here as on
	// the history routes, and answers the same way — see statusForWalkError.
	if errors.Is(err, git.ErrEmptyQuery) || errors.Is(err, git.ErrBadRefName) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}
