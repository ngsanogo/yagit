import { useCallback, useEffect, useId, useLayoutEffect, useRef } from 'react';
import type { ReactNode, RefObject } from 'react';

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
 * `medium` has one caller: the box for adding a repository, which is a form
 * with a scan, a list and a path field in it — too much for `default` and not
 * a document. It is here because that caller had been writing
 * `className="max-w-xl"`, which is exactly the silent coin-toss the paragraph
 * above describes.
 *
 * `wide` has one caller: a rebase plan, which is a list of rows to be read one
 * by one rather than a sentence and a command.
 */
export type DialogSize = 'default' | 'medium' | 'wide';

const DIALOG_WIDTH: Record<DialogSize, string> = {
  default: 'max-w-lg',
  medium: 'max-w-xl',
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
  /**
   * Where focus lands when the box opens.
   *
   * `autoFocus` cannot do this job here, and it is worth naming why before
   * somebody reaches for it again: React never renders the attribute, it
   * calls focus() itself during the commit — a moment when a dialog that has
   * not been shown yet is still `display: none` and has nothing focusable
   * inside it. showModal() then runs the platform's own focusing steps and
   * lands on the first focusable child, which in a confirmation is the button
   * that copies the command rather than either answer to the question.
   *
   * Left unset, that platform default stands, which is the right answer for a
   * dialog whose first control is the one to use.
   */
  initialFocus?: RefObject<HTMLElement | null>;
  /**
   * The dialog is waiting on the command it has already sent.
   *
   * It refuses Escape for the duration. A confirmation greys out its own
   * Cancel button while the command runs, and a keyboard that can do what the
   * button refuses is the interface contradicting itself: the box disappears,
   * the `git push --force` it started carries on, and the only reading
   * available to the user is that they stopped it.
   */
  busy?: boolean;
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
  initialFocus,
  busy = false,
  className,
}: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);

  /**
   * Closes this component performed itself, whose events have not arrived yet.
   *
   * close() queues the `close` event rather than firing it inline, and both of
   * the closes below are ours: the one the parent asks for by setting `open`
   * false, and the one on the way out, when a caller dismisses the box by
   * rendering nothing. Reporting either back would be an echo — in a
   * confirmation, a cancel landing immediately after the confirm.
   *
   * A count of them rather than a flag holding what the parent last wanted,
   * which is what stood here and cannot answer the question any more. React
   * mounts a component, runs its cleanups and mounts it again in development,
   * so the unmount path below fires once against a dialog that is shown again
   * in the same breath; its queued close event then arrives while the parent
   * still wants the box open, and a flag reading that answer takes the echo
   * for the user's dismissal and shuts the dialog the instant it opens.
   */
  const unreportedCloses = useRef(0);

  /**
   * Whoever had focus when the box opened, captured because the platform's
   * own copy of it is unreachable half the time.
   *
   * A <dialog> remembers this itself and hands focus back when it closes —
   * but only for a dialog that is still in the document when the close
   * happens, and this one usually is not: every caller renders it with `open`
   * hardcoded true and dismisses it by rendering nothing, so React removes a
   * <dialog> that is still showing and no close ever runs. Escape looked
   * correct only because the browser closes the element before React hears
   * about it. Cancel, a confirm that resolved and a search result each left
   * the caret on <body> — the top of the page, and a long walk back to the
   * commit the user was reading.
   */
  const openerRef = useRef<HTMLElement | null>(null);

  const titleId = useId();
  const descriptionId = useId();

  /**
   * Puts focus back where it came from, when nothing else has.
   *
   * The guard is what keeps this from fighting the platform: after a close
   * the browser has usually restored focus already, and after a dismissal
   * that moved focus somewhere deliberate — a menu, the next dialog — that
   * destination must stand. Only a caret sitting on <body> means nobody
   * claimed it. Memoized so the unmount effect below can depend on it and
   * still run only on unmount.
   */
  const returnFocus = useCallback(() => {
    const opener = openerRef.current;
    openerRef.current = null;

    if (opener === null || !opener.isConnected) {
      return;
    }
    if (document.activeElement !== null && document.activeElement !== document.body) {
      return;
    }

    // Without a scroll: the control that opened the box has not moved, and
    // dragging the page back to it would undo a scroll made since.
    opener.focus({ preventScroll: true });
  }, []);

  /**
   * The only way this component closes the box, so that no close of ours can
   * be counted twice or not at all.
   *
   * Counted before the call rather than after it: the event is queued today,
   * and a handler that ran inline would otherwise reach a count that had not
   * been made yet.
   */
  const closeQuietly = useCallback((element: HTMLDialogElement) => {
    unreportedCloses.current += 1;
    element.close();
  }, []);

  useEffect(() => {
    const element = dialogRef.current;
    if (element === null) {
      return;
    }

    if (open && !element.open) {
      openerRef.current =
        document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      initialFocus?.current?.focus({ preventScroll: true });
    } else if (!open && element.open) {
      closeQuietly(element);
      returnFocus();
    }
  }, [open, initialFocus, returnFocus, closeQuietly]);

  /*
   * The dismissal the platform never gets to see.
   *
   * A layout cleanup rather than a passive one, and the difference is the
   * whole fix: React runs layout destroys while the node is still in the
   * document and passive ones a frame after it has gone, and closing a
   * <dialog> that has already been removed restores focus to nothing.
   * Closing it here makes every exit behave the way Escape does — the one
   * that always worked.
   *
   * It goes through closeQuietly for the same reason everything else does:
   * the event this close queues arrives after the caller has unmounted us,
   * and reported back it would be a cancel nobody asked for.
   */
  useLayoutEffect(() => {
    // Read on the way in, not on the way out: a ref React fills is the one
    // thing a cleanup cannot count on still holding what it wants, and the
    // element behind this one never changes for as long as the component
    // lives.
    const element = dialogRef.current;

    return () => {
      if (element !== null && element.open) {
        closeQuietly(element);
      }
      returnFocus();
    };
  }, [returnFocus, closeQuietly]);

  /*
   * Only `close`, and only the ones this component did not ask for.
   *
   * Listening to `cancel` for the dismissal as well called onClose twice for a
   * single Escape: the browser fires `cancel`, and then `close` once the
   * dialog is gone. The `cancel` handler on the element below is not that —
   * it reports nothing and only ever refuses, so the pair cannot come back.
   *
   * What is left after the count is subtracted is a close the user made: the
   * Escape the platform honoured. That is the one worth telling the parent
   * about.
   */
  const handleClose = () => {
    if (unreportedCloses.current > 0) {
      unreportedCloses.current -= 1;
      return;
    }
    onClose();
  };

  return (
    <dialog
      ref={dialogRef}
      onClose={handleClose}
      // Escape is the platform's, and this is the platform's own way of
      // declining it: prevent the default on `cancel` and no close is queued,
      // so nothing has to be undone afterwards.
      onCancel={(event) => {
        if (busy) {
          event.preventDefault();
        }
      }}
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
