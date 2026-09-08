package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Authentication keeps the half of Jupyter's model that still holds: a random
// token is generated at startup and exchanged for an HttpOnly cookie, so the
// browser carries a credential no script of its own can read.
//
// How the token first arrives is not Jupyter's. The announced URL carries no
// query string; the daemon prints where the token lives, and the page served
// to a request without a cookie posts it to /api/session. A ?token= query is
// still accepted on GET / for tools that already open that URL, exchanged for
// a cookie and stripped from the address. It is not accepted on API routes —
// those take the header — so the secret does not linger in a request URL.
//
// Without that token, any web page the user happens to visit could query the
// daemon: browsers allow requests to 127.0.0.1, and the daemon has write
// access to every open repository.

const (
	tokenCookieName     = "yagit_token"
	tokenQueryParameter = "token"
	tokenHeaderName     = "X-Yagit-Token"
)

var (
	// This message is the whole of what a client gets: no page, no form, no
	// second chance. It has to name where the token is and what to do with
	// it, because the address the caller used says neither.
	errTokenRequired = errors.New(
		"missing or invalid authentication token; " +
			"run ./do token to read it, then send it in an X-Yagit-Token header " +
			"or POST it to /api/session")

	errOriginRejected = errors.New(
		"origin rejected for a mutating request authenticated by cookie")
)

// originRejected explains a failed origin check. Origin "null" is what a
// sandboxed preview sends, and what browsers send on form POSTs when the
// page was served under Referrer-Policy: no-referrer — both look the same
// on the wire, so the message names the preview case the user can fix.
//
// The URL is named as the one the daemon printed rather than the one ./do
// printed: a release install has no ./do, and this message is exactly what
// somebody who installed from a release reaches.
func originRejected(origin string) error {
	switch origin {
	case "":
		return errOriginRejected
	case "null":
		return fmt.Errorf(
			"%w (got %q — open yagit in a regular browser at the URL the daemon printed at startup)",
			errOriginRejected, origin)
	default:
		// forLog, not the raw header. This value is the client's, and
		// writeError puts the message it lands in through the logger: an
		// Origin of a megabyte — which fits inside net/http's default header
		// limit — is a megabyte of log line, once per attempt, on a route
		// that answers before any token is checked. See maxLoggedValue.
		return fmt.Errorf("%w (got %q)", errOriginRejected, forLog(origin))
	}
}

// credentialSource says how a request authenticated. The distinction is not
// cosmetic: it decides whether the origin check applies.
type credentialSource int

const (
	credentialAbsent credentialSource = iota

	// credentialExplicit: the token came in a header or in the URL, so the
	// caller knows the secret. A third-party page can neither guess the token
	// nor set a custom header without a CORS preflight that yagit never
	// grants.
	credentialExplicit

	// credentialCookie: the browser attached the cookie on its own. This is
	// the only case where a third-party site could fire a request without the
	// user knowing, so the only one that needs an origin check.
	credentialCookie
)

func (s *Server) tokenMatches(candidate string) bool {
	if candidate == "" {
		return false
	}
	// Digests rather than the raw strings: ConstantTimeCompare returns at once
	// when lengths differ, which would leak the length of a custom YAGIT_TOKEN
	// under a timing probe. Hashes are fixed length either way.
	expected := sha256.Sum256([]byte(s.token))
	got := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(expected[:], got[:]) == 1
}

// authenticate keeps the first source that carries a valid token.
func (s *Server) authenticate(request *http.Request) credentialSource {
	if s.tokenMatches(request.Header.Get(tokenHeaderName)) {
		return credentialExplicit
	}
	if bearer, found := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer "); found {
		if s.tokenMatches(bearer) {
			return credentialExplicit
		}
	}
	// Query tokens are accepted only on the SPA entry. Putting ?token= on an
	// API URL leaves the secret in history, Referer and crash reports; tools
	// that need an API credential send the header instead. The page load path
	// still exchanges the query for a cookie and strips it (below).
	if request.Method == http.MethodGet && request.URL.Path == "/" &&
		s.tokenMatches(request.URL.Query().Get(tokenQueryParameter)) {
		return credentialExplicit
	}
	// A missing cookie is not an anomaly: that is what the very first request
	// looks like. Only its presence with the right value matters here.
	if cookie, err := request.Cookie(tokenCookieName); err == nil && s.tokenMatches(cookie.Value) {
		return credentialCookie
	}
	return credentialAbsent
}

func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		source := s.authenticate(request)
		if source == credentialAbsent {
			// Charged to the same budget as the session exchange, because it
			// is the same act: a credential offered and refused. Without this
			// the limit would guard one door of two, and the one it left open
			// answers the same question — was that token right? — on every
			// route under /api/.
			//
			// Counted after the refusal is decided, so a client that holds
			// the token never touches this counter however much it asks for.
			if s.credentials != nil && !s.credentials.allow(clientKey(request)) {
				writeError(writer, s.logger, http.StatusTooManyRequests, errTooManyRequests)
				return
			}
			writeUnauthorized(writer, request, s.logger)
			return
		}

		if isMutating(request.Method) && source == credentialCookie {
			if origin := request.Header.Get("Origin"); !s.originAllowed(origin) {
				writeError(writer, s.logger, http.StatusForbidden, originRejected(origin))
				return
			}
		}

		if s.tokenMatches(request.URL.Query().Get(tokenQueryParameter)) &&
			request.Method == http.MethodGet && request.URL.Path == "/" {
			http.SetCookie(writer, s.sessionCookie())

			// The token is stripped from the URL so it lingers neither in
			// browser history, nor in a screen share, nor in a bookmark.
			redirectWithoutToken(writer, request)
			return
		}

		next.ServeHTTP(writer, request)
	})
}

func (s *Server) sessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:  tokenCookieName,
		Value: s.token,
		Path:  "/",
		// HttpOnly: the token stays out of reach of any JavaScript, yagit's
		// own included, which has no use for it.
		HttpOnly: true,
		// SameSite=Strict: the browser does not attach this cookie to
		// requests coming from another site. That is the first barrier; the
		// origin check above is the second.
		SameSite: http.SameSiteStrictMode,
		// Secure follows the scheme the daemon is serving, and it has to go
		// that way round rather than always on: a browser never sends a
		// Secure cookie over http://, so setting it on plain HTTP would not
		// harden the session, it would end it — every request after the
		// exchange arriving with no cookie at all, and nothing anywhere
		// saying why. Under TLS it is what keeps the token off the wire the
		// first time anything addresses this daemon as http://.
		Secure: s.secureCookies,
	}
}

// redirectWithoutToken sends the browser back to the page it asked for, with
// the token gone from the query.
//
// The destination is built from a literal path rather than copied out of the
// request, and that is not pedantry. request.URL.Path is whatever the client
// sent: a request line of `GET //evil.com` gives it the value "//evil.com",
// RequestURI() hands that straight back, and a Location of "//evil.com" is a
// protocol-relative URL that a browser resolves against another host. The token
// would be gone from the address bar, and so would the user.
//
// Nothing is exploitable today — the only caller has already checked that the
// path is exactly "/", and the caller before that has checked the token. But
// the guard is twenty lines away from the redirect, and the second caller
// somebody adds will not have read it. A destination that cannot name another
// host is one fewer invariant to keep holding by hand.
func redirectWithoutToken(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	query.Del(tokenQueryParameter)

	// Path, not RawPath or Opaque: url.URL escapes it on the way out, so no
	// value in it can end the path early and start a host.
	cleaned := url.URL{Path: "/", RawQuery: query.Encode()}

	http.Redirect(writer, request, cleaned.RequestURI(), http.StatusFound)
}

// originAllowed takes the header value rather than the request: every caller
// needs the value anyway, to name it in the refusal, and reading one header in
// three places is three chances for the check and the message to disagree.
func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		// Every current browser sends Origin on a mutating request. Its
		// absence therefore signals a client that is not a browser — and
		// that should have authenticated with a header — or an attempt to
		// slip past the check. Both are refused the same way.
		return false
	}
	return slices.Contains(s.allowedOrigins, origin)
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
