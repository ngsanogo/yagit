package api

import (
	"errors"
	"net/http"

	"github.com/ngsanogo/yagit/internal/git"
)

// Git preferences that are not about one repository: the committer identity
// and a read-only view of pull.rebase.

type identityRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (s *Server) handleGetIdentity(writer http.ResponseWriter, request *http.Request) {
	identity, err := s.runner.ReadIdentity(request.Context())
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, identity)
}

func (s *Server) handleSetIdentity(writer http.ResponseWriter, request *http.Request) {
	var body identityRequest
	if !decodeBody(writer, s.logger, request, &body, `{"name":"…","email":"…"}`) {
		return
	}
	if err := s.runner.SetIdentity(request.Context(), body.Name, body.Email); err != nil {
		if errors.Is(err, git.ErrBadIdentity) {
			writeError(writer, s.logger, http.StatusBadRequest, err)
			return
		}
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	identity, err := s.runner.ReadIdentity(request.Context())
	if err != nil {
		writeError(writer, s.logger, statusForOperationError(err), err)
		return
	}
	writeJSON(writer, s.logger, http.StatusOK, identity)
}
