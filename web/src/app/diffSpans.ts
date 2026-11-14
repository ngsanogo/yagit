import type { DiffHunk, DiffLine } from '../api/types';

/**
 * One run of a line, and what is true of it.
 *
 * A line is a list of these or nothing at all — nothing being the ordinary
 * case, where the text is drawn as it arrived and no span wraps it.
 */
export interface LineSpan {
  text: string;
  /** Part of what differs from the line this one is drawn against. */
  changed: boolean;
  /** Whitespace with nothing to show for itself: drawn as glyphs. */
  glyphs: boolean;
}

/**
 * What changed INSIDE each changed line of a hunk, where that can be said.
 *
 * A one-character change is otherwise two fully tinted lines and no mark on
 * the character: the reader compares two long strings by eye, which is the
 * work this screen exists to spare them. Comparing the two lines is only
 * meaningful if they are versions of each other, and nothing on the wire says
 * they are — `DiffLine` carries kind, text and numbers, and git's own output
 * has no intra-line information in it at all. So the pairing is inferred, and
 * inferred narrowly: a run of removed lines followed by a run of added ones,
 * of the SAME length, is taken as line-for-line replacement. Three removed and
 * five added is a rewrite whose lines do not correspond, and a confidently
 * drawn highlight over the wrong pair is worse than the flat tint, which at
 * least only claims the line changed.
 *
 * A word-level diff in the daemon is the larger version of this and would
 * change the wire type. This is the frontend half of it: pure, cheap, and
 * wrong about nothing it does not first check.
 *
 * `drawable` is how many of the hunk's lines the cap left room for, and the
 * rest are not read at all: nothing past it is on screen to be marked, and the
 * pane's whole defence against a sixty-thousand-line hunk is that it does no
 * work per line it does not draw. A pair the boundary cuts in half stops being
 * a pair — the two runs are no longer the same length — so the last lines
 * before the drawing stops keep the flat tint, which is the same answer this
 * function gives anywhere else it cannot see both sides.
 */
export function markedSpans(hunk: DiffHunk, drawable: number): Map<number, LineSpan[]> {
  const marked = new Map<number, LineSpan[]>();
  // Clamped rather than trusted. `drawable` is a subtraction at the call site,
  // and a negative one handed to slice counts back from the END of the hunk —
  // which would mark the lines the cap threw away and none of the ones on
  // screen, silently and only on the largest patches in a repository.
  const room = Math.min(Math.max(drawable, 0), hunk.lines.length);
  const lines = room === hunk.lines.length ? hunk.lines : hunk.lines.slice(0, room);

  const record = (line: DiffLine, against: string | undefined) => {
    const spans = spansAgainst(line.text, against);
    if (spans !== undefined) {
      marked.set(line.index, spans);
    }
  };

  let at = 0;
  while (at < lines.length) {
    if (lines[at]?.kind === 'context') {
      at += 1;
      continue;
    }

    const removed = runOf(lines, at, 'removed');
    const added = runOf(lines, at + removed.length, 'added');
    if (removed.length === 0 && added.length === 0) {
      at += 1;
      continue;
    }

    const paired = removed.length === added.length;
    removed.forEach((line, index) => record(line, paired ? added[index]?.text : undefined));
    added.forEach((line, index) => record(line, paired ? removed[index]?.text : undefined));
    at += removed.length + added.length;
  }

  return marked;
}

/** A run of consecutive lines of one kind, starting where it is told to. */
function runOf(lines: readonly DiffLine[], from: number, kind: DiffLine['kind']): DiffLine[] {
  const run: DiffLine[] = [];
  for (let at = from; at < lines.length; at += 1) {
    const line = lines[at];
    if (line === undefined || line.kind !== kind) {
      break;
    }
    run.push(line);
  }
  return run;
}

/**
 * One line split into what stayed, what moved, and the whitespace nobody can
 * see — or nothing, when there is neither.
 *
 * `against` is the line this one replaced, where there is one. Without it only
 * trailing whitespace can be marked, because everything else is a comparison.
 *
 * Which whitespace is drawn is a decision and not an omission. Always-on
 * markers turn the indent of every added line in a Go or Python file into a
 * field of dots, so the ones that matter are read as texture and skipped;
 * behind a toggle they are off on the render that mattered, because nobody
 * turns on a control to see something they do not yet know is there. So they
 * are drawn exactly where they carry information: whitespace that DIFFERS from
 * the line this one replaced, and trailing whitespace, which is invisible by
 * construction and never deliberate. An ordinary indent is not news; an indent
 * that changed is the whole of the news.
 *
 * Only spaces and tabs count as whitespace here. A carriage return is the
 * third candidate and it is deliberately left out: a file with CRLF endings
 * carries one on every line, and marking them would put a glyph at the end of
 * every changed line in every repository written on Windows to say nothing at
 * all.
 */
export function spansAgainst(text: string, against: string | undefined): LineSpan[] | undefined {
  const mine = split(text);
  const theirs = against === undefined ? undefined : split(against);

  const leadChanged = theirs !== undefined && mine.lead !== theirs.lead;
  const trailChanged = theirs !== undefined && mine.trail !== theirs.trail;

  const spans: LineSpan[] = [];
  if (mine.lead !== '') {
    spans.push({ text: mine.lead, changed: leadChanged, glyphs: leadChanged });
  }
  spans.push(...bodySpans(mine.body, theirs?.body));
  if (mine.trail !== '') {
    spans.push({ text: mine.trail, changed: trailChanged, glyphs: true });
  }

  // Nothing to say about the line: it is drawn as the plain text it always
  // was, which is also what keeps the ordinary line to a single text node.
  return spans.some((span) => span.changed || span.glyphs) ? spans : undefined;
}

/**
 * The body of a line against the body of the line it replaced.
 *
 * Tokens rather than characters, and the difference is what makes the result
 * readable: `colour` against `color` shares the letters either side of the
 * `u`, so a character-level answer highlights one letter in the middle of a
 * word and leaves the reader to work out which word it was in. A token-level
 * answer highlights `colour` and `color`, which is the word that changed.
 *
 * Two lines that share no token at all get no highlight. They are not versions
 * of each other in any way this can see — `two` against `TWO` shares nothing —
 * and a highlight over the whole line only restates the tint already under it.
 */
function bodySpans(body: string, against: string | undefined): LineSpan[] {
  if (body === '') {
    return [];
  }

  const flat = [{ text: body, changed: false, glyphs: false }];
  if (against === undefined || against === '') {
    return flat;
  }

  const mine = tokens(body);
  const theirs = tokens(against);

  let head = 0;
  while (head < mine.length && head < theirs.length && mine[head] === theirs[head]) {
    head += 1;
  }

  let tail = 0;
  while (
    tail < mine.length - head &&
    tail < theirs.length - head &&
    mine[mine.length - 1 - tail] === theirs[theirs.length - 1 - tail]
  ) {
    tail += 1;
  }

  if (head === 0 && tail === 0) {
    return flat;
  }

  const middle = mine.slice(head, mine.length - tail).join('');
  if (middle === '') {
    // Everything this line holds is shared: the change is on the other side of
    // the pair, which will mark it. Saying so twice would mark a line for
    // holding what did not change.
    return flat;
  }

  const before = mine.slice(0, head).join('');
  const after = mine.slice(mine.length - tail).join('');

  const spans: LineSpan[] = [];
  if (before !== '') {
    spans.push({ text: before, changed: false, glyphs: false });
  }
  spans.push({ text: middle, changed: true, glyphs: blank(middle) });
  if (after !== '') {
    spans.push({ text: after, changed: false, glyphs: false });
  }
  return spans;
}

/**
 * A line's leading whitespace, its body, and its trailing whitespace.
 *
 * A line that is nothing but whitespace is all trailing: there is no body for
 * it to lead, and trailing whitespace is the run that gets drawn whether or
 * not there is a line to compare it with.
 *
 * Scanned from both ends rather than matched with `/[ \t]+$/`, which is the
 * obvious way to write it and is quadratic on the input this pane is given.
 * That pattern consumes a run of whitespace, fails the anchor, hands one
 * character back, fails again — and starts over from the next position in the
 * run. The daemon caps a line at two thousand runes, so a line that is two
 * thousand spaces and a character costs two million steps, times every line of
 * a file made of them. A scan is linear and says the same thing.
 */
function split(text: string): { lead: string; body: string; trail: string } {
  let end = text.length;
  while (end > 0 && isBlank(text[end - 1])) {
    end -= 1;
  }

  let start = 0;
  while (start < end && isBlank(text[start])) {
    start += 1;
  }

  return { lead: text.slice(0, start), body: text.slice(start, end), trail: text.slice(end) };
}

/**
 * The whitespace this pane draws, one character at a time.
 *
 * Spaces and tabs and nothing else, in one place, because `split` and `blank`
 * asking the question two different ways is how a carriage return ends up
 * dotted on one side of the pane and not the other.
 */
function isBlank(character: string | undefined): boolean {
  return character === ' ' || character === '\t';
}

/**
 * A line cut into words, whitespace and everything else.
 *
 * Unicode letters and digits, not `\w`: an identifier in a language people
 * write in is `préférence` or `店舗名`, and a class that stops at ASCII would
 * cut both into a run of single characters and mark the whole word as moved.
 */
function tokens(text: string): string[] {
  return text.match(/[\p{L}\p{N}_]+|[ \t]+|[^\p{L}\p{N}_ \t]+/gu) ?? [];
}

/** Whitespace and nothing else — a run that would be drawn as blank. */
function blank(text: string): boolean {
  for (const character of text) {
    if (!isBlank(character)) {
      return false;
    }
  }
  return text !== '';
}
