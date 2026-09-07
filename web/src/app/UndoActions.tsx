import { useState } from 'react';

import type { UndoPlan } from '../api/types';
import { Button } from '../components/Button';
import { ConfirmDialog } from '../components/ConfirmDialog';
import { errorDescription } from '../lib/errorDisplay';
import { useToast } from '../components/ToastHost';
import { undoButtonLabel, undoConfirmLabel, useUndo } from './useUndo';

/**
 * Undo of the tip reflog entry, offered beside the remote actions.
 *
 * Lives above History/Changes because checkout undo is most useful right after
 * switching from the sidebar — which leaves you on History, not Changes.
 */

export function UndoActions({ repositoryId }: { repositoryId: string }) {
  const undo = useUndo(repositoryId);
  const toast = useToast();
  const [pending, setPending] = useState<UndoPlan>();

  const offer = undo.status.data?.available === true ? undo.status.data.offer : undefined;
  if (offer === undefined && pending === undefined) {
    return null;
  }

  function propose() {
    undo.plan.mutate(undefined, {
      onSuccess: (plan) => setPending(plan),
      onError: (error: Error) => {
        toast.push({
          tone: 'danger',
          title: 'Could not read what undoing would run',
          detail: errorDescription(error),
        });
      },
    });
  }

  return (
    <>
      {offer !== undefined && (
        <Button
          size="sm"
          variant="ghost"
          disabled={undo.plan.isPending || undo.run.isPending}
          onClick={propose}
          title={offer.subject}
        >
          {undoButtonLabel(offer)}
        </Button>
      )}

      <UndoDialog
        plan={pending}
        busy={undo.run.isPending}
        onCancel={() => setPending(undefined)}
        onConfirm={(plan) => {
          setPending(undefined);
          undo.run.mutate(plan);
        }}
      />
    </>
  );
}

function UndoDialog({
  plan,
  busy,
  onCancel,
  onConfirm,
}: {
  plan: UndoPlan | undefined;
  busy: boolean;
  onCancel: () => void;
  onConfirm: (plan: UndoPlan) => void;
}) {
  if (plan === undefined) {
    return null;
  }

  return (
    <ConfirmDialog
      open
      onCancel={onCancel}
      onConfirm={() => onConfirm(plan)}
      title={undoTitle(plan)}
      description={undoDescription(plan)}
      command={plan.command}
      confirmLabel={undoConfirmLabel(plan.kind)}
      busy={busy}
    />
  );
}

function undoTitle(plan: UndoPlan): string {
  switch (plan.kind) {
    case 'amend':
      return 'Undo this amend?';
    case 'checkout':
      return 'Undo this checkout?';
    case 'commit':
      return 'Undo this commit?';
    case 'reset':
      return 'Undo this reset?';
    case 'branch-delete':
      return 'Put this branch back?';
    default:
      return 'Undo?';
  }
}

function undoDescription(plan: UndoPlan): string {
  switch (plan.kind) {
    case 'amend':
      return `“${plan.subject}” leaves ${plan.into || 'HEAD'}. The amended tree stays staged.`;
    case 'checkout':
      return plan.detach
        ? `Return to ${plan.subject}, leaving HEAD detached.`
        : `Return to ${plan.subject}.`;
    case 'commit':
      return `“${plan.subject}” leaves ${plan.into || 'HEAD'}. Its changes stay staged.`;
    case 'reset':
      // Named rather than implied: a soft reset back is the only reverse that
      // cannot destroy anything, and the price is that a hard reset's
      // discarded files are not among what comes back.
      return (
        `${plan.into || 'HEAD'} returns to “${plan.subject}”. ` +
        'Your files and what is staged stay exactly as they are — if the reset ' +
        'was hard, what it discarded does not come back.'
      );
    case 'branch-delete':
      return (
        `${plan.branch} comes back pointing at “${plan.subject}”. ` +
        'Nothing else moves — you stay where you are.'
      );
    default:
      return '';
  }
}
