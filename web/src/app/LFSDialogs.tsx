import { useState } from 'react';

import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';

/**
 * Routing a path pattern through Git LFS, and taking one back out.
 *
 * Both write .gitattributes and neither commits it, which both dialogs say:
 * "tracked" reads as finished, and what has actually happened is one work-tree
 * file changed and waiting for a message.
 *
 * Tracking is a dialog and not the inline form it used to be, because the form
 * lived in a panel that a repository tracking nothing is not given — so the
 * first pattern could be added only by a repository that already had one. A
 * dialog is reachable from RepositoryAdditions whether the panel is there or
 * not, and it is the shape every other "add" in this application already has:
 * the fields, then the command the daemon answered with, then the button.
 */

export function TrackLFSDialog({
  busy,
  plan,
  onCancel,
  onPlan,
  onTrack,
}: {
  busy: boolean;
  /** The command the daemon answered with, once it has. */
  plan: string | undefined;
  onCancel: () => void;
  onPlan: (pattern: string) => void;
  onTrack: (pattern: string) => void;
}) {
  const [pattern, setPattern] = useState('');

  const ready = pattern.trim() !== '';

  return (
    <Dialog
      open
      onClose={onCancel}
      title="Track a pattern with Git LFS"
      description="Files matching it are stored outside the repository from the next commit on. What is already committed stays where it is."
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
              onClick={() => onPlan(pattern.trim())}
            >
              Continue
            </Button>
          ) : (
            <Button variant="primary" loading={busy} onClick={() => onTrack(pattern.trim())}>
              Track
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <Field
          label="Pattern"
          hint="A path pattern as .gitattributes spells one — *.psd, assets/**, one.bin."
          value={pattern}
          onChange={(event) => setPattern(event.target.value)}
          placeholder="*.psd"
          autoComplete="off"
          spellCheck={false}
        />

        <p className="text-2xs text-ink-subtle">
          .gitattributes is written and not committed. yagit never stages on your behalf.
        </p>

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

export function UntrackLFSDialog({
  pattern,
  command,
  busy,
  onCancel,
  onUntrack,
}: {
  pattern: string;
  command: string;
  busy: boolean;
  onCancel: () => void;
  onUntrack: (pattern: string) => void;
}) {
  return (
    <ConfirmDialog
      open
      title={`Stop tracking ${pattern}?`}
      description="Files already stored in LFS stay there until they are committed again: untracking decides where FUTURE content goes. .gitattributes is rewritten, and not committed."
      command={command}
      confirmLabel="Untrack"
      busy={busy}
      onCancel={onCancel}
      onConfirm={() => onUntrack(pattern)}
    />
  );
}
