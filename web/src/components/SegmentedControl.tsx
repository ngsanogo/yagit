import type { KeyboardEvent, ReactNode } from 'react';

import { cx } from '../lib/cx';

/**
 * A switch between two or three views of the same thing.
 *
 * Not Tabs. Tabs open and close and each one holds a different object — a
 * repository. This holds one object and switches which face of it is shown,
 * and the difference matters to a screen reader: this is a radio group, and
 * that is what it renders.
 *
 * Every option is visible at once, which is the point. A dropdown would hide
 * the fact that there is uncommitted work behind it, and a single button whose
 * label changes has to say what is true and what will happen in the same word
 * — the reader cannot tell which.
 *
 * For two or three short options. Past that they stop fitting side by side and
 * the answer is a menu.
 */

export interface Segment<Value extends string> {
  value: Value;
  label: string;
  /** A count or a state, shown after the label. */
  badge?: ReactNode;
}

interface SegmentedControlProps<Value extends string> {
  label: string;
  segments: readonly Segment<Value>[];
  value: Value;
  onChange: (value: Value) => void;
  className?: string;
  /**
   * Locks the choice while the answer to the last one is still coming, or
   * while the operation it describes is running.
   *
   * A switch whose value is a question asked of the daemon cannot stay live
   * between the click and the answer: two clicks are two requests, and which
   * one the group ends up showing would be whichever the network returned
   * last. Greyed rather than merely ignored, because a control that takes a
   * click and does nothing reads as broken.
   */
  disabled?: boolean;
}

export function SegmentedControl<Value extends string>({
  label,
  segments,
  value,
  onChange,
  className,
  disabled = false,
}: SegmentedControlProps<Value>) {
  /*
   * Keyboard handling for a radio group, which is not the same as for a row of
   * buttons — nor the same as the tab bar.
   *
   * A radio group is ONE stop in the page's tab order, the checked segment,
   * and the arrows move within it. They also CHECK what they move to, which is
   * where this parts company with Tabs: a radio group's whole purpose is its
   * selection, while arrowing past three repository tabs must not open all
   * three. Announcing "radio button, 1 of 2" and then doing nothing when the
   * arrows are pressed is the contract the role promised, broken.
   */
  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) {
      return;
    }
    const current = segments.findIndex((segment) => segment.value === value);
    if (current === -1) {
      return;
    }

    const target = {
      ArrowLeft: current - 1,
      ArrowUp: current - 1,
      ArrowRight: current + 1,
      ArrowDown: current + 1,
      Home: 0,
      End: segments.length - 1,
    }[event.key];
    if (target === undefined) {
      return;
    }

    // Wraps around, as a radio group does: a dead end at each edge is the
    // thing people report as "the arrows stopped working".
    const wrapped = (target + segments.length) % segments.length;
    const segment = segments[wrapped];
    if (segment === undefined) {
      return;
    }

    event.preventDefault();
    onChange(segment.value);
    // Focus follows the check. The roving tabindex moves with it on the next
    // render, and without this the focus ring stays on a segment that is no
    // longer the group's one tab stop.
    Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]'))[
      wrapped
    ]?.focus();
  };

  return (
    <div
      role="radiogroup"
      aria-label={label}
      onKeyDown={handleKeyDown}
      className={cx(
        'inline-flex items-center gap-0.5 rounded-md border border-line bg-sunken p-0.5',
        className,
      )}
    >
      {segments.map((segment) => {
        const active = segment.value === value;
        return (
          <button
            key={segment.value}
            type="button"
            role="radio"
            aria-checked={active}
            // Roving tabindex: the checked segment is the group's single stop
            // in the page's tab order, and the arrows do the rest.
            tabIndex={active ? 0 : -1}
            // aria-disabled and not the DOM attribute: a disabled button is
            // removed from the tab order, and a radio group whose checked
            // segment cannot be focused loses its one stop — the focus ring
            // jumps out of the dialog mid-operation. This keeps the group
            // where it was and refuses the click.
            aria-disabled={disabled || undefined}
            onClick={disabled ? undefined : () => onChange(segment.value)}
            className={cx(
              'inline-flex h-6 items-center gap-1.5 rounded-sm px-2.5',
              'text-xs font-medium whitespace-nowrap',
              'transition-colors transition-instant outline-none focus-visible:focus-ring',
              active ? 'bg-raised text-ink shadow-raised' : 'text-ink-muted hover:text-ink',
              disabled && 'opacity-45',
            )}
          >
            {segment.label}
            {segment.badge}
          </button>
        );
      })}
    </div>
  );
}
