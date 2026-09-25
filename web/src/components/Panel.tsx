import { useId, type ReactNode } from 'react';

import { cx } from '../lib/cx';

interface PanelProps {
  title?: string;
  /**
   * A glyph beside the title, naming the kind of thing the panel holds.
   *
   * The sidebar is five panels of rows in the same type, and a reader
   * scanning down it finds the stashes by reading five headers. A glyph per
   * kind is what lets the eye stop before the word is read.
   */
  icon?: ReactNode;
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
 *
 * The title is drawn in two weights and read as one string. Every panel that
 * counts something writes its title as "History — 17" or "Changes — 1 staged,
 * 2 unstaged", and the count is a fact about the moment where the name is a
 * fact about the panel; drawn at one weight the two competed, and the name of
 * the panel was the part that lost. Split at the dash, the name leads and the
 * count follows in quieter ink — while the heading's accessible name is still
 * the whole sentence, which is what every test and every screen reader reads.
 */
export function Panel({ title, icon, actions, children, className, flush = false }: PanelProps) {
  const titleId = useId();
  const [name, count] = splitTitle(title);

  return (
    <section
      aria-labelledby={title === undefined ? undefined : titleId}
      className={cx(
        'flex min-w-0 flex-col overflow-hidden rounded-lg border border-line bg-surface shadow-panel',
        className,
      )}
    >
      {(title !== undefined || actions !== undefined) && (
        <header className="flex h-11 shrink-0 items-center justify-between gap-3 border-b border-line px-3">
          {title !== undefined && (
            <h2
              id={titleId}
              className="flex min-w-0 items-center gap-2 truncate text-sm font-semibold text-ink"
            >
              {icon !== undefined && <span className="shrink-0 text-ink-subtle">{icon}</span>}
              <span className="truncate">
                {name}
                {count !== undefined && (
                  <span className="font-normal text-ink-subtle">{` — ${count}`}</span>
                )}
              </span>
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

/**
 * "History — 17" into "History" and "17"; a title with no dash is all name.
 *
 * Exported for its test: the accessible name has to come back out of the two
 * halves exactly as it went in, and a separator lost or doubled would rename
 * every panel in the workbench at once.
 */
export function splitTitle(title: string | undefined): [string | undefined, string | undefined] {
  if (title === undefined) {
    return [undefined, undefined];
  }
  const at = title.indexOf(' — ');
  if (at === -1) {
    return [title, undefined];
  }
  return [title.slice(0, at), title.slice(at + ' — '.length)];
}
