import type { ReactNode } from 'react';

import { Button } from './Button';
import { Dialog, type DialogSize } from './Dialog';
import { GitCommand } from './GitCommand';

interface ConfirmDialogBaseProps {
  open: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  title: string;
  /**
   * What confirming will do, in a sentence, where the command alone does not
   * say it.
   *
   * `git merge --no-ff --no-edit -- refs/heads/pickup` names the operation
   * exactly and still does not say that a commit is about to be made under the
   * user's signature, or how much is arriving. It becomes the dialog's
   * accessible description, so it is read out before the buttons rather than
   * found after them.
   */
  description?: string;
  /**
   * The exact git command, or commands, that run if the user confirms.
   *
   * Several because some operations really are several: discarding a
   * selection that mixes tracked and untracked files is `git restore` and then
   * `git clean`, and showing one of them would make this dialog's promise
   * half true.
   */
  command: string | readonly [string, ...string[]];
  confirmLabel: string;
  busy?: boolean;
  /**
   * Refuses the confirmation without pretending to be working on it.
   *
   * Told apart from `busy`, which draws a spinner and means the answer is
   * coming. This one means the dialog is holding something it will not send —
   * a rebase plan with a combine at the top of it — and the sentence saying
   * why belongs in `children`, next to what has to change.
   */
  confirmDisabled?: boolean;
  /**
   * How wide the box is allowed to grow — see Dialog.
   *
   * One caller passes anything but the default: a rebase plan is a list of
   * rows to be read and rearranged, and the width that suits a sentence and a
   * command line is not the width that suits forty of those.
   */
  size?: DialogSize;
  /**
   * Extra content drawn above the command — a mode picker, a count, anything
   * the confirmation needs that is not the sentence, the loss list or the
   * line itself.
   *
   * Optional so every existing caller stays a command-and-confirm dialog; the
   * reset confirmation is what first needs it, because soft/mixed/hard is a
   * choice made on the dialog rather than before it opens.
   */
  children?: ReactNode;
}

/**
 * A destructive confirmation has to name what it destroys.
 *
 * That rule used to live in a comment, and a comment does not compile: a
 * dialog for `git branch -D` could be marked destructive with no `losing` at
 * all, and the user would be asked to approve a deletion without being told
 * what disappears. The non-empty tuple is the part that matters — an empty
 * array names nothing while satisfying `readonly string[]`.
 */
type ConfirmDialogProps = ConfirmDialogBaseProps &
  (
    | {
        destructive: true;
        /**
         * What will be lost, named precisely: "3 commits", "the contents of
         * src/parser.go". At least one entry.
         */
        losing: readonly [string, ...string[]];
      }
    | { destructive?: false; losing?: readonly string[] }
  );

/**
 * Confirmation for a git operation.
 *
 * This component exists to make it structurally impossible to run a
 * destructive command without showing it first: the command is a required
 * field, not an option. A confirmation dialog with no command does not
 * compile.
 */
export function ConfirmDialog({
  open,
  onCancel,
  onConfirm,
  title,
  description,
  command,
  losing,
  confirmLabel,
  destructive = false,
  busy = false,
  confirmDisabled = false,
  size,
  children,
}: ConfirmDialogProps) {
  return (
    <Dialog
      open={open}
      onClose={onCancel}
      title={title}
      {...(size === undefined ? {} : { size })}
      {...(description === undefined ? {} : { description })}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button
            variant={destructive ? 'danger' : 'primary'}
            onClick={onConfirm}
            loading={busy}
            disabled={confirmDisabled}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-3">
        {children}

        {losing !== undefined && losing.length > 0 && (
          <div className="rounded-md border border-danger/35 bg-danger-soft/50 px-3 py-2">
            <p className="text-xs font-medium text-danger">This will permanently discard</p>
            <ul className="mt-1 list-disc pl-4 text-sm text-ink">
              {losing.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          </div>
        )}

        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-ink-muted">
            {typeof command === 'string' || command.length === 1
              ? 'yagit will run'
              : 'yagit will run, in this order'}
          </p>
          {(typeof command === 'string' ? [command] : command).map((line) => (
            <GitCommand key={line} command={line} />
          ))}
        </div>
      </div>
    </Dialog>
  );
}
