import { describe, expect, it } from 'vitest';

import type { RevertPlan } from '../api/types';
import { revertSummary } from './revert';

function plan(overrides: Partial<RevertPlan> = {}): RevertPlan {
  return {
    command: 'git revert --no-edit --no-reference -- abcdef0123456789',
    commit: 'abcdef0123456789',
    subject: 'the change',
    into: 'main',
    outcome: 'revert',
    ...overrides,
  };
}

describe('what the revert confirmation says will happen', () => {
  it('names the commit and that a new one is recorded', () => {
    const sentence = revertSummary(plan());
    expect(sentence).toContain('abcdef0');
    expect(sentence).toContain('the change');
    expect(sentence).toContain('main');
    expect(sentence).toContain('new commit');
    expect(sentence).toContain('hooks and signing');
  });

  it('falls back when the outcome is one this page does not know', () => {
    const sentence = revertSummary(
      plan({ outcome: 'something-new' as RevertPlan['outcome'], command: 'git revert' }),
    );
    expect(sentence).toContain('Reload before reverting');
  });
});
