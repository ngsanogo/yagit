import { useState } from 'react';

import type { StashApplyMode, StashApplyPlan, StashDropPlan, StashPushPlan } from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { Dialog } from '../components/Dialog';
import { Field } from '../components/Field';
import { SegmentedControl, type Segment } from '../components/SegmentedControl';
import { counted } from '../lib/format';
import {
  stashApplySummary,
  stashDropLosses,
  stashDropSummary,
  stashLabel,
  stashPushSaves,
  stashPushSummary,
} from './stash';

/**
 * The three stash questions: one form and two confirmations.
 *
 * Which of the two shapes each takes follows the rule the rest of this
 * interface already draws, rather than a preference. A confirmation showing
 * the exact command is what an operation gets when it can take something away
 * — see ConfirmDialog, whose destructive arm will not compile without a list
 * of what disappears. Making a stash takes nothing away: the work is saved,
 * and getting it back is the entire feature. So it is a form, like creating a
 * branch, and what it shows instead is what would be SAVED.
 *
 * Putting one back does write the work tree and can end in conflict markers,
 * so it shows its line. Dropping one is the only destructive thing here, and
 * it is the only one drawn in red.
 */

/**
 * Making a stash.
 *
 * Two fields, and the second is the one that matters. `git stash push` leaves
 * every untracked file exactly where it is, so a dialog for a work tree of one
 * changed file and two new ones has to say which of the three are going —
 * which is why the box re-asks the daemon rather than only setting a flag: the
 * counts on screen, and whether there is anything to save at all, both change
 * with it.
 *
 * The message is not part of that round trip. It is typed, and re-planning per
 * keystroke to keep a command current would be a request per keystroke — the
 * reason this dialog shows no command at all. What ran is in the log panel a
 * moment later, message and flags exactly as they went to git.
 */
export function StashPushDialog({
  plan,
  busy,
  planning = false,
  onCancel,
  onStash,
  onUntrackedChange,
}: {
  plan: StashPushPlan;
  busy: boolean;
  /** True while a change to the box is re-reading the plan. */
  planning?: boolean;
  onCancel: () => void;
  onStash: (message: string, untracked: boolean) => void;
  onUntrackedChange: (untracked: boolean) => void;
}) {
  const [message, setMessage] = useState('');

  // Locked while the daemon is answering and while the stash itself runs: the
  // box is a question with a round trip behind it, and the counts beside it
  // have to be the ones the button will act on.
  const locked = busy || planning;
  const trimmed = message.trim();

  // A work tree holding nothing but untracked files, with the box unticked, is
  // a real state for this dialog to be in — it is the state the box exists to
  // change. The daemon refuses the command; this stops the click before it,
  // and the sentence above says why.
  const saving = stashPushSaves(plan) > 0;

  return (
    <Dialog
      open
      onClose={onCancel}
      title="Stash changes"
      description={stashPushSummary(plan)}
      footer={
        <>
          <Button variant="ghost" disabled={locked} onClick={onCancel}>
            Cancel
          </Button>
          <Button
            variant="primary"
            loading={locked}
            disabled={!saving}
            onClick={() => onStash(trimmed, plan.include_untracked)}
          >
            Stash
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-4">
        {/* Optional, and it says so. git writes one either way — "WIP on main:
            5956208 feat(…)" — so an empty field is a real answer rather than a
            thing left undone. */}
        <Field
          label="Message"
          value={message}
          onChange={(event) => setMessage(event.target.value)}
          placeholder="what you were in the middle of"
          hint="Optional. Without one, git writes “WIP on <branch>” and the commit it was sitting on."
          disabled={locked}
          autoFocus
        />

        <label className="flex items-start gap-2 text-xs text-ink-muted">
          <input
            type="checkbox"
            checked={plan.include_untracked}
            disabled={locked}
            onChange={(event) => onUntrackedChange(event.target.checked)}
            className="mt-0.5 accent-accent"
          />
          <span>
            Include untracked files
            <span className="block text-ink-subtle">
              {plan.untracked === 0
                ? 'There are none here.'
                : `${counted(plan.untracked, 'file', 'git does not track yet. Without this it stays', 'git does not track yet. Without this they stay')} in the work tree.`}
            </span>
          </span>
        </label>
      </div>
    </Dialog>
  );
}

/**
 * The two ways a stash comes back, drawn as the choice they are.
 *
 * git spells them as two subcommands rather than as a flag, and they are two
 * different promises: `apply` leaves the entry in the stack so the same work
 * can go onto another branch, `pop` takes it off. One segment each, so the
 * command on screen is always the one the button will run.
 */
const APPLY_MODES: readonly Segment<StashApplyMode>[] = [
  { value: 'pop', label: 'Pop' },
  { value: 'apply', label: 'Apply' },
];

/**
 * Putting a stash back.
 *
 * Not destructive — nothing is lost either way, and a pop that fails keeps the
 * entry — so it carries no loss list and no red button. It carries the command
 * because the command is the one thing that says which of the two this is, and
 * a sentence because the command does not say what is already in the way.
 */
export function StashApplyDialog({
  plan,
  busy,
  planning = false,
  onCancel,
  onConfirm,
  onModeChange,
}: {
  plan: StashApplyPlan;
  busy: boolean;
  /** True while a mode change is re-reading the plan. */
  planning?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onModeChange: (mode: StashApplyMode) => void;
}) {
  const locked = busy || planning;

  return (
    <ConfirmDialog
      open
      busy={locked}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Put ${stashLabel(plan)} back?`}
      description={stashApplySummary(plan)}
      command={plan.command}
      confirmLabel={plan.mode === 'pop' ? 'Pop' : 'Apply'}
    >
      <SegmentedControl
        label="What happens to the stash"
        segments={APPLY_MODES}
        value={plan.mode}
        onChange={onModeChange}
        disabled={locked}
      />
    </ConfirmDialog>
  );
}

/**
 * Throwing a stash away.
 *
 * The only destructive dialog in this family, and the loss list is precise on
 * purpose: the commit is not deleted, it becomes unreachable, and it stays
 * readable until git collects it. Saying "gone" would be the kind of warning
 * people learn to click through — and the object name in the list is the one
 * thing that can still get the work back.
 */
export function StashDropDialog({
  plan,
  busy,
  onCancel,
  onConfirm,
}: {
  plan: StashDropPlan;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <ConfirmDialog
      open
      destructive
      busy={busy}
      onCancel={onCancel}
      onConfirm={onConfirm}
      title={`Drop ${stashLabel(plan)}?`}
      description={stashDropSummary(plan)}
      command={plan.command}
      losing={stashDropLosses(plan)}
      confirmLabel="Drop"
    />
  );
}
