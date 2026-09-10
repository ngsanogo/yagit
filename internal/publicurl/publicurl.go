// Package publicurl reads the address a reverse proxy serves yagit at, and
// answers with what both programs need from it: the origin a browser presents
// on a page served from there.
//
// `./do` reads it from .env, refuses a malformed one before anything starts,
// and prints it on the card; the daemon reads the same value from -public-url,
// accepts its origin on cookie-authenticated writes, and announces it. Two
// copies of the rule would drift in the one direction nobody sees — a card
// printing an address whose origin the daemon refuses on the first click — so,
// as internal/session is for the token, this package is the one place.
package publicurl

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Origin checks a public URL and returns the origin a browser presents on it:
// the scheme, the host in lower case, and the port only when it is not the
// scheme's default.
//
// The normalising is not tidiness. The daemon compares Origin headers by exact
// string, and a browser never writes a default port or an upper-case host — so
// https://Yagit.example.com:443/ put on the allowlist as typed would match
// nothing a browser ever sends, and every write would be refused with a 403
// naming an origin that looks, to a person, exactly like the one configured.
//
// Everything beyond an origin is refused rather than dropped. A path is the
// case that matters: the interface requests /api/… from the root of its host,
// so a proxy serving yagit under /yagit/ cannot work, and accepting the URL
// would move that failure from this sentence to a blank page.
func Origin(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("not a URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("a public URL starts with https:// or http://, as in https://yagit.example.com")
	}
	if parsed.User != nil {
		// Refused rather than stripped: an origin never carries one, and a
		// password written into a configuration file is a leak whichever
		// program reads it.
		return "", errors.New("a public URL carries no user name or password")
	}

	// Checked on the raw text, because the parsed form cannot tell every case
	// apart: "https://host/#" parses to an empty fragment, the same as none.
	// With the scheme http(s) and no user information, a '?' or a '#' anywhere
	// in the text can only begin a query or a fragment.
	if strings.ContainsAny(raw, "?#") {
		return "", errors.New("a public URL has no query and no fragment: an origin is a scheme, a host and a port")
	}
	if path := parsed.EscapedPath(); path != "" && path != "/" {
		return "", fmt.Errorf(
			"a public URL has no path, and this one has %q: yagit is served at the root of its host, "+
				"because every address the interface requests begins with /", path)
	}

	host := parsed.Hostname()
	if host == "" {
		return "", errors.New("a public URL names a host, as in https://yagit.example.com")
	}
	if strings.ContainsFunc(host, func(r rune) bool { return r > '~' }) {
		// A browser presents an internationalised name in its ASCII form, and
		// converting it here would take a dependency for a case nobody has.
		return "", fmt.Errorf("write the host %q in its ASCII form (xn--…), which is the one a browser presents", host)
	}
	host = strings.ToLower(host)

	port, err := originPort(parsed.Scheme, parsed.Port())
	if err != nil {
		return "", err
	}

	if port == "" {
		if strings.Contains(host, ":") {
			// An IPv6 literal, which Hostname unbracketed and an origin needs
			// bracketed. JoinHostPort would add the brackets only with a port.
			host = "[" + host + "]"
		}
		return parsed.Scheme + "://" + host, nil
	}
	return parsed.Scheme + "://" + net.JoinHostPort(host, port), nil
}

// originPort returns the port as a browser writes it in an origin: nothing for
// the scheme's default, and the number without leading zeros otherwise.
func originPort(scheme, port string) (string, error) {
	if port == "" {
		return "", nil
	}
	// url.Parse has already refused anything but digits; the range is not its
	// concern, and a port of 0 or 70000 is a typo that would otherwise sit on
	// the allowlist matching nothing.
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", fmt.Errorf("the port %q is not a port number", port)
	}
	if (scheme == "https" && number == 443) || (scheme == "http" && number == 80) {
		return "", nil
	}
	return strconv.Itoa(number), nil
}
