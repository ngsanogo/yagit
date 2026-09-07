import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { Head } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';
import { shortenSha } from '../lib/format';
import { settle } from './settle';

/**
 * Checking out, from wherever the interface offers it.
 *
 * One hook because there is one operation, and its consequences are the same
 * whichever button ran it: HEAD moved, so the history is a different walk, the
 * working directory is a different set of files, and the message git would
 * prepare for the next commit came from somewhere else. Two copies of that
 * list would be two chances for one of them to forget an entry.
 */

/** What to check out, and what to call it on screen. */
export interface CheckOutRequest {
  /** The reference git is given, exactly as it appears in the command. */
  ref: string;

  /**
   * Whether HEAD ends up on the commit rather than on the name.
   *
   * True for anything that is not a local branch — a tag and a
   * remote-tracking branch are not places HEAD can sit — and true for a row of
   * the history, which is a commit and nothing else.
   */
  detach: boolean;

  /** How the reference is named in the toast. `ref` is often a raw SHA. */
  label: string;
}

/** What to check out for a commit: itself, with no branch on it. */
export function checkOutRequestForCommit(sha: string): CheckOutRequest {
  return { ref: sha, detach: true, label: shortenSha(sha) };
}

export function useCheckOut(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  return useMutation({
    mutationFn: ({ ref, detach }: CheckOutRequest) => api.switchTo(repositoryId, ref, detach),

    onSuccess: async (refs, request) => {
      // The operation settle.ts was written for: the references the daemon
      // just answered with, and everything HEAD moving changed that no answer
      // carries.
      await settle(queryClient, repositoryId, { refs, headMoved: true });

      toast.push({ tone: 'success', title: arrivalOf(refs.head, request) });
    },

    onError: (error: Error, { label }) => {
      toast.push({
        tone: 'danger',
        // git's own refusal, whole. It is the only thing on screen that can
        // say which files are in the way of the switch.
        title: `Could not check out ${label}`,
        detail: errorDescription(error),
      });
    },
  });
}

/**
 * What the toast says once HEAD has moved.
 *
 * Read off the HEAD the daemon sent back rather than off the request, so the
 * sentence describes where the repository actually is. Those differ more often
 * than they look: `main` and `main^` and `v1.0` are three requests that can
 * land on one commit, and only one of them leaves a branch name to report.
 */
function arrivalOf(head: Head | undefined, request: CheckOutRequest): string {
  if (head === undefined) {
    // No commit in the repository at all. Nothing can be checked out there,
    // so this is unreachable from any button — and a toast that said "now on
    // undefined" would be worse than a plain one.
    return `Checked out ${request.label}`;
  }
  if (head.detached) {
    return `HEAD detached at ${request.label} — ${shortenSha(head.sha)}`;
  }
  return `Now on ${head.name}`;
}
