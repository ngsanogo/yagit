import { describe, expect, it } from 'vitest';

import { commitMenuItems } from './CommitDetails';

/**
 * What the commit panel keeps behind its menu, and what it refuses.
 *
 * Asserted here because a closed menu has built nothing: the markup of a shut
 * Menu holds no items to read, so the function that describes them is the only
 * place the two operations that move a branch can be pinned. Three things are
 * worth pinning — the order they are read in, which of them is drawn as
 * destructive, and that an operation whose plan is already being read cannot
 * be asked for a second time. The click itself belongs to the end-to-end
 * suite; this is what the menu says before anybody clicks.
 */

/** What the repository is in the middle of, or refuses the operation for. */
type Conditions = {
  resetting?: boolean;
  resetRefusal?: string;
  rewriting?: boolean;
  rewriteRefusal?: string;
};

function items(conditions: Conditions = {}) {
  return commitMenuItems({
    onReset: () => undefined,
    resetting: conditions.resetting ?? false,
    resetRefusal: conditions.resetRefusal,
    onRewrite: () => undefined,
    rewriting: conditions.rewriting ?? false,
    rewriteRefusal: conditions.rewriteRefusal,
  });
}

describe('commitMenuItems', () => {
  it('reads from the reversible to the ruinous', () => {
    expect(items().map((item) => item.label)).toEqual([
      'Reset the current branch to this…',
      'Rewrite the commits after this…',
    ]);
  });

  it('reddens the rewrite and not the reset', () => {
    const [reset, rewrite] = items();

    // A rewrite always writes the commits after this one again under new
    // hashes. A reset is soft, mixed or hard, and which of the three it is
    // gets chosen in the dialog the item opens.
    expect(reset?.danger).toBeUndefined();
    expect(rewrite?.danger).toBe(true);
  });

  it('keeps a refused operation on the menu, with its reason', () => {
    const [reset] = items({ resetRefusal: 'HEAD is detached, so there is no branch to reset' });

    expect(reset?.disabled).toBe(true);
    expect(reset?.reason).toBe('HEAD is detached, so there is no branch to reset');
  });

  it('refuses an operation whose plan is already being read', () => {
    const [reset, rewrite] = items({ resetting: true, rewriting: true });

    expect(reset?.reason).toBe('Reading what this reset would do');
    expect(rewrite?.reason).toBe('Reading what this rewrite would do');
  });

  it('says why the repository refuses before it says what it is doing', () => {
    const [reset] = items({ resetting: true, resetRefusal: 'this repository has no work tree' });

    expect(reset?.reason).toBe('this repository has no work tree');
  });

  it('builds no menu at all where neither operation exists', () => {
    const none = commitMenuItems({
      onReset: undefined,
      resetting: false,
      resetRefusal: undefined,
      onRewrite: undefined,
      rewriting: false,
      rewriteRefusal: undefined,
    });

    expect(none).toEqual([]);
  });
});
