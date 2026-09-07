package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ngsanogo/yagit/internal/git"
)

// The daemon's one push channel, and the two things it carries.
//
// One stream for the whole session, never one per repository: a browser opens
// at most six connections to an origin over HTTP/1.1, and a stream per
// repository would park all six and leave every ordinary request queued
// behind them. See docs/adr/0007.

const (
	// eventRepositoryChanged says a repository's git directory moved. It
	// carries the identifier and nothing else — every consumer re-reads git
	// anyway, and a "reason" nobody acts on goes stale unnoticed.
	eventRepositoryChanged = "repository"

	// eventGitCommand is one git command yagit ran, successful or not. This
	// is what feeds the log panel, and it is a promise the project makes: the
	// user must always be able to learn git by watching yagit work.
	eventGitCommand = "git"
)

// bufferedEvents is how many events are kept for replay.
//
// It serves two purposes at once, which is why it is not smaller. It is the
// backlog GET /api/log answers with — the commands that ran before the
// interface was looking — and it is what a reconnecting stream resumes from.
// A few hundred entries of a few hundred bytes each is nothing to hold and
// covers a long working session.
const bufferedEvents = 500

// subscriberBuffer is how far behind one connection may fall before it is cut
// loose. Reconnecting and resuming is better than dropping events silently:
// EventSource comes back on its own with Last-Event-ID, and everything it
// missed is still in the ring above.
const subscriberBuffer = 64

// heartbeatInterval keeps an idle stream alive.
//
// A stream with nothing to say writes nothing, and a connection that writes
// nothing is one a proxy, a laptop suspending, or the daemon's own dev-server
// proxy is free to drop. The comment costs two bytes and makes the difference
// between an interface that stops updating and one that reconnects.
const heartbeatInterval = 25 * time.Second

// streamEvent is one thing to say, already encoded.
//
// The payload is marshalled once at publication rather than once per
// subscriber, and it has to be: the same bytes must reach every connection,
// or two tabs on one repository would disagree about what happened.
type streamEvent struct {
	ID   uint64
	Kind string
	Data []byte
}

// executionView is a git command as the log panel reads it.
type executionView struct {
	ID         string    `json:"id"`
	Command    string    `json:"command"`
	ExitCode   int       `json:"exit_code"`
	DurationMS int64     `json:"duration_ms"`
	Stderr     string    `json:"stderr"`
	StartedAt  time.Time `json:"started_at"`
}

type repositoryChangedView struct {
	ID string `json:"id"`
}

// EventStream fans changes out to every open connection and keeps the recent
// ones for replay.
//
// Built by cmd/yagit rather than by the server, because the git Runner has to
// publish into it and the Runner exists before the server does. Safe for
// concurrent use: it is written to from HTTP handlers, from the filesystem
// watcher's goroutine, and from whichever goroutine a git command finished on.
type EventStream struct {
	logger *slog.Logger

	// done is closed when the daemon is stopping, and every open stream ends
	// with it. Without that, shutdown has no way to end a request that by
	// design never ends: http.Server.Shutdown closes idle connections and
	// then WAITS for the ones still in flight, so one open tab stalls it for
	// the whole grace period and the daemon reports a failure on a clean
	// stop. Under `./do dev` it is worse — air's kill delay expires first and
	// every hot reload costs the difference.
	done      chan struct{}
	closeOnce sync.Once

	mutex       sync.Mutex
	nextID      uint64
	buffer      []streamEvent
	subscribers map[*subscriber]struct{}
}

type subscriber struct {
	events chan streamEvent

	// lagging is closed when this connection fell a whole buffer behind. The
	// handler then hangs up, and the browser reconnects with Last-Event-ID —
	// which is the difference between a stream that recovers and one that
	// quietly stops being true.
	lagging   chan struct{}
	closeOnce sync.Once
}

func NewEventStream(logger *slog.Logger) *EventStream {
	return &EventStream{
		logger:      logger,
		done:        make(chan struct{}),
		buffer:      make([]streamEvent, 0, bufferedEvents),
		subscribers: make(map[*subscriber]struct{}),
	}
}

// Close ends every open stream.
//
// Registered on the HTTP server's shutdown hook rather than called from a
// handler: it is the daemon saying it is going, and the streams are the only
// requests that would not end on their own. Safe to call twice, because
// Shutdown may be reached from more than one path.
func (s *EventStream) Close() {
	s.closeOnce.Do(func() { close(s.done) })
}

// PublishRepositoryChanged announces that a repository moved on disk.
func (s *EventStream) PublishRepositoryChanged(repositoryID string) {
	s.publish(eventRepositoryChanged, func(uint64) any {
		return repositoryChangedView{ID: repositoryID}
	})
}

// PublishExecution announces a git command that ran.
//
// Called from the Runner's observer, so it runs on the goroutine that served
// the request the command belonged to. It must not block, and it does not:
// every send below is non-blocking.
func (s *EventStream) PublishExecution(execution git.Execution) {
	// The payload is built from the sequence number the event is about to be
	// given, so a command has one identity in the stream and in the backlog.
	// Reading the counter first and publishing afterwards would hand the same
	// number to two commands that finished at once.
	s.publish(eventGitCommand, func(id uint64) any {
		return executionView{
			ID:         strconv.FormatUint(id, 10),
			Command:    execution.CommandLine(),
			ExitCode:   execution.ExitCode,
			DurationMS: execution.Duration.Milliseconds(),
			Stderr:     execution.Stderr,
			StartedAt:  execution.StartedAt.UTC(),
		}
	})
}

// Executions returns the git commands still in the buffer, oldest first.
//
// This is the backlog: what ran before the interface connected. The stream
// carries what happens next.
func (s *EventStream) Executions() []executionView {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Built with make rather than declared: a nil slice marshals to null, and
	// a log panel handed null where it expected a list is a panel that
	// crashes on a daemon that has run nothing yet.
	views := make([]executionView, 0, len(s.buffer))
	for _, event := range s.buffer {
		if event.Kind != eventGitCommand {
			continue
		}
		var view executionView
		if err := json.Unmarshal(event.Data, &view); err != nil {
			// The bytes came from this file's own Marshal a moment ago.
			// Being unable to read them back means something is badly wrong,
			// and it says so rather than serving a shorter list.
			s.logger.Error("cannot read back a buffered execution", "error", err)
			continue
		}
		views = append(views, view)
	}
	return views
}

func (s *EventStream) publish(kind string, build func(id uint64) any) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.nextID++

	data, err := json.Marshal(build(s.nextID))
	if err != nil {
		// Nothing here can fail to marshal — the two payload types hold
		// strings and numbers — so this is a change to one of them, and
		// dropping the event silently is how a log panel starts missing
		// commands nobody can explain.
		s.logger.Error("cannot encode an event", "kind", kind, "error", err)
		return
	}
	event := streamEvent{ID: s.nextID, Kind: kind, Data: data}

	s.buffer = append(s.buffer, event)
	if len(s.buffer) > bufferedEvents {
		// Shift rather than reallocate. The copy is a few hundred small
		// structs and happens once per event at steady state; a fresh slice
		// each time would hand the garbage collector the whole ring instead.
		s.buffer = append(s.buffer[:0], s.buffer[1:]...)
	}

	for listener := range s.subscribers {
		select {
		case listener.events <- event:
		default:
			listener.dropped()
		}
	}
}

func (listener *subscriber) dropped() {
	listener.closeOnce.Do(func() { close(listener.lagging) })
}

// subscribe registers a connection and hands back everything published after
// afterID that is still buffered. A nil afterID replays nothing.
//
// The replay and the subscription happen under one lock, which is the whole
// point: taken separately, an event published in between would be in neither,
// and the interface would go on showing a repository that had moved.
func (s *EventStream) subscribe(afterID *uint64) (*subscriber, []streamEvent) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var missed []streamEvent
	if afterID != nil {
		for _, event := range s.buffer {
			if event.ID > *afterID {
				missed = append(missed, event)
			}
		}
	}

	listener := &subscriber{
		events:  make(chan streamEvent, subscriberBuffer),
		lagging: make(chan struct{}),
	}
	s.subscribers[listener] = struct{}{}

	return listener, missed
}

func (s *EventStream) unsubscribe(listener *subscriber) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.subscribers, listener)
}

// handleEvents serves the stream.
//
// It authenticates by cookie like every other route, and it has to: EventSource
// cannot set a request header, so X-Yagit-Token is not available to it. That
// is what makes the cookie load-bearing rather than a convenience — see
// docs/adr/0007.
func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, streamable := writer.(http.Flusher)
	if !streamable {
		// Every writer in this daemon flushes; a build where one does not is
		// broken in a way that would otherwise show up as a stream that never
		// delivers anything.
		writeError(writer, s.logger, http.StatusInternalServerError,
			fmt.Errorf("this connection cannot stream (%T does not flush)", writer))
		return
	}

	header := writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	// A cached event stream is a stream that plays yesterday's events back
	// once and then ends.
	header.Set("Cache-Control", "no-store")
	// Nginx and friends buffer proxied responses by default, which turns a
	// live stream into one long silence.
	header.Set("X-Accel-Buffering", "no")

	listener, missed := s.events.subscribe(lastEventID(request))

	defer s.events.unsubscribe(listener)

	writer.WriteHeader(http.StatusOK)

	// How long the browser waits before reconnecting. EventSource's own
	// default is three seconds; a local daemon that restarts on every saved
	// file deserves to be picked up faster than that.
	if !write(writer, s.logger, "retry: 1000\n\n") {
		return
	}
	for _, event := range missed {
		if !writeEvent(writer, s.logger, event) {
			return
		}
	}
	flusher.Flush()

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-request.Context().Done():
			return

		case <-s.events.done:
			// The daemon is stopping. Ending here is what lets Shutdown
			// finish: it waits for requests still in flight, and this one
			// would otherwise outlive the grace period and turn a clean stop
			// into a reported failure.
			return

		case <-listener.lagging:
			// Hanging up is the recovery. The browser comes back with
			// Last-Event-ID and the replay above fills the gap.
			s.logger.Warn("event stream fell behind, hanging up so it reconnects",
				"remote", request.RemoteAddr)
			return

		case event := <-listener.events:
			if !writeEvent(writer, s.logger, event) {
				return
			}
			flusher.Flush()

		case <-heartbeat.C:
			// A comment: legal SSE, ignored by every client, and enough to
			// keep the connection from being reclaimed as idle.
			if !write(writer, s.logger, ": keep-alive\n\n") {
				return
			}
			flusher.Flush()
		}
	}
}

// lastEventID reads where a reconnecting stream left off, or nil on a first
// connection.
//
// Nil and zero are different answers and the type says so. A first connection
// has just fetched the state it needs through the ordinary routes, and
// replaying the whole buffer at it would only make it fetch that state again —
// whereas "everything after event zero" is exactly the whole buffer, which is
// what a reconnection from the very beginning legitimately asks for.
func lastEventID(request *http.Request) *uint64 {
	raw := request.Header.Get("Last-Event-ID")
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		// A client-supplied header this daemon did not write. Treating it as
		// a first connection loses nothing: everything the interface needs is
		// available through the ordinary routes.
		return nil
	}
	return &parsed
}

func writeEvent(writer http.ResponseWriter, logger *slog.Logger, event streamEvent) bool {
	// The data field must hold no newline, and it holds none: it is compact
	// JSON, where every newline inside a string is escaped. That is why the
	// payload is marshalled rather than formatted.
	return write(writer, logger, fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n",
		event.ID, event.Kind, event.Data))
}

// write reports whether the stream is still worth writing to.
//
// A failed write on an event stream is the ordinary way one ends — the tab
// was closed — so it is a reason to stop rather than an error to report. It
// is still recorded, at a level that says as much.
func write(writer http.ResponseWriter, logger *slog.Logger, text string) bool {
	if _, err := writer.Write([]byte(text)); err != nil {
		logger.Debug("event stream ended while writing", "error", err)
		return false
	}
	return true
}

type logPayload struct {
	Executions []executionView `json:"executions"`
}

// handleLog answers with the git commands the daemon still remembers.
//
// The backlog and the stream are two halves of one promise, split the way
// HTTP splits things: this route is the collection, /api/events is what
// happens next.
func (s *Server) handleLog(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, s.logger, http.StatusOK, logPayload{Executions: s.events.Executions()})
}
