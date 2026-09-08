/**
 * The measurements the history and its graph share, and the arithmetic that
 * turns an edge into a shape.
 *
 * It is its own module because both the list and the graph read these numbers,
 * and because the arithmetic below is worth testing on its own: a line that
 * starts half a row above its commit is not a crash, it is a picture that
 * looks nearly right.
 *
 * Coordinates are local to the window being drawn. The graph is never a
 * hundred thousand rows tall — it is an SVG the height of the rows on screen,
 * moved down as they scroll, which is what makes drawing it cost the same
 * whatever the history's length (docs/adr/0003).
 */

import { ABSENT_ROW, type GraphEdge } from '../api/types';

/**
 * Row height, in pixels, fixed.
 *
 * Fixed because measuring every row is what makes a virtualised list feel
 * loose: the scrollbar changes size as you drag it, and the position you were
 * dragging towards moves.
 *
 * Forty, and it used to be fifty-six. A row is one line now — subject, author,
 * date and sha side by side in columns rather than stacked in two — and the
 * tallest thing on it is the author chip at twenty-four pixels. Fifty-six left
 * seventeen pixels of nothing and cost the history on screen a third of its
 * rows, which is the price paid twice over the moment a commit is selected and
 * the panel halves. Nothing in a row wraps, so this height is the content's
 * and not the text's luck.
 *
 * BEND below is this number, so the merge curves rescale with it rather than
 * being retuned by hand: at forty a line still takes a whole row to change
 * column, which is what keeps a merge a curve rather than a corner.
 */
export const ROW_HEIGHT = 40;

/**
 * The horizontal step between two columns, in pixels. Four spacing units, so
 * the graph lands on the same 4px grid as everything else.
 */
export const LANE_WIDTH = 16;

/**
 * The widest graph worth drawing, in columns.
 *
 * Twenty-four columns is 384 pixels, which already takes a third of the panel
 * from the subjects the graph is there to sit beside. Past that the picture
 * stops being a picture.
 *
 * The number is not hypothetical. git's own repository needs 280 columns when
 * every ref is drawn — its thousand tags and its topic branches in flight are
 * genuinely that many lines at once, and git's own `--graph` needs 67 for the
 * same history. Nothing readable exists at either figure.
 *
 * What fixes it is choosing which refs the graph draws, and that choice now
 * exists: the graph is the current branch unless every ref is asked for
 * (docs/adr/0016). This bound stays, as the honest refusal for the day the
 * chosen set is itself too wide — saying so is better than a picture nobody
 * can read, and far better than a narrower one that leaves branches out.
 */
export const MOST_DRAWABLE_COLUMNS = 24;

/** Whether a graph this wide is worth drawing at all. */
export function graphFits(columns: number): boolean {
  return columns <= MOST_DRAWABLE_COLUMNS;
}

/** Radius of a commit's dot. */
export const DOT_RADIUS = 4;

/**
 * Space to the left of the first column, in pixels.
 *
 * Without it the leftmost branch is drawn against the panel's own border,
 * which reads as part of the frame rather than as part of the history.
 */
const GRAPH_INSET = 12;

/**
 * How far a line takes to bend from one column into another: one row, so a
 * merge reads as a curve rather than a corner.
 */
const BEND = ROW_HEIGHT;

/** The width, in pixels, a graph of this many columns needs. */
export function graphWidth(columns: number): number {
  return GRAPH_INSET + Math.max(columns, 1) * LANE_WIDTH;
}

/**
 * The most of the list the graph gutter may take.
 *
 * MOST_DRAWABLE_COLUMNS reasons in exactly this fraction — "384 pixels, which
 * already takes a third of the panel" — but it is written in columns, and a
 * column count cannot see how wide the window got. Twenty-three columns is
 * 380 pixels: about a third of the history panel at the design viewport, and
 * two thirds of a panel half that size, where the graph kept all 380 anyway
 * and the subjects beside it went to nothing. It is the same judgement, asked
 * of the panel rather than of a number fixed at one window width.
 */
const GRAPH_SHARE_OF_LIST = 1 / 3;

/**
 * How much room the rows leave for the graph, in pixels.
 *
 * `available` is the width of the list. Zero means nobody has measured it yet
 * — the first render, and every render in a runner with no layout — and has to
 * read as "no bound known", never as "no room": a gutter that collapsed on the
 * first frame would draw the whole window at the wrong indent and then move
 * it, which is the horizontal twin of the jitter ROW_HEIGHT is fixed to
 * prevent.
 *
 * Measured against the list and not against the rows on screen, deliberately.
 * Sizing the gutter from the widest lane currently rendered would slide the
 * entire subject column sideways during an ordinary flick of the wheel, and a
 * text column that stays put is worth more than one that is tight.
 *
 * The floor is one column: below that what is left is not a narrower picture,
 * it is a vertical line with nothing to relate.
 */
export function graphGutter(columns: number, available: number): number {
  const wanted = graphWidth(columns);
  if (available <= 0) {
    return wanted;
  }
  return Math.max(graphWidth(1), Math.min(wanted, Math.floor(available * GRAPH_SHARE_OF_LIST)));
}

/**
 * How many whole columns a gutter this wide has room for.
 *
 * The rows say so out loud when it is fewer than the history has. A picture
 * that quietly left branches out is the one answer MOST_DRAWABLE_COLUMNS
 * above refuses, and clipping one at the panel edge without a word would be
 * that answer arrived at sideways.
 */
export function columnsWithin(gutter: number): number {
  return Math.max(1, Math.floor((gutter - GRAPH_INSET) / LANE_WIDTH));
}

/** The centre of a column. */
export function columnCentre(lane: number): number {
  return GRAPH_INSET + lane * LANE_WIDTH + LANE_WIDTH / 2;
}

/** The centre of a row, relative to the first row being drawn. */
export function rowCentre(row: number, first: number): number {
  return (row - first) * ROW_HEIGHT + ROW_HEIGHT / 2;
}

/**
 * The points a line passes through: out of its commit's dot, down its own
 * column, and onto its parent's dot.
 *
 * topY and bottomY bound the straight part. When the two ends are less than
 * two rows apart there is no room for both bends and no straight part left, so
 * they collapse onto the midpoint and the line becomes one continuous curve.
 * Without that clamp a short merge draws its middle section backwards.
 */
export interface EdgeShape {
  startX: number;
  startY: number;
  laneX: number;
  topY: number;
  bottomY: number;
  endX: number;
  endY: number;
}

export function edgeShape(edge: GraphEdge, first: number, total: number): EdgeShape {
  const startX = columnCentre(edge.from_lane);
  const startY = rowCentre(edge.from, first);
  const laneX = columnCentre(edge.lane);

  // A parent that is not in the history — a shallow clone, a history cut
  // short — is a real line that leaves the bottom of the picture. It ends
  // below the last row, in its own column, with nothing to bend onto.
  const ends = edge.to !== ABSENT_ROW;
  const endX = ends ? columnCentre(edge.to_lane) : laneX;
  const endY = rowCentre(ends ? edge.to : total, first);

  let topY = edge.from_lane === edge.lane ? startY : startY + BEND;
  let bottomY = ends && edge.to_lane !== edge.lane ? endY - BEND : endY;

  if (topY > bottomY) {
    const middle = (startY + endY) / 2;
    topY = middle;
    bottomY = middle;
  }

  return { startX, startY, laneX, topY, bottomY, endX, endY };
}

/**
 * The `d` attribute for a shape.
 *
 * Each bend is a cubic whose control points sit on the vertical through each
 * end, at the same height. That is what gives the curve a vertical tangent at
 * both ends, so a line leaves a dot and joins its column without a visible
 * corner at either join.
 */
export function edgePath(shape: EdgeShape): string {
  const { startX, startY, laneX, topY, bottomY, endX, endY } = shape;

  let path = `M${startX},${startY}`;
  if (startX !== laneX || startY !== topY) {
    const middle = (startY + topY) / 2;
    path += `C${startX},${middle} ${laneX},${middle} ${laneX},${topY}`;
  }
  if (bottomY !== topY) {
    path += `L${laneX},${bottomY}`;
  }
  if (endX !== laneX || endY !== bottomY) {
    const middle = (bottomY + endY) / 2;
    path += `C${laneX},${middle} ${endX},${middle} ${endX},${endY}`;
  }
  return path;
}
