import type { ReflogRestorePlan, ResetMode } from '../api/types';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { SegmentedControl } from '../components/SegmentedControl';
import { namedCommit, pluralize } from '../lib/format';
import { resetLosses } from './reset';

const RESET_MODES: { value: ResetMode; label: string }[] = [
  { value: 'soft', label: 'Soft' },
  { value: 'mixed', label: 'Mixed' },
  { value: 'hard', label: 'Hard' },
];

const MODE_EFFECTS: Record<ResetMode, string> = {
  soft: 'Soft keeps the index and the work tree as they are.',
  mixed: 'Mixed resets the index; the work tree stays.',
  hard: 'Hard resets the index and the work tree.',
};

/**
 * Confirm restoring HEAD to a reflog entry, with soft / mixed / hard chosen
 * the way reset asks for them.
 *
 * A restore IS a reset — the command is `git reset` — so what it discards is
 * named by reset's own list, from the same two counts the daemon reads for
 * one: the commits on HEAD the entry does not hold, and the tracked files a
 * hard restore puts back. Destructive only when that list has something in
 * it, for the reason ResetDialog gives.
 */
export function ReflogRestoreDialog({
  plan,
  busy,
  planning = false,
  onCancel,
  onConfirm,
  onModeChange,
}: {
  plan: ReflogRestorePlan;
  busy: boolean;
  planning?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onModeChange: (mode: ResetMode) => void;
}) {
  const locked = busy || planning;
  const where = plan.into === '' ? 'HEAD' : plan.into;
  const losses = restoreLosses(plan, where);
  const modePicker = (
    <SegmentedControl
      label="Restore mode"
      segments={RESET_MODES}
      value={plan.mode}
      onChange={onModeChange}
      disabled={locked}
    />
  );

  const shared = {
    open: true,
    busy: locked,
    onCancel,
    onConfirm,
    title: `Restore ${where} to ${plan.selector}?`,
    description: `Moves ${where} to ${namedCommit(plan.sha, plan.subject)}, where HEAD was at ${plan.selector}. ${MODE_EFFECTS[plan.mode]}`,
    command: plan.command,
    confirmLabel: `Restore --${plan.mode}`,
  };

  if (losses !== undefined) {
    return (
      <ConfirmDialog {...shared} destructive losing={losses}>
        {modePicker}
      </ConfirmDialog>
    );
  }

  return <ConfirmDialog {...shared}>{modePicker}</ConfirmDialog>;
}

/**
 * Reset's own list, plus the one loss a restore meets and a reset almost never
 * does: untracked files the entry tracks, which a hard restore writes over.
 */
function restoreLosses(
  plan: ReflogRestorePlan,
  where: string,
): readonly [string, ...string[]] | undefined {
  const items = [...(resetLosses({ ...plan, commit: plan.sha, into: where }) ?? [])];
  if (plan.overwritten_untracked > 0) {
    items.push(
      `${pluralize(plan.overwritten_untracked, 'untracked file')} the entry tracks, written over`,
    );
  }
  const [first, ...rest] = items;
  return first === undefined ? undefined : [first, ...rest];
}
