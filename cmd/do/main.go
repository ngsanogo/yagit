// Command do is yagit's single entry point.
//
// One project action, one subcommand here. There is no second path: no
// Makefile, no `npm run` to type by hand, no container to build. When two
// paths lead to the same place, one of them goes stale and nobody notices.
//
// It is invoked through the `./do` shim, which installs the toolchain pinned
// in mise.toml and then hands over. Running `go run ./cmd/do` directly works
// too, on a machine where that toolchain is already on PATH.
//
// This program is Go rather than shell for three reasons, each of which cost
// the shell version something real. It runs on Windows, which yagit ships a
// binary for. It needs no interpreter beyond the one the project already
// pins, where the shell version needed bash 4.3 — newer than the bash macOS
// ships, which made the README's "mise is the only prerequisite" untrue. And
// it can be tested: the .env reader and the listen-address rule below have
// tests, where before they had none.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// The project's two ports are defined here and passed through the environment
// to everything the stack starts — the daemon, Vite, Playwright. A value
// hard-coded in three config files is a value that will drift.
//
// A released binary has no `./do` to hand it one, so cmd/yagit carries its own
// loopback default for that case, and nothing else does.
const (
	daemonPort = 7420
	vitePort   = 5173
)

// daemonURL is how this program reaches the daemon: it runs on this machine.
// That is not necessarily how a browser reaches it — see publicHost.
var daemonURL = fmt.Sprintf("http://127.0.0.1:%d", daemonPort)

// Runtime state: the session token, the throwaway stack's log, screenshots.
//
// One token file. It holds the secret this checkout authenticates with, is
// written by this program alone, and outlives every stack it starts. Whether
// a daemon is running is a different question, and it is put to the daemon —
// probeStack, over HTTP — never to a file: a file survives a daemon killed
// outright, and an answer on the port does not.
const (
	stateDirectory       = ".yagit"
	sessionTokenFileName = stateDirectory + "/session-token"
)

// goPackages names the project's Go packages explicitly.
//
// `./...` from the root would also descend into web/node_modules, where some
// npm packages ship Go sources as examples. The pattern below says exactly
// where our code lives, and will still say it when the npm tree changes.
var goPackages = []string{"./cmd/...", "./internal/..."}

// buildTargets are what `do build` produces. Building for every mainstream
// desktop and server platform costs a few seconds and spares anyone the need
// for a cross-compiler.
var buildTargets = []string{
	"linux/amd64", "linux/arm64",
	"darwin/amd64", "darwin/arm64",
	"windows/amd64", "windows/arm64",
}

// command is one subcommand. The table below is the only list of them: it
// drives dispatch and it prints the help, so the two cannot disagree.
type command struct {
	usage   string
	summary string
	run     func(project *project, args []string) error
}

var commands = map[string]command{
	"up": {
		usage:   "up [--restart] [--foreground] [--new-token]",
		summary: "Start the stack in the background. Print the URL and token, then return.",
		run:     runUp,
	},
	"down": {
		usage:   "down",
		summary: "Stop the background stack.",
		run:     runDown,
	},
	"status": {
		usage:   "status",
		summary: "Report whether the stack is running, and print the URL if it is.",
		run:     runStatus,
	},
	"logs": {
		usage:   "logs [--no-follow]",
		summary: "Show output from the background stack.",
		run:     runLogs,
	},
	"dev": {
		usage:   "dev [--new-token]",
		summary: "Run the stack in the foreground, with logs in the terminal.",
		run:     runDev,
	},
	"bootstrap": {
		usage:   "bootstrap [--browsers]",
		summary: "Install dependencies into .yagit/ and web/node_modules/.",
		run:     runBootstrap,
	},
	"shell-hook": {
		usage:   "shell-hook [--write]",
		summary: "Generate shell aliases that call ./do up, down and logs from anywhere.",
		run:     runShellHook,
	},
	"build": {
		usage:   "build",
		summary: "Build the frontend and the binaries into dist/.",
		run:     runBuild,
	},
	"test": {
		usage:   "test [go|web|e2e|release|bench|fuzz|soak|coverage]",
		summary: "Run tests. With no argument, go, web and e2e. release needs ./do build first.",
		run:     runTest,
	},
	"lint": {
		usage:   "lint",
		summary: "Static analysis of Go, the frontend, the ./do shim, and the CI workflows.",
		run:     runLint,
	},
	"audit": {
		usage:   "audit",
		summary: "Check every dependency, the Go standard library included, against the vulnerability databases.",
		run:     runAudit,
	},
	"fmt": {
		usage:   "fmt",
		summary: "Reformat the code.",
		run:     runFormat,
	},
	"shot": {
		usage:   "shot [url] [out]",
		summary: "Screenshot a page into a PNG, through Playwright.",
		run:     runShot,
	},
	"drive": {
		usage:   "drive [url]",
		summary: "Drive the running interface from stdin: one command per line, in a headless browser.",
		run:     runDrive,
	},
	"version": {
		usage:   "version",
		summary: "Print the version this checkout would be released as, if anything warrants one.",
		run:     runVersion,
	},
	"token": {
		usage:   "token",
		summary: "Print the session token, to query the API with curl. ./do up --new-token replaces it.",
		run:     runToken,
	},
	"agent": {
		usage:   "agent sync|check",
		summary: "Sync or verify tool shims generated from agent/.",
		run:     runAgent,
	},
}

// commandOrder fixes the order the help prints them in, which a map cannot.
// It runs from what you do every day to what you do once a month.
var commandOrder = []string{"up", "down", "status", "logs", "dev", "bootstrap", "shell-hook", "build", "test", "lint", "audit", "fmt", "shot", "drive", "token", "version", "agent"}

// exitInterrupted is the status a shell expects from a program the terminal
// interrupted: 128 plus SIGINT. Returning 0 there would make `./do dev && …`
// carry on as if the command had succeeded.
const exitInterrupted = 130

// errInterrupted says the command stopped because the user asked it to, which
// is not a failure and needs no error message.
var errInterrupted = errors.New("interrupted")

func main() {
	err := dispatch(os.Args[1:])

	switch {
	case err == nil:
		return
	case errors.Is(err, errInterrupted):
		os.Exit(exitInterrupted)
	default:
		fmt.Fprintf(os.Stderr, "%serror: %s%s\n", colorRed, err, colorOff)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	if len(args) > 0 && args[0] == stackSuperviseCmd {
		project, err := locateProjectForSupervise()
		if err != nil {
			return err
		}
		return runStackSupervise(project)
	}

	name := "help"
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
	}

	switch name {
	case "help", "-h", "--help":
		printHelp()
		return nil
	}

	chosen, known := commands[name]
	if !known {
		return fmt.Errorf("unknown command: %q. Run './do help' for the list", name)
	}

	// Before the command runs, and therefore before anything it would install,
	// start or delete. Asking how a command is spelled is not a reason to pay
	// for an `npm ci` — and it is not a failure either, so it exits 0.
	if helpRequested(args) {
		printCommandHelp(chosen)
		return nil
	}

	// A command whose usage line names no argument takes none, and says so.
	// `./do token --new-token` used to print the live token and exit 0: the
	// one flag a person reaches for after a leak, ignored without a word, and
	// the secret it was meant to replace handed back as if it had been.
	if chosen.usage == name && len(args) > 0 {
		return fmt.Errorf("%s takes no arguments, got %q. Run './do %s --help'", name, args[0], name)
	}

	project, err := locateProject()
	if err != nil {
		return err
	}

	return chosen.run(project, args)
}

// helpRequested reports whether the arguments ask for the usage line rather
// than for the command. One place decides it, so no command can forget to.
func helpRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printCommandHelp(chosen command) {
	fmt.Printf("usage: ./do %s\n\n  %s\n", chosen.usage, chosen.summary)
}

func printHelp() {
	fmt.Printf("%syagit%s — every project command goes through this script.\n\n", colorBold, colorOff)
	for _, name := range commandOrder {
		fmt.Printf("  ./do %-24s %s\n", commands[name].usage, commands[name].summary)
	}
	fmt.Println()
}

// project is the checkout this run works in. Every path below is derived from
// its directory, so nothing depends on where the command was typed.
type project struct {
	directory string

	// copyShims makes `agent sync` duplicate the files it would otherwise
	// link, which is the shim tree a Windows checkout gets when the process
	// has no right to create a symlink. Only a test sets it: os.Symlink
	// succeeds on every machine this repository's tests run on, so that half
	// of sync is unreachable by construction and would be executed nowhere.
	copyShims bool
}

// locateProject finds the checkout by walking up from the working directory
// until go.mod appears.
//
// Symlinks are resolved: the path handled here has to be the real one, the
// same path yagit will show in its git command log.
func locateProject() (*project, error) {
	start, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("reading the working directory: %w", err)
	}

	directory, err := filepath.EvalSymlinks(start)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", start, err)
	}

	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return &project{directory: directory}, nil
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			return nil, fmt.Errorf(
				"no go.mod above %s: this command has to run inside the yagit checkout", start)
		}
		directory = parent
	}
}

// path joins a repository-relative path onto the checkout.
func (p *project) path(elements ...string) string {
	return filepath.Join(append([]string{p.directory}, elements...)...)
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

var colorDim, colorBold, colorRed, colorYellow, colorOff = detectColors()

// detectColors decides whether to emit ANSI escapes.
//
// Three conditions, and all of them have to hold. NO_COLOR is honoured because
// it is the one convention every tool agrees on. The stream has to be a
// terminal, or the escapes end up inside a log file and inside CI output. And
// on Windows a console only interprets them once virtual-terminal processing
// is on, which is the case in Windows Terminal and in anything that sets TERM,
// and not the case in a bare conhost — where colouring would print the escapes
// literally instead.
func detectColors() (dim, bold, red, yellow, off string) {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return "", "", "", "", ""
	}

	info, err := os.Stderr.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return "", "", "", "", ""
	}

	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" && os.Getenv("TERM") == "" {
		return "", "", "", "", ""
	}

	return "\033[2m", "\033[1m", "\033[31m", "\033[33m", "\033[0m"
}

func info(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "%s→ %s%s\n", colorDim, fmt.Sprintf(format, arguments...), colorOff)
}

func warn(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "%s! %s%s\n", colorYellow, fmt.Sprintf(format, arguments...), colorOff)
}

// indent lays a multi-line hint under an error message, so advice that needs
// three lines can have three lines.
func indent(lines ...string) string {
	return "\n       " + strings.Join(lines, "\n       ")
}

// errMissingTool is what every "the toolchain is not here" error wraps, so a
// caller can tell that case apart from a tool that ran and failed.
var errMissingTool = errors.New("missing tool")
