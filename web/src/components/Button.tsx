import type { ButtonHTMLAttributes, ReactNode, Ref } from 'react';

import { cx } from '../lib/cx';
import { Spinner } from './Spinner';

/**
 * The yagit button.
 *
 * Four variants, and one rule for choosing: `primary` is the action the
 * screen is waiting for, `danger` is the one that destroys work, `secondary`
 * is everything else, `ghost` is what should stay out of the way until it is
 * hovered. A screen has exactly one `primary` button.
 */
export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'sm' | 'md';

/*
 * Every variant states its hover and its press as tokens, danger included.
 *
 * It used to be the exception: `hover:brightness-110 active:brightness-95`, a
 * filter that knows nothing about which theme it is in. On the dark page it
 * lightened, which is the direction the accent moves too; on the light page it
 * lightened as well, towards the paper, while the primary button beside it
 * darkened away from it — so the two buttons that matter most answered the
 * same gesture in opposite directions, and the destructive one lost label
 * contrast (5.7:1 to 4.9:1) exactly as the pointer arrived on it. A filter
 * also multiplies the whole element, text included, which is why its hover
 * felt like a different kind of hover from every other button's.
 *
 * What tokens cost in exchange: a colour utility exists only for as long as
 * its token does. Tailwind builds `bg-danger-hover` out of
 * `--color-danger-hover` and emits nothing at all — no rule, no warning — for
 * a name tokens.css has not declared, so dropping one of those declarations
 * does not give the destructive button a wrong hover, it gives it none. The
 * test beside this file is what says so out loud; a screenshot of a button at
 * rest cannot.
 */
const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-accent-ink hover:bg-accent-hover active:bg-accent-active shadow-raised',
  // A visible edge and a lifted ground at rest, which is the whole of what
  // separates a button from a label. The toolbar used to draw Fetch, Pull and
  // Push as ghosts, and three verbs in muted ink read as a caption for the
  // branch beside them rather than as three things to press.
  secondary:
    'bg-raised text-ink border border-line-strong shadow-raised hover:bg-hover hover:border-ink-subtle active:bg-selected',
  ghost: 'text-ink-muted hover:bg-hover hover:text-ink active:bg-selected',
  danger: 'bg-danger text-canvas hover:bg-danger-hover active:bg-danger-active shadow-raised',
};

const SIZE_CLASSES: Record<ButtonSize, string> = {
  sm: 'h-7 px-2.5 gap-1.5 text-xs rounded-md',
  md: 'h-9 px-3.5 gap-2 text-sm rounded-md',
};

/**
 * The same heights, with no room for a label: a square that holds one glyph.
 * `size-7` and `size-9`, so an icon-only control lines up with the labelled
 * button beside it to the pixel.
 */
const SQUARE_CLASSES: Record<ButtonSize, string> = {
  sm: 'size-7 rounded-md',
  md: 'size-9 rounded-md',
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /**
   * The button is working on what it was last pressed for.
   *
   * Deliberately not the same thing as `disabled`, and deliberately not
   * implemented with it. A browser blurs an element the moment it becomes
   * disabled, so `loading` used to eject a keyboard user to <body> — the top
   * of the document, dozens of stops from the history — for the whole of a
   * fetch, a push or a commit, and then land the result where they were no
   * longer standing. SegmentedControl and Menu had both already found this
   * trap and both solve it the same way, with aria-disabled on an element
   * that keeps its place.
   *
   * So: the element stays focusable and announces itself as busy, the click
   * is refused in the handler, and the dim belongs to the other prop. A
   * control that is working is not a control that is refused — it exists to
   * be read while you wait, and 45% of any ink was about 2:1 over the light
   * theme's white.
   */
  loading?: boolean;
  /**
   * Decorative element placed before the label, usually an icon.
   *
   * Replaced by the spinner while `loading`, which is the one thing that
   * changes about the button's contents: the label stays, because a word that
   * disappears the moment it is pressed is a button whose purpose has to be
   * remembered rather than read.
   */
  leading?: ReactNode;
  /**
   * Named as an ordinary prop, which React 19 allows and which is the whole of
   * what forwardRef used to be for here.
   *
   * One thing wants it: a dialog deciding which of its buttons focus opens on.
   * Reaching for it to read or write anything else about the element is a sign
   * the state belongs in a prop instead.
   */
  ref?: Ref<HTMLButtonElement>;
}

export function Button({
  variant = 'secondary',
  size = 'md',
  loading = false,
  leading,
  disabled,
  className,
  children,
  onClick,
  ref,
  ...rest
}: ButtonProps) {
  return (
    <button
      type="button"
      ref={ref}
      // The attribute is for refusal only. It is also what Tooltip depends on
      // — a refused button drops pointer events, so the sentence explaining
      // why has to be hung on the span around it — and what stops a form
      // submitting when its one action is unavailable.
      disabled={disabled === true}
      aria-disabled={loading || undefined}
      aria-busy={loading || undefined}
      onClick={(event) => {
        if (loading) {
          // preventDefault as well as returning early: some of these are a
          // form's submit button, and a click on one of those submits the
          // form whether or not any React handler runs.
          event.preventDefault();
          return;
        }
        onClick?.(event);
      }}
      className={cx(
        'inline-flex items-center justify-center font-medium whitespace-nowrap select-none',
        'transition-colors transition-instant',
        'focus-visible:focus-ring outline-none',
        // Dimmed like every other refused control in this design system, and
        // only for the refusal: `aria-disabled:opacity-45` would take the
        // busy button with it, which is the pairing Menu's own comment
        // records as unreadable in the light theme.
        'disabled:opacity-45 disabled:pointer-events-none',
        // The one thing the pointer is told, now that a busy button is no
        // longer inert under it: the spinner says the same thing to the eye.
        loading && 'cursor-progress',
        SIZE_CLASSES[size],
        VARIANT_CLASSES[variant],
        className,
      )}
      {...rest}
    >
      {loading ? <Spinner size={size === 'sm' ? 12 : 14} /> : leading}
      {children}
    </button>
  );
}

interface IconButtonProps extends Omit<ButtonProps, 'leading' | 'children' | 'aria-label'> {
  /** The one glyph the button holds. */
  icon: ReactNode;
  /**
   * What the button does, in words. Required rather than inherited as an
   * optional attribute: a button with a glyph and no name is a button a
   * screen reader announces as "button", and there is no glyph on this screen
   * that a word could not name.
   */
  'aria-label': string;
}

/**
 * A button that is one glyph — close, copy, the theme, a menu's trigger.
 *
 * The same component underneath, so it takes every state Button takes: busy,
 * refused, pressed, focused. What it adds is the square footprint and the
 * rule that the name is not optional.
 */
export function IconButton({
  icon,
  size = 'sm',
  variant = 'ghost',
  className,
  title,
  ...rest
}: IconButtonProps) {
  return (
    <Button
      size={size}
      variant={variant}
      // The label doubles as the hover text unless the caller has something
      // longer to say: an icon-only control owes the pointer the word it owes
      // the screen reader.
      title={title ?? rest['aria-label']}
      className={cx('px-0', SQUARE_CLASSES[size], className)}
      {...rest}
    >
      {icon}
    </Button>
  );
}
