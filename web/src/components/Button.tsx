import type { ButtonHTMLAttributes, ReactNode } from 'react';

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

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-accent-ink hover:bg-accent-hover active:bg-accent-active shadow-raised',
  secondary: 'bg-raised text-ink border border-line-strong hover:bg-hover active:bg-selected',
  ghost: 'text-ink-muted hover:bg-hover hover:text-ink active:bg-selected',
  danger: 'bg-danger text-canvas hover:brightness-110 active:brightness-95 shadow-raised',
};

const SIZE_CLASSES: Record<ButtonSize, string> = {
  sm: 'h-7 px-2.5 gap-1.5 text-xs rounded-sm',
  md: 'h-9 px-3.5 gap-2 text-sm rounded-md',
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Replaces the content with a loading indicator and disables the button. */
  loading?: boolean;
  /** Decorative element placed before the label, usually an icon. */
  leading?: ReactNode;
}

export function Button({
  variant = 'secondary',
  size = 'md',
  loading = false,
  leading,
  disabled,
  className,
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      type="button"
      disabled={disabled === true || loading}
      className={cx(
        'inline-flex items-center justify-center font-medium whitespace-nowrap select-none',
        'transition-colors transition-instant',
        'focus-visible:focus-ring outline-none',
        'disabled:opacity-45 disabled:pointer-events-none',
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
