package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/ngsanogo/yagit/internal/repo"
)

// Progress for a long network command rides the request that started it —
// see docs/adr/0030. Clone was the first consumer; fetch, pull and push reuse
// the same NDJSON shape: progress lines while git runs, then either the
// operation's answer or the failure.

type progressEvent struct {
	Type string `json:"type"` // "progress"
	Line string `json:"line"`
}

type progressDoneEvent struct {
	Type string `json:"type"` // "done"
	// Embedded so the done event carries the same fields answerWithRefs
	// would have written — refs and head at the top level — rather than
	// nesting them under a second "refs" key the ordinary JSON shape never
	// had.
	refsPayload
}

type progressErrorEvent struct {
	Type  string      `json:"type"` // "error"
	Error errorDetail `json:"error"`
}

// beginProgressStream prepares the response for NDJSON progress. Validation
// errors that happen before git starts still use writeError — there is
// nothing to stream yet.
func beginProgressStream(writer http.ResponseWriter) (emit func(any) bool, err error) {
	flusher, streamable := writer.(http.Flusher)
	if !streamable {
		return nil, fmt.Errorf("this connection cannot stream (%T does not flush)", writer)
	}

	writer.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()

	encode := json.NewEncoder(writer)
	encode.SetEscapeHTML(false)

	streaming := true
	emit = func(event any) bool {
		if !streaming {
			return false
		}
		if writeErr := encode.Encode(event); writeErr != nil {
			streaming = false
			return false
		}
		flusher.Flush()
		return true
	}
	return emit, nil
}

// progressBuffer is how many progress lines may be waiting to be written
// before the oldest is dropped.
//
// Sixty-four, the same as subscriberBuffer and for the same reason: it is
// several seconds of git's counter, and a reader further behind than that is
// not going to catch up on the next line.
const progressBuffer = 64

// progressPump moves the writing of progress lines off the goroutine os/exec
// hands them over on.
//
// Why it exists. When a command sets OnProgress, its stderr is not an *os.File
// and os/exec therefore opens a pipe and copies it on a goroutine of its own.
// Writing the HTTP response from that goroutine makes the copy as slow as the
// reader: a tab put in the background, a suspended laptop, a paused debugger,
// and the browser stops reading — the send buffer fills, Encode blocks, the
// copy stops draining git's pipe, git's own 64 KiB stderr pipe fills, and GIT
// BLOCKS. The transfer that the progress was there to make visible is stopped
// by the progress. os/exec's documentation is explicit that this is not
// something WaitDelay rescues: "Regardless of WaitDelay, Wait can block until a
// Write to Stdout or Stderr completes."
//
// So the copy goroutine only ever does a non-blocking send, and a separate
// goroutine does the writing. A line dropped because the buffer is full is the
// right loss and the project already makes it twice — see announce in
// internal/watch and publish in internal/api/events.go: what is dropped is one
// frame of a counter that is about to be superseded, and the alternative is
// holding up the thing being counted.
type progressPump struct {
	lines chan string

	// closing is what stop closes, and lines deliberately is not.
	//
	// A send on a closed channel panics, and `select` with a `default` arm
	// does NOT protect against it — only against a full one. The producer here
	// is a goroutine inside os/exec, so whether it can still be running when
	// the handler stops is a question about somebody else's ordering. Closing
	// a second channel instead makes the answer not matter: a late line lands
	// in a buffer nobody reads, or is dropped, and neither is a crash.
	closing chan struct{}

	// drained is closed by the delivering goroutine once it has written
	// everything it is going to.
	drained chan struct{}

	once sync.Once
}

// startProgressPump begins delivering lines to deliver, on a goroutine of its
// own. Every pump must be stopped, and stopped before the handler writes
// anything else to the same response.
func startProgressPump(deliver func(string)) *progressPump {
	pump := &progressPump{
		lines:   make(chan string, progressBuffer),
		closing: make(chan struct{}),
		drained: make(chan struct{}),
	}

	go func() {
		defer close(pump.drained)
		for {
			select {
			case line := <-pump.lines:
				deliver(line)
			case <-pump.closing:
				// Everything already handed over is still written: the last
				// lines of a transfer are the ones that say it finished.
				for {
					select {
					case line := <-pump.lines:
						deliver(line)
					default:
						return
					}
				}
			}
		}
	}()

	return pump
}

// line is the OnProgress callback, and it never blocks.
func (p *progressPump) line(line string) {
	select {
	case p.lines <- line:
	default:
		// Dropped, deliberately. See the type comment: the reader is behind by
		// more than sixty-four lines of a counter, and making git wait for it
		// is the failure this whole type exists to avoid.
	}
}

// stop ends delivery and waits for the last line to be written.
//
// It has to be called before the handler emits anything else on the same
// response — the done event, an error — because until it returns there is a
// second goroutine writing to that writer, and the two would interleave. It is
// idempotent so that a deferred stop can guard the paths that return early.
func (p *progressPump) stop() {
	p.once.Do(func() {
		close(p.closing)
		<-p.drained
	})
}

// emitProgressLine is the OnProgress callback for a stream that has already
// begun. A failed write stops further emits without cancelling git — same
// reason clone keeps running after the reader goes.
func emitProgressLine(emit func(any) bool) func(string) {
	return func(line string) {
		_ = emit(progressEvent{Type: "progress", Line: line})
	}
}

func emitProgressError(emit func(any) bool, err error) {
	_ = emit(progressErrorEvent{
		Type: "error",
		Error: errorDetail{
			Message: err.Error(),
			Git:     gitFailureOf(err),
		},
	})
}

// finishProgressWithRefs reads the references the operation moved and emits
// them as the done event. A read failure after a successful network command
// is still reported on the stream — the command already ran.
func (s *Server) finishProgressWithRefs(
	emit func(any) bool, request *http.Request, opened *repo.Repo,
) {
	payload, err := s.readRefs(request.Context(), opened)
	if err != nil {
		_ = emit(progressErrorEvent{
			Type:  "error",
			Error: errorDetail{Message: err.Error()},
		})
		return
	}
	_ = emit(progressDoneEvent{Type: "done", refsPayload: payload})
}
