import type { Operation } from '../api/types';

/**
 * The conflict markers git leaves in a file, read back out of it.
 *
 * When a merge cannot decide, git writes both versions into the work-tree file
 * between markers and leaves the file for a human. That file is ordinary text
 * — the daemon reads and writes it like any other — and everything that makes
 * it a conflict is in these seven-character lines:
 *
 *     <<<<<<< HEAD
 *     what our side says
 *     ||||||| 8f3a1c2          ← only under diff3 and zdiff3
 *     what they both started from
 *     =======
 *     what their side says
 *     >>>>>>> feature
 *
 * Parsed here rather than in the daemon because the editor re-parses on every
 * keystroke: taking one side rewrites the buffer, and the region markers have
 * to move with it before the next frame. A round trip per character is not a
 * design, it is a lag.
 *
 * Nothing here decides anything. It finds the regions and rewrites one when it
 * is told which side to keep; whether that is what the user wanted is a
 * question the buttons ask.
 */

/** Which version of one region to keep. */
export type RegionChoice = 'ours' | 'theirs' | 'both';

/** What "ours" and "theirs" mean, in the words drawn beside the buttons. */
export interface ConflictSideNotes {
  ours: string;
  theirs: string;
}

/** Our side, wherever the operation does not move it: HEAD, where you are. */
const HERE = 'ours — the branch you are on';

/**
 * What "ours" and "theirs" mean, one row per operation.
 *
 * The buttons keep git's words. The notes beside them are the whole of the
 * explanation, and they have to follow the operation, because git does not
 * mean the same thing by "theirs" twice running:
 *
 * A rebase swaps the two outright. Our side is the branch being replayed ONTO,
 * so "ours — the branch you are on" would name the upstream and "Keep ours"
 * would throw the user's work away.
 *
 * A revert inverts what theirs holds. git reverts by merging the reverted
 * commit's PARENT over HEAD — its marker says `>>>>>>> parent of c5cd002
 * (second)` — so their side is the file with that commit taken back out, which
 * is the opposite of its content. "theirs — the commit being reverted" is the
 * one sentence that could make somebody press Keep theirs to keep a change and
 * discard it instead, which is the mistake this whole table exists to prevent.
 *
 * A table rather than a switch, and the type is the reason. Record<Operation,
 * …> stops compiling the day the daemon's union grows a member; a switch with
 * a default would have shipped merge wording for it in silence — and merge
 * wording says "branch", which is a claim, not a hedge.
 *
 * `''` is the row that has to hedge. It means git records no operation, and a
 * stash pop that conflicted looks exactly like a file somebody left markers
 * in: neither has a branch coming in, so neither is told there is one.
 */
const SIDE_NOTES: Record<Operation, ConflictSideNotes> = {
  '': { ours: HERE, theirs: 'theirs — the version coming in' },
  merge: { ours: HERE, theirs: 'theirs — the branch coming in' },
  rebase: {
    ours: 'ours — the branch being replayed onto',
    theirs: 'theirs — the commit being replayed',
  },
  'cherry-pick': { ours: HERE, theirs: 'theirs — the commit being picked' },
  revert: { ours: HERE, theirs: 'theirs — the file before the commit being reverted' },
  bisect: { ours: HERE, theirs: 'theirs — the version coming in' },
  am: { ours: HERE, theirs: 'theirs — the patch being applied' },
};

/**
 * The notes for the operation in progress.
 *
 * The fallback is not dead code, whatever the parameter type says: the
 * operation is a string off the wire from a daemon that can be newer than the
 * page holding it, and a row that does not exist yet would otherwise leave the
 * two buttons with no explanation at all. It falls back to the row that
 * claims least.
 */
export function conflictSideNotes(operation: Operation): ConflictSideNotes {
  return SIDE_NOTES[operation] ?? SIDE_NOTES[''];
}

/** One `<<<<<<< … >>>>>>>` block, and what is inside it. */
export interface ConflictRegion {
  /** Line index of the `<<<<<<<` marker, counting the file's lines from zero. */
  start: number;
  /** Line index of the `>>>>>>>` marker. Inclusive. */
  end: number;

  /**
   * What git wrote after each marker: usually `HEAD` and a branch name, but
   * during a rebase a commit's subject. Shown verbatim — it is git's own note
   * about which side is which, and rewording it is how an interface tells
   * somebody their branch is called something it is not.
   */
  ourLabel: string;
  theirLabel: string;

  ours: string[];
  /** Present only under `merge.conflictStyle = diff3` or `zdiff3`. */
  base?: string[];
  theirs: string[];
}

/**
 * git's markers are seven characters, then either end of line or a space and a
 * label.
 *
 * Anchored and exact, because the alternative is a false positive in ordinary
 * text: `=======` under a heading is how Markdown underlines one, and a
 * looser test would cut a document in half at it. The `<<<<<<<` is what opens
 * a region, so a stray `=======` outside one is never even looked at.
 */
const OURS = /^<<<<<<<(?: (.*))?$/;
const BASE = /^\|\|\|\|\|\|\|(?: (.*))?$/;
const SEPARATOR = /^=======$/;
const THEIRS = /^>>>>>>>(?: (.*))?$/;

/**
 * Every complete conflict region in the text, in the order they appear.
 *
 * Complete is the word that matters. A region needs its opening marker, its
 * separator and its closing marker, in that order; anything else is a file
 * somebody is halfway through editing by hand, and half a region has no side
 * to take. Those are passed over rather than guessed at — the text is still
 * shown and still editable, which is the honest answer to "I do not know what
 * this is".
 */
export function findConflicts(text: string): ConflictRegion[] {
  const lines = text.split('\n');
  const regions: ConflictRegion[] = [];

  for (let index = 0; index < lines.length; index += 1) {
    const opening = OURS.exec(lines[index] ?? '');
    if (opening === null) {
      continue;
    }

    const region = readRegion(lines, index, opening[1] ?? '');
    if (region === undefined) {
      continue;
    }

    regions.push(region);
    // Past the whole region, not past its first line: a `<<<<<<<` inside the
    // text of another conflict is content, not a second region.
    index = region.end;
  }

  return regions;
}

/** Reads one region starting at its `<<<<<<<`, or nothing if it never closes. */
function readRegion(lines: string[], start: number, ourLabel: string): ConflictRegion | undefined {
  const sides = { ours: [] as string[], base: [] as string[], theirs: [] as string[] };

  let sawBase = false;
  let side: keyof typeof sides = 'ours';

  for (let index = start + 1; index < lines.length; index += 1) {
    const line = lines[index] ?? '';

    if (side !== 'theirs' && BASE.test(line)) {
      sawBase = true;
      side = 'base';
      continue;
    }
    if (side !== 'theirs' && SEPARATOR.test(line)) {
      side = 'theirs';
      continue;
    }

    const closing = THEIRS.exec(line);
    if (closing !== null) {
      if (side !== 'theirs') {
        // A `>>>>>>>` before the separator: the file is malformed, and the
        // region has no two sides to choose between.
        return undefined;
      }
      return {
        start,
        end: index,
        ourLabel,
        theirLabel: closing[1] ?? '',
        ours: sides.ours,
        // Absent, not empty, when git wrote no base: the two are different
        // answers. An empty base means both sides added to nothing; no base
        // means this repository does not use a conflict style that shows one.
        ...(sawBase ? { base: sides.base } : {}),
        theirs: sides.theirs,
      };
    }

    // A second `<<<<<<<` before this one closed means the first never does.
    // Stopping here lets the loop above find the second as a region of its
    // own rather than swallowing it as our side's content.
    if (OURS.test(line)) {
      return undefined;
    }

    sides[side].push(line);
  }

  return undefined;
}

/**
 * Rewrites one region, keeping the side asked for.
 *
 * The markers go, the chosen lines stay, and nothing else in the file moves.
 * "Both" keeps ours then theirs, in that order — which is what somebody
 * resolving two independent additions means, and it is a starting point they
 * can edit rather than an answer.
 *
 * The base is never kept. It is what the two sides diverged FROM, shown so a
 * reader can see what each of them changed; keeping it would resolve the
 * conflict by undoing both.
 */
export function applyChoice(text: string, region: ConflictRegion, choice: RegionChoice): string {
  const lines = text.split('\n');

  const kept = keptLines(region, choice);

  return [...lines.slice(0, region.start), ...kept, ...lines.slice(region.end + 1)].join('\n');
}

function keptLines(region: ConflictRegion, choice: RegionChoice): string[] {
  switch (choice) {
    case 'ours':
      return region.ours;
    case 'theirs':
      return region.theirs;
    case 'both':
      return [...region.ours, ...region.theirs];
  }
}

/** A line of the file, with the number it actually has in it. */
export interface NumberedLine {
  /** One-based, as an editor counts. */
  number: number;
  text: string;
}

/** One region as the resolver draws it: what surrounds it, and what it skips. */
export interface ConflictBlock {
  region: ConflictRegion;
  before: NumberedLine[];
  after: NumberedLine[];
  /** Lines between the end of the previous block and the start of this one. */
  skippedBefore: number;
}

export interface ConflictLayout {
  blocks: ConflictBlock[];
  /** Lines after the last block that are not drawn. */
  skippedAfter: number;
}

/**
 * Where each region sits in the file, and how much of the file is left out
 * around it.
 *
 * A conflict in a two-thousand-line file is three lines somebody needs to see
 * and 1,997 they do not, so the resolver draws a window around each region.
 * Two things then have to be true, and neither is free:
 *
 * No line is drawn twice. Two conflicts four lines apart have overlapping
 * windows, and drawing both in full repeats the lines between them — under two
 * different headings, which reads as two copies of the same code rather than
 * as one stretch of file seen twice.
 *
 * Every line left out is counted. A jump from line 17 to line 400 with nothing
 * between them is a pane quietly implying the file ends; saying "382 unchanged
 * lines" is the difference between a window and a lie.
 *
 * Separated from the drawing because it is arithmetic with an off-by-one in
 * every clause of it, and arithmetic is testable.
 */
export function layOutConflicts(text: string, context: number): ConflictLayout {
  const lines = fileLines(text);
  const regions = findConflicts(text);

  const blocks: ConflictBlock[] = [];
  // Where the previous block stopped drawing. Exclusive, and the floor for
  // everything below: it is what keeps a window from reaching back into lines
  // already on screen.
  let drawnTo = 0;

  for (const region of regions) {
    const from = Math.max(drawnTo, region.start - context);
    const to = Math.min(lines.length, region.end + 1 + context);

    blocks.push({
      region,
      before: numbered(lines.slice(from, region.start), from),
      after: numbered(lines.slice(region.end + 1, to), region.end + 1),
      skippedBefore: Math.max(0, region.start - context - drawnTo),
    });

    drawnTo = to;
  }

  return {
    blocks,
    skippedAfter: blocks.length === 0 ? 0 : lines.length - drawnTo,
  };
}

function numbered(slice: string[], from: number): NumberedLine[] {
  return slice.map((text, offset) => ({ number: from + offset + 1, text }));
}

/**
 * The file's lines, without the one a trailing newline invents.
 *
 * `"a\n".split('\n')` is `['a', '']`, and that second element is not a line —
 * it is what follows the last one. Nearly every file on disk ends in a newline,
 * so counting it would be wrong nearly every time: a numbered blank row drawn
 * under the file's real last line, or "1 unchanged line" reported below the
 * last block where the file has already ended. Counting every skipped line
 * exactly is the difference between a window and a lie, and this is where the
 * count comes from.
 *
 * Only the layout drops it. applyChoice rebuilds the file with join, so a line
 * removed there would be a trailing newline silently deleted from somebody's
 * source.
 */
function fileLines(text: string): string[] {
  const lines = text.split('\n');
  if (lines[lines.length - 1] === '') {
    lines.pop();
  }
  return lines;
}

/**
 * Whether the text still holds a marker, complete region or not.
 *
 * Broader than findConflicts and asked for a different reason. findConflicts
 * asks "what can I put a button on"; this asks "is this file finished", and a
 * region somebody half-deleted by hand answers no. Staging that commits a
 * marker, which is the mistake this pane exists to prevent — git itself only
 * warns about it, and only sometimes.
 *
 * The separator is NOT one of the markers looked for, and leaving it out is
 * the whole reason this is a function and not a regular expression at the call
 * site. `=======` under a line of text is how Markdown underlines a heading:
 * every README with a setext title would be reported as unresolved. The other
 * three are seven identical punctuation characters that occur in real text
 * essentially never, so they are evidence and it is not. Refusing to guess
 * costs a warning in the one case where a user deleted both ends of a region
 * and left the middle — and buys never crying wolf about a document.
 */
export function hasConflictMarkers(text: string): boolean {
  return text.split('\n').some((line) => OURS.test(line) || BASE.test(line) || THEIRS.test(line));
}
