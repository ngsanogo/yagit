package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	errTooManyRequests = errors.New("too many requests; wait a moment and try again")

	errInvalidSessionBody = errors.New(
		"unreadable request body; send {\"token\": \"…\"} or form field token=…")
)

type sessionRequest struct {
	Token string `json:"token"`
}

// handleCreateSession exchanges a session token for an HttpOnly cookie.
//
// This route is intentionally outside requireToken: it is how a browser gets
// its first credential without putting the secret in the URL, where it would
// linger in history, proxy logs and Referer headers.
func (s *Server) handleCreateSession(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeError(writer, s.logger, http.StatusMethodNotAllowed,
			fmt.Errorf("method %s not allowed; use POST", request.Method))
		return
	}

	// A browser always sends Origin on a cross-site POST. When one is present
	// it must be allowed — otherwise a page that phished the token could plant
	// the HttpOnly cookie via a form aimed at this host. curl and other tools
	// send no Origin and keep working.
	if origin := request.Header.Get("Origin"); origin != "" && !s.originAllowed(request) {
		writeError(writer, s.logger, http.StatusForbidden, errOriginRejected)
		return
	}

	token, err := readSessionToken(request)
	if err != nil {
		writeError(writer, s.logger, http.StatusBadRequest, err)
		return
	}

	if !s.tokenMatches(token) {
		writeError(writer, s.logger, http.StatusUnauthorized, errTokenRequired)
		return
	}

	http.SetCookie(writer, s.sessionCookie())

	// A browser form lands here with no JavaScript. Send it to the app; an API
	// client gets an empty success and keeps control.
	if wantsHTML(request) || strings.HasPrefix(request.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

func readSessionToken(request *http.Request) (string, error) {
	contentType := request.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "application/x-www-form-urlencoded") {
		if err := request.ParseForm(); err != nil {
			return "", fmt.Errorf("%w: %w", errInvalidSessionBody, err)
		}
		return strings.TrimSpace(request.FormValue("token")), nil
	}

	decoder := json.NewDecoder(io.LimitReader(request.Body, maxRequestBody))
	decoder.DisallowUnknownFields()

	var body sessionRequest
	if err := decoder.Decode(&body); err != nil {
		return "", fmt.Errorf("%w: %w", errInvalidSessionBody, err)
	}
	return strings.TrimSpace(body.Token), nil
}
