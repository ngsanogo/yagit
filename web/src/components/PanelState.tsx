import type { ReactNode } from 'react';

import { Button } from './Button';
import { EmptyState } from './EmptyState';
import { errorDescription } from '../lib/errorDisplay';

/**
 * What a panel shows instead of its content: while it waits, and after it
 * failed.
 *
 * Two components rather than one, because they are not the same shape. Waiting
 * is a spinner somebody centres; failing is a title and everything git said
 * underneath it. What they share is the reason they are here at all — every
 * panel in the workbench has both states, and a panel that invented its own
 * would be a panel where a failure looks different from the failure next to it.
 */

/**
 * The query a failed panel can ask again.
 *
 * Two fields of a TanStack query result rather than the result itself, so a
 * call site passes what it already holds — `retry={stashes}` — and this file
 * does not depend on a library it otherwise has no use for. `isFetching` is
 * here because the button has to say that the second attempt is running:
 * without it, a retry against a daemon that is still not answering looks like
 * a button that does nothing.
 */
export interface RetryableQuery {
  refetch: () => Promise<unknown>;
  isFetching: boolean;
}

/**
 * A failed query, reported whole.
 *
 * The title says what could not be read; the detail is the command, the exit
 * code and the raw stderr, exactly as the daemon sent them. Never "Something
 * went wrong" — a git failure the user cannot see is a git failure they cannot
 * fix.
 *
 * `retry` is the way out, and it is offered here rather than invented per
 * panel for the reason above: a failure that can be retried in the references
 * and not in the stashes is a workbench where the user has to learn which
 * panels recover. Automatic retries stay refused — App.tsx says why, and a
 * failed git command is not a network blip — but the index.lock a terminal
 * held for a second is exactly the failure a person can answer, and until now
 * the only gesture that answered it was clicking to another window and back.
 */
export function QueryErrorState({
  title,
  error,
  compact,
  retry,
}: {
  title: string;
  error: Error;
  compact?: boolean;
  retry?: RetryableQuery;
}) {
  return (
    <EmptyState
      title={title}
      detail={errorDescription(error)}
      compact={compact}
      action={
        retry === undefined ? undefined : (
          <Button
            size="sm"
            loading={retry.isFetching}
            // Not awaited, and nothing is dropped by that: what the second
            // attempt answers is this query's own state, so a failure comes
            // back as this component drawn again with the newer error in it.
            onClick={() => void retry.refetch()}
          >
            Retry
          </Button>
        )
      }
    />
  );
}

/**
 * The middle of the space a panel was given.
 *
 * `compact` is for a panel in the sidebar column, which is a few rows tall and
 * where the generous padding would be most of the panel.
 */
export function Centered({ children, compact }: { children: ReactNode; compact?: boolean }) {
  return (
    <div className={compact ? 'grid place-items-center p-4' : 'grid flex-1 place-items-center p-8'}>
      {children}
    </div>
  );
}
