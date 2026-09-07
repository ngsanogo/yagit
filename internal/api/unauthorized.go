package api

import (
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

// What an unauthenticated request is answered with, and why it depends on who
// is asking.
//
// A client gets JSON, because a client parses. A person who has typed the
// daemon's address into a browser gets a page, because a person reads — and
// what they were getting was this, rendered as text in a browser window:
//
//	{"error":{"message":"missing or invalid authentication token; run ./do
//	token to read it, then send it in an X-Yagit-Token header or POST it to
//	/api/session"}}
//
// That tells a client exactly what to do and leaves a reader nowhere to do
// it: no field to paste into, and a header no address bar can set. It is the
// first thing anyone browsing from another machine sees — the exact case
// YAGIT_PUBLIC_HOST exists for.
//
// This is Jupyter's model, which the token scheme already borrows: the same
// page that refuses you tells you how to get in, and takes the token when you
// have it.

// wantsHTML reports whether the request came from someone reading rather than
// something parsing.
//
// The Accept header is the honest signal. A browser navigating to a page asks
// for text/html; fetch() defaults to */*, and curl sends no Accept at all. A
// request under /api is never a navigation whatever it claims, and answering
// one with a page would hand a client HTML where it expects an object.
func wantsHTML(request *http.Request) bool {
	if strings.HasPrefix(request.URL.Path, "/api/") {
		return false
	}
	return strings.Contains(request.Header.Get("Accept"), "text/html")
}

// unauthorizedPage is the door.
//
// The form posts to /api/session, which sets the HttpOnly cookie without ever
// putting the secret in the URL — where it would linger in history, proxy
// logs and Referer headers.
//
// html/template rather than a string: everything interpolated here is fixed
// text today, and the day someone interpolates the host or the error it will
// escape it instead of reflecting it.
var unauthorizedPage = template.Must(template.New("unauthorized").Parse(
	`<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>yagit needs its session token</title>
<style>
  :root { color-scheme: dark }
  body {
    margin: 0; min-height: 100vh; display: grid; place-items: center;
    background: #0a1213; color: #e5eded;
    font: 15px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
  }
  main { width: min(34rem, 90vw); padding: 2rem }
  h1 { font-size: 1.25rem; margin: 0 0 .75rem }
  p { color: #919d9e; margin: 0 0 1rem }
  code, input {
    font-family: ui-monospace, "JetBrains Mono", SFMono-Regular, Menlo, monospace;
    font-size: .875rem;
  }
  code { color: #7cd7b0 }
  form { display: flex; gap: .5rem; margin: 1.5rem 0 0 }
  input {
    flex: 1; padding: .55rem .7rem; border-radius: .375rem;
    border: 1px solid #4b5a5b; background: #050c0c; color: #e5eded;
  }
  input:focus-visible { outline: 2px solid #5eead4; outline-offset: 1px }
  button {
    padding: .55rem 1rem; border: 0; border-radius: .375rem;
    background: #5eead4; color: #06201a; font-weight: 600; cursor: pointer;
  }
</style>
<main>
  <h1>yagit needs its session token</h1>
  <p>
    Every route requires it. The daemon says where the token lives at startup,
    in the line that begins <code>Session token:</code> — paste it below. In a
    checkout, <code>./do token</code> prints it.
  </p>
  <p>
    The token is exchanged for a cookie and never appears in the address bar.
  </p>
  <form method="POST" action="/api/session">
    <input name="token" type="password" autocomplete="off" autofocus
           aria-label="Session token" placeholder="session token">
    <button type="submit">Open</button>
  </form>
</main>
`))

// writeUnauthorized answers a missing token: a page for a person, JSON for
// everything else.
func writeUnauthorized(writer http.ResponseWriter, request *http.Request, logger *slog.Logger) {
	if !wantsHTML(request) {
		writeError(writer, logger, http.StatusUnauthorized, errTokenRequired)
		return
	}

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")

	// The status stays 401. The page is a courtesy to the reader, not a claim
	// that the request succeeded — a proxy, a probe or a `curl -f` still has
	// to see the refusal for what it is.
	writer.WriteHeader(http.StatusUnauthorized)

	if err := unauthorizedPage.Execute(writer, nil); err != nil {
		logger.Warn("could not write the unauthorized page", "error", err)
	}
}
