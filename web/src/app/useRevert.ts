import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { RefsPayload, RevertPlan } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { settle } from './settle';

/**
 * Reverting a selected commit on the branch HEAD is on.
 *
 * Beside useCherryPick rather than inside it: the two start from the same
 * panel and land on the same banner when they conflict, but they are two
 * questions with two commands, and collapsing them would make each one's
 * toast and refusal sentences a special case of the other.
 */

export function useRevert(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const settleRevert = (refs: RefsPayload | 'stale') =>
    settle(queryClient, repositoryId, { refs, headMoved: true });

  /**
   * What reverting would do — asked before the confirmation opens.
   *
   * A mutation and not a query, for the reason planCherryPick is one: it is a
   * question with a body and no cache.
   */
  const plan = useMutation({
    mutationFn: (commit: string) => api.planRevert(repositoryId, commit),
  });

  const run = useMutation({
    mutationFn: (pending: RevertPlan) => api.revert(repositoryId, pending),
    onSuccess: async (refs, pending) => {
      await settleRevert(refs);
      toast.push({ tone: 'success', title: revertReport(pending) });
    },
    onError: (error: Error, pending) => {
      // A conflict arrives here, and git has already written the markers. The
      // banner above the history says what the repository is in the middle of —
      // now rather than at the next poll.
      void settleRevert('stale');
      toast.push({
        tone: 'danger',
        title: `Could not revert ${shortenSha(pending.commit)}`,
        detail: errorDescription(error),
      });
    },
  });

  return { plan, run };
}

function revertReport(plan: RevertPlan): string {
  switch (plan.outcome) {
    case 'revert':
      return `Reverted ${shortenSha(plan.commit)} on ${plan.into}`;
    default:
      // Every revert plan carries a command — there is no revert that succeeds
      // as a no-op the way cherry-pick's up-to-date does — so an outcome this
      // build does not know still has an exact line to name.
      return `Ran ${plan.command}`;
  }
}
