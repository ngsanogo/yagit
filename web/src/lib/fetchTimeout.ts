import { pluralize } from './format';

/** How long the interface waits for the daemon before giving up. */
export const REQUEST_TIMEOUT_MS = 35_000;

/**
 * How long it waits for a network request that reports no progress.
 *
 * Pushing a tag is the one: it crosses a network and streams nothing, so
 * there is no liveness signal to measure and elapsed time is all there is.
 * The daemon bounds it the same way — `networkTimeout` in
 * internal/git/command.go — and this is that number plus the same room the
 * ordinary one leaves, so the deadline that fires is the daemon's.
 *
 * Everything that DOES report progress uses NETWORK_IDLE_MS below instead.
 */
export const NETWORK_TIMEOUT_MS = 615_000;

/**
 * How long a transfer may say nothing before the interface gives up on it.
 *
 * Not how long the transfer may take. Fetching, pulling, pushing and cloning
 * wait on somebody else's server over somebody's line, and four gigabytes on a
 * domestic connection is fifty-five minutes of legitimate work — which an
 * elapsed-time limit turned into a repository written most of the way and a
 * message about a network that was working. What a dead connection looks like
 * is silence, so silence is what is measured, and the countdown restarts on
 * every progress line the daemon streams.
 *
 * The daemon measures the same thing — `networkIdle` in
 * internal/git/command.go — and this is that number plus the same room the
 * ordinary one leaves, so the deadline that fires is the daemon's, which can
 * answer with what git said, rather than this one, which can only say that
 * nothing came back.
 *
 * Two constants for one policy is a duplication with a reason: the browser
 * cannot read a Go constant, and the alternative is a request that gives up
 * while the work it asked for is still running.
 */
export const NETWORK_IDLE_MS = 315_000;

/**
 * A deadline a stream can push back.
 *
 * `AbortSignal.timeout` cannot be restarted, and restarting is the whole
 * point: a transfer that is still reporting is a transfer that is still alive.
 * `alive()` is called for every line that arrives, and `settled()` stops the
 * countdown once the stream has ended — a timer left armed would abort nothing,
 * but it would keep the page awake for five minutes after the work finished.
 */
export interface IdleDeadline {
  signal: AbortSignal;
  alive(): void;
  settled(): void;
}

export function idleDeadline(afterMs: number = NETWORK_IDLE_MS): IdleDeadline {
  const controller = new AbortController();
  let timer = setTimeout(() => controller.abort(new DOMException('idle', 'TimeoutError')), afterMs);

  return {
    signal: controller.signal,
    alive() {
      clearTimeout(timer);
      timer = setTimeout(() => controller.abort(new DOMException('idle', 'TimeoutError')), afterMs);
    },
    settled() {
      clearTimeout(timer);
    },
  };
}

/**
 * How long a request waited, said the way a person would.
 *
 * "615 seconds" is arithmetic; "10 minutes" is the sentence somebody reads
 * after waiting through it. Here rather than in lib/format.ts because it
 * exists to phrase the two numbers above and nothing else.
 */
export function waitedFor(afterMs: number): string {
  const seconds = Math.round(afterMs / 1000);
  return seconds < 120
    ? pluralize(seconds, 'second')
    : pluralize(Math.round(seconds / 60), 'minute');
}

/**
 * An AbortSignal that fires when the daemon takes too long.
 *
 * Aligned with git's thirty-second command timeout plus a little room for the
 * daemon to turn a timeout into JSON rather than hanging forever on fetch.
 */
export function requestTimeoutSignal(afterMs: number = REQUEST_TIMEOUT_MS): AbortSignal {
  return AbortSignal.timeout(afterMs);
}
