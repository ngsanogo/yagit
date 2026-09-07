import { describe, expect, it } from 'vitest';

import type { Commit, InteractiveRebasePlan, RebaseInstruction, RebaseStep } from '../api/types';
import {
  firstRewritten,
  initialSteps,
  instructionLabel,
  moveStep,
  planLosses,
  planRefusal,
  planReport,
  planSummary,
  withInstruction,
} from './interactiveRebase';

function commitNamed(letter: string, subject: string): Commit {
  return { sha: letter.repeat(40), parents: [], author: 'Ada', date: '', subject, refs: [] };
}

/** Three commits to plan over, oldest first, as the daemon answers them. */
const ONE = commitNamed('a', 'one');
const TWO = commitNamed('b', 'two');
const THREE = commitNamed('c', 'three');
const COMMITS: Commit[] = [ONE, TWO, THREE];

const PLAN: InteractiveRebasePlan = {
  command: 'git rebase --interactive -- 0'.padEnd(30, '0'),
  base: '0'.repeat(40),
  subject: 'base',
  from: 'main',
  commits: COMMITS,
};

// One row per commit, in the branch's own order. A row left unnamed is kept,
// which is what a plan starts as.
function steps(...instructions: RebaseInstruction[]): RebaseStep[] {
  return COMMITS.map((commit, index) => ({
    commit: commit.sha,
    instruction: instructions[index] ?? 'pick',
  }));
}

describe('the plan a dialog opens on', () => {
  it('is the history exactly as it stands', () => {
    expect(initialSteps(COMMITS)).toEqual(steps('pick', 'pick', 'pick'));
  });
});

describe('editing a plan', () => {
  it('moves a row without disturbing the others', () => {
    const moved = moveStep(initialSteps(COMMITS), 2, 0);
    expect(moved.map((step) => step.commit)).toEqual([THREE.sha, ONE.sha, TWO.sha]);
  });

  it('leaves the plan alone when the row cannot move', () => {
    const plan = initialSteps(COMMITS);
    // The buttons at the ends are disabled; a click that lands anyway between
    // a re-render is not worth losing a plan to.
    expect(moveStep(plan, 0, -1)).toEqual(plan);
    expect(moveStep(plan, 5, 0)).toEqual(plan);
  });

  it('replaces one row’s instruction and copies the list', () => {
    const before = initialSteps(COMMITS);
    const after = withInstruction(before, 1, 'drop');

    expect(after[1]?.instruction).toBe('drop');
    expect(after[0]).toEqual(before[0]);
    expect(before[1]?.instruction).toBe('pick');
  });
});

// git fast-forwards over the bottom of a plan for as long as it matches the
// history already there, and those commits keep their hashes. This is the
// reading that lets the dialog say so — and the reason no --no-ff is passed.
describe('which commits get written again', () => {
  it('is none when the plan is the history as it stands', () => {
    expect(firstRewritten(steps('pick', 'pick', 'pick'), COMMITS)).toBe(3);
  });

  it('starts at the first row that differs', () => {
    expect(firstRewritten(steps('pick', 'pick', 'drop'), COMMITS)).toBe(2);
    expect(firstRewritten(steps('pick', 'edit', 'pick'), COMMITS)).toBe(1);
  });

  it('starts at the first row that moved', () => {
    const reordered = moveStep(initialSteps(COMMITS), 2, 0);
    expect(firstRewritten(reordered, COMMITS)).toBe(0);
  });
});

describe('the plans the dialog will not send', () => {
  it('refuses a combine with nothing above it', () => {
    const refusal = planRefusal(steps('fixup', 'pick', 'pick'), COMMITS);
    expect(refusal).toContain('nothing above it');
    expect(refusal).toContain('aaaaaaa');
  });

  it('refuses a combine whose only company above it is a drop', () => {
    expect(planRefusal(steps('drop', 'fixup -C', 'pick'), COMMITS)).toContain('nothing above it');
  });

  it('refuses a plan that changes nothing', () => {
    expect(planRefusal(steps('pick', 'pick', 'pick'), COMMITS)).toContain('change nothing');
  });

  it('refuses an empty plan', () => {
    expect(planRefusal([], COMMITS)).toContain('at least one commit');
  });

  it('accepts dropping every commit', () => {
    // The branch ends at the base. git carries it out without complaint, and
    // `drop` on every row is the one plan that says so exactly.
    expect(planRefusal(steps('drop', 'drop', 'drop'), COMMITS)).toBeUndefined();
  });

  it('accepts a rearrangement', () => {
    expect(planRefusal(moveStep(initialSteps(COMMITS), 2, 0), COMMITS)).toBeUndefined();
  });
});

describe('what the confirmation says will happen', () => {
  it('counts what survives, what goes and what is written again', () => {
    const sentence = planSummary(steps('pick', 'fixup', 'drop'), COMMITS, PLAN);

    expect(sentence).toContain('main keeps 1 commit of 3');
    expect(sentence).toContain('base');
    expect(sentence).toContain('1 commit dropped');
    expect(sentence).toContain('folded into the one above');
    expect(sentence).toContain('written again under new hashes');
  });

  it('says which commits keep their hashes', () => {
    const sentence = planSummary(steps('pick', 'pick', 'drop'), COMMITS, PLAN);
    expect(sentence).toContain('1 commit written again');
    expect(sentence).toContain('2 commits below them keep theirs');
  });

  it('says that git will stop', () => {
    const sentence = planSummary(steps('pick', 'edit', 'pick'), COMMITS, PLAN);
    expect(sentence).toContain('git stops once');
  });
});

describe('what a plan takes away', () => {
  it('names dropped changes and rewritten hashes apart', () => {
    const losses = planLosses(steps('pick', 'pick', 'drop'), COMMITS);

    expect(losses).toBeDefined();
    expect(losses?.join(' ')).toContain('dropped commit');
    expect(losses?.join(' ')).toContain('ccccccc');
    expect(losses?.join(' ')).toContain('reflog');
  });

  it('names a discarded message where a fixup discards one', () => {
    const losses = planLosses(steps('pick', 'fixup', 'pick'), COMMITS);
    expect(losses?.join(' ')).toContain('the message of 1 commit');
  });

  it('names nothing for a plan that changes nothing', () => {
    expect(planLosses(steps('pick', 'pick', 'pick'), COMMITS)).toBeUndefined();
  });
});

// A plan holding an `edit` does not finish: git stops in the middle of it
// having exited zero. A toast reporting a rewrite over a repository waiting at
// the second of three commits would be the wrong half of what happened.
describe('what the toast reports', () => {
  it('names the rewrite when the plan finished', () => {
    expect(planReport(steps('pick', 'pick', 'drop'), 'main', false)).toBe(
      'Rewrote 3 commits on main',
    );
  });

  it('names where it stopped when it stopped', () => {
    expect(planReport(steps('pick', 'edit', 'pick'), 'main', true, 2, 3)).toContain(
      'Stopped at 2 of 3 on main',
    );
  });

  it('still says it stopped when the daemon could not say where', () => {
    expect(planReport(steps('pick', 'edit', 'pick'), 'main', true)).toContain('Stopped part way');
  });
});

describe('the words on a row', () => {
  it('says what happens rather than which flag runs', () => {
    expect(instructionLabel('fixup -C')).toBe('Combine, keep this message');
    expect(instructionLabel('pick')).toBe('Keep');
  });

  it('names a verb it does not know rather than nothing at all', () => {
    // A daemon newer than this page. git's own word is more useful than blank.
    expect(instructionLabel('reword' as RebaseInstruction)).toBe('reword');
  });
});
