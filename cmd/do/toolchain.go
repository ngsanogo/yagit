package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ---------------------------------------------------------------------------
// Running other programs
// ---------------------------------------------------------------------------

// tool builds a command from the toolchain, in the project directory, with
// this program's own streams.
//
// The `./do` shim runs everything under `mise exec`, so the tools pinned in
// mise.toml are already at the front of PATH by the time we get here. A tool
// that is still missing means the shim was bypassed, and saying so beats a
// bare "executable file not found".
func (p *project) tool(name string, arguments ...string) (*exec.Cmd, error) {
	resolved, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not on PATH.%s", errMissingTool, name, indent(
			"It is pinned in mise.toml, and ./do installs it.",
			"Run the command through ./do rather than invoking this program directly."))
	}

	command := exec.Command(resolved, arguments...)
	command.Dir = p.directory
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command, nil
}

// run executes a toolchain command and waits for it.
func (p *project) run(name string, arguments ...string) error {
	command, err := p.tool(name, arguments...)
	if err != nil {
		return err
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), err)
	}
	return nil
}

// capture executes a toolchain command and returns its standard output,
// trimmed. Standard error still reaches the terminal: a tool that explains
// itself while producing a value should not be silenced.
func (p *project) capture(name string, arguments ...string) (string, error) {
	command, err := p.tool(name, arguments...)
	if err != nil {
		return "", err
	}
	command.Stdout = nil
	command.Stdin = nil

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

// npm runs npm against the frontend workspace.
func (p *project) npm(arguments ...string) error {
	return p.run("npm", append([]string{"--prefix", "web"}, arguments...)...)
}

// ---------------------------------------------------------------------------
// Frontend dependencies
// ---------------------------------------------------------------------------

// nodeBinary is the path of a binary npm linked into web/node_modules/.bin.
//
// npm writes a .cmd wrapper on Windows and a symlink everywhere else; naming
// the extensionless one there would look like a missing install.
func (p *project) nodeBinary(name string) string {
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	return p.path("web", "node_modules", ".bin", name)
}

// ensureFrontendDependencies installs web/node_modules when it is absent, or
// when it was installed from a different lockfile.
//
// `npm ci` reproduces the lockfile exactly, where `npm install` would quietly
// rewrite it — and it deletes node_modules first, so it costs minutes. What
// decides is the lockfile's content, recorded at the last successful install:
// a timestamp comparison would reinstall the whole tree after every `git
// checkout`, which rewrites mtimes without changing a byte.
func (p *project) ensureFrontendDependencies() error {
	if p.frontendDependenciesAreCurrent() {
		return nil
	}

	info("installing frontend dependencies")
	if err := p.npm("ci", "--no-audit", "--no-fund"); err != nil {
		return err
	}
	return p.recordStamp(frontendStamp)
}

// frontendDependenciesAreCurrent reports whether node_modules holds what the
// lockfile asks for.
//
// Both halves are needed. The stamp answers "installed from this lockfile",
// and the binary answers "still on disk" — an interrupted `npm ci`, or a
// deleted node_modules, leaves a stamp that is true about an install that is
// no longer there.
func (p *project) frontendDependenciesAreCurrent() bool {
	if _, err := os.Stat(p.nodeBinary("vite")); err != nil {
		return false
	}
	return p.stampIsCurrent(frontendStamp)
}

// ensurePlaywrightBrowser installs Chromium on first use.
//
// The browsers weigh about 150 MB and are only needed for screenshots and
// end-to-end tests. PLAYWRIGHT_BROWSERS_PATH is set by the ./do shim to
// .yagit/browsers/, so they stay inside the checkout.
func (p *project) ensurePlaywrightBrowser() error {
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}

	// The local binary, not `npm exec`: with --yes npm would silently fetch a
	// Playwright from the registry when the local one is missing, and drive
	// the browsers with a version other than the one pinned in
	// web/package.json.
	playwright := p.nodeBinary("playwright")
	if _, err := os.Stat(playwright); err != nil {
		return fmt.Errorf("%s is missing. Delete web/node_modules and run this again", playwright)
	}

	command := exec.Command(playwright, "install", "chromium")
	command.Dir = p.directory
	command.Env = os.Environ()
	command.Stdout = nil
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("could not install Chromium through Playwright: %w.%s", err, indent(
			"If launching it then fails on missing system libraries:",
			"sudo "+playwright+" install-deps chromium"))
	}
	return nil
}
