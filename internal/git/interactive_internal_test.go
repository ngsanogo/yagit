package git

import "testing"

// shellQuote is written for display and is executed in exactly one place: the
// GIT_SEQUENCE_EDITOR an interactive rebase sets, which git's own contract
// defines as a shell command and appends to without quoting. What that has to
// survive is an installation path — git builds the script as `<value> "$@"`,
// so an unquoted path with a space in it would have run its first word as a
// program.
//
// One word out of every one of them is the property under test, so each case
// is written as what a shell would see rather than as a string comparison of
// convenience.
func TestShellQuoteMakesOneWordOfAPath(t *testing.T) {
	for value, want := range map[string]string{
		// Nothing a shell reads: one word already.
		"/usr/local/bin/yagit": "/usr/local/bin/yagit",

		// The case that matters. A space is what makes an unquoted path two
		// words, and macOS puts applications in a directory with one.
		"/Applications/My Apps/yagit": `'/Applications/My Apps/yagit'`,

		// An apostrophe in a home directory. Double quotes carry it whole
		// because it holds none of the four a shell still reads inside them.
		`/home/obrien's/yagit`: `"/home/obrien's/yagit"`,

		// Everything a shell would otherwise act on, made literal.
		"/tmp/a;rm -rf ~/b":    `'/tmp/a;rm -rf ~/b'`,
		`/tmp/$(whoami)/yagit`: `'/tmp/$(whoami)/yagit'`,
		"/tmp/`id`/yagit":      "'/tmp/`id`/yagit'",
		`/tmp/back\slash`:      `'/tmp/back\slash'`,
		"":                     `''`,
	} {
		if got := shellQuote(value); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", value, got, want)
		}
	}
}
