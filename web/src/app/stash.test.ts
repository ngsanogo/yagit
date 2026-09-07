import { describe, expect, it } from 'vitest';

import type { StashApplyPlan, StashDropPlan, StashPushPlan } from '../api/types';
import {
  stashApplySummary,
  stashDropLosses,
  stashDropSummary,
  stashLabel,
  stashPushRefusal,
  stashPushSummary,
  stashRef,
} from './stash';

/**
 * What the stash dialogs say.
 *
 * The sentences are the subject, and two of them are worth pinning for the
 * same reason the daemon's refusals are: a stash push that leaves untracked
 * files behind, and an apply onto a work tree that already differs, are the
 * two moments where what the command does and what a reader assumes it does
 * come apart.
 */

function pushPlan(overrides: Partial<StashPushPlan> = {}): StashPushPlan {
  return { branch: 'main', include_untracked: false, tracked: 3, untracked: 0, ...overrides };
}

function applyPlan(overrides: Partial<StashApplyPlan> = {}): StashApplyPlan {
  return {
    command: "git stash apply 'stash@{0}'",
    index: 0,
    sha: 'a2801ba9c4d5e6f70819',
    message: 'half the parser',
    branch: 'main',
    mode: 'apply',
    files: 2,
    dirty_files: 0,
    ...overrides,
  };
}

function dropPlan(overrides: Partial<StashDropPlan> = {}): StashDropPlan {
  return {
    command: "git stash drop 'stash@{1}'",
    index: 1,
    sha: 'a2801ba9c4d5e6f70819',
    message: 'half the parser',
    branch: 'main',
    files: 2,
    ...overrides,
  };
}

describe('stashLabel', () => {
  it('names a stash by its message', () => {
    expect(stashLabel({ index: 0, message: 'half the parser' })).toBe('“half the parser”');
  });

  it('falls back to the position where there is no message', () => {
    // Rarer than it looks — git writes "WIP on main: …" for a stash made
    // without -m — and it still has to read as something rather than as an
    // empty pair of quotes.
    expect(stashLabel({ index: 2, message: '' })).toBe('stash@{2}');
  });
});

describe('stashRef', () => {
  it('writes a position the way git does', () => {
    expect(stashRef(0)).toBe('stash@{0}');
    expect(stashRef(11)).toBe('stash@{11}');
  });
});

describe('stashPushSummary', () => {
  it('counts what is saved', () => {
    expect(stashPushSummary(pushPlan())).toContain('3 files go into a stash filed under main');
    expect(stashPushSummary(pushPlan({ tracked: 1 }))).toContain('1 file goes into a stash');
  });

  it('says which files would be left where they are', () => {
    // The sentence that keeps the dialog honest: without the flag, git leaves
    // every untracked file exactly where it is.
    const summary = stashPushSummary(pushPlan({ tracked: 1, untracked: 2 }));
    expect(summary).toContain('1 file goes');
    expect(summary).toContain('2 untracked files stay where they are');
    expect(stashPushSummary(pushPlan({ tracked: 1, untracked: 1 }))).toContain(
      '1 untracked file stays where it is',
    );
  });

  it('counts untracked files in once they are included', () => {
    const summary = stashPushSummary(
      pushPlan({ tracked: 1, untracked: 2, include_untracked: true }),
    );
    expect(summary).toContain('3 files go');
    expect(summary).not.toContain('stay where');
  });

  it('names the commit rather than a branch on a detached HEAD', () => {
    // git records "(no branch)" here, and the daemon sends an empty string
    // rather than that phrase. Naming a branch nobody is on would be worse
    // than naming none.
    const summary = stashPushSummary(pushPlan({ branch: '' }));
    expect(summary).toContain('the commit HEAD is on');
    expect(summary).not.toContain('’s tip');
  });
});

describe('stashApplySummary', () => {
  it('says the entry stays for an apply', () => {
    expect(stashApplySummary(applyPlan())).toContain('stays in the stack');
  });

  it('says the entry goes for a pop', () => {
    const summary = stashApplySummary(applyPlan({ mode: 'pop' }));
    expect(summary).toContain('leaves the stack');
    expect(summary).not.toContain('stays in the stack');
  });

  it('says nothing has to be merged into a clean work tree', () => {
    expect(stashApplySummary(applyPlan())).toContain('nothing has to be merged');
  });

  it('names the work already in the way, and what git does about it', () => {
    // Not a refusal: applying onto work in progress is ordinary. It is just
    // the one path that can end in conflict markers rather than in files.
    const summary = stashApplySummary(applyPlan({ dirty_files: 4 }));
    expect(summary).toContain('4 files already differ here');
    expect(stashApplySummary(applyPlan({ dirty_files: 1 }))).toContain('1 file already differs');
    expect(summary).toContain('conflict markers');
  });
});

describe('stashDropSummary', () => {
  it('says the commit outlives the entry', () => {
    // "gone" would be a lie, and a warning that overstates is one people learn
    // to click through.
    expect(stashDropSummary(dropPlan())).toContain('git collects it in its own time');
  });
});

describe('stashDropLosses', () => {
  it('names the files and the object that holds them', () => {
    const [files, object] = stashDropLosses(dropPlan());
    expect(files).toBe('the changes it holds, in 2 files');
    expect(object).toContain('stash@{1}');
    // The short SHA is in the list because it is the thing to write down: an
    // unreachable commit is still reachable by name until git collects it.
    expect(object).toContain('a2801ba');
  });

  it('always names something', () => {
    // ConfirmDialog's destructive arm requires a non-empty list, and a stash
    // git would make holds at least one file — so unlike resetLosses this
    // never has to answer with nothing.
    expect(stashDropLosses(dropPlan({ files: 1 })).length).toBeGreaterThan(0);
  });
});

describe('stashPushRefusal', () => {
  it('refuses a work tree that matches HEAD', () => {
    expect(stashPushRefusal(0)).toContain('nothing to stash');
  });

  it('offers one that does not', () => {
    expect(stashPushRefusal(1)).toBeUndefined();
  });
});
