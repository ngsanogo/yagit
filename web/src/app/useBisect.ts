import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { WorkingDirectory } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';

/**
 * Beginning a bisect search.
 *
 * The verdicts and the reset live on the operation banner once one is under
 * way; this is only the door into that state. Plan and run share the same
 * shape every other shown-command write uses.
 */

export interface BisectStartRequest {
  bad?: string;
  good?: string[];
}

export interface BisectStartPlan {
  command: string;
  bad: string;
  good: string[];
}

export function useBisect(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const settle = async (status: WorkingDirectory) => {
    await queryClient.cancelQueries({ queryKey: ['status', repositoryId] });
    queryClient.setQueryData(['status', repositoryId], status);
    for (const key of ['refs', 'commits', 'commit', 'diff', 'prepared-message']) {
      void queryClient.invalidateQueries({ queryKey: [key, repositoryId] });
    }
  };

  const plan = useMutation({
    mutationFn: async ({ bad, good }: BisectStartRequest): Promise<BisectStartPlan> => {
      const answer = await api.planStartBisect(repositoryId, bad, good);
      return { command: answer.command, bad: bad ?? '', good: good ?? [] };
    },
  });

  const run = useMutation({
    mutationFn: ({ bad, good }: BisectStartRequest) => api.startBisect(repositoryId, bad, good),
    onSuccess: async (status) => {
      await settle(status);
      toast.push({ tone: 'success', title: 'Started a bisect' });
    },
    onError: (error: Error) => {
      void queryClient.invalidateQueries({ queryKey: ['status', repositoryId] });
      toast.push({
        tone: 'danger',
        title: 'Could not start a bisect',
        detail: errorDescription(error),
      });
    },
  });

  return { plan, run };
}
