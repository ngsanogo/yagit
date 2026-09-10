package publicurl

import (
	"strings"
	"testing"
)

// Every accepted case is written as what a person types and what a browser
// then presents, because the gap between the two is the bug this package
// exists for: an allowlist entry that looks right and matches nothing.
func TestOriginIsWhatTheBrowserPresents(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"a bare origin", "https://yagit.devvm.orb.local", "https://yagit.devvm.orb.local"},
		{"a lone trailing slash", "https://yagit.devvm.orb.local/", "https://yagit.devvm.orb.local"},
		// A browser lower-cases the scheme and the host before it writes an
		// Origin header, whatever the address bar was given.
		{"upper case", "HTTPS://Yagit.DevVM.orb.local/", "https://yagit.devvm.orb.local"},
		// The default port is never written in an origin. Kept, it would put
		// an entry on the allowlist that no browser ever sends.
		{"https on its default port", "https://yagit.example.com:443/", "https://yagit.example.com"},
		{"http on its default port", "http://yagit.example.com:80", "http://yagit.example.com"},
		{"a port with leading zeros", "https://yagit.example.com:0443", "https://yagit.example.com"},
		{"an empty port", "https://yagit.example.com:/", "https://yagit.example.com"},
		{"another port", "https://yagit.example.com:8443/", "https://yagit.example.com:8443"},
		// 443 is https's default, not http's.
		{"another scheme's default", "http://yagit.example.com:443", "http://yagit.example.com:443"},
		{"plain http", "http://yagit.example.com", "http://yagit.example.com"},
		{"an IPv6 literal with a port", "https://[::1]:8443/", "https://[::1]:8443"},
		{"an IPv6 literal without one", "https://[::1]/", "https://[::1]"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := Origin(testCase.raw)
			if err != nil {
				t.Fatalf("Origin(%q): %v", testCase.raw, err)
			}
			if got != testCase.want {
				t.Errorf("Origin(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

// Every refusal has to say what is wrong with the value, not only that it is:
// the sentence is read by somebody looking at one line of .env.
func TestOriginRefusesWhatIsNotAnOrigin(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		says string
	}{
		{"no scheme", "yagit.devvm.orb.local", "https://"},
		// A host and a port with no scheme parses as a scheme called the host.
		{"a host and a port", "yagit.devvm.orb.local:443", "https://"},
		{"another scheme", "ftp://yagit.example.com", "https://"},
		{"no host", "https://", "host"},
		{"only a port", "https://:8443", "host"},
		{"no slashes", "https:yagit.example.com", "host"},
		{"credentials", "https://ada:secret@yagit.example.com", "password"},
		// The one worth a sentence of its own: the interface asks for /api/
		// from the root, so a proxy serving it under a prefix cannot work.
		{"a path", "https://yagit.example.com/yagit/", "/yagit/"},
		{"a query", "https://yagit.example.com/?next=/", "query"},
		{"a fragment", "https://yagit.example.com/#top", "fragment"},
		{"an empty fragment", "https://yagit.example.com#", "fragment"},
		{"an empty query", "https://yagit.example.com?", "query"},
		{"port zero", "https://yagit.example.com:0", "port"},
		{"a port out of range", "https://yagit.example.com:70000", "port"},
		{"a space", "https://yagit example.com", "not a URL"},
		{"a name outside ASCII", "https://yägit.example.com", "ASCII"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := Origin(testCase.raw)
			if err == nil {
				t.Fatalf("Origin(%q) = %q, want a refusal", testCase.raw, got)
			}
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("Origin(%q) refused with %q, want it to mention %q", testCase.raw, err, testCase.says)
			}
		})
	}
}
