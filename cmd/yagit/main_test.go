package main

import (
	"errors"
	"flag"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/session"
)

// This file is wiring, and wiring is where a security property is decided by
// four lines nobody reads twice: how long a token has to be, what mode its file
// gets, which origins a browser may present. None of it was covered.
//
// The daemon's own lifecycle is not tested here — a listener, a signal and a
// graceful shutdown are what the end-to-end suite exercises against a real
// process. What is tested is every decision made before that process is worth
// starting. The one lifecycle that suite never reaches, because the process it
// drives serves plain HTTP, is in tls_test.go.

// --------------------------------------------------------------------------
// sessionToken — the whole of the authentication model
// --------------------------------------------------------------------------

func TestSessionTokenGeneratesAFullStrengthToken(t *testing.T) {
	t.Setenv("YAGIT_TOKEN", "")

	token, err := sessionToken()
	if err != nil {
		t.Fatalf("sessionToken: %v", err)
	}

	// 32 bytes in base64url without padding is 43 characters. Asserting the
	// length is asserting the entropy: this token is the only thing standing
	// between a web page and every open repository.
	if len(token) != 43 {
		t.Errorf("generated token is %d characters, expected 43 (256 bits): %q", len(token), token)
	}

	// Twice, because a token that repeats is not a secret. A broken source of
	// randomness usually shows up as a constant, not as a subtle bias.
	other, err := sessionToken()
	if err != nil {
		t.Fatalf("sessionToken, second call: %v", err)
	}
	if token == other {
		t.Error("two generated tokens are identical")
	}
}

func TestSessionTokenAcceptsASuppliedToken(t *testing.T) {
	// `./do dev` mints one per development session and passes it in, so that
	// air restarting the daemon does not log the browser out.
	const supplied = "50GnzRHZOpt_P-9VNnJFaNFlemBy-4eytJaqqjlxRrw"
	t.Setenv("YAGIT_TOKEN", supplied)

	token, err := sessionToken()
	if err != nil {
		t.Fatalf("sessionToken: %v", err)
	}
	if token != supplied {
		t.Errorf("token = %q, expected the supplied %q", token, supplied)
	}
}

// TestSessionTokenRefusesAWeakOrUnusableToken covers the checks that keep a
// documented convenience from becoming a silent downgrade of the whole model.
func TestSessionTokenRefusesAWeakOrUnusableToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		says  string
	}{
		{
			name:  "too short to be a secret",
			token: "hunter2",
			says:  "characters",
		},
		{
			// 21 characters: one below the floor, which is the only boundary
			// worth testing on a threshold.
			name:  "one character below the floor",
			token: strings.Repeat("a", session.MinimumTokenLength-1),
			says:  "characters",
		},
		{
			// It travels in a URL and in a Set-Cookie header. A space ends the
			// cookie value, and the daemon would compare against a token the
			// browser never sends back.
			name:  "holds a space",
			token: "aaaaaaaaaaa aaaaaaaaaaaa",
			says:  "space or a control character",
		},
		{
			name:  "holds a newline",
			token: "aaaaaaaaaaa\naaaaaaaaaaaa",
			says:  "space or a control character",
		},
		{
			name:  "holds a tab",
			token: "aaaaaaaaaaa\taaaaaaaaaaaa",
			says:  "space or a control character",
		},
		{
			// Non-ASCII is percent-encoded in a URL and rejected outright in a
			// cookie value: the token that comes back is not the one sent.
			name:  "holds a non-ASCII rune",
			token: "aaaaaaaaaaa🌳aaaaaaaaaaaa",
			says:  "space or a control character",
		},
		{
			name:  "holds a DEL",
			token: "aaaaaaaaaaa\x7faaaaaaaaaaaa",
			says:  "space or a control character",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("YAGIT_TOKEN", testCase.token)

			token, err := sessionToken()
			if err == nil {
				t.Fatalf("accepted %q and returned %q", testCase.token, token)
			}
			// The message has to name what is wrong with it. "invalid token"
			// sends the reader to the source; this project does not do that.
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("error does not mention %q: %v", testCase.says, err)
			}
		})
	}
}

func TestSessionTokenAcceptsExactlyTheFloor(t *testing.T) {
	// The other side of the boundary above. A threshold tested from one side
	// only is a threshold that can be off by one forever.
	t.Setenv("YAGIT_TOKEN", strings.Repeat("a", session.MinimumTokenLength))

	if _, err := sessionToken(); err != nil {
		t.Errorf("a token of exactly the documented minimum was refused: %v", err)
	}
}

// --------------------------------------------------------------------------
// The token file
// --------------------------------------------------------------------------

func TestWriteTokenFileIsUnreadableByAnyoneElse(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(directory, "token")

	if err := writeTokenFile(path, "a-token"); err != nil {
		t.Fatalf("writeTokenFile: %v", err)
	}

	// This file grants full access to every open repository. protect.Check
	// asserts the lockdown on every platform — mode bits on Unix, a protected
	// owner-only ACL on Windows.
	if err := protect.Check(path); err != nil {
		t.Errorf("token file: %v", err)
	}

	// The trailing newline is not cosmetic: `./do token` pipes this into curl,
	// and every tool that reads it expects a line.
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(content) != "a-token\n" {
		t.Errorf("content = %q, expected %q", content, "a-token\n")
	}
}

func TestRemoveTokenFileToleratesAnAbsentFile(t *testing.T) {
	// Shutdown must not report a failure for a file that is already gone —
	// two shutdown paths can both reach here, and the second one finds nothing.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	removeTokenFile(filepath.Join(t.TempDir(), "never-written"), logger)
}

func TestRemoveTokenFileTakesTheTokenBackOffDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := writeTokenFile(path, "a-token"); err != nil {
		t.Fatalf("writeTokenFile: %v", err)
	}

	removeTokenFile(path, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the token outlived the daemon: stat = %v", err)
	}
}

// --------------------------------------------------------------------------
// Origins: the other half of the cookie model
// --------------------------------------------------------------------------

func TestBrowserOriginsCoversEverySpellingOfTheLoopback(t *testing.T) {
	origins := browserOrigins("http", "127.0.0.1", "127.0.0.1:7420")

	expected := []string{
		"http://127.0.0.1:7420",
		"http://localhost:7420",
		// Bracketed, because that is how a browser writes an IPv6 origin. An
		// unbracketed one matches nothing a browser will ever send.
		"http://[::1]:7420",
	}
	if !slices.Equal(origins, expected) {
		t.Errorf("origins = %v, expected %v", origins, expected)
	}
}

func TestBrowserOriginsAddsTheHostTheBrowserActuallyUses(t *testing.T) {
	// The machine running the daemon is not always the one displaying it.
	// Without this entry every cookie-authenticated mutating request from a
	// headless development box would be refused.
	origins := browserOrigins("http", "dev-box.local", "0.0.0.0:7420")

	if !slices.Contains(origins, "http://dev-box.local:7420") {
		t.Errorf("the public host is missing from %v", origins)
	}
	if len(origins) != 4 {
		t.Errorf("expected the three loopback spellings plus the public host, got %v", origins)
	}
}

func TestBrowserOriginsDoesNotRepeatTheLoopback(t *testing.T) {
	// The default public host IS the loopback. Listing it twice would not be
	// wrong, only sloppy — and a duplicate in an allowlist is how a reader
	// stops trusting the list.
	for _, host := range []string{"127.0.0.1", "localhost", "::1"} {
		origins := browserOrigins("http", host, "127.0.0.1:7420")
		if len(origins) != 3 {
			t.Errorf("public host %q produced %v, expected three entries", host, origins)
		}
	}
}

func TestBrowserOriginsRefusesToGuessWithoutAPort(t *testing.T) {
	// An origin is scheme + host + port. With no port there is nothing to
	// build, and inventing one would put an origin nobody listens on into the
	// allowlist.
	if origins := browserOrigins("http", "127.0.0.1", "not-an-address"); origins != nil {
		t.Errorf("origins = %v, expected none", origins)
	}
}

func TestBrowserOriginsFollowsTheSchemeBeingServed(t *testing.T) {
	// An origin is scheme + host + port, so https://127.0.0.1:7420 and
	// http://127.0.0.1:7420 are two different ones. A daemon serving HTTPS
	// that allowed the http spellings would be allowing origins nothing can
	// reach it on, and refusing the ones a browser will actually present:
	// every cookie-authenticated mutating request rejected, on the one
	// deployment where TLS is switched on.
	origins := browserOrigins("https", "127.0.0.1", "127.0.0.1:7420")

	expected := []string{
		"https://127.0.0.1:7420",
		"https://localhost:7420",
		"https://[::1]:7420",
	}
	if !slices.Equal(origins, expected) {
		t.Errorf("origins = %v, expected %v", origins, expected)
	}
}

// --------------------------------------------------------------------------
// The URL the user is told to open
// --------------------------------------------------------------------------

func TestBrowserURLAnnouncesAPortSomeoneCanReach(t *testing.T) {
	url := browserURL("http", "dev-box.local", "0.0.0.0:7420")

	if url != "http://dev-box.local:7420/" {
		t.Errorf("url = %q", url)
	}
}

func TestBrowserURLUsesThePortTheKernelPicked(t *testing.T) {
	url := browserURL("http", "127.0.0.1", "127.0.0.1:54321")

	if !strings.Contains(url, ":54321") {
		t.Errorf("url = %q, expected the bound port", url)
	}
}

func TestBrowserURLBracketsALiteralIPv6Host(t *testing.T) {
	url := browserURL("http", "::1", "127.0.0.1:7420")

	if url != "http://[::1]:7420/" {
		t.Errorf("url = %q, expected the host in brackets", url)
	}
}

func TestBrowserURLFallsBackToTheDefaultHost(t *testing.T) {
	url := browserURL("http", "", "127.0.0.1:7420")

	if !strings.Contains(url, defaultPublicHost) {
		t.Errorf("url = %q, expected the default public host", url)
	}
}

func TestBrowserURLUsesHTTPSSchemeWhenAsked(t *testing.T) {
	url := browserURL("https", "127.0.0.1", "127.0.0.1:7420")
	if url != "https://127.0.0.1:7420/" {
		t.Errorf("url = %q", url)
	}
}

// --------------------------------------------------------------------------
// Configuration
// --------------------------------------------------------------------------

func TestSplitOrigins(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		expected []string
	}{
		{"empty", "", nil},
		{"blank", "   ", nil},
		{"one", "http://a", []string{"http://a"}},
		{"several, spaced", " http://a , http://b ", []string{"http://a", "http://b"}},
		// A trailing comma is what a hand-edited .env looks like. Producing an
		// empty origin from it would put "" in an allowlist, and an empty
		// string is what a request with no Origin header presents.
		{"trailing comma", "http://a,", []string{"http://a"}},
		{"only commas", ",,,", nil},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := splitOrigins(testCase.value); !slices.Equal(got, testCase.expected) {
				t.Errorf("splitOrigins(%q) = %v, expected %v", testCase.value, got, testCase.expected)
			}
		})
	}
}

func TestEnvironmentOrTreatsAnEmptyValueAsAbsent(t *testing.T) {
	// `FOO=` in a .env is how someone writes "I have not set this", not "I
	// want the empty string" — and the empty string here would be a listen
	// address of "".
	t.Setenv("YAGIT_TEST_KEY", "")
	if got := environmentOr("YAGIT_TEST_KEY", "fallback"); got != "fallback" {
		t.Errorf("environmentOr with an empty value = %q, expected the fallback", got)
	}

	t.Setenv("YAGIT_TEST_KEY", "set")
	if got := environmentOr("YAGIT_TEST_KEY", "fallback"); got != "set" {
		t.Errorf("environmentOr = %q, expected the environment's value", got)
	}
}

// TestParseConfigurationRequiresARoot covers the one configuration error that
// is a security property rather than a convenience: with no root there is no
// boundary, and the daemon must refuse to start rather than pick something.
func TestParseConfigurationRequiresARoot(t *testing.T) {
	withCleanFlags(t, []string{"yagit"})
	t.Setenv("YAGIT_ROOT", "")

	if _, err := parseConfiguration(); err == nil {
		t.Fatal("a daemon with no allowed root must not start")
	} else if !strings.Contains(err.Error(), "YAGIT_ROOT") {
		t.Errorf("the error must name what to set, got: %v", err)
	}
}

func TestParseConfigurationReadsTheEnvironment(t *testing.T) {
	withCleanFlags(t, []string{"yagit"})
	t.Setenv("YAGIT_ROOT", "/srv/repositories")
	t.Setenv("YAGIT_ADDR", "0.0.0.0:9999")
	t.Setenv("YAGIT_LISTEN_ALL", "1")
	t.Setenv("YAGIT_ALLOW_ORIGINS", "http://a, http://b")

	config, err := parseConfiguration()
	if err != nil {
		t.Fatalf("parseConfiguration: %v", err)
	}
	if config.root != "/srv/repositories" {
		t.Errorf("root = %q", config.root)
	}
	if config.addr != "0.0.0.0:9999" {
		t.Errorf("addr = %q", config.addr)
	}
	if !slices.Equal(config.allowedOrigins, []string{"http://a", "http://b"}) {
		t.Errorf("allowedOrigins = %v", config.allowedOrigins)
	}
}

func TestParseConfigurationRequiresListenAllForNonLoopbackBind(t *testing.T) {
	withCleanFlags(t, []string{"yagit", "-root", "/srv", "-addr", "0.0.0.0:7420"})
	t.Setenv("YAGIT_LISTEN_ALL", "")

	if _, err := parseConfiguration(); err == nil {
		t.Fatal("binding to 0.0.0.0 without -listen-all must not start")
	}
}

func TestParseConfigurationPrefersTheFlagOverTheEnvironment(t *testing.T) {
	// A flag is typed deliberately, an environment variable is inherited. When
	// they disagree the deliberate one wins, or `-root` would be a lie.
	withCleanFlags(t, []string{"yagit", "-root", "/from/the/flag"})
	t.Setenv("YAGIT_ROOT", "/from/the/environment")

	config, err := parseConfiguration()
	if err != nil {
		t.Fatalf("parseConfiguration: %v", err)
	}
	if config.root != "/from/the/flag" {
		t.Errorf("root = %q, expected the flag to win", config.root)
	}
}

func TestParseConfigurationDefaultsToTheLoopback(t *testing.T) {
	withCleanFlags(t, []string{"yagit"})
	t.Setenv("YAGIT_ROOT", "/srv")
	t.Setenv("YAGIT_ADDR", "")

	config, err := parseConfiguration()
	if err != nil {
		t.Fatalf("parseConfiguration: %v", err)
	}
	// The default that matters most in this file: a binary run by hand listens
	// where only this machine can reach it.
	if config.addr != defaultAddr {
		t.Errorf("addr = %q, expected %q", config.addr, defaultAddr)
	}
	if !strings.HasPrefix(config.addr, "127.0.0.1:") {
		t.Errorf("the default listen address is not on the loopback: %q", config.addr)
	}
}

// TestParseConfigurationRequiresBothTLSFiles covers the other configuration
// rule that is a security property rather than a convenience. Half a TLS setup
// is not a lesser TLS setup: a certificate with no key serves no HTTPS at all,
// and a daemon that accepted one would fall back to plain HTTP while whoever
// configured it believed otherwise — session cookie included, since its Secure
// attribute follows the scheme and not the intent.
func TestParseConfigurationRequiresBothTLSFiles(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{"certificate without a key", []string{"yagit", "-root", "/srv", "-tls-cert", "/tmp/cert.pem"}},
		{"key without a certificate", []string{"yagit", "-root", "/srv", "-tls-key", "/tmp/key.pem"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			withCleanFlags(t, testCase.arguments)
			// Both variables, not just the missing half: the environment
			// supplies a file as readily as the command line does, and a
			// machine with YAGIT_TLS_KEY exported would make this pass for
			// the wrong reason.
			t.Setenv("YAGIT_TLS_CERT", "")
			t.Setenv("YAGIT_TLS_KEY", "")

			_, err := parseConfiguration()
			if err == nil {
				t.Fatal("half a TLS configuration must not start")
			}
			// Both names, because the reader holds one of them and needs the
			// other.
			for _, needed := range []string{"-tls-cert", "-tls-key"} {
				if !strings.Contains(err.Error(), needed) {
					t.Errorf("the error does not name %s: %v", needed, err)
				}
			}
		})
	}
}

func TestParseConfigurationAcceptsACompletePair(t *testing.T) {
	// The other side of the rule above. A check that refused both halves too
	// would pass every case in it and leave the daemon on plain HTTP forever,
	// which is the failure nobody notices.
	withCleanFlags(t, []string{
		"yagit", "-root", "/srv",
		"-tls-cert", "/etc/yagit/cert.pem",
		"-tls-key", "/etc/yagit/key.pem",
	})

	config, err := parseConfiguration()
	if err != nil {
		t.Fatalf("parseConfiguration: %v", err)
	}
	if config.tlsCert != "/etc/yagit/cert.pem" {
		t.Errorf("tlsCert = %q", config.tlsCert)
	}
	if config.tlsKey != "/etc/yagit/key.pem" {
		t.Errorf("tlsKey = %q", config.tlsKey)
	}
}

// withCleanFlags gives one test its own command line.
//
// parseConfiguration registers flags on the global flag.CommandLine, which can
// only be done once per set; calling it twice in one process panics on the
// redefinition. A fresh set per test is what makes these independent, and
// restoring both globals is what keeps them from leaking into the next one.
func withCleanFlags(t *testing.T, arguments []string) {
	t.Helper()

	previousFlags, previousArgs := flag.CommandLine, os.Args
	t.Cleanup(func() { flag.CommandLine, os.Args = previousFlags, previousArgs })

	flag.CommandLine = flag.NewFlagSet(arguments[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = arguments
}
