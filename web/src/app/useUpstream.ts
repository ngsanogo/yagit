import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { RefsPayload } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * Recording and forgetting what a local branch follows.
 *
 * Beside useRemote: that hook moves commits against an upstream; this one
 * configures which upstream the counts and the push button read. Both settle
 * the references and the tracking status, because either changes what the bar
 * draws.
 */

export function useUpstream(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const settleUpstream = (refs: RefsPayload | 'stale') =>
    settle(queryClient, repositoryId, { refs, tracking: true });

  const planSet = useMutation({
    mutationFn: ({
      branch,
      remote,
      upstream,
    }: {
      branch: string;
      remote: string;
      upstream: string;
    }) => api.planSetUpstream(repositoryId, branch, remote, upstream),
  });

  const set = useMutation({
    mutationFn: ({
      branch,
      remote,
      upstream,
    }: {
      branch: string;
      remote: string;
      upstream: string;
    }) => api.setUpstream(repositoryId, branch, remote, upstream),
    onSuccess: async (refs, { branch, remote, upstream }) => {
      await settleUpstream(refs);
      toast.push({ tone: 'success', title: `${branch} now follows ${remote}/${upstream}` });
    },
    onError: (error: Error, { branch }) => {
      toast.push({
        tone: 'danger',
        title: `Could not set upstream for ${branch}`,
        detail: errorDescription(error),
      });
    },
  });

  const planUnset = useMutation({
    mutationFn: (branch: string) => api.planUnsetUpstream(repositoryId, branch),
  });

  const unset = useMutation({
    mutationFn: (branch: string) => api.unsetUpstream(repositoryId, branch),
    onSuccess: async (refs, branch) => {
      await settleUpstream(refs);
      toast.push({ tone: 'success', title: `${branch} follows nothing now` });
    },
    onError: (error: Error, branch) => {
      toast.push({
        tone: 'danger',
        title: `Could not unset upstream for ${branch}`,
        detail: errorDescription(error),
      });
    },
  });

  return { planSet, set, planUnset, unset };
}
