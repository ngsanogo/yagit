// Package session mints and checks the session token: the one secret that
// stands between a web page and every repository the daemon has open.
//
// Two programs handle it. The daemon generates a token when nothing hands it
// one; `./do` mints one into the checkout and hands it down. Each used to
// carry its own copy of the same two numbers — how many bytes a token holds,
// how short one may be. A copy is a place to drift. This package is the one
// place.
package session

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// TokenBytes is the entropy of a minted token: 32 bytes, 256 bits. The token
// guards write access to every open repository; there is no reason to be
// frugal here.
const TokenBytes = 32

// MinimumTokenLength is the floor for a token handed in from outside —
// YAGIT_TOKEN in the daemon's environment. It is well below the 43
// characters Mint produces, because a caller may have a scheme of its own;
// 128 bits of entropy is the point below which the token stops being one.
const MinimumTokenLength = 22

// encoding is base64url without padding. The token travels in a header, a
// cookie and a form field, and none of them wants a `=` or a `/`.
var encoding = base64.RawURLEncoding

// Mint produces a token: TokenBytes of randomness, encoded.
func Mint() (string, error) {
	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating a session token: %w", err)
	}
	return encoding.EncodeToString(raw), nil
}

// Minted reports whether s is exactly what Mint produces: TokenBytes of data,
// and the text those bytes encode to — nothing more, nothing less.
//
// The round trip is the point. The decoder skips a newline it meets inside
// the text, so a token an editor hard-wrapped onto two lines decodes to the
// right number of bytes and looks fine — and the daemon then refuses it, on
// every start, for the control character it holds. Encoding the bytes back
// and comparing catches that, and padding, and everything else the decoder
// is lenient about.
func Minted(s string) bool {
	raw, err := encoding.DecodeString(s)
	return err == nil && len(raw) == TokenBytes && encoding.EncodeToString(raw) == s
}

// Check refuses a supplied token that is too short to be a secret, or that
// carries a character a URL or a cookie would mangle.
//
// It is the daemon's rule for a token it did not generate: looser than
// Minted, because a caller may have a scheme of its own, and still a rule —
// accepting `hunter2` without a word would turn a documented convenience into
// a silent downgrade of the whole model.
func Check(s string) error {
	if len(s) < MinimumTokenLength {
		return fmt.Errorf("%d characters is too short for a token: at least %d are required, and %d is what a minted one has",
			len(s), MinimumTokenLength, encoding.EncodedLen(TokenBytes))
	}
	if strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r > '~' }) {
		return errors.New("a token cannot hold a space or a control character: it travels in a URL and in a cookie, and must be printable ASCII")
	}
	return nil
}
