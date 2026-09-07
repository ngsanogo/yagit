import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { RefsPayload, ResetMode, ResetPlan } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { settle } from './settle';

/**
 * Resetting the branch HEAD is on to a selected commit.
 *
 * Beside useRevert rather than inside it: both start from the same panel, but
 * a reset moves the tip and a revert records a new commit, and collapsing them
 * would make each one's toast and mode choice a special case of the other.
 */

export function useReset(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const settleReset = (refs: RefsPayload | 'stale') =>
    settle(queryClient, repositoryId, { refs, headMoved: true });

  /**
   * What resetting would do — asked before the confirmation opens, and again
   * when the mode on the dialog changes, so the command on screen always
   * matches the flag that will run.
   */
  const plan = useMutation({
    mutationFn: ({ commit, mode }: { commit: string; mode: ResetMode }) =>
      api.planReset(repositoryId, commit, mode),
  });

  const run = useMutation({
    mutationFn: (pending: ResetPlan) => api.reset(repositoryId, pending),
    onSuccess: async (refs, pending) => {
      await settleReset(refs);
      toast.push({ tone: 'success', title: resetReport(pending) });
    },
    onError: (error: Error, pending) => {
      void settleReset('stale');
      toast.push({
        tone: 'danger',
        title: `Could not reset ${pending.into} to ${shortenSha(pending.commit)}`,
        detail: errorDescription(error),
      });
    },
  });

  return { plan, run };
}

function resetReport(plan: ResetPlan): string {
  switch (plan.mode) {
    case 'soft':
    case 'mixed':
    case 'hard':
      return `Reset ${plan.into} to ${shortenSha(plan.commit)} (${plan.mode})`;
    default:
      return `Ran ${plan.command}`;
  }
}
