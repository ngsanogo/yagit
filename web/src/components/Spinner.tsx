import { cx } from '../lib/cx';

interface SpinnerProps {
  /** Diameter in pixels. Follows the text size of its neighbor. */
  size?: number;
  className?: string;
  /**
   * What the user is waiting for. Read out by screen readers; leave it unset
   * when visible text next to it already says the same thing.
   */
  label?: string;
}

/**
 * Loading indicator.
 *
 * An arc rather than a full circle: the gap is what makes the rotation
 * visible.
 */
export function Spinner({ size = 14, className, label }: SpinnerProps) {
  return (
    <span
      // Announced only when it carries a label of its own. Without this, a
      // button reading "Pushing" with a spinner inside is announced "Loading
      // Pushing": the decoration says the same thing as the text beside it,
      // twice.
      {...(label === undefined
        ? { 'aria-hidden': true }
        : { role: 'status' as const, 'aria-label': label })}
      className={cx('inline-block shrink-0 animate-spin', className)}
      style={{ width: size, height: size }}
    >
      <svg viewBox="0 0 16 16" fill="none" width={size} height={size} aria-hidden="true">
        <circle cx="8" cy="8" r="6.5" stroke="currentColor" strokeOpacity="0.25" strokeWidth="2" />
        <path
          d="M14.5 8a6.5 6.5 0 0 0-6.5-6.5"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
        />
      </svg>
    </span>
  );
}
