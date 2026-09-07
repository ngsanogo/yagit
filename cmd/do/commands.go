package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/ngsanogo/yagit/internal/protect"
	"github.com/ngsanogo/yagit/internal/session"
)

// ---------------------------------------------------------------------------
// build
// ---------------------------------------------------------------------------

func runBuild(p *project, _ []string) error {
	info("building the frontend")
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}

	if err := p.emptyEmbeddedAssets(); err != nil {
		return err
	}
	if err := p.npm("run", "build"); err != nil {
		return err
	}
	if err := p.verifyAssetsAreFiles(); err != nil {
		return err
	}

	// The binary carries the version git describes. With no tag and no commit,
	// git describe fails, and "dev" is the honest answer.
	version, err := p.capture("git", "describe", "--tags", "--always", "--dirty")
	if err != nil || version == "" {
		version = "dev"
	}

	distribution := p.path("dist")
	if err := os.MkdirAll(distribution, 0o755); err != nil {
		return fmt.Errorf("creating dist/: %w", err)
	}

	for _, target := range buildTargets {
		operatingSystem, architecture, _ := strings.Cut(target, "/")

		output := filepath.Join(distribution, fmt.Sprintf("yagit-%s-%s", operatingSystem, architecture))
		if operatingSystem == "windows" {
			output += ".exe"
		}

		info("compiling %s", target)
		command, err := p.tool("go", "build",
			"-trimpath",
			"-ldflags", "-s -w -X main.version="+version,
			"-o", output,
			"./cmd/yagit")
		if err != nil {
			return err
		}

		// CGO is off: yagit only shells out to the git binary, so nothing
		// justifies a dependency on the target machine's libc.
		command.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS="+operatingSystem,
			"GOARCH="+architecture,
		)

		if err := command.Run(); err != nil {
			return fmt.Errorf("compiling %s: %w", target, err)
		}
	}

	return listBuiltBinaries(distribution)
}

// inlinedAssetPattern finds a stylesheet asking for a data: URI, quoted or
// not — both spellings are legal CSS and bundlers emit either.
var inlinedAssetPattern = regexp.MustCompile(`url\(\s*["']?(data:[^)"'\s]*)`)

// inlinedAsset returns the first data: URI a stylesheet embeds, shortened
// enough to name in an error, or "" when it embeds none.
func inlinedAsset(stylesheet string) string {
	match := inlinedAssetPattern.FindStringSubmatch(stylesheet)
	if match == nil {
		return ""
	}

	// A base64 payload is thousands of characters and says nothing; the type
	// at the front is what identifies which asset slipped through.
	uri := match[1]
	if len(uri) > 48 {
		uri = uri[:48] + "…"
	}
	return uri
}

// verifyAssetsAreFiles refuses a build whose stylesheets carry an asset inside
// them rather than beside them.
//
// The daemon sends default-src 'self', so a data: URI is refused by the
// browser — and only in the released binary, because development serves this
// CSS from Vite unbuilt. The page then draws a fallback and says so in a
// console nobody has open. web/vite.config.ts is what prevents it; this is
// what notices when that stops being true, at the moment it stops.
func (p *project) verifyAssetsAreFiles() error {
	directory := p.path("internal", "assets", "dist")

	return filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("reading the built assets: %w", err)
		}
		if entry.IsDir() || filepath.Ext(path) != ".css" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		uri := inlinedAsset(string(content))
		if uri == "" {
			return nil
		}

		name := path
		if relative, err := filepath.Rel(p.directory, path); err == nil {
			name = relative
		}
		return fmt.Errorf("%s embeds an asset as %s.%s", name, uri, indent(
			"The daemon's Content-Security-Policy is default-src 'self', which forbids",
			"data:, and only the released binary would show it.",
			"web/vite.config.ts sets assetsInlineLimit to 0 to keep every asset a file:",
			"fix the bundler rather than widening the policy in internal/api/middleware.go."))
	})
}

// emptyEmbeddedAssets clears the directory Vite builds into, sparing the
// committed .gitkeep.
//
// Without that file the assets package's //go:embed does not compile on a
// fresh clone. That is why Vite has emptyOutDir set to false: it cannot make
// the exception, and restoring the file afterwards would leave the working
// tree dirty the day the restore silently fails.
func (p *project) emptyEmbeddedAssets() error {
	directory := p.path("internal", "assets", "dist")

	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("reading %s: %w", directory, err)
	}

	for _, entry := range entries {
		if entry.Name() == ".gitkeep" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(directory, entry.Name())); err != nil {
			return fmt.Errorf("emptying %s: %w", directory, err)
		}
	}
	return nil
}

func listBuiltBinaries(distribution string) error {
	entries, err := os.ReadDir(distribution)
	if err != nil {
		return fmt.Errorf("reading dist/: %w", err)
	}

	fmt.Println()
	for _, entry := range entries {
		details, err := entry.Info()
		if err != nil {
			return fmt.Errorf("reading dist/%s: %w", entry.Name(), err)
		}
		fmt.Printf("  %8.1f MiB  %s\n", float64(details.Size())/(1024*1024), entry.Name())
	}
	return nil
}

// ---------------------------------------------------------------------------
// test
// ---------------------------------------------------------------------------

func runTest(p *project, args []string) error {
	target := "all"
	if len(args) > 0 {
		target = args[0]
		args = args[1:]
	}

	switch target {
	case "go":
		return p.testGo()
	case "web":
		return p.testWeb()
	case "e2e":
		return p.testEndToEnd()
	case "coverage":
		return p.testCoverage()
	case "bench":
		pattern := "."
		if len(args) > 0 {
			pattern = args[0]
		}
		return p.testBench(pattern)
	case "fuzz":
		duration := "60s"
		if len(args) > 0 {
			duration = args[0]
		}
		return p.testFuzz(duration)
	case "soak":
		runs, err := parseSoakRuns(args)
		if err != nil {
			return err
		}
		return p.testSoak(runs)
	case "release":
		return p.testRelease()
	case "all":
		if err := p.testGo(); err != nil {
			return err
		}
		if err := p.testWeb(); err != nil {
			return err
		}
		return p.testEndToEnd()
	default:
		return fmt.Errorf("unknown test target: %q. Expected go, web, e2e, release, bench, fuzz, soak, coverage, or nothing", target)
	}
}

func (p *project) testGo() error {
	info("Go tests")
	// -race: the daemon serves concurrent requests and will soon push events;
	// a data race there would be silent and intermittent. The detector costs a
	// few seconds on a suite this size.
	return p.run("go", append([]string{"test", "-race"}, goPackages...)...)
}

func (p *project) testCoverage() error {
	if err := p.testGoCoverage(); err != nil {
		return err
	}
	return p.testWebCoverage()
}

func (p *project) testGoCoverage() error {
	info("Go coverage")
	profile := p.path(stateDirectory, "coverage-go.out")
	if err := os.MkdirAll(p.path(stateDirectory), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", stateDirectory, err)
	}
	args := append([]string{"test", "-race", "-coverprofile=" + profile, "-covermode=atomic"}, goPackages...)
	if err := p.run("go", args...); err != nil {
		return err
	}
	summary, err := p.capture("go", "tool", "cover", "-func="+profile)
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(summary, "\n") {
		if strings.HasPrefix(line, "total:") {
			info("Go %s", strings.TrimSpace(line))
			break
		}
	}
	return nil
}

func (p *project) testWebCoverage() error {
	info("frontend coverage")
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}
	return p.npm("run", "test:coverage")
}

func (p *project) testWeb() error {
	info("frontend tests")
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}
	return p.npm("run", "test")
}

// testEndToEnd runs Playwright against a daemon and a Vite.
//
// It starts both itself, or reuses the ones `./do up` is already running in
// another terminal. Requiring people to remember to start the stack by hand is
// a guaranteed daily false failure.
func (p *project) testEndToEnd() error {
	info("end-to-end tests")
	if err := p.ensurePlaywrightBrowser(); err != nil {
		return err
	}
	config, err := p.loadConfiguration()
	if err != nil {
		return err
	}

	playwright := func(token string) error {
		command, err := p.tool("npm", "--prefix", "web", "run", "test:e2e")
		if err != nil {
			return err
		}
		command.Env = append(os.Environ(),
			"YAGIT_BASE_URL="+daemonURL,
			"YAGIT_ROOT="+config.root,
			"YAGIT_FIXTURE_DIR="+p.path(stateDirectory, "e2e"),
			// The suite reads nothing under .yagit/: the token reaches it
			// here, whichever stack it is about to drive.
			"YAGIT_TOKEN="+token,
		)
		if err := command.Run(); err != nil {
			return fmt.Errorf("end-to-end tests: %w", err)
		}
		return nil
	}

	// A stack that is still starting is waited for rather than raced: a
	// throwaway stack started during an air rebuild finds both ports taken a
	// second later, and fails on something unrelated to what the tests check.
	answer := p.probeStack()
	if supervisor, alive := p.livingSupervisor(); alive && (answer == nobodyAnswers || answer == daemonAnswers) {
		info("a stack is starting — waiting for it")
		if err := p.waitForStackReady(supervisor, stackReadyTimeout); err != nil {
			return err
		}
		answer = stackAnswers
	}
	switch answer {
	case stackAnswers:
		token, err := p.storedSessionToken()
		if err != nil {
			return err
		}
		info("stack already running, reused")
		return playwright(token)
	case nobodyAnswers:
		// Started below.
	default:
		return fmt.Errorf("%s at %s, and the tests cannot use it. Run ./do down or ./do up first",
			answer, daemonURL)
	}

	// A token of its own, never the checkout's. A workbench tab left open on
	// the checkout's token would otherwise reconnect to the throwaway daemon —
	// its event stream retries on its own — and every click in that tab would
	// land in the fixtures the specs are asserting on.
	token, err := session.Mint()
	if err != nil {
		return err
	}
	environment, err := p.prepareDevStack(config, token)
	if err != nil {
		return err
	}

	log, err := p.openStackLog()
	if err != nil {
		return err
	}
	defer func() {
		if err := log.Close(); err != nil {
			warn("closing %s: %s", log.Name(), err)
		}
	}()

	info("starting a throwaway stack (log: %s)", log.Name())

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupted)

	stack, err := p.startDevStack(environment, log)
	if err != nil {
		return err
	}
	defer stack.stop()

	answers := func() bool { return p.probeStackWith(token) == stackAnswers }
	switch stack.waitUntil(answers, interrupted, stackReadyTimeout) {
	case stackInterrupted:
		return errInterrupted
	case stackDied, stackTimedOut:
		// The stack's output was redirected to a file so it would not drown
		// the test output; when it fails to start, that file is exactly what
		// needs reading, and nobody should have to guess that.
		warn("the throwaway stack did not answer. Its output:")
		printFile(log.Name())
		return errors.New("the end-to-end stack did not start")
	}

	stack.stopOnInterrupt(interrupted)
	return playwright(token)
}

// printFile copies a log to stderr, for the moment a process failed and its
// output is the only explanation there is.
func printFile(path string) {
	contents, err := os.ReadFile(path)
	if err != nil {
		warn("could not read %s: %s", path, err)
		return
	}
	if _, err := os.Stderr.Write(contents); err != nil {
		warn("could not print %s: %s", path, err)
	}
}

// openStackLog creates the throwaway stack's log, locked down before anything
// writes to it.
//
// It captures the daemon's whole output — every git command it runs, and the
// path of every repository it touches — in the directory that also holds the
// token. Creating it under the ambient umask would leave that world-readable.
func (p *project) openStackLog() (*os.File, error) {
	if err := p.ensureStateDirectory(); err != nil {
		return nil, err
	}

	path := p.path(stateDirectory, "e2e-stack.log")
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

// testBench times what the other tests only check the answer of.
//
// Not a gate and not part of `./do test`, for the same reason fuzzing is not:
// a number that depends on the machine it was measured on cannot fail a pull
// request. It is the tool for the question asked before an optimisation and
// again after it, which is the only way to know one was worth committing.
//
// Without -race, unlike every other Go test here. The detector intercepts
// every memory access, and a benchmark run under it measures the detector.
//
// -run '^$' matches no test at all: a benchmark run that also ran the suite
// would spend most of its time somewhere the numbers do not report.
func (p *project) testBench(pattern string) error {
	info("benchmarks matching %s", pattern)

	args := append([]string{"test", "-run", "^$", "-bench", pattern, "-benchmem"}, goPackages...)
	return p.run("go", args...)
}

// testFuzz is a search, not a gate.
//
// It answers "is there an input I did not think of", and most of the time the
// answer is no — which is not a result you can gate a pull request on: the run
// that finds something takes as long as it takes. So it is not part of
// `./do test`.
//
// What IS a gate is already covered: `go test` runs every target's seed corpus
// on each run, and an input found here is written to testdata/fuzz/ and
// committed, which turns it into an ordinary regression test from then on.
//
// The targets are discovered rather than listed: adding one to a package is
// enough, and nothing here has to be kept in sync with the test files.
func (p *project) testFuzz(duration string) error {
	info("fuzzing, %s per target", duration)

	packages, err := p.capture("go", append([]string{"list"}, goPackages...)...)
	if err != nil {
		return err
	}

	found := 0
	for _, packageName := range strings.Fields(packages) {
		listed, err := p.capture("go", "test", packageName, "-list", "^Fuzz")
		if err != nil {
			return err
		}

		for _, target := range strings.Fields(listed) {
			if !strings.HasPrefix(target, "Fuzz") {
				continue
			}
			found++

			info("  %s: %s", packageName[strings.LastIndex(packageName, "/")+1:], target)
			pattern := "^" + target + "$"
			if err := p.run("go", "test", packageName,
				"-run", pattern, "-fuzz", pattern, "-fuzztime", duration); err != nil {
				return err
			}
		}
	}

	if found == 0 {
		return fmt.Errorf("no fuzz target found in %s", strings.Join(goPackages, " "))
	}
	return nil
}

// ---------------------------------------------------------------------------
// lint, audit, fmt
// ---------------------------------------------------------------------------

func runLint(p *project, _ []string) error {
	info("analyzing Go")
	if err := p.run("golangci-lint", "run"); err != nil {
		return err
	}

	if err := p.checkOtherPlatforms(); err != nil {
		return err
	}

	info("analyzing the frontend")
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}
	if err := p.npm("run", "lint"); err != nil {
		return err
	}
	if err := p.npm("run", "fmt:check"); err != nil {
		return err
	}

	// --shell=sh because that is what every shebang here says: this shell has
	// to run under the /bin/sh of a machine that has nothing installed yet.
	//
	// The shim is one of them. The others are the install and uninstall
	// scripts under scripts/, and they are the ones with the most to lose:
	// each is fetched over the network and piped straight into a shell by
	// somebody who has never run this project before, so a mistake in them is
	// the first thing a new user sees and there is no earlier gate than this
	// one. They were outside the lint until they were noticed missing, which
	// is exactly how a front door goes unchecked — nobody adds a linter for a
	// file that did not exist when the linter was written.
	info("analyzing the shell")
	shell := []string{"./do"}
	installers, err := filepath.Glob(p.path("scripts", "*.sh"))
	if err != nil {
		return fmt.Errorf("listing the shell scripts: %w", err)
	}
	shell = append(shell, installers...)
	if err := p.run("shellcheck", append([]string{"--shell=sh"}, shell...)...); err != nil {
		return err
	}

	// A mistake in a workflow file otherwise surfaces only when the workflow
	// runs, which is the worst possible moment to find out.
	info("analyzing the GitHub workflows")
	workflows, err := filepath.Glob(p.path(".github", "workflows", "*.yml"))
	if err != nil {
		return fmt.Errorf("listing the workflows: %w", err)
	}
	if len(workflows) == 0 {
		return fmt.Errorf("no workflow found under .github/workflows")
	}
	if err := p.run("actionlint", workflows...); err != nil {
		return err
	}

	// actionlint reads the workflows as YAML and as shell. zizmor reads them
	// as an attack surface, which is a different question: a tag interpolated
	// into a `run:` block is valid YAML and valid shell, and it is also
	// arbitrary code execution with whatever token the job holds.
	//
	// --offline keeps this gate hermetic: the same answer on a plane as in CI,
	// which is the whole point of a lint. The pedantic persona is on because
	// its one extra rule — a permission without a comment saying why — is this
	// project's own review rule, and a rule a machine checks is a rule that
	// holds.
	//
	// The whole of .github, not just workflows/: dependabot.yml is audited too.
	info("auditing the GitHub workflows and Dependabot")
	if err := p.run("zizmor", "--offline", "--persona", "pedantic", ".github"); err != nil {
		return err
	}

	info("checking agent configuration")
	return agentCheck(p, false)
}

// checkOtherPlatforms type-checks the project for the platforms this machine
// is not.
//
// golangci-lint answers for the host and for nothing else, and `./do build`
// cross-compiles only the daemon. cmd/do has a file per platform, so its
// Windows half is compiled by no local gate at all — which is how a syscall
// constant that does not exist there can pass a green lint and a green test
// run on Linux, and leave a Windows build broken at the entry point.
//
// `go vet` type-checks the packages before it inspects them, and type-checking
// is the whole of what is being asked here. One architecture per platform: the
// code below branches on the operating system and never on the word size.
func (p *project) checkOtherPlatforms() error {
	for _, target := range otherPlatforms() {
		operatingSystem, architecture, _ := strings.Cut(target, "/")
		info("type-checking for %s", operatingSystem)

		command, err := p.tool("go", append([]string{"vet"}, goPackages...)...)
		if err != nil {
			return err
		}
		command.Env = append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS="+operatingSystem,
			"GOARCH="+architecture,
		)

		if err := command.Run(); err != nil {
			return fmt.Errorf("type-checking for %s: %w", operatingSystem, err)
		}
	}
	return nil
}

// otherPlatforms lists one build target per operating system yagit ships to,
// leaving out the one running this command — golangci-lint has already
// answered for that one.
func otherPlatforms() []string {
	seen := map[string]bool{runtime.GOOS: true}

	var targets []string
	for _, target := range buildTargets {
		operatingSystem, _, _ := strings.Cut(target, "/")
		if seen[operatingSystem] {
			continue
		}
		seen[operatingSystem] = true
		targets = append(targets, target)
	}
	return targets
}

// runAudit asks a question runLint deliberately does not: is anything we
// depend on known to be vulnerable *today*?
//
// It is its own command because it is a different kind of check. lint is
// hermetic — same code, same answer, offline, forever — which is what lets it
// be a gate. This one queries a database over the network, and its answer
// changes without the code changing. A green audit this morning and a red one
// this afternoon is the correct outcome, not a flake. Folding the two together
// would make the project's gate depend on the weather, and make it impossible
// to lint on a plane.
func runAudit(p *project, _ []string) error {
	info("auditing Go and the standard library")

	// Reachability, not just version arithmetic: govulncheck reports an
	// advisory only when the vulnerable function can actually be called from
	// this code.
	//
	// Dependabot watches the entry go.mod now has — fsnotify, and the
	// golang.org/x/sys it brings with it. What it does not watch is the
	// standard library, which every line of the daemon runs on and which gets
	// advisories like anything else. This is the only thing that looks at it.
	if err := p.run("govulncheck", goPackages...); err != nil {
		return err
	}

	info("auditing the frontend dependencies")
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}

	// --audit-level=low, meaning everything. A threshold is how a moderate
	// advisory in a build tool sits unread for a year; this tree is small
	// enough that every finding is worth the minute it takes to read.
	return p.npm("audit", "--audit-level=low")
}

func runFormat(p *project, _ []string) error {
	info("reformatting Go")
	if err := p.run("golangci-lint", "fmt"); err != nil {
		return err
	}

	info("reformatting the frontend")
	if err := p.ensureFrontendDependencies(); err != nil {
		return err
	}
	return p.npm("run", "fmt")
}

// ---------------------------------------------------------------------------
// shot, drive, token
// ---------------------------------------------------------------------------

// runShot screenshots a page served by the daemon, so the interface can be
// reviewed without someone sitting in front of a screen — useful on a headless
// development machine, and the only eye available in CI.
func runShot(p *project, args []string) error {
	url := daemonURL + "/"
	if len(args) > 0 {
		url = args[0]
	}
	output := p.path(stateDirectory, "shot.png")
	if len(args) > 1 {
		output = args[1]
	}

	// Only for a stack that answers it: a capture of a 401 page is a PNG that
	// looks exactly like a successful one.
	token, err := p.runningStackToken()
	if err != nil {
		return err
	}

	if err := p.ensurePlaywrightBrowser(); err != nil {
		return err
	}
	if err := p.ensureStateDirectory(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("creating the output directory: %w", err)
	}

	command, err := p.tool("node", "web/scripts/shot.mjs", url, output)
	if err != nil {
		return err
	}
	command.Env = append(os.Environ(), "YAGIT_TOKEN="+token)

	if err := command.Run(); err != nil {
		return fmt.Errorf("screenshotting %s: %w", url, err)
	}
	return nil
}

// runDrive opens the interface in a headless browser and takes commands for it
// on stdin, one per line, printing each answer before reading the next.
//
// It is `./do shot` with the browser left open: where a screenshot answers
// "what does this look like", this answers "what happens when I click that" —
// the only way to work an interface on a machine with no screen. The script it
// reads is a pipe or a tmux pane, so the same command serves an agent driving
// a whole flow and a person stepping through one.
//
// The url argument points it at another daemon — a binary from dist/ on a port
// of its own, which is the only way to see what a release really renders.
func runDrive(p *project, args []string) error {
	url := daemonURL
	if len(args) > 0 {
		url = args[0]
	}

	// The token comes from here rather than from the driver, so that one
	// program asks whether the stack is up, and the sentence when it is not
	// names the command that starts it.
	token, err := p.runningStackToken()
	if err != nil {
		return err
	}

	if err := p.ensurePlaywrightBrowser(); err != nil {
		return err
	}
	if err := p.ensureStateDirectory(); err != nil {
		return err
	}

	shots := p.path(stateDirectory, "shots")
	if err := os.MkdirAll(shots, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", shots, err)
	}

	command, err := p.tool("node", "web/scripts/driver.mjs", url, shots)
	if err != nil {
		return err
	}
	command.Env = append(os.Environ(), "YAGIT_TOKEN="+token)

	// The driver exits non-zero when any command in the script failed, and
	// that status is the whole point for a caller that chains steps: it is
	// returned rather than swallowed.
	if err := command.Run(); err != nil {
		return fmt.Errorf("driving %s: %w", url, err)
	}
	return nil
}

// runToken prints the session token, which makes every endpoint reachable with
// curl:
//
//	curl -H "X-Yagit-Token: $(./do token)" http://127.0.0.1:7420/api/repos
//
// Once the stack has answered it. The token itself is in the checkout whether
// or not a daemon is up, but a secret that opens nothing is worth less than
// the sentence saying the stack is down — or that it runs on another token.
func runToken(p *project, _ []string) error {
	token, err := p.runningStackToken()
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}
