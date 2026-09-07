package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/session"
)

// testRelease exercises the built binary the way a release does: embedded
// frontend, no Vite proxy. `./do test e2e` never reaches this path — it runs
// against the development stack — and `./do build` only compiles until
// something actually starts the executable.
func (p *project) testRelease() error {
	info("release smoke test")

	binary, err := p.releaseBinaryPath()
	if err != nil {
		return err
	}

	root, err := os.MkdirTemp("", "yagit-release-*")
	if err != nil {
		return fmt.Errorf("creating a scratch root: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(root); err != nil {
			warn("removing %s: %s", root, err)
		}
	}()

	port, err := freePort()
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	baseURL := "http://" + addr

	// A token of its own, never the checkout's: a `./do up` running in another
	// terminal keeps its session, and a tab left open on it cannot wander into
	// this daemon. It travels in the environment, and no -token-file is asked
	// for — nothing reads one.
	token, err := session.Mint()
	if err != nil {
		return err
	}

	log, err := p.openReleaseLog()
	if err != nil {
		return err
	}
	defer func() {
		if err := log.Close(); err != nil {
			warn("closing %s: %s", log.Name(), err)
		}
	}()

	command := exec.Command(binary,
		"-root", root,
		"-addr", addr,
	)
	command.Env = append(os.Environ(), "YAGIT_TOKEN="+token)
	command.Stdin = nil
	command.Stdout = log
	command.Stderr = log
	isolateProcessTree(command)

	if err := command.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", binary, err)
	}

	stopped := make(chan struct{})
	go func() {
		if waitErr := command.Wait(); waitErr != nil {
			warn("%s exited: %s", filepath.Base(binary), waitErr)
		}
		close(stopped)
	}()
	defer func() {
		if err := terminateProcessTree(command); err != nil {
			warn("%s", err)
		}
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			warn("the release binary did not stop within 10 s")
		}
	}()

	if err := p.waitForRelease(baseURL, token, 30*time.Second); err != nil {
		warn("the release binary did not answer. Its output:")
		printFile(log.Name())
		return err
	}

	return p.testReleaseBrowser(baseURL, token)
}

// releaseBinaryPath is dist/yagit-GOOS-GOARCH for this machine.
func (p *project) releaseBinaryPath() (string, error) {
	name := fmt.Sprintf("yagit-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	path := p.path("dist", name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no release binary at %s: run ./do build first", path)
		}
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return path, nil
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("finding a free port: %w", err)
	}
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("releasing the probe listener: %w", err)
	}

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("the probe listener did not return a TCP address")
	}
	return address.Port, nil
}

func (p *project) waitForRelease(baseURL, token string, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if p.releaseResponds(baseURL, token) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("the release binary did not answer in time")
}

func (p *project) releaseResponds(baseURL, token string) bool {
	return p.probeAt(baseURL, token, "/api/health", 5*time.Second) &&
		p.probeReleaseFrontend(baseURL, token, 5*time.Second)
}

// probeReleaseFrontend checks that / serves the embedded application, not a
// proxy error or the "run ./do build" refusal.
func (p *project) probeReleaseFrontend(baseURL, token string, timeout time.Duration) bool {
	status, body, err := p.fetchAt(baseURL, token, "/", timeout)
	if err != nil || status != http.StatusOK {
		return false
	}
	return strings.Contains(string(body), "<title>yagit</title>")
}

// testReleaseBrowser runs the one spec that belongs to a release, under a
// Playwright configuration of its own.
//
// Not `test:e2e` with a file argument: that configuration's global setup asks
// the daemon which repositories are open and then deletes the fixtures nobody
// holds. Pointed at a release binary with an empty root of its own, it would
// answer "none" and sweep away the fixtures a development daemon in another
// terminal is still using. The release suite touches no fixtures at all, so it
// runs without that setup rather than with it defused.
//
// The token travels in the environment because that is the only place it is:
// this run minted it, and the release daemon wrote no file.
func (p *project) testReleaseBrowser(baseURL, token string) error {
	info("release browser test")
	if err := p.ensurePlaywrightBrowser(); err != nil {
		return err
	}

	command, err := p.tool("npm", "--prefix", "web", "run", "test:release")
	if err != nil {
		return err
	}
	command.Env = append(os.Environ(),
		"YAGIT_BASE_URL="+baseURL,
		"YAGIT_TOKEN="+token,
	)

	if err := command.Run(); err != nil {
		return fmt.Errorf("release browser test: %w", err)
	}
	return nil
}

func (p *project) openReleaseLog() (*os.File, error) {
	if err := p.ensureStateDirectory(); err != nil {
		return nil, err
	}

	path := p.path(stateDirectory, "release.log")
	log, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", path, err)
	}
	if err := protect.OwnerOnly(path); err != nil {
		if closeErr := log.Close(); closeErr != nil {
			warn("closing %s after a failed lockdown: %s", path, closeErr)
		}
		return nil, err
	}
	return log, nil
}
