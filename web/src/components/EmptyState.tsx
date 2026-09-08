import type { ReactNode } from 'react';

import { cx } from '../lib/cx';

interface EmptyStateProps {
  title: string;
  /**
   * What the user can do next. An empty screen that only reports the
   * emptiness is a failed screen: it has to name the next action.
   *
   * Optional, and the exception is exactly one shape: a read that FAILED. The
   * next action there is not a sentence this component can write — it is
   * git's own account of what went wrong, which goes in `detail`, and a
   * Retry, which goes in `action`. Anything else — nothing stashed yet, no
   * commands run yet, no results for that search — names the action or says
   * nothing worth reading. It was a required prop that eleven call sites
   * satisfied with an empty string, which is a rule with eleven exceptions
   * rather than a rule.
   */
  description?: string;
  /** Extra content below the description — git stderr, for example. */
  detail?: ReactNode;
  action?: ReactNode;
  /**
   * For a panel that is a few rows tall — the sidebar column, where the
   * generous padding is most of the panel.
   *
   * A variant rather than a `py-6` from the outside, because the two land on
   * the element together and the stylesheet decides between them: every call
   * site that tried it got 48px of padding in a 63px box, with the failure it
   * was reporting entirely below the fold.
   */
  compact?: boolean;
  className?: string;
}

export function EmptyState({
  title,
  description,
  detail,
  action,
  compact = false,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cx(
        'flex flex-col items-center justify-center gap-3 px-6 text-center',
        compact ? 'py-4' : 'py-12',
        className,
      )}
    >
      <div className="flex flex-col gap-1">
        <p className="text-base font-medium text-ink">{title}</p>
        {description !== undefined && description !== '' && (
          <p className="max-w-sm text-sm text-ink-muted">{description}</p>
        )}
      </div>
      {/* A string detail is the failure message of everything that is not git
          — the daemon stopped, the repository is gone, the request timed out —
          and those are the longest sentences this component ever shows. Given
          no element of its own it inherited the container's 14px ink across
          the full width of the panel, brighter and wider than the title above
          it. It is the same sentence as the git branch's, so it gets the same
          paragraph. Nodes are left alone: they brought their own. */}
      {typeof detail === 'string' ? (
        <p className="max-w-sm text-sm text-ink-muted">{detail}</p>
      ) : (
        detail
      )}
      {action}
    </div>
  );
}
