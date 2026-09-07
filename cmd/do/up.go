package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ngsanogo/yagit/internal/protect"
)

const (
	stackPIDFileName      = stateDirectory + "/stack.pid"
	stackChildrenFileName = stateDirectory + "/stack.children"
	devLogFileName        = stateDirectory + "/dev.log"
	stackSuperviseCmd     = "__stack-supervise"
	checkoutEnvVar        = "YAGIT_CHECKOUT"
	stackReadyTimeout     = 120 * time.Second
	stackStopTimeout      = 15 * time.Second
)

// Two questions get asked about the background stack, and they are not the
// same question.
//
//   - Is a supervisor of ours ALIVE? .yagit/stack.pid plus processAlive.
//   - Does the stack ANSWER? probeStack, over HTTP, with the stored token.
//
// A stack that is still compiling is alive and does not answer, and answering
// the first question with the second is what makes `./do down` a no-op during
// startup and lets a second `./do up` spawn a supervisor on top of the first.
// Every decision below names which of the two it is asking.

// runUp starts the development stack, detached by default.
func runUp(p *project, args []string) error {
	options, err := parseStackFlags("up", args)
	if err != nil {
		return err
	}
	return p.startStack(options)
}

// runDev is `up --foreground` under the name people reach for: every line of
// output in this terminal, until Ctrl-C.
func runDev(p *project, args []string) error {
	options, err := parseStackFlags("dev", args)
	if err != nil {
		return err
	}
	options.foreground = true
	return p.startStack(options)
}

// stackOptions is what the flags on `up` and `dev` decide.
type stackOptions struct {
	foreground bool
	restart    bool
	newToken   bool
}

// parseStackFlags reads the flags first, before anything is installed or
// started: a typo must cost a sentence, not a bootstrap.
//
// `dev` takes fewer flags than `up`, and not by oversight. It takes no
// --foreground because it is one, and no --restart because a foreground stack
// always replaces the background one — the two cannot share the ports.
func parseStackFlags(command string, args []string) (stackOptions, error) {
	var options stackOptions
	for _, arg := range args {
		switch {
		case arg == "--new-token":
			options.newToken = true
		case arg == "--restart" && command == "up":
			options.restart = true
		case (arg == "--foreground" || arg == "-f") && command == "up":
			options.foreground = true
		default:
			return stackOptions{}, fmt.Errorf("unknown flag for %s: %q", command, arg)
		}
	}

	// --new-token implies --restart: a daemon already up is holding the
	// secret about to be replaced, and left there it would mean two live
	// secrets and a printed one that opens nothing. Decided here, once, where
	// a test can see it and neither command can forget it.
	if options.newToken {
		options.restart = true
	}
	return options, nil
}

// startStack is what `up` and `dev` share, in the order that keeps a failure
// from leaving two secrets behind: the checkout is made ready, the
// configuration is read, only then is anything stopped, and only after that
// is the token touched. A stop that fails, or a .env that does not parse,
// returns with the old daemon still holding the old token — which is the one
// `./do token` prints, so nothing printed is false.
func (p *project) startStack(options stackOptions) error {
	if err := p.ensureBootstrapped(); err != nil {
		return err
	}
	config, err := p.loadConfiguration()
	if err != nil {
		return err
	}
	if options.foreground {
		return p.runStackForeground(config, options.newToken)
	}
	return p.runStackBackground(config, options)
}

func runDown(p *project, _ []string) error {
	stopped, err := p.stopBackgroundStack()
	if err != nil {
		return err
	}
	if stopped {
		info("stopped")
	} else {
		info("not running")
	}
	return nil
}

func runStatus(p *project, _ []string) error {
	config, err := p.loadConfiguration()
	if err != nil {
		return err
	}

	answer := p.probeStack()
	if answer == stackAnswers {
		return p.printStackCard(config, true)
	}

	// Alive but not answering is its own answer: air is compiling, or Vite is
	// building the module graph. Reporting "not running" there sends people to
	// start a second one.
	_, alive := p.livingSupervisor()
	switch {
	case alive && (answer == nobodyAnswers || answer == daemonAnswers):
		info("starting — %s. ./do logs follows its output", answer)
	case answer == nobodyAnswers:
		info("not running")
	case answer == tokenRefused:
		info("%s at %s. ./do up restarts it on the stored one", answer, daemonURL)
	default:
		info("%s at %s", answer, daemonURL)
	}
	return nil
}

func runLogs(p *project, args []string) error {
	follow := true
	for _, arg := range args {
		switch arg {
		case "--no-follow", "-n":
			follow = false
		default:
			return fmt.Errorf("unknown flag for logs: %q", arg)
		}
	}

	path := p.path(devLogFileName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log yet — run ./do up first")
		}
		return fmt.Errorf("reading %s: %w", devLogFileName, err)
	}
	return tailFile(os.Stdout, path, follow)
}

// runStackSupervise is the detached child that owns air and Vite until it
// exits. It is invoked as `./do __stack-supervise`, not from the help text.
//
// It writes to the streams it was given, and opens no log of its own. `./do
// up` points both at .yagit/dev.log before starting this process, which is
// what puts a failure that happens BEFORE the stack starts — an unreadable
// YAGIT_ROOT, a missing npm, a port already held — into the log the parent
// prints when its wait runs out. Opening the log here would truncate it and
// then write that one line to a stream no terminal is attached to.
func runStackSupervise(p *project) error {
	config, err := p.loadConfiguration()
	if err != nil {
		return err
	}
	// The parent resolved this a moment ago, so the read below finds a usable
	// file and says nothing. Anything it would have had to say — a token
	// replaced because the file was corrupt — was said there, on a terminal.
	token, err := p.sessionToken()
	if err != nil {
		return err
	}
	environment, err := p.prepareDevStack(config, token)
	if err != nil {
		return err
	}

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupted)

	stack, err := p.startDevStack(environment, os.Stdout)
	if err != nil {
		return err
	}
	defer stack.stop()

	stack.waitForFirstExit(interrupted)
	return nil
}

func (p *project) runStackForeground(config configuration, newToken bool) error {
	// A foreground stack replaces the background one. The two cannot share
	// the ports, and the card `./do up` prints offers this command as the way
	// to watch the same stack from a terminal — not as a way to start a
	// second one beside it.
	stopped, err := p.stopBackgroundStack()
	if err != nil {
		return err
	}
	if stopped {
		info("stopped the background stack, to run it here instead")
	}

	if newToken {
		if err := p.replaceSessionToken(); err != nil {
			return err
		}
	}
	token, err := p.sessionToken()
	if err != nil {
		return err
	}
	environment, err := p.prepareDevStack(config, token)
	if err != nil {
		return err
	}

	info("Ctrl-C to stop")

	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupted)

	stack, err := p.startDevStack(environment, os.Stdout)
	if err != nil {
		return err
	}
	defer stack.stop()

	switch stack.waitUntil(p.stackResponds, interrupted, stackReadyTimeout) {
	case stackReady:
		// The same stamp `./do up` records, for the same reason: a `./do up`
		// typed in another terminal has to be able to tell this stack apart
		// from one whose dependencies moved underneath it.
		if err := p.recordStamp(stackStamp); err != nil {
			warn("recording the dependency stamp: %s", err)
		}
		if err := p.printStackCard(config, false); err != nil {
			warn("%s", err)
		}
	case stackInterrupted:
		return errInterrupted
	case stackDied:
		return errors.New("the stack stopped before it answered; the output above says why")
	case stackTimedOut:
		// Not a failure yet: the output is on this screen, and whoever is
		// watching it can decide. It is said, so the missing card is not a
		// mystery.
		warn("the stack has not answered within %s; the output above may say why", stackReadyTimeout)
	}

	if interruptedByUser := stack.waitForFirstExit(interrupted); interruptedByUser {
		return errInterrupted
	}
	return errors.New("the stack stopped on its own; the output above says why")
}

func (p *project) runStackBackground(config configuration, options stackOptions) error {
	if options.restart {
		// Unconditionally, and before anything else is asked. --restart is
		// typed about a stack that is behaving badly, which is exactly the
		// stack that fails an "is it answering?" test.
		if _, err := p.stopBackgroundStack(); err != nil {
			return err
		}
	} else if done, err := p.settleRunningStack(config); done || err != nil {
		return err
	}

	// Only now, with nothing of ours holding the old token. Rotating first
	// and stopping second left the old daemon serving the old token whenever
	// the stop failed, under a notice claiming every browser had been logged
	// out — and `./do token` printing a secret the running daemon refused.
	if options.newToken {
		if err := p.replaceSessionToken(); err != nil {
			return err
		}
	}
	// Resolved here, in the process that has the terminal. The supervisor
	// reads the same file a moment later and finds it usable; a file that had
	// to be replaced is replaced here, where the warning saying so can be
	// read, rather than in a log nobody is following.
	if _, err := p.sessionToken(); err != nil {
		return err
	}

	info("starting…")
	started := time.Now()

	supervisor, err := p.spawnStackSupervisor()
	if err != nil {
		return err
	}
	if err := p.writeStackPID(supervisor); err != nil {
		if stopErr := p.stopProcess(supervisor); stopErr != nil {
			warn("could not stop the supervisor after failing to record its pid: %s", stopErr)
		}
		return err
	}

	return p.reportStack(config, supervisor, started)
}

// settleRunningStack decides what a plain `./do up` does about whatever is
// already there. It reports done when there is nothing left for the caller to
// start: the stack was already up and its card has been printed, or a stack
// was already starting and has been waited for.
func (p *project) settleRunningStack(config configuration) (done bool, err error) {
	answer := p.probeStack()
	supervisor, supervisorAlive := p.livingSupervisor()

	switch {
	case answer == stackAnswers && !p.stampIsCurrent(stackStamp):
		info("dependencies changed — restarting")

	case answer == stackAnswers:
		return true, p.printStackCard(config, true)

	case answer == tokenRefused:
		// The file is the checkout's token, and a daemon that refuses it was
		// started on one the file no longer holds: deleted, or edited by
		// hand. It is restarted on the stored one. stopBackgroundStack refuses
		// to touch a daemon it did not start, so a stranger on the port is
		// reported rather than killed.
		info("%s — restarting it on the stored one", answer)

	case answer == strangerAnswers:
		return true, fmt.Errorf("%s at %s.%s", answer, daemonURL, indent(
			"Stop it where it was started, or free port "+strconv.Itoa(daemonPort)+" by hand."))

	case supervisorAlive:
		// Alive and not answering: it is still booting. Starting a second
		// supervisor here would leave two of them fighting over both ports,
		// and the pid file naming only the newer one — which is how the first
		// becomes unstoppable by `./do down` for the rest of its life.
		info("a stack is already starting — waiting for it")
		return true, p.reportStack(config, supervisor, time.Now())

	case answer == daemonAnswers:
		// The daemon is up, the frontend is not, and no supervisor owns
		// either: what a supervisor killed outright leaves behind. The pids
		// it recorded are what stopBackgroundStack falls back on.
		info("%s, and no supervisor owns it — restarting", answer)

	default:
		return false, nil
	}

	_, err = p.stopBackgroundStack()
	return false, err
}

// reportStack waits for a supervisor's stack to answer, then prints where it
// is. A stack that never answers is stopped rather than left half-started, and
// its log is printed rather than left to be found.
func (p *project) reportStack(config configuration, supervisor int, started time.Time) error {
	if err := p.waitForStackReady(supervisor, stackReadyTimeout); err != nil {
		p.printDevLogTail(40)
		if _, stopErr := p.stopBackgroundStack(); stopErr != nil {
			warn("could not stop the stack after a failed start: %s", stopErr)
		}
		return err
	}

	info("ready in %s", time.Since(started).Round(time.Second))

	if err := p.recordStamp(stackStamp); err != nil {
		warn("recording the dependency stamp: %s", err)
	}

	return p.printStackCard(config, false)
}

// waitForStackReady polls until the stack answers, or until the supervisor
// dies, or until the deadline passes.
//
// Watching the supervisor is what turns a stack that failed to start into an
// answer in a second rather than in two minutes: YAGIT_ROOT pointing at a
// directory that no longer exists is reported by the supervisor immediately,
// and there is nothing left to wait for once it has exited.
func (p *project) waitForStackReady(supervisor int, within time.Duration) error {
	deadline := time.Now().Add(within)
	for {
		if p.stackResponds() {
			return nil
		}
		if !processAlive(supervisor) {
			return errors.New("the stack supervisor exited before the stack answered")
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("the stack did not answer within %s", within)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// spawnStackSupervisor starts the detached process that owns air and Vite, and
// hands it the stack log to write to.
func (p *project) spawnStackSupervisor() (int, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("finding this program: %w", err)
	}

	log, err := p.openDevLog()
	if err != nil {
		return 0, err
	}
	// Closed as soon as the child holds it: exec gives the child a descriptor
	// of its own at Start, and nothing in this process writes to the log.
	defer func() {
		if err := log.Close(); err != nil {
			warn("closing %s: %s", devLogFileName, err)
		}
	}()

	command := exec.Command(executable, stackSuperviseCmd)
	command.Dir = p.directory
	command.Env = append(os.Environ(), checkoutEnvVar+"="+p.directory)
	command.Stdin = nil
	command.Stdout = log
	command.Stderr = log
	detachProcess(command)

	if err := command.Start(); err != nil {
		return 0, fmt.Errorf("starting the stack supervisor: %w", err)
	}

	// Reaped here, and not for the exit status: on a successful start this
	// process is long gone before the supervisor stops, and on a failed one
	// everything the supervisor had to say is already in the stack log, which
	// the caller prints. What the reaping is for is that a supervisor which
	// dies during startup must actually leave the process table — a zombie
	// still answers `kill -0`, and waitForStackReady would take it for a stack
	// that is merely slow.
	//
	// A supervisor that dies without `./do down` leaves a stale pid file; the
	// next `./do up` clears it. Removing the file here would race `./do down`,
	// which reads the pid first.
	go func() {
		//nolint:errcheck // see the note above: the exit status has no reader.
		command.Wait()
	}()

	return command.Process.Pid, nil
}

// stopBackgroundStack stops the stack and reports whether there was one.
//
// The supervisor first, because stopping it stops the two processes it owns.
// When it is gone but its children are not — killed with -9, or taken by the
// OOM killer — the pids it recorded are what is left to stop, and the ports
// they hold are the reason the next `./do up` would fail to bind.
func (p *project) stopBackgroundStack() (stopped bool, err error) {
	if supervisor, alive := p.livingSupervisor(); alive {
		if err := p.stopProcess(supervisor); err != nil {
			return false, err
		}
		if err := waitForProcessExit(supervisor, stackStopTimeout); err != nil {
			// Its own shutdown is bounded — it signals air and Vite and waits
			// ten seconds for them — so getting here means the supervisor
			// itself is wedged and never ran its handler. Nothing this program
			// can send will reach it, and saying which process to end is more
			// use than repeating the timeout.
			return false, fmt.Errorf("%w.%s", err, indent(
				"The stack supervisor did not answer the signal to stop.",
				"End process "+strconv.Itoa(supervisor)+" by hand, then run ./do down again:",
				"the pids it recorded are enough to stop what it leaves behind."))
		}
		p.removeStackPID()
		p.removeStackChildren()
		return true, nil
	}

	p.removeStackPID()

	// Nothing answering means nothing to stop. It is also what keeps a stale
	// list of pids from being signalled: those numbers were reused by the
	// system long ago, and the only evidence that they are still the stack is
	// that something is still up on its port.
	answer := p.probeStack()
	if answer == nobodyAnswers {
		p.removeStackChildren()
		return false, nil
	}

	orphans := p.livingStackChildren()
	if len(orphans) == 0 {
		return false, fmt.Errorf("%s at %s, and ./do did not start it.%s",
			answer, daemonURL, indent(
				"No supervisor of ours is running and no process it recorded is left.",
				"Stop it where it was started, or free ports "+
					strconv.Itoa(daemonPort)+" and "+strconv.Itoa(vitePort)+" by hand."))
	}

	info("the supervisor is gone; stopping the %d processes it left", len(orphans))
	for _, pid := range orphans {
		if err := stopProcessTree(pid); err != nil {
			return false, err
		}
	}
	if err := p.waitForStackDown(stackStopTimeout); err != nil {
		return false, err
	}
	p.removeStackChildren()
	return true, nil
}

func (p *project) stopProcess(pid int) error {
	return signalProcess(pid, syscall.SIGTERM)
}

// waitForProcessExit blocks until a pid leaves the process table.
//
// The supervisor stops air and Vite before it exits, so its own disappearance
// is the signal that the ports are free — a stronger one than an HTTP probe,
// which a stack that never answered would pass instantly.
func waitForProcessExit(pid int, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("process %d did not stop within %s", pid, within)
}

func (p *project) waitForStackDown(within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if p.probeStack() == nobodyAnswers {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("the stack did not stop in time")
}

// ---------------------------------------------------------------------------
// What is running, on disk
// ---------------------------------------------------------------------------

func (p *project) writeStackPID(pid int) error {
	return p.writeStateFile(stackPIDFileName, []byte(strconv.Itoa(pid)+"\n"))
}

func (p *project) readStackPID() (int, error) {
	raw, err := os.ReadFile(p.path(stackPIDFileName))
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", stackPIDFileName, err)
	}
	return pid, nil
}

func (p *project) removeStackPID() {
	removeIfPresent(p.path(stackPIDFileName), stackPIDFileName)
}

// livingSupervisor reports the recorded supervisor, and whether it still
// exists. This is the "is a stack of ours running?" question, and the only
// thing that answers it.
func (p *project) livingSupervisor() (pid int, alive bool) {
	pid, err := p.readStackPID()
	if err != nil {
		return 0, false
	}
	return pid, processAlive(pid)
}

// writeStackChildren records the pids of air and Vite.
//
// They are each a process-group leader, so these are the numbers `./do down`
// signals when the supervisor that owned them is no longer there to do it.
func (p *project) writeStackChildren(pids []int) error {
	lines := make([]string, 0, len(pids))
	for _, pid := range pids {
		lines = append(lines, strconv.Itoa(pid))
	}
	return p.writeStateFile(stackChildrenFileName, []byte(strings.Join(lines, "\n")+"\n"))
}

// livingStackChildren returns the recorded pids that still exist.
func (p *project) livingStackChildren() []int {
	raw, err := os.ReadFile(p.path(stackChildrenFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			warn("reading %s: %s", stackChildrenFileName, err)
		}
		return nil
	}

	var alive []int
	for field := range strings.FieldsSeq(string(raw)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			warn("%s holds %q, which is not a pid", stackChildrenFileName, field)
			continue
		}
		if processAlive(pid) {
			alive = append(alive, pid)
		}
	}
	return alive
}

func (p *project) removeStackChildren() {
	removeIfPresent(p.path(stackChildrenFileName), stackChildrenFileName)
}

// ---------------------------------------------------------------------------
// The stack log
// ---------------------------------------------------------------------------

func (p *project) openDevLog() (*os.File, error) {
	if err := p.ensureStateDirectory(); err != nil {
		return nil, err
	}
	path := p.path(devLogFileName)
	log, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", devLogFileName, err)
	}
	if err := protect.OwnerOnly(path); err != nil {
		if closeErr := log.Close(); closeErr != nil {
			warn("closing %s after a failed lockdown: %s", path, closeErr)
		}
		return nil, err
	}
	return log, nil
}

func (p *project) printDevLogTail(lines int) {
	path := p.path(devLogFileName)
	content, err := os.ReadFile(path)
	if err != nil {
		warn("could not read %s: %s", devLogFileName, err)
		return
	}

	warn("last lines of %s:", devLogFileName)
	all := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	for _, line := range all[start:] {
		fmt.Fprintf(os.Stderr, "       %s\n", line)
	}
}

// tailFile writes a file to a stream, and keeps writing what is appended to it.
//
// It follows the file across a restart. `./do up` truncates the log when it
// starts a supervisor, and a reader that kept its old offset would sit past
// the end of a file that is now empty — silent for the rest of the session,
// with no error to explain it. A file shorter than the offset can only have
// been truncated, so the read starts again from the top.
func tailFile(writer io.Writer, path string, follow bool) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			warn("closing %s: %s", path, err)
		}
	}()

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			if _, err := io.WriteString(writer, line); err != nil {
				return err
			}
		}
		if readErr == nil {
			continue
		}
		if !errors.Is(readErr, io.EOF) {
			return readErr
		}
		if !follow {
			return nil
		}
		if err := rewindIfTruncated(file, reader); err != nil {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// rewindIfTruncated seeks back to the start when the file has shrunk below the
// point already read. It is called at end of file, where the buffered reader
// has handed over everything it holds and the descriptor's offset is therefore
// the offset actually consumed.
func rewindIfTruncated(file *os.File, reader *bufio.Reader) error {
	offset, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("reading the position in %s: %w", file.Name(), err)
	}

	status, err := file.Stat()
	if err != nil {
		return fmt.Errorf("reading %s: %w", file.Name(), err)
	}
	if status.Size() >= offset {
		return nil
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewinding %s: %w", file.Name(), err)
	}
	reader.Reset(file)
	return nil
}

// ---------------------------------------------------------------------------
// Reporting
// ---------------------------------------------------------------------------

func (p *project) printStackCard(config configuration, alreadyRunning bool) error {
	token, err := p.storedSessionToken()
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("no session token in %s, and a stack is up: it was started on a token this checkout no longer holds", sessionTokenFileName)
	}

	url, err := browserURL(config)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  %syagit%s   %s\n", colorBold, colorOff, url)
	fmt.Printf("  %sToken%s    %s   (paste on first visit)\n", colorBold, colorOff, token)
	fmt.Println()
	if alreadyRunning {
		fmt.Printf("  %s(already running)%s\n", colorDim, colorOff)
		fmt.Println()
	}
	fmt.Printf("  %s./do down%s    stop\n", colorDim, colorOff)
	fmt.Printf("  %s./do logs%s    follow output\n", colorDim, colorOff)
	fmt.Printf("  %s./do dev%s     run in the foreground\n", colorDim, colorOff)
	fmt.Println()
	return nil
}

func browserURL(config configuration) (string, error) {
	if _, err := listenAddress(config.publicHost, config.listenAll); err != nil {
		return "", err
	}
	host := config.publicHost
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s/", net.JoinHostPort(host, strconv.Itoa(daemonPort))), nil
}

func locateProjectForSupervise() (*project, error) {
	if checkout := os.Getenv(checkoutEnvVar); checkout != "" {
		if _, err := os.Stat(filepath.Join(checkout, "go.mod")); err != nil {
			return nil, fmt.Errorf("%s=%q is not a yagit checkout: %w", checkoutEnvVar, checkout, err)
		}
		return &project{directory: checkout}, nil
	}
	return locateProject()
}
