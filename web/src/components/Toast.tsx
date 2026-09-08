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
  /**
   * How many times this notification has arrived, when the host collapsed
   * repeats of it into this one card. Drawn from 2 up.
   */
  repeats?: number;
  onDismiss?: () => void;
  className?: string;
}

export function Toast({
  tone = 'info',
  title,
  detail,
  action,
  repeats = 1,
  onDismiss,
  className,
}: ToastProps) {
  return (
    <div
      // A failure interrupts; everything else waits its turn. `status` is
      // polite, which means it queues behind whatever is being read and can
      // be dropped when the queue is long — the right bargain for "Pushed 3
      // commits" and the wrong one for the toast this project cares most
      // about. A danger toast is the copy of git's stderr, it is the one that
      // never expires because the user has to act on it, and delivered
      // politely it can reach a screen reader as nothing at all.
      role={tone === 'danger' ? 'alert' : 'status'}
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
        <div className="flex items-baseline gap-2">
          <p className="min-w-0 flex-1 text-sm font-medium text-ink">{title}</p>
          {repeats > 1 && (
            <>
              {/* The glyph for the eye and the words for the reader. "×3"
                  saves the width a repeated failure has no room for, and is
                  read out as anything from "times three" to silence. */}
              <span aria-hidden="true" className="shrink-0 text-2xs text-ink-subtle tabular">
                ×{repeats}
              </span>
              <span className="sr-only">{repeats} times</span>
            </>
          )}
        </div>
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
          // Named after what it dismisses. Two failures on screen are two
          // buttons called "Dismiss", and a reader walking the page is asked
          // to choose between them with nothing to choose by.
          aria-label={`Dismiss ${title}`}
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
