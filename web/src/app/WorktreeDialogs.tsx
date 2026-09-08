import { useState } from 'react';

import type { WorktreeRequest } from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { GitCommand } from '../components/GitCommand';
import { SegmentedControl, type Segment } from '../components/SegmentedControl';

/**
 * Making another checkout, and removing one.
 *
 * The add dialog asks for two things and offers three shapes of the same
 * command, because that is how many git has: an existing branch checked out
 * there, a new branch made there, or a detached HEAD — which is the only way a
 * tag or a commit can be checked out at all, exactly as in the sidebar.
 *
 * git refuses a branch that is already checked out somewhere else, and that
 * refusal names the directory holding it. It is not pre-empted here: the
 * sentence git writes is the one the person needs, and repeating the check
 * would be a second answer to keep in step with it.
 */

/** What the destination is checked out at. */
type Mode = 'branch' | 'new-branch' | 'detach';

const MODES: readonly Segment<Mode>[] = [
  { value: 'branch', label: 'Existing branch' },
  { value: 'new-branch', label: 'New branch' },
  { value: 'detach', label: 'Detached' },
];

const HINTS: Record<Mode, string> = {
  branch: 'A branch that exists and is not checked out anywhere else.',
  'new-branch': 'The branch is made in the new checkout, starting from the reference below.',
  detach: 'A tag or a commit. HEAD sits on it with no branch, as it does in the sidebar.',
};

export interface PendingWorktreeRemove {
  path: string;
  force: boolean;
  command: string;
}

export function AddWorktreeDialog({
  busy,
  rootHint,
  plan,
  onCancel,
  onPlan,
  onCreate,
}: {
  busy: boolean;
  /** The allowed root, for the placeholder. Undefined until a discover answers. */
  rootHint: string | undefined;
  /** The command the daemon answered with, once it has. */
  plan: { command: string; path: string } | undefined;
  onCancel: () => void;
  onPlan: (request: WorktreeRequest) => void;
  onCreate: (request: WorktreeRequest) => void;
}) {
  const [mode, setMode] = useState<Mode>('branch');
  const [path, setPath] = useState('');
  const [ref, setRef] = useState('');
  const [newBranch, setNewBranch] = useState('');

  const request: WorktreeRequest = {
    path: path.trim(),
    ref: ref.trim(),
    new_branch: mode === 'new-branch' ? newBranch.trim() : '',
    detach: mode === 'detach',
  };

  const incomplete =
    request.path === '' ||
    (mode === 'branch' && request.ref === '') ||
    (mode === 'new-branch' && request.new_branch === '') ||
    (mode === 'detach' && request.ref === '');

  return (
    <Dialog
      open
      onClose={onCancel}
      title="New worktree"
      description="Another checkout of this repository, in a directory of its own. One branch can be checked out in one worktree at a time."
      footer={
        <>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
          {plan === undefined ? (
            <Button
              variant="primary"
              loading={busy}
              disabled={incomplete}
              onClick={() => onPlan(request)}
            >
              Continue
            </Button>
          ) : (
            <Button variant="primary" loading={busy} onClick={() => onCreate(request)}>
              Create
            </Button>
          )}
        </>
      }
    >
      <div className="flex flex-col gap-3">
        <SegmentedControl
          label="What to check out there"
          segments={MODES}
          value={mode}
          onChange={setMode}
        />

        <Field
          label="Folder"
          hint="Absolute path inside the allowed root. The parent must exist; the folder itself must not."
          value={path}
          onChange={(event) => setPath(event.target.value)}
          placeholder={rootHint === undefined ? '/home/you/repo-side' : `${rootHint}/repo-side`}
          autoComplete="off"
          spellCheck={false}
        />

        {mode === 'new-branch' && (
          <Field
            label="New branch"
            hint={HINTS['new-branch']}
            value={newBranch}
            onChange={(event) => setNewBranch(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        )}

        <Field
          label={mode === 'new-branch' ? 'Starting from' : 'Reference'}
          hint={
            mode === 'new-branch' ? 'Empty means HEAD, which is git’s own default.' : HINTS[mode]
          }
          value={ref}
          onChange={(event) => setRef(event.target.value)}
          placeholder={mode === 'detach' ? 'v1.0, or a commit' : 'side'}
          autoComplete="off"
          spellCheck={false}
        />

        {plan !== undefined && (
          <div className="flex flex-col gap-1.5">
            <p className="text-xs font-medium text-ink-muted">yagit will run</p>
            <GitCommand command={plan.command} />
          </div>
        )}
      </div>
    </Dialog>
  );
}

export function RemoveWorktreeDialog({
  pending,
  busy,
  onCancel,
  onForce,
  onRemove,
}: {
  pending: PendingWorktreeRemove;
  busy: boolean;
  onCancel: () => void;
  /**
   * Re-reads the plan with `--force` on or off.
   *
   * A callback rather than local state, for the reason the stash-push tick box
   * takes one: the line on screen is the daemon's, and a box that only set a
   * flag would leave the command saying one thing while the button did
   * another.
   */
  onForce: (force: boolean) => void;
  onRemove: (pending: PendingWorktreeRemove) => void;
}) {
  return (
    <ConfirmDialog
      open
      onCancel={onCancel}
      onConfirm={() => onRemove(pending)}
      title="Remove this worktree?"
      description={
        'The branch it has checked out stays where it is, and so does every commit on it. ' +
        'What goes is the directory.'
      }
      command={pending.command}
      confirmLabel="Remove"
      destructive
      // Named precisely, which for this command means naming what --force is
      // for: without it git refuses a checkout holding uncommitted work, and
      // with it that work is what disappears.
      losing={
        pending.force
          ? [`the directory ${pending.path}`, 'any uncommitted work in it, tracked or not']
          : [`the directory ${pending.path}`]
      }
      busy={busy}
    >
      <label className="flex items-start gap-2 text-xs text-ink-muted">
        <input
          type="checkbox"
          className="mt-0.5"
          checked={pending.force}
          disabled={busy}
          onChange={(event) => onForce(event.target.checked)}
        />
        <span>
          Remove it even if it holds uncommitted work. Without this, git refuses and names the files
          in the way.
        </span>
      </label>
    </ConfirmDialog>
  );
}
