import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { RefsPayload } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * Making, unmaking and sending tags.
 *
 * Beside useBranches for create/delete. Push is here rather than in useRemote:
 * a tag has no upstream, so the destination is a remote the dialog chooses and
 * a refspec of its own — not the branch push path.
 */

export interface CreateTagRequest {
  name: string;
  message: string;
  /** Any revision git accepts. Empty means HEAD. */
  target: string;
  /** False for a lightweight tag. Defaults to annotated. */
  annotated?: boolean;
}

export function useTags(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const settleTags = (refs: RefsPayload | 'stale') => settle(queryClient, repositoryId, { refs });

  const create = useMutation({
    mutationFn: ({ name, message, target, annotated = true }: CreateTagRequest) =>
      api.createTag(repositoryId, name, message, target, annotated),
    onSuccess: async (refs, { name }) => {
      await settleTags(refs);
      toast.push({ tone: 'success', title: `Tagged ${name}` });
    },
    onError: (error: Error, { name }) => {
      toast.push({
        tone: 'danger',
        title: `Could not create ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  const planDelete = useMutation({
    mutationFn: (name: string) => api.planDeleteTag(repositoryId, name),
  });

  const remove = useMutation({
    mutationFn: (name: string) => api.deleteTag(repositoryId, name),
    onSuccess: async (refs, name) => {
      await settleTags(refs);
      toast.push({ tone: 'success', title: `Deleted tag ${name}` });
    },
    onError: (error: Error, name) => {
      toast.push({
        tone: 'danger',
        title: `Could not delete ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  const planPush = useMutation({
    mutationFn: ({ name, remote }: { name: string; remote: string }) =>
      api.planPushTag(repositoryId, name, remote),
  });

  const push = useMutation({
    mutationFn: ({ name, remote }: { name: string; remote: string }) =>
      api.pushTag(repositoryId, name, remote),
    onSuccess: async (refs, { name, remote }) => {
      await settleTags(refs);
      toast.push({ tone: 'success', title: `Pushed ${name} to ${remote}` });
    },
    onError: (error: Error, { name }) => {
      toast.push({
        tone: 'danger',
        title: `Could not push ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  return { create, planDelete, remove, planPush, push };
}
