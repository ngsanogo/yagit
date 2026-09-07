import { useId, type ReactNode } from 'react';

import { cx } from '../lib/cx';

interface PanelProps {
  title?: string;
  /** Actions aligned to the right of the title. */
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
  /** Drops the inner padding, for content that handles its own edge. */
  flush?: boolean;
}

/**
 * Surface container.
 *
 * The only component allowed to paint a panel background: as soon as a view
 * paints one itself, the surface hierarchy starts drifting from one screen to
 * the next.
 *
 * A titled panel is a NAMED region, not a bare section. A <section> with no
 * accessible name is not a landmark at all — it is invisible to the shortcut
 * every screen reader has for moving between the parts of a page, and this
 * application is four or five panels side by side. Naming them is what turns
 * "History", "Commit", "Changes" and "References" into places somebody can
 * jump to instead of scrolling past.
 */
export function Panel({ title, actions, children, className, flush = false }: PanelProps) {
  const titleId = useId();

  return (
    <section
      aria-labelledby={title === undefined ? undefined : titleId}
      className={cx(
        'flex min-w-0 flex-col overflow-hidden rounded-lg border border-line bg-surface',
        className,
      )}
    >
      {(title !== undefined || actions !== undefined) && (
        <header className="flex h-10 shrink-0 items-center justify-between gap-3 border-b border-line px-3">
          {title !== undefined && (
            <h2
              id={titleId}
              className="truncate text-xs font-semibold tracking-wide text-ink-muted uppercase"
            >
              {title}
            </h2>
          )}
          {actions !== undefined && (
            <div className="flex shrink-0 items-center gap-1">{actions}</div>
          )}
        </header>
      )}
      <div className={cx('min-h-0 flex-1', flush ? '' : 'p-3')}>{children}</div>
    </section>
  );
}
