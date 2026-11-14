import type { DiffHunk, FileDiff } from '../api/types';
import { MAX_DRAWN_LINES } from './drawnLines';

/**
 * Every added and removed line of a hunk that the cap let through: what a
 * selection can name.
 *
 * Bounded rather than complete, and the discard is why. A 3,200-line patch
 * draws its first 2,000 and the hunk header above them keeps a working
 * "Discard hunk" — unbounded, that button destroys 1,200 lines the pane
 * deliberately refused to show, and the only thing between it and silent loss
 * is a confirmation naming a count nobody can check. `firstDrawnLine` is where
 * this hunk starts in everything being drawn, which is what makes the answer
 * the same one the renderer arrived at.
 */
export function changedIn(hunk: DiffHunk, firstDrawnLine: number): number[] {
  const indices: number[] = [];
  hunk.lines.forEach((line, offset) => {
    if (line.kind !== 'context' && firstDrawnLine + offset < MAX_DRAWN_LINES) {
      indices.push(line.index);
    }
  });
  return indices;
}

/**
 * The same across a whole diff, in the order the lines are drawn — which is
 * the order a Shift+click range runs through, and the order the arrow keys
 * walk.
 */
export function changedLines(diff: FileDiff): number[] {
  const indices: number[] = [];
  let firstDrawnLine = 0;
  for (const hunk of diff.hunks) {
    indices.push(...changedIn(hunk, firstDrawnLine));
    firstDrawnLine += hunk.lines.length;
  }
  return indices;
}

/** How many body lines the whole diff holds. */
export function countDrawnLines(diff: FileDiff): number {
  return diff.hunks.reduce((total, hunk) => total + hunk.lines.length, 0);
}

/** Where a hunk starts, counted in drawn lines from the top of the diff. */
export function firstLineOfHunk(diff: FileDiff, position: number): number {
  let total = 0;
  for (let index = 0; index < position; index += 1) {
    total += diff.hunks[index]?.lines.length ?? 0;
  }
  return total;
}

/**
 * Where each file of a patch starts, counted in drawn lines from its top.
 *
 * A commit's patch is every file it touched, drawn into one scroll region. The
 * cap is spent across them in order, so the files past it cost their header
 * and nothing more.
 */
export function firstLineOfFile(files: readonly FileDiff[]): number[] {
  const starts: number[] = [];
  let total = 0;
  for (const file of files) {
    starts.push(total);
    total += countDrawnLines(file);
  }
  return starts;
}

/** How many body lines a whole patch holds. */
export function countPatchLines(files: readonly FileDiff[]): number {
  return files.reduce((total, file) => total + countDrawnLines(file), 0);
}

/**
 * How wide both line-number columns are drawn, for the whole of what is drawn
 * at once.
 *
 * One width for both columns and for every line, chosen from the largest
 * number any of them will show. The fixed gutter is what keeps the +/- marker
 * from stepping sideways down a file, and at the single width it used to have
 * a six-digit number ran 2px into the column beside it — real, if only in a
 * file of a hundred thousand lines. Sized in steps off the spacing scale
 * rather than in `ch` through an inline style: a width invented in a component
 * is the thing the token rule exists to stop, and three steps cover every file
 * anybody has.
 */
export function gutterWidth(files: readonly FileDiff[]): string {
  let widest = 0;
  for (const file of files) {
    for (const hunk of file.hunks) {
      widest = Math.max(widest, hunk.old_start + hunk.old_lines, hunk.new_start + hunk.new_lines);
    }
  }

  const digits = String(widest).length;
  if (digits <= 4) {
    return 'w-10';
  }
  return digits <= 6 ? 'w-14' : 'w-16';
}
