import type { RevertPlan } from '../api/types';
import { namedCommit } from '../lib/format';

/**
 * What a revert is about to do, said in words.
 *
 * A pure function over the plan, kept apart from the dialog that draws it for
 * the reason cherryPickSummary is: every sentence here is a fact about git, and
 * facts about git are worth reading and testing on their own.
 *
 * It exists because the command alone is not an explanation. `git revert
 * --no-edit --no-reference -- a2801ba` says what will run and says nothing
 * about a commit being recorded under somebody's signature, or that the
 * changes being taken back out are the ones that commit introduced.
 */
export function revertSummary(plan: RevertPlan): string {
  const named = namedCommit(plan.commit, plan.subject);

  switch (plan.outcome) {
    case 'revert':
      return (
        `The changes from ${named} are taken back out of ${plan.into} as a new commit, ` +
        `which runs your hooks and signing.`
      );

    default:
      return (
        `This page cannot explain what ${plan.command || 'this revert'} does here — ` +
        `it is older than the daemon it is talking to. Reload before reverting.`
      );
  }
}
