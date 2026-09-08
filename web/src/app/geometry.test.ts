import { describe, expect, it } from 'vitest';

import { ABSENT_ROW, type GraphEdge } from '../api/types';
import {
  columnCentre,
  columnsWithin,
  edgePath,
  edgeShape,
  graphFits,
  graphGutter,
  graphWidth,
  MOST_DRAWABLE_COLUMNS,
  ROW_HEIGHT,
  rowCentre,
} from './geometry';

/**
 * A line that starts half a row above its commit is not a crash. It is a
 * picture that looks nearly right, which is the kind of defect that ships.
 */

function edge(parts: Partial<GraphEdge>): GraphEdge {
  return { from: 0, from_lane: 0, to: 1, to_lane: 0, lane: 0, ...parts };
}

describe('graphWidth', () => {
  it('gives every column its step, plus the inset at the left', () => {
    expect(graphWidth(3) - graphWidth(2)).toBe(16);
  });

  it('never collapses to nothing, so a one-column history still has a column', () => {
    expect(graphWidth(0)).toBe(graphWidth(1));
    expect(graphWidth(0)).toBeGreaterThan(16);
  });

  it('leaves the first column clear of the panel edge', () => {
    expect(columnCentre(0)).toBeGreaterThan(16 / 2);
  });
});

describe('graphFits', () => {
  it('draws the graphs a screen can hold', () => {
    expect(graphFits(1)).toBe(true);
    expect(graphFits(MOST_DRAWABLE_COLUMNS)).toBe(true);
  });

  it('refuses the ones it cannot', () => {
    // git's own repository needs 280 columns when every ref is drawn. There
    // is no readable picture at that width, and drawing a narrower one would
    // mean leaving branches out of it without saying so.
    expect(graphFits(MOST_DRAWABLE_COLUMNS + 1)).toBe(false);
    expect(graphFits(280)).toBe(false);
  });
});

describe('graphGutter', () => {
  it('gives the graph everything it asked for while the panel is wide enough', () => {
    expect(graphGutter(4, 1200)).toBe(graphWidth(4));
  });

  it('takes no more than a third of the panel', () => {
    // Twenty-three columns is 380 pixels: a third of the design viewport's
    // panel, and two thirds of a panel half that size, where the subjects the
    // picture exists to sit beside reach zero.
    expect(graphGutter(23, 1140)).toBe(380);
    expect(graphGutter(23, 600)).toBe(200);
  });

  it('reads an unmeasured panel as no bound rather than as no room', () => {
    // Zero is the first render, and every render in a runner with no layout. A
    // gutter that collapsed there would draw a whole window at the wrong
    // indent and then move it.
    expect(graphGutter(9, 0)).toBe(graphWidth(9));
    expect(graphGutter(9, -1)).toBe(graphWidth(9));
  });

  it('keeps a whole column even when a third of the panel is less than one', () => {
    expect(graphGutter(9, 40)).toBe(graphWidth(1));
  });
});

describe('columnsWithin', () => {
  it('counts the columns a gutter has room for', () => {
    for (const columns of [1, 2, 9, MOST_DRAWABLE_COLUMNS]) {
      expect(columnsWithin(graphWidth(columns))).toBe(columns);
    }
  });

  it('never claims a fraction of a column, and never claims none', () => {
    expect(columnsWithin(graphWidth(3) - 1)).toBe(2);
    expect(columnsWithin(0)).toBe(1);
  });

  it('agrees with the gutter it is asked about, which is what the rows say out loud', () => {
    // The pair is the honest refusal at panel scale: the gutter clips the
    // picture and this is the number the notice above the rows names.
    const gutter = graphGutter(23, 600);

    expect(columnsWithin(gutter)).toBeLessThan(23);
    expect(graphWidth(columnsWithin(gutter))).toBeLessThanOrEqual(gutter);
  });
});

describe('edgeShape', () => {
  it('runs straight down when both ends share the line’s column', () => {
    const shape = edgeShape(edge({ from: 4, to: 6 }), 4, 100);

    expect(shape.startX).toBe(shape.laneX);
    expect(shape.endX).toBe(shape.laneX);
    // No bend at either end: the straight part spans the whole line.
    expect(shape.topY).toBe(shape.startY);
    expect(shape.bottomY).toBe(shape.endY);
  });

  it('starts on its commit’s dot and ends on its parent’s', () => {
    const shape = edgeShape(edge({ from: 2, from_lane: 0, to: 9, to_lane: 1, lane: 3 }), 0, 100);

    expect(shape.startX).toBe(columnCentre(0));
    expect(shape.startY).toBe(rowCentre(2, 0));
    expect(shape.endX).toBe(columnCentre(1));
    expect(shape.endY).toBe(rowCentre(9, 0));
    expect(shape.laneX).toBe(columnCentre(3));
  });

  it('takes one row to bend out of a dot and one to bend onto another', () => {
    const shape = edgeShape(edge({ from: 0, from_lane: 0, to: 8, to_lane: 0, lane: 2 }), 0, 100);

    expect(shape.topY).toBe(shape.startY + ROW_HEIGHT);
    expect(shape.bottomY).toBe(shape.endY - ROW_HEIGHT);
    expect(shape.topY).toBeLessThan(shape.bottomY);
  });

  it('collapses both bends into one curve when the ends are too close to hold them', () => {
    // A merge whose second parent is the very next row: there is no room for
    // a bend out, a straight run and a bend in. Without the clamp the middle
    // section would be drawn backwards.
    const shape = edgeShape(edge({ from: 3, from_lane: 0, to: 4, to_lane: 0, lane: 2 }), 0, 100);

    expect(shape.topY).toBe(shape.bottomY);
    expect(shape.topY).toBeGreaterThanOrEqual(shape.startY);
    expect(shape.topY).toBeLessThanOrEqual(shape.endY);
  });

  it('leaves the bottom of the picture when the parent is not in the history', () => {
    const total = 40;
    const shape = edgeShape(
      edge({ from: 38, from_lane: 1, to: ABSENT_ROW, to_lane: ABSENT_ROW, lane: 1 }),
      30,
      total,
    );

    // Straight down its own column, past the last row, with nothing to bend
    // onto: a column of ABSENT_ROW must never reach the geometry.
    expect(shape.endX).toBe(shape.laneX);
    expect(shape.endY).toBe(rowCentre(total, 30));
    expect(shape.endY).toBeGreaterThan(rowCentre(total - 1, 30));
  });

  it('measures from the first row drawn, not from the top of the history', () => {
    const near = edgeShape(edge({ from: 500, to: 501 }), 500, 100_000);
    const top = edgeShape(edge({ from: 0, to: 1 }), 0, 100_000);

    expect(near.startY).toBe(top.startY);
    expect(near.endY).toBe(top.endY);
  });
});

describe('edgePath', () => {
  it('begins on the dot it leaves and finishes on the dot it joins', () => {
    const shape = edgeShape(edge({ from: 1, from_lane: 0, to: 7, to_lane: 2, lane: 1 }), 0, 100);
    const path = edgePath(shape);

    expect(path.startsWith(`M${shape.startX},${shape.startY}`)).toBe(true);
    expect(path.endsWith(`${shape.endX},${shape.endY}`)).toBe(true);
  });

  it('is one straight line when nothing bends', () => {
    const path = edgePath(edgeShape(edge({ from: 0, to: 5 }), 0, 100));

    expect(path).toBe(
      `M${columnCentre(0)},${rowCentre(0, 0)}L${columnCentre(0)},${rowCentre(5, 0)}`,
    );
    expect(path).not.toContain('C');
  });

  it('curves rather than corners where a line changes column', () => {
    const path = edgePath(
      edgeShape(edge({ from: 0, from_lane: 0, to: 9, to_lane: 0, lane: 3 }), 0, 100),
    );

    // One curve out of the dot, one straight run, one curve back onto it.
    expect(path.match(/C/g)).toHaveLength(2);
    expect(path).toContain('L');
  });
});
