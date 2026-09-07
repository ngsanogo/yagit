import { describe, expect, it } from 'vitest';

import type { MergePlan } from '../api/types';
import { mergeSummary } from './merge';

/**
 * What the confirmation says a merge will do.
 *
 * The command beside it is exact and the daemon wrote it; these sentences are
 * the part a person actually reads, and each of the three has to say the thing
 * that distinguishes it — that nothing is committed, that something is, or
 * that nothing happens at all.
 */

function plan(overrides: Partial<MergePlan> = {}): MergePlan {
  return {
    command: 'git merge --ff-only -- pickup',
    branch: 'pickup',
    into: 'main',
    outcome: 'fast-forward',
    ahead: 0,
    behind: 1,
    ...overrides,
  };
}

describe('what the merge confirmation says will happen', () => {
  it('says a fast-forward commits nothing', () => {
    const sentence = mergeSummary(plan({ behind: 3 }));

    expect(sentence).toContain('3 commits');
    expect(sentence).toContain('Nothing is committed');
  });

  // The one case where hooks run, a signature is made and the history grows a
  // node. Somebody about to approve that should be told.
  it('says a merge commit is a commit', () => {
    const sentence = mergeSummary(
      plan({ outcome: 'merge-commit', ahead: 2, behind: 1, command: 'git merge --no-ff' }),
    );

    expect(sentence).toContain('1 commit,');
    expect(sentence).toContain('2 commits of its own');
    expect(sentence).toContain('merge commit');
    expect(sentence).toContain('hooks');
  });

  // Not an error and not a merge. The button is still there, so the sentence
  // is the only thing that can say pressing it changes nothing.
  it('says so when the branch is already in', () => {
    expect(mergeSummary(plan({ outcome: 'up-to-date', behind: 0 }))).toBe(
      'main already contains pickup. Merging changes nothing.',
    );
  });

  it('names both branches, whichever way it turns out', () => {
    for (const outcome of ['up-to-date', 'fast-forward', 'merge-commit'] as const) {
      const sentence = mergeSummary(plan({ outcome, branch: 'lanes', into: 'trunk' }));

      expect(sentence).toContain('lanes');
      expect(sentence).toContain('trunk');
    }
  });

  // One commit is one commit. The counts come from a walk of the history and
  // land in the middle of a sentence, so the plural has to follow them.
  it('counts one commit as one', () => {
    expect(mergeSummary(plan({ behind: 1 }))).toContain('1 commit.');
  });

  // The outcome is a string from a daemon that can be newer than this page.
  // Falling off the end of the switch returns undefined, which reaches the
  // dialog as a missing description — the command with no explanation beside
  // it, and nothing at all for a screen reader to announce.
  it('still says something about an outcome it has never heard of', () => {
    const sentence = mergeSummary(
      plan({ outcome: 'squash' as MergePlan['outcome'], command: 'git merge --squash -- pickup' }),
    );

    expect(sentence).toContain('git merge --squash -- pickup');
    expect(sentence).toContain('Reload');
  });
});
