import { useEffect, useId, useRef } from 'react';
import type { ReactNode } from 'react';

import { cx } from '../lib/cx';

/**
 * How wide the box is allowed to grow.
 *
 * A prop with two values rather than a width passed in `className`, and the
 * reason is that the second does not work: two Tailwind utilities setting the
 * same property are decided by their order in the generated stylesheet, not by
 * their order in the attribute, so a caller's `max-w-3xl` loses to this
 * component's `max-w-lg` about half the time and silently. Naming the sizes
 * here keeps one definition of each and makes the choice a thing a reader can
 * see.
 *
 * `wide` has one caller: a rebase plan, which is a list of rows to be read one
 * by one rather than a sentence and a command.
 */
export type DialogSize = 'default' | 'wide';

const DIALOG_WIDTH: Record<DialogSize, string> = {
  default: 'max-w-lg',
  wide: 'max-w-3xl',
};

interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  children?: ReactNode;
  footer?: ReactNode;
  size?: DialogSize;
  className?: string;
}

/**
 * Modal dialog.
 *
 * Built on the native <dialog> element, which brings the focus trap, Escape to
 * close, inertness for the rest of the page and the backdrop for free.
 * Reimplementing all of that in React would cost several hundred lines and be
 * less accessible. It is the first half of ADR 0018; Menu is the second.
 *
 * Maximum depth: one. There is no dialog inside a dialog.
 */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'default',
  className,
}: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  // What the parent currently wants, readable from the `close` handler, which
  // fires after the render that changed it. See the handler for why.
  const wantedOpen = useRef(open);

  const titleId = useId();
  const descriptionId = useId();

  useEffect(() => {
    const element = dialogRef.current;
    if (element === null) {
      return;
    }

    // Set before closing, not after: close() queues its event rather than
    // firing it inline, and the handler has to already know this close came
    // from here.
    wantedOpen.current = open;

    if (open && !element.open) {
      element.showModal();
    } else if (!open && element.open) {
      element.close();
    }
  }, [open]);

  /*
   * Only `close`, and only when the parent still wanted the dialog open.
   *
   * Listening to `cancel` as well called onClose twice for a single Escape:
   * the browser fires `cancel`, and then `close` once the dialog is gone.
   *
   * And `close` fires again when the effect above closes the dialog because
   * the parent already set open to false. Reporting that back would be an
   * echo — one that arrives, in a confirmation dialog, as a cancel landing
   * immediately after the confirm.
   */
  const handleClose = () => {
    if (wantedOpen.current) {
      onClose();
    }
  };

  return (
    <dialog
      ref={dialogRef}
      onClose={handleClose}
      // Without a name, Chrome's accessibility tree reports role "dialog" with
      // an empty name, and a screen reader announces nothing but "dialog".
      aria-labelledby={titleId}
      aria-describedby={description === undefined ? undefined : descriptionId}
      className={cx(
        'm-auto w-full rounded-xl border border-line-strong bg-raised p-0 text-ink',
        DIALOG_WIDTH[size],
        'shadow-dialog backdrop:bg-scrim backdrop:backdrop-blur-scrim',
        className,
      )}
    >
      <div className="flex flex-col gap-4 p-5">
        {/*
          Plain divs, not <header> and <footer>: neither is a descendant of a
          sectioning element here, so both would map to the banner and
          contentinfo landmarks — page-level landmarks, announced from inside a
          dialog.
        */}
        <div className="flex flex-col gap-1">
          <h2 id={titleId} className="text-base font-semibold text-ink">
            {title}
          </h2>
          {description !== undefined && (
            <p id={descriptionId} className="text-sm text-ink-muted">
              {description}
            </p>
          )}
        </div>

        {children}

        {footer !== undefined && <div className="flex justify-end gap-2">{footer}</div>}
      </div>
    </dialog>
  );
}
