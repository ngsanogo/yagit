import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type {
  Stash,
  StashApplyMode,
  StashApplyPlan,
  StashDropPlan,
  StashHandle,
} from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';
import { stashLabel, stashRef } from './stash';

/**
 * The stash stack: reading it, and the three things that change it.
 *
 * One hook for all four because they share one cache entry, and the entry is
 * the whole reason: every write answers with the list as it stands afterwards,
 * so the stack on screen is what the daemon read a moment ago rather than
 * something asked for again and arriving later.
 *
 * That matters more here than anywhere else in this interface. A stash is
 * addressed by POSITION — stash@{0} is whatever is most recent — so a push or
 * a drop renumbers everything below it. A list left stale for even one render
 * is a list whose rows send the wrong number to the daemon; the daemon refuses
 * it, correctly, but the person clicking gets a refusal for a row they were
 * looking straight at.
 */

/**
 * The stack, keyed so that everything drawing it shares one request.
 *
 * Exported because two things read it: this hook, and the panel that draws the
 * list. Two keys would be two `git stash list` calls answering the same
 * question.
 */
export function stashesQuery(repositoryId: string, enabled: boolean) {
  return {
    queryKey: ['stashes', repositoryId],
    queryFn: () => api.stashes(repositoryId),
    // No interval. A stash changes when something in this session changes it,
    // or when git's directory moves — and the event stream says so, which
    // invalidates this key by name (ADR 0007). Polling for it would be a
    // subprocess every two seconds for a list that is usually empty.
    enabled,
  };
}

export function useStash(repositoryId: string, enabled: boolean) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const stashes = useQuery(stashesQuery(repositoryId, enabled));

  /**
   * What every one of these leaves stale.
   *
   * The work tree in all three cases — a push takes files out of it, an apply
   * puts them back — and the stack itself, written from the answer where there
   * is one. Not `headMoved`: a stash moves files without moving HEAD, and
   * saying otherwise would drop the prepared message for an operation git
   * never started.
   */
  const settleStash = (list: Stash[] | 'stale') =>
    settle(queryClient, repositoryId, {
      // Stale rather than written: refs/stash is a reference, so pushing and
      // dropping change what `for-each-ref` answers, and none of these routes
      // hands back that list.
      refs: 'stale',
      workTree: true,
      stashes: list,
    });

  const planPush = useMutation({
    mutationFn: (untracked: boolean) => api.planStash(repositoryId, untracked),
  });

  const push = useMutation({
    mutationFn: (saving: { message: string; untracked: boolean }) =>
      api.stashPush(repositoryId, saving),
    onSuccess: async (list) => {
      await settleStash(list);
      const made = list[0];
      toast.push({
        tone: 'success',
        title: made === undefined ? 'Stashed the work tree' : `Stashed ${stashLabel(made)}`,
      });
    },
    onError: (error: Error) => {
      // Settled even on a failure. A push that git refused changed nothing,
      // but a push refused by the daemon's own reading — nothing to stash —
      // means the work tree is not what this page thought it was, and the
      // count on the button is drawn from exactly that.
      void settleStash('stale');
      toast.push({
        tone: 'danger',
        title: 'Could not stash the work tree',
        detail: errorDescription(error),
      });
    },
  });

  const planApply = useMutation({
    mutationFn: ({ stash, mode }: { stash: StashHandle; mode: StashApplyMode }) =>
      api.planStashApply(repositoryId, stash, mode),
  });

  const apply = useMutation({
    mutationFn: (pending: StashApplyPlan) => api.stashApply(repositoryId, pending),
    onSuccess: async (list, pending) => {
      await settleStash(list);
      toast.push({ tone: 'success', title: applyReport(pending) });
    },
    onError: (error: Error, pending) => {
      void settleStash('stale');
      toast.push({
        tone: 'danger',
        title: `Could not ${pending.mode} ${stashLabel(pending)}`,
        // git's own account, which for a conflicting apply says both which
        // file it stopped on and that the entry was kept — neither of which
        // this page could work out for itself.
        detail: errorDescription(error),
      });
    },
  });

  const planDrop = useMutation({
    mutationFn: (stash: StashHandle) => api.planStashDrop(repositoryId, stash),
  });

  const drop = useMutation({
    mutationFn: (pending: StashDropPlan) => api.stashDrop(repositoryId, pending),
    onSuccess: async (list, pending) => {
      await settleStash(list);
      toast.push({
        tone: 'success',
        title: `Dropped ${stashLabel(pending)}`,
        // The object name, because it is the one thing that can still get the
        // work back: the commit is unreachable rather than deleted, and it
        // stays readable until git collects it.
        detail: `Its commit ${pending.sha} is unreachable now, and readable until git collects it.`,
      });
    },
    onError: (error: Error, pending) => {
      void settleStash('stale');
      toast.push({
        tone: 'danger',
        title: `Could not drop ${stashRef(pending.index)}`,
        detail: errorDescription(error),
      });
    },
  });

  return { stashes, planPush, push, planApply, apply, planDrop, drop };
}

/** What a finished apply or pop is reported as. */
function applyReport(plan: StashApplyPlan): string {
  const named = stashLabel(plan);
  return plan.mode === 'pop'
    ? `Popped ${named} — it has left the stack`
    : `Applied ${named} — it is still in the stack`;
}

/** What a stash's own patch is keyed by, for the panel that draws it. */
export function stashQuery(repositoryId: string, index: number) {
  return {
    queryKey: ['stash', repositoryId, index],
    queryFn: () => api.stash(repositoryId, index),
    // Keyed by POSITION rather than by object name, deliberately: it is what
    // the route takes, and it is what the row knows. What that costs is that
    // the key means something different after the stack moves — so every
    // operation here drops it, rather than letting a cached answer be drawn
    // under a row that is now a different stash. See settle.
    staleTime: Infinity,
  };
}
