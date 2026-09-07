import type { ReactNode } from 'react';

import { cx } from '../lib/cx';

export type ToastTone = 'info' | 'success' | 'warning' | 'danger';

const TONE_ACCENT: Record<ToastTone, string> = {
  info: 'bg-info',
  success: 'bg-success',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

interface ToastProps {
  tone?: ToastTone;
  title: string;
  /**
   * The detail of what happened. On a git error this is where the raw stderr
   * goes: a notification that only says "failed" is a useless notification.
   */
  detail?: ReactNode;
  action?: ReactNode;
  onDismiss?: () => void;
  className?: string;
}

export function Toast({ tone = 'info', title, detail, action, onDismiss, className }: ToastProps) {
  return (
    <div
      role="status"
      className={cx(
        'flex w-full max-w-md items-start gap-3 overflow-hidden rounded-lg',
        'border border-line-strong bg-raised pr-3 shadow-popover',
        className,
      )}
    >
      {/* A colored strip rather than an icon: it fits in one spacing step and
          reads just as fast, without adding one more glyph to the screen. */}
      <span aria-hidden="true" className={cx('w-1 shrink-0 self-stretch', TONE_ACCENT[tone])} />

      <div className="flex min-w-0 flex-1 flex-col gap-1 py-2.5">
        <p className="text-sm font-medium text-ink">{title}</p>
        {detail !== undefined && (
          <div className="font-mono text-2xs break-words whitespace-pre-wrap text-ink-muted">
            {detail}
          </div>
        )}
        {action !== undefined && <div className="mt-1 flex gap-2">{action}</div>}
      </div>

      {onDismiss !== undefined && (
        <button
          type="button"
          onClick={onDismiss}
          aria-label="Dismiss"
          className="mt-2.5 shrink-0 rounded-sm p-1 text-ink-subtle outline-none transition-colors transition-instant hover:text-ink focus-visible:focus-ring"
        >
          <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
            <path
              d="m3 3 6 6M9 3l-6 6"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
            />
          </svg>
        </button>
      )}
    </div>
  );
}
