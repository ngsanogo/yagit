import { describe, expect, it } from 'vitest';

import {
  counted,
  formatRelativeTime,
  initialsFromName,
  pluralize,
  shortenSha,
  stableIndex,
} from './format';

describe('shortenSha', () => {
  it('returns the first seven characters, like git', () => {
    expect(shortenSha('60ae86f1e54f0cf269094a8b7b47b3bd517b9dda')).toBe('60ae86f');
  });

  it('never runs past the available length', () => {
    expect(shortenSha('abc')).toBe('abc');
  });
});

describe('formatRelativeTime', () => {
  const now = new Date('2026-03-10T12:00:00Z');

  const cases: Array<[string, string]> = [
    ['2026-03-10T11:59:30Z', 'just now'],
    ['2026-03-10T11:59:00Z', '1 minute ago'],
    ['2026-03-10T11:30:00Z', '30 minutes ago'],
    ['2026-03-10T11:00:00Z', '1 hour ago'],
    ['2026-03-09T12:00:00Z', '1 day ago'],
    ['2026-03-07T12:00:00Z', '3 days ago'],
  ];

  it.each(cases)('%s → %s', (value, expected) => {
    expect(formatRelativeTime(new Date(value), now)).toBe(expected);
  });

  it('switches to an absolute date past a week', () => {
    expect(formatRelativeTime(new Date('2026-01-05T12:00:00Z'), now)).toBe('5 Jan 2026');
  });

  // A skewed clock is a fact worth showing, not an error to paper over.
  it('shows a future date as an absolute one rather than a negative duration', () => {
    expect(formatRelativeTime(new Date('2026-03-11T12:00:00Z'), now)).toBe('11 Mar 2026');
  });
});

describe('pluralize', () => {
  it('leaves the singular alone', () => {
    expect(pluralize(1, 'commit')).toBe('1 commit');
  });

  it('pluralizes everything else, zero included', () => {
    expect(pluralize(0, 'commit')).toBe('0 commits');
    expect(pluralize(4, 'commit')).toBe('4 commits');
  });
});

describe('counted', () => {
  it('agrees the verb with the count', () => {
    // The reason it exists: concatenating pluralize with a fixed verb reads
    // "1 file differ", and a reader who notices that stops trusting the number
    // in front of it.
    expect(counted(1, 'file', 'differs', 'differ')).toBe('1 file differs');
    expect(counted(3, 'file', 'differs', 'differ')).toBe('3 files differ');
  });

  it('treats zero as plural, as English does', () => {
    expect(counted(0, 'file', 'stays', 'stay')).toBe('0 files stay');
  });

  it('takes both forms rather than appending an s', () => {
    // The verbs that turn up here do not take one.
    expect(counted(1, 'change', 'is queued', 'are queued')).toBe('1 change is queued');
    expect(counted(2, 'change', 'is queued', 'are queued')).toBe('2 changes are queued');
  });
});

describe('initialsFromName', () => {
  it('takes the first and the last initial', () => {
    expect(initialsFromName('Ada Lovelace')).toBe('AL');
  });

  it('makes do with one letter for a single-word name', () => {
    expect(initialsFromName('Ada')).toBe('A');
  });

  it('ignores extra whitespace', () => {
    expect(initialsFromName('  Grace   Brewster   Hopper  ')).toBe('GH');
  });

  it('returns a question mark rather than crashing on an empty name', () => {
    expect(initialsFromName('   ')).toBe('?');
  });
});

describe('stableIndex', () => {
  it('returns the same index for the same text every time', () => {
    expect(stableIndex('Ada Lovelace', 10)).toBe(stableIndex('Ada Lovelace', 10));
  });

  it('stays within the bounds asked for', () => {
    for (const name of ['Ada', 'Grace Hopper', 'Alan Turing', '', 'é🌳']) {
      const index = stableIndex(name, 10);
      expect(index).toBeGreaterThanOrEqual(0);
      expect(index).toBeLessThan(10);
    }
  });

  // A name can start outside the Basic Multilingual Plane, and charAt(0) cuts
  // such a character in half: the result was a lone surrogate, which is not
  // well-formed text and renders as a replacement character.
  it('keeps an astral first character whole', () => {
    for (const [name, expected] of [
      ['👩 Smith', '👩S'],
      ['𝒜da Lovelace', '𝒜L'],
      ['𝔊auss', '𝔊'],
    ] as const) {
      const initials = initialsFromName(name);
      expect(initials).toBe(expected);
      expect(initials.isWellFormed()).toBe(true);
    }
  });
});
