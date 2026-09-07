// Package git runs the git binary and turns its machine-readable output into
// Go structures.
//
// This is the project's only boundary with git: no other package spawns a
// subprocess. Arguments are always passed as a slice, never inside a string a
// shell would interpret — a branch name containing `;` must not be able to
// trigger anything.
//
// This package depends on no other yagit package. Dependencies run one way:
// git ← repo ← api ← web.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// defaultTimeout bounds a local git command. They return instantly; the
// deadline exists so that one which does not cannot hold a request open
// forever.
const defaultTimeout = 30 * time.Second

// waitDelay bounds the wait for git's output pipes once the deadline has
// fired and git itself has been signalled.
//
// A timeout that kills git is not a timeout that ends the request.
// exec.CommandContext signals the process when the context is done, but Run
// returns only once the pipes it handed out are closed — and git hands them to
// its own children. `git push` over SSH starts ssh; a fetch starts the
// credential helper the user configured. Killing git leaves those holding the
// write end of stdout, so a request whose deadline passed ten minutes ago goes
// on holding a goroutine, two buffers and a connection until a grandchild
// nobody is waiting for decides to exit — which for an ssh stalled on a dead
// TCP connection is not a bounded amount of time.
//
// os/exec closes the pipes itself once this elapses, so Run comes back and the
// caller gets the deadline error it was owed. Five seconds is room for a
// well-behaved child to notice its parent is gone and flush what it had; past
// that, whatever it still holds is worth less than the request.
const waitDelay = 5 * time.Second

// networkTimeout bounds a command that talks to another machine.
//
// Thirty seconds is the wrong number for those, and not by a little: cloning
// or fetching a repository of any size is minutes of legitimate work, and a
// deadline that cuts it off leaves a half-written pack and a user told their
// network failed when it was the daemon that gave up. Ten minutes is long
// enough for the work and short enough that a connection which has silently
// died still ends in an answer rather than in a request nobody ever gets back.
//
// The deadline stays because the alternative is not "no limit" but "until the
// process is killed". GIT_TERMINAL_PROMPT=0 means git never waits for input
// that will not come, so what is left to bound is a network that stopped
// answering without closing — the case TCP itself can take hours to notice.
const networkTimeout = 10 * time.Minute

// networkIdle bounds how long a transfer may say nothing before it is treated
// as dead.
//
// The four commands that reach another machine use this INSTEAD of
// networkTimeout, because their honest length is the size of the repository
// divided by somebody's line. Four gigabytes on a domestic connection is
// fifty-five minutes of work that git reports progress through the whole of,
// and a ten-minute ceiling turned that into a repository written most of the
// way and a message blaming the user's network.
//
// Five minutes of complete silence, not one. A server counting objects for a
// very large repository is quiet for a long time before the first byte moves,
// and cutting that off would replace one wrong refusal with another. What it
// still catches is the case networkTimeout was written for: a connection that
// stopped answering without closing, which TCP itself can take hours to
// notice — and it now catches it in five minutes rather than ten.
const networkIdle = 5 * time.Minute

// rewriteTimeout bounds a local command that rewrites the work tree.
//
// Thirty seconds is the wrong number for these too, and being local is what
// makes it wrong rather than what makes it safe: `git rebase --continue`
// replays every commit that is left, `git rebase --abort` checks out a whole
// tree, and either can run a pre-commit hook per commit. None of that is a
// hang, and none of it belongs to the deadline meant for commands that return
// instantly.
//
// What the wrong deadline costs here is not a slow answer but a broken
// repository: the process is killed with SIGKILL in the middle of a step,
// leaving a half-replayed sequence and possibly an index.lock, while the user
// is told their command timed out. Ten minutes, for the same reason
// networkTimeout is ten: long enough for the work, short enough that a request
// still ends in an answer.
const rewriteTimeout = 10 * time.Minute

// historyTimeout bounds the walk that draws the graph.
//
// Thirty seconds is the wrong number here for the reason it is wrong for a
// rebase, and the reason is in defaultTimeout's own words: that deadline is for
// commands which "return instantly". This one does not. It reads every commit
// in the repository in one pass, because a commit's column follows from every
// commit above it and so the walk cannot be paginated
// (docs/adr/0012-lanes-are-assigned-in-the-daemon.md) — the pagination the
// interface shows is over an answer already computed.
//
// What the wrong deadline costs is the largest repositories, which are exactly
// the ones a graphical client is bought for: a monorepo whose walk takes forty
// seconds does not open slowly, it fails, and what the user is told is
// "command timed out after 30s" — a sentence about the daemon that reads as a
// sentence about their repository.
//
// Two minutes, not ten. A walk is not a transfer over somebody else's network
// and not a rebase running the user's hooks; it is bounded work on a local
// disk, and past two minutes the honest answer is that this repository is too
// large for yagit to draw rather than that it needs longer.
const historyTimeout = 2 * time.Minute

// maxBorrowedStdout caps the standard output joined onto a failed command's
// stderr. See bothStreams and its caller in Exec: what it copies is an
// explanation of a few lines, and what it must not copy is a megabyte of `git
// rev-list` that died halfway. Every byte here is broadcast to every open tab
// on the event stream and kept in the log panel's buffer.
const maxBorrowedStdout = 4 << 10

// Execution describes a finished git command, successful or not.
//
// Every execution is handed to the Runner's observer: that is what feeds the
// UI's log panel, where the user can always see the commands yagit actually
// ran.
type Execution struct {
	Args     []string
	Dir      string
	ExitCode int

	// StartedAt is when the command was launched, not when it finished. The
	// log panel orders by it, and a slow command must not jump ahead of the
	// fast ones that started after it and came back first.
	StartedAt time.Time
	Duration  time.Duration

	Stderr string
}

// CommandLine renders the execution the way you would type it in a terminal.
func (e Execution) CommandLine() string { return CommandLine(e.Args) }

// Observer receives every finished execution.
type Observer func(Execution)

// Error carries everything needed to explain a git failure without ever
// showing "Something went wrong": the exact command, its exit code and the
// raw stderr.
type Error struct {
	Args     []string
	Dir      string
	ExitCode int
	Stderr   string
	Cause    error
}

func (e *Error) Error() string {
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		stderr = "(no error output)"
	}
	return fmt.Sprintf("%s: exit code %d: %s", e.CommandLine(), e.ExitCode, stderr)
}

func (e *Error) Unwrap() error { return e.Cause }

// CommandLine renders the failing command the way you would type it in a
// terminal, so the user can replay it and see for themselves.
func (e *Error) CommandLine() string { return CommandLine(e.Args) }

// Runner runs git commands in a given working directory.
type Runner struct {
	// binary is the name of the git binary. It is a field rather than a
	// constant so tests can point it at a fake executable.
	binary string

	// observe receives every finished execution. Nil is accepted: the Runner
	// is then silent.
	observe Observer

	timeout time.Duration

	// writes serialises the commands that take .git/index.lock, one repository
	// at a time. See serialize.go.
	writes *dirLocks

	// What git this is, read once and only where a flag depends on it. See
	// version.go.
	versionCache
}

// NewRunner builds a Runner. The observer may be nil.
func NewRunner(observe Observer) *Runner {
	return &Runner{binary: "git", observe: observe, timeout: defaultTimeout, writes: newDirLocks()}
}

// Command describes a git invocation that needs more than a directory and a
// list of arguments.
//
// A struct rather than a growing list of Run variants: standard input and the
// accepted exit codes are both rare, both easy to pass to the wrong
// parameter, and both worth naming at the call site.
type Command struct {
	// Dir is the working directory git runs in.
	Dir string

	Args []string

	// Stdin is fed to the command and the pipe is then closed. Nil sends
	// nothing — which is not the same as sending nothing and leaving the pipe
	// open, where a command reading standard input would wait for the
	// timeout.
	Stdin []byte

	// SuccessCodes are exit codes to accept besides zero.
	//
	// Empty for almost every command, and it should stay that way: a command
	// listed here is one whose non-zero exit is documented to mean something
	// other than failure. `git diff --no-index` is the case that exists — it
	// follows diff(1), where 1 means "the files differ".
	SuccessCodes []int

	// OutputRequired refuses an accepted non-zero exit that produced nothing
	// on standard output.
	//
	// It exists for the same command as SuccessCodes above, because that
	// command overloads its exit code: `git diff --no-index` also exits 1 —
	// not 128 — for a path it could not open, writing the reason to stderr
	// and nothing at all to standard output. Accepting that as an empty diff
	// draws "no change here" over git's own explanation.
	OutputRequired bool

	// IdleTimeout bounds SILENCE rather than the whole command, and replaces
	// Timeout for the commands that reach another machine.
	//
	// Set by clone, fetch, pull and push, which are the four whose honest
	// length is unknown: what bounds them is that git has stopped saying
	// anything, not that the work has taken long. Zero everywhere else, where a
	// wall clock is the right instrument.
	//
	// It only works on a command that reports progress, which is why the four
	// that set it also pass --progress. A command with neither would run
	// unbounded.
	IdleTimeout time.Duration

	// Timeout bounds this command, in place of the Runner's own.
	//
	// Zero means the Runner's, which is right for everything that runs on the
	// local disk. It is set by the commands that reach another machine, and
	// those name networkTimeout rather than a number of their own: a per-call
	// duration is a second definition of the same policy, and the one that
	// gets forgotten is always the one on the command that needed it most.
	Timeout time.Duration

	// AcceptsPreparedMessage lets git commit under the message it prepared,
	// with no editor opened over it.
	//
	// Off everywhere by default, and that default is the point: a daemon has
	// no terminal, so an editor it cannot show is an editor whose contents
	// nobody read. git's own answer to that — "Terminal is dumb, but EDITOR
	// unset" — is a loud failure, which is the right one for a command about
	// to record something unseen.
	//
	// `git rebase --continue` is why it exists. That command commits the
	// resolved conflict under the message of the commit being replayed and
	// opens an editor to confirm it, so without this the button cannot work at
	// all. Accepting there is not a guess at what the user meant: replaying a
	// commit under its own message is the definition of continuing.
	//
	// It is set per command rather than in commandEnvironment for exactly that
	// reason — the justification is about one command, and pinning it for
	// every git the daemon runs would extend it to messages that do not exist
	// yet. A rebase with a `squash` in its plan is the case that proves the
	// difference, and State.Blocked is where it is refused.
	AcceptsPreparedMessage bool

	// SequenceEditor is the program git runs over the todo list of an
	// interactive rebase, empty for every other command.
	//
	// The one input `git rebase --interactive` does not take as an argument.
	// git writes a todo list, runs this over it, and executes whatever the
	// file holds afterwards — so this is how a plan assembled on screen
	// reaches git, and there is no flag that would take it instead. See
	// sequenceEditor in interactive.go for what is put here and why.
	//
	// Separate from AcceptsPreparedMessage above, and never set with it: one
	// accepts a message git prepared, and this replaces a list git prepared.
	// An interactive rebase deliberately leaves the editor unset, so that a
	// plan holding a step which opens one fails loudly instead of committing
	// something nobody read.
	SequenceEditor string

	// MessageEditor is the program git runs when it wants a commit message
	// edited, empty to leave GIT_EDITOR alone.
	//
	// One value is ever put here — the program that refuses, in
	// interactive.go — and it exists because leaving GIT_EDITOR unset is not
	// portably loud. git answers "Terminal is dumb, but EDITOR unset" on Linux
	// and macOS, and on Windows falls back to an editor from its own bundled
	// environment which, with no terminal, waits. A daemon that hangs for ten
	// minutes and is then killed halfway through a sequence is worse than one
	// that fails, so what git runs is named rather than left out.
	//
	// Never set with AcceptsPreparedMessage: one accepts the message git
	// prepared and this refuses to write one at all. environmentFor makes them
	// exclusive rather than trusting a caller to.
	MessageEditor string

	// MaxOutput caps how many bytes of standard output are kept, zero meaning
	// no cap.
	//
	// The daemon holds every byte git writes, several times over once it is
	// parsed. A rewritten generated file — a database dump, a lockfile, a
	// minified bundle — answers with a diff the size of two copies of itself,
	// and one click on such a row would otherwise grow the process until the
	// kernel kills it, taking every open repository with it. Reaching the cap
	// is an error, never a truncated answer: half a diff is a patch that
	// applies to the wrong thing.
	MaxOutput int

	// OnProgress receives each line (or carriage-return segment) git writes to
	// stderr while the command runs. Nil for every command that does not need
	// it.
	//
	// Clone is why it exists: a ten-minute transfer writes counters to stderr
	// the whole time, and a spinner that waits for the Execution at the end
	// is not a progress report. The full stderr is still kept for the
	// Execution and the Error — this is a tee, not a divert. See
	// docs/adr/0030.
	OnProgress func(line string)
}

// Run executes git in dir and returns its standard output.
//
// A failing command produces an *Error, which keeps the command, the exit
// code and the raw stderr. Nothing is reworded along the way.
func (r *Runner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return r.Exec(ctx, Command{Dir: dir, Args: args})
}

// Exec is Run's general form: every git command in the project ends up here.
func (r *Runner) Exec(ctx context.Context, command Command) ([]byte, error) {
	timeout := r.timeout
	if command.Timeout > 0 {
		timeout = command.Timeout
	}

	// Two kinds of deadline, and a network command wants the second.
	//
	// A wall clock is right for work whose length is known: a local command
	// that has taken thirty seconds has hung. It is wrong for a transfer,
	// where the length is the size of the repository divided by somebody's
	// line. A four-gigabyte clone on a domestic connection is fifty-five
	// minutes of work git reports progress through the whole of — and under a
	// ten-minute ceiling it does not clone slowly, it dies at ten minutes,
	// having written most of a repository, and tells the user their network
	// failed.
	//
	// What a dead connection actually looks like is silence, so that is what
	// is measured. The countdown restarts on every line git writes, which is
	// what makes networkTimeout's own promise — "a connection which has
	// silently died still ends in an answer" — true without cutting the
	// transfers it was never about.
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	onProgress := command.OnProgress

	// Atomic because the timer fires on a goroutine of its own and this is
	// read on ours, after the command has returned.
	var wentQuiet atomic.Bool

	if command.IdleTimeout > 0 {
		idle := time.AfterFunc(command.IdleTimeout, func() {
			wentQuiet.Store(true)
			cancel()
		})
		defer idle.Stop()

		// Reset from the goroutine os/exec copies stderr on. AfterFunc's Reset
		// is safe to call from anywhere, and this is the only place that knows
		// the transfer is still alive.
		onProgress = func(line string) {
			idle.Reset(command.IdleTimeout)
			if command.OnProgress != nil {
				command.OnProgress(line)
			}
		}
	} else {
		var deadline context.CancelFunc
		runContext, deadline = context.WithTimeout(runContext, timeout)
		defer deadline()
	}

	dir, args := command.Dir, command.Args

	// Serialised against the other writes to this repository, and only for the
	// commands that take .git/index.lock — see serialize.go. Before the process
	// is built rather than after, so nothing is started that would then have to
	// be killed, and released by the defer whichever way this returns.
	if r.writes != nil && writesTheIndex(args) {
		release, err := r.writes.acquire(runContext, dir)
		if err != nil {
			return nil, &Error{
				Args:     redactArguments(args),
				Dir:      dir,
				ExitCode: -1,
				Stderr:   err.Error(),
				Cause:    err,
			}
		}
		defer release()
	}

	process := exec.CommandContext(runContext, r.binary, args...)
	process.Dir = dir
	process.Env = environmentFor(command)

	// Without this, the deadline above bounds git and nothing else. See
	// waitDelay: the grandchildren git starts inherit these pipes and outlive
	// the signal that ends it.
	process.WaitDelay = waitDelay

	// A nil Stdin leaves the pipe at os/exec's default, which is the null
	// device: a command that reads it sees end of file at once. Handing it an
	// empty reader instead would be the same thing said less clearly.
	if command.Stdin != nil {
		process.Stdin = bytes.NewReader(command.Stdin)
	}

	stderr := boundedBuffer{limit: maxStderr}
	stdout := cappedBuffer{limit: command.MaxOutput}
	process.Stdout = &stdout
	if onProgress != nil {
		process.Stderr = &progressWriter{buffer: &stderr, onLine: onProgress}
	} else {
		process.Stderr = &stderr
	}

	started := time.Now()
	runError := process.Run()
	duration := time.Since(started)

	if progress, ok := process.Stderr.(*progressWriter); ok {
		progress.flush()
	}

	// Redacted, and a copy for two reasons at once.
	//
	// The copy is defensive: the caller keeps a reference to its own slice,
	// and both the Execution and the Error outlive the call.
	//
	// The redaction is the point. Everything built below this line — the
	// Execution handed to the observer, and every Error returned — is what the
	// user, the log panel, the backlog at GET /api/log, the JSON of a failure
	// and the daemon's journal on disk all read. A remote URL of the form
	// `https://ada:ghp_xxx@github.com/ada/x.git` is a working one and a common
	// one, and until this line the confirmation dialog showed it with the
	// token hidden while the log panel printed it in full two seconds later,
	// into a file that outlives the session.
	//
	// Here rather than at each of the four places that display it: RedactURL's
	// own comment claims the secret is removed at the boundary so that no
	// later caller can forget, and this is the boundary it meant. `args` stays
	// untouched, because that is what git was actually handed.
	recordedArgs := redactArguments(args)

	// ProcessState is nil when the binary could not even start; -1 flags that
	// case without confusing it with a legitimate exit code.
	exitCode := -1
	if process.ProcessState != nil {
		exitCode = process.ProcessState.ExitCode()
	}

	// A process that never started wrote nothing to stderr, so reporting the
	// captured stderr would report emptiness — and the user would be told a
	// command failed with "(no error output)" while os/exec was holding the
	// only sentence that explains it: git missing from PATH, the directory
	// gone, a permission refused. The rule of this project is that an error
	// carries everything it knows, so the cause takes stderr's place here.
	reportedStderr := stderr.String()
	if process.ProcessState == nil && runError != nil {
		reportedStderr = runError.Error()
	}

	// exitCode > 0 guards the accepted-codes list against the two ways it
	// could swallow a real failure: a process that never started and one the
	// timeout killed both report -1, and neither is a code any caller means
	// to accept. OutputRequired guards it against the third: a command whose
	// accepted code also means "I could not read that".
	// A grandchild that outlived git is not git failing.
	//
	// WaitDelay closes the pipes and reports ErrWaitDelay even when git itself
	// exited cleanly, and one ordinary thing is enough to trigger it: a
	// pre-commit hook that starts something in the background — a watcher, a
	// development server, an agent behind `tool &` — inherits git's stderr and
	// holds it open after git is gone. Without this line the commit IS WRITTEN
	// and the interface reports a failure, which is the worst answer available:
	// the user retries and makes a second commit.
	//
	// The exit code decides, not the wait. It is the only signal here that
	// describes what git did rather than what something else is still doing.
	abandonedPipes := errors.Is(runError, exec.ErrWaitDelay)

	answered := !command.OutputRequired || stdout.Len() > 0
	accepted := runError == nil ||
		(abandonedPipes && exitCode == 0) ||
		(exitCode > 0 && answered && slices.Contains(command.SuccessCodes, exitCode))

	// Everything git said when it failed, not whichever half of it landed on
	// stderr.
	//
	// git splits one explanation across two streams, and it splits it
	// differently per command. Reading only stderr therefore loses a
	// different sentence each time, and which sentence is lost is not
	// something a caller can be expected to know — so nothing here asks it to.
	//
	// A conflict is where both halves matter, and where reading one of them
	// went wrong. `git merge` writes "CONFLICT (content): Merge conflict in
	// notes.md" to stdout and nothing to stderr. `git rebase` writes that same
	// CONFLICT line to stdout and "error: could not apply …" with its hints to
	// stderr. `git rebase --continue` over an unmerged file writes "f.txt:
	// needs merge" to stdout and nothing to stderr — the case that first found
	// this, where the user was shown "exit code 1: (no error output)" for the
	// most ordinary mistake there is on that button. Every one of them is the
	// same failure, git stopped and named a file, and the rule that borrowed
	// stdout only when stderr was EMPTY answered them differently: it gave the
	// merge its filename and gave the rebase progress noise and advice with no
	// filename anywhere in it.
	//
	// stdout first, because that is where git names what it was doing and
	// which file it stopped on; stderr is the complaint and the advice that
	// follow it.
	//
	// Only on a command that FAILED, and never on one whose exit code is
	// accepted: `git diff --no-index` exits 1 to mean "these differ", and its
	// stdout is the diff, which is an answer rather than a complaint. Not on
	// a truncated one either, where stdout is a fragment cut mid-line and the
	// branch below has a better sentence for it.
	if !accepted && !stdout.exceeded {
		reportedStderr = bothStreams(
			beginningOf(stdout.Bytes(), maxBorrowedStdout), reportedStderr)
	}

	// git names the URL it could not reach, and it names it whole:
	//
	//	fatal: repository 'https://ada:ghp_xxx@github.com/ada/x.git/' not found
	//
	// That sentence is the one this project insists on showing rather than
	// replacing with "something went wrong", and it is shown on screen,
	// broadcast to every open tab and written to the journal. Redacting the
	// arguments and not this would hide the token on the command line and
	// print it in the error underneath — the redaction has to cover both
	// halves of what a failure says, or it covers neither.
	//
	// Only the credentials go. Everything else git wrote is left exactly as it
	// wrote it, which is the whole point of showing it.
	reportedStderr = RedactText(reportedStderr)

	if r.observe != nil {
		r.observe(Execution{
			Args:      recordedArgs,
			Dir:       dir,
			ExitCode:  exitCode,
			StartedAt: started,
			Duration:  duration,
			Stderr:    reportedStderr,
		})
	}

	if stdout.exceeded {
		return nil, &Error{
			Args:     recordedArgs,
			Dir:      dir,
			ExitCode: exitCode,
			Stderr: fmt.Sprintf("git wrote more than %d bytes and was stopped: %s",
				command.MaxOutput, strings.TrimSpace(reportedStderr)),
			Cause: ErrDiffTooLarge,
		}
	}

	if accepted {
		return stdout.Bytes(), nil
	}

	// On a timeout, os/exec reports "signal: killed", which explains nothing.
	// We say what actually happened.
	if wentQuiet.Load() {
		return nil, &Error{
			Args:     recordedArgs,
			Dir:      dir,
			ExitCode: exitCode,
			Stderr: fmt.Sprintf(
				"no progress from git for %s, so the connection was treated as dead",
				command.IdleTimeout),
			Cause: runContext.Err(),
		}
	}
	if errors.Is(runContext.Err(), context.DeadlineExceeded) {
		return nil, &Error{
			Args:     recordedArgs,
			Dir:      dir,
			ExitCode: exitCode,
			Stderr:   fmt.Sprintf("command timed out after %s", timeout),
			Cause:    runContext.Err(),
		}
	}

	return nil, &Error{
		Args:     recordedArgs,
		Dir:      dir,
		ExitCode: exitCode,
		Stderr:   reportedStderr,
		Cause:    runError,
	}
}
