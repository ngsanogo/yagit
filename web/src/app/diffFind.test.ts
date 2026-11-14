import { describe, expect, it } from 'vitest';

import type { DiffHunk, DiffLine, FileDiff } from '../api/types';
import { findInDiff, findInPatch, findSpans, matchesByLine, stepMatch } from './diffFind';

function line(kind: DiffLine['kind'], text: string, index: number): DiffLine {
  return { kind, text, index, old_line: 0, new_line: 0, no_newline: false };
}

function hunk(lines: DiffLine[]): DiffHunk {
  return {
    old_start: 1,
    old_lines: lines.length,
    new_start: 1,
    new_lines: lines.length,
    heading: '',
    lines,
  };
}

function diff(hunks: DiffHunk[], path = 'a.txt'): FileDiff {
  return { id: 'fp', path, binary: false, added: false, removed: false, hunks };
}

describe('findInDiff', () => {
  it('finds nothing for an empty query', () => {
    const file = diff([hunk([line('added', 'hello world', 0)])]);
    expect(findInDiff(file, '')).toEqual([]);
  });

  it('keeps leading spaces, which is how an indentation is found', () => {
    const file = diff([hunk([line('added', '    return x;', 0), line('added', 'return y;', 1)])]);
    expect(findInDiff(file, '  return')).toEqual([{ line: 0, start: 2, length: 8, path: 'a.txt' }]);
  });

  it('matches punctuation literally', () => {
    const file = diff([hunk([line('added', 'fix(api): a(b', 0)])]);
    expect(findInDiff(file, 'a(b')).toEqual([{ line: 0, start: 10, length: 3, path: 'a.txt' }]);
  });

  it('paints offsets in the text as drawn, where lower-casing changes length', () => {
    // "İ" lower-cases to two code units; an offset found in a lowered copy
    // would land one character early on everything after it.
    const file = diff([hunk([line('added', 'İstanbul colour', 0)])]);
    expect(findInDiff(file, 'colour')).toEqual([{ line: 0, start: 9, length: 6, path: 'a.txt' }]);
  });

  it('matches literally and case-insensitively', () => {
    const file = diff([
      hunk([line('context', 'const Colour = 1;', 0), line('added', 'const colour = 2;', 1)]),
    ]);
    expect(findInDiff(file, 'COLOUR')).toEqual([
      { line: 0, start: 6, length: 6, path: 'a.txt' },
      { line: 1, start: 6, length: 6, path: 'a.txt' },
    ]);
  });

  it('finds every occurrence on one line', () => {
    const file = diff([hunk([line('added', 'foo foo foo', 0)])]);
    expect(findInDiff(file, 'foo')).toEqual([
      { line: 0, start: 0, length: 3, path: 'a.txt' },
      { line: 0, start: 4, length: 3, path: 'a.txt' },
      { line: 0, start: 8, length: 3, path: 'a.txt' },
    ]);
  });

  it('skips binary files', () => {
    const file: FileDiff = {
      id: 'fp',
      path: 'a.png',
      binary: true,
      added: false,
      removed: false,
      hunks: [],
    };
    expect(findInDiff(file, 'png')).toEqual([]);
  });
});

describe('findInPatch', () => {
  it('names the path of each match', () => {
    const files = [
      diff([hunk([line('added', 'alpha', 0)])], 'a.txt'),
      diff([hunk([line('added', 'alpha beta', 0)])], 'b.txt'),
    ];
    expect(findInPatch(files, 'alpha')).toEqual([
      { line: 0, start: 0, length: 5, path: 'a.txt' },
      { line: 0, start: 0, length: 5, path: 'b.txt' },
    ]);
  });
});

describe('findSpans', () => {
  it('marks the active hit apart from the others', () => {
    const spans = findSpans(
      'foo foo',
      [
        { start: 0, length: 3 },
        { start: 4, length: 3 },
      ],
      { start: 4, length: 3 },
    );
    expect(spans).toEqual([
      { text: 'foo', hit: true, current: false },
      { text: ' ', hit: false, current: false },
      { text: 'foo', hit: true, current: true },
    ]);
  });
});

describe('stepMatch', () => {
  it('wraps both ways', () => {
    expect(stepMatch(3, 2, 1)).toBe(0);
    expect(stepMatch(3, 0, -1)).toBe(2);
    expect(stepMatch(0, 0, 1)).toBe(0);
  });
});

describe('matchesByLine', () => {
  it('groups the matches under their line', () => {
    const grouped = matchesByLine([
      { line: 0, start: 0, length: 1, path: 'a' },
      { line: 2, start: 0, length: 1, path: 'a' },
      { line: 0, start: 4, length: 1, path: 'a' },
    ]);
    expect(grouped.get(0)?.map((match) => match.start)).toEqual([0, 4]);
    expect(grouped.get(1)).toBeUndefined();
  });
});
