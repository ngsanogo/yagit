import { describe, expect, it } from 'vitest';

import type { CompareResult } from '../api/types';
import { compareSummary } from './compare';

function result(overrides: Partial<CompareResult> = {}): CompareResult {
  return {
    head: 'topic',
    against: 'refs/heads/main',
    against_short: 'main',
    ahead: 0,
    behind: 0,
    head_sha: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    against_sha: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
    ...overrides,
  };
}

describe('compareSummary', () => {
  it('names two tips that match', () => {
    expect(compareSummary(result())).toBe('topic and main point at the same commit.');
  });

  it('names only ahead', () => {
    expect(compareSummary(result({ ahead: 2 }))).toBe('topic is 2 commits ahead of main.');
  });

  it('names only behind', () => {
    expect(compareSummary(result({ behind: 1 }))).toBe('topic is 1 commit behind main.');
  });

  it('names both sides', () => {
    expect(compareSummary(result({ ahead: 2, behind: 1 }))).toBe(
      'topic is 2 commits ahead of main and 1 commit behind it.',
    );
  });

  it('says HEAD when detached', () => {
    expect(compareSummary(result({ head: '', ahead: 1 }))).toBe('HEAD is 1 commit ahead of main.');
  });
});
