package main

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/publicurl"
	"github.com/ngsanogo/yagit/internal/session"
)

// ---------------------------------------------------------------------------
// Local configuration
// ---------------------------------------------------------------------------

// unquote strips one matching pair of quotes, and nothing more.
//
// A path with a space in it has to be quotable, and someone will quote a value
// that did not need it. This is a two-key file, not a shell: no escapes, no
// interpolation, no nesting.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if first == last && (first == '"' || first == '\'') {
		return value[1 : len(value)-1]
	}
	return value
}

// configuration is what .env says about this machine.
type configuration struct {
	// root is the directory under which the daemon may open repositories.
	root string

	// publicHost is the name a browser reaches the daemon by.
	//
	// It defaults to the loopback address, which is right when you develop and
	// browse on the same machine. Set YAGIT_PUBLIC_HOST in .env when they
	// differ — a headless development box reached over SSH, a VM, a remote
	// workstation.
	//
	// .env is machine-local and never committed, which is exactly where a
	// hostname belongs: the repository stays generic, the machine describes
	// itself.
	publicHost string

	// listenAll acknowledges that widening the listen address exposes the
	// daemon to the network. Without it, a non-loopback YAGIT_PUBLIC_HOST
	// is refused rather than silently binding 0.0.0.0.
	listenAll bool

	// publicURL is where a reverse proxy on this machine serves yagit — a
	// development VM that gives every app a host name of its own — held as the
	// origin a browser presents there once loadConfiguration has checked it.
	// Empty when the browser reaches the daemon directly.
	//
	// It is the other way to browse from elsewhere, and the one that widens
	// nothing: the proxy connects to the loopback like any local client.
	publicURL string
}

// envFile is .env parsed once: what it assigns, and what something asked for.
//
// Once, because two readings of one file are two chances to disagree, and the
// disagreement here would be invisible — a key read by one reading and called
// ignored by the other.
//
// The record of what was asked is what lets `./do` say which assignments it
// ignored. A hand-written list of the keys it understands would be a second
// definition of something readConfiguration already states, and the day the
// two drifted the warning would start naming the wrong lines — which is worse
// than no warning, because this one is read by somebody already confused.
type envFile struct {
	values map[string]string

	// keys is the assignment order, so a warning names lines in the order
	// somebody scrolling the file will meet them.
	keys []string

	asked map[string]bool
}

// parseEnvFile reads .env without executing it: a configuration file must not
// be able to run code.
//
// What counts as an assignment is the shell's rule, because that is the file
// this looks like: a name, then `=` against it, with no space between. `FOO =
// bar` is a command in a shell and is not a setting here either — refusing it
// is what keeps a file that reads like one thing from meaning another.
//
// A comment is not an assignment, which is what keeps the settings
// .env.example documents inside comments from being read as values or reported
// as ignored. The last assignment wins, which is what someone editing the file
// by hand expects when they paste a line at the bottom.
func parseEnvFile(contents string) *envFile {
	env := &envFile{values: make(map[string]string), asked: make(map[string]bool)}

	for line := range strings.Lines(contents) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, value, found := strings.Cut(trimmed, "=")
		if !found || key == "" || strings.ContainsAny(key, " \t") {
			continue
		}

		if _, seen := env.values[key]; !seen {
			env.keys = append(env.keys, key)
		}
		env.values[key] = unquote(strings.TrimSpace(value))
	}

	return env
}

// value reads a key and records that this program wanted it.
//
// An absent key is empty rather than an error — an optional one falls back to
// its default, and a required one is refused by name where it is used.
func (e *envFile) value(key string) string {
	e.asked[key] = true
	return e.values[key]
}

// ignoredKeys names the YAGIT_ assignments in the file that nothing read.
//
// Silence here is how YAGIT_TOKEN, YAGIT_PORT and YAGIT_ADDRESS come to sit
// in a .env doing nothing at all: every one of them is a reasonable guess, two
// of them are nearly the name of something real, and the file gives back no
// sign either way. A setting that does nothing has to say so.
func (e *envFile) ignoredKeys() []string {
	var ignored []string
	for _, key := range e.keys {
		if strings.HasPrefix(key, "YAGIT_") && !e.asked[key] {
			ignored = append(ignored, key)
		}
	}
	return ignored
}

// warnAboutIgnoredKeys reports them, naming what is read in the same breath so
// the reader can see which of the two lists their key was meant to be on.
func (e *envFile) warnAboutIgnoredKeys() {
	ignored := e.ignoredKeys()
	if len(ignored) == 0 {
		return
	}

	warn(".env sets %s, which nothing reads.%s",
		strings.Join(ignored, ", "),
		indent(
			"./do reads "+strings.Join(slices.Sorted(maps.Keys(e.asked)), ", ")+",",
			"and passes them to the daemon. The daemon never reads .env itself:",
			"everything else it takes from a flag or from its environment.",
			"See .env.example.",
		))
}

// loadConfiguration reads .env, creating it from .env.example on first run.
func (p *project) loadConfiguration() (configuration, error) {
	envPath := p.path(".env")

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		if err := p.createEnvFile(envPath); err != nil {
			return configuration{}, err
		}
		info(".env created from .env.example.")
	} else if err != nil {
		return configuration{}, fmt.Errorf("reading %s: %w", envPath, err)
	}

	raw, err := os.ReadFile(envPath)
	if err != nil {
		return configuration{}, fmt.Errorf("reading %s: %w", envPath, err)
	}
	env := parseEnvFile(string(raw))
	config := readConfiguration(env)

	// After every read, so the record of what was asked is complete — and
	// before the first refusal, which is the order that helps: YAGIT_ROOTT=/srv
	// earns both "YAGIT_ROOT is empty" and a line naming the key that is not it.
	env.warnAboutIgnoredKeys()

	if config.root == "" {
		return configuration{}, fmt.Errorf(
			"YAGIT_ROOT is empty in .env. Point it at the directory holding your git repositories")
	}
	if info, err := os.Stat(config.root); err != nil || !info.IsDir() {
		return configuration{}, fmt.Errorf(
			"YAGIT_ROOT is %q in .env, but that directory does not exist. Fix it or create it", config.root)
	}

	// Here, before anything starts, and in the form the card prints and the
	// daemon compares: an address a browser would write differently is an
	// allowlist entry that matches nothing, and it would be found out as a
	// 403 on the door page rather than as this sentence.
	if config.publicURL != "" {
		origin, err := publicurl.Origin(config.publicURL)
		if err != nil {
			// No full stop after %w: several of the reasons end in an example
			// URL, and a dot pasted onto it is a dot somebody pastes into .env.
			return configuration{}, fmt.Errorf("YAGIT_PUBLIC_URL is %q in .env: %w%s", config.publicURL, err, indent(
				"It is the address a reverse proxy on this machine serves yagit at.",
				"Without a proxy, comment it out of .env."))
		}
		config.publicURL = origin
	}

	return config, nil
}

// readConfiguration pulls out every setting `./do` understands, and is the
// only place that decides what that set is — ignoredKeys works out the rest by
// subtraction rather than from a list somebody has to remember to update.
func readConfiguration(env *envFile) configuration {
	host := env.value("YAGIT_PUBLIC_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	return configuration{
		root:       env.value("YAGIT_ROOT"),
		publicHost: host,
		listenAll:  envTruthy(env.value("YAGIT_LISTEN_ALL")),
		publicURL:  env.value("YAGIT_PUBLIC_URL"),
	}
}

func (p *project) createEnvFile(envPath string) error {
	template, err := os.ReadFile(p.path(".env.example"))
	if err != nil {
		return fmt.Errorf("reading .env.example: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding your home directory, which .env.example defaults to: %w", err)
	}

	filled := strings.ReplaceAll(string(template), "__HOME__", home)
	if err := os.WriteFile(envPath, []byte(filled), 0o600); err != nil {
		return fmt.Errorf("writing .env: %w", err)
	}
	return nil
}

// listenAddress derives the daemon's listen address from how a browser reaches
// it.
//
// Reaching the daemon by a name other than the loopback only works if it
// listens beyond the loopback. That widening requires YAGIT_LISTEN_ALL=1 —
// an explicit acknowledgment that the daemon will be reachable on the network.
//
// A reverse proxy is the other way to be reached from elsewhere, and it needs
// no widening at all: the proxy runs on this machine and connects to the
// loopback like any local client. So beside YAGIT_PUBLIC_URL, anything that
// asks for the widening is a .env saying two things at once, and it is refused
// rather than settled by a guess — widening would expose a daemon whose owner
// meant it to sit behind a proxy, and staying put would leave dead the direct
// address its owner meant to use.
func listenAddress(config configuration) (string, error) {
	loopback := fmt.Sprintf("127.0.0.1:%d", daemonPort)

	if config.publicURL != "" {
		if !isLoopbackName(config.publicHost) || config.listenAll {
			return "", publicURLBesideWidening(config)
		}
		return loopback, nil
	}

	if isLoopbackName(config.publicHost) {
		return loopback, nil
	}
	if !config.listenAll {
		// Both ways out, not only the widening one. This is the refusal a
		// .env written before the acknowledgment existed runs into, and advice
		// that says nothing but "listen on all interfaces" talks the reader
		// into the exposure the refusal is here to prevent — including the
		// reader who now browses on the machine itself, or through a proxy.
		return "", fmt.Errorf(
			"YAGIT_PUBLIC_HOST=%q names a host other than the loopback.%s",
			config.publicHost,
			indent(
				"Reaching the daemon by that name means listening on all interfaces.",
				"To do that, set YAGIT_LISTEN_ALL=1 in .env.",
				"To stay on the loopback, comment YAGIT_PUBLIC_HOST out of .env —",
				"and set YAGIT_PUBLIC_URL if a reverse proxy serves yagit to your browser."))
	}
	return fmt.Sprintf("0.0.0.0:%d", daemonPort), nil
}

// isLoopbackName reports whether a public host keeps the daemon on the
// loopback: the three spellings a browser on this machine uses for it.
func isLoopbackName(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	default:
		return false
	}
}

// publicURLBesideWidening is the refusal for a .env that puts yagit behind a
// proxy and also asks the daemon to be reached directly. It names what asked,
// and both ways out, in the order ADR 0014 settled on.
func publicURLBesideWidening(config configuration) error {
	var assignments, names []string
	if !isLoopbackName(config.publicHost) {
		assignments = append(assignments, fmt.Sprintf("YAGIT_PUBLIC_HOST=%q", config.publicHost))
		names = append(names, "YAGIT_PUBLIC_HOST")
	}
	if config.listenAll {
		assignments = append(assignments, "YAGIT_LISTEN_ALL=1")
		names = append(names, "YAGIT_LISTEN_ALL")
	}

	return fmt.Errorf(
		"YAGIT_PUBLIC_URL puts yagit behind a reverse proxy, and %s in .env asks for the daemon to be reached directly.%s",
		strings.Join(assignments, " with "),
		indent(
			"Behind a proxy the daemon stays on the loopback, which is where the proxy connects.",
			"To go through the proxy at "+config.publicURL+"/, comment "+strings.Join(names, " and ")+" out of .env.",
			"To reach the daemon directly instead, comment YAGIT_PUBLIC_URL out of .env."))
}

func envTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// daemonEnvironment is the environment `do dev` and the end-to-end tests both
// start the daemon with: they have to start exactly the same program.
//
// It is returned rather than exported into this process, so that what the
// child sees is visible at the call site instead of hidden in a side effect.
//
// It fails rather than panics on a listen address it cannot derive. The one
// way to get there is a .env naming a public host without acknowledging the
// exposure, which is a thing a person typed and a thing a person can fix —
// and listenAddress already says exactly how. A stack trace would bury that
// sentence under twenty lines of goroutine dump.
func (p *project) daemonEnvironment(config configuration, token string) ([]string, error) {
	addr, err := listenAddress(config)
	if err != nil {
		return nil, err
	}

	env := append(os.Environ(),
		"YAGIT_ROOT="+config.root,
		"YAGIT_ADDR="+addr,
		"YAGIT_PUBLIC_HOST="+config.publicHost,

		// Always, and empty when .env names none. The daemon reads this from
		// its environment, so one exported in the shell would otherwise reach
		// it unasked: an origin on the allowlist, and a startup line naming an
		// address, that .env never mentioned and the card does not print.
		// YAGIT_ALLOW_ORIGINS is left as the shell has it, on purpose — it is
		// the daemon's own flag, which .env has no key for and `./do` has no
		// opinion about, and the public origin needs no help from it.
		"YAGIT_PUBLIC_URL="+config.publicURL,

		// In development the daemon proxies everything outside /api to Vite.
		// This variable is absent in production, where the frontend is
		// embedded in the binary — and that is the only difference between the
		// two.
		fmt.Sprintf("YAGIT_FRONTEND_DEV_SERVER=http://127.0.0.1:%d", vitePort),

		// Vite reads this to pick its port. Nothing tells its hot-reload
		// client a port: it opens its WebSocket on the page's own origin,
		// which is the daemon when the browser comes straight to it and the
		// proxy when one is in front — and the daemon passes the upgrade on
		// to Vite like any other request outside /api.
		fmt.Sprintf("YAGIT_VITE_PORT=%d", vitePort),

		// The session token is handed down rather than left to the daemon to
		// generate: air restarts it on every saved Go file, and a fresh token
		// per rebuild would log the browser out in the middle of writing code.
		// It is the checkout's, not this run's — see sessionToken — and it is
		// the only token file there is: the daemon is given no YAGIT_TOKEN_FILE
		// to write, because a file it wrote at start and removed at stop was
		// read as "a daemon is up", and survived every daemon killed outright.
		"YAGIT_TOKEN="+token,
	)
	if config.listenAll {
		env = append(env, "YAGIT_LISTEN_ALL=1")
	}
	return env, nil
}

// ---------------------------------------------------------------------------
// The session token
// ---------------------------------------------------------------------------

// sessionToken returns the token this checkout authenticates with, minting one
// only when there is none to reuse.
//
// The token is deliberately older than the process AND older than this `./do`
// run. It was taken away from the daemon because air restarts it on every
// saved Go file, and a token per rebuild logged the browser out mid-keystroke.
// A token that died with `./do down` had the same fault one level up: every
// `./do up` began by invalidating the cookie in a tab that was still open, and
// the first thing it printed was a secret to paste again. The session is this
// checkout; `./do up --new-token` is how it ends.
func (p *project) sessionToken() (string, error) {
	stored, err := p.storedSessionToken()
	if err != nil {
		return "", err
	}
	if session.Minted(stored) {
		return stored, nil
	}
	if stored != "" {
		// Not silently. Handing this to the daemon would fail the start with
		// a message about YAGIT_TOKEN — a variable whoever reads it never
		// set, naming a file this sentence can name instead.
		warn("%s did not hold a usable token; minting a new one", sessionTokenFileName)
	}
	return p.renewSessionToken()
}

// storedSessionToken reads the token as it is on disk, and mints nothing:
// "" when there is no file. Every command that starts a stack goes through
// sessionToken instead; this is for the ones that only ask.
func (p *project) storedSessionToken() (string, error) {
	path := p.path(sessionTokenFileName)
	if err := refuseIrregularFile(path, sessionTokenFileName); err != nil {
		return "", err
	}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", sessionTokenFileName, err)
	}

	// The trailing newline is the file's, not the token's: a header built from
	// the raw bytes authenticates nothing and says only "invalid token".
	return strings.TrimSpace(string(raw)), nil
}

// renewSessionToken mints a token and replaces the stored one, ending every
// session that was running on its predecessor.
func (p *project) renewSessionToken() (string, error) {
	token, err := session.Mint()
	if err != nil {
		return "", err
	}
	// 0600, and all at once: this file grants full access to every repository
	// the daemon opens, and a reader must never find it half-written.
	if err := p.writeStateFile(sessionTokenFileName, []byte(token+"\n")); err != nil {
		return "", err
	}
	return token, nil
}

// replaceSessionToken carries out --new-token: it replaces the checkout's
// token and says what that costs, which is every browser holding the old
// cookie.
//
// The sentence is conditional on there having been a token to replace. On a
// checkout's very first start there is no session to end, and a warning that
// every browser must paste again would be a lie about a browser that has
// never seen the door page.
func (p *project) replaceSessionToken() error {
	previous, err := p.storedSessionToken()
	if err != nil {
		return err
	}
	if _, err := p.renewSessionToken(); err != nil {
		return err
	}
	if previous == "" {
		info("no session token to replace yet; minted the checkout's first")
		return nil
	}
	info("new session token — every browser holding the old one must paste again")
	return nil
}

// ---------------------------------------------------------------------------
// Runtime state
// ---------------------------------------------------------------------------

// ensureStateDirectory creates the runtime state directory, readable by nobody
// else.
//
// It holds the session token, and the throwaway stack's log — the daemon's
// whole output, every git command it ran and every repository path in it. A
// default umask would have left both world-readable. The daemon locks the
// directory down when it creates it itself; this program gets there first, so
// it has to do the same. protect.OwnerOnly is the one call that means that on
// every platform — chmod on Unix, an explicit owner-only ACL on Windows.
func (p *project) ensureStateDirectory() error {
	directory := p.path(stateDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", directory, err)
	}
	if err := protect.OwnerOnly(directory); err != nil {
		return err
	}
	return nil
}

// writeStateFile puts a file under the state directory, readable by nobody
// else, and all at once.
//
// All at once, because two `./do` commands can run in the same moment — a
// `./do up` in one terminal, a `./do test e2e` in another — and each reads
// the token the other may be writing. A reader that arrived between a
// truncate and a write found an empty file, decided the token was corrupt,
// and minted a replacement: the logout ADR 0026 set out to remove, with a
// warning about corruption on top. Written to a temporary name and renamed
// over, the file is either the old one or the new one and never in between.
func (p *project) writeStateFile(name string, content []byte) error {
	if err := p.ensureStateDirectory(); err != nil {
		return err
	}
	path := p.path(name)
	if err := refuseIrregularFile(path, name); err != nil {
		return err
	}

	// The same directory, so the rename is one step on one file system.
	// CreateTemp opens 0600; OwnerOnly re-asserts the lockdown on every
	// platform (chmod where modes govern access, an ACL where they do not).
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	if err := writeAndClose(temporary, content); err != nil {
		removeIfPresent(temporary.Name(), temporary.Name())
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := os.Rename(temporary.Name(), path); err != nil {
		removeIfPresent(temporary.Name(), temporary.Name())
		return fmt.Errorf("replacing %s: %w", name, err)
	}
	if err := protect.OwnerOnly(path); err != nil {
		return err
	}
	return nil
}

// writeAndClose finishes a file that was opened for writing, and reports the
// close as well as the write: on a full disk it is the close that fails.
func writeAndClose(file *os.File, content []byte) error {
	if _, err := file.Write(content); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			warn("closing %s: %s", file.Name(), closeErr)
		}
		return err
	}
	return file.Close()
}

// refuseIrregularFile keeps a state file from being anything but a regular
// file: a symbolic link, a directory, a device.
//
// The link is the case that matters. os.ReadFile follows it, os.WriteFile
// follows it too, and a dangling one reads as "no file" — so the token would
// be minted at the link's target, outside the 0700 directory, in whatever
// mode that directory's umask gives. The token lives in the checkout, which
// ADR 0026 decided deliberately; a link pointing elsewhere is refused by name
// rather than followed.
func refuseIrregularFile(path, name string) error {
	status, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	if status.Mode().IsRegular() {
		return nil
	}
	return fmt.Errorf("%s is %s, not a regular file.%s", name, kindOfFile(status.Mode()), indent(
		"Move it out of the way: what lives under "+stateDirectory+"/ is written by ./do alone."))
}

func kindOfFile(mode fs.FileMode) string {
	switch {
	case mode&fs.ModeSymlink != 0:
		return "a symbolic link"
	case mode.IsDir():
		return "a directory"
	default:
		return "of mode " + mode.String()
	}
}

// removeIfPresent deletes a state file. Its absence is the state we wanted;
// anything else is worth saying out loud, because the next command will read
// the file that could not be removed and believe it.
func removeIfPresent(path, name string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		warn("removing %s: %s", name, err)
	}
}
