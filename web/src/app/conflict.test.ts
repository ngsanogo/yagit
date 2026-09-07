import { describe, expect, it } from 'vitest';

import type { Operation } from '../api/types';
import {
  applyChoice,
  conflictSideNotes,
  findConflicts,
  hasConflictMarkers,
  layOutConflicts,
} from './conflict';

/**
 * The markers are the only thing that makes a conflicted file different from
 * any other, so misreading one is the whole failure this module can have —
 * either by missing a region the user then commits, or by cutting an ordinary
 * document in half at a line that only looks like a marker.
 */

/** A file with one ordinary conflict in the middle of it. */
const ONE = ['one', '<<<<<<< HEAD', 'MAIN', '=======', 'SIDE', '>>>>>>> feature', 'three'].join(
  '\n',
);

describe('findConflicts', () => {
  it('finds nothing in a file with no markers', () => {
    expect(findConflicts('one\ntwo\nthree\n')).toEqual([]);
  });

  it('reads both sides and both labels', () => {
    const [region] = findConflicts(ONE);

    expect(region).toBeDefined();
    expect(region?.ours).toEqual(['MAIN']);
    expect(region?.theirs).toEqual(['SIDE']);
    expect(region?.ourLabel).toBe('HEAD');
    expect(region?.theirLabel).toBe('feature');
    expect(region?.start).toBe(1);
    expect(region?.end).toBe(5);
  });

  it('reads the base of a diff3 conflict', () => {
    const text = [
      '<<<<<<< HEAD',
      'MAIN',
      '||||||| 8f3a1c2',
      'BASE',
      '=======',
      'SIDE',
      '>>>>>>> feature',
    ].join('\n');

    const [region] = findConflicts(text);
    expect(region?.base).toEqual(['BASE']);
    expect(region?.ours).toEqual(['MAIN']);
    expect(region?.theirs).toEqual(['SIDE']);
  });

  it('leaves the base absent when git wrote none', () => {
    // Absent and empty are different answers: no base means this repository
    // does not use a conflict style that shows one.
    expect(findConflicts(ONE)[0]?.base).toBeUndefined();
  });

  it('finds every region in a file with several', () => {
    const text = [ONE, ONE].join('\n');
    expect(findConflicts(text)).toHaveLength(2);
  });

  it('keeps empty sides, which are a real resolution', () => {
    // One side added lines and the other added nothing. Taking "theirs" here
    // deletes the lines, which is exactly what the user would mean.
    const text = ['<<<<<<< HEAD', 'MAIN', '=======', '>>>>>>> feature'].join('\n');
    const [region] = findConflicts(text);

    expect(region?.ours).toEqual(['MAIN']);
    expect(region?.theirs).toEqual([]);
  });

  it('reads a marker with no label at all', () => {
    const text = ['<<<<<<<', 'MAIN', '=======', 'SIDE', '>>>>>>>'].join('\n');
    const [region] = findConflicts(text);

    expect(region?.ourLabel).toBe('');
    expect(region?.theirs).toEqual(['SIDE']);
  });

  it('passes over a region that never closes', () => {
    // Half a region has no side to take. The text is still shown and still
    // editable, which is the honest answer to not knowing what it is.
    expect(findConflicts('<<<<<<< HEAD\nMAIN\n=======\nSIDE\n')).toEqual([]);
  });

  it('passes over a closing marker that comes before the separator', () => {
    expect(findConflicts('<<<<<<< HEAD\nMAIN\n>>>>>>> feature\n')).toEqual([]);
  });

  it('does not read a Markdown heading as a separator', () => {
    // Seven '=' under a line of text is how Markdown underlines a title. A
    // separator is only ever looked for inside an open region, which is what
    // keeps this from cutting the document in half.
    const text = 'Title\n=======\n\nSome prose.\n';
    expect(findConflicts(text)).toEqual([]);
    expect(hasConflictMarkers(text)).toBe(false);
  });

  it('does not read a longer run of characters as a marker', () => {
    // git writes exactly seven. A row of angle brackets in ASCII art is not a
    // conflict.
    expect(findConflicts('<<<<<<<<<<\nMAIN\n=======\nSIDE\n>>>>>>>>>>\n')).toEqual([]);
  });
});

describe('applyChoice', () => {
  it('keeps our side and removes every marker', () => {
    expect(applyChoice(ONE, findConflicts(ONE)[0]!, 'ours')).toBe('one\nMAIN\nthree');
  });

  it('keeps their side', () => {
    expect(applyChoice(ONE, findConflicts(ONE)[0]!, 'theirs')).toBe('one\nSIDE\nthree');
  });

  it('keeps both, ours first', () => {
    // What somebody resolving two independent additions means. A starting
    // point they can edit, not an answer.
    expect(applyChoice(ONE, findConflicts(ONE)[0]!, 'both')).toBe('one\nMAIN\nSIDE\nthree');
  });

  it('never keeps the base', () => {
    // The base is what the two sides diverged from. Keeping it would resolve
    // the conflict by undoing both of them.
    const text = [
      '<<<<<<< HEAD',
      'MAIN',
      '||||||| 8f3a1c2',
      'BASE',
      '=======',
      'SIDE',
      '>>>>>>> feature',
    ].join('\n');

    expect(applyChoice(text, findConflicts(text)[0]!, 'both')).toBe('MAIN\nSIDE');
  });

  it('leaves the rest of the file exactly where it was', () => {
    const text = ['before', ONE, 'after'].join('\n');
    const resolved = applyChoice(text, findConflicts(text)[0]!, 'ours');

    expect(resolved).toBe(['before', 'one', 'MAIN', 'three', 'after'].join('\n'));
  });

  it('resolves the regions of a multi-conflict file one at a time', () => {
    const text = [ONE, ONE].join('\n');

    // The later region first, so the earlier one's line numbers still hold.
    // Resolving the earlier one moves everything below it, which is why the
    // caller re-reads between choices.
    const once = applyChoice(text, findConflicts(text)[1]!, 'theirs');
    expect(findConflicts(once)).toHaveLength(1);

    const twice = applyChoice(once, findConflicts(once)[0]!, 'ours');
    expect(findConflicts(twice)).toEqual([]);
    expect(hasConflictMarkers(twice)).toBe(false);
  });
});

describe('hasConflictMarkers', () => {
  it('is true for a file git left mid-merge', () => {
    expect(hasConflictMarkers(ONE)).toBe(true);
  });

  it('is true for half a region somebody deleted by hand', () => {
    // findConflicts offers no button for this, and it must still not be
    // staged: the marker would be committed.
    expect(hasConflictMarkers('one\n<<<<<<< HEAD\nMAIN\nthree\n')).toBe(true);
  });

  it('is false for an ordinary file', () => {
    expect(hasConflictMarkers('one\ntwo\nthree\n')).toBe(false);
  });
});

describe('what ours and theirs mean', () => {
  it('names the branch you are on during a merge', () => {
    const notes = conflictSideNotes('merge');

    expect(notes.ours).toContain('the branch you are on');
    expect(notes.theirs).toContain('the branch coming in');
  });

  // git swaps the two during a rebase. A note that kept merge language would
  // tell somebody "Keep ours" is their work, then throw that work away.
  it('names the upstream as ours during a rebase', () => {
    const notes = conflictSideNotes('rebase');

    expect(notes.ours).toContain('replayed onto');
    expect(notes.theirs).toContain('commit being replayed');
    expect(notes.ours).not.toContain('the branch you are on');
  });

  it('names the incoming commit during a cherry-pick', () => {
    expect(conflictSideNotes('cherry-pick').theirs).toContain('commit being picked');
  });

  // The one that was backwards. git reverts by merging the reverted commit's
  // PARENT over HEAD, so their side is the file WITHOUT that commit — the
  // opposite of its content. A note reading "the commit being reverted" makes
  // Keep theirs look like the way to keep the change it actually discards.
  it('names their side as the file with the commit taken out during a revert', () => {
    const notes = conflictSideNotes('revert');

    expect(notes.theirs).toContain('before the commit being reverted');
    expect(notes.ours).toContain('the branch you are on');
  });

  it('applies the patch during an am', () => {
    expect(conflictSideNotes('am').theirs).toContain('patch being applied');
  });

  // No operation is not a merge. A stash pop that conflicted records nothing,
  // and so does a file somebody left markers in: there is no branch coming in
  // to name, so neither is told there is one.
  it('claims no branch when git records no operation', () => {
    const notes = conflictSideNotes('');

    expect(notes.theirs).toContain('the version coming in');
    expect(notes.theirs).not.toContain('branch');
  });

  // The daemon can be newer than the page. An operation this build has never
  // heard of still has two buttons that need words under them.
  it('falls back to the notes that claim least for an unknown operation', () => {
    const notes = conflictSideNotes('bisect-run' as Operation);

    expect(notes).toEqual(conflictSideNotes(''));
  });
});

describe('layOutConflicts', () => {
  /** Twelve numbered lines with one conflict at lines 6-10. */
  const spaced = [
    'a', // 1
    'b', // 2
    'c', // 3
    'd', // 4
    'e', // 5
    '<<<<<<< HEAD', // 6
    'MAIN', // 7
    '=======', // 8
    'SIDE', // 9
    '>>>>>>> f', // 10
    'x', // 11
    'y', // 12
    'z', // 13
    'w', // 14
  ].join('\n');

  it('shows the lines around a region, with their real numbers', () => {
    const { blocks } = layOutConflicts(spaced, 3);

    expect(blocks).toHaveLength(1);
    expect(blocks[0]?.before.map((line) => line.number)).toEqual([3, 4, 5]);
    expect(blocks[0]?.after.map((line) => line.number)).toEqual([11, 12, 13]);
    expect(blocks[0]?.before.map((line) => line.text)).toEqual(['c', 'd', 'e']);
  });

  it('counts the lines it leaves out on each side', () => {
    const { blocks, skippedAfter } = layOutConflicts(spaced, 3);

    // Lines 1 and 2 above, line 14 below. A jump with nothing said about it
    // reads as a file that starts and ends where the window does.
    expect(blocks[0]?.skippedBefore).toBe(2);
    expect(skippedAfter).toBe(1);
  });

  it('draws no line twice when two regions are close together', () => {
    // Two conflicts with one line between them: the windows overlap, and
    // drawing both in full would print that line under two headings.
    const text = [
      '<<<<<<< HEAD',
      'ONE',
      '=======',
      'one',
      '>>>>>>> f',
      'between',
      '<<<<<<< HEAD',
      'TWO',
      '=======',
      'two',
      '>>>>>>> f',
    ].join('\n');

    const { blocks } = layOutConflicts(text, 3);
    expect(blocks).toHaveLength(2);

    const drawn = blocks.flatMap((block) => [...block.before, ...block.after]).map((l) => l.number);
    expect(drawn).toEqual([...new Set(drawn)]);
    expect(blocks[1]?.skippedBefore).toBe(0);
  });

  it('has no layout at all for a file with no conflicts', () => {
    expect(layOutConflicts('one\ntwo\n', 3)).toEqual({ blocks: [], skippedAfter: 0 });
  });

  // `"a\n".split('\n')` ends in an empty string, and that is not a line — it
  // is what follows the last one. Counting it draws a numbered blank row under
  // the file's real end, or reports an unchanged line below the last block
  // where the file has already finished.
  it('does not invent a line from the trailing newline', () => {
    const text = ['<<<<<<< HEAD', 'MAIN', '=======', 'SIDE', '>>>>>>> f', ''].join('\n');

    const { blocks, skippedAfter } = layOutConflicts(text, 3);

    expect(blocks[0]?.after).toEqual([]);
    expect(skippedAfter).toBe(0);
  });

  it('counts a real last line that has no newline after it', () => {
    // The other half of the same rule: a file not ending in a newline has a
    // last line, and leaving it out would under-count by one instead.
    const text = ['<<<<<<< HEAD', 'MAIN', '=======', 'SIDE', '>>>>>>> f', 'tail'].join('\n');

    const { blocks, skippedAfter } = layOutConflicts(text, 0);

    expect(blocks[0]?.after).toEqual([]);
    expect(skippedAfter).toBe(1);
  });

  it('never counts a negative gap', () => {
    // A region at the very top: the window would reach above line 1.
    const text = ['<<<<<<< HEAD', 'ONE', '=======', 'one', '>>>>>>> f'].join('\n');
    const { blocks, skippedAfter } = layOutConflicts(text, 3);

    expect(blocks[0]?.skippedBefore).toBe(0);
    expect(blocks[0]?.before).toEqual([]);
    expect(skippedAfter).toBe(0);
  });
});
