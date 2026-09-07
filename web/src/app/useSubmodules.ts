import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * The repositories this one pins: the list, and the four operations that
 * change it.
 *
 * Adding and removing move the superproject's own index — a gitlink is a
 * staged change like any other — so those two settle the working directory as
 * well as this list. Update and sync do not: they change what is inside the
 * submodule and the local config, neither of which the superproject stages.
 */

export function submodulesQuery(repositoryId: string) {
  return {
    queryKey: ['submodules', repositoryId] as const,
    queryFn: () => api.submodules(repositoryId),
  };
}

export function useSubmodules(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const list = useQuery(submodulesQuery(repositoryId));

  const refreshList = () =>
    queryClient.invalidateQueries({ queryKey: ['submodules', repositoryId] });

  // A gitlink is a path in the index, and adding or removing one also writes
  // .gitmodules: two staged changes the Changes panel has to hear about, and a
  // directory that appeared or went in the work tree.
  const refreshIndex = () => settle(queryClient, repositoryId, { refs: 'stale', workTree: true });

  const plan = useMutation({
    mutationFn: ({ url, path }: { url: string; path: string }) =>
      api.planAddSubmodule(repositoryId, url, path),
  });

  const add = useMutation({
    mutationFn: ({ url, path }: { url: string; path: string }) =>
      api.addSubmodule(repositoryId, url, path),
    onSuccess: async (_answer, { path }) => {
      await refreshList();
      await refreshIndex();
      toast.push({
        tone: 'success',
        title: `Added ${path}`,
        detail: 'Staged, and not yet committed.',
      });
    },
    onError: (error: Error, { path }) => {
      toast.push({
        tone: 'danger',
        title: `Could not add a submodule at ${path}`,
        detail: errorDescription(error),
      });
    },
  });

  const update = useMutation({
    mutationFn: (path: string) => api.updateSubmodules(repositoryId, path),
    onSuccess: async (answer, path) => {
      await refreshList();
      toast.push({
        tone: 'success',
        title: path === '' ? `Updated ${answer.submodules.length} submodules` : `Updated ${path}`,
      });
    },
    onError: (error: Error, path) => {
      toast.push({
        tone: 'danger',
        title: path === '' ? 'Could not update the submodules' : `Could not update ${path}`,
        detail: errorDescription(error),
      });
    },
  });

  const sync = useMutation({
    mutationFn: (path: string) => api.syncSubmodules(repositoryId, path),
    onSuccess: async () => {
      await refreshList();
      toast.push({ tone: 'success', title: 'Submodule URLs copied from .gitmodules' });
    },
    onError: (error: Error) => {
      toast.push({
        tone: 'danger',
        title: 'Could not sync the submodule URLs',
        detail: errorDescription(error),
      });
    },
  });

  const planRemove = useMutation({
    mutationFn: ({ path, force }: { path: string; force: boolean }) =>
      api.planRemoveSubmodule(repositoryId, path, force),
  });

  const remove = useMutation({
    mutationFn: ({ path, force }: { path: string; force: boolean }) =>
      api.removeSubmodule(repositoryId, path, force),
    onSuccess: async (_answer, { path }) => {
      await refreshList();
      await refreshIndex();
      toast.push({
        tone: 'success',
        title: `Removed ${path}`,
        detail: 'Staged, and not yet committed.',
      });
    },
    onError: (error: Error, { path }) => {
      toast.push({
        tone: 'danger',
        title: `Could not remove ${path}`,
        detail: errorDescription(error),
      });
    },
  });

  return { list, plan, add, update, sync, planRemove, remove };
}
