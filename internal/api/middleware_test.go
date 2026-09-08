package api_test

import (
	"fmt"
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

// The two defenses in front of every route, and the two ways each of them was
// wrong before these tests existed: a policy that blocked the interface it was
// meant to protect, and a counter that fired on ordinary use.

// frontendMarker is what the stand-in frontend writes, so a test can tell a
// request that reached it from one a middleware turned back.
const frontendMarker = "frontend"

func middlewareServer(t *testing.T, development bool) http.Handler {
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
		Logger:         slog.New(slog.DiscardHandler),
		Development:    development,
		Frontend: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(writer, frontendMarker)
		}),
		Events: api.NewEventStream(slog.New(slog.DiscardHandler)),
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return server.Handler()
}

// headerOf reads one response header off any route, because securityHeaders
// wraps the whole handler and sets the same set on every answer it gives.
func headerOf(t *testing.T, handler http.Handler, name string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	request.Header.Set("X-Yagit-Token", testToken)
	return execute(handler, request).Header().Get(name)
}

func policyOf(t *testing.T, handler http.Handler) string {
	t.Helper()

	return headerOf(t, handler, "Content-Security-Policy")
}

// The production policy is the strict one: nothing inline may run.
func TestContentSecurityPolicyForbidsInlineScriptsInProduction(t *testing.T) {
	policy := policyOf(t, middlewareServer(t, false))

	if !strings.Contains(policy, "script-src 'self';") {
		t.Fatalf("policy = %q, want script-src 'self'", policy)
	}
	if strings.Contains(policy, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("policy = %q, production must not allow inline scripts", policy)
	}
}

// And the development one differs in exactly one directive.
//
// Vite injects an inline module — React Fast Refresh's preamble — into the
// HTML it generates. Under the strict policy the browser blocks it, every
// component fails to mount with "can't detect preamble", and the only clue is
// in a console nobody has open. It cost a full end-to-end suite once.
func TestContentSecurityPolicyAllowsVitesPreambleInDevelopment(t *testing.T) {
	policy := policyOf(t, middlewareServer(t, true))

	if !strings.Contains(policy, "script-src 'self' 'unsafe-inline';") {
		t.Fatalf("policy = %q, want the dev policy to allow Vite's inline preamble", policy)
	}

	// One directive, and no other. A development mode that quietly relaxed
	// the rest would be a policy nobody is really testing.
	for _, directive := range []string{
		"default-src 'self'",
		"img-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
	} {
		if !strings.Contains(policy, directive) {
			t.Errorf("policy = %q, want %q kept", policy, directive)
		}
	}
}

// Referrer-Policy must leave Origin intact on HTML form POSTs.
//
// no-referrer looks stricter, and browsers honour it by setting Origin to the
// literal "null" on navigate-mode POSTs (the session token form). The cookie
// CSRF check then refuses the only door a person has. same-origin still drops
// the Referer on cross-origin requests.
//
// The browser half of that cannot be observed from here — no request this
// test makes has a referrer policy applied to it — so what is pinned is the
// value, in both modes: the door is the same form in development, and a
// policy that differed there would break the interface nobody runs in
// production while developing it.
func TestReferrerPolicyKeepsFormPostOrigins(t *testing.T) {
	for _, development := range []bool{false, true} {
		policy := headerOf(t, middlewareServer(t, development), "Referrer-Policy")

		if policy != "same-origin" {
			t.Errorf("Referrer-Policy = %q with development=%v, want same-origin",
				policy, development)
		}
	}
}

// Nothing may be fetched from a data: URI, in either mode.
//
// A bundler that inlines a small asset produces one, and the browser refuses
// it against default-src 'self' — silently, in the released binary only. The
// project answers that in the bundler rather than here: assets stay files, and
// `./do build` checks it. Widening the policy instead is the change this
// guards against, because it is the tempting one — it makes the console clean
// again while leaving default-src meaning less than it says.
func TestContentSecurityPolicyAdmitsNoDataURIs(t *testing.T) {
	for _, development := range []bool{false, true} {
		policy := policyOf(t, middlewareServer(t, development))

		if strings.Contains(policy, "data:") {
			t.Errorf("development=%v: policy = %q, want no directive to allow data:",
				development, policy)
		}
	}
}

// The rate limit guards one thing: presenting a credential.
//
// One page load in development asks the Vite proxy for a couple of hundred
// modules; counting those meant a developer refreshing twice was locked out of
// their own daemon for a minute. This walks well past any limit in the file
// and expects every one of them through.
func TestRateLimitDoesNotCountTheInterfacesOwnAssets(t *testing.T) {
	handler := middlewareServer(t, true)

	for attempt := range 1200 {
		request := httptest.NewRequest(http.MethodGet, "/src/main.tsx", nil)
		request.Header.Set("X-Yagit-Token", testToken)

		response := execute(handler, request)
		if response.Code != http.StatusOK {
			t.Fatalf("asset request %d = %d, want 200 — the interface is not rate limited", attempt, response.Code)
		}
		if body := response.Body.String(); body != frontendMarker {
			t.Fatalf("asset request %d reached %q, want the frontend", attempt, body)
		}
	}
}

// A client that holds the token is never throttled, and that is the point of
// the limit being where it is.
//
// An open repository polls its status and its diff every two seconds. Six
// hundred a minute — what this used to allow across the whole API — is a
// handful of tabs doing exactly what the interface is built to do, and the
// end-to-end suite reached it as soon as phase 5 landed. Reaching these routes
// at all means holding the secret, so there is nothing left to throttle.
func TestAnAuthenticatedClientIsNotThrottled(t *testing.T) {
	handler := middlewareServer(t, false)

	for attempt := range 1200 {
		request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		request.Header.Set("X-Yagit-Token", testToken)

		if response := execute(handler, request); response.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 — a client with the token is not an attacker",
				attempt, response.Code)
		}
	}
}

// Guessing is. Whichever door the guess is aimed at.
//
// The session exchange is the route that TAKES a secret; every other route
// under /api/ answers the same question through requireToken. Both are charged
// to one budget, because presenting a token that is wrong is the same act
// either way — and a limit on one of the two doors is not a limit.
func TestPresentingACredentialIsLimited(t *testing.T) {
	for _, probe := range []struct {
		name    string
		request func() *http.Request
	}{
		{
			name: "the session exchange",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/api/session",
					strings.NewReader(`{"token":"not-the-token"}`))
			},
		},
		{
			name: "a token that is wrong, on an ordinary route",
			request: func() *http.Request {
				request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
				request.Header.Set("X-Yagit-Token", "not-the-token")
				return request
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			handler := middlewareServer(t, false)

			var refused *httptest.ResponseRecorder
			for range 1200 {
				response := execute(handler, probe.request())
				if response.Code == http.StatusTooManyRequests {
					refused = response
					break
				}
			}

			if refused == nil {
				t.Fatal("1200 refused credentials in a row were all answered; the limit never fired")
			}
			if body := refused.Body.String(); !strings.Contains(body, "wait") {
				t.Errorf("refusal body = %q, want it to say what to do", body)
			}
		})
	}
}
