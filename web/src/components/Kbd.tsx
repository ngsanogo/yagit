import type { ReactNode } from 'react';

import { cx } from '../lib/cx';

interface KbdProps {
  children: ReactNode;
  /**
   * What a screen reader says in place of what is drawn.
   *
   * A key is often a glyph, and a glyph is where the eye and the ear stop
   * agreeing: `⌘` is announced as "place of interest sign" by one reader and
   * skipped in silence by the next, and `↵` fares no better. Where the two
   * differ the word goes beside the glyph — the glyph hidden from the
   * accessibility tree, the word hidden from the page — so that both readings
   * name the same key.
   *
   * Left off for a key whose label is already a word. `Tab` needs no
   * translation, and a hidden "Tab" beside a visible one is the same sentence
   * twice.
   */
  label?: string;
  className?: string;
}

/** A keyboard key, in help text or a menu. */
export function Kbd({ children, label, className }: KbdProps) {
  return (
    <kbd
      className={cx(
        'inline-flex h-5 min-w-5 items-center justify-center rounded-sm px-1',
        'border border-line-strong bg-raised text-2xs font-medium text-ink-muted',
        className,
      )}
    >
      {label === undefined ? (
        children
      ) : (
        <>
          <span aria-hidden="true">{children}</span>
          <span className="sr-only">{label}</span>
        </>
      )}
    </kbd>
  );
}
