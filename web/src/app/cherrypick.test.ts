import { describe, expect, it } from 'vitest';

import type { CherryPickPlan } from '../api/types';
import { cherryPickSummary } from './cherrypick';

function plan(overrides: Partial<CherryPickPlan> = {}): CherryPickPlan {
  return {
    command: 'git cherry-pick --no-edit --no-ff -- abcdef0123456789',
    commit: 'abcdef0123456789abcdef0123456789abcdef01',
    subject: 'side only',
    into: 'main',
    outcome: 'cherry-pick',
    ...overrides,
  };
}

describe('what the cherry-pick confirmation says will happen', () => {
  it('says an apply is a new commit under hooks', () => {
    const sentence = cherryPickSummary(plan());

    expect(sentence).toContain('abcdef0');
    expect(sentence).toContain('side only');
    expect(sentence).toContain('new commit');
    expect(sentence).toContain('hooks and signing');
  });

  it('says a fast-forward commits nothing', () => {
    const sentence = cherryPickSummary(
      plan({
        outcome: 'fast-forward',
        command: 'git cherry-pick --no-edit --ff -- abcdef0123456789',
      }),
    );

    expect(sentence).toContain('moves onto that commit');
    expect(sentence).toContain('Nothing is committed');
  });

  it('says up-to-date changes nothing', () => {
    const sentence = cherryPickSummary(plan({ outcome: 'up-to-date', command: '' }));

    expect(sentence).toContain('already contains');
    expect(sentence).toContain('changes nothing');
  });

  it('names an unknown outcome rather than going silent', () => {
    const sentence = cherryPickSummary(
      plan({ outcome: 'something-new' as CherryPickPlan['outcome'], command: 'git cherry-pick' }),
    );

    expect(sentence).toContain('Reload before cherry-picking');
  });
});
