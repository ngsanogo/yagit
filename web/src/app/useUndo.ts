import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { UndoOffer, UndoPlan } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * Undo the most recent action yagit knows how to reverse: a tip reflog entry
 * (commit, amend, checkout, reset), or a branch this session deleted.
 */

export function undoQuery(repositoryId: string) {
  return {
    queryKey: ['undo', repositoryId] as const,
    queryFn: () => api.undoStatus(repositoryId),
  };
}

export function useUndo(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  // No interval of its own. The event stream invalidates ['undo', id] when the
  // repository moves, and settle marks it stale when an operation here moved
  // HEAD — a two-second poll would be `git reflog show` plus `git rev-parse`
  // spawned forever, per open repository, for an answer nothing had changed.
  const status = useQuery(undoQuery(repositoryId));

  const settleUndo = (refs: Awaited<ReturnType<typeof api.undo>> | 'stale') =>
    settle(queryClient, repositoryId, {
      ...(refs === 'stale' ? { refs: 'stale' as const } : { refs }),
      headMoved: true,
    });

  const plan = useMutation({
    mutationFn: () => api.planUndo(repositoryId),
  });

  const run = useMutation({
    mutationFn: (pending: UndoPlan) => api.undo(repositoryId, pending),
    onSuccess: async (refs, pending) => {
      await settleUndo(refs);
      toast.push({ tone: 'success', title: undoReport(pending) });
    },
    onError: (error: Error, pending) => {
      void settleUndo('stale');
      toast.push({
        tone: 'danger',
        title: `Could not undo “${pending.subject}”`,
        detail: errorDescription(error),
      });
    },
  });

  return { status, plan, run };
}

function undoReport(plan: UndoPlan): string {
  switch (plan.kind) {
    case 'amend':
      return `Undid amend of “${plan.subject}”`;
    case 'checkout':
      return plan.detach ? `Detached at ${plan.subject}` : `Now on ${plan.subject}`;
    case 'commit':
      return `Undid “${plan.subject}”`;
    case 'reset':
      return `Undid reset — back at “${plan.subject}”`;
    case 'branch-delete':
      return `Restored ${plan.branch}`;
    default:
      return `Ran ${plan.command}`;
  }
}

/**
 * What the button says. Takes the offer rather than the kind alone, because a
 * restored branch is the one action whose label names something the repository
 * does not have — "Restore side", where every other kind is about where HEAD
 * has just been.
 */
export function undoButtonLabel(offer: Pick<UndoOffer, 'kind' | 'branch'>): string {
  switch (offer.kind) {
    case 'amend':
      return 'Undo amend';
    case 'checkout':
      return 'Undo checkout';
    case 'commit':
      return 'Undo last commit';
    case 'reset':
      return 'Undo reset';
    case 'branch-delete':
      return `Restore ${offer.branch}`;
    default:
      return 'Undo';
  }
}

export function undoConfirmLabel(kind: UndoPlan['kind']): string {
  switch (kind) {
    case 'amend':
      return 'Undo amend';
    case 'checkout':
      return 'Undo checkout';
    case 'commit':
      return 'Undo commit';
    case 'reset':
      return 'Undo reset';
    case 'branch-delete':
      return 'Restore branch';
    default:
      return 'Undo';
  }
}
