import type { ReflogRestorePlan, ResetMode } from '../api/types';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { SegmentedControl } from '../components/SegmentedControl';
import { namedCommit, shortenSha } from '../lib/format';

const RESET_MODES: { value: ResetMode; label: string }[] = [
  { value: 'soft', label: 'Soft' },
  { value: 'mixed', label: 'Mixed' },
  { value: 'hard', label: 'Hard' },
];

/**
 * Confirm restoring HEAD to a reflog entry, with soft / mixed / hard chosen
 * the way reset asks for them.
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
    description: `Move ${where} to ${namedCommit(plan.sha, plan.subject)}. Soft keeps the index and work tree; mixed resets the index; hard resets both.`,
    command: plan.command,
    confirmLabel: `Restore --${plan.mode}`,
  };

  if (plan.mode === 'hard') {
    return (
      <ConfirmDialog
        {...shared}
        destructive
        losing={[
          'Any uncommitted changes in tracked files will be discarded.',
          `Commits after ${shortenSha(plan.sha)} leave the tip unless another ref still names them.`,
        ]}
      >
        {modePicker}
      </ConfirmDialog>
    );
  }

  return <ConfirmDialog {...shared}>{modePicker}</ConfirmDialog>;
}
