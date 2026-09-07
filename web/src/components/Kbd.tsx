import type { ReactNode } from 'react';

import { cx } from '../lib/cx';

interface KbdProps {
  children: ReactNode;
  className?: string;
}

/** A keyboard key, in help text or a menu. */
export function Kbd({ children, className }: KbdProps) {
  return (
    <kbd
      className={cx(
        'inline-flex h-5 min-w-5 items-center justify-center rounded-sm px-1',
        'border border-line-strong bg-raised text-2xs font-medium text-ink-muted',
        className,
      )}
    >
      {children}
    </kbd>
  );
}
