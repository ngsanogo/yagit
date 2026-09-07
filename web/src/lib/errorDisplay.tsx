import type { ReactNode } from 'react';

import { ApiError } from '../api/client';
import { GitFailureDetail } from '../components/GitFailureDetail';

/**
 * Turns a query or mutation error into what the user should read.
 *
 * The message alone is enough for a daemon that stopped; git failures need the
 * stderr block, and this is the one function that knows the difference.
 */
export function errorDescription(error: Error): ReactNode {
  if (error instanceof ApiError && error.git !== undefined) {
    return (
      <div className="flex flex-col items-center gap-3">
        <p className="max-w-sm text-sm text-ink-muted">{error.message}</p>
        <GitFailureDetail failure={error.git} />
      </div>
    );
  }
  return error.message;
}

/**
 * The refusal the daemon expresses with a status code its message does not
 * carry.
 *
 * 413 arrives for two different reasons and only one of them explains itself.
 * A selection over the body cap comes with the daemon's own sentence about it,
 * and needs nothing from here. A diff over the cap comes wrapped in git's words
 * about a command that was stopped mid-write, which reads as a git failure and
 * is not one: the daemon stopped it on purpose rather than hold a hundred
 * megabytes of a lockfile. A command on the error is what separates the two —
 * only the second ran one.
 *
 * Everything else the daemon refuses says why in the message it sends, and the
 * message is shown; repeating a status code back at those would be noise.
 */
export function refusalHeading(error: Error): string | undefined {
  if (!(error instanceof ApiError) || error.status !== 413) {
    return undefined;
  }
  return error.git === undefined
    ? 'That selection is too large to send'
    : 'That diff is too large to read';
}

/**
 * The same failure on one line: what git said, and what it was asked.
 *
 * A row of the history is a fixed 56 pixels (app/geometry.ts) and has no room
 * for the block above. So the block is flattened rather than a second wording
 * being invented for it — the same three facts, from the same failure.
 *
 * Their order is the block's, reversed, and the reason is the clipping. The
 * line shares a row with a graph that may take a third of its width, and what
 * does not fit is cut from the right. Leading with the command would spend
 * that width on the half the reader can already guess from the screen they
 * are looking at, and cut git's own words — which are the promise.
 *
 * Undefined when the failure did not come from git, because the message is
 * then the whole of it and repeating it under itself says nothing.
 */
export function gitFailureLine(error: Error): string | undefined {
  if (!(error instanceof ApiError) || error.git === undefined) {
    return undefined;
  }
  return `${error.git.stderr} — exit ${error.git.exit_code} — ${error.git.command}`;
}
