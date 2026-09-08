package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// securityHeaders adds baseline HTTP defenses on every response.
//
// CSP allows inline styles because the unauthorized page and the embedded SPA
// ship self-contained HTML with a <style> block. Scripts come only from the
// same origin — there is no third-party analytics to whitelist.
//
// scriptPolicy is the one directive that differs between the two ways the
// interface is served, and it is worth being explicit about why. In
// production the daemon serves a built bundle: every script is a file on this
// origin, so 'self' is exactly right and nothing inline has any business
// running. In development the same page comes from Vite through the proxy,
// and Vite injects an inline module — React Fast Refresh's preamble — into
// the HTML it generates. 'self' alone blocks it, the preamble never defines
// the hook the transformed modules look for, and every component fails to
// mount with "@vitejs/plugin-react can't detect preamble" in the console.
//
// So the development policy allows 'unsafe-inline' for scripts and nothing
// else. It is the narrowest possible divergence, it applies only where a
// dev server was configured — which production never does — and it is
// written here rather than left as an accident of whichever handler answered.
func securityHeaders(development bool) func(http.Handler) http.Handler {
	scriptPolicy := "script-src 'self'; "
	if development {
		scriptPolicy = "script-src 'self' 'unsafe-inline'; "
	}

	// No directive admits data:, and the build is what keeps that true.
	//
	// A bundler that inlines a small asset turns it into a data: URI the
	// policy then refuses — in the released binary only, because development
	// serves that CSS from Vite unbuilt. It happened once, to a font, and the
	// answer is not a directive per asset type: web/vite.config.ts sets
	// assetsInlineLimit to 0, so every asset is a file on this origin, and
	// `./do build` fails if a stylesheet ever carries one anyway.
	policy := "default-src 'self'; " +
		scriptPolicy +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self'; " +
		"connect-src 'self'; " +
		"frame-ancestors 'none'; " +
		"base-uri 'self'"

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			header := writer.Header()
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "DENY")
			// same-origin, not no-referrer: under no-referrer browsers set
			// Origin to "null" on HTML form POSTs (the session exchange), and
			// the cookie CSRF check then refuses the only legitimate way in.
			header.Set("Referrer-Policy", "same-origin")
			header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
			header.Set("Content-Security-Policy", policy)
			next.ServeHTTP(writer, request)
		})
	}
}

// rateLimiter caps how many requests one client may make in a fixed window.
//
// The limit is generous enough for normal browsing — scrolling a history loads
// pages in bursts — but stops an automated probe from hammering the API.
type rateLimiter struct {
	limit   int
	window  time.Duration
	mutex   sync.Mutex
	clients map[string]*clientWindow
}

type clientWindow struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]*clientWindow),
	}
}

func (limiter *rateLimiter) allow(key string) bool {
	now := time.Now()

	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	entry, present := limiter.clients[key]
	if !present || now.After(entry.resetAt) {
		limiter.forgetExpired(now)
		// Cap distinct keys so YAGIT_LISTEN_ALL cannot grow the map without
		// bound under a flood of source addresses. Refusing a new key is a
		// 429, which is the same answer a known client gets when over budget.
		// Checked after the sweep: an expired key that was just forgotten is
		// a new insertion again.
		if _, held := limiter.clients[key]; !held && len(limiter.clients) >= maxCredentialClients {
			return false
		}
		limiter.clients[key] = &clientWindow{count: 1, resetAt: now.Add(limiter.window)}
		return true
	}

	if entry.count >= limiter.limit {
		return false
	}
	entry.count++
	return true
}

// forgetExpired drops the windows that have run out. The caller holds the lock.
//
// Without it the map only ever grows, keyed by the source address of whoever
// connects: on a loopback daemon that is one entry, but YAGIT_LISTEN_ALL
// exists precisely so the daemon can be reached from elsewhere, and there the
// key is chosen by the client. A sweep on the path that starts a window is
// enough — it runs exactly as often as new keys appear, and never on the hot
// path of a client already inside its window.
func (limiter *rateLimiter) forgetExpired(now time.Time) {
	for key, window := range limiter.clients {
		if now.After(window.resetAt) {
			delete(limiter.clients, key)
		}
	}
}

func clientKey(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

// What the number is, and what it is not for.
//
// It is not derived from an attack. The token is 256 bits, so guessing it is
// infeasible at sixty a minute and infeasible at six hundred; no human-scale
// figure changes that arithmetic, and picking one as though it did would be
// arithmetic theatre. What a bound does buy is that a runaway loop cannot
// occupy the daemon, and any number comfortably above legitimate use gives
// that.
//
// Six hundred is that number. A browser exchanges its token once per page
// load, so ordinary use is single figures a minute — while the end-to-end
// suite is six browsers behind one address doing a fresh exchange per test,
// and a tighter figure was found firing on the project's own tests rather
// than on anything worth stopping.
const (
	sessionRateLimit  = 600 // credential exchanges per minute per client
	sessionRateWindow = time.Minute

	// maxCredentialClients caps how many distinct source addresses the
	// credential limiter keeps. Without it, listen-all grows one entry per
	// probing IP forever (forgetExpired only runs when a new window starts,
	// and a flood of new IPs never reuses a key).
	maxCredentialClients = 4096
)

// rateLimit counts attempts to present a credential, and only those.
//
// It used to count every request under /api/, at six hundred a minute, and
// that number could not be defended in either direction. Against the
// application it was too low: an open repository polls its status and its
// diff every two seconds, so a handful of tabs reaches six hundred a minute
// doing exactly what the interface is built to do — and the end-to-end suite,
// six browsers behind one address, hit it as soon as phase 5 landed. Against
// an attacker it bought nothing: the daemon binds the loopback, so every
// legitimate client and any local process share the one key this limiter can
// see, and a probe already inside the machine is not slowed by a budget it
// shares with the user it is hiding behind.
//
// What a limit does protect is the door. Everything under /api/ but the
// session exchange sits behind requireToken, so reaching it at all means
// holding the secret; a client that holds it is not an attacker to be
// throttled. The exchange is the one route that TAKES a secret and answers
// whether it was right, and that is the only thing here worth making slow.
//
// Requests that fail authentication are counted too, and by the same limiter:
// presenting a token that is wrong is the same guess whichever route it was
// aimed at. That happens in requireToken, which is where the answer is known —
// see its credentialAbsent branch.
//
// Everything outside /api/ is the interface itself: its HTML, its bundle, its
// fonts. One page load in development asks the Vite proxy for a couple of
// hundred modules, and counting those was how a developer got locked out of
// their own daemon by two refreshes.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	s.credentials = newRateLimiter(sessionRateLimit, sessionRateWindow)

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/session" {
			next.ServeHTTP(writer, request)
			return
		}

		if !s.credentials.allow(clientKey(request)) {
			// fromTheDoorPage, the same predicate the handler behind this
			// uses to decide a refused token: whoever the exchange would have
			// answered on the page is answered on the page here too. A budget
			// that fired is still a refusal a reader has to be able to read.
			writeTooManyAttempts(writer, s.logger, fromTheDoorPage(request))
			return
		}
		next.ServeHTTP(writer, request)
	})
}
