package git

import "strings"

// Credentials travel in URLs, and a URL yagit runs reaches four places that
// outlive the request: the log panel of every open tab, the backlog served by
// GET /api/log, the JSON of a failed command, and the daemon's own journal on
// disk.
//
// RedactURL already says why that matters and takes the secret out at the
// boundary rather than at each point of display. What it did not cover was the
// boundary that matters most: the command as it actually ran. A confirmation
// dialog showed `https://ada:***@github.com/ada/x.git` and the log panel showed
// the token two seconds later, on the same screen.
//
// So the redaction happens once, in Exec, on the way out — see recordExecution.
// Nothing downstream has to remember, which is the property the original
// comment claimed and this file makes true.

// redactArguments returns args with the credentials taken out of every URL
// among them.
//
// A fresh slice: the caller's own arguments are what git was handed and what a
// test asserting the real command line needs to keep seeing.
func redactArguments(args []string) []string {
	redacted := make([]string, len(args))
	for index, argument := range args {
		redacted[index] = RedactText(argument)
	}
	return redacted
}

// RedactText hides the credentials of every URL inside a longer string.
//
// RedactURL takes a string that IS a URL. This one takes a string that
// CONTAINS them, which is what git's own error output looks like:
//
//	fatal: repository 'https://ada:ghp_xxx@github.com/ada/x.git/' not found
//
// That sentence is shown to the user, written to the journal and broadcast to
// every open tab, so the token in the middle of it is as exposed as the one in
// the argument list — and this is the half a per-argument pass would miss.
//
// What is found is a scheme, `://`, and an authority holding an at sign. The
// scan reads the authority up to the first character that cannot be in one, so
// the trailing quote, comma or full stop of the sentence around it is left
// alone.
func RedactText(text string) string {
	const separator = "://"

	var built strings.Builder
	rest := text

	for {
		at := strings.Index(rest, separator)
		if at < 0 {
			built.WriteString(rest)
			return built.String()
		}

		// Backwards over the scheme. RFC 3986 allows letters, digits, `+`, `-`
		// and `.`, and the first character must be a letter — a `://` with no
		// scheme in front of it is not a URL, and skipping it here is what
		// keeps this from rewriting prose that happens to hold the sequence.
		start := at
		for start > 0 && isSchemeByte(rest[start-1]) {
			start--
		}
		if start == at || !isLetter(rest[start]) {
			built.WriteString(rest[:at+len(separator)])
			rest = rest[at+len(separator):]
			continue
		}

		authorityStart := at + len(separator)
		authorityEnd := authorityStart
		for authorityEnd < len(rest) && isAuthorityByte(rest[authorityEnd]) {
			authorityEnd++
		}

		url := rest[start:authorityEnd]
		built.WriteString(rest[:start])
		built.WriteString(RedactURL(url))
		rest = rest[authorityEnd:]
	}
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isSchemeByte(b byte) bool {
	return isLetter(b) || (b >= '0' && b <= '9') || b == '+' || b == '-' || b == '.'
}

// isAuthorityByte reports whether b can appear in the part of a URL before the
// path.
//
// Everything except the delimiters that end an authority — `/`, `?`, `#` — and
// the characters that end a URL inside running text: whitespace, and the
// quotes and brackets git wraps a URL in when it names one in an error. A
// password may legitimately contain punctuation, which is why the rule is
// stated as what stops the scan rather than as what is allowed.
func isAuthorityByte(b byte) bool {
	switch b {
	case '/', '?', '#', ' ', '\t', '\n', '\r', '"', '\'', '`', '<', '>', '[', ']', '(', ')', ',', ';':
		return false
	}
	return true
}
