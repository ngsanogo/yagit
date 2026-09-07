import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { WorktreeRequest } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';

/**
 * The linked checkouts a repository has: reading the list, and the three
 * operations that change it.
 *
 * No interval of its own. `git worktree` writes under the common git
 * directory, which is exactly what the watch follows, so the event stream says
 * when the list moved — including when it moved because somebody ran the
 * command in a terminal.
 */

export function worktreesQuery(repositoryId: string) {
  return {
    queryKey: ['worktrees', repositoryId] as const,
    queryFn: () => api.worktrees(repositoryId),
  };
}

export function useWorktrees(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const list = useQuery(worktreesQuery(repositoryId));

  const settle = () => queryClient.invalidateQueries({ queryKey: ['worktrees', repositoryId] });

  const plan = useMutation({
    mutationFn: (request: WorktreeRequest) => api.planAddWorktree(repositoryId, request),
  });

  const add = useMutation({
    mutationFn: (request: WorktreeRequest) => api.addWorktree(repositoryId, request),
    onSuccess: async (_answer, request) => {
      await settle();
      // A worktree is also a reference decision — `-b` made a branch — so the
      // sidebar has to hear about it too.
      await queryClient.invalidateQueries({ queryKey: ['refs', repositoryId] });
      toast.push({ tone: 'success', title: `Checked out into ${request.path}` });
    },
    onError: (error: Error, request) => {
      toast.push({
        tone: 'danger',
        title: `Could not make a worktree at ${request.path}`,
        detail: errorDescription(error),
      });
    },
  });

  const planRemove = useMutation({
    mutationFn: ({ path, force }: { path: string; force: boolean }) =>
      api.planRemoveWorktree(repositoryId, path, force),
  });

  const remove = useMutation({
    mutationFn: ({ path, force }: { path: string; force: boolean }) =>
      api.removeWorktree(repositoryId, path, force),
    onSuccess: async (_answer, { path }) => {
      await settle();
      toast.push({ tone: 'success', title: `Removed the worktree at ${path}` });
    },
    onError: (error: Error, { path }) => {
      toast.push({
        tone: 'danger',
        title: `Could not remove the worktree at ${path}`,
        detail: errorDescription(error),
      });
    },
  });

  const prune = useMutation({
    mutationFn: () => api.pruneWorktrees(repositoryId),
    onSuccess: async (answer) => {
      await settle();
      toast.push({
        tone: 'success',
        title: `${answer.worktrees.length} worktrees left after pruning`,
      });
    },
    onError: (error: Error) => {
      toast.push({
        tone: 'danger',
        title: 'Could not prune the worktrees',
        detail: errorDescription(error),
      });
    },
  });

  return { list, plan, add, planRemove, remove, prune };
}
