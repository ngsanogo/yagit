import { cloneElement, useId } from 'react';
import type { ReactElement } from 'react';

import { cx } from '../lib/cx';

interface TooltipProps {
  label: string;
  /**
   * A single element, not arbitrary children: the bubble has to name the
   * thing it describes, and that means putting an aria-describedby on it.
   */
  children: ReactElement<{ 'aria-describedby'?: string }>;

  /**
   * Which edge the bubble hangs from. Centred over its anchor by default.
   *
   * `end` is for an anchor at the right of its row: a centred bubble on the
   * last button of a toolbar reaches half its width past the window, and
   * nothing here measures anything to notice. Two words of layout beat a
   * positioning engine for the two places that need it.
   */
  align?: 'center' | 'end';
  className?: string;
}

/**
 * Tooltip on hover and on keyboard focus.
 *
 * Pure CSS: no measurement, no portal, no repositioning. That assumes it has
 * room to show above its anchor, which holds everywhere yagit uses it. The
 * day that stops being true, it will need real positioning — not before, and
 * ADR 0018 says where it would come from.
 *
 * It is what a disabled control explains itself with, and the only thing that
 * can. A `title` attribute on a disabled Button never appears: Button drops
 * pointer events when disabled, so the browser fires no hover on it and reads
 * nothing out to a screen reader either. Here the hover lands on the span
 * around the button, and the sentence reaches the button itself through
 * aria-describedby — and the moment it is worth reading is exactly the moment
 * the button cannot be pressed.
 */
export function Tooltip({ label, children, align = 'center', className }: TooltipProps) {
  const bubbleId = useId();

  return (
    <span className={cx('group/tooltip relative inline-flex', className)}>
      {cloneElement(children, { 'aria-describedby': bubbleId })}
      <span
        id={bubbleId}
        role="tooltip"
        className={cx(
          'pointer-events-none absolute bottom-full z-50 mb-1.5',
          align === 'end' ? 'right-0' : 'left-1/2 -translate-x-1/2',
          // Wrapping, with a width to wrap at. A sentence that says why a
          // button is refused does not fit on one line, and one line is what a
          // bubble with nothing to stop it grows to.
          'max-w-xs text-pretty',
          'rounded-sm bg-raised px-2 py-1 text-2xs text-ink shadow-popover',
          // `invisible`, not opacity alone. An element at opacity 0 is still
          // in the accessibility tree, so the bubble was announced whether or
          // not it was showing; visibility takes it out until it is. The
          // opacity stays, because it is what animates.
          'invisible opacity-0 transition-opacity transition-fast',
          'group-hover/tooltip:visible group-hover/tooltip:opacity-100',
          'group-focus-within/tooltip:visible group-focus-within/tooltip:opacity-100',
        )}
      >
        {label}
      </span>
    </span>
  );
}
