import { describe, expect, it } from 'vitest';

import type { RebasePlan } from '../api/types';
import { rebaseLosses, rebaseSummary } from './rebase';

function plan(overrides: Partial<RebasePlan> = {}): RebasePlan {
  return {
    command:
      'git rebase --merge --no-autosquash --no-autostash --no-rebase-merges ' +
      '--no-update-refs --no-ff -- refs/heads/main',
    onto: 'main',
    from: 'feature',
    outcome: 'rebase',
    rewriting: 3,
    flattening: 0,
    behind: 2,
    ...overrides,
  };
}

describe('what the rebase confirmation says will happen', () => {
  it('counts what the branch holds and what it would move over', () => {
    const sentence = rebaseSummary(plan({ rewriting: 3, behind: 2 }));

    expect(sentence).toContain('3 commits');
    expect(sentence).toContain('2 commits');
    expect(sentence).toContain('feature');
    expect(sentence).toContain('main');
  });

  // The half the command cannot say: these commits do not survive as they are.
  it('says the commits are written again under new hashes', () => {
    expect(rebaseSummary(plan())).toContain('new hashes');
  });

  it('says up to date is not a rebase', () => {
    expect(rebaseSummary(plan({ outcome: 'up-to-date', rewriting: 0, behind: 0 }))).toBe(
      'main is already part of feature. Rebasing changes nothing.',
    );
  });

  // The outcome a one-directional count reported as "changes nothing" while git
  // moved the branch. It writes no commit, so the sentence says that, and it
  // does move, so the sentence says that too.
  it('says a fast-forward moves the branch and rewrites nothing', () => {
    const sentence = rebaseSummary(plan({ outcome: 'fast-forward', rewriting: 0, behind: 2 }));

    expect(sentence).toContain('no commit of its own');
    expect(sentence).toContain('2 commits');
    expect(sentence).toContain('Nothing is replayed and no commit is rewritten');
  });

  // The half that rewrites nothing and still changes everything on disk. It is
  // the whole of what a fast-forward costs, and a sentence that stopped at "no
  // commit is rewritten" would read as "nothing happens" over a working
  // directory about to become somebody else's.
  it('says a fast-forward still rewrites the work tree', () => {
    const sentence = rebaseSummary(plan({ outcome: 'fast-forward', rewriting: 0, behind: 2 }));

    expect(sentence).toContain('work tree');
    expect(sentence).toContain("become main's");
  });

  it('names both branches in every sentence', () => {
    for (const outcome of ['up-to-date', 'fast-forward', 'rebase'] as const) {
      const sentence = rebaseSummary(plan({ outcome }));

      expect(sentence).toContain('feature');
      expect(sentence).toContain('main');
    }
  });

  // A branch whose only commits past the fork are merge commits: a
  // straight-line replay writes none of them again and recreates none of them
  // either. The general sentence would open with "0 commits" and promise to
  // write them.
  it('says what happens where the branch holds nothing but merge commits', () => {
    const sentence = rebaseSummary(plan({ rewriting: 0, flattening: 2, behind: 2 }));

    expect(sentence).toContain('nothing but 2 merge commits');
    expect(sentence).toContain('none of them is recreated');
    expect(sentence).not.toContain('0 commit');
  });

  // The arrangement ADR 0023 exists for, said in words: the branch is AHEAD of
  // the upstream, so nothing is arriving, and git rewrites it anyway because a
  // merge commit in the range leaves it no shortcut. The general sentence
  // would report "main has 0 commits of its own" and give no reason at all.
  it('explains the rewrite of a branch the upstream brings nothing to', () => {
    const sentence = rebaseSummary(plan({ rewriting: 3, flattening: 1, behind: 0 }));

    expect(sentence).not.toContain('0 commit');
    expect(sentence).toContain('brings nothing new');
    expect(sentence).toContain('1 merge commit');
    expect(sentence).toContain('3 commits');
  });

  it('counts one commit as one', () => {
    expect(rebaseSummary(plan({ rewriting: 1, behind: 1 }))).toContain('1 commit that main');
  });

  // The general sentence counted only ordinary commits, so a diverged branch
  // that also holds a merge was described as rewriting 3 while 4 were about
  // to go. Flattening belongs in the sentence, not only in the red list.
  it('counts the merge commits a diverged replay will discard', () => {
    const sentence = rebaseSummary(plan({ rewriting: 3, flattening: 2, behind: 1 }));

    expect(sentence).toContain('3 commits');
    expect(sentence).toContain('2 merge commits');
    expect(sentence).not.toContain('0 commit');
  });

  // Counted there, explained here. Both go on the same dialog, so a summary
  // that also said why the merges go printed one sentence twice — once under
  // the explanation and once in the red list directly below it.
  it('leaves the reason the merge commits go to the losses beside it', () => {
    const diverged = plan({ rewriting: 3, flattening: 2, behind: 1 });

    expect(rebaseSummary(diverged)).not.toContain('straight line');
    expect(rebaseLosses(diverged).join(' ')).toContain('straight line');
  });

  it('falls back to the command for an unknown outcome', () => {
    const sentence = rebaseSummary(
      plan({ outcome: 'squash' as RebasePlan['outcome'], command: 'git rebase --onto x' }),
    );

    expect(sentence).toContain('git rebase --onto x');
  });
});

describe('what the rebase confirmation says will be lost', () => {
  it('names the commits that stop being what the branch points at', () => {
    expect(rebaseLosses(plan({ rewriting: 3 }))).toEqual([
      'the 3 commits feature points at now — the rebase writes new ones in their place',
    ]);
  });

  // --no-rebase-merges replays a straight line, and nothing else on the dialog
  // would say that the merge commits are gone rather than recreated.
  it('names the merge commits a rebase flattens, where there are any', () => {
    const losses = rebaseLosses(plan({ rewriting: 3, flattening: 2 }));

    expect(losses).toHaveLength(2);
    expect(losses[1]).toContain('2 merge commits');
    expect(losses[1]).toContain('straight line');
  });

  it('says nothing about merge commits where there are none', () => {
    const losses = rebaseLosses(plan({ flattening: 0 }));

    expect(losses).toHaveLength(1);
    expect(losses[0]).toContain('3 commits');
  });

  // The other side of the same rule: naming "the 0 commits feature points at
  // now" over a branch that holds only merge commits would be a red panel
  // about nothing, beside the one thing that does disappear.
  it('names only the merge commits where nothing else is rewritten', () => {
    const losses = rebaseLosses(plan({ rewriting: 0, flattening: 2 }));

    expect(losses).toHaveLength(1);
    expect(losses[0]).toContain('2 merge commits');
  });

  // ConfirmDialog takes a non-empty tuple, and this is what keeps that true:
  // a replay always takes at least the commits it rewrites.
  it('always names at least one thing', () => {
    for (const rewriting of [1, 2, 50]) {
      expect(rebaseLosses(plan({ rewriting })).length).toBeGreaterThan(0);
    }
  });
});
