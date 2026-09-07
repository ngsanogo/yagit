# 0030 — Progress for a long command rides the request that started it

**Status:** new. Completes what
[0020](0020-the-network-is-gits-and-so-are-the-credentials.md) deferred:
streaming git's progress to the interface belongs with cloning, which is the
operation that actually takes ten minutes. Applied.

## The problem

`git fetch`, `git pull` and `git push` already wait on another machine, and the
interface answers them with a busy button and the command log when they end.
That is honest for a request that finishes in seconds. It is not honest for
`git clone`, which can spend most of ten minutes writing counters to stderr
while the browser shows nothing but a spinner.

Two places look like they could carry those counters:

1. The session event stream (`GET /api/events`) — one SSE connection for the
   whole session ([0007](0007-one-event-stream.md)).
2. The HTTP response of the request that started the clone.

## Decision

**Progress lines travel as NDJSON on the clone response itself.** Each line git
writes to stderr (with `--progress`) becomes one JSON object; the final object
is either the opened repository or the failure. The browser that asked is the
only client that needs the lines, and the request is already open for up to
`networkTimeout`.

The session stream stays what it is: finished executions for the log panel, and
repository-change events that name a repository. A clone in flight has no
repository id yet — the destination is a path under `YAGIT_ROOT`, not an open
entry — so putting progress there would either invent an id for something that
does not exist, or break the rule that every event names a repository.

## What was decided against

- **Progress events on `/api/events`.** The stream is multiplexed across open
  repositories, and reconnects with `Last-Event-ID`. Progress for a clone is
  ephemeral, belongs to one request, and has nothing to resume after a
  reconnect that already lost the request.
- **A second SSE endpoint per clone.** Another long-lived connection per
  operation, with the six-connection ceiling [0007](0007-one-event-stream.md)
  already warned about, for a flow that already holds one HTTP request open.
- **Leaving the spinner alone.** That is what 0020 accepted until clone
  arrived. Clone is here.

## Consequences

- `Command.OnProgress` in `internal/git` tees stderr while the process runs and
  still keeps the full buffer for the final `Execution` and `Error`.
- **The clone route is the first consumer.** Fetch, pull and push reuse the
  same shape: progress lines, then either the references the operation moved
  or the failure. Until they did, they kept the busy-button answer.
- The browser reads the body with a stream reader under `NETWORK_TIMEOUT_MS`,
  the same ceiling the daemon already applies.
