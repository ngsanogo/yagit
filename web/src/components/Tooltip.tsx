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
  /**
   * Above the anchor unless told otherwise.
   *
   * `bottom` is for a control in the toolbar under the header: a bubble that
   * hangs upwards from there lands on the header's own buttons, and the
   * sentence saying why Push is refused was drawn over "Add repository…".
   * Below, it hangs into the panel gap, where nothing else is.
   */
  side?: 'top' | 'bottom';
  className?: string;
}

/**
 * Tooltip on hover and on keyboard focus.
 *
 * Pure CSS: no measurement, no portal, no repositioning. What that buys is a
 * component with no state and no lifecycle; what it costs is a bubble that
 * stays in the normal flow, and the constraint is sharper than "there is room
 * above the anchor". The bubble is clipped by the first `overflow: auto`
 * above it, so the anchor must not be inside a scroll container — room or no
 * room. A row at the top of a scrolling table hangs its bubble over the
 * container's own edge, where it is cut in half or removed entirely, and
 * scrolling cannot bring it back because the row is already at the top of the
 * range. Menu.tsx walked into the same trap first and says so: it is on the
 * top layer precisely because the references scroll inside their panel.
 *
 * So inside a scroll container, use a native `title` instead: the browser
 * draws it outside the page and nothing clips it, and it is already this
 * codebase's idiom for hover text that is worth reading and not worth
 * blocking on. The day a bubble with real styling is needed inside a scroller,
 * this needs the top layer and the measure-on-open Menu already implements —
 * which reverses the third bullet of ADR 0018 and therefore starts with a new
 * ADR saying the premise stopped holding.
 *
 * It is what a disabled control explains itself with, and the only thing that
 * can. A `title` attribute on a disabled Button never appears: Button drops
 * pointer events when disabled, so the browser fires no hover on it and reads
 * nothing out to a screen reader either. Here the hover lands on the span
 * around the button, and the sentence reaches the button itself through
 * aria-describedby — and the moment it is worth reading is exactly the moment
 * the button cannot be pressed.
 */
export function Tooltip({
  label,
  children,
  align = 'center',
  side = 'top',
  className,
}: TooltipProps) {
  const bubbleId = useId();

  return (
    <span className={cx('group/tooltip relative inline-flex', className)}>
      {cloneElement(children, { 'aria-describedby': bubbleId })}
      <span
        id={bubbleId}
        role="tooltip"
        className={cx(
          'pointer-events-none absolute z-50',
          side === 'top' ? 'bottom-full mb-1.5' : 'top-full mt-1.5',
          align === 'end' ? 'right-0' : 'left-1/2 -translate-x-1/2',
          // As wide as its sentence, up to a ceiling. An absolutely positioned
          // box shrinks to fit the width of the element it is positioned
          // against, and that element is the button: a sentence over a
          // sixty-pixel Fetch was wrapped into a column three words tall,
          // one word to a line. `w-max` asks for the sentence's own width and
          // the ceiling is what makes a long refusal wrap at all.
          'w-max max-w-xs text-pretty',
          'rounded-md border border-line-strong bg-raised px-2.5 py-1.5 text-xs text-ink shadow-popover',
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
