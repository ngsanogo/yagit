import { describe, expect, it } from 'vitest';

import { suggestedCloneName } from './CloneRepository';

describe('suggestedCloneName', () => {
  it('takes the last path segment of an https URL', () => {
    expect(suggestedCloneName('https://example.com/ada/yagit.git')).toBe('yagit');
  });

  it('handles the scp-like form', () => {
    expect(suggestedCloneName('git@github.com:ada/yagit.git')).toBe('yagit');
  });

  it('takes the last path segment of a local path', () => {
    expect(suggestedCloneName('/srv/mirrors/yagit.git')).toBe('yagit');
  });

  it('returns empty when there is nothing useful', () => {
    expect(suggestedCloneName('')).toBe('');
    // A bare host names no repository, and `example.com` is not a destination
    // anybody asked for.
    expect(suggestedCloneName('https://example.com')).toBe('');
    expect(suggestedCloneName('example.com')).toBe('');
  });
});
