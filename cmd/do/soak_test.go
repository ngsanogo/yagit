package main

import (
	"testing"
	"time"
)

// The load is the half of soaking that fails silently. A run count parsed
// wrongly announces itself on the next line; a load that burns nothing, or one
// that keeps burning after it was stopped, looks exactly like a load that
// works.
//
// None of these call t.Parallel: they read and write one process-wide sink,
// and two of them running at once would each see the other's arithmetic.

func TestParseSoakRuns(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    int
		wantErr bool
	}{
		{name: "no argument is the default", args: nil, want: defaultSoakRuns},
		{name: "an empty argument list is the default", args: []string{}, want: defaultSoakRuns},
		{name: "a positive number is taken", args: []string{"5"}, want: 5},
		{name: "one is the minimum", args: []string{"1"}, want: 1},
		{
			name: "zero is refused",
			// Accepting it would print "0 runs, all passed", which is true,
			// green, and answers nothing.
			args:    []string{"0"},
			wantErr: true,
		},
		{name: "a negative number is refused", args: []string{"-1"}, wantErr: true},
		{name: "text is refused", args: []string{"often"}, wantErr: true},
		{
			name: "a duration is refused",
			// `./do test fuzz 60s` is the neighbouring command and does take
			// one. Reading it here as 60 runs would soak until tomorrow.
			args:    []string{"60s"},
			wantErr: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseSoakRuns(testCase.args)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("parseSoakRuns(%v) = %d, want an error", testCase.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSoakRuns(%v): %v", testCase.args, err)
			}
			if got != testCase.want {
				t.Errorf("parseSoakRuns(%v) = %d, want %d", testCase.args, got, testCase.want)
			}
		})
	}
}

// TestCPULoadBurnsCPU is the line soak.go points at: without a use for the
// result the compiler can delete the arithmetic, and a soak that measures the
// idle case is worse than no soak at all, because it reports success.
func TestCPULoadBurnsCPU(t *testing.T) {
	before := cpuLoadSink.Load()

	load := startCPULoad(1)
	defer load.stop()

	// Compared for inequality, not for growth: the sink is a uint64 that
	// wraps, and a worker whose chunk carries it past the top would fail a
	// greater-than test while doing exactly the work being asserted.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cpuLoadSink.Load() != before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the load never advanced the sink — the arithmetic was optimised away")
}

// TestCPULoadStopsEveryWorker is the leak this file exists to prevent, in the
// only form it can still take. Eight shell loops once outlived the command
// that started them by an hour and a half; a goroutine cannot outlive the
// process, but it can outlive the stop that was supposed to end it.
func TestCPULoadStopsEveryWorker(t *testing.T) {
	load := startCPULoad(2)
	load.stop()

	// stop waits for the workers, so the sink is frozen the moment it
	// returns. The pause is here to catch the worker that got away from the
	// WaitGroup rather than to give the others time to finish.
	after := cpuLoadSink.Load()
	time.Sleep(50 * time.Millisecond)

	if moved := cpuLoadSink.Load(); moved != after {
		t.Fatalf("a worker outlived stop(): the sink moved from %d to %d", after, moved)
	}
}

func TestCPULoadStopIsIdempotent(t *testing.T) {
	load := startCPULoad(1)

	// Two calls are the normal path, not an edge case: testSoak defers stop
	// and every early return reaches that defer. Closing an already closed
	// channel panics, which would turn a finished soak into a crash report.
	load.stop()
	load.stop()
}
