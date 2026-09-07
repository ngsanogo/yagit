import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../api/client';
import type { Operation, OperationAction, WorkingDirectory } from '../api/types';
import { useToast } from '../components/ToastHost';
import { errorDescription } from '../lib/errorDisplay';

/**
 * Finishing, or calling off, what the repository is in the middle of.
 *
 * One mutation for all three instructions, because they differ in a word and
 * settle identically: every one of them can move HEAD, rewrite the work tree
 * and end the operation, so there is nothing any of them changes that the
 * others do not.
 *
 * The plan beside it is a mutation rather than a query for the reason
 * planPush is one: it is a question with a body and no cache, and two aborts a
 * minute apart must both ask, because the answer describes the repository as
 * it is now.
 */

/** What was asked for, and what to call it in a toast once it has happened. */
export interface OperationRequest {
  action: OperationAction;

  /**
   * What the interface was showing. Sent to be disagreed with: the daemon
   * reads the state itself and refuses when it has moved.
   */
  operation: Operation;

  /**
   * Which instance of that operation. Kind alone is not enough when one
   * rebase finishes and another starts while a dialog sits open.
   */
  identity: string;
}

export function useOperation(repositoryId: string) {
  const queryClient = useQueryClient();
  const toast = useToast();

  const act = useMutation({
    mutationFn: ({ action, operation, identity }: OperationRequest) =>
      api.actOnOperation(repositoryId, action, operation, identity),

    onSuccess: async (status: WorkingDirectory, { action, operation }) => {
      // The status is written rather than invalidated: the daemon read it
      // after the command, and asking again is a second `git status` for an
      // answer already in hand. The cancel first, so a poll already in flight
      // cannot land on top with the operation as it was before the click.
      await queryClient.cancelQueries({ queryKey: ['status', repositoryId] });
      queryClient.setQueryData(['status', repositoryId], status);

      // Everything else this moved, and it is nearly everything. An abort puts
      // a branch back and a continue commits: HEAD moved, so the graph, the
      // references, the open diff and the message git had prepared are all
      // answers to questions asked of a repository that no longer exists.
      for (const key of ['refs', 'commits', 'commit', 'diff', 'prepared-message']) {
        void queryClient.invalidateQueries({ queryKey: [key, repositoryId] });
      }

      toast.push({ tone: 'success', title: succeeded(action, operation) });
    },

    onError: (error: Error, { action }) => {
      // A refused instruction changed nothing, and the status still has to be
      // re-read: the commonest refusal is "the operation is not the one you
      // named", which is the screen being stale by definition. Waiting up to
      // two seconds for the poll would leave the banner arguing with the
      // toast in front of it.
      void queryClient.invalidateQueries({ queryKey: ['status', repositoryId] });
      void queryClient.invalidateQueries({ queryKey: ['refs', repositoryId] });

      toast.push({
        tone: 'danger',
        title: failed(action),
        // git's own refusal, whole. It is the only thing on screen that can
        // name the file still unmerged, or say the commit became empty and
        // has to be skipped.
        detail: errorDescription(error),
      });
    },
  });

  const plan = useMutation({
    mutationFn: (action: OperationAction) => api.planOperation(repositoryId, action),
  });

  return { act, plan };
}

/**
 * What happened, in git's own vocabulary.
 *
 * The operation is named because the toast outlives the banner it came from:
 * aborting makes the banner disappear, so "Aborted" alone would be a sentence
 * about something no longer on screen.
 */
function succeeded(action: OperationAction, operation: Operation): string {
  const name = operationName(operation);
  switch (action) {
    case 'abort':
      return `Aborted the ${name}`;
    case 'continue':
      return `Continued the ${name}`;
    case 'skip':
      return `Skipped a commit in the ${name}`;
  }
}

function failed(action: OperationAction): string {
  switch (action) {
    case 'abort':
      return 'Could not abort';
    case 'continue':
      return 'Could not continue';
    case 'skip':
      return 'Could not skip this commit';
  }
}

/**
 * The operation as a noun, for a sentence that puts "the" in front of it.
 *
 * git's own words throughout, for the reason the banner's headline uses them:
 * a friendlier invention would be a name that appears in no other answer
 * anybody finds.
 */
export function operationName(operation: Operation): string {
  switch (operation) {
    case 'merge':
      return 'merge';
    case 'rebase':
      return 'rebase';
    case 'cherry-pick':
      return 'cherry-pick';
    case 'revert':
      return 'revert';
    case 'bisect':
      return 'bisect';
    case 'am':
      return 'patch series';
    case '':
      return 'operation';
  }
}
