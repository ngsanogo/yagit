import { ABSENT_ROW, type GraphEdge } from '../api/types';
import { laneColor } from '../design/tokens';
import { columnCentre, DOT_RADIUS, edgePath, edgeShape, ROW_HEIGHT, rowCentre } from './geometry';

/**
 * The commit graph, drawn for the rows on screen and no others.
 *
 * SVG rather than canvas (docs/adr/0003): a lane's colour is
 * `stroke: var(--color-lane-3)`, which the browser recalculates by itself when
 * the theme changes, so the palette lives in `tokens.css` and nowhere else.
 *
 * Lines are batched one `<path>` per column instead of one per edge. Sixty
 * dots and a dozen paths is the whole picture, whatever the history's length.
 *
 * The whole thing is `aria-hidden`: it is a picture of the relationships
 * between rows that are already in the accessibility tree with their author,
 * their date and their subject, and announcing a column number would add
 * noise rather than information.
 */
interface CommitGraphProps {
  /** First row drawn. Coordinates inside the SVG are relative to it. */
  first: number;
  /** How many rows are drawn. */
  count: number;
  /**
   * The gutter the rows have left, in pixels.
   *
   * Given rather than derived from `columns`, because on a narrow window the
   * two differ: the list bounds the gutter against its own width and the
   * picture is clipped at that edge, which the rows announce. Coordinates are
   * unchanged either way — a clipped graph is the same graph with less of it
   * on screen, not a redrawn one, so a lane keeps its colour and its x as the
   * window is resized.
   */
  width: number;
  /** Commits in the whole history, so a line with no parent knows where the
   * bottom of the picture is. */
  total: number;
  edges: GraphEdge[];
  /** The column of a row's dot, or undefined for a row not loaded yet. */
  laneOf: (row: number) => number | undefined;
}

export function CommitGraph({ first, count, width, total, edges, laneOf }: CommitGraphProps) {
  return (
    <svg
      aria-hidden="true"
      className="pointer-events-none absolute left-0 top-0 overflow-hidden"
      width={width}
      height={count * ROW_HEIGHT}
      style={{ transform: `translateY(${first * ROW_HEIGHT}px)` }}
    >
      {[...batchByColumn(edges, first, count, total)].map(([lane, path]) => (
        <path
          key={lane}
          d={path}
          fill="none"
          stroke={laneColor(lane)}
          strokeWidth={2}
          strokeLinecap="round"
        />
      ))}

      {Array.from({ length: count }, (_, offset) => {
        const row = first + offset;
        const lane = laneOf(row);
        if (lane === undefined) {
          return null;
        }
        return (
          <circle
            key={row}
            cx={columnCentre(lane)}
            cy={rowCentre(row, first)}
            r={DOT_RADIUS}
            fill={laneColor(lane)}
          />
        );
      })}
    </svg>
  );
}

/**
 * Joins every line of one column into a single `d` string.
 *
 * They share a colour, so they can share an element — which is what keeps the
 * element count proportional to the graph's width rather than to the number of
 * lines crossing the screen.
 *
 * Lines with nothing to show in these rows are left out first. The pages
 * loaded cover several hundred rows and the window is a few dozen, so most of
 * what arrives belongs to rows nobody is looking at: drawing it would put
 * kilobytes of path data on every frame of a scroll, clipped away by the
 * viewport. The daemon applies the same rule at page granularity, which is as
 * fine as it can be from where it stands.
 */
function batchByColumn(
  edges: GraphEdge[],
  first: number,
  count: number,
  total: number,
): Map<number, string> {
  const last = first + count;

  const paths = new Map<number, string>();
  for (const edge of edges) {
    if (edge.from >= last) {
      continue;
    }
    if (edge.to !== ABSENT_ROW && edge.to < first) {
      continue;
    }
    const drawn = edgePath(edgeShape(edge, first, total));
    paths.set(edge.lane, (paths.get(edge.lane) ?? '') + drawn);
  }
  return paths;
}
