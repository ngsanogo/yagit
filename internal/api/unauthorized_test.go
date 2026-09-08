package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The door is the first screen anyone who types the daemon's address ever
// sees, and the one instruction on it is the whole of what they have to go on.
// These pin what that instruction says and what the page is allowed to load.

// theDoor is the page a person gets for arriving without a token: a
// navigation, outside /api/, asking for HTML.
func theDoor(t *testing.T, handler http.Handler) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")

	response := execute(handler, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
	return response.Body.String()
}

// The line the daemon prints at startup is a path — announceStartup writes
// "Session token:" followed by the file the token was written to, or by the
// command that prints one when there is no file. Neither form contains a
// token. A page that named that line and then said "paste it below" was
// telling the reader to paste a filesystem path into the field, and the step
// in between was nowhere on the screen.
func TestTheDoorNamesTheStepBetweenThePrintedLineAndTheToken(t *testing.T) {
	body := theDoor(t, testServer(t))

	for _, needed := range []string{
		// The line to look for, and what to do with what it names.
		"Session token:", "open that file",
		// The other way in, for a reader who has a checkout and no patience
		// for finding the file.
		"./do token",
	} {
		if !strings.Contains(body, needed) {
			t.Errorf("the instruction does not mention %q: %s", needed, body)
		}
	}
}

// A first visit is not a refused attempt. The reader has not typed anything
// yet, and a page that arrived already telling them they were wrong would be
// answering a question nobody asked.
func TestTheFirstVisitShowsNoRefusal(t *testing.T) {
	body := theDoor(t, testServer(t))

	// aria-describedby rather than aria-invalid: the stylesheet on the page
	// carries an [aria-invalid] selector whether or not anything matches it,
	// so only the description wiring proves an attempt was refused.
	for _, absent := range []string{"not accepted", "aria-describedby", `class="refusal"`} {
		if strings.Contains(body, absent) {
			t.Errorf("the first visit carries %q, which belongs to a failed attempt: %s", absent, body)
		}
	}
}

// The page loads nothing, and it cannot: every route but the form's target
// sits behind requireToken, so a stylesheet or a bundle referenced from here
// would be refused with the same 401 as the page that asked for it — and the
// production Content-Security-Policy allows no inline script either. A
// reference added in good faith would fail silently, on the one screen with no
// interface left to report it.
func TestTheDoorLoadsNothingItCannotHave(t *testing.T) {
	body := theDoor(t, testServer(t))

	for _, forbidden := range []string{"<script", "<link", "src=", "@import"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the door references %q, which is behind the token it is asking for: %s",
				forbidden, body)
		}
	}
}
