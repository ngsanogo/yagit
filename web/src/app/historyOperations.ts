import { useQuery } from '@tanstack/react-query';

import { useBranches } from './useBranches';
import { useCheckOut } from './useCheckOut';
import { useCherryPick } from './useCherryPick';
import { useInteractiveRebase } from './useInteractiveRebase';
import { useLFS } from './useLFS';
import { remotesQuery } from './useRemote';
import { useReset } from './useReset';
import { useRevert } from './useRevert';
import { useStash } from './useStash';
import { useSubmodules } from './useSubmodules';
import { useTags } from './useTags';
import { useUpstream } from './useUpstream';
import { useWorktrees } from './useWorktrees';

/**
 * Everything the history screen can do to a repository, created in one place.
 *
 * Twelve hooks, and the screen needs all of them at once: a reference offers a
 * checkout, a merge, a rebase, an upstream and a delete; the commit under the
 * history offers a cherry-pick, a revert, a reset and a rewrite; the panels
 * beside it offer stashes, worktrees, submodules and LFS patterns. Naming them
 * here rather than at the top of the component is what lets the panels and the
 * confirmations they open be two files instead of one: both are handed this,
 * and both are therefore working on the same mutations and the same cache.
 *
 * The bare-repository flags are the only decision made here, and they are the
 * same one twice: a repository with no work tree has nothing to stash and
 * nowhere to write `.gitattributes`, so the daemon refuses those routes and
 * the lists behind them are never asked for.
 */
export function useHistoryOperations(repositoryId: string, bare: boolean) {
  return {
    checkOut: useCheckOut(repositoryId),
    branches: useBranches(repositoryId),
    upstream: useUpstream(repositoryId),
    tags: useTags(repositoryId),
    cherryPick: useCherryPick(repositoryId),
    revert: useRevert(repositoryId),
    reset: useReset(repositoryId),
    rewrite: useInteractiveRebase(repositoryId),
    stash: useStash(repositoryId, !bare),
    worktrees: useWorktrees(repositoryId),
    submodules: useSubmodules(repositoryId),
    lfs: useLFS(repositoryId, !bare),
    remotes: useQuery(remotesQuery(repositoryId)),
  };
}

export type HistoryOperations = ReturnType<typeof useHistoryOperations>;
