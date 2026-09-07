import { useId } from 'react';
import type { InputHTMLAttributes, ReactNode } from 'react';

import { cx } from '../lib/cx';

interface FieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'id'> {
  label: string;
  /** Permanent explanation, below the field. */
  hint?: ReactNode;
  /**
   * Error message. Its presence colors the field and replaces the hint: the
   * user needs to see what is blocking, not what was expected in general.
   */
  error?: string;
  /** Element inside the field, on the right: unit, button, indicator. */
  trailing?: ReactNode;
}

export function Field({ label, hint, error, trailing, className, ...rest }: FieldProps) {
  const inputId = useId();
  const descriptionId = `${inputId}-description`;
  const hasError = error !== undefined && error !== '';

  return (
    <div className={cx('flex min-w-0 flex-col gap-1.5', className)}>
      <label htmlFor={inputId} className="text-xs font-medium text-ink-muted">
        {label}
      </label>

      <div className="relative flex items-center">
        <input
          id={inputId}
          aria-invalid={hasError}
          aria-describedby={hasError || hint !== undefined ? descriptionId : undefined}
          className={cx(
            'h-9 w-full min-w-0 rounded-md bg-sunken px-2.5 text-sm text-ink',
            'border transition-colors transition-instant outline-none',
            'placeholder:text-ink-subtle',
            'focus-visible:focus-ring',
            'disabled:opacity-45',
            hasError ? 'border-danger' : 'border-line-strong hover:border-ink-subtle',
            trailing !== undefined && 'pr-9',
          )}
          {...rest}
        />
        {trailing !== undefined && (
          <div className="absolute right-2 flex items-center text-ink-subtle">{trailing}</div>
        )}
      </div>

      {(hasError || hint !== undefined) && (
        <p
          id={descriptionId}
          className={cx('text-2xs', hasError ? 'text-danger' : 'text-ink-subtle')}
        >
          {hasError ? error : hint}
        </p>
      )}
    </div>
  );
}
