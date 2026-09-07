# 0007 — One SSE stream per session, not one per repository

**Status:** refines an existing decision. Server-sent events were already
chosen; how many of them was not.

## The decision

Server-sent events, and **exactly one stream for the whole session**, carrying
events for every open repository. Each event names the repository it concerns.

## Why server-sent events rather than WebSocket

Nothing flows upward. Every action the user takes is a request with a body and
a status code; the stream exists to say "this repository changed". A WebSocket
would be a bidirectional channel used in one direction, with framing, ping and
pong, and a reconnection loop to write.

`EventSource` reconnects on its own, with a back-off, resuming from
`Last-Event-ID`. It is plain HTTP, so it passes through the daemon's proxy to
Vite in development exactly as it does in production, which keeps the two
identical — the point of the single-origin model.

## Why one stream and not one per repository

Because a browser will not give you many. Over HTTP/1.1 it opens at most six
connections per origin, and the daemon serves plain HTTP on the loopback. Open
six repositories in tabs and the seventh stream is not slow — it never
connects, and every ordinary API call queues behind the six that are parked
open forever. The failure looks like the application hanging, and nothing
points at the cause.

One stream, multiplexed, has no such ceiling, and the repository identifier is
already in every event.

## The authentication detail that decides the shape

`EventSource` cannot set request headers. It cannot send `X-Yagit-Token`, and
there is no option that makes it. The stream therefore authenticates by
**cookie**, which is exactly the credential the token-for-cookie exchange
already establishes, and which `SameSite=Strict` plus the origin check already
cover.

This is worth writing down because it makes the cookie load-bearing rather than
a convenience: removing it in favour of header-only authentication would take
the event stream with it.

## Consequences

- Events are debounced at 100 ms and ignore `.git/index.lock`, which appears
  and vanishes during any write.
- Each event carries the repository identifier, and the frontend turns it into
  `queryClient.invalidateQueries({ queryKey: ['commits', repoID] })` — see
  [0005](0005-server-state-and-virtualisation.md).
- The stream is a route like any other and requires the token like any other.
- If the stream cannot be established, the interface says so. An interface that
  has silently stopped refreshing is worse than one that never refreshed.
