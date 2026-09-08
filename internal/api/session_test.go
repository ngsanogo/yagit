package api_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The exchange has two callers with nothing in common: the application, which
// posts JSON and reads the answer, and the form on the unauthorized page,
// which posts a body a browser encodes and shows the reader whatever comes
// back. These pin the second one, because it was the one being answered in the
// first one's language — a wrong token returned the JSON refusal, rendered as
// text in the browser window, with the field the reader had just typed into a
// page behind them. That is the exact failure the door page exists to prevent.

// doorSubmission is what the form sends: form-encoded, no script involved, and
// no Accept header worth reading — a browser asks for text/html on the
// navigation that follows, which is not this request.
func doorSubmission(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func TestAWrongTokenFromTheDoorComesBackAsTheDoor(t *testing.T) {
	handler := testServer(t)

	response := execute(handler, doorSubmission("token=not-the-token"))

	// 401, not 200: the page is a courtesy to the reader, and a proxy or a
	// `curl -f` still has to see the refusal for what it is.
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html: %s", contentType, response.Body)
	}

	body := response.Body.String()
	// The refusal has to arrive with somewhere to act on it. A sentence with
	// no field under it is the dead end in a friendlier font.
	for _, needed := range []string{`name="token"`, `action="/api/session"`, "not accepted"} {
		if !strings.Contains(body, needed) {
			t.Errorf("the answer does not carry %q: %s", needed, body)
		}
	}
}

// Two strings must never reach this page, and only one of them is the secret.
// The other is the guess: it is chosen by whoever is guessing, and a page that
// echoed it back would be a reflection point on the one route that answers
// before any credential has been checked.
func TestTheDoorRefusalReflectsNothingThatWasSubmitted(t *testing.T) {
	handler := testServer(t)

	guess := `<script>alert(1)</script>`
	response := execute(handler, doorSubmission("token="+guess))

	body := response.Body.String()
	if strings.Contains(body, testToken) {
		t.Error("the refusal page carries the session token")
	}
	// Both forms: raw, which would be a script on the page, and escaped, which
	// would merely be the guess printed back. Neither belongs there.
	if strings.Contains(body, guess) || strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("the refusal reflects the submitted value: %s", body)
	}
}

// An empty field is the other way a person arrives at the refusal — the Enter
// key on a page they have not typed into yet. It must not be mistaken for a
// missing credential and answered somewhere else.
func TestAnEmptyTokenFromTheDoorComesBackAsTheDoor(t *testing.T) {
	handler := testServer(t)

	response := execute(handler, doorSubmission("token="))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if !strings.Contains(response.Body.String(), "not accepted") {
		t.Errorf("an empty submission got no refusal to read: %s", response.Body)
	}
}

// A body the daemon cannot decode is answered on the page too, and at its own
// status. The reader is the same reader; only the reason changed.
func TestAnUnreadableFormComesBackAsTheDoor(t *testing.T) {
	handler := testServer(t)

	// "%zz" is not a percent escape, so ParseForm refuses the body before any
	// field exists to read.
	response := execute(handler, doorSubmission("token=%zz"))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body)
	}
	body := response.Body.String()
	if !strings.Contains(body, "Reload this page") {
		t.Errorf("the answer does not say what to do: %s", body)
	}
	// The parser quotes the offending escape back in its error, and that error
	// is built from the request. The page says a constant instead.
	if strings.Contains(body, "%zz") {
		t.Errorf("the page carries a value taken from the request: %s", body)
	}
}

// The client's answer is unchanged, and Accept does not move it. A request
// under /api/ is never a navigation whatever it claims: handing JSON's caller
// a page is the mirror image of the bug above, and the harder one to notice,
// because the status line still says 401 while the parse fails.
func TestAWrongTokenFromAClientIsStillJSON(t *testing.T) {
	handler := testServer(t)

	request := httptest.NewRequest(http.MethodPost, "/api/session",
		strings.NewReader(`{"token":"not-the-token"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/html")

	response := execute(handler, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if !strings.Contains(response.Body.String(), `"error"`) {
		t.Errorf("a client got %q, which is not the JSON refusal", response.Body)
	}
}

// The budget is what makes the door slow to guess at, and it is charged by the
// middleware in front of the handler. Answering a browser in HTML must not
// step around it: a refusal that came back as a page and cost nothing would be
// an unlimited oracle reachable from any browser on the network.
func TestAFailedDoorAttemptIsStillCharged(t *testing.T) {
	handler := testServer(t)

	var refused *httptest.ResponseRecorder
	for range 1200 {
		response := execute(handler, doorSubmission("token=not-the-token"))
		if response.Code == http.StatusTooManyRequests {
			refused = response
			break
		}
	}

	if refused == nil {
		t.Fatal("1200 wrong tokens from the door were all answered; the limit never fired")
	}
	if body := refused.Body.String(); !strings.Contains(body, "wait") {
		t.Errorf("refusal body = %q, want it to say what to do", body)
	}
}

// A wrong token that reached the daemon is a record worth keeping whichever
// way it was answered. The HTML arm does not go through writeError, so the
// line it writes is its own — and it has to be there.
func TestTheDoorRefusalIsLogged(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	handler := testServerWithLogger(t, logger)

	if response := execute(handler, doorSubmission("token=not-the-token")); response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "session refused") {
		t.Errorf("the refusal never reached the log, got: %s", logged)
	}
	// Neither the secret nor the guess. A log is read by whoever can read the
	// disk, and a token written there outlives the process that minted it.
	if strings.Contains(logged, testToken) || strings.Contains(logged, "not-the-token") {
		t.Errorf("the log carries a token value: %s", logged)
	}
}
