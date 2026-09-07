import type { Stash, StashApplyPlan, StashDropPlan, StashPushPlan } from '../api/types';
import { counted, pluralize, shortenSha } from '../lib/format';

/**
 * What each stash operation is about to do, said in words.
 *
 * Pure functions over the plans, kept apart from the dialogs that draw them
 * for the reason resetSummary is: every sentence here is a fact about git, and
 * facts about git are worth reading and testing on their own.
 *
 * They exist because a command is not an explanation, and in this family the
 * gap is unusually wide. `git stash pop 'stash@{1}'` says nothing about which
 * work is coming back, whether the entry survives, or that there is already
 * uncommitted work in the way — and that last one is the whole difference
 * between an apply that lands and one that stops on a conflict.
 */

/**
 * How a stash is named in a sentence.
 *
 * The message where there is one, the position where there is not. A stash
 * made without `-m` still has a message — git writes "WIP on main: 5956208
 * feat(…)" — so the empty case is rarer than it looks and still has to read as
 * something rather than as nothing.
 */
export function stashLabel(stash: Pick<Stash, 'index' | 'message'>): string {
  return stash.message === '' ? stashRef(stash.index) : `“${stash.message}”`;
}

/**
 * The position as git writes it.
 *
 * One definition, so a row, a sentence and a command all say stash@{2} the
 * same way. The daemon has its own — see git.Stash.Ref — and they agree
 * because there is only one way to write it, not because either read the
 * other.
 */
export function stashRef(index: number): string {
  return `stash@{${index}}`;
}

/**
 * What stashing would save, and what it would leave behind.
 *
 * The second half is the point. `git stash push` without --include-untracked
 * leaves every untracked file exactly where it is, and a dialog that said "3
 * files" over a work tree of one changed file and two new ones would be
 * describing something that does not happen.
 */
export function stashPushSummary(plan: StashPushPlan): string {
  const saved = stashPushSaves(plan);
  const where = plan.branch === '' ? 'the commit HEAD is on' : plan.branch;
  const back = plan.branch === '' ? 'that commit' : `${plan.branch}’s tip`;

  // Nothing to save is a state the dialog is allowed to be in — it is the one
  // the tick box exists to change — so it gets a sentence rather than "0 files
  // go into a stash", which reads as a broken count rather than as an answer.
  if (saved === 0) {
    return plan.untracked > 0
      ? `Nothing tracked has changed here, and ${counted(plan.untracked, 'untracked file', 'the untracked file below is', 'the untracked files below are')} left where they are unless you include them.`
      : `The work tree matches ${where}. There is nothing to stash.`;
  }

  const leftBehind =
    !plan.include_untracked && plan.untracked > 0
      ? ` ${counted(plan.untracked, 'untracked file', 'stays where it is', 'stay where they are')}.`
      : '';

  return (
    `${counted(saved, 'file', 'goes', 'go')} into a stash filed under ${where}, ` +
    `and the work tree goes back to ${back}.` +
    leftBehind
  );
}

/**
 * How many files this plan's flags would actually save.
 *
 * One definition, because two things read it and they must not disagree: the
 * sentence above, and the button that is disabled when the answer is zero. The
 * daemon has the same reading — see git.StashPushPreview.Saving — and it is
 * the one that enforces it; this one only decides what the dialog looks like.
 */
export function stashPushSaves(plan: StashPushPlan): number {
  return plan.include_untracked ? plan.tracked + plan.untracked : plan.tracked;
}

/**
 * What putting a stash back would do.
 *
 * Three things the command does not say: which work comes back, whether the
 * entry survives, and whether there is anything in the way. The third is not a
 * refusal — git merges a stash into work in progress, and that is an ordinary
 * thing to want — but it is the moment worth naming, because it is the one
 * that can end in conflict markers rather than in files.
 */
export function stashApplySummary(plan: StashApplyPlan): string {
  const files = pluralize(plan.files, 'file');
  const named = stashLabel(plan);

  const fate =
    plan.mode === 'pop'
      ? `${named} comes back into the work tree, in ${files}, and leaves the stack.`
      : `${named} comes back into the work tree, in ${files}, and stays in the stack.`;

  if (plan.dirty_files === 0) {
    return `${fate} The work tree is clean, so nothing has to be merged.`;
  }
  return (
    `${fate} ${counted(plan.dirty_files, 'file', 'already differs', 'already differ')} here, ` +
    `so git merges the two — and stops on conflict markers where they disagree.`
  );
}

/**
 * What dropping a stash would do.
 *
 * Said plainly, because the loss list beside it is the part that has to be
 * exact and this is the part that has to be read.
 */
export function stashDropSummary(plan: StashDropPlan): string {
  return (
    `${stashLabel(plan)} leaves the stack. Nothing else points at its commit ` +
    `afterwards, so git collects it in its own time.`
  );
}

/**
 * What dropping takes away, named precisely.
 *
 * The non-empty tuple is what makes naming the loss a compile-time
 * requirement of ConfirmDialog's destructive arm. There is always something to
 * name here — a stash git would make holds at least one file — so unlike
 * resetLosses this never has to answer with nothing.
 *
 * "until git collects it" and not "gone": the commit survives as an
 * unreachable object, and somebody who wrote down the object name can still
 * reach it. Saying so is the difference between a warning people believe and
 * one they learn to click through. The name is in the list for exactly that
 * reason — it is the thing to write down.
 */
export function stashDropLosses(plan: StashDropPlan): readonly [string, ...string[]] {
  return [
    `the changes it holds, in ${pluralize(plan.files, 'file')}`,
    `${stashRef(plan.index)}, whose commit ${shortenSha(plan.sha)} becomes unreachable ` +
      `and stays readable only until git collects it`,
  ];
}

/**
 * Whether stashing can be offered at all, and why not when it cannot.
 *
 * The sentence rather than a boolean, for the reason OperationBanner's
 * blockedBy answers with one: a button greyed with nothing to say reads as a
 * screen that is broken rather than as a repository that is not ready.
 *
 * Only the reasons this page can see for itself. A repository in the middle of
 * a merge is one the daemon refuses, and its refusal arrives as a toast — the
 * button cannot be disabled on a state that is not in this component's hands.
 */
export function stashPushRefusal(changed: number): string | undefined {
  return changed === 0 ? 'The work tree matches HEAD, so there is nothing to stash' : undefined;
}
