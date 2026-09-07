import type { ReactNode } from 'react';

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
 * A failed query, reported whole.
 *
 * The title says what could not be read; the detail is the command, the exit
 * code and the raw stderr, exactly as the daemon sent them. Never "Something
 * went wrong" — a git failure the user cannot see is a git failure they cannot
 * fix.
 */
export function QueryErrorState({
  title,
  error,
  compact,
}: {
  title: string;
  error: Error;
  compact?: boolean;
}) {
  return (
    <EmptyState
      title={title}
      description=""
      detail={errorDescription(error)}
      className={compact ? 'py-6' : undefined}
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
