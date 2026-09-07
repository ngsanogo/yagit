import { useState } from 'react';

import type { Submodule } from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';

/**
 * Pinning another repository inside this one, and unpinning it.
 *
 * Both are commits waiting to happen: a gitlink is a path in the index, so
 * each of these leaves a staged change the user still has to write a message
 * for. Both dialogs say so, because "added" reads as finished and it is not.
 */

export interface PendingSubmoduleRemove {
  submodule: Submodule;
  force: boolean;
  /** BOTH lines: git has no `submodule remove`. */
  commands: readonly [string, ...string[]];
}

export function AddSubmoduleDialog({
  busy,
  plan,
  onCancel,
  onPlan,
  onAdd,
}: {
  busy: boolean;
  /** The command the daemon answered with, URL redacted, once it has. */
  plan: string | undefined;
  onCancel: () => void;
  onPlan: (url: string, path: string) => void;
  onAdd: (url: string, path: string) => void;
}) {
  const [url, setUrl] = useState('');
  const [path, setPath] = useState('');

  const ready = url.trim() !== '' && path.trim() !== '';

  return (
    <Dialog
      open
      onClose={onCancel}
      title="New submodule"
      description="Another repository, pinned inside this one at one commit. It is cloned now and staged; the commit that records it is yours to make."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          {plan === undefined ? (
            <Button
              variant="primary"
              loading={busy}
              disabled={!ready}
              onClick={() => onPlan(url.trim(), path.trim())}
            >
              Continue
            </Button>
          ) : (
            <Button variant="primary" loading={busy} onClick={() => onAdd(url.trim(), path.trim())}>
              Add
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field
          label="URL"
          hint="https, ssh, or a path git can read. Credentials stay with git — yagit never asks for a password."
          value={url}
          onChange={(event) => setUrl(event.target.value)}
          placeholder="https://example.com/owner/lib.git"
          autoComplete="off"
          spellCheck={false}
        />
        <Field
          label="Folder"
          hint="Where it goes inside this repository, relative to its root."
          value={path}
          onChange={(event) => setPath(event.target.value)}
          placeholder="vendor/lib"
          autoComplete="off"
          spellCheck={false}
        />

        {plan !== undefined && (
          <div className="flex flex-col gap-1.5">
            <p className="text-xs font-medium text-ink-muted">yagit will run</p>
            <GitCommand command={plan} />
          </div>
        )}
      </div>
    </Dialog>
  );
}

export function RemoveSubmoduleDialog({
  pending,
  busy,
  onCancel,
  onForce,
  onRemove,
}: {
  pending: PendingSubmoduleRemove;
  busy: boolean;
  onCancel: () => void;
  /** Re-reads the plan with `--force` on or off; the line shown is the daemon's. */
  onForce: (force: boolean) => void;
  onRemove: (pending: PendingSubmoduleRemove) => void;
}) {
  const { submodule } = pending;

  return (
    <ConfirmDialog
      open
      onCancel={onCancel}
      onConfirm={() => onRemove(pending)}
      title="Remove this submodule?"
      description={
        'Two commands, because git has no single one for it: the first takes the checkout away, ' +
        'the second takes the gitlink and its .gitmodules entry. Both leave a staged change you ' +
        'still have to commit.'
      }
      command={pending.commands}
      confirmLabel="Remove"
      destructive
      losing={
        pending.force
          ? [`the checkout at ${submodule.path}`, 'any uncommitted work inside it, tracked or not']
          : [`the checkout at ${submodule.path}`]
      }
      busy={busy}
    >
      <div className="flex flex-col gap-2">
        <label className="flex items-start gap-2 text-xs text-ink-muted">
          <input
            type="checkbox"
            className="mt-0.5"
            checked={pending.force}
            disabled={busy}
            onChange={(event) => onForce(event.target.checked)}
          />
          <span>
            Remove it even if it holds uncommitted work. Without this, git refuses and names what is
            in the way.
          </span>
        </label>
        {/* Said out loud rather than discovered: git keeps the objects on
            purpose, and somebody who thinks a removal deleted them will look
            for the disk space and not find it freed. */}
        <p className="text-2xs text-ink-subtle">
          Its history stays on disk under <code className="font-mono">.git/modules</code>. That is
          git's design, and it is what makes this reversible by hand.
        </p>
      </div>
    </ConfirmDialog>
  );
}
