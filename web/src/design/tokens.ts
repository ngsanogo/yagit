/**
 * The handful of design tokens TypeScript needs to know the names of.
 *
 * Everything else comes from `tokens.css`, and components reach it through
 * the Tailwind utilities generated from it. Only one case cannot: a colour
 * chosen by data rather than by design — a lane's colour, an author's tint —
 * has no class name to write, so it is applied as `var(--color-lane-N)` in an
 * inline style.
 *
 * There is deliberately no function here that reads a computed value back out
 * of the document. The graph is SVG (docs/adr/0003), so it takes the variable
 * itself and lets the browser resolve it — including when the theme changes.
 * Reading tokens into JavaScript would put a second copy of the palette in
 * memory, and the first one is the only one.
 */

/** How many distinct graph lanes there are; past that, the palette cycles. */
export const LANE_COLOR_COUNT = 10;

/**
 * The colour of a lane, as a CSS reference usable straight from an inline
 * style or an SVG attribute.
 *
 * It depends on the index alone, so it is stable over time: a branch does not
 * change colour as you scroll. Past LANE_COLOR_COUNT the palette cycles, and
 * the tokens' order is chosen so that the wrap puts two distant hues side by
 * side rather than two neighbours.
 *
 * A function rather than an array lookup because every caller indexes it by
 * data — a column, a name's hash — and an index into an array is a value that
 * may not be there. This one always answers a colour.
 */
export function laneColor(lane: number): string {
  const index = ((lane % LANE_COLOR_COUNT) + LANE_COLOR_COUNT) % LANE_COLOR_COUNT;
  return `var(--color-lane-${index + 1})`;
}
