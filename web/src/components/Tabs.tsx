import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';

import { cx } from '../lib/cx';

export interface TabItem {
  id: string;
  label: string;
  /** Secondary text, such as a repository's path, already shortened to fit. */
  detail?: string;
  /**
   * The whole of what the line above stands for, one hover away.
   *
   * Not the only copy of anything: what is drawn has to be enough to tell two
   * tabs apart on its own, because a title is a pointer affordance and a
   * keyboard reaches nothing here. It is for the reader whose `detail` was cut
   * to fit the strip — or replaced by a shorter fact about the same thing,
   * which leaves the identifying string drawn nowhere at all.
   */
  detailInFull?: string;
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
  const listRef = useRef<HTMLDivElement>(null);

  /*
   * Whether the strip has more tabs in each direction.
   *
   * Past five repositories the strip overflows, and it did so with nothing on
   * screen saying it had: the last tab was cut mid-word against a hard edge,
   * which reads as a rendering fault rather than as "scroll for more". The
   * scrollbar cannot be the answer — every platform yagit runs on hides it
   * until something moves, and one of them never gives it height at all.
   */
  const [more, setMore] = useState({ before: false, after: false });

  const measure = useCallback(() => {
    const list = listRef.current;
    if (list === null) {
      return;
    }
    // A pixel of slack: a strip scrolled to its end reports a fractional
    // pixel short of it often enough, and a fade that never goes away is a
    // fade that has stopped meaning "there is more".
    const before = list.scrollLeft > 1;
    const after = list.scrollLeft + list.clientWidth < list.scrollWidth - 1;
    setMore((current) => {
      // The same object back, not an equal one: React bails out of the
      // re-render when the state is unchanged by reference, and that is what
      // makes measuring after every render cost a comparison and no paint.
      if (current.before === before && current.after === after) {
        return current;
      }
      return { before, after };
    });
  }, []);

  // After every render rather than off a dependency list: a tab arriving, a
  // tab going and a label changing length all change how much there is to
  // scroll, and a list of the causes is a list that will one day be missing
  // the fourth.
  useEffect(measure);

  useEffect(() => {
    window.addEventListener('resize', measure);
    return () => window.removeEventListener('resize', measure);
  }, [measure]);

  /*
   * The selected tab is scrolled to, and it has to be.
   *
   * Selection does not always come from a click on something already visible:
   * opening a repository selects the tab it creates, at the far end of a strip
   * still scrolled to the left, and a reload restores one. Either way the
   * header would then name a repository nobody can see while the panes below
   * show its contents — the one question a tab strip exists to answer, left
   * unanswered. The keyboard path already scrolls, because focus does.
   *
   * Before the paint rather than after it: a restore selects a tab that may be
   * three screens along, and an ordinary effect would draw the strip at the
   * left edge for one frame and then jump it. The scroll is instant either
   * way, so the frame is the whole of what this buys.
   */
  useLayoutEffect(() => {
    const selected = listRef.current?.querySelector('[role="tab"][aria-selected="true"]');
    selected?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [activeId]);

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
    //
    // Backspace is bound as well, and announced as well, which is the half
    // that was missing. On the keyboards of one whole platform the key in the
    // backspace position is labelled "delete" and reports `Backspace`; a real
    // `Delete` there is a two-key chord. Binding it and promising only
    // `Delete` told those readers about the chord and left the key under their
    // finger undocumented, while it closed repositories without confirmation.
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
      // The fades below are siblings of the strip rather than children of it:
      // inside, they would scroll away with the tabs they are marking, and a
      // tablist may own nothing but tabs anyway.
      className={cx('relative flex min-w-0', className)}
    >
      <div
        ref={listRef}
        role="tablist"
        onKeyDown={handleKeyDown}
        onScroll={measure}
        className="flex min-w-0 flex-1 items-stretch gap-px overflow-x-auto"
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
                {...(onClose === undefined ? {} : { 'aria-keyshortcuts': 'Delete Backspace' })}
                // Fills in nothing the accessible name needs — a button with
                // content takes its name from the content — so this is the
                // pointer's copy of a path too long for the strip.
                {...(item.detailInFull === undefined ? {} : { title: item.detailInFull })}
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

      {more.before && <MoreTabs side="before" />}
      {more.after && <MoreTabs side="after" />}
    </div>
  );
}

/**
 * The mark on an edge with tabs behind it.
 *
 * A fade rather than a chevron, and it does not take the click it would seem
 * to offer: the strip is scrollable by every gesture the platform already has,
 * and a button that scrolls a list one notch is a control the pointer has to
 * hit repeatedly to do what a wheel does in one movement. What was missing was
 * the information, not another way to move.
 */
function MoreTabs({ side }: { side: 'before' | 'after' }) {
  return (
    <span
      aria-hidden="true"
      className={cx(
        'pointer-events-none absolute inset-y-0 w-6 from-surface',
        side === 'before' ? 'left-0 bg-linear-to-r' : 'right-0 bg-linear-to-l',
      )}
    />
  );
}
