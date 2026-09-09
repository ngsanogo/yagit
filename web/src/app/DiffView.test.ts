import { describe, expect, it } from 'vitest';

import type { DiffHunk, DiffLine, FileDiff } from '../api/types';
import { changedIn, changedLines, gutterWidth, markedSpans, spansAgainst } from './DiffView';
import { MAX_DRAWN_LINES } from './drawnLines';

/**
 * What the diff pane says about a line before anything is drawn.
 *
 * Every one of these is a claim about somebody's code: that this run of
 * characters is what moved, that this indent is not the same indent as the one
 * above it. Being wrong here is not a crash — it is a highlight over the wrong
 * half of a line, which is worse than no highlight at all, because a reader
 * who trusts it stops reading the rest.
 */

function line(kind: DiffLine['kind'], text: string, index: number): DiffLine {
  return { kind, text, index, old_line: 0, new_line: 0, no_newline: false };
}

function hunk(lines: DiffLine[], starts: Partial<DiffHunk> = {}): DiffHunk {
  return {
    old_start: 1,
    old_lines: lines.length,
    new_start: 1,
    new_lines: lines.length,
    heading: '',
    lines,
    ...starts,
  };
}

function diff(hunks: DiffHunk[]): FileDiff {
  return { id: 'fingerprint', path: 'a.txt', binary: false, added: false, removed: false, hunks };
}

/** Every span put back together has to be the line the daemon sent. */
function joined(spans: readonly { text: string }[] | undefined): string | undefined {
  return spans?.map((span) => span.text).join('');
}

describe('spansAgainst', () => {
  it('says nothing about a line that has nothing to say', () => {
    expect(spansAgainst('const width = 240;', undefined)).toBeUndefined();
    expect(spansAgainst('const width = 240;', 'const width = 240;')).toBeUndefined();
  });

  it('marks the word that changed, and leaves the rest of the line alone', () => {
    const spans = spansAgainst('const colour = lane(3);', 'const color = lane(3);');

    expect(spans).toEqual([
      { text: 'const ', changed: false, glyphs: false },
      { text: 'colour', changed: true, glyphs: false },
      { text: ' = lane(3);', changed: false, glyphs: false },
    ]);
    expect(joined(spans)).toBe('const colour = lane(3);');
  });

  // The case the whole feature exists for: two lines of identical ink. Without
  // the glyphs the reader is told something changed and shown nothing.
  it('draws an indent that changed, on both sides of the pair', () => {
    const tabbed = spansAgainst('\tfmt.Println("hi")', '    fmt.Println("hi")');
    const spaced = spansAgainst('    fmt.Println("hi")', '\tfmt.Println("hi")');

    expect(tabbed?.[0]).toEqual({ text: '\t', changed: true, glyphs: true });
    expect(spaced?.[0]).toEqual({ text: '    ', changed: true, glyphs: true });
    expect(joined(tabbed)).toBe('\tfmt.Println("hi")');
  });

  it('leaves an indent that did not change as plain text', () => {
    const spans = spansAgainst('\tone(a)', '\tone(b)');

    expect(spans?.[0]?.glyphs).toBe(false);
    expect(spans?.some((span) => span.changed && span.glyphs)).toBe(false);
  });

  it('draws trailing whitespace whether or not there is a line to compare', () => {
    expect(spansAgainst('done   ', 'done')).toEqual([
      { text: 'done', changed: false, glyphs: false },
      { text: '   ', changed: true, glyphs: true },
    ]);

    // No pair at all — a line nothing replaced. Trailing whitespace is still
    // invisible, and still nobody's intention.
    expect(spansAgainst('done ', undefined)).toEqual([
      { text: 'done', changed: false, glyphs: false },
      { text: ' ', changed: false, glyphs: true },
    ]);
  });

  it('draws a line that is nothing but whitespace as whitespace', () => {
    expect(spansAgainst('    ', '\t\t')).toEqual([{ text: '    ', changed: true, glyphs: true }]);
  });

  it('draws whitespace that collapsed in the middle of a line', () => {
    const spans = spansAgainst('a b', 'a  b');

    expect(spans).toEqual([
      { text: 'a', changed: false, glyphs: false },
      { text: ' ', changed: true, glyphs: true },
      { text: 'b', changed: false, glyphs: false },
    ]);
  });

  // A file written on Windows carries one on every line. Marking them would
  // put a glyph at the end of every changed line in the repository to say
  // nothing that is true of the change.
  it('draws no glyph for a carriage return', () => {
    const spans = spansAgainst('done\r', 'gone\r');

    // The word that changed is still marked; the invisible character at the
    // end of it is left where it is.
    expect(spans?.[0]).toEqual({ text: 'done', changed: true, glyphs: false });
    expect(spans?.some((span) => span.glyphs)).toBe(false);
  });

  it('refuses to guess at two lines that share no word', () => {
    // 'two' against 'TWO' is a case-only change, and there is no shared token
    // to hang a prefix or a suffix on: a highlight over the whole line only
    // repeats the tint already under it.
    expect(spansAgainst('two', 'TWO')).toBeUndefined();
    expect(spansAgainst('return nil', 'for index := range rows {')).toBeUndefined();
  });

  it('marks nothing on the side a pure insertion left untouched', () => {
    expect(spansAgainst('draw(a)', 'draw(a, b)')).toBeUndefined();
    expect(spansAgainst('draw(a, b)', 'draw(a)')).toEqual([
      { text: 'draw(a', changed: false, glyphs: false },
      { text: ', b', changed: true, glyphs: false },
      { text: ')', changed: false, glyphs: false },
    ]);
  });

  it('cuts words at letters no ASCII class knows about', () => {
    const spans = spansAgainst('const préférence = 1;', 'const préférences = 1;');

    expect(spans?.[1]).toEqual({ text: 'préférence', changed: true, glyphs: false });
  });
});

describe('markedSpans', () => {
  /** Every line drawn, which is what happens to all but the largest hunks. */
  const marksIn = (lines: DiffLine[]) => markedSpans(hunk(lines), lines.length);

  it('pairs a removed run with an added run of the same length', () => {
    const marked = marksIn([
      line('context', 'one', 0),
      line('removed', 'const colour = 1;', 1),
      line('added', 'const color = 1;', 2),
    ]);

    expect(marked.get(1)?.[1]?.text).toBe('colour');
    expect(marked.get(2)?.[1]?.text).toBe('color');
    expect(marked.has(0)).toBe(false);
  });

  // Three lines out and five in is a rewrite whose lines do not correspond.
  // Pairing them by position would mark a run of one line as the change in
  // another, which is a confident answer to a question nothing asked.
  it('refuses to pair runs of different lengths', () => {
    const marked = marksIn([
      line('removed', 'const colour = 1;', 0),
      line('added', 'const color = 1;', 1),
      line('added', 'const width = 2;', 2),
    ]);

    expect(marked.size).toBe(0);
  });

  it('marks trailing whitespace even where nothing pairs', () => {
    expect(
      marksIn([line('added', 'const width = 2; ', 0)])
        .get(0)
        ?.at(-1),
    ).toEqual({
      text: ' ',
      changed: false,
      glyphs: true,
    });
  });

  it('walks a hunk of nothing but context without stopping', () => {
    expect(marksIn([line('context', 'one', 0), line('context', 'two', 1)]).size).toBe(0);
  });

  // The pane's defence against a regenerated lockfile is that it does no work
  // per line it does not draw, and a word diff over sixty thousand lines to
  // mark the two thousand on screen would spend exactly what the cap saves.
  it('reads no further than the cap let the pane draw', () => {
    const pair = [line('removed', 'const colour = 1;', 0), line('added', 'const color = 1;', 1)];

    expect(markedSpans(hunk(pair), 2).size).toBe(2);
    // One half of the pair is drawn and the other is not, so there is no pair
    // to diff: the drawn line keeps the flat tint rather than a highlight
    // measured against a line nobody was shown.
    expect(markedSpans(hunk(pair), 1).size).toBe(0);
    expect(markedSpans(hunk(pair), 0).size).toBe(0);

    // A hunk wholly past the cap asks for a negative number of lines, and a
    // slice reads a negative count from the END — so the unclamped version of
    // this marks the lines the cap threw away and none of the ones on screen.
    const three = [...pair, line('added', 'const width = 2;', 2)];
    expect(markedSpans(hunk(three), -1).size).toBe(0);
  });
});

describe('gutterWidth', () => {
  const of = (overrides: Partial<DiffHunk>) => diff([hunk([line('context', 'one', 0)], overrides)]);

  it('takes three steps, and the widest number decides which', () => {
    // Four digits and fewer fit the width the pane has always had.
    expect(gutterWidth([of({ new_lines: 9_000 })])).toBe('w-10');
    expect(gutterWidth([of({ new_lines: 99_000 })])).toBe('w-14');
    expect(gutterWidth([of({ new_lines: 1_200_000 })])).toBe('w-16');

    // A file with no hunk left in it is still a column: an empty gutter that
    // collapsed would move the marker of every file drawn beside it.
    expect(gutterWidth([])).toBe('w-10');
  });

  it('reads both columns, and every file drawn beside this one', () => {
    // One width for the whole patch. Sized per file, a commit that touches a
    // lockfile and a README would step the gutter sideways at the header
    // between them — which is the jitter the fixed column exists to stop.
    expect(gutterWidth([of({ old_start: 99_000, old_lines: 500 })])).toBe('w-14');
    expect(gutterWidth([of({ new_lines: 40 }), of({ new_lines: 99_000 })])).toBe('w-14');
  });
});

describe('what a hunk button can name', () => {
  const many = hunk(
    Array.from({ length: MAX_DRAWN_LINES + 200 }, (_, at) => line('added', `line ${at}`, at)),
  );

  it('names only the lines the cap let through', () => {
    // Unbounded, "Discard hunk" on a truncated patch destroys the lines the
    // pane refused to draw — the one destructive button in the app that could
    // act on something nobody was shown.
    expect(changedIn(many, 0)).toHaveLength(MAX_DRAWN_LINES);
    expect(changedIn(many, MAX_DRAWN_LINES)).toEqual([]);
    expect(changedLines(diff([many]))).toHaveLength(MAX_DRAWN_LINES);
  });

  it('names every changed line of a patch that fits', () => {
    const small = hunk([
      line('context', 'one', 0),
      line('removed', 'two', 1),
      line('added', 'TWO', 2),
    ]);

    expect(changedIn(small, 0)).toEqual([1, 2]);
    expect(changedLines(diff([small]))).toEqual([1, 2]);
  });
});
