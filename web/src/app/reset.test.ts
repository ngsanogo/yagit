import { describe, expect, it } from 'vitest';

import type { ResetPlan } from '../api/types';
import { resetLosses, resetSummary } from './reset';

function plan(overrides: Partial<ResetPlan> = {}): ResetPlan {
  return {
    command: 'git reset --mixed abcdef0123456789',
    commit: 'abcdef0123456789',
    subject: 'the change',
    into: 'main',
    mode: 'mixed',
    dropping: 2,
    dirty_files: 0,
    ...overrides,
  };
}

describe('what the reset confirmation says will happen', () => {
  it('names a soft reset as moving HEAD only', () => {
    const sentence = resetSummary(
      plan({ mode: 'soft', command: 'git reset --soft abcdef0123456789' }),
    );
    expect(sentence).toContain('main');
    expect(sentence).toContain('abcdef0');
    expect(sentence).toContain('the change');
    expect(sentence).toContain('2 commits');
    expect(sentence).toContain('staged');
    expect(sentence).toContain('index and the work tree stay');
  });

  it('names a mixed reset as moving HEAD and the index', () => {
    const sentence = resetSummary(plan());
    expect(sentence).toContain('unstaged');
    expect(sentence).toContain('work tree stays');
  });

  it('names a hard reset as moving all three trees', () => {
    const sentence = resetSummary(
      plan({ mode: 'hard', command: 'git reset --hard abcdef0123456789', dirty_files: 3 }),
    );
    expect(sentence).toContain('leave the branch');
    expect(sentence).toContain('uncommitted change goes with them');
  });

  it('says a hard reset to HEAD with a dirty tree discards only the work', () => {
    const sentence = resetSummary(
      plan({
        mode: 'hard',
        dropping: 0,
        dirty_files: 4,
        command: 'git reset --hard abcdef0123456789',
      }),
    );
    expect(sentence).toContain('already points');
    expect(sentence).toContain('4 files');
    expect(sentence).toContain('discards');
  });

  it('falls back when the mode is one this page does not know', () => {
    const sentence = resetSummary(
      plan({ mode: 'keep' as ResetPlan['mode'], command: 'git reset --keep' }),
    );
    expect(sentence).toContain('Reload before resetting');
  });
});

describe('what a hard reset takes away', () => {
  it('names the commits and the dirty files', () => {
    const losses = resetLosses(
      plan({
        mode: 'hard',
        dropping: 2,
        dirty_files: 3,
        command: 'git reset --hard abcdef0123456789',
      }),
    );
    expect(losses).toEqual([
      '2 commits past abcdef0 (“the change”) — off main, reachable afterwards through the reflog',
      'uncommitted changes in 3 files',
    ]);
  });

  it('is undefined for soft and mixed', () => {
    expect(resetLosses(plan({ mode: 'soft' }))).toBeUndefined();
    expect(resetLosses(plan({ mode: 'mixed' }))).toBeUndefined();
  });

  it('is undefined for a clean hard reset to HEAD', () => {
    expect(resetLosses(plan({ mode: 'hard', dropping: 0, dirty_files: 0 }))).toBeUndefined();
  });
});
