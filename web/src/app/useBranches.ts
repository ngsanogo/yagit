import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { MergePlan, RebasePlan, RefsPayload } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * Making, unmaking, joining and replaying local branches.
 *
 * Beside useCheckOut rather than inside it, because these change a different
 * thing. A checkout moves HEAD, so the history, the working directory and the
 * message git would prepare all become somebody else's — that hook's job is
 * that list. These edit the set of references, and the exceptions are written
 * out where they happen: creating a branch can also stand on it, renaming the
 * branch HEAD is on carries HEAD with it, and merging and rebasing both move
 * the branch HEAD is standing on while rewriting the files under it — the
 * second of them by writing its commits again under new hashes.
 */

/** What creating a branch asks for. */
export interface CreateBranchRequest {
  name: string;
  /** Any revision git accepts. Empty means HEAD, which git resolves. */
  start: string;
  /** Whether to stand on it once it exists. */
  switchTo: boolean;
}

export function useBranches(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  /** What every one of these leaves stale. See settle.ts. */
  const settleBranches = (refs: RefsPayload | 'stale', headMoved = false) =>
    settle(queryClient, repositoryId, { refs, headMoved });

  const create = useMutation({
    mutationFn: ({ name, start, switchTo }: CreateBranchRequest) =>
      api.createBranch(repositoryId, name, start, switchTo),
    onSuccess: async (refs, { name, switchTo }) => {
      // Standing somewhere new is the same set of consequences a checkout has,
      // which is why it is the same call rather than the same list written out
      // again.
      await settleBranches(refs, switchTo);
      toast.push({
        tone: 'success',
        title: switchTo ? `Now on ${name}` : `Created ${name}`,
      });
    },
    onError: (error: Error, { name }) => {
      toast.push({
        tone: 'danger',
        title: `Could not create ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  const rename = useMutation({
    mutationFn: ({ from, to }: { from: string; to: string }) =>
      api.renameBranch(repositoryId, from, to),
    onSuccess: async (refs, { from, to }) => {
      await settleBranches(refs);
      // The prepared message is not touched, and that is deliberate: renaming
      // the branch HEAD is on moves HEAD but does not change which commit it
      // points at, so the message an amend would start from is the same one.
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

  /**
   * What deleting would run — asked before the confirmation opens.
   *
   * A mutation and not a query, because it is a question with a body and no
   * cache: two deletes of the same branch a minute apart must both ask, since
   * the answer names a flag that depends on what the user chose this time.
   */
  const planDelete = useMutation({
    mutationFn: ({ name, force }: { name: string; force: boolean }) =>
      api.planDeleteBranch(repositoryId, name, force),
  });

  const remove = useMutation({
    mutationFn: ({ name, force }: { name: string; force: boolean }) =>
      api.deleteBranch(repositoryId, name, force),
    onSuccess: async (refs, { name }) => {
      await settleBranches(refs);
      toast.push({ tone: 'success', title: `Deleted ${name}` });
    },
    onError: (error: Error, { name }) => {
      toast.push({
        tone: 'danger',
        // git's own refusal, whole. It is the only thing on screen that can
        // say the branch is not fully merged, and which commits go with it.
        title: `Could not delete ${name}`,
        detail: errorDescription(error),
      });
    },
  });

  /**
   * What merging would do — asked before the confirmation opens.
   *
   * A mutation and not a query, for the reason planDelete is one: it is a
   * question with a body and no cache. The answer is more than a command here.
   * Whether the merge is a pointer moving, a commit under the user's signature
   * or nothing at all is read from the two branches at this moment, and the
   * plan carries it back so the same reading names the operation, writes the
   * sentence and chooses the flag.
   */
  const planMerge = useMutation({
    mutationFn: ({ branch, mergeCommit = false }: { branch: string; mergeCommit?: boolean }) =>
      api.planMerge(repositoryId, branch, mergeCommit),
  });

  const merge = useMutation({
    mutationFn: (plan: MergePlan) => api.merge(repositoryId, plan),
    onSuccess: async (refs, plan) => {
      // A merge moves the branch HEAD is on, so everything a checkout settles
      // has to settle here too: the files on disk are a different set, and the
      // message for the next commit came from another place.
      await settleBranches(refs, true);
      toast.push({ tone: 'success', title: mergeReport(plan) });
    },
    onError: (error: Error, plan) => {
      // A conflict arrives here, and git has already written the markers into
      // the work tree. The banner above the history says what the repository
      // is in the middle of — and it says it now rather than at the next poll,
      // which is what the re-read below buys.
      void settleBranches('stale', true);
      toast.push({
        tone: 'danger',
        // git's own refusal, whole. It is the only thing on screen that can
        // name the files that stopped the merge.
        title: `Could not merge ${plan.branch}`,
        detail: errorDescription(error),
      });
    },
  });

  /**
   * What rebasing would do — asked before the confirmation opens.
   *
   * A mutation and not a query, for the reason planMerge is one. The answer
   * decides more here than it does there: whether the branch is rewritten,
   * merely moved or left alone is read from the two branches at this moment,
   * and the plan carries it back so the same reading names the operation,
   * writes the sentence, lists what disappears and chooses the flag.
   */
  const planRebase = useMutation({
    mutationFn: (onto: string) => api.planRebase(repositoryId, onto),
  });

  const rebase = useMutation({
    mutationFn: (plan: RebasePlan) => api.rebase(repositoryId, plan),
    onSuccess: async (refs, plan) => {
      // A rebase moves the branch HEAD is on and checks out a tree per commit,
      // so everything a checkout settles has to settle here too — and more of
      // it than a merge does: the commits themselves are different objects, so
      // nothing keyed on a SHA is still about anything.
      await settleBranches(refs, true);
      toast.push({ tone: 'success', title: rebaseReport(plan) });
    },
    onError: (error: Error, plan) => {
      // A conflict arrives here, and git has already stopped partway through
      // the sequence with the markers written. The banner above the history
      // says what the repository is in the middle of — now rather than at the
      // next poll, which is what the re-read below buys.
      void settleBranches('stale', true);
      toast.push({
        tone: 'danger',
        // git's own refusal, whole. It is the only thing on screen that can
        // name the commit the replay stopped on.
        title: `Could not rebase ${plan.from} onto ${plan.onto}`,
        detail: errorDescription(error),
      });
    },
  });

  return { create, rename, planDelete, remove, planMerge, merge, planRebase, rebase };
}

/**
 * What the toast says once the merge is done.
 *
 * The outcome the user approved rather than one word for all three: "Merged"
 * over a repository that did not move is a sentence somebody would go looking
 * for the commit of.
 *
 * The last line is not unreachable, whatever the type says. The outcome is a
 * string from a daemon that can be newer than the page holding it — a reload
 * behind a release, a tab left open across an upgrade — and falling off the
 * end of this switch returns undefined, which is a toast with no title at all
 * where a merge just happened. So a fourth outcome says the plainest true
 * thing instead. git.MergeArgs has the same shape on the other side, and for
 * the same reason.
 */
function mergeReport(plan: MergePlan): string {
  switch (plan.outcome) {
    case 'up-to-date':
      return `${plan.into} already had ${plan.branch}`;
    case 'fast-forward':
      return `Fast-forwarded ${plan.into} to ${plan.branch}`;
    case 'merge-commit':
      return `Merged ${plan.branch} into ${plan.into}`;
    default:
      return `Ran ${plan.command}`;
  }
}

/**
 * What the toast says once the rebase is done.
 *
 * Three sentences for three outcomes, for the reason mergeReport has three:
 * "Rebased feature onto main" over a repository that did not move is a
 * sentence somebody would go looking for the new commits of.
 *
 * The last line is not unreachable either, and it is unreachable for the same
 * reason mergeReport's is not — a daemon newer than the page holding it, and a
 * toast with no title at all where a rebase just happened. git.RebaseArgs has
 * the same shape on the other side.
 */
function rebaseReport(plan: RebasePlan): string {
  switch (plan.outcome) {
    case 'up-to-date':
      return `${plan.onto} was already part of ${plan.from}`;
    case 'fast-forward':
      return `Fast-forwarded ${plan.from} to ${plan.onto}`;
    case 'rebase':
      return `Rebased ${plan.from} onto ${plan.onto}`;
    default:
      return `Ran ${plan.command}`;
  }
}
