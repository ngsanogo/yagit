import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { LFSSupport } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';

/**
 * Git LFS: what this machine and this repository can do about it, and the two
 * operations that change the second.
 *
 * A hook rather than a query inside the panel, like the submodules, the stash
 * and the worktrees beside it. The reason is not symmetry: the answer decides
 * something OUTSIDE the panel. A repository tracking nothing is not given a
 * panel at all, and the entry point for its first pattern therefore cannot
 * live in one — so the workbench asks this too, and offers the action from
 * RepositoryAdditions, the row under the panels that is always drawn.
 *
 * Neither operation moves a byte of content. `git lfs track` writes
 * .gitattributes and stops; the transfer is a filter git runs during a fetch
 * or a checkout yagit already drives.
 */

export function lfsQuery(repositoryId: string, enabled: boolean) {
  return {
    queryKey: ['lfs', repositoryId] as const,
    queryFn: () => api.lfs(repositoryId),
    // Not asked of a bare repository. The route answers 409 for one — every
    // LFS operation writes .gitattributes into a work tree, and a bare
    // repository has none — so asking would be a request whose only possible
    // answer is a refusal nothing on screen is waiting for. Same reason the
    // stash list takes this flag.
    enabled,
  };
}

export type LFSAction = 'track' | 'untrack';

export function useLFS(repositoryId: string, enabled: boolean) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const support = useQuery(lfsQuery(repositoryId, enabled));

  // No onError of its own, unlike run below. Every caller of this reaches it
  // through the dialog slot's `propose`, which reports a failed plan naming
  // what was asked — "what tracking *.psd would run" — and a second toast
  // saying "that" would be the same news twice, in vaguer words.
  const plan = useMutation({
    mutationFn: ({ action, pattern }: { action: LFSAction; pattern: string }) =>
      api.planLFS(repositoryId, action, pattern).then((planned) => planned.command),
  });

  const run = useMutation({
    mutationFn: ({ action, pattern }: { action: LFSAction; pattern: string }) =>
      api.trackLFS(repositoryId, action, pattern),
    onSuccess: (next: LFSSupport, { action, pattern }) => {
      // The answer IS the new state, so it is written rather than refetched:
      // the route reads .gitattributes after writing it.
      queryClient.setQueryData(lfsQuery(repositoryId, enabled).queryKey, next);

      // .gitattributes was rewritten in the work tree, so the status the
      // changes view is holding is one file out of date — and so is its diff,
      // which is the pane somebody watching .gitattributes is actually looking
      // at. The two go together everywhere else in this application; see the
      // workTree case in settle.
      void queryClient.invalidateQueries({ queryKey: ['status', repositoryId] });
      void queryClient.invalidateQueries({ queryKey: ['diff', repositoryId] });

      toast.push({
        tone: 'success',
        title: action === 'track' ? `Tracking ${pattern}` : `No longer tracking ${pattern}`,
        detail: '.gitattributes is written, and not committed. That commit is yours to make.',
      });
    },
    // Named after what was asked for, like every other failure in the
    // application: "Could not untrack *.psd" and not "The LFS command
    // failed". The pattern is what makes it useful — a repository tracking
    // three of them fails one at a time, and a title that names none of them
    // leaves the reader to guess which — and it is already in scope, because
    // the success toast one line up is built from the same two values.
    onError: (error, { action, pattern }) => {
      toast.push({
        tone: 'danger',
        title: action === 'track' ? `Could not track ${pattern}` : `Could not untrack ${pattern}`,
        detail: errorDescription(error),
      });
    },
  });

  return { support, plan, run };
}
