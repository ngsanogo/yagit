package api

import (
	"sync"
	"testing"
	"time"
)

// A producer that never blocks is the whole point: the caller of line() is the
// goroutine os/exec copies git's stderr on, and blocking it stops git.
func TestProgressPumpNeverBlocksTheProducer(t *testing.T) {
	release := make(chan struct{})
	var delivered int

	pump := startProgressPump(func(string) {
		// The first line blocks until the test lets it go, standing in for a
		// browser that has stopped reading.
		<-release
		delivered++
	})

	// One line is taken by the delivering goroutine, the buffer takes the
	// next progressBuffer, and everything past that must be dropped rather
	// than waited on.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range progressBuffer * 4 {
			pump.line("Receiving objects:  50%")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("line() blocked: a browser that stopped reading would now be stopping git")
	}

	close(release)
	pump.stop()
}

// stop() is the barrier that keeps two goroutines off one http.ResponseWriter.
// Until it returns, the pump is still writing; the handler emits its done or
// error event afterwards.
func TestProgressPumpStopWaitsForTheLastLine(t *testing.T) {
	var mutex sync.Mutex
	var seen []string

	pump := startProgressPump(func(line string) {
		// Slow enough that a stop which did not wait would return first.
		time.Sleep(2 * time.Millisecond)
		mutex.Lock()
		defer mutex.Unlock()
		seen = append(seen, line)
	})

	for _, line := range []string{"one", "two", "three"} {
		pump.line(line)
	}
	pump.stop()

	mutex.Lock()
	defer mutex.Unlock()
	if len(seen) != 3 {
		t.Fatalf("stop returned with %d of 3 lines written: the handler would write over the pump", len(seen))
	}
	for index, want := range []string{"one", "two", "three"} {
		if seen[index] != want {
			t.Errorf("line %d = %q, want %q: order is what makes a counter readable", index, seen[index], want)
		}
	}
}

// Every handler defers a stop and also stops explicitly once git returns, so
// the second call has to be harmless rather than a close of a closed channel.
func TestProgressPumpStopIsIdempotent(t *testing.T) {
	pump := startProgressPump(func(string) {})
	pump.line("one")
	pump.stop()
	pump.stop()
	pump.stop()
}

// A line handed over after the pump has stopped is dropped, not delivered and
// not a panic: os/exec's copy goroutine can still be finishing when the
// handler has already moved on.
func TestProgressPumpDropsALineAfterStop(t *testing.T) {
	var mutex sync.Mutex
	var count int

	pump := startProgressPump(func(string) {
		mutex.Lock()
		defer mutex.Unlock()
		count++
	})
	pump.line("before")
	pump.stop()

	// Sending on a closed channel panics; the non-blocking select must take
	// the default arm instead. Whether this line is counted is not the point —
	// not crashing is.
	func() {
		defer func() {
			if raised := recover(); raised != nil {
				t.Fatalf("a line handed over after stop panicked: %v", raised)
			}
		}()
		pump.line("after")
	}()

	mutex.Lock()
	defer mutex.Unlock()
	if count != 1 {
		t.Errorf("delivered %d lines, want the one sent before stop", count)
	}
}
