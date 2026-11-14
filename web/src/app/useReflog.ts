import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { RefsPayload, ReflogRestorePlan, ResetMode } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { settle } from './settle';

/**
 * HEAD's reflog: the list, and restoring one entry after a plan.
 */
export function useReflog(repositoryId: string, enabled = true) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const entries = useQuery({
    queryKey: ['reflog', repositoryId],
    queryFn: () => api.reflog(repositoryId),
    enabled,
  });

  const planRestore = useMutation({
    mutationFn: ({ selector, mode }: { selector: string; mode: ResetMode }) =>
      api.planReflogRestore(repositoryId, selector, mode),
  });

  const restore = useMutation({
    mutationFn: (pending: ReflogRestorePlan) => api.reflogRestore(repositoryId, pending),
    onSuccess: async (refs: RefsPayload, pending) => {
      await settle(queryClient, repositoryId, { refs, headMoved: true });
      void queryClient.invalidateQueries({ queryKey: ['reflog', repositoryId] });
      toast.push({
        tone: 'success',
        title: `Restored HEAD to ${shortenSha(pending.sha)} (${pending.mode})`,
      });
    },
    onError: (error: Error) => {
      void settle(queryClient, repositoryId, { refs: 'stale', headMoved: true });
      toast.push({
        tone: 'danger',
        title: 'Could not restore from the reflog',
        detail: errorDescription(error),
      });
    },
  });

  return { entries, planRestore, restore };
}
