import type { MergePlan } from '../api/types';
import { pluralize } from '../lib/format';

/**
 * What a merge is about to do, said in words.
 *
 * A pure function over the plan, kept apart from the dialog that draws it for
 * the reason remote.ts is kept apart from the bar: every sentence here is a
 * fact about git — that a fast-forward writes no object, that a merge commit
 * runs the user's hooks — and facts about git are worth reading and testing on
 * their own rather than spelt out inside a JSX ternary.
 *
 * It exists because the command alone is not an explanation. `git merge
 * --no-ff --no-edit -- pickup` says what will run and says nothing about what
 * comes back: whether a commit is recorded under somebody's signature,
 * how much is arriving, or that the answer is "nothing at all". The daemon has
 * already read all three off the two branches; this is where they become a
 * sentence.
 */
export function mergeSummary(plan: MergePlan): string {
  const arriving = pluralize(plan.behind, 'commit');

  switch (plan.outcome) {
    case 'up-to-date':
      // Not an error, and not a merge either. The button still runs, because
      // "already up to date" is git's answer and hearing it is not a failure —
      // but nobody should press it expecting their history to change.
      return `${plan.into} already contains ${plan.branch}. Merging changes nothing.`;

    case 'fast-forward':
      // The half worth saying is what does NOT happen: no commit is written,
      // so no hook runs, nothing is signed, and there is no merge to undo
      // afterwards — the branch simply moves.
      return (
        `${plan.into} has no commit of its own since ${plan.branch} left it, ` +
        `so it moves up by ${arriving}. Nothing is committed.`
      );

    case 'merge-commit':
      // And here the opposite, for the same reason: this one commits, and a
      // commit means the user's hooks, their identity and their signing key.
      return (
        `${plan.branch} brings ${arriving}, and ${plan.into} has ` +
        `${pluralize(plan.ahead, 'commit')} of its own. ` +
        `git joins them with a merge commit, which runs your hooks and signing.`
      );

    default:
      // Not unreachable, whatever the type says: the outcome is a string from
      // a daemon that can be newer than the page holding it, and falling off
      // the end of this switch returns undefined — which reaches the dialog as
      // a missing description, so the command above the button would be the
      // only thing on screen and a screen reader would announce no explanation
      // at all. Saying that the page cannot explain the line is worse than a
      // sentence and far better than silence.
      return (
        `This page cannot explain what ${plan.command} does here — it is older ` +
        `than the daemon it is talking to. Reload before merging.`
      );
  }
}
