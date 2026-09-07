import type { ReactNode } from 'react';

import { cx } from '../lib/cx';

interface EmptyStateProps {
  title: string;
  /**
   * What the user can do next. An empty screen that only reports the emptiness
   * is a failed screen: it has to name the next action.
   */
  description: string;
  /** Extra content below the description — git stderr, for example. */
  detail?: ReactNode;
  action?: ReactNode;
  className?: string;
}

export function EmptyState({ title, description, detail, action, className }: EmptyStateProps) {
  return (
    <div
      className={cx(
        'flex flex-col items-center justify-center gap-3 px-6 py-12 text-center',
        className,
      )}
    >
      <div className="flex flex-col gap-1">
        <p className="text-base font-medium text-ink">{title}</p>
        {description !== '' && <p className="max-w-sm text-sm text-ink-muted">{description}</p>}
      </div>
      {detail}
      {action}
    </div>
  );
}
