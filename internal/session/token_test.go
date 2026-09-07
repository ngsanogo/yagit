package session

import (
	"bytes"
	"strings"
	"testing"
)

func TestMintIsFortyThreePrintableCharacters(t *testing.T) {
	// The daemon refuses a token below 22 characters or carrying anything but
	// printable ASCII. 32 bytes of base64url is 43 characters and satisfies
	// both; this is here so a change of encoding cannot quietly stop doing so.
	token, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 {
		t.Errorf("token is %d characters, want 43", len(token))
	}
	for _, character := range token {
		if character <= ' ' || character > '~' {
			t.Fatalf("token holds %q, which cannot travel in a URL or a cookie", character)
		}
	}
	if err := Check(token); err != nil {
		t.Errorf("Check refuses what Mint produced: %v", err)
	}

	// Twice, because a token that repeats is not a secret.
	second, err := Mint()
	if err != nil {
		t.Fatal(err)
	}
	if token == second {
		t.Error("two calls produced the same token")
	}
}

func TestMintedAcceptsOnlyWhatMintProduces(t *testing.T) {
	minted, err := Mint()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		token string
		want  bool
	}{
		{"a minted token", minted, true},
		{"nothing", "", false},
		{"a password", "hunter2", false},
		{"one character short", minted[:len(minted)-1], false},
		{"one character long", minted + "A", false},
		// An editor that wraps at forty columns leaves this. The decoder skips
		// the newline, so the bytes come out right and the text is still
		// something the daemon refuses on every start.
		{"hard-wrapped onto two lines", minted[:40] + "\n" + minted[40:], false},
		{"with a carriage return inside", minted[:20] + "\r\n" + minted[20:], false},
		// Padding is not what Mint writes, and a decoder that tolerates it is
		// no reason to store it.
		{"padded", minted + "=", false},
		// The standard alphabet: the same 32 bytes, the wrong spelling. 0xfb
		// encodes to "-_v7", so every group carries both URL-safe characters.
		{"the standard alphabet", strings.NewReplacer("-", "+", "_", "/").Replace(
			encoding.EncodeToString(bytes.Repeat([]byte{0xfb}, TokenBytes))), false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Minted(testCase.token); got != testCase.want {
				t.Errorf("Minted(%q) = %v, want %v", testCase.token, got, testCase.want)
			}
		})
	}
}

func TestCheckHoldsTheFloorFromBothSides(t *testing.T) {
	// A threshold tested from one side only is a threshold that can be off by
	// one forever.
	if err := Check(strings.Repeat("a", MinimumTokenLength)); err != nil {
		t.Errorf("a token of exactly the documented minimum was refused: %v", err)
	}
	if err := Check(strings.Repeat("a", MinimumTokenLength-1)); err == nil {
		t.Error("a token one character below the floor was accepted")
	}
}

func TestCheckRefusesWhatACookieWouldMangle(t *testing.T) {
	for name, token := range map[string]string{
		"a space":          "aaaaaaaaaaa aaaaaaaaaaaa",
		"a newline":        "aaaaaaaaaaa\naaaaaaaaaaaa",
		"a tab":            "aaaaaaaaaaa\taaaaaaaaaaaa",
		"a non-ASCII rune": "aaaaaaaaaaa🌳aaaaaaaaaaaa",
		"a DEL":            "aaaaaaaaaaa\x7faaaaaaaaaaaa",
	} {
		t.Run(name, func(t *testing.T) {
			err := Check(token)
			if err == nil {
				t.Fatalf("accepted %q", token)
			}
			// The message has to name what is wrong with it. "invalid token"
			// sends the reader to the source; this project does not do that.
			if !strings.Contains(err.Error(), "space or a control character") {
				t.Errorf("error does not say what is wrong: %v", err)
			}
		})
	}
}
