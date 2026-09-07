import { describe, expect, it } from 'vitest';

import type { DiscoverSkipped } from '../api/types';
import { boundedScanDepth, scanDepthFromInput, scanSkipExplanation } from './discover';

/**
 * The sentence under an empty scan is the whole point of counting anything, so
 * it is checked here rather than looked at.
 */

const NOTHING_SKIPPED: DiscoverSkipped = {
  unreadable: 0,
  too_deep: 0,
  ignored_name: 0,
  dotted: 0,
  worktrees: 0,
  submodules: 0,
  not_a_repository: 0,
};

/** A scan that ran below its ceiling, unless a test says otherwise. */
function scan(counts: Partial<DiscoverSkipped>, depth = 4, limit = 8) {
  return { skipped: { ...NOTHING_SKIPPED, ...counts }, depth, depth_limit: limit };
}

describe('scanSkipExplanation', () => {
  it('adds nothing when the scan skipped nothing', () => {
    expect(scanSkipExplanation(scan({}))).toBeUndefined();
  });

  it('names the one reason with the highest count and no other', () => {
    const sentence = scanSkipExplanation(scan({ too_deep: 4, dotted: 11 }));

    expect(sentence).toContain('11 directories');
    expect(sentence).toContain('dot');
    expect(sentence).not.toContain('4');
  });

  it('agrees with a count of one', () => {
    expect(scanSkipExplanation(scan({ worktrees: 1 }))).toBe(
      '1 linked worktree was skipped. Turn on “Include linked worktrees” to list it.',
    );
  });

  it('breaks a tie towards the reason the screen can fix', () => {
    expect(scanSkipExplanation(scan({ too_deep: 3, unreadable: 3 }))).toContain('scan depth');
  });

  it('names the switch that would have listed what it skipped', () => {
    expect(scanSkipExplanation(scan({ submodules: 2 }))).toContain('Include submodules');
  });

  it('offers the depth while there is depth left to offer', () => {
    expect(scanSkipExplanation(scan({ too_deep: 2 }, 4, 8))).toContain('Raise the depth');
  });

  // The state the whole feature exists to end: a scan at the ceiling has
  // nothing left to raise, and pointing at the control that prints that
  // ceiling in its own label is no move at all.
  it('offers the scan directory instead once the depth is at its ceiling', () => {
    const sentence = scanSkipExplanation(scan({ too_deep: 2 }, 8, 8));

    expect(sentence).toContain('Point “Scan in” further down');
    expect(sentence).not.toContain('Raise the depth');
  });
});

describe('scanDepthFromInput', () => {
  it('leaves an emptied box empty rather than refilling it', () => {
    expect(scanDepthFromInput('')).toBe('');
  });

  it('reads the number that was typed', () => {
    expect(scanDepthFromInput('6')).toBe(6);
  });
});

describe('boundedScanDepth', () => {
  it('clamps to the ceiling the daemon reported, which it caps at silently', () => {
    expect(boundedScanDepth(40, 8)).toBe(8);
  });

  // The ceiling arrives with a scan, so the keystroke that started one is
  // typed while there is none. It has to be brought back inside the ceiling
  // when the answer carries it, not left standing above it forever.
  it('passes a value through until a scan has reported a ceiling', () => {
    expect(boundedScanDepth(40, undefined)).toBe(40);
    expect(boundedScanDepth(40, 8)).toBe(8);
  });

  it('refuses a depth below one, which scans nothing at all', () => {
    expect(boundedScanDepth(0, 8)).toBe(1);
    expect(boundedScanDepth(-3, 8)).toBe(1);
  });
});
