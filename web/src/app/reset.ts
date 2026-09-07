import type { ResetPlan } from '../api/types';
import { namedCommit, pluralize } from '../lib/format';

/**
 * What a reset is about to do, said in words.
 *
 * A pure function over the plan, kept apart from the dialog that draws it for
 * the reason revertSummary is: every sentence here is a fact about git's three
 * trees, and facts about git are worth reading and testing on their own.
 *
 * It exists because the command alone is not an explanation. `git reset
 * --hard a2801ba` says what will run and says nothing about which of HEAD,
 * the index and the work tree move, or how many commits leave the branch.
 */
export function resetSummary(plan: ResetPlan): string {
  const named = namedCommit(plan.commit, plan.subject);
  const dropped = pluralize(plan.dropping, 'commit');

  switch (plan.mode) {
    case 'soft':
      if (plan.dropping === 0) {
        return (
          `${plan.into} already points at ${named}. Soft reset changes nothing: ` +
          `HEAD, the index and the work tree stay where they are.`
        );
      }
      return (
        `${plan.into} moves back to ${named}. The ${dropped} past it become staged ` +
        `changes against that tip — HEAD moves; the index and the work tree stay.`
      );

    case 'mixed':
      if (plan.dropping === 0) {
        return (
          `${plan.into} already points at ${named}. Mixed reset clears the index ` +
          `to match that tip; uncommitted work in the work tree stays.`
        );
      }
      return (
        `${plan.into} moves back to ${named}. The ${dropped} past it become unstaged ` +
        `changes against that tip — HEAD and the index move; the work tree stays.`
      );

    case 'hard':
      if (plan.dropping === 0 && plan.dirty_files === 0) {
        return (
          `${plan.into} already points at ${named} and the work tree is clean. ` +
          `Hard reset changes nothing.`
        );
      }
      if (plan.dropping === 0) {
        return (
          `${plan.into} already points at ${named}. Hard reset discards the ` +
          `uncommitted changes in ${pluralize(plan.dirty_files, 'file')} and leaves ` +
          `the work tree matching that tip.`
        );
      }
      return (
        `${plan.into} moves back to ${named}. The ${dropped} past it leave the branch, ` +
        `and the index and work tree become that tip's — any uncommitted change goes with them.`
      );

    default:
      return (
        `This page cannot explain what ${plan.command || 'this reset'} does here — ` +
        `it is older than the daemon it is talking to. Reload before resetting.`
      );
  }
}

/**
 * What a hard reset takes away, named precisely.
 *
 * Soft and mixed keep the work; only hard is destructive in ConfirmDialog's
 * sense. The non-empty tuple is what makes naming the loss a compile-time
 * requirement. Counts of zero are dropped: a clean reset to HEAD has nothing
 * to lose and is not offered as destructive at all.
 *
 * Returns undefined when there is nothing to name — the caller treats that as
 * a non-destructive confirmation of a no-op hard reset.
 */
export function resetLosses(plan: ResetPlan): readonly [string, ...string[]] | undefined {
  if (plan.mode !== 'hard') {
    return undefined;
  }

  const items: string[] = [];
  if (plan.dropping > 0) {
    // "through the reflog" and not "only through the reflog": a dropped commit
    // that a tag or another branch also points at stays perfectly reachable,
    // and this list is read by people deciding whether to press a red button.
    // What is always true is that the reflog still holds them.
    items.push(
      `${pluralize(plan.dropping, 'commit')} past ${namedCommit(plan.commit, plan.subject)} — ` +
        `off ${plan.into}, reachable afterwards through the reflog`,
    );
  }
  if (plan.dirty_files > 0) {
    items.push(`uncommitted changes in ${pluralize(plan.dirty_files, 'file')}`);
  }
  if (items.length === 0) {
    return undefined;
  }
  return items as [string, ...string[]];
}
