package main

import (
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Soaking is running the end-to-end suite over and over on a machine that has
// no spare CPU, to answer the one question a single green run cannot: is this
// suite actually stable, or does it only pass when nothing else is happening?
// A timing race loses when the machine is contended and wins when it is idle,
// so an idle machine is exactly where it hides.
//
// The load comes from goroutines inside this process, and that is the whole
// design of this file.
//
// The obvious alternative was tried first, in a shell: start eight
// `while :; do :; done` subshells in the background, run the suite, kill the
// pids on the way out. It left eight processes spinning on a development
// machine for an hour and a half. The shell holding them was killed outright,
// so the line that killed the pids never ran, and the loops were reparented to
// init with nothing left in the world that knew which pids they were. A trap
// would not have saved it either: no handler runs on SIGKILL.
//
// Goroutines cannot end that way. They are threads of this process, so the
// load dies with it — on Ctrl-C, on SIGKILL, on a panic, on the terminal
// window closing. There is no pid to write down, nothing to reap, and nothing
// that can outlive the command that asked for it.

// defaultSoakRuns is what `./do test soak` runs with no argument.
//
// Three is the smallest count that distinguishes the two answers worth
// telling apart: a suite that fails every time is broken, and a suite that
// fails once in three is flaky.
const defaultSoakRuns = 3

// testSoak runs the end-to-end suite `runs` times with every CPU busy.
//
// A failure here is a signal to investigate, not a verdict. The suite's own
// timeouts are measured in wall-clock time, and starving it of CPU is a way
// to exceed them honestly — so a run that fails needs its report read, not a
// revert.
func (p *project) testSoak(runs int) error {
	workers := runtime.NumCPU()
	info("soak: %d end-to-end runs, %d CPU workers busy throughout", runs, workers)

	load := startCPULoad(workers)
	defer load.stop()

	failures := 0
	for run := 1; run <= runs; run++ {
		started := time.Now()
		info("run %d of %d", run, runs)

		if err := p.testEndToEnd(); err != nil {
			// Reported and carried on from, rather than returned. The number
			// that answers the question is how many of the runs failed, and
			// stopping at the first one throws that number away — one failure
			// out of three is the flake this command exists to find, and it
			// looks identical to three out of three until the third run.
			warn("run %d failed after %s: %s", run, time.Since(started).Round(time.Second), err)
			failures++
			continue
		}
		info("run %d passed in %s", run, time.Since(started).Round(time.Second))
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d soak runs failed", failures, runs)
	}
	info("soak: %d of %d runs passed", runs, runs)
	return nil
}

// parseSoakRuns reads the run count from the command line.
func parseSoakRuns(args []string) (int, error) {
	if len(args) == 0 {
		return defaultSoakRuns, nil
	}

	// strconv's own message names the function that failed, which tells the
	// reader nothing they can act on. This one names the rule instead.
	runs, err := strconv.Atoi(args[0])
	if err != nil || runs < 1 {
		return 0, fmt.Errorf("soak takes a number of runs, 1 or more, not %q", args[0])
	}
	return runs, nil
}

// ---------------------------------------------------------------------------
// The load
// ---------------------------------------------------------------------------

// cpuLoad keeps a fixed number of goroutines spinning until it is stopped.
type cpuLoad struct {
	// halt is closed rather than sent to: stopping is a broadcast, every
	// worker watches the same channel, and closing it needs no count of them.
	halt chan struct{}

	// finished is what lets stop promise that the machine is idle again when
	// it returns, rather than merely that it has been asked to be.
	finished sync.WaitGroup

	// stopped guards the close: closing a closed channel panics, and stop is
	// reached twice whenever a deferred call follows an explicit one.
	stopped sync.Once
}

// cpuLoadSink absorbs the arithmetic the workers do.
//
// Without somewhere for the result to go, the compiler is free to delete the
// loop that produced it, and the load becomes a set of goroutines burning
// nothing at all — a soak that quietly measures the idle case, which is the
// one case it must not measure. TestCPULoadBurnsCPU holds that line.
var cpuLoadSink atomic.Uint64

// startCPULoad puts workers goroutines to work and returns immediately.
//
// One worker per CPU is deliberate oversubscription: the daemon, Vite and the
// browser then have to take every cycle they run on away from a worker, which
// is the state that makes a timing race lose.
func startCPULoad(workers int) *cpuLoad {
	load := &cpuLoad{halt: make(chan struct{})}

	load.finished.Add(workers)
	for range workers {
		go load.burn()
	}
	return load
}

// burn is one worker: arithmetic, until told to stop.
func (l *cpuLoad) burn() {
	defer l.finished.Done()

	// The chunk is what keeps stopping quick without making the check into
	// the work. Sixty-odd thousand multiply-adds run in well under a
	// millisecond, so a stopped load is idle immediately, while the channel
	// is still only read once per chunk instead of once per operation.
	const chunk = 1 << 16

	// Seeded from the sink rather than from a constant, and never reset. The
	// compiler cannot know what an atomic load returns, so it cannot fold the
	// arithmetic below back into the constant it would otherwise be — which
	// it could do, in principle, to a fixed number of steps from zero.
	accumulator := cpuLoadSink.Load()

	for {
		select {
		case <-l.halt:
			return
		default:
		}

		for range chunk {
			// A linear congruential step. Each one needs the result of the
			// one before it, so there is nothing here to vectorise, run ahead
			// of, or hoist out of the loop: the CPU has to do the work in the
			// order it is written.
			accumulator = accumulator*1664525 + 1013904223
		}

		// Published every chunk, not only on the way out, so that a test can
		// watch the load do its job while it is still running.
		cpuLoadSink.Add(accumulator)
	}
}

// stop ends the load and waits for the machine to be idle again.
func (l *cpuLoad) stop() {
	l.stopped.Do(func() {
		close(l.halt)
		l.finished.Wait()
	})
}
