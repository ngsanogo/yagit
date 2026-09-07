import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { api } from '../api/client';
import type { PullStrategy, RefsPayload } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { settle } from './settle';

/**
 * The three commands that leave the machine.
 *
 * One hook, because what they change overlaps and the overlap is the part that
 * is easy to get wrong. All three move where the branch stands relative to its
 * upstream, which is drawn from two different queries — the references in the
 * sidebar and the status the bar counts from — so all three settle both. A
 * pull changes more than that, and says so where it happens.
 *
 * These are also the only mutations in the application that can take minutes,
 * or an hour — a large repository over a domestic line. Nothing here waits
 * differently for that: the client's deadline on these routes measures SILENCE
 * rather than elapsed time (see NETWORK_IDLE_MS), so a transfer that keeps
 * reporting is never cut off, and every button that starts one shows it is
 * running until the answer comes back.
 */

/**
 * The configured remotes, in one place.
 *
 * A factory rather than a key written out at each call site: the bar and the
 * history view both need the list, and two hand-written keys are two chances
 * for one of them to read a cache nothing writes. Same shape as commitQuery
 * and undoQuery.
 */
export function remotesQuery(repositoryId: string) {
  return {
    queryKey: ['remotes', repositoryId] as const,
    queryFn: () => api.remotes(repositoryId),
  };
}

/** What pushing asks for, and what to call it once it has happened. */
export interface PushRequest {
  /**
   * Where to publish, used only when the branch follows nothing. A branch with
   * an upstream goes to that upstream whatever is sent: the daemon reads the
   * follow, and the browser never assembles a destination.
   */
  remote: string;

  /** `--force-with-lease --force-if-includes`, after the user was asked. */
  force: boolean;

  /** How the destination is named in the toast: "main → origin/main". */
  label: string;

  /**
   * What the confirmation showed, echoed so a run cannot push a different
   * branch. Absent on the ordinary push button, which shows no confirmation.
   */
  lease?: { local_branch: string; ref: string };
}

export function useRemote(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();
  /** The last stderr line from a fetch, pull or push still in flight. */
  const [progress, setProgress] = useState<string | undefined>();

  /**
   * What every one of these changed about where the branch stands, whether or
   * not it finished.
   *
   * `tracking` is the half all three share and the reason it is named in
   * settle.ts at all: the ahead and behind counts on these very buttons are
   * read from the status rather than from the references, and waiting up to
   * two seconds to stop being three commits behind is two seconds of a screen
   * disagreeing with itself.
   */
  const settleRemote = (refs: RefsPayload | 'stale', headMoved = false) =>
    settle(queryClient, repositoryId, { refs, tracking: true, headMoved });

  const fetchFrom = useMutation({
    mutationFn: (remote: string) => {
      setProgress(undefined);
      return api.fetchRemote(repositoryId, remote, setProgress);
    },
    onSettled: () => setProgress(undefined),
    onSuccess: async (refs, remote) => {
      await settleRemote(refs);
      toast.push({
        tone: 'success',
        title: remote === '' ? 'Fetched from every remote' : `Fetched from ${remote}`,
        // Nothing about what arrived, deliberately: the counts on the Pull
        // button say that, and they say it for as long as it is true rather
        // than for the four seconds a toast lasts.
      });
    },
    onError: (error: Error, remote) => {
      void settleRemote('stale');
      toast.push({
        tone: 'danger',
        title: remote === '' ? 'Could not fetch' : `Could not fetch from ${remote}`,
        detail: errorDescription(error),
      });
    },
  });

  const pull = useMutation({
    mutationFn: (strategy: PullStrategy) => {
      setProgress(undefined);
      return api.pull(repositoryId, strategy, setProgress);
    },
    onSettled: () => setProgress(undefined),
    onSuccess: async (refs) => {
      // What a pull changes and a fetch does not: HEAD moved, so the files on
      // disk are a different set and the message for the next commit came from
      // somewhere else. The same list a checkout settles, for the same reason.
      await settleRemote(refs, true);
      toast.push({ tone: 'success', title: 'Pulled' });
    },
    onError: (error: Error, strategy) => {
      // A conflict arrives here, and it is the reason this toast says as much
      // as it does: git stopped halfway, the work tree now holds markers, and
      // the sentence that says which files reached this state is git's own.
      // The banner above the history says what the repository is in the middle
      // of for as long as it lasts — and it says it now rather than at the
      // next poll, which is what the re-read below buys.
      void settleRemote('stale', true);
      toast.push({
        tone: 'danger',
        title: strategy === 'rebase' ? 'Could not pull and rebase' : 'Could not pull',
        detail: errorDescription(error),
      });
    },
  });

  const push = useMutation({
    mutationFn: ({ remote, force, lease }: PushRequest) => {
      setProgress(undefined);
      return api.push(repositoryId, remote, force, setProgress, lease);
    },
    onSettled: () => setProgress(undefined),
    onSuccess: async (refs, { label }) => {
      await settleRemote(refs);
      toast.push({ tone: 'success', title: `Pushed ${label}` });
    },
    onError: (error: Error, { label }) => {
      // A push that was refused moved nothing here, but a push that was
      // refused BECAUSE the remote has commits this branch does not is a fact
      // about the counts on these buttons — and the ref this repository holds
      // for that remote may have moved under the lease check.
      void settleRemote('stale');
      toast.push({
        tone: 'danger',
        // git's own refusal, whole. It is the only thing on screen that can
        // say the remote has commits this branch does not, or that the lease
        // was not held.
        title: `Could not push ${label}`,
        detail: errorDescription(error),
      });
    },
  });

  /**
   * What pushing would run — asked before a confirmation opens.
   *
   * A mutation and not a query, for the reason planDelete is one: it is a
   * question with a body and no cache, and two pushes of the same branch a
   * minute apart must both ask, because the answer names a destination read
   * from the repository as it is now.
   */
  const planPush = useMutation({
    mutationFn: ({ remote, force }: { remote: string; force: boolean }) =>
      api.pushPlan(repositoryId, remote, force),
  });

  return { fetchFrom, pull, push, planPush, progress };
}
