import type { KeyboardEvent } from 'react';

import { cx } from '../lib/cx';

export interface TabItem {
  id: string;
  label: string;
  /** Secondary text, such as a repository's current branch. */
  detail?: string;
}

interface TabsProps {
  items: readonly TabItem[];
  activeId: string;
  onSelect: (id: string) => void;
  onClose?: (id: string) => void;
  className?: string;
}

/**
 * Tabs for the open repositories.
 *
 * One level, never nested: yagit's navigation depth is capped at two, and
 * the tabs already spend one of them.
 */
export function Tabs({ items, activeId, onSelect, onClose, className }: TabsProps) {
  /*
   * Keyboard handling for a tablist, which is not the same as for a row of
   * buttons.
   *
   * A tablist is ONE stop in the page's tab order — the selected tab — and the
   * arrows move between tabs from there. Leaving every tab tabbable, as this
   * did, means ten open repositories cost ten presses of Tab to walk past.
   *
   * Focus moves without selecting: switching repository is expensive enough
   * that arrowing past three of them should not load all three.
   */
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const tabs = Array.from(
      event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]'),
    );
    const current = tabs.indexOf(document.activeElement as HTMLButtonElement);
    if (current === -1) {
      return;
    }

    // Delete is how a keyboard closes a tab: the close button is deliberately
    // out of the tab order, so it cannot be the answer.
    if (event.key === 'Delete' || event.key === 'Backspace') {
      const item = items[current];
      if (onClose === undefined || item === undefined) {
        return;
      }
      event.preventDefault();
      onClose(item.id);
      return;
    }

    const target = {
      ArrowLeft: current - 1,
      ArrowRight: current + 1,
      Home: 0,
      End: tabs.length - 1,
    }[event.key];
    if (target === undefined) {
      return;
    }

    event.preventDefault();
    // Wraps around: at the last tab, ArrowRight returns to the first. A dead
    // end at each edge is the thing people report as "the arrows stopped
    // working".
    tabs[(target + tabs.length) % tabs.length]?.focus();
  };

  return (
    <div
      role="tablist"
      onKeyDown={handleKeyDown}
      className={cx('flex items-stretch gap-px overflow-x-auto', className)}
    >
      {items.map((item) => {
        const active = item.id === activeId;

        return (
          <div
            key={item.id}
            // Presentational: a tablist owns tabs, not the wrapper each one
            // needs for its close button and its selection line.
            //
            // Removing the wrapper from the accessibility tree promotes its
            // children into the tablist, which is the point — and also the
            // trap: everything inside becomes a child of the tablist, and a
            // tablist may own nothing but tabs. See the close button below.
            role="presentation"
            className={cx(
              'group/tab relative flex min-w-0 shrink-0 items-center',
              'transition-colors transition-instant',
              active ? 'bg-surface' : 'bg-canvas hover:bg-hover',
            )}
          >
            <button
              type="button"
              role="tab"
              aria-selected={active}
              // Roving tabindex: the selected tab is the tablist's single stop
              // in the page's tab order, and the arrows do the rest.
              tabIndex={active ? 0 : -1}
              {...(onClose === undefined ? {} : { 'aria-keyshortcuts': 'Delete' })}
              onClick={() => onSelect(item.id)}
              className={cx(
                'flex min-w-0 max-w-56 flex-col items-start gap-px py-1.5 pl-3 outline-none',
                onClose === undefined ? 'pr-3' : 'pr-1',
                'focus-visible:focus-ring',
              )}
            >
              <span
                className={cx(
                  'max-w-full truncate text-xs font-medium',
                  active ? 'text-ink' : 'text-ink-muted',
                )}
              >
                {item.label}
              </span>
              {item.detail !== undefined && (
                <span className="max-w-full truncate font-mono text-2xs text-ink-subtle">
                  {item.detail}
                </span>
              )}
            </button>

            {onClose !== undefined && (
              <button
                type="button"
                // Out of the sequential order, so the tablist stays one stop:
                // a pointer uses the button, a keyboard uses Delete on the tab.
                tabIndex={-1}
                onClick={() => onClose(item.id)}
                // Hidden from assistive technology, deliberately, and this is
                // the only way to have both halves.
                //
                // The presentational wrapper promotes this button into the
                // tablist, and a tablist may own nothing but tabs — Chromium
                // reported "Element has children which are not allowed:
                // button[aria-label]". Nesting it inside the tab instead trades
                // that for an interactive control inside a widget that cannot
                // own one.
                //
                // So it is what it already was in practice: a pointer
                // affordance. Nothing is lost, because the keyboard path is not
                // this button — it is Delete on the tab itself, announced by
                // the aria-keyshortcuts above. An aria-label here named a
                // control no screen reader could reach anyway.
                aria-hidden="true"
                className={cx(
                  'mr-1.5 rounded-sm p-1 text-ink-subtle outline-none',
                  'transition-opacity transition-instant hover:bg-selected hover:text-ink',
                  'focus-visible:focus-ring focus-visible:opacity-100',
                  active ? 'opacity-100' : 'opacity-0 group-hover/tab:opacity-100',
                )}
              >
                <svg width="10" height="10" viewBox="0 0 12 12" fill="none" aria-hidden="true">
                  <path
                    d="m3 3 6 6M9 3l-6 6"
                    stroke="currentColor"
                    strokeWidth="1.6"
                    strokeLinecap="round"
                  />
                </svg>
              </button>
            )}

            {/* The selection line is overlaid rather than drawn as a border:
                a border would add two pixels to the height of the active tab
                and make the bar jump on every switch. */}
            {active && (
              <span aria-hidden="true" className="absolute inset-x-0 top-0 h-0.5 bg-accent" />
            )}
          </div>
        );
      })}
    </div>
  );
}
