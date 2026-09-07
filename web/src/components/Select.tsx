import { useId } from 'react';
import type { ReactNode, SelectHTMLAttributes } from 'react';

import { cx } from '../lib/cx';

/**
 * One choice out of a list, on a native `<select>`.
 *
 * The third of the three shapes a choice takes here, and the line between them
 * is how many options there are and whether they fit on screen at once.
 * SegmentedControl is two or three, all visible, all short — the scope of the
 * history, the side of a diff. Menu is a handful of ACTIONS behind a button. A
 * list of things the user picks between, whose length is decided by their
 * repository rather than by this code, is neither: it is a select.
 *
 * Native rather than built. A listbox with a popover, roving focus, type-ahead
 * and touch behaviour is a component; `<select>` is all of that already,
 * correct in every browser and every assistive technology, and the only thing
 * it costs is that the open list is drawn by the operating system rather than
 * from tokens.css. ADR 0018 is the same trade taken for overlays, and this is
 * it taken one step further.
 */

export interface SelectOption {
  value: string;
  label: string;
}

interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, 'id' | 'children'> {
  label: string;
  options: readonly SelectOption[];
  /** Permanent explanation, below the control. */
  hint?: ReactNode;
  /**
   * Keeps the label out of the picture and in the accessibility tree.
   *
   * For a control repeated down a list, where a column heading already names
   * what every one of them chooses and printing that word beside each row
   * would be the same noun forty times. The label is still there — sr-only is
   * not `aria-label`, and a select with no name is a select a screen reader
   * announces as nothing.
   */
  hideLabel?: boolean;
}

export function Select({
  label,
  options,
  hint,
  hideLabel = false,
  className,
  ...rest
}: SelectProps) {
  const selectId = useId();
  const hintId = `${selectId}-hint`;

  return (
    <div className={cx('flex min-w-0 flex-col gap-1.5', className)}>
      <label
        htmlFor={selectId}
        className={cx(hideLabel ? 'sr-only' : 'text-xs font-medium text-ink-muted')}
      >
        {label}
      </label>

      <select
        id={selectId}
        aria-describedby={hint === undefined ? undefined : hintId}
        className={cx(
          'h-9 w-full min-w-0 rounded-md bg-sunken px-2.5 text-sm text-ink',
          'border border-line-strong transition-colors transition-instant outline-none',
          'hover:border-ink-subtle focus-visible:focus-ring',
          'disabled:opacity-45',
        )}
        {...rest}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>

      {hint !== undefined && (
        <p id={hintId} className="text-2xs text-ink-subtle">
          {hint}
        </p>
      )}
    </div>
  );
}
