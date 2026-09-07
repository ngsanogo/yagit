# 0003 — The commit graph is virtualised SVG

**Status:** reverses the earlier decision to draw it on a 2D canvas.

## What was decided before, and why it does not hold

> **Graph**: 2D canvas. Neither SVG nor DOM — both collapse past a few thousand
> commits.

The premise is true and the conclusion does not follow, because of a decision
taken separately: **the commit list is virtualised** (phase 3). Only the rows
in the viewport are ever rendered — sixty of them, not a hundred thousand. The
graph is a column beside those rows and draws exactly the same window.

So SVG is never asked to hold a few thousand nodes. It is asked to hold about
as many as the rows themselves, and those rows are DOM already, with an author,
a date, a subject and a set of ref badges each. Beside that, the graph is a
rounding error. The canvas decision was made about a problem virtualisation had
already removed.

## The decision

The graph is SVG, rendered for the visible window only, batched **one `<path>`
per lane** rather than one element per edge: a lane's segments across sixty
rows join into a single `d` string. Sixty commit dots and ten paths is the
whole picture.

## Why that is better here, and not merely equal

**It is the design system, without a bridge.** A lane's colour is
`stroke: var(--color-lane-3)`. Nothing reads a token at runtime, nothing
converts it, nothing re-reads it when the theme changes — the browser
recalculates the stroke because that is what a custom property does. Canvas
takes colour strings, so it needed `readDesignToken()` and `readLanePalette()`
to pull computed values back out of the document, plus a listener to redraw on
a theme switch. That bridge is deleted by this decision. Given `tokens.css` is
described as "the single source of every visual value", the option that keeps
no second copy of the palette is the one that matches.

**Hit testing is free.** Clicking a commit dot, hovering an edge to highlight a
branch, a tooltip anchored to a merge point: `onClick` on the element. Canvas
needs a parallel geometry model in JavaScript and manual coordinate arithmetic,
kept in sync with the drawing code by hand — a second source of truth for the
shape of the picture.

**No device-pixel-ratio arithmetic.** A canvas has to be sized in CSS pixels
and scaled by `devicePixelRatio`, redone on every resize and on every drag
between a laptop screen and an external monitor. Getting it wrong is a blurry
graph, and it is the single most common defect in canvas interfaces. SVG is
resolution independent by construction.

**It is inspectable.** A wrong lane is an element in the developer tools with
the wrong `stroke`. On canvas it is a pixel, and the only debugger is
`console.log`.

## What it costs

**Reconciliation on scroll.** Seventy elements re-diffed per frame instead of
one imperative draw call. Measured against the rows that must be DOM anyway,
this is a fraction of the frame the list already spends. The moment it stops
being one, the answer is not canvas — it is to stop re-creating the paths and
translate them, which SVG also allows.

**A whole-history minimap will want canvas.** A strip showing all 100 000
commits at once genuinely cannot be DOM. That is a different component, drawing
no text and needing no hit testing, and it gets its own decision when it
exists. It is not a reason to draw the main graph that way today.

## Consequences

- `web/src/design/tokens.ts` loses `readDesignToken`, `readLanePalette` and
  `laneColor`. `LANE_COLOR_COUNT` and `LANE_COLOR_VARIABLES` stay: components
  that colour by data still apply `var(--color-lane-N)` as an inline style.
- Lane assignment is unaffected. It is a pure function from commits to lane
  indices, tested on its own, and it does not know what draws its output.
- The graph column is `aria-hidden`. It is a picture of the relationships
  between rows that are already in the accessibility tree with their author,
  date and subject; announcing a lane number would add noise, not information.
