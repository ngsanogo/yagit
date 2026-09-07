import { describe, expect, it } from 'vitest';

import { signatureNote } from './signature';

describe("git's signature verdict, as a badge", () => {
  it('says nothing about an unsigned commit, which is most of them', () => {
    expect(signatureNote('N')).toBeUndefined();
  });

  it('says nothing when the daemon sent no verdict at all', () => {
    // An older daemon, not a less trustworthy repository.
    expect(signatureNote('')).toBeUndefined();
  });

  it('separates a verified signature from a forged one', () => {
    expect(signatureNote('G')).toEqual({ label: 'signature verified', tone: 'success' });
    expect(signatureNote('B')?.tone).toBe('danger');
  });

  it('does not call an unverifiable signature good or bad', () => {
    // A good signature from a key this machine does not trust is not a
    // forgery and not a clean verification either. Collapsing the two into
    // one colour is how a badge stops meaning anything.
    for (const verdict of ['U', 'X', 'Y']) {
      expect(signatureNote(verdict)?.tone).toBe('warning');
    }
    expect(signatureNote('R')?.tone).toBe('danger');
    expect(signatureNote('E')?.tone).toBe('info');
  });

  it('says nothing about a letter git has not defined', () => {
    expect(signatureNote('?')).toBeUndefined();
  });
});
