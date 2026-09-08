import type { RebasePlan } from '../api/types';
import { pluralize } from '../lib/format';

/**
 * What a rebase is about to do, said in words.
 *
 * A pure function over the plan, kept apart from the dialog that draws it for
 * the reason mergeSummary is: every sentence here is a fact about git — that a
 * fast-forward writes no commit, that a replay writes each one again under a
 * new hash — and facts about git are worth reading and testing on their own
 * rather than spelt out inside a JSX ternary.
 *
 * It exists because the command alone is not an explanation. `git rebase
 * --merge --no-autosquash --no-autostash --no-rebase-merges --no-update-refs
 * --no-ff -- refs/heads/main` says what will run and says nothing about what
 * comes back: whether the branch is rewritten or merely moved, how much of it,
 * or that the answer is "nothing at all". The daemon has already read all of
 * that off the two branches; this is where it becomes a sentence.
 */
/**
 * Why merge commits do not survive a rebase, written once.
 *
 * The dialog says this on the explanation and again on the red list, and it
 * used to say it in three different sets of words in this one file — so a
 * change to how the loss is worded had to be made in three places and the
 * dialog read the same sentence twice. One clause, three sentences that use
 * it, and `--no-rebase-merges` in RebaseArgs is the flag it is about.
 */
const STRAIGHT_LINE = 'a rebase replays a straight line';

export function rebaseSummary(plan: RebasePlan): string {
  const arriving = pluralize(plan.behind, 'commit');
  const own = pluralize(plan.rewriting, 'commit');

  switch (plan.outcome) {
    case 'up-to-date':
      // Not an error, and not a rebase either. True whether the branch sits
      // exactly on the upstream or has run a hundred commits past it: git
      // answers "Current branch is up to date" to both, because in both the
      // upstream is already an ancestor and there is nowhere to move to.
      return `${plan.onto} is already part of ${plan.from}. Rebasing changes nothing.`;

    case 'fast-forward':
      // Two halves, and the second is the one a count of replayed commits
      // would never reach. Nothing is replayed, no commit is written, no hook
      // runs and no hash changes — and the branch still moves, so the work
      // tree is checked out again underneath it and the files on disk stop
      // being the ones that were there. An outcome that rewrites no history
      // is not an outcome that changes nothing.
      return (
        `${plan.from} has no commit of its own, so it moves up to ${plan.onto} ` +
        `by ${arriving}. Nothing is replayed and no commit is rewritten, but the ` +
        `files in your work tree become ${plan.onto}'s.`
      );

    case 'rebase':
      // The branch that holds nothing past the fork but merge commits: a
      // straight-line replay writes none of them again and recreates none of
      // them either, so the branch simply lands on the new base with them
      // gone. Rare and reachable — a branch made only of merges the upstream
      // already contains — and the sentence below would otherwise open with
      // "0 commits" and promise to write them again.
      if (plan.rewriting === 0) {
        return (
          `${plan.from} has nothing but ${pluralize(plan.flattening, 'merge commit')} ` +
          `past ${plan.onto}, and ${STRAIGHT_LINE}: none of them is recreated. The ` +
          `branch lands on ${plan.onto} with them gone.`
        );
      }

      // The branch that is already PAST the upstream and gets rewritten
      // anyway. git takes its up-to-date shortcut only across linear history,
      // so one merge commit in the range costs the whole branch — and nothing
      // is arriving, which the sentence below would report as `0 commits` of
      // the upstream's own while giving no reason for a rewrite at all. The
      // reason is the merges, so they are what it names. Reachable whenever
      // somebody merges main into their branch twice and then rebases; see
      // git.rebaseOutcomeOf.
      if (plan.behind === 0) {
        return (
          `${plan.onto} brings nothing new to ${plan.from}, and a rebase would leave it ` +
          `alone but for ${pluralize(plan.flattening, 'merge commit')} in the way: git ` +
          `has no shortcut across one. It writes ${own} again on top of ${plan.onto} — ` +
          `new hashes, your hooks on each, and a conflict as the ordinary way to stop ` +
          `halfway.`
        );
      }

      // And here the opposite of a fast-forward, for the same reason: this one
      // rewrites, and the sentence has to say so before the button does it.
      // The count is what the branch holds past the fork rather than a
      // prediction of git's replay loop — git also drops a commit whose patch
      // is already upstream, and one that comes out empty against the new base
      // — but every one of them stops being what the branch points at either
      // way, which is what this sentence and rebaseLosses are about.
      //
      // Flattening is counted when there is any, because the sentence that
      // stopped at "N commits that main does not" counted only ordinary
      // commits and left the merge commits for the red list to mention — a
      // dialog whose explanation and whose warning disagreed about how much
      // of the branch was about to go.
      //
      // Counted, and not explained twice. rebaseLosses names the same merge
      // commits on the same dialog and says why they go; repeating the reason
      // here is the explanation and the warning reading as one sentence
      // printed twice.
      return (
        `${plan.from} has ${own} that ${plan.onto} does not, and ${plan.onto} has ` +
        `${arriving} of its own. Rebasing writes them again on top of ${plan.onto} — ` +
        `new hashes, your hooks on each, and a conflict as the ordinary way to stop halfway.` +
        (plan.flattening > 0
          ? ` It also discards ${pluralize(plan.flattening, 'merge commit')}.`
          : '')
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
        `than the daemon it is talking to. Reload before rebasing.`
      );
  }
}

/**
 * What a rebase takes away, named precisely.
 *
 * Only the replay has any, which is why this is not called for the other two:
 * an up-to-date rebase writes nothing and a fast-forward moves a pointer.
 * A replay is the most history-destroying thing the interface does, and
 * ConfirmDialog's non-empty tuple is what makes naming the loss a compile-time
 * requirement rather than a rule somebody remembers.
 *
 * Two entries, and each is dropped where it would be about nothing. The commits
 * the branch points at now do not survive the replay — new ones with the same
 * changes take their place, and the originals are reachable only through the
 * reflog. And `--no-rebase-merges` replays a straight line, so any merge commit
 * inside the range is simply gone; nothing else on the dialog would say so, and
 * both counts come from the same reading of the two branches as the sentence.
 *
 * Never empty, which is what ConfirmDialog's tuple requires: the two numbers
 * partition what the branch holds past the fork, and a replay is the outcome
 * where that is not zero. The both-zero case is therefore unreachable, and the
 * order below is what keeps the reachable ones from reading "0 commits".
 */
export function rebaseLosses(plan: RebasePlan): readonly [string, ...string[]] {
  const rewritten =
    `the ${pluralize(plan.rewriting, 'commit')} ${plan.from} points at now — ` +
    `the rebase writes new ones in their place`;
  const flattened =
    `${pluralize(plan.flattening, 'merge commit')} on ${plan.from} — ` +
    `${STRAIGHT_LINE} and recreates none of them`;

  if (plan.rewriting === 0) {
    return [flattened];
  }
  if (plan.flattening === 0) {
    return [rewritten];
  }
  return [rewritten, flattened];
}
