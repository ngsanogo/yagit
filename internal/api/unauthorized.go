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
// YAGIT_PUBLIC_HOST and YAGIT_PUBLIC_URL exist for.
//
// This is Jupyter's model, which the token scheme already borrows: the same
// page that refuses you tells you how to get in, and takes the token when you
// have it.
//
// The page is served three times over: for arriving without a token, for
// arriving with the wrong one, and for arriving too often. The second is the
// likeliest of the three — a token is forty-odd characters pasted from another
// terminal — and answering it in JSON reproduced the dead end above one step
// later in the same flow, with the reader's own retry field one screen behind
// them. See refuseSession, which decides between the two answers, and
// writeTooManyAttempts, which decides the third.

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

// fromTheDoorPage reports whether a session exchange was submitted by the form
// on the page above, rather than by a client.
//
// Content-Type is the signal here, not Accept, and it has to be: the exchange
// lives at /api/session, and wantsHTML refuses every path under /api/ by
// design — so the door's own POST cannot be recognised by what it accepts,
// however loudly the browser asks for HTML. A form-encoded body is what a
// browser sends when no script is involved, and nothing else in yagit sends
// one: the application's own fetch() posts JSON.
//
// The consequence for a client that posts a form on purpose — curl -d, which
// sends this Content-Type by default — is that it gets the page. That is the
// same trade the success path has always made, and it is the right way round:
// a tool reading a page it did not expect can still see the status line, while
// a person reading JSON has nowhere to go.
func fromTheDoorPage(request *http.Request) bool {
	return strings.HasPrefix(
		request.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
}

// doorPage is everything the page below is told.
//
// Refusal is empty on a first visit and one of the constants below on a failed
// exchange. It is a field rather than a formatted sentence because nothing the
// caller submitted may reach it: the closest thing to the secret a wrong guess
// produces is the guess, and echoing that back would put it in the DOM, in a
// screenshot, and in whatever the browser keeps of the page.
type doorPage struct {
	Refusal string
}

// The refusals, one per way the exchange can fail for a reader. Constants, for
// the reason doorPage.Refusal gives.
const (
	// No advice about trailing whitespace: readSessionToken trims it before
	// the comparison, so telling the reader to look for a stray space would
	// send them after a cause this daemon has already ruled out. What is left
	// is a paste that did not carry the whole value, and a token that was
	// right for a daemon holding a different one — which is what `./do up
	// --new-token` and a binary started with no token file both produce.
	//
	// It ends on an instruction because both causes have the same one, and a
	// refusal that named two causes and stopped would leave the reader where
	// the JSON refusal left them: told what went wrong, not what to do.
	refusalWrongToken = "That token was not accepted. " +
		"Space at either end is ignored, so the usual causes are a paste that " +
		"stopped short and a token this daemon no longer holds. " +
		"Read the value again and paste the whole of it."

	refusalUnreadableForm = "The form did not arrive in a state this daemon could read. " +
		"Reload this page and paste the token again."

	// Not a verdict on the value, and the sentence has to say so. The budget
	// counts attempts and not mistakes — the RIGHT token trips it just as
	// hard, which is exactly what a reader who has been retrying a good paste
	// runs into — so a refusal worded like the one above would send them
	// hunting for a fault in a value that has none.
	//
	// No figure in it. The window and the ceiling are sessionRateLimit and
	// sessionRateWindow, and a sentence naming either would be a second copy
	// of a number that lives in middleware.go: change the budget and this
	// page would go on quoting the old one, with nothing to say it had.
	refusalTooManyAttempts = "Too many attempts have arrived from this address. " +
		"The daemon counts attempts and not mistakes, so this says nothing about " +
		"the token you pasted: wait a moment, then send it again."
)

// unauthorizedPage is the door.
//
// The form posts to /api/session, which sets the HttpOnly cookie without ever
// putting the secret in the URL — where it would linger in history, proxy
// logs and Referer headers.
//
// html/template rather than a string: everything interpolated here is fixed
// text today, and the day someone interpolates the host or the error it will
// escape it instead of reflecting it.
//
// The page carries its own markup, its own styles and no script, and that is
// not a matter of taste. Every route but this form's target sits behind
// requireToken, so a stylesheet, a font or a bundle referenced from here would
// be refused with the same 401 as the page that asked for it; and the
// production Content-Security-Policy allows no inline script, so a field that
// revealed what was pasted into it could not be built here anyway.
//
// Which is also why the colours below are hex rather than the design tokens:
// this is the one screen that has to render before anything can be loaded, so
// the dark ramp is copied here by hand. Nothing keeps the copy in step —
// move a hue in web/src/design/tokens.css and this page silently keeps the old
// one. The set is kept small for that reason, not for brevity.
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
  .refusal {
    margin: 1.5rem 0 0; padding: .6rem .75rem; border-radius: .375rem;
    border: 1px solid #7a2c27; background: #4a1513; color: #f76d67;
  }
  form { display: flex; gap: .5rem; margin: 1.5rem 0 0 }
  .refusal + form { margin-top: .75rem }
  input {
    flex: 1; padding: .55rem .7rem; border-radius: .375rem;
    border: 1px solid #4b5a5b; background: #050c0c; color: #e5eded;
  }
  input:focus-visible { outline: 2px solid #5eead4; outline-offset: 1px }
  input[aria-invalid="true"] { border-color: #7a2c27 }
  button {
    padding: .55rem 1rem; border: 0; border-radius: .375rem;
    background: #5eead4; color: #06201a; font-weight: 600; cursor: pointer;
  }
</style>
<main>
  <h1>yagit needs its session token</h1>
  <p>
    Every route requires it, and the address the daemon printed does not carry
    it. At startup the daemon prints a second line beginning
    <code>Session token:</code>. That line names the file the token was written
    to, when there is one — open that file and paste its contents below. In a
    checkout, <code>./do token</code> prints the token itself.
  </p>
  <p>
    The token is exchanged for a cookie and never appears in the address bar.
  </p>
  {{- if .Refusal}}
  <p class="refusal" id="refusal">{{.Refusal}}</p>
  {{- end}}
  <form method="POST" action="/api/session">
    <input name="token" type="password" autocomplete="off" autofocus
           aria-label="Session token" placeholder="session token"
           {{- if .Refusal}} aria-invalid="true" aria-describedby="refusal"{{end}}>
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
	writeDoor(writer, logger, http.StatusUnauthorized, "")
}

// writeTooManyAttempts answers an exhausted credential budget in whichever
// language the route it guards would have used for the attempt itself.
//
// The budget stands in front of two doors — the session exchange, counted in
// rateLimit, and every other route, counted in requireToken — and both used to
// answer it in JSON. For a client that is right. For a reader it reproduced,
// one layer further up, the dead end the page above exists to prevent: an
// object rendered as text in a browser window, with the field they had just
// typed into a page behind them. It is the worse of the two dead ends, because
// the reader's token may well have been correct.
//
// asPage is the caller's answer rather than this function's, and it has to be:
// the two callers cannot ask the same question. A reader reaches rateLimit by
// submitting the form above, which fromTheDoorPage recognises by its
// Content-Type; a reader reaches requireToken by navigating, which wantsHTML
// recognises by its Accept — and wantsHTML refuses every path under /api/ by
// design, so it can never see the form. Choosing one test here would answer
// one of the two callers with the wrong one.
func writeTooManyAttempts(writer http.ResponseWriter, logger *slog.Logger, asPage bool) {
	if !asPage {
		writeError(writer, logger, http.StatusTooManyRequests, errTooManyRequests)
		return
	}

	// Logged for the reason refuseSession gives: this arm does not go through
	// writeError, and a budget that fired has to leave the same record
	// whichever way it was answered, or how often the door is hammered
	// depends on who was hammering it. Nothing from the request goes in the
	// line — the only value this path has seen is the credential that was
	// offered, and that is the one thing a log must never keep.
	logger.Warn("credential budget exhausted at the door", "status", http.StatusTooManyRequests)

	writeDoor(writer, logger, http.StatusTooManyRequests, refusalTooManyAttempts)
}

// writeDoor renders the page, with refusal shown on it when there is one.
//
// The status is the caller's, never assumed. A page instead of an object is a
// courtesy to the reader, not a claim that the request succeeded — a proxy, a
// probe or a `curl -f` still has to see the refusal for what it is, and a
// wrong token is still a 401 whether it is answered in JSON or in HTML.
func writeDoor(writer http.ResponseWriter, logger *slog.Logger, status int, refusal string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(status)

	if err := unauthorizedPage.Execute(writer, doorPage{Refusal: refusal}); err != nil {
		logger.Warn("could not write the unauthorized page", "error", err)
	}
}
