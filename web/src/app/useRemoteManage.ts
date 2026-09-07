import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { Remote } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * Adding, renaming and removing remotes.
 *
 * Beside useRemote: that hook talks to remotes that already exist; this one
 * edits the list itself. Rename and remove move refs under refs/remotes/, so
 * the held reference list is marked stale as well as the remotes query.
 */

async function settleRemotes(
  queryClient: ReturnType<typeof useQueryClient>,
  repositoryId: string,
  remotes: Remote[],
  refsMoved: boolean,
) {
  queryClient.setQueryData(['remotes', repositoryId], remotes);
  if (refsMoved) {
    await settle(queryClient, repositoryId, { refs: 'stale' });
  }
}

export function useRemoteManage(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const add = useMutation({
    mutationFn: ({ name, url }: { name: string; url: string }) =>
      api.addRemote(repositoryId, name, url),
    onSuccess: async (remotes, { name }) => {
      await settleRemotes(queryClient, repositoryId, remotes, false);
      toast.push({ tone: 'success', title: `Added remote ${name}` });
    },
    onError: (error: Error, { name }) => {
      toast.push({
        tone: 'danger',
        title: `Could not add ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  const rename = useMutation({
    mutationFn: ({ from, to }: { from: string; to: string }) =>
      api.renameRemote(repositoryId, from, to),
    onSuccess: async (remotes, { from, to }) => {
      await settleRemotes(queryClient, repositoryId, remotes, true);
      toast.push({ tone: 'success', title: `Renamed ${from} to ${to}` });
    },
    onError: (error: Error, { from }) => {
      toast.push({
        tone: 'danger',
        title: `Could not rename ${from}`,
        detail: errorDescription(error),
      });
    },
  });

  const planRemove = useMutation({
    mutationFn: (name: string) => api.planRemoveRemote(repositoryId, name),
  });

  const remove = useMutation({
    mutationFn: (name: string) => api.removeRemote(repositoryId, name),
    onSuccess: async (remotes, name) => {
      await settleRemotes(queryClient, repositoryId, remotes, true);
      toast.push({ tone: 'success', title: `Removed remote ${name}` });
    },
    onError: (error: Error, name) => {
      toast.push({
        tone: 'danger',
        title: `Could not remove ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  const planSetUrl = useMutation({
    mutationFn: ({ name, url }: { name: string; url: string }) =>
      api.planSetRemoteURL(repositoryId, name, url),
  });

  const setUrl = useMutation({
    mutationFn: ({ name, url }: { name: string; url: string }) =>
      api.setRemoteURL(repositoryId, name, url),
    onSuccess: async (remotes, { name }) => {
      await settleRemotes(queryClient, repositoryId, remotes, false);
      toast.push({ tone: 'success', title: `Updated URL for ${name}` });
    },
    onError: (error: Error, { name }) => {
      toast.push({
        tone: 'danger',
        title: `Could not update URL for ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  return { add, rename, planSetUrl, setUrl, planRemove, remove };
}
