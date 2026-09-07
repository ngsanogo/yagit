import type { CherryPickPlan } from '../api/types';
import { namedCommit } from '../lib/format';

/**
 * What a cherry-pick is about to do, said in words.
 *
 * A pure function over the plan, kept apart from the dialog that draws it for
 * the reason mergeSummary is: every sentence here is a fact about git, and
 * facts about git are worth reading and testing on their own.
 *
 * It exists because the command alone is not an explanation. `git cherry-pick
 * --no-edit --no-ff -- a2801ba` says what will run and says nothing about
 * whether a commit is recorded under somebody's signature, or that the branch
 * is simply moving onto a commit it already has as its next parent.
 */
export function cherryPickSummary(plan: CherryPickPlan): string {
  const named = namedCommit(plan.commit, plan.subject);

  switch (plan.outcome) {
    case 'up-to-date':
      // The confirmation never opens for this one — see useCherryPick — but
      // the sentence still exists so a test, and a future dialog, can say the
      // same thing the toast does.
      return `${plan.into} already contains ${named}. Cherry-picking changes nothing.`;

    case 'fast-forward':
      return (
        `${plan.into} is the parent of ${named}, so it moves onto that commit. ` +
        `Nothing is committed.`
      );

    case 'cherry-pick':
      return (
        `${named} is applied onto ${plan.into} as a new commit, ` +
        `which runs your hooks and signing.`
      );

    default:
      return (
        `This page cannot explain what ${plan.command || 'this cherry-pick'} does here — ` +
        `it is older than the daemon it is talking to. Reload before cherry-picking.`
      );
  }
}
