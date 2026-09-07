package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ngsanogo/yagit/internal/api"
	"github.com/ngsanogo/yagit/internal/git"
	"github.com/ngsanogo/yagit/internal/repo"
)

// A daemon that cannot stop cleanly reports a failure that did not happen.
//
// http.Server.Shutdown closes idle connections and then waits for the requests
// still in flight, and an event stream is a request that by design never ends:
// it returns when the client goes away, when it falls behind, or when a write
// fails, and a signal is none of those. So the one thing worth proving here is
// that a stream held open does not make a clean stop look like a crash.
func TestAnOpenEventStreamDoesNotHoldUpShutdown(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	const token = "a-token-long-enough-to-be-accepted-by-the-daemon"

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolving the root: %v", err)
	}

	events := api.NewEventStream(slog.New(slog.DiscardHandler))
	runner := git.NewRunner(events.PublishExecution)
	registry, err := repo.NewRegistry(root, runner)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	server, err := api.NewServer(api.Options{
		Registry: registry,
		Runner:   runner,
		Token:    token,
		Logger:   slog.New(slog.DiscardHandler),
		Frontend: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		Events:   events,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	httpServer := newHTTPServer(server.Handler(), events)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()

	request, err := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String()+"/api/events", nil)
	if err != nil {
		t.Fatalf("building the request: %v", err)
	}
	request.Header.Set("X-Yagit-Token", token)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("opening the stream: %v", err)
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("closing the stream: %v", err)
		}
	}()

	// The stream is live: the reconnection delay is written and flushed
	// before the handler settles into its select.
	buffer := make([]byte, len("retry: 1000\n\n"))
	if _, err := response.Body.Read(buffer); err != nil {
		t.Fatalf("reading the stream: %v", err)
	}

	// A budget far shorter than the daemon's own grace period, so that a
	// Shutdown which merely waits it out fails here rather than passing
	// slowly.
	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	started := time.Now()
	if err := httpServer.Shutdown(shutdownContext); err != nil {
		t.Fatalf("shutdown took %s and returned %v; a clean stop must not be reported as a failure",
			time.Since(started), err)
	}

	if err := <-served; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("Serve: %v", err)
	}
}
