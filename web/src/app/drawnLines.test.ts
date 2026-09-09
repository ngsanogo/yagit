import { describe, expect, it } from 'vitest';

import { MAX_DRAWN_LINES, overCap } from './drawnLines';

/**
 * Where the cap bites, pinned in the one place that decides it.
 *
 * The boundary is the whole reason this is a function. It was written out
 * three times — once as `drawn > MAX`, twice as `lines <= MAX` negated — and
 * two views drawing the same patch disagreeing by one line is a pane that
 * draws every line it has and says it did not.
 */
describe('overCap', () => {
  it('bites past the cap and not at it', () => {
    // A file of exactly the cap is drawn whole. A notice over it would read
    // "showing the first 2,000 lines of 2,000", which is a sentence that
    // tells the reader something is missing when nothing is.
    expect(overCap(MAX_DRAWN_LINES - 1)).toBe(false);
    expect(overCap(MAX_DRAWN_LINES)).toBe(false);
    expect(overCap(MAX_DRAWN_LINES + 1)).toBe(true);
  });

  it('says nothing is cut when there is nothing to draw', () => {
    // An empty file and an empty patch both reach the notices.
    expect(overCap(0)).toBe(false);
  });
});
