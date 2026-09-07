import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { InteractiveRebaseResult, RebaseStep, RefsPayload } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { planReport } from './interactiveRebase';
import { settle } from './settle';

/**
 * Rewriting the commits after a selected one, from a plan.
 *
 * Beside useReset rather than inside useBranches, for the reason useRevert
 * sits beside useCherryPick: it starts from the same panel and lands on the
 * same banner when it stops, and it is a different question with a different
 * command.
 *
 * The one thing this does that no other operation here does is report a
 * SUCCESS that is not an ending. A plan holding a `Stop to amend` leaves git
 * waiting in the middle of it, having exited zero, and the daemon says so in
 * the answer — so the toast has two forms and the caller does not get to
 * choose between them.
 */

export function useInteractiveRebase(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  // headMoved, because every plan worth running moves it: the branch ends up
  // pointing at commits that did not exist a moment ago, so the work tree
  // under it is another commit's.
  const settleRebase = (refs: RefsPayload | 'stale') =>
    settle(queryClient, repositoryId, { refs, headMoved: true });

  /**
   * The commits a plan may cover — asked before the dialog opens.
   *
   * A mutation and not a query, for the reason planRevert is one: it is a
   * question with a body and no cache. It is also where every refusal about
   * the RANGE arrives — a merge inside it, nothing after the commit at all —
   * so the dialog never opens over a plan that could not be written.
   */
  const plan = useMutation({
    mutationFn: (commit: string) => api.planInteractiveRebase(repositoryId, commit),
  });

  const run = useMutation({
    mutationFn: ({ base, from, steps }: { base: string; from: string; steps: RebaseStep[] }) =>
      api.interactiveRebase(repositoryId, base, from, steps),
    onSuccess: async (result: InteractiveRebaseResult, pending) => {
      await settleRebase(result);
      toast.push({
        // A stop is not a failure — it is the plan doing what it said — but it
        // is not a finished job either, and a success toast over a repository
        // waiting for an amend would send the user away from the one screen
        // that can finish it.
        tone: result.stopped === true ? 'info' : 'success',
        title: planReport(
          pending.steps,
          pending.from,
          result.stopped === true,
          result.step,
          result.total,
        ),
      });
    },
    onError: (error: Error, pending) => {
      // A conflict arrives here, and git has already written the markers into
      // the work tree. The banner above the history says what the repository
      // is in the middle of — now rather than at the next poll.
      void settleRebase('stale');
      toast.push({
        tone: 'danger',
        title: `Could not rewrite the commits after ${shortenSha(pending.base)}`,
        detail: errorDescription(error),
      });
    },
  });

  return { plan, run };
}
