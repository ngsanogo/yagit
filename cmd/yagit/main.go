// Command yagit is yagit's local daemon.
//
// It serves the web interface and drives the git repositories the user opens
// explicitly. This file is wiring only: read the configuration, generate the
// token, assemble the layers, manage the lifecycle. All the logic lives in
// internal/.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ngsanogo/yagit/internal/api"
	"github.com/ngsanogo/yagit/internal/assets"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/repo"
	"github.com/ngsanogo/yagit/internal/session"
	"github.com/ngsanogo/yagit/internal/watch"
)

// version is filled in at build time by `./do build` through -ldflags.
var version = "dev"

// defaultAddr listens on the loopback only: the safe default for a binary
// running on the same machine as the browser that talks to it.
//
// `./do` widens the listen address only when both a non-loopback
// YAGIT_PUBLIC_HOST and YAGIT_LISTEN_ALL=1 are set — a headless development
// machine browsed from somewhere else, and a person who agreed to it. The
// binary never reads .env and so cannot apply that rule, but it applies the
// half it can: validateListenAddress refuses any non-loopback -addr without
// -listen-all or YAGIT_LISTEN_ALL=1, so neither path widens by accident.
// What protects the daemon once it is widened is no longer the listen address
// but the session token, required on every route.
const defaultAddr = "127.0.0.1:" + defaultPort

// defaultPort is the port the daemon binds unless -addr says otherwise. It is
// written once: defaultAddr joins it above, and the announced URL falls back on
// it when the bound address carries no port of its own.
const defaultPort = "7420"

// defaultPublicHost is the host name a browser reaches the daemon by when
// nothing else is configured.
const defaultPublicHost = "127.0.0.1"

// shutdownGracePeriod bounds how long shutdown waits for in-flight requests.
const shutdownGracePeriod = 5 * time.Second

type configuration struct {
	addr              string
	publicHost        string
	root              string
	tokenFile         string
	allowedOrigins    []string
	frontendDevServer string
	tlsCert           string
	tlsKey            string
	listenAll         bool
}

func main() {
	// Before anything else, because this is not a daemon starting.
	//
	// An interactive rebase points git at this very executable for both of the
	// editors git may open — one over the todo list, which it writes, and one
	// over a commit message, which it refuses — so a rebase launches this
	// program again with a private flag. It has to come before the logger and
	// the flags: there is no configuration to read, nothing to serve, and a
	// usage error printed by flag.Parse over a private argument list would
	// reach git as a failed editor rather than as an explanation.
	//
	// git.RunAsEditor decides, on the exact flag and the exact argument count,
	// so an ordinary start falls through here untouched. See interactive.go,
	// where the request is made.
	if handled, err := git.RunAsEditor(os.Args); handled {
		if err != nil {
			// stderr and a non-zero exit, which is the whole vocabulary an
			// editor has. git stops rather than carrying on, and prints this
			// underneath its own "problem with the editor" — the right outcome
			// either way: a todo list that could not be written must never be
			// executed half-written, and a message nobody can be shown must
			// never be committed.
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	// Whatever handler goes here has to escape control characters in values.
	//
	// Request paths, git's stderr and panic messages all reach the log
	// verbatim, and all three are attacker-controlled in part. A handler that
	// wrote them raw would let a newline in a path close one entry and open a
	// forged one — "level=ERROR msg=..." of the client's choosing, in the
	// journal an operator reads to find out what happened.
	//
	// TextHandler quotes any value needing it and escapes the newline inside
	// the quotes; JSONHandler escapes it too. Verified rather than assumed. A
	// hand-rolled handler is where this stops being true.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	config, err := parseConfiguration()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	if err := run(config, logger); err != nil {
		logger.Error("daemon stopping on error", "error", err)
		os.Exit(1)
	}
}

func parseConfiguration() (configuration, error) {
	addr := flag.String("addr", environmentOr("YAGIT_ADDR", defaultAddr),
		"HTTP listen address")
	publicHost := flag.String("public-host", environmentOr("YAGIT_PUBLIC_HOST", defaultPublicHost),
		"host name a browser reaches this daemon by; used to build the announced URL and the accepted origins")
	root := flag.String("root", os.Getenv("YAGIT_ROOT"),
		"root under which yagit is allowed to open repositories (required)")
	tokenFile := flag.String("token-file", os.Getenv("YAGIT_TOKEN_FILE"),
		"file to write the session token to, so local tools can read it")
	allowOrigins := flag.String("allow-origins", os.Getenv("YAGIT_ALLOW_ORIGINS"),
		"additional origins accepted on mutating requests, comma-separated")
	frontendDevServer := flag.String("frontend-dev-server", os.Getenv("YAGIT_FRONTEND_DEV_SERVER"),
		"frontend development server to proxy to; empty in production")
	tlsCert := flag.String("tls-cert", os.Getenv("YAGIT_TLS_CERT"),
		"TLS certificate file; when set, -tls-key is required and the daemon serves HTTPS")
	tlsKey := flag.String("tls-key", os.Getenv("YAGIT_TLS_KEY"),
		"TLS private key file")
	listenAll := flag.Bool("listen-all", environmentTruthy("YAGIT_LISTEN_ALL"),
		"listen on all interfaces (0.0.0.0); required when -addr names a non-loopback host")
	flag.Parse()

	if *root == "" {
		return configuration{}, errors.New(
			"no repository root: pass -root or set YAGIT_ROOT")
	}

	if (*tlsCert == "") != (*tlsKey == "") {
		return configuration{}, errors.New(
			"TLS requires both -tls-cert and -tls-key (or YAGIT_TLS_CERT and YAGIT_TLS_KEY)")
	}

	if err := validateListenAddress(*addr, *listenAll); err != nil {
		return configuration{}, err
	}

	return configuration{
		addr:              *addr,
		publicHost:        *publicHost,
		root:              *root,
		tokenFile:         *tokenFile,
		allowedOrigins:    splitOrigins(*allowOrigins),
		frontendDevServer: *frontendDevServer,
		tlsCert:           *tlsCert,
		tlsKey:            *tlsKey,
		listenAll:         *listenAll,
	}, nil
}

func environmentOr(key, fallback string) string {
	if value, found := os.LookupEnv(key); found && value != "" {
		return value
	}
	return fallback
}

func environmentTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func validateListenAddress(addr string, listenAll bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen address %q: %w", addr, err)
	}

	if isLoopbackHost(host) {
		return nil
	}

	if listenAll {
		return nil
	}

	return fmt.Errorf(
		"listen address %q is not on the loopback; "+
			"pass -listen-all or set YAGIT_LISTEN_ALL=1 to acknowledge network exposure",
		addr)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func splitOrigins(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func run(config configuration, logger *slog.Logger) error {
	token, err := sessionToken()
	if err != nil {
		return err
	}

	// The event stream is built before the Runner, and the Runner publishes
	// into it. That ordering is why it is not created inside the server: the
	// promise that the log panel shows every command yagit runs holds only
	// if the observer is in place from the first command onwards.
	events := api.NewEventStream(logger)

	runner := git.NewRunner(func(execution git.Execution) {
		logCommand(logger, execution)
		events.PublishExecution(execution)
	})

	registry, err := repo.NewRegistry(config.root, runner)
	if err != nil {
		return err
	}

	watcher := startWatcher(logger, events)
	if watcher != nil {
		defer closeWatcher(watcher, logger)
	}

	api.Version = version

	frontend, err := buildFrontendHandler(config, logger)
	if err != nil {
		return err
	}

	// Listen before building the server, and before serving: a port already
	// taken fails right here, legibly, instead of surfacing from inside a
	// goroutine.
	//
	// The order also matters for correctness. Everything below reads the port
	// from the listener rather than from the requested address, because the two
	// differ whenever the address names port 0 — the kernel picks one, and an
	// announced URL of `http://127.0.0.1:0/` would send nobody anywhere.
	listener, err := net.Listen("tcp", config.addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", config.addr, err)
	}

	boundAddr := listener.Addr().String()
	scheme := "http"
	if config.tlsCert != "" {
		scheme = "https"
	}

	server, err := api.NewServer(api.Options{
		Registry:       registry,
		Runner:         runner,
		Token:          token,
		AllowedOrigins: append(browserOrigins(scheme, config.publicHost, boundAddr), config.allowedOrigins...),
		SecureCookies:  scheme == "https",
		Logger:         logger,
		Events:         events,
		Watcher:        watcher,
		Frontend:       frontend,
		Development:    config.frontendDevServer != "",
	})
	if err != nil {
		// The listener owns a file descriptor, and nothing below will take it
		// over now.
		if closeErr := listener.Close(); closeErr != nil {
			logger.Warn("could not close the listener", "error", closeErr)
		}
		return err
	}

	if config.tokenFile != "" {
		if err := writeTokenFile(config.tokenFile, token); err != nil {
			return err
		}
		defer removeTokenFile(config.tokenFile, logger)
	}

	logger.Info("yagit daemon started",
		"version", version,
		"addr", boundAddr,
		"scheme", scheme,
		"root", registry.Root(),
		"go", runtime.Version(),
		"git", gitVersionForTheLog(runner, logger),
	)
	announceStartup(scheme, config.publicHost, boundAddr, config.tokenFile)

	httpServer := newHTTPServer(server.Handler(), events)

	if config.tlsCert != "" {
		httpServer.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return serveUntilSignalTLS(httpServer, listener, config.tlsCert, config.tlsKey, logger)
	}

	return serveUntilSignal(httpServer, listener, logger)
}

// buildFrontendHandler picks who serves the interface.
//
// A single origin in both cases: in development the daemon proxies to Vite,
// in production it serves the embedded frontend. The browser sees no
// difference, so the authentication model is exactly the same on both sides —
// a model that differs between dev and prod is a model nobody ever really
// tests.
func buildFrontendHandler(config configuration, logger *slog.Logger) (http.Handler, error) {
	if config.frontendDevServer != "" {
		proxy, err := api.NewDevFrontendProxy(config.frontendDevServer, logger)
		if err != nil {
			return nil, err
		}
		logger.Info("interface proxied from the development server",
			"target", config.frontendDevServer)
		return proxy, nil
	}

	// Fail outright rather than serve a courtesy page: a binary without an
	// interface is broken, and finding that out at startup beats finding it
	// out in front of a blank screen.
	embedded, err := assets.Handler(logger)
	if err != nil {
		return nil, fmt.Errorf("embedded interface unavailable: %w", err)
	}
	logger.Info("interface served from the binary")
	return embedded, nil
}

// newHTTPServer pairs the routing with the hook that ends the event streams.
//
// The pairing is the point, and it is why this is a function rather than four
// lines inside run. An open event stream is a request that by design never
// ends, and Shutdown closes idle connections and then WAITS for the ones still
// in flight — so without the hook a single open tab makes every stop wait out
// the whole grace period and then exit non-zero, which any supervisor reads as
// a crash. Under `./do dev` air's kill delay expires first and every hot
// reload costs the difference.
func newHTTPServer(handler http.Handler, events *api.EventStream) *http.Server {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	server.RegisterOnShutdown(events.Close)
	return server
}

func serveUntilSignal(server *http.Server, listener net.Listener, logger *slog.Logger) error {
	serveErrors := make(chan error, 1)
	go func() {
		// ErrServerClosed is the normal ending Shutdown triggers: it is not
		// a failure, and it is the only error we may ignore here.
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrors <- err
			return
		}
		serveErrors <- nil
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErrors:
		if err != nil {
			return fmt.Errorf("HTTP service: %w", err)
		}
		return nil
	case received := <-signals:
		logger.Info("signal received, shutting down", "signal", received.String())
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	if err := <-serveErrors; err != nil {
		return fmt.Errorf("HTTP service: %w", err)
	}

	logger.Info("yagit daemon stopped")
	return nil
}

func serveUntilSignalTLS(
	server *http.Server,
	listener net.Listener,
	certFile, keyFile string,
	logger *slog.Logger,
) error {
	serveErrors := make(chan error, 1)
	go func() {
		if err := server.ServeTLS(listener, certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrors <- err
			return
		}
		serveErrors <- nil
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErrors:
		if err != nil {
			return fmt.Errorf("HTTPS service: %w", err)
		}
		return nil
	case received := <-signals:
		logger.Info("signal received, shutting down", "signal", received.String())
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}
	if err := <-serveErrors; err != nil {
		return fmt.Errorf("HTTPS service: %w", err)
	}

	logger.Info("yagit daemon stopped")
	return nil
}

// logCommand traces every git command that runs.
//
// Half of a promise the project makes: the user must always be able to learn
// git by watching yagit work. This half is the daemon's journal; the other is
// the event stream, which carries the same executions to the interface's log
// panel. Both are fed from the one observer, so neither can drift from what
// actually ran.
// gitVersionForTheLog reads the version of the git this daemon will drive, for
// the one line that says what started.
//
// yagit is a front end over that binary, so a startup line naming the Go
// runtime and not the git is describing the smaller half of what is running.
// Which git it is decides whether a rebase, a force push and a worktree list
// behave — and it is the first thing worth knowing about a machine where one
// of them misbehaves and nowhere else does.
//
// A git that cannot be read is reported in the line rather than raised: the
// daemon starts anyway, on purpose. Everything except the network and the
// operations that need a newer flag still works, and refusing to start would
// take the history, the diffs and the staging away from someone whose only
// problem is an old distribution. The operations that need more say so
// themselves, by name, at the moment they are asked for — and GET /api/health
// names them all before anyone hits one.
func gitVersionForTheLog(runner *git.Runner, logger *slog.Logger) string {
	// Its own deadline, short: this runs before the listener is serving, and a
	// git that hangs here would hang the startup rather than one request.
	ctx, cancel := context.WithTimeout(context.Background(), gitVersionProbeTimeout)
	defer cancel()

	version, err := runner.GitVersion(ctx)
	if err != nil {
		logger.Warn("could not read the git version; git is what yagit drives, so most operations will fail",
			"error", err)
		return "unreadable"
	}
	return version.String()
}

// gitVersionProbeTimeout bounds the one git command that runs before the
// daemon is listening. `git version` does not touch a repository or the
// network, so a second is already generous; what it guards against is a git
// that is not really git.
const gitVersionProbeTimeout = 5 * time.Second

func logCommand(logger *slog.Logger, execution git.Execution) {
	attributes := []any{
		"command", execution.CommandLine(),
		"dir", execution.Dir,
		"exit", execution.ExitCode,
		"duration", execution.Duration.Round(time.Microsecond).String(),
	}

	if execution.ExitCode == 0 {
		logger.Info("git", attributes...)
		return
	}
	logger.Warn("git", append(attributes, "stderr", strings.TrimSpace(execution.Stderr))...)
}

// startWatcher follows the git directories of open repositories and turns
// what it sees into events.
//
// A daemon whose watcher will not start still serves. The interface then
// refreshes when it asks rather than when the repository moves, which is worse
// and is not nothing — and the reason is on the record instead of being a
// mystery about why a commit made in a terminal never appears.
func startWatcher(logger *slog.Logger, events *api.EventStream) *watch.Watcher {
	watcher, err := watch.New(logger)
	if err != nil {
		logger.Warn("repositories will not refresh on their own", "error", err)
		return nil
	}

	go func() {
		// Ends when the watcher is closed, which closes this channel.
		for change := range watcher.Changes() {
			events.PublishRepositoryChanged(change.RepositoryID)
		}
	}()

	return watcher
}

func closeWatcher(watcher *watch.Watcher, logger *slog.Logger) {
	if err := watcher.Close(); err != nil {
		// Shutdown carries on. The descriptors go with the process either
		// way, but a failure here is worth a line rather than silence.
		logger.Warn("cannot stop the filesystem watch", "error", err)
	}
}

// sessionToken returns the token that guards this run.
//
// It is generated here unless the environment already provides one, which is
// what `./do` does: air restarts the daemon on every Go file saved, and a
// fresh token on each rebuild would invalidate the browser cookie — you would
// be logged out while writing code. `./do` goes further and keeps the token in
// the checkout, so `./do down` followed by `./do up` does not log you out
// either; a development session lasts as long as the checkout, not as long as
// one process. A released binary run on its own has no such file and generates
// its own, which is the branch below.
//
// There is deliberately no matching `-token` flag: a process command line is
// readable by everyone through `ps`, its environment is not.
func sessionToken() (string, error) {
	if provided := os.Getenv("YAGIT_TOKEN"); provided != "" {
		// A supplied token is still the only thing standing between a web page
		// and every open repository, so it is checked rather than trusted.
		if err := session.Check(provided); err != nil {
			return "", fmt.Errorf("YAGIT_TOKEN: %w", err)
		}
		return provided, nil
	}
	return session.Mint()
}

// writeTokenFile drops the token where a local tool can read it, when a
// -token-file asks for one.
//
// Nothing under `./do` asks: there the token is the checkout's, minted into
// .yagit/session-token before this process exists, and whether a daemon is
// running is a question `./do` puts to the port rather than to a file — a
// file outlives a daemon killed outright, and an HTTP answer does not. The
// flag is for a released binary run by hand, whose token would otherwise be
// known to nobody.
func writeTokenFile(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating the token file directory: %w", err)
	}
	// 0600 on Unix; an owner-only ACL on Windows. This file grants full access
	// to the open repositories. The parent directory is not locked down here:
	// -token-file may sit under a home directory the user already shares, and
	// OwnerOnly on that path would revoke everyone else's listing of it.
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing token file %s: %w", path, err)
	}
	if err := protect.OwnerOnly(path); err != nil {
		return err
	}
	return nil
}

func removeTokenFile(path string, logger *slog.Logger) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Shutdown carries on anyway, but a token left behind on disk
		// deserves a line rather than silence.
		logger.Warn("cannot remove token file", "path", path, "error", err)
	}
}

// browserOrigins builds the origins accepted out of the box: the ones a
// browser legitimately reaches this daemon through.
//
// The loopback is always there, in its three spellings. publicHost joins them
// because the machine running the daemon is not always the one displaying it:
// in development the daemon runs on a headless machine and the browser is
// elsewhere, so the origin the browser presents is not 127.0.0.1. Without
// that entry, every cookie-authenticated mutating request would be refused,
// with an error that talks about origins without naming the missing one.
func browserOrigins(scheme, publicHost, addr string) []string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		// Address with no explicit port: nothing can be deduced, and
		// guessing an origin would be worse than accepting none.
		return nil
	}

	hosts := []string{"127.0.0.1", "localhost", "::1"}
	if publicHost != "" && !slices.Contains(hosts, publicHost) {
		hosts = append(hosts, publicHost)
	}

	origins := make([]string, 0, len(hosts))
	for _, host := range hosts {
		// JoinHostPort adds the brackets a literal IPv6 address requires.
		origins = append(origins, scheme+"://"+net.JoinHostPort(host, port))
	}
	return origins
}

// browserURL builds the address to open: the name a browser reaches this
// daemon by, not the address it listens on — 0.0.0.0 is the name of nothing,
// and a URL you cannot paste into an address bar is of no use to anyone.
//
// The session token is deliberately absent: it belongs in a POST to
// /api/session or in ./do token, not in a URL that history and proxies keep.
func browserURL(scheme, publicHost, addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = defaultPort
	}
	if publicHost == "" {
		publicHost = defaultPublicHost
	}
	return fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(publicHost, port))
}

// announceStartup writes what the user needs to open yagit, outside the
// structured log: one line for the address, one for where the token lives.
//
// The second line has to be true of whoever is reading it. A binary installed
// from a release has no checkout and no `./do`, and the launcher gives it a
// -token-file, so naming that file is the whole answer. Without one, the token
// exists only inside this process — the way in is `./do token` in a checkout,
// and for a binary started on its own, a -token-file or a token of the user's
// own in YAGIT_TOKEN. Saying so at startup is cheaper than locking somebody out
// of a daemon that is otherwise running perfectly.
func announceStartup(scheme, publicHost, addr, tokenFile string) {
	url := browserURL(scheme, publicHost, addr)

	if _, err := fmt.Fprintf(os.Stderr, "\n  Open yagit: %s\n", url); err != nil {
		return
	}

	if tokenFile != "" {
		if _, err := fmt.Fprintf(os.Stderr, "  Session token: %s\n\n", tokenFile); err != nil {
			return
		}
		return
	}

	if _, err := fmt.Fprintf(os.Stderr,
		"  Session token: run ./do token in the checkout. Started on its own, this\n"+
			"                 binary needs -token-file or YAGIT_TOKEN to be reachable.\n\n"); err != nil {
		return
	}
}
