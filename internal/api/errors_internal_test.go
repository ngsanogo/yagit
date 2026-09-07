package api

import (
	"strings"
	"testing"
)

func TestForLogKeepsOneLineOnOneLine(t *testing.T) {
	// The case the sanitiser exists for: a name written to forge a second
	// entry that a reader of the log could not tell from one yagit wrote.
	forged := "release\nlevel=ERROR msg=\"repository deleted\""
	got := forLog(forged)
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("forLog kept a line break: %q", got)
	}
	if !strings.Contains(got, "release") {
		t.Fatalf("forLog = %q, want the name still readable", got)
	}
}

func TestForLogDropsControlCharacters(t *testing.T) {
	if got := forLog("a\x00b\x1bc\x7fd"); strings.ContainsAny(got, "\x00\x1b\x7f") {
		t.Fatalf("forLog = %q", got)
	}
}

func TestForLogBoundsTheLength(t *testing.T) {
	got := forLog(strings.Repeat("x", maxLoggedValue*4))
	if len([]rune(got)) > maxLoggedValue+1 {
		t.Fatalf("forLog kept %d characters", len([]rune(got)))
	}
}

func TestForLogLeavesAnOrdinaryNameAlone(t *testing.T) {
	if got := forLog("release/2.0"); got != "release/2.0" {
		t.Fatalf("forLog = %q", got)
	}
}
