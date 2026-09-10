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
	if origin := request.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
		writeError(writer, s.logger, http.StatusForbidden, originRejected(origin))
		return
	}

	token, err := readSessionToken(request)
	if err != nil {
		s.refuseSession(writer, request, http.StatusBadRequest, err, refusalUnreadableForm)
		return
	}

	if !s.tokenMatches(token) {
		s.refuseSession(writer, request, http.StatusUnauthorized, errTokenRequired, refusalWrongToken)
		return
	}

	http.SetCookie(writer, s.sessionCookie(request))

	// The form that sent this has no script behind it and nothing it could do
	// with a 204, so the browser is sent on to the application. A client gets
	// the empty success and keeps control of what happens next.
	if fromTheDoorPage(request) {
		http.Redirect(writer, request, "/", http.StatusSeeOther)
		return
	}

	writer.WriteHeader(http.StatusNoContent)
}

// refuseSession answers a failed exchange: the door page again for the form
// that came from it, JSON for everything else.
//
// The two arms are the same refusal, at the same status, differing only in
// what can read it — and the two sentences differ for the same reason. cause
// is the client's account: it names a header to set and a route to post to,
// neither of which a reader can act on. refusal is the reader's: one line
// above the field they are already looking at. Each refusal is a fixed
// sentence rather than a format string, because the only value this route
// could interpolate is the one that was just refused — and that value is
// chosen by whoever is guessing.
//
// The origin check above does not come through here on purpose. A rejected
// Origin is not a reader at the door mistyping — it is a page on another site
// posting at this daemon, and the refusal names the origin it refused. That
// sentence carries a value from the request, and the one thing this page must
// never do is put such a value on screen.
func (s *Server) refuseSession(
	writer http.ResponseWriter, request *http.Request,
	status int, cause error, refusal string,
) {
	if !fromTheDoorPage(request) {
		writeError(writer, s.logger, status, cause)
		return
	}

	// Logged here rather than left to writeError, which the HTML arm does not
	// go through: a wrong token that reached the daemon and was turned away
	// has to appear in the log whichever way it was answered, or the one
	// record of someone guessing depends on how they asked.
	s.logger.Warn("session refused at the door", "status", status, "error", cause)

	writeDoor(writer, s.logger, status, refusal)
}

func readSessionToken(request *http.Request) (string, error) {
	// The predicate that decides how a refusal is answered, not a second
	// spelling of it. refuseSession shows refusalUnreadableForm exactly when
	// fromTheDoorPage is true, and that sentence tells the reader to reload
	// the page — advice that is only true if what failed was this parse. Two
	// copies of "is the body form-encoded?" is how the two come to disagree,
	// and the disagreement is silent: a reader told to reload a page whose
	// form was never read.
	if fromTheDoorPage(request) {
		if err := request.ParseForm(); err != nil {
			return "", fmt.Errorf("%w: %w", errInvalidSessionBody, err)
		}
		return strings.TrimSpace(request.FormValue("token")), nil
	}

	decoder := json.NewDecoder(io.LimitReader(request.Body, maxRequestBody))
	decoder.DisallowUnknownFields()

	var body sessionRequest
	if err := decoder.Decode(&body); err != nil {
		// forLog, and a %s rather than a second %w: DisallowUnknownFields
		// quotes the offending field name back, and this route answers before
		// any token is checked. An unknown field of 64 KiB — which is what
		// maxRequestBody lets through — is 64 KiB of response body and of log
		// line, once per attempt, from a caller holding no secret at all. See
		// maxLoggedValue.
		return "", fmt.Errorf("%w: %s", errInvalidSessionBody, forLog(err.Error()))
	}
	return strings.TrimSpace(body.Token), nil
}
