package main

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/session"
)

// The case for rewriting this program in Go was that it could then be tested.
// do_test.go took the two pieces where being wrong is silent; this file takes
// the rest of what is worth taking.
//
// Not everything is. runLint and runBuild are sequences of `run()` calls, and
// a test for them would assert that a list of strings is still the same list
// of strings — it would break on every legitimate edit and catch nothing. What
// is here instead is the logic underneath: what .env means, what environment
// the daemon is handed, which files end up readable by whom, and what happens
// to the ports when a command is interrupted.

// newProject builds a checkout in a temporary directory. go.mod is what
// locateProject looks for, so its presence is what makes this a project at all.
func newProject(t *testing.T) *project {
	t.Helper()

	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module test\n"), 0o600); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
	return &project{directory: directory}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// loadConfiguration — the daemon's security boundary comes out of here
// ---------------------------------------------------------------------------

func TestLoadConfigurationReadsTheRootAndTheHost(t *testing.T) {
	p := newProject(t)
	root := t.TempDir()
	writeFile(t, p.path(".env"), "YAGIT_ROOT="+root+"\nYAGIT_PUBLIC_HOST=dev-box.local\n")

	config, err := p.loadConfiguration()
	if err != nil {
		t.Fatalf("loadConfiguration: %v", err)
	}
	if config.root != root {
		t.Errorf("root = %q, want %q", config.root, root)
	}
	if config.publicHost != "dev-box.local" {
		t.Errorf("publicHost = %q", config.publicHost)
	}
}

func TestLoadConfigurationDefaultsTheHostToTheLoopback(t *testing.T) {
	p := newProject(t)
	writeFile(t, p.path(".env"), "YAGIT_ROOT="+t.TempDir()+"\n")

	config, err := p.loadConfiguration()
	if err != nil {
		t.Fatalf("loadConfiguration: %v", err)
	}

	// This default is the one that keeps the daemon on the loopback, since the
	// listen address is derived from it. An empty host would widen it.
	if config.publicHost != "127.0.0.1" {
		t.Errorf("publicHost = %q, want the loopback", config.publicHost)
	}
	addr, err := listenAddress(config)
	if err != nil || addr != "127.0.0.1:7420" {
		t.Errorf("a defaulted host produced listen address %q (%v)", addr, err)
	}
	if config.publicURL != "" {
		t.Errorf("publicURL = %q from a .env that names none", config.publicURL)
	}
}

// TestLoadConfigurationReadsThePublicURLAsAnOrigin checks that the value comes
// out of .env in the one form both the card and the daemon's allowlist use —
// the form a browser writes in an Origin header, whatever was typed.
func TestLoadConfigurationReadsThePublicURLAsAnOrigin(t *testing.T) {
	p := newProject(t)
	writeFile(t, p.path(".env"),
		"YAGIT_ROOT="+t.TempDir()+"\nYAGIT_PUBLIC_URL=https://Yagit.devvm.orb.local:443/\n")

	config, err := p.loadConfiguration()
	if err != nil {
		t.Fatalf("loadConfiguration: %v", err)
	}
	if config.publicURL != "https://yagit.devvm.orb.local" {
		t.Errorf("publicURL = %q, want the origin a browser presents", config.publicURL)
	}
}

// A public URL no browser can present as an origin stops the command before
// anything starts, and says which line of .env and what is wrong with it.
func TestLoadConfigurationRefusesAPublicURLThatIsNotAnOrigin(t *testing.T) {
	p := newProject(t)
	writeFile(t, p.path(".env"),
		"YAGIT_ROOT="+t.TempDir()+"\nYAGIT_PUBLIC_URL=https://yagit.example.com/yagit\n")

	_, err := p.loadConfiguration()
	if err == nil {
		t.Fatal("a public URL with a path must stop the command")
	}
	for _, needed := range []string{"YAGIT_PUBLIC_URL", "https://yagit.example.com/yagit", "path"} {
		if !strings.Contains(err.Error(), needed) {
			t.Errorf("the refusal does not mention %q: %v", needed, err)
		}
	}
}

// TestLoadConfigurationRefusesAnUnusableRoot covers the two ways YAGIT_ROOT
// can be wrong. Both have to stop the command: the root is the directory the
// daemon may open repositories under, and carrying on with a bad one means
// either no boundary or a boundary somewhere unintended.
func TestLoadConfigurationRefusesAnUnusableRoot(t *testing.T) {
	cases := []struct {
		name string
		env  string
		says string
	}{
		{"absent", "YAGIT_PUBLIC_HOST=localhost\n", "empty"},
		{"empty", "YAGIT_ROOT=\n", "empty"},
		{"not a directory that exists", "YAGIT_ROOT=/does/not/exist\n", "does not exist"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			p := newProject(t)
			writeFile(t, p.path(".env"), testCase.env)

			_, err := p.loadConfiguration()
			if err == nil {
				t.Fatal("an unusable root must stop the command")
			}
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the error must say what is wrong, got: %v", err)
			}
		})
	}
}

func TestLoadConfigurationRefusesARootThatIsAFile(t *testing.T) {
	p := newProject(t)
	notADirectory := filepath.Join(t.TempDir(), "file")
	writeFile(t, notADirectory, "")
	writeFile(t, p.path(".env"), "YAGIT_ROOT="+notADirectory+"\n")

	// os.Stat succeeds on a file, so only the IsDir check catches this. Without
	// it the daemon would start with a root it can never resolve anything
	// under, and say nothing until the first repository fails to open.
	if _, err := p.loadConfiguration(); err == nil {
		t.Fatal("a root that is a file must stop the command")
	}
}

// ---------------------------------------------------------------------------
// .env creation
// ---------------------------------------------------------------------------

func TestLoadConfigurationCreatesEnvFromTheExample(t *testing.T) {
	p := newProject(t)
	root := t.TempDir()
	writeFile(t, p.path(".env.example"), "YAGIT_ROOT=__HOME__\n")

	t.Setenv("HOME", root)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", root)
	}

	config, err := p.loadConfiguration()
	if err != nil {
		t.Fatalf("loadConfiguration on a fresh checkout: %v", err)
	}
	if config.root != root {
		t.Errorf("root = %q, want the home directory %q substituted for __HOME__", config.root, root)
	}

	// .env holds a path to everything the daemon may touch. It is not a secret
	// the way the token is, but it is not the ambient umask's business either.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(p.path(".env"))
		if err != nil {
			t.Fatalf("stat .env: %v", err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf(".env mode is %04o, want 0600", mode)
		}
	}
}

func TestLoadConfigurationSaysSoWhenTheExampleIsMissing(t *testing.T) {
	p := newProject(t)

	// A checkout with neither .env nor .env.example is broken, and the message
	// has to name the file rather than reporting a bare "no such file".
	_, err := p.loadConfiguration()
	if err == nil {
		t.Fatal("a checkout with no .env.example must stop the command")
	}
	if !strings.Contains(err.Error(), ".env.example") {
		t.Errorf("the error must name the missing file, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// The environment the daemon is started with
// ---------------------------------------------------------------------------

func environmentValue(environment []string, key string) (string, bool) {
	for _, entry := range slices.Backward(environment) {
		if rest, found := strings.CutPrefix(entry, key+"="); found {
			return rest, true
		}
	}
	return "", false
}

func TestDaemonEnvironmentCarriesWhatTheDaemonNeeds(t *testing.T) {
	p := newProject(t)
	config := configuration{root: "/srv/repositories", publicHost: "127.0.0.1"}

	environment, err := p.daemonEnvironment(config, "a-token")
	if err != nil {
		t.Fatalf("daemonEnvironment: %v", err)
	}

	for key, want := range map[string]string{
		"YAGIT_ROOT":        "/srv/repositories",
		"YAGIT_ADDR":        "127.0.0.1:7420",
		"YAGIT_PUBLIC_HOST": "127.0.0.1",
		"YAGIT_TOKEN":       "a-token",
	} {
		got, found := environmentValue(environment, key)
		if !found {
			t.Errorf("%s is missing from the daemon's environment", key)
			continue
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}

	// No token file. The daemon's own copy was how `./do token` decided a
	// daemon was up, and it outlived every daemon killed outright; the port is
	// asked now, and the checkout's file is the only one there is.
	if value, found := environmentValue(environment, "YAGIT_TOKEN_FILE"); found {
		t.Errorf("YAGIT_TOKEN_FILE = %q; the daemon must write no token file under ./do", value)
	}

	// The daemon proxies to the port Vite is told to take. Two numbers that
	// disagree are a 502 on every page with nothing pointing at the cause.
	vite, _ := environmentValue(environment, "YAGIT_VITE_PORT")
	devServer, _ := environmentValue(environment, "YAGIT_FRONTEND_DEV_SERVER")
	if vite == "" || devServer != "http://127.0.0.1:"+vite {
		t.Errorf("the daemon proxies to %q and Vite listens on port %q", devServer, vite)
	}

	// Nothing reads it any more: Vite's hot-reload client follows the page's
	// own port, and handing it the daemon's is what broke it behind a proxy.
	if value, found := environmentValue(environment, "YAGIT_DAEMON_PORT"); found {
		t.Errorf("YAGIT_DAEMON_PORT = %q; nothing is meant to pin the hot-reload port", value)
	}
}

// TestDaemonEnvironmentHandsThePublicURLDown covers the reverse proxy: the
// daemon stays on the loopback, where the proxy connects, and is told the
// public URL — which it accepts as an origin and announces.
func TestDaemonEnvironmentHandsThePublicURLDown(t *testing.T) {
	p := newProject(t)
	environment, err := p.daemonEnvironment(configuration{
		root: "/srv", publicHost: "127.0.0.1", publicURL: "https://yagit.devvm.orb.local",
	}, "t")
	if err != nil {
		t.Fatalf("daemonEnvironment: %v", err)
	}

	for key, want := range map[string]string{
		"YAGIT_ADDR":       "127.0.0.1:7420",
		"YAGIT_PUBLIC_URL": "https://yagit.devvm.orb.local",
	} {
		if got, _ := environmentValue(environment, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if value, found := environmentValue(environment, "YAGIT_LISTEN_ALL"); found {
		t.Errorf("YAGIT_LISTEN_ALL = %q behind a proxy, which widens nothing", value)
	}
}

// TestDaemonEnvironmentWidensOnlyForARemoteHost is the security-relevant half.
//
// The listen address is derived from the public host so the two cannot
// contradict each other. A regression here binds 0.0.0.0 on a machine whose
// owner asked for nothing of the sort, and nothing in the output would say so.
func TestDaemonEnvironmentWidensOnlyForARemoteHost(t *testing.T) {
	p := newProject(t)

	for host, want := range map[string]string{
		"127.0.0.1": "127.0.0.1:7420",
		"localhost": "127.0.0.1:7420",
		"::1":       "127.0.0.1:7420",
	} {
		environment, err := p.daemonEnvironment(configuration{root: "/srv", publicHost: host}, "t")
		if err != nil {
			t.Fatalf("public host %q: %v", host, err)
		}
		got, _ := environmentValue(environment, "YAGIT_ADDR")
		if got != want {
			t.Errorf("public host %q gave listen address %q, want %q", host, got, want)
		}
	}

	environment, err := p.daemonEnvironment(configuration{
		root: "/srv", publicHost: "dev-box.local", listenAll: true,
	}, "t")
	if err != nil {
		t.Fatalf("remote host with listen-all: %v", err)
	}
	got, _ := environmentValue(environment, "YAGIT_ADDR")
	if got != "0.0.0.0:7420" {
		t.Errorf("remote host with listen-all gave %q, want 0.0.0.0:7420", got)
	}
	if got, _ := environmentValue(environment, "YAGIT_LISTEN_ALL"); got != "1" {
		t.Errorf("YAGIT_LISTEN_ALL = %q, want 1", got)
	}
}

// A public host named without YAGIT_LISTEN_ALL is a .env somebody wrote by
// hand, and the answer to it has to be a sentence they can act on. It used to
// be a panic: `./do test e2e` on a machine configured for remote browsing
// printed a goroutine dump and no advice.
func TestDaemonEnvironmentRefusesARemoteHostWithoutAcknowledgment(t *testing.T) {
	p := newProject(t)

	_, err := p.daemonEnvironment(configuration{root: "/srv", publicHost: "dev-box.local"}, "t")
	if err == nil {
		t.Fatal("a non-loopback public host without listen-all should be refused")
	}
	if !strings.Contains(err.Error(), "YAGIT_LISTEN_ALL") {
		t.Errorf("error = %q, want it to name the variable that fixes it", err)
	}

	// The sentence has to offer the loopback as well. It is the announcement
	// this change reaches an existing .env by (ADR 0014), and one that only
	// ever advises listening on all interfaces argues for the exposure it was
	// added to stop.
	if !strings.Contains(err.Error(), "comment YAGIT_PUBLIC_HOST out") {
		t.Errorf("error = %q, want it to offer staying on the loopback too", err)
	}
}

// TestDaemonEnvironmentOverridesTheAmbientOne guards the append order. The
// daemon's own variables are appended to os.Environ(), and the last assignment
// is the one exec passes on — so a YAGIT_ROOT already exported in the shell
// must not win over the one .env just produced.
func TestDaemonEnvironmentOverridesTheAmbientOne(t *testing.T) {
	t.Setenv("YAGIT_ROOT", "/from/the/ambient/shell")
	t.Setenv("YAGIT_ADDR", "0.0.0.0:9999")
	t.Setenv("YAGIT_PUBLIC_URL", "https://from.the.ambient.shell")
	t.Setenv("YAGIT_ALLOW_ORIGINS", "https://tool.example.com")

	p := newProject(t)
	environment, err := p.daemonEnvironment(configuration{root: "/from/dotenv", publicHost: "127.0.0.1"}, "t")
	if err != nil {
		t.Fatalf("daemonEnvironment: %v", err)
	}

	if got, _ := environmentValue(environment, "YAGIT_ROOT"); got != "/from/dotenv" {
		t.Errorf("YAGIT_ROOT = %q, want .env to win over the shell", got)
	}
	if got, _ := environmentValue(environment, "YAGIT_ADDR"); got != "127.0.0.1:7420" {
		t.Errorf("YAGIT_ADDR = %q, want the derived address to win over the shell", got)
	}

	// Present and empty, not absent: absent, the shell's would reach the
	// daemon, which would accept an origin and announce an address that .env
	// never named and the card never prints.
	if got, found := environmentValue(environment, "YAGIT_PUBLIC_URL"); !found || got != "" {
		t.Errorf("YAGIT_PUBLIC_URL = %q (set: %v), want it cleared when .env names none", got, found)
	}

	// The daemon's own flag, which .env has no key for: `./do` leaves it as
	// the shell has it rather than deciding it away.
	if got, _ := environmentValue(environment, "YAGIT_ALLOW_ORIGINS"); got != "https://tool.example.com" {
		t.Errorf("YAGIT_ALLOW_ORIGINS = %q, want the shell's left alone", got)
	}
}

// ---------------------------------------------------------------------------
// The files that hold the token
// ---------------------------------------------------------------------------

func TestStateDirectoryIsUnreadableByAnyoneElse(t *testing.T) {
	p := newProject(t)
	if err := p.ensureStateDirectory(); err != nil {
		t.Fatalf("ensureStateDirectory: %v", err)
	}

	// It holds the session token and the throwaway stack's log, which captures
	// the startup line carrying that token in clear. protect.Check is the
	// platform's own record — mode bits on Unix, a protected owner-only ACL
	// on Windows.
	if err := protect.Check(p.path(stateDirectory)); err != nil {
		t.Errorf("%s: %v", stateDirectory, err)
	}
}

func TestStateDirectoryIsLockedDownEvenIfItExisted(t *testing.T) {
	p := newProject(t)
	// A directory left behind world-readable by an older version, or by a
	// permissive umask, must be corrected rather than accepted. MkdirAll alone
	// would leave it as it found it. On Windows the same call replaces a
	// permissive inherited ACL.
	if err := os.MkdirAll(p.path(stateDirectory), 0o777); err != nil {
		t.Fatalf("creating a loose state directory: %v", err)
	}

	if err := p.ensureStateDirectory(); err != nil {
		t.Fatalf("ensureStateDirectory: %v", err)
	}

	if err := protect.Check(p.path(stateDirectory)); err != nil {
		t.Errorf("an existing loose directory was left open: %v", err)
	}
}

func TestStackLogIsUnreadableAndStartsEmpty(t *testing.T) {
	p := newProject(t)

	log, err := p.openStackLog()
	if err != nil {
		t.Fatalf("openStackLog: %v", err)
	}
	if _, err := log.WriteString("Open yagit: http://127.0.0.1:7420/\n"); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	if err := protect.Check(log.Name()); err != nil {
		t.Errorf("the stack log: %v — it carries the daemon's whole output", err)
	}

	// Truncated on reopen: yesterday's run has no business surviving into
	// today's, and a log that only grows is one nobody reads.
	reopened, err := p.openStackLog()
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}

	contents, err := os.ReadFile(log.Name())
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(contents) != 0 {
		t.Errorf("the log kept %d bytes from the previous run", len(contents))
	}
}

func TestStoredSessionTokenDropsTheTrailingNewline(t *testing.T) {
	p := newProject(t)
	writeFile(t, p.path(sessionTokenFileName), "a-token\n")

	// The newline is the file's; it is not part of the token. A header built
	// from the raw bytes authenticates nothing, and the daemon's only answer
	// is "invalid token" — which sends the reader looking in the wrong place.
	token, err := p.storedSessionToken()
	if err != nil {
		t.Fatalf("storedSessionToken: %v", err)
	}
	if token != "a-token" {
		t.Errorf("token = %q, want it trimmed", token)
	}
}

func TestStoredSessionTokenIsEmptyWhenThereIsNone(t *testing.T) {
	p := newProject(t)

	// Empty and not an error: a checkout that has never started a stack is
	// the ordinary case, and the commands that only ask — status, token — must
	// be able to tell it from a file they cannot read.
	token, err := p.storedSessionToken()
	if err != nil {
		t.Fatalf("storedSessionToken: %v", err)
	}
	if token != "" {
		t.Errorf("token = %q, want nothing", token)
	}
	if _, err := os.Stat(p.path(sessionTokenFileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("asking minted a file: stat = %v", err)
	}
}

// ---------------------------------------------------------------------------
// The checkout's own token, which outlives every stack started from it
// ---------------------------------------------------------------------------

func TestSessionTokenIsReusedAcrossRuns(t *testing.T) {
	p := newProject(t)

	// This is the whole point of the file: `./do down` followed by `./do up`
	// used to hand the daemon a different secret, which logged out a browser
	// tab that had never closed.
	first, err := p.sessionToken()
	if err != nil {
		t.Fatalf("sessionToken: %v", err)
	}
	if !session.Minted(first) {
		t.Fatalf("minted %q, which the daemon would refuse", first)
	}

	second, err := p.sessionToken()
	if err != nil {
		t.Fatalf("sessionToken, second run: %v", err)
	}
	if second != first {
		t.Errorf("a second run got %q, want the stored %q", second, first)
	}
}

func TestRenewSessionTokenReplacesTheStoredOne(t *testing.T) {
	p := newProject(t)

	first, err := p.sessionToken()
	if err != nil {
		t.Fatalf("sessionToken: %v", err)
	}

	renewed, err := p.renewSessionToken()
	if err != nil {
		t.Fatalf("renewSessionToken: %v", err)
	}
	if renewed == first {
		t.Fatal("--new-token produced the token it was asked to replace")
	}

	// Replaced on disk, not just returned: the next `./do up` reads the file.
	stored, err := p.sessionToken()
	if err != nil {
		t.Fatalf("sessionToken after renewal: %v", err)
	}
	if stored != renewed {
		t.Errorf("stored %q, want the renewed %q", stored, renewed)
	}
}

func TestUnusableSessionTokenIsReplacedRatherThanForwarded(t *testing.T) {
	minted, err := session.Mint()
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		// Edited by hand. Handing it to the daemon fails the start with a
		// message naming YAGIT_TOKEN — a variable whoever reads it never set.
		"a password": "hunter2\n",
		// Truncated by a crash.
		"a truncated token": minted[:30] + "\n",
		// Wrapped by an editor. The base64 decoder skips the newline, so the
		// bytes come out right and a length check alone would forward it — to
		// a daemon that refuses it on every start, and never replace it.
		"a token wrapped onto two lines": minted[:40] + "\n" + minted[40:] + "\n",
		"a padded token":                 minted + "=\n",
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			writeFile(t, p.path(sessionTokenFileName), contents)

			token, err := p.sessionToken()
			if err != nil {
				t.Fatalf("sessionToken: %v", err)
			}
			if token == strings.TrimSpace(contents) {
				t.Fatal("a token the daemon refuses was forwarded to it anyway")
			}
			if !session.Minted(token) {
				t.Errorf("replacement %q is not usable either", token)
			}
		})
	}
}

func TestSessionTokenFileIsPrivate(t *testing.T) {
	p := newProject(t)
	if _, err := p.sessionToken(); err != nil {
		t.Fatalf("sessionToken: %v", err)
	}

	// It grants full access to every repository the daemon opens, and it is
	// still there tomorrow. protect.Check asserts the lockdown on every
	// platform — mode bits on Unix, a protected owner-only ACL on Windows.
	if err := protect.Check(p.path(sessionTokenFileName)); err != nil {
		t.Errorf("%s: %v", sessionTokenFileName, err)
	}
	if err := protect.Check(p.path(stateDirectory)); err != nil {
		t.Errorf("%s: %v", stateDirectory, err)
	}
}

func TestSessionTokenRefusesASymbolicLink(t *testing.T) {
	p := newProject(t)
	if err := p.ensureStateDirectory(); err != nil {
		t.Fatal(err)
	}

	// The obvious way to share one token between two checkouts, since ADR
	// 0026 refuses to store it outside the checkout. A dangling link reads as
	// "no file", and a write through it would mint the secret at the target
	// — outside the 0700 directory, in whatever mode that directory's umask
	// gives.
	target := filepath.Join(t.TempDir(), "shared-token")
	if err := os.Symlink(target, p.path(sessionTokenFileName)); err != nil {
		t.Skipf("cannot create a symbolic link here: %v", err)
	}

	_, err := p.sessionToken()
	if err == nil {
		t.Fatal("a symbolic link in place of the token file was followed")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("the error must say what it found, got: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the token was minted at the link's target: stat = %v", err)
	}
}

func TestSessionTokenRefusesADirectory(t *testing.T) {
	p := newProject(t)
	if err := os.MkdirAll(p.path(sessionTokenFileName), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := p.sessionToken()
	if err == nil {
		t.Fatal("a directory in place of the token file was accepted")
	}
	// Named, and with a remedy: "is a directory" from the operating system
	// says neither which file nor what to do about it.
	if !strings.Contains(err.Error(), "a directory") || !strings.Contains(err.Error(), sessionTokenFileName) {
		t.Errorf("the error must name the file and what it is, got: %v", err)
	}
}

func TestReplaceSessionTokenMintsTheFirstOneToo(t *testing.T) {
	p := newProject(t)

	// `./do up --new-token` on a fresh checkout: nothing to replace, and still
	// a token to start on.
	if err := p.replaceSessionToken(); err != nil {
		t.Fatalf("replaceSessionToken: %v", err)
	}
	first, err := p.storedSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if !session.Minted(first) {
		t.Fatalf("stored %q after --new-token on a fresh checkout", first)
	}

	if err := p.replaceSessionToken(); err != nil {
		t.Fatalf("replaceSessionToken again: %v", err)
	}
	second, err := p.storedSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Error("--new-token kept the token it was asked to replace")
	}
}

func TestWriteStateFileLeavesOnlyTheFile(t *testing.T) {
	p := newProject(t)
	name := stateDirectory + "/probe"

	// Written through a temporary name and renamed, so a reader never sees a
	// half-written file. The temporary name must not survive either: a
	// directory full of them is how the next person learns about the rename.
	for _, content := range []string{"first\n", "second\n"} {
		if err := p.writeStateFile(name, []byte(content)); err != nil {
			t.Fatalf("writeStateFile: %v", err)
		}
		if got := readFileString(t, p.path(name)); got != content {
			t.Errorf("read back %q, want %q", got, content)
		}
	}

	entries, err := os.ReadDir(p.path(stateDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "probe" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("%s holds %v, want only the file written", stateDirectory, names)
	}
}

// ---------------------------------------------------------------------------
// Probing the stack
// ---------------------------------------------------------------------------

// useDaemonAt points this program's probes at a test server for the length of
// one test. The tests that use it cannot run in parallel with each other.
func useDaemonAt(t *testing.T, url string) {
	t.Helper()
	previous := daemonURL
	daemonURL = url
	t.Cleanup(func() { daemonURL = previous })
}

// useNoDaemon points the probes at a port nothing listens on, so a test about
// "nothing is running" does not depend on whether a stack happens to be up on
// the machine running the suite.
func useNoDaemon(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	useDaemonAt(t, server.URL)
}

// fakeStack answers the way the daemon does for a given token, with the
// frontend either up or not: the two answers a probe has to tell apart.
func fakeStack(token string, frontendUp bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Yagit-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","version":"test","repos":0}`))
		case frontendUp:
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusBadGateway) // Vite not up yet
		}
	})
}

func TestProbeStackTellsTheAnswersApart(t *testing.T) {
	cases := []struct {
		name    string
		handler http.Handler
		want    stackAnswer
	}{
		{"the whole stack", fakeStack("a-token", true), stackAnswers},
		// The daemon alone is not the stack: an end-to-end test that opens
		// the page one second too early gets a 502 unrelated to what it was
		// checking.
		{"the daemon with Vite still building", fakeStack("a-token", false), daemonAnswers},
		{"a daemon on another token", fakeStack("another-token", true), tokenRefused},
		{"a rate-limited refusal", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}), tokenRefused},
		// A server that says 200 to everything must not be taken for a stack:
		// the end-to-end suite would run against it.
		{"a server that is not yagit", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>hello</html>"))
		}), strangerAnswers},
		{"a server with no such route", http.NotFoundHandler(), strangerAnswers},
		// The fingerprint without a credential: a squatter that forges the
		// health JSON would otherwise receive the checkout's token on the
		// next request of every `./do status` and `./do token`.
		{"a forged health response", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","version":"forged","repos":0}`))
		}), strangerAnswers},
		{"health without a version", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Yagit-Token") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}), strangerAnswers},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(testCase.handler)
			defer server.Close()
			useDaemonAt(t, server.URL)

			p := newProject(t)
			writeFile(t, p.path(sessionTokenFileName), "a-token\n")

			if got := p.probeStack(); got != testCase.want {
				t.Errorf("probeStack() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// A listener that answers the health JSON to anyone must never see the
// checkout's token: the challenge is what keeps it off the wire.
func TestProbeStackDoesNotHandTheTokenToAForgedHealth(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("X-Yagit-Token"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"forged","repos":0}`))
	}))
	defer server.Close()
	useDaemonAt(t, server.URL)

	p := newProject(t)
	writeFile(t, p.path(sessionTokenFileName), "secret-token\n")

	if got := p.probeStack(); got != strangerAnswers {
		t.Fatalf("probeStack() = %v, want %v", got, strangerAnswers)
	}
	for _, token := range seen {
		if token == "secret-token" {
			t.Fatal("the stored token was sent to a listener that never refused an unauthenticated probe")
		}
	}
}

// Go's default client forwards custom headers on a cross-host redirect. A
// listener that refuses the challenge and then redirects must not bounce the
// token off the loopback.
func TestProbeStackDoesNotFollowRedirects(t *testing.T) {
	var leaked bool
	sink := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Yagit-Token") != "" {
			leaked = true
		}
	}))
	defer sink.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Yagit-Token") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		http.Redirect(w, r, sink.URL, http.StatusFound)
	}))
	defer server.Close()
	useDaemonAt(t, server.URL)

	p := newProject(t)
	writeFile(t, p.path(sessionTokenFileName), "secret-token\n")

	if got := p.probeStack(); got != strangerAnswers {
		t.Errorf("probeStack() = %v, want %v", got, strangerAnswers)
	}
	if leaked {
		t.Fatal("the probe followed a redirect and took the token with it")
	}
}

func TestProbeStackFindsNobodyOnAClosedPort(t *testing.T) {
	useNoDaemon(t)
	p := newProject(t)
	writeFile(t, p.path(sessionTokenFileName), "a-token\n")

	if got := p.probeStack(); got != nobodyAnswers {
		t.Errorf("probeStack() = %v on a closed port, want %v", got, nobodyAnswers)
	}
	if p.stackResponds() {
		t.Error("stackResponds must be false when nothing listens")
	}
}

func TestProbeStackWithoutATokenIsRefused(t *testing.T) {
	server := httptest.NewServer(fakeStack("a-token", true))
	defer server.Close()
	useDaemonAt(t, server.URL)

	// No token file means whatever answers is running on a token this
	// checkout does not hold. Treating it as our stack is how the end-to-end
	// tests would run against something else entirely.
	p := newProject(t)
	if got := p.probeStack(); got != tokenRefused {
		t.Errorf("probeStack() = %v with no token to present, want %v", got, tokenRefused)
	}
}

func TestRunningStackTokenNamesWhatIsWrong(t *testing.T) {
	cases := []struct {
		name    string
		handler http.Handler
		says    string
	}{
		{"nothing running", nil, "not running"},
		{"another token", fakeStack("another-token", true), "refuses the token"},
		{"the frontend not up", fakeStack("a-token", false), "frontend"},
		{"a stranger", http.NotFoundHandler(), "not a yagit daemon"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.handler == nil {
				useNoDaemon(t)
			} else {
				server := httptest.NewServer(testCase.handler)
				defer server.Close()
				useDaemonAt(t, server.URL)
			}

			p := newProject(t)
			writeFile(t, p.path(sessionTokenFileName), "a-token\n")

			token, err := p.runningStackToken()
			if err == nil {
				t.Fatalf("handed out %q for a stack that cannot use it", token)
			}
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("error = %q, want it to say %q", err, testCase.says)
			}
		})
	}
}

func TestRunningStackTokenIsTheStoredOneOnceTheStackAnswers(t *testing.T) {
	server := httptest.NewServer(fakeStack("a-token", true))
	defer server.Close()
	useDaemonAt(t, server.URL)

	p := newProject(t)
	writeFile(t, p.path(sessionTokenFileName), "a-token\n")

	token, err := p.runningStackToken()
	if err != nil {
		t.Fatalf("runningStackToken: %v", err)
	}
	if token != "a-token" {
		t.Errorf("token = %q, want the stored one", token)
	}
}

func TestWaitUntilAnswers(t *testing.T) {
	stack := &devStack{exits: make(chan error, 2)}
	if got := stack.waitUntil(func() bool { return true }, make(chan os.Signal), time.Second); got != stackReady {
		t.Errorf("waitUntil = %v, want %v", got, stackReady)
	}
}

func TestWaitUntilStopsWhenAProcessExits(t *testing.T) {
	// A port already held fails Vite within a second. Waiting out the whole
	// timeout with the reason already printed is the failure this guards.
	stack := &devStack{exits: make(chan error, 2)}
	stack.exits <- nil

	started := time.Now()
	got := stack.waitUntil(func() bool { return false }, make(chan os.Signal), time.Minute)
	if got != stackDied {
		t.Errorf("waitUntil = %v, want %v", got, stackDied)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("waited %s for a process that had already exited", elapsed)
	}
	if stack.collected != 1 {
		t.Errorf("collected %d exits, want the one read", stack.collected)
	}
}

func TestWaitUntilGivesUp(t *testing.T) {
	stack := &devStack{exits: make(chan error, 2)}

	started := time.Now()
	got := stack.waitUntil(func() bool { return false }, make(chan os.Signal), 300*time.Millisecond)
	if got != stackTimedOut {
		t.Errorf("waitUntil = %v, want %v", got, stackTimedOut)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("waited %s for a 300 ms deadline", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Build helpers
// ---------------------------------------------------------------------------

func TestEmptyEmbeddedAssetsSparesTheGitkeep(t *testing.T) {
	p := newProject(t)
	assets := p.path("internal", "assets", "dist")

	writeFile(t, filepath.Join(assets, ".gitkeep"), "")
	writeFile(t, filepath.Join(assets, "index.html"), "<html></html>")
	writeFile(t, filepath.Join(assets, "assets", "index-abc.js"), "console.log(1)")

	if err := p.emptyEmbeddedAssets(); err != nil {
		t.Fatalf("emptyEmbeddedAssets: %v", err)
	}

	// Without .gitkeep the assets package's //go:embed does not compile on a
	// fresh clone. Deleting it here would produce a build that works on this
	// machine and fails on everyone else's.
	if _, err := os.Stat(filepath.Join(assets, ".gitkeep")); err != nil {
		t.Errorf(".gitkeep did not survive: %v", err)
	}

	entries, err := os.ReadDir(assets)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the previous build survived: %v", names)
	}
}

// ---------------------------------------------------------------------------
// Frontend dependencies
// ---------------------------------------------------------------------------

func TestFrontendDependenciesAreCurrent(t *testing.T) {
	p := newProject(t)
	vite := p.nodeBinary("vite")
	lockfile := p.path("web", "package-lock.json")

	if p.frontendDependenciesAreCurrent() {
		t.Error("nothing installed must not count as current")
	}

	writeFile(t, vite, "")
	writeFile(t, lockfile, `{"one": true}`)
	if p.frontendDependenciesAreCurrent() {
		t.Error("an install nothing recorded must not count as current")
	}

	if err := p.recordStamp(frontendStamp); err != nil {
		t.Fatalf("recording the install: %v", err)
	}
	if !p.frontendDependenciesAreCurrent() {
		t.Error("an install recorded for this lockfile is current")
	}

	// A branch switch rewrites the lockfile's mtime and not one byte of it.
	// Reinstalling there deletes node_modules and spends minutes reproducing
	// it exactly — on every `git checkout`, for everyone.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(lockfile, future, future); err != nil {
		t.Fatalf("moving the lockfile's mtime: %v", err)
	}
	if !p.frontendDependenciesAreCurrent() {
		t.Error("a lockfile whose contents did not change is still installed")
	}

	// Dependencies that actually moved. Getting this backwards means a
	// checkout that silently builds against the previous dependency set.
	writeFile(t, lockfile, `{"two": true}`)
	if p.frontendDependenciesAreCurrent() {
		t.Error("a lockfile that changed must not count as installed")
	}

	// The stamp says installed, and node_modules is gone: an interrupted
	// `npm ci`, or somebody clearing the tree by hand.
	if err := p.recordStamp(frontendStamp); err != nil {
		t.Fatalf("recording the install: %v", err)
	}
	if err := os.Remove(vite); err != nil {
		t.Fatalf("removing the install: %v", err)
	}
	if p.frontendDependenciesAreCurrent() {
		t.Error("a stamp must not outlive the install it describes")
	}
}

func TestNodeBinaryNamesWhatNpmActuallyWrote(t *testing.T) {
	p := newProject(t)
	name := p.nodeBinary("playwright")

	// npm writes a .cmd wrapper on Windows and a symlink everywhere else.
	// Naming the extensionless one there looks exactly like a missing install.
	if runtime.GOOS == "windows" {
		if !strings.HasSuffix(name, "playwright.cmd") {
			t.Errorf("nodeBinary = %q, want the .cmd wrapper on Windows", name)
		}
		return
	}
	if !strings.HasSuffix(name, "playwright") || strings.HasSuffix(name, ".cmd") {
		t.Errorf("nodeBinary = %q", name)
	}
}

// ---------------------------------------------------------------------------
// Dispatch
// ---------------------------------------------------------------------------

func TestDispatchRefusesAnUnknownCommand(t *testing.T) {
	err := dispatch([]string{"deploy"})
	if err == nil {
		t.Fatal("an unknown command must be an error, not a silent success")
	}
	// The name typed, and where to find the real list. "unknown command" on
	// its own leaves the reader guessing which one.
	if !strings.Contains(err.Error(), "deploy") || !strings.Contains(err.Error(), "help") {
		t.Errorf("the error must quote the command and point at help, got: %v", err)
	}
}

func TestDispatchHelpNeedsNoProject(t *testing.T) {
	// help is answered before the checkout is located, so it works from
	// anywhere — including from a directory with no go.mod above it, which is
	// exactly where someone types it to find out what went wrong.
	t.Chdir(t.TempDir())

	for _, name := range []string{"help", "-h", "--help", ""} {
		args := []string{name}
		if name == "" {
			args = nil
		}
		if err := dispatch(args); err != nil {
			t.Errorf("dispatch(%q) = %v, want help to be printed", name, err)
		}
	}
}
