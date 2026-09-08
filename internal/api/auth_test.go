package api_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/api"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// These tests pin down the authentication model. It was verified by hand
// with curl once; these keep it verified without anyone repeating that.

const (
	testToken     = "a-perfectly-arbitrary-test-token"
	allowedOrigin = "http://127.0.0.1:7420"
)

// testServer is the daemon as it runs out of the box: plain HTTP, and so a
// session cookie with no Secure attribute. Every test but the cookie's own
// wants that one.
func testServer(t *testing.T) http.Handler {
	t.Helper()
	return testServerWithSecureCookies(t, false)
}

func testServerWithSecureCookies(t *testing.T, secureCookies bool) http.Handler {
	t.Helper()
	return newTestServer(t, secureCookies, slog.New(slog.DiscardHandler))
}

// testServerWithLogger is the same daemon with somewhere to read its log from,
// for the tests that check what a refusal writes as well as what it answers.
func testServerWithLogger(t *testing.T, logger *slog.Logger) http.Handler {
	t.Helper()
	return newTestServer(t, false, logger)
}

func newTestServer(t *testing.T, secureCookies bool, logger *slog.Logger) http.Handler {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}

	runner := git.NewRunner(nil)
	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	server, err := api.NewServer(api.Options{
		Registry:       registry,
		Runner:         runner,
		Token:          testToken,
		AllowedOrigins: []string{allowedOrigin},
		SecureCookies:  secureCookies,
		Logger:         logger,
		// The real frontend has no business in an API test. This stand-in
		// is enough to check that a non-API route does reach it once the
		// token has been accepted.
		Frontend: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}),
		Events: api.NewEventStream(logger),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server.Handler()
}

func execute(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestRequestWithoutTokenIsRefused(t *testing.T) {
	handler := testServer(t)

	response := execute(handler, httptest.NewRequest(http.MethodGet, "/api/health", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	// The message must say what to do, not only that access was refused.
	if !strings.Contains(response.Body.String(), "token") {
		t.Errorf("the body must explain the refusal, got: %s", response.Body.String())
	}
}

func TestInvalidTokenIsRefused(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set("X-Yagit-Token", testToken+"-but-not-quite")

	if response := execute(handler, request); response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestTokenAcceptedFromHeaderAndBearer(t *testing.T) {
	handler := testServer(t)

	cases := []struct {
		name    string
		prepare func(*http.Request)
	}{
		{"dedicated header", func(r *http.Request) { r.Header.Set("X-Yagit-Token", testToken) }},
		{"Authorization header", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+testToken) }},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			testCase.prepare(request)

			if response := execute(handler, request); response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
		})
	}
}

// A ?token= on an API URL is refused: the secret must not linger in a request
// line. The same query on GET / still exchanges for a cookie.
func TestTokenQueryIsRefusedOnAPIRoutes(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/health?token="+testToken, nil)
	if response := execute(handler, request); response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", response.Code, response.Body)
	}
}

// TestRedirectAfterTheTokenExchangeStaysOnThisSite pins the one property that
// keeps that redirect from becoming an open one.
//
// A Location beginning with "//" is a protocol-relative URL, and a browser
// resolves it against another host — so a redirect built by copying the
// client's own path is a way off this site. The path is checked before the
// redirect is reached, which is why nothing here is exploitable; this asserts
// the redirect itself, so the check twenty lines away stops being the only
// thing standing between a request line and somebody else's server.
func TestRedirectAfterTheTokenExchangeStaysOnThisSite(t *testing.T) {
	handler := testServer(t)

	for _, target := range []string{
		"/?token=" + testToken,
		"//evil.example.com/?token=" + testToken,
		"/\\evil.example.com/?token=" + testToken,
		"//evil.example.com/%2f..?token=" + testToken,
	} {
		t.Run(target, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, target, nil)
			response := execute(handler, request)

			location := response.Header().Get("Location")
			if location == "" {
				return // no redirect at all is the safest answer of the three
			}

			// The property is about the SHAPE of the destination, not about
			// what is spelled in it. "/evil.example.com/" is a path on this
			// host and perfectly fine — net/http's own mux produces it when it
			// collapses the doubled slash. "//evil.example.com/" is a host.
			// One slash apart, and only the second one leaves the site.
			if !strings.HasPrefix(location, "/") {
				t.Errorf("Location = %q, which is not a path on this host", location)
			}
			if strings.HasPrefix(location, "//") || strings.HasPrefix(location, `/\`) {
				t.Errorf("Location = %q, which a browser resolves against another host", location)
			}
		})
	}
}

// The deprecated ?token= query, kept for tools that already send one: it is
// exchanged for a cookie and stripped from the address, so a token nobody
// should have put there does not stay in browser history either.
func TestTokenInURLIsExchangedForACookieThenStripped(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/?token="+testToken, nil)
	response := execute(handler, request)

	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	if location := response.Header().Get("Location"); location != "/" {
		t.Errorf("Location = %q, want \"/\" with no token", location)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("want 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Value != testToken {
		t.Errorf("cookie value = %q", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Error("the cookie must be HttpOnly: no JavaScript needs the token")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Error("the cookie must be SameSite=Strict")
	}
}

// TestTheSecureAttributeFollowsTheScheme is asserted in both directions
// deliberately: the two ways of getting it wrong are symmetrical, and both are
// silent. A Secure cookie on plain HTTP is never sent back by the browser, so
// the session dies at the first request after the exchange with no error
// anywhere; a cookie without it under TLS travels in clear the first time
// anything addresses the daemon as http://. One direction alone would leave an
// inverted attribute passing.
func TestTheSecureAttributeFollowsTheScheme(t *testing.T) {
	cases := []struct {
		name          string
		secureCookies bool
	}{
		{"serving https", true},
		{"serving plain http", false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := testServerWithSecureCookies(t, testCase.secureCookies)

			request := httptest.NewRequest(http.MethodPost, "/api/session",
				strings.NewReader(`{"token":"`+testToken+`"}`))
			request.Header.Set("Content-Type", "application/json")

			response := execute(handler, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", response.Code)
			}

			cookies := response.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("want 1 cookie, got %d", len(cookies))
			}
			if cookies[0].Secure != testCase.secureCookies {
				t.Errorf("Secure = %v, want %v", cookies[0].Secure, testCase.secureCookies)
			}
		})
	}
}

// The heart of the CSRF protection: a mutating request authenticated by the
// cookie alone is the only one a third-party site can fire without the user
// knowing, so the only one that demands a known origin.
func TestMutatingCookieRequestDemandsAKnownOrigin(t *testing.T) {
	handler := testServer(t)

	cases := []struct {
		name       string
		origin     string
		wantStatus int
		// wantInBody is what the refusal has to name, so the reader learns
		// which origin was refused instead of only that one was.
		wantInBody string
	}{
		{"no origin", "", http.StatusForbidden, ""},
		{
			"foreign origin", "https://malicious-site.example",
			http.StatusForbidden, "https://malicious-site.example",
		},
		{"null origin", "null", http.StatusForbidden, "regular browser"},
		// 400 and not 200: the body carries no path, so the request is
		// rejected further along. What matters is that it got past the origin
		// check.
		{"yagit's own origin", allowedOrigin, http.StatusBadRequest, ""},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/repos", strings.NewReader("{}"))
			request.AddCookie(&http.Cookie{Name: "yagit_token", Value: testToken})
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}

			response := execute(handler, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, testCase.wantStatus)
			}
			if testCase.wantInBody != "" &&
				!strings.Contains(response.Body.String(), testCase.wantInBody) {
				t.Errorf("refusal must name %q, got: %s",
					testCase.wantInBody, response.Body.String())
			}
		})
	}
}

// A request carrying the token in the open proves it knows the secret: no
// third-party page can set that header without a CORS preflight yagit does
// not grant. The origin check therefore does not apply to it, and curl stays
// usable without ceremony.
func TestMutatingHeaderRequestNeedsNoOrigin(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodPost, "/api/repos", strings.NewReader("{}"))
	request.Header.Set("X-Yagit-Token", testToken)

	// 400: the path field is empty. The origin check was cleared.
	if response := execute(handler, request); response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (and above all not 403)", response.Code)
	}
}

func TestUnknownRepoAnswers404(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/repos/000000000000/commits", nil)
	request.Header.Set("X-Yagit-Token", testToken)

	response := execute(handler, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if !strings.Contains(response.Body.String(), "POST /api/repos") {
		t.Errorf("the message must say how to open a repository, got: %s", response.Body.String())
	}
}

// An unknown route under /api must answer in JSON instead of landing in the
// frontend, which would render an HTML page in reply to an API call — the
// worst possible clue for a typo in a URL.
func TestUnknownAPIRouteAnswers404AsJSON(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/api/doesnotexist", nil)
	request.Header.Set("X-Yagit-Token", testToken)

	response := execute(handler, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", contentType)
	}
}

// Everything that is not under /api goes back to the frontend once the token
// checks out: this is a single-page application, routing happens client-side.
func TestNonAPIPathIsHandedToTheFrontend(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodGet, "/history/abc123", nil)
	request.Header.Set("X-Yagit-Token", testToken)

	if response := execute(handler, request); response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}

func TestSessionExchangeRefusesAForeignOrigin(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodPost, "/api/session",
		strings.NewReader(`{"token":"`+testToken+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")

	response := execute(handler, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "https://evil.example") {
		t.Errorf("refusal must name the origin, got: %s", response.Body.String())
	}
}

func TestSessionExchangeRefusesANullOrigin(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodPost, "/api/session",
		strings.NewReader(`{"token":"`+testToken+`"}`))
	request.Header.Set("Content-Type", "application/json")
	// Sandboxed previews (editor simple browsers) send the literal string
	// "null", which is not an absent Origin and must stay refused.
	request.Header.Set("Origin", "null")

	response := execute(handler, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}
	if !strings.Contains(response.Body.String(), "regular browser") {
		t.Errorf("null Origin must say how to open yagit, got: %s", response.Body.String())
	}
}

// An Origin longer than a log line is still answered, and neither the log nor
// the response carries the whole of it.
//
// The session exchange decides the origin before it checks the token, so any
// local process can drive this path without holding the secret. Without a
// bound, one attempt writes as much as net/http will accept in a header.
func TestARejectedOriginIsBoundedInTheRefusal(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	handler := testServerWithLogger(t, logger)

	padding := strings.Repeat("x", 64*1024)
	request := httptest.NewRequest(http.MethodPost, "/api/session",
		strings.NewReader(`{"token":"`+testToken+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example/"+padding)

	response := execute(handler, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body)
	}

	// Both halves, and the first one is what keeps the bounds below from
	// passing on an empty answer: a refusal that said nothing at all would
	// satisfy every "not too long" check here.
	body := response.Body.String()
	if !strings.Contains(body, "https://evil.example/") {
		t.Errorf("refusal must still name the origin it refused, got: %s", body)
	}
	if strings.Contains(body, padding) {
		t.Errorf("refusal body carried the whole origin (%d bytes)", response.Body.Len())
	}
	// maxLoggedValue is 512; the wrapper around it is a short fixed phrase.
	// Anything near a kilobyte means the bound was lost.
	if response.Body.Len() > 1024 {
		t.Errorf("refusal body is %d bytes: the origin was not bounded", response.Body.Len())
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "origin rejected") {
		t.Errorf("the refusal must reach the log, got: %s", logged)
	}
	if strings.Contains(logged, padding) {
		t.Errorf("log carried the whole origin (%d bytes)", len(logged))
	}
	if len(logged) > 2048 {
		t.Errorf("log line is %d bytes: the origin was not bounded", len(logged))
	}
}

func TestSessionExchangeAllowsAToolWithNoOrigin(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodPost, "/api/session",
		strings.NewReader(`{"token":"`+testToken+`"}`))
	request.Header.Set("Content-Type", "application/json")

	response := execute(handler, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", response.Code, response.Body)
	}
}
