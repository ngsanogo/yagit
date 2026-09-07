package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The development stack is the daemon (under air, for hot reload) and Vite,
// started together and stopped together.
//
// If either one dies the other has no reason to live: a daemon with no
// interface, or an interface with no daemon, both look fine while half the
// system is missing.

// devStack owns the two processes for as long as they run.
type devStack struct {
	// project is where the record of what is running lives: the file of child
	// pids that `./do down` falls back on when this process is no longer
	// around to be asked to stop them itself.
	project *project

	commands []*exec.Cmd

	// exits carries one value per process, in the order they finish. It is
	// buffered to the number of processes so the waiting goroutines never
	// block, and so nothing leaks when the stack is stopped without anyone
	// having read from it.
	exits chan error

	// collected counts the exits already read, so stopping knows how many are
	// still outstanding.
	collected int

	// stopped guards stop, which two paths can reach at once: the normal one
	// on the way out, and the handler that runs when the terminal interrupts
	// a long command.
	stopped sync.Once
}

// prepareDevStack does everything that has to happen before the two processes
// start, and returns the environment they start with.
//
// The token is handed in rather than read here, because the three callers
// want three different things done with it. `./do up` resolves it in the
// parent, so a warning about a replaced file lands on the terminal and not in
// a log nobody is following; `./do dev` resolves it in place; and the
// end-to-end tests mint a throwaway one, so a tab left open on the checkout's
// token cannot join a test run.
func (p *project) prepareDevStack(config configuration, token string) ([]string, error) {
	if err := p.ensureFrontendDependencies(); err != nil {
		return nil, err
	}
	return p.daemonEnvironment(config, token)
}

// startDevStack launches air and Vite. The caller decides where their output
// goes: the terminal for `do dev`, a file for the tests.
func (p *project) startDevStack(environment []string, output io.Writer) (*devStack, error) {
	stack := &devStack{project: p, exits: make(chan error, 2)}

	launches := [][]string{
		{"air", "-c", ".air.toml"},
		{"npm", "--prefix", "web", "run", "dev"},
	}

	for _, launch := range launches {
		command, err := p.tool(launch[0], launch[1:]...)
		if err != nil {
			stack.stop()
			return nil, err
		}

		command.Env = environment
		command.Stdin = nil
		command.Stdout = output
		command.Stderr = output
		isolateProcessTree(command)

		if err := command.Start(); err != nil {
			stack.stop()
			return nil, fmt.Errorf("starting %s: %w", launch[0], err)
		}

		stack.commands = append(stack.commands, command)
		go func() { stack.exits <- command.Wait() }()
	}

	// Recorded here rather than in `./do up`, because this is the one place
	// that knows the pids, and all three ways of starting a stack — up, dev,
	// and the throwaway stack the end-to-end tests bring up — come through it.
	// Only `./do down`'s ability to clean up after a supervisor that was
	// killed outright depends on the file, so failing to write it is a
	// warning and not a refusal to start.
	if err := p.writeStackChildren(stack.pids()); err != nil {
		warn("%s", err)
	}

	return stack, nil
}

// pids names the processes this stack owns.
func (s *devStack) pids() []int {
	var pids []int
	for _, command := range s.commands {
		if command.Process != nil {
			pids = append(pids, command.Process.Pid)
		}
	}
	return pids
}

// stackOutcome is how a wait on a freshly started stack ended.
type stackOutcome int

const (
	// stackReady: the stack answers.
	stackReady stackOutcome = iota

	// stackDied: one of the two processes exited before the stack answered —
	// a port already held, a missing tool, a daemon that refused its token.
	stackDied

	// stackInterrupted: the terminal interrupted the wait.
	stackInterrupted

	// stackTimedOut: nothing exited, and nothing answered either.
	stackTimedOut
)

// waitUntil polls a condition, and stops early when one of the two processes
// exits or the terminal interrupts.
//
// Early is the point. A port already held fails Vite within a second, and a
// wait that only watched the clock would sit out the whole two minutes with
// the reason already printed and scrolled past.
func (s *devStack) waitUntil(answers func() bool, interrupted <-chan os.Signal, within time.Duration) stackOutcome {
	deadline := time.Now().Add(within)
	for {
		if answers() {
			return stackReady
		}
		select {
		case <-s.exits:
			s.collected++
			return stackDied
		case <-interrupted:
			return stackInterrupted
		case <-time.After(250 * time.Millisecond):
		}
		if !time.Now().Before(deadline) {
			return stackTimedOut
		}
	}
}

// waitForFirstExit blocks until one of the two processes stops, or until the
// terminal interrupts this one. It reports which of the two happened.
func (s *devStack) waitForFirstExit(interrupted <-chan os.Signal) (byUser bool) {
	select {
	case <-s.exits:
		s.collected++
		info("one of the two processes stopped; stopping the other")
		return false
	case <-interrupted:
		return true
	}
}

// stopOnInterrupt stops the stack and leaves, for the commands that spend
// their time waiting on a child rather than on a signal.
//
// Without it the terminal's Ctrl-C reaches this process and its default
// handler ends it on the spot — before any deferred cleanup runs, and while
// air and Vite sit in process groups of their own where the terminal's signal
// never reached them. They would survive, holding both ports.
func (s *devStack) stopOnInterrupt(interrupted <-chan os.Signal) {
	go func() {
		<-interrupted
		warn("interrupted; stopping the development stack")
		s.stop()
		os.Exit(exitInterrupted)
	}()
}

// stop terminates both process trees and waits for them.
//
// Errors are reported rather than returned: this runs on the way out, where
// there is nothing left to abort, and a stack that could not be stopped is
// something the next `./do dev` will trip over. Saying so now is the only
// chance to explain it.
func (s *devStack) stop() {
	s.stopped.Do(s.terminate)
}

func (s *devStack) terminate() {
	// Before the processes are gone rather than after: whatever happens next,
	// the file must not outlive them and send a later `./do down` at pids the
	// system has since handed to somebody else.
	s.project.removeStackChildren()

	for _, command := range s.commands {
		if err := terminateProcessTree(command); err != nil {
			warn("%s", err)
		}
	}

	// Wait rather than return immediately: the ports are only free once the
	// children are actually reaped, and the next command in the same shell
	// session may well want them.
	deadline := time.After(10 * time.Second)
	for s.collected < len(s.commands) {
		select {
		case <-s.exits:
			s.collected++
		case <-deadline:
			warn("a development process did not stop within 10 s; "+
				"ports %d or %d may still be held", daemonPort, vitePort)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Probing
// ---------------------------------------------------------------------------

// stackAnswer is what asking the daemon's port found there.
//
// Asked of the port, never of a file. The daemon used to write a token file
// at start and remove it at stop, and that file's presence was read as "a
// daemon is up" — which held until the first `kill -9`, after which `./do
// token` handed out a secret for a daemon that was gone and `./do shot`
// launched a browser at nothing. A file outlives the process that wrote it.
// An HTTP answer does not.
type stackAnswer int

const (
	// nobodyAnswers: the connection was refused. No daemon on the port.
	nobodyAnswers stackAnswer = iota

	// daemonAnswers: the daemon answered the stored token, and the frontend
	// behind it did not — Vite is still building the module graph, or died.
	daemonAnswers

	// tokenRefused: a yagit daemon answered, and refused the stored token.
	// Ours, started before the file was deleted or edited — or somebody
	// else's.
	tokenRefused

	// strangerAnswers: something answered that is not a yagit daemon.
	strangerAnswers

	// stackAnswers: the daemon and the frontend both answered the token.
	stackAnswers
)

// String says what was found, for the sentences that have to name it.
func (a stackAnswer) String() string {
	switch a {
	case nobodyAnswers:
		return "nothing answers"
	case daemonAnswers:
		return "the daemon answers and the frontend does not yet"
	case tokenRefused:
		return "a daemon answers and refuses the token in " + sessionTokenFileName
	case strangerAnswers:
		return "something answers that is not a yagit daemon"
	case stackAnswers:
		return "the stack answers"
	default:
		return fmt.Sprintf("stackAnswer(%d)", int(a))
	}
}

// probeStack asks the daemon's port what is there, presenting the stored
// token only after a challenge proves the listener refuses an unauthenticated
// request.
func (p *project) probeStack() stackAnswer {
	token, err := p.storedSessionToken()
	if err != nil {
		// Whoever needs the token to use it reports this. The probe only asks
		// whether the port opens to it, and with no token it does not — which
		// is the right answer about whatever is running there.
		token = ""
	}
	return p.probeStackWith(token)
}

// probeStackWith asks the daemon's port what is there, presenting a token of
// the caller's — the throwaway one the end-to-end tests start their stack on.
//
// Both halves are asked. /api/health alone proves the daemon is listening,
// not that Vite started behind it — and an end-to-end test that opens the
// page one second too early gets a 502 unrelated to what it was checking.
func (p *project) probeStackWith(token string) stackAnswer {
	if answer, ok := p.challengeDaemon(); !ok {
		return answer
	}

	status, body, err := p.fetchAt(daemonURL, token, "/api/health", 5*time.Second)
	switch {
	case err != nil:
		return nobodyAnswers
	case status == http.StatusUnauthorized || status == http.StatusTooManyRequests:
		// 429 is the same refusal, counted: the daemon rate-limits requests
		// arriving without a valid token, and a probe is one of them.
		return tokenRefused
	case status != http.StatusOK || !isHealthPayload(body):
		return strangerAnswers
	}

	// The frontend gets the longer deadline: Vite compiles the module graph on
	// the first request, and on a cold checkout that is seconds, not
	// milliseconds.
	if !p.probe(token, "/", 15*time.Second) {
		return daemonAnswers
	}
	return stackAnswers
}

// challengeDaemon asks /api/health with no token. A yagit daemon refuses
// that; anything that answers 200 (or anything other than a refusal) is a
// stranger, and the stored token is never sent to it.
//
// Without the challenge, a process that binds 127.0.0.1:7420 while the stack
// is down and answers 200 to everything would receive the long-lived token on
// the very first `./do status` or `./do token` — and on a multi-user host the
// loopback is shared, not private.
func (p *project) challengeDaemon() (answer stackAnswer, authentic bool) {
	status, body, err := p.fetchAt(daemonURL, "", "/api/health", 5*time.Second)
	switch {
	case err != nil:
		return nobodyAnswers, false
	case status == http.StatusUnauthorized || status == http.StatusTooManyRequests:
		return 0, true
	case status == http.StatusOK && isHealthPayload(body):
		// Serves health without a credential. That is not yagit's auth model.
		return strangerAnswers, false
	default:
		return strangerAnswers, false
	}
}

// isHealthPayload recognises the daemon's own answer: status and a version
// field. A bare `{"status":"ok"}` is too easy for a squatter to forge; the
// version string is what every real health response carries.
func isHealthPayload(body []byte) bool {
	var payload struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	return json.Unmarshal(body, &payload) == nil &&
		payload.Status == "ok" &&
		payload.Version != ""
}

// stackResponds reports whether the whole stack answers the stored token.
func (p *project) stackResponds() bool {
	return p.probeStack() == stackAnswers
}

// runningStackToken returns the stored token once the stack has answered it.
//
// For the commands that hand the token to a browser or a curl: a token the
// running daemon refuses, or one for a daemon that is not there, would only
// send them to a 401 — and the sentence that says which is worth more than
// the secret.
func (p *project) runningStackToken() (string, error) {
	switch answer := p.probeStack(); answer {
	case stackAnswers:
		return p.storedSessionToken()
	case nobodyAnswers:
		if _, alive := p.livingSupervisor(); alive {
			return "", errors.New("the stack is starting and does not answer yet. ./do logs follows its output")
		}
		return "", errors.New("the stack is not running. Run ./do up")
	case daemonAnswers:
		return "", fmt.Errorf("%s. ./do logs follows its output", answer)
	case tokenRefused:
		return "", fmt.Errorf("%s at %s.%s", answer, daemonURL, indent(
			"./do up restarts the stack on the stored token."))
	default:
		return "", fmt.Errorf("%s at %s. Stop it where it was started, or free port %d by hand",
			answer, daemonURL, daemonPort)
	}
}

func (p *project) probe(token, path string, timeout time.Duration) bool {
	status, _, err := p.fetchAt(daemonURL, token, path, timeout)
	return err == nil && status == http.StatusOK
}

func (p *project) probeAt(baseURL, token, path string, timeout time.Duration) bool {
	status, _, err := p.fetchAt(baseURL, token, path, timeout)
	return err == nil && status == http.StatusOK
}

func (p *project) fetchAt(baseURL, token, path string, timeout time.Duration) (int, []byte, error) {
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		request.Header.Set("X-Yagit-Token", token)
	}

	// Never follow redirects: Go's default client forwards custom headers
	// such as X-Yagit-Token across hosts, and a squatter that answers 302
	// would otherwise bounce the checkout's secret off the loopback.
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			warn("closing the probe response: %s", err)
		}
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, err
	}
	return response.StatusCode, body, nil
}
