import type { DiffLine, FileDiff } from '../api/types';

import { MAX_DRAWN_LINES } from './drawnLines';

/**
 * Finding a literal string in a drawn patch.
 *
 * The query is literal and case-insensitive: the history search already
 * taught that a box which handed POSIX patterns to git would refuse ordinary
 * punctuation, and a find-in-diff that did the same would punish the same
 * readers for the same reason. It is not trimmed either — leading spaces are
 * how somebody looks for an indentation. Matches past the draw cap are
 * ignored — those lines are not on screen, and a "next" that jumped to one
 * would scroll nowhere.
 */

export interface DiffFindMatch {
  /** Flat line index as the daemon numbered it. */
  line: number;
  /** Character offset into `DiffLine.text`. */
  start: number;
  length: number;
  /** The file the line belongs to: line indices restart in every file. */
  path: string;
}

/** Every match of `query` in the lines the pane actually draws. */
export function findInDiff(diff: FileDiff, query: string): DiffFindMatch[] {
  return findInLines(diff.path, drawnLines(diff), query);
}

/** Every match across a commit/stash patch, still bounded by the shared cap. */
export function findInPatch(files: readonly FileDiff[], query: string): DiffFindMatch[] {
  const needle = literalPattern(query);
  if (needle === undefined) {
    return [];
  }

  const matches: DiffFindMatch[] = [];
  let drawn = 0;
  for (const file of files) {
    if (file.binary) {
      continue;
    }
    for (const hunk of file.hunks) {
      for (const line of hunk.lines) {
        if (drawn >= MAX_DRAWN_LINES) {
          return matches;
        }
        drawn += 1;
        pushMatches(matches, file.path, line, needle);
      }
    }
  }
  return matches;
}

function findInLines(path: string, lines: readonly DiffLine[], query: string): DiffFindMatch[] {
  const needle = literalPattern(query);
  if (needle === undefined) {
    return [];
  }
  const matches: DiffFindMatch[] = [];
  for (const line of lines) {
    pushMatches(matches, path, line, needle);
  }
  return matches;
}

function drawnLines(diff: FileDiff): DiffLine[] {
  if (diff.binary) {
    return [];
  }
  const lines: DiffLine[] = [];
  for (const hunk of diff.hunks) {
    for (const line of hunk.lines) {
      if (lines.length >= MAX_DRAWN_LINES) {
        return lines;
      }
      lines.push(line);
    }
  }
  return lines;
}

/**
 * The query as a case-insensitive pattern that matches it literally, or
 * undefined for an empty one.
 *
 * A pattern rather than lower-casing both sides and comparing: lower-casing
 * can change a string's length — "İ" becomes two code units — and an offset
 * found in the lowered copy would then paint the wrong characters of the
 * original. A match of the pattern is an offset into the text as drawn.
 */
function literalPattern(query: string): RegExp | undefined {
  if (query === '') {
    return undefined;
  }
  return new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi');
}

function pushMatches(into: DiffFindMatch[], path: string, line: DiffLine, needle: RegExp): void {
  for (const match of line.text.matchAll(needle)) {
    into.push({ line: line.index, start: match.index, length: match[0].length, path });
  }
}

/**
 * The matches grouped by line, so each drawn line looks up its own rather
 * than filtering the whole list — a common letter in a long diff is
 * thousands of matches, and every line scanning them is the product.
 */
export function matchesByLine(
  matches: readonly DiffFindMatch[],
): ReadonlyMap<number, readonly DiffFindMatch[]> {
  const byLine = new Map<number, DiffFindMatch[]>();
  for (const match of matches) {
    const existing = byLine.get(match.line);
    if (existing === undefined) {
      byLine.set(match.line, [match]);
    } else {
      existing.push(match);
    }
  }
  return byLine;
}

/**
 * Split a line into runs so the current match can be painted without
 * disturbing the rest of the text.
 *
 * When `active` is set, only that one occurrence is marked current; every
 * other match on the line is marked as a hit. No match → undefined, so the
 * caller keeps whatever spans it already had for intra-line edits.
 */
export function findSpans(
  text: string,
  matches: readonly { start: number; length: number }[],
  active?: { start: number; length: number },
): { text: string; hit: boolean; current: boolean }[] | undefined {
  if (matches.length === 0) {
    return undefined;
  }

  const cuts = new Set<number>([0, text.length]);
  for (const match of matches) {
    cuts.add(match.start);
    cuts.add(match.start + match.length);
  }
  const edges = [...cuts].sort((a, b) => a - b);
  const spans: { text: string; hit: boolean; current: boolean }[] = [];

  for (let i = 0; i < edges.length - 1; i += 1) {
    const start = edges[i]!;
    const end = edges[i + 1]!;
    if (end <= start) {
      continue;
    }
    const hit = matches.some((m) => start >= m.start && end <= m.start + m.length);
    const current =
      active !== undefined && hit && start >= active.start && end <= active.start + active.length;
    spans.push({ text: text.slice(start, end), hit, current });
  }
  return spans;
}

/** Cycle the active match; wraps. */
export function stepMatch(count: number, current: number, delta: 1 | -1): number {
  if (count <= 0) {
    return 0;
  }
  return (current + delta + count) % count;
}
