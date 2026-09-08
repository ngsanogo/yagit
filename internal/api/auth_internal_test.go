package api

import (
	"strings"
	"testing"
)

// originRejected is what writeError logs and returns to the client. Pin the
// bound here, against maxLoggedValue, so a looser HTTP-level assertion cannot
// quietly accept a multi-kilobyte truncation that still "passes".
func TestOriginRejectedAppliesForLog(t *testing.T) {
	padding := strings.Repeat("x", maxLoggedValue*4)
	origin := "https://evil.example/" + padding

	msg := originRejected(origin).Error()
	if strings.Contains(msg, padding) {
		t.Fatalf("originRejected kept the whole origin (%d bytes)", len(msg))
	}
	// errOriginRejected + ` (got "` + forLog(origin) + `")`
	ceiling := len(errOriginRejected.Error()) + len(` (got "`) + maxLoggedValue + len(`…")`)
	if len(msg) > ceiling {
		t.Fatalf("originRejected message is %d bytes, want ≤ %d", len(msg), ceiling)
	}
	if !strings.Contains(msg, "https://evil.example/") {
		t.Fatalf("originRejected dropped the readable prefix: %q", msg)
	}
}
