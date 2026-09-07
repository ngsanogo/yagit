import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { CherryPickPlan, RefsPayload } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { cherryPickSummary } from './cherrypick';
import { settle } from './settle';

/**
 * Cherry-picking a selected commit onto the branch HEAD is on.
 *
 * Beside useCheckOut and useBranches rather than inside either: it starts from
 * a commit in the history, not from a branch row, and its consequences are the
 * same as a merge's — HEAD's branch moved, the work tree may have conflict
 * markers, the next commit message came from somewhere else.
 */

export function useCherryPick(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const settlePick = (refs: RefsPayload | 'stale') =>
    settle(queryClient, repositoryId, { refs, headMoved: true });

  /**
   * What cherry-picking would do — asked before the confirmation opens.
   *
   * A mutation and not a query, for the reason planMerge is one: it is a
   * question with a body and no cache. Up-to-date answers here without a
   * command; the caller toasts and never opens a dialog that would have
   * nothing exact to show.
   */
  const plan = useMutation({
    mutationFn: (commit: string) => api.planCherryPick(repositoryId, commit),
  });

  const run = useMutation({
    mutationFn: (pending: CherryPickPlan) => api.cherryPick(repositoryId, pending),
    onSuccess: async (refs, pending) => {
      await settlePick(refs);
      toast.push({ tone: 'success', title: cherryPickReport(pending) });
    },
    onError: (error: Error, pending) => {
      // A conflict arrives here, and git has already written the markers. The
      // banner above the history says what the repository is in the middle of —
      // now rather than at the next poll.
      void settlePick('stale');
      toast.push({
        tone: 'danger',
        title: `Could not cherry-pick ${shortenSha(pending.commit)}`,
        detail: errorDescription(error),
      });
    },
  });

  return { plan, run };
}

/**
 * Toast when the plan itself is the whole answer: the commit is already on
 * the branch, and there is no command to confirm.
 */
export function alreadyOnBranchToast(pending: CherryPickPlan): {
  tone: 'success';
  title: string;
} {
  return { tone: 'success', title: cherryPickSummary(pending) };
}

function cherryPickReport(plan: CherryPickPlan): string {
  switch (plan.outcome) {
    case 'up-to-date':
      return `${plan.into} already had ${shortenSha(plan.commit)}`;
    case 'fast-forward':
      return `Fast-forwarded ${plan.into} to ${shortenSha(plan.commit)}`;
    case 'cherry-pick':
      return `Cherry-picked ${shortenSha(plan.commit)} onto ${plan.into}`;
    default:
      return plan.command === '' ? `Cherry-picked onto ${plan.into}` : `Ran ${plan.command}`;
  }
}
