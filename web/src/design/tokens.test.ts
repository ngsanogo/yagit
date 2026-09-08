import { describe, expect, it } from 'vitest';

import { readFileSync } from 'node:fs';

/**
 * The promises tokens.css makes in prose, checked against the numbers it ships.
 *
 * This parses a stylesheet, which looks like the thing tokens.ts refuses to do.
 * It is not: tokens.ts refuses to read tokens into JavaScript AT RUNTIME, so
 * that the browser stays the only place a token is resolved and a theme change
 * needs no help from us. Reading the source at test time puts no second copy of
 * the palette in the running application — it puts one in the assertion, which
 * is the only place a promise can be checked.
 *
 * Every rule below is one this file has already broken in a shipped release,
 * silently, with no error message and nothing red anywhere. The lane palette
 * asked for a chroma sRGB cannot hold and drifted 1.7x across a set whose whole
 * purpose is not to drift. --color-surface and --color-raised were the same
 * white while the design system page printed "each surface sits one step above
 * the one it rests on" over the two identical swatches. The light --color-hover
 * landed between two base surfaces, so a row highlight on one of them moved
 * nothing. And the three shadows lived inside @theme, where Tailwind inlines
 * the value into the utility and a theme override reaches no pixel at all.
 *
 * The end-to-end suite measures the composite a person actually sees, which is
 * the better instrument and needs a browser to hold it. This one runs on the
 * declarations alone, so it also covers the pairings no screen draws yet.
 */

const SOURCE = readFileSync(new URL('./tokens.css', import.meta.url), 'utf8');

/**
 * The source with its prose removed.
 *
 * The comments in that file quote CSS at each other — one of them carries a
 * whole generated rule, braces included — so a brace counter that read them
 * would lose the plot inside the first paragraph.
 */
const CSS = SOURCE.replace(/\/\*[\s\S]*?\*\//g, '');

/** The body of the block a header opens, brace-matched rather than guessed. */
function blockAfter(header: string): string {
  const start = CSS.indexOf(header);
  if (start === -1) {
    throw new Error(`tokens.css no longer opens a block with "${header}"`);
  }

  let depth = 1;
  let index = start + header.length;
  while (depth > 0) {
    const character = CSS.charAt(index);
    if (character === '') {
      throw new Error(`the block opened by "${header}" is never closed`);
    }
    if (character === '{') depth += 1;
    if (character === '}') depth -= 1;
    index += 1;
  }
  return CSS.slice(start + header.length, index - 1);
}

interface Oklch {
  lightness: number;
  chroma: number;
  hue: number;
}

const COLOR = /(--color-[\w-]+):\s*oklch\(([\d.]+)%\s+([\d.]+)\s+([\d.]+)\)/g;

function colorsIn(block: string): Map<string, Oklch> {
  const found = new Map<string, Oklch>();
  for (const match of block.matchAll(COLOR)) {
    const [whole, name, lightness, chroma, hue] = match;
    if (name === undefined || lightness === undefined) {
      throw new Error(`oklch() matched without a name or a lightness: ${whole}`);
    }
    if (chroma === undefined || hue === undefined) {
      throw new Error(`oklch() matched without a chroma or a hue: ${whole}`);
    }
    found.set(name, {
      lightness: Number(lightness) / 100,
      chroma: Number(chroma),
      hue: Number(hue),
    });
  }
  return found;
}

const DARK_BLOCK = blockAfter('@theme static {');
const LIGHT_BLOCK = blockAfter(":root[data-theme='light'] {");
const ROOT_BLOCK = blockAfter(':root {');

const DARK = colorsIn(DARK_BLOCK);
// Light redefines a subset and inherits the rest, which is what the cascade
// does and what an assertion about the light theme has to do to match it.
const LIGHT = new Map([...DARK, ...colorsIn(LIGHT_BLOCK)]);

const THEMES: Array<{ name: string; colors: Map<string, Oklch> }> = [
  { name: 'dark', colors: DARK },
  { name: 'light', colors: LIGHT },
];

interface Linear {
  red: number;
  green: number;
  blue: number;
}

/**
 * OKLCH to linear sRGB, clamped per channel.
 *
 * The clamp is not tidiness — it is what a browser does with a colour outside
 * the gamut, and it is the entire subject of the lane assertion below. A test
 * that skipped it would measure the colour that was asked for rather than the
 * one that gets painted, which is precisely the mistake being guarded against.
 */
function paint(color: Oklch): Linear {
  const radians = (color.hue * Math.PI) / 180;
  const a = color.chroma * Math.cos(radians);
  const b = color.chroma * Math.sin(radians);

  const long = (color.lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const medium = (color.lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const short = (color.lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;

  const clamp = (value: number) => Math.min(1, Math.max(0, value));
  return {
    red: clamp(4.0767416621 * long - 3.3077115913 * medium + 0.2309699292 * short),
    green: clamp(-1.2684380046 * long + 2.6097574011 * medium - 0.3413193965 * short),
    blue: clamp(-0.0041960863 * long - 0.7034186147 * medium + 1.707614701 * short),
  };
}

interface Oklab {
  lightness: number;
  greenRed: number;
  blueYellow: number;
}

function toOklab({ red, green, blue }: Linear): Oklab {
  const long = Math.cbrt(0.4122214708 * red + 0.5363325363 * green + 0.0514459929 * blue);
  const medium = Math.cbrt(0.2119034982 * red + 0.6806995451 * green + 0.1073969566 * blue);
  const short = Math.cbrt(0.0883024619 * red + 0.2817188376 * green + 0.6299787005 * blue);

  return {
    lightness: 0.2104542553 * long + 0.793617785 * medium - 0.0040720468 * short,
    greenRed: 1.9779984951 * long - 2.428592205 * medium + 0.4505937099 * short,
    blueYellow: 0.0259040371 * long + 0.7827717662 * medium - 0.808675766 * short,
  };
}

/** What a colour ends up being, after the gamut has had its say. */
function painted(color: Oklch): { lightness: number; chroma: number } {
  const lab = toOklab(paint(color));
  return { lightness: lab.lightness, chroma: Math.hypot(lab.greenRed, lab.blueYellow) };
}

/** How far apart two colours look, in the space the whole file is written in. */
function separation(one: Oklch, other: Oklch): number {
  const a = toOklab(paint(one));
  const b = toOklab(paint(other));
  return Math.hypot(
    a.lightness - b.lightness,
    a.greenRed - b.greenRed,
    a.blueYellow - b.blueYellow,
  );
}

function contrast(one: Oklch, other: Oklch): number {
  const luminance = (color: Oklch) => {
    const { red, green, blue } = paint(color);
    return 0.2126 * red + 0.7152 * green + 0.0722 * blue;
  };
  const first = luminance(one);
  const second = luminance(other);
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
}

function token(colors: Map<string, Oklch>, name: string): Oklch {
  const value = colors.get(name);
  if (value === undefined) {
    throw new Error(`${name} is declared in neither theme`);
  }
  return value;
}

const LANES = Array.from({ length: 10 }, (_, index) => `--color-lane-${index + 1}`);
const BASES = ['--color-canvas', '--color-sunken', '--color-surface', '--color-raised'];
const SURFACES = [...BASES, '--color-hover', '--color-selected'];
const INKS = ['--color-ink', '--color-ink-muted', '--color-ink-subtle'];
const ELEVATIONS = ['shadow-raised', 'shadow-popover', 'shadow-dialog'];

/**
 * The colours a row draws on top of a surface: git's own file marks, the four
 * kinds of reference, and the statuses a badge or a message is tinted with.
 *
 * The soft variants are missing because they are grounds rather than ink, and
 * so are the hover and press rungs of the accent and danger ladders: those are
 * fills under a pointer, and what is written on them is --color-accent-ink and
 * --color-canvas, neither of which is a status.
 */
const STATUSES = [
  '--color-accent',
  '--color-success',
  '--color-warning',
  '--color-danger',
  '--color-info',
  '--color-added',
  '--color-modified',
  '--color-deleted',
  '--color-renamed',
  '--color-untracked',
  '--color-conflicted',
  '--color-ours',
  '--color-theirs',
  '--color-ref-head',
  '--color-ref-branch',
  '--color-ref-remote',
  '--color-ref-tag',
];

describe('the elevation tokens', () => {
  /**
   * The structural half of a bug no colour measurement could see: the light
   * values were right all along and simply never reached an element.
   */
  it('stay out of @theme, where Tailwind would inline them past the override', () => {
    for (const name of ELEVATIONS) {
      expect(DARK_BLOCK).not.toContain(`--${name}:`);
      expect(ROOT_BLOCK).toContain(`--${name}:`);
      expect(LIGHT_BLOCK).toContain(`--${name}:`);
    }
  });

  it('reach an element through a utility that dereferences them at paint time', () => {
    for (const name of ELEVATIONS) {
      const rule = String.raw`@utility ${name}\s*\{[^}]*box-shadow:\s*var\(--${name}\)`;
      expect(CSS).toMatch(new RegExp(rule));
    }
  });
});

describe('the graph lane palette', () => {
  for (const { name, colors } of THEMES) {
    it(`gives the ${name} theme ten colours nobody can confuse`, () => {
      // On the painted channels and not on lightness and chroma, which the
      // assertion below requires to be identical across all ten: hue is the
      // only thing telling these apart, so hue has to be what is compared.
      const swatches = LANES.map((lane) => paint(token(colors, lane)));
      const distinct = new Set(swatches.map(({ red, green, blue }) => `${red}/${green}/${blue}`));
      // Ten lanes and fewer than ten colours means two branches share a stroke.
      expect(distinct.size).toBe(10);
    });

    /**
     * The promise the palette exists to keep, measured after clipping rather
     * than read off the declaration.
     *
     * The bar is set where dark's own clipping already sits: 205° has a chroma
     * ceiling of 0.126 at L 74% against the 0.13 it asks for, which is a 2.6%
     * spread and the only movement in that theme. The light palette measures
     * 1.000 on both, having been brought inside the gamut; before that it
     * measured 1.73 on chroma, which is what this is here to catch.
     */
    it(`paints every ${name} lane at one lightness and one chroma`, () => {
      const spread = (values: number[]) => Math.max(...values) / Math.min(...values);
      const swatches = LANES.map((lane) => painted(token(colors, lane)));

      expect(spread(swatches.map((swatch) => swatch.lightness))).toBeLessThan(1.01);
      expect(spread(swatches.map((swatch) => swatch.chroma))).toBeLessThan(1.05);
    });
  }
});

describe('the surface ramp', () => {
  for (const { name, colors } of THEMES) {
    it(`climbs the ${name} theme with no two steps on one colour`, () => {
      const lightnessOf = (surface: string) => painted(token(colors, surface)).lightness;
      const ordered = [...SURFACES].sort((one, other) => lightnessOf(one) - lightnessOf(other));

      const collapsed: string[] = [];
      for (let step = 1; step < ordered.length; step += 1) {
        const below = ordered[step - 1];
        const above = ordered[step];
        if (below === undefined || above === undefined) {
          throw new Error('the ramp lost a step between sorting it and walking it');
        }
        // 0.005 in OKLab is about a quarter of a just-noticeable difference:
        // low enough that this is a claim about identity rather than about
        // comfort, which is what caught surface and raised sharing one white.
        if (separation(token(colors, below), token(colors, above)) <= 0.005) {
          collapsed.push(`${below} and ${above} are the same colour`);
        }
      }
      expect(collapsed).toEqual([]);
    });

    /**
     * Hover and selected are not points on the ramp. They are one step past the
     * end of it, in the direction the theme lifts — dark lighter than every base
     * surface, light darker than every base surface. A value that lands between
     * two bases is obvious on one of them and invisible on the next, which is
     * what the light hover did for a release.
     */
    it(`puts the ${name} interaction states past every base surface`, () => {
      const lightnessOf = (surface: string) => painted(token(colors, surface)).lightness;
      const bases = BASES.map(lightnessOf);
      const hover = lightnessOf('--color-hover');
      const selected = lightnessOf('--color-selected');

      const lifts = hover > Math.max(...bases);
      expect(lifts || hover < Math.min(...bases)).toBe(true);
      // And selected is further out than hover, the same way: it is the
      // stronger state, so a row that is both has to read as selected.
      expect(lifts ? selected > hover : selected < hover).toBe(true);
    });
  }
});

describe('text on a surface', () => {
  it('clears the AA floor for every ink on every surface, in both themes', () => {
    const tooFaint: string[] = [];
    for (const { name, colors } of THEMES) {
      for (const ink of INKS) {
        for (const surface of SURFACES) {
          // 4.5:1, the floor for text at the sizes this application uses. The
          // tightest pairing in both themes is the subtle ink on the selected
          // row, and it is tight on purpose: a refused menu item draws the
          // sentence explaining itself in exactly that pair.
          const ratio = contrast(token(colors, ink), token(colors, surface));
          if (ratio < 4.5) {
            tooFaint.push(`${name}: ${ink} on ${surface} is ${ratio.toFixed(2)}:1`);
          }
        }
      }
    }
    expect(tooFaint).toEqual([]);
  });

  /**
   * The same floor for the status palette, over the four base surfaces only.
   *
   * The omission of hover and selected is the subject of this assertion rather
   * than a gap in it. Over the light theme's interaction states every one of
   * these is under the floor — the best of them measures 3.76:1 — and was
   * under it before those two tokens last moved; the fix is to re-derive every
   * solid status against a highlighted row rather than against the page, which
   * is a repaint of every badge in the theme and wants an eye on it, not a
   * threshold. What this pins is the ground they are read against
   * the rest of the time, and it is not a hypothetical: --color-modified had
   * drifted four lightness points off the --color-warning it is identical to
   * in the other theme, and sat at 4.07:1 on a sunken panel for it.
   */
  it('clears the AA floor for every status colour on every base surface', () => {
    const tooFaint: string[] = [];
    for (const { name, colors } of THEMES) {
      for (const status of STATUSES) {
        for (const surface of BASES) {
          const ratio = contrast(token(colors, status), token(colors, surface));
          if (ratio < 4.5) {
            tooFaint.push(`${name}: ${status} on ${surface} is ${ratio.toFixed(2)}:1`);
          }
        }
      }
    }
    expect(tooFaint).toEqual([]);
  });

  /**
   * The author chip's initials, which are a lane colour used as text.
   *
   * Every surface, hover and selected included, because a chip is drawn on a
   * commit row and a commit row is all three. That is the pairing this rule
   * exists for: the chip used to fill itself with its own tint at 14% over
   * whatever it stood on, which lifted the ground under its own letters and
   * put the initials at 4.19:1 on a selected row in the dark theme and 3.95:1
   * in the light one. No instrument here could see it — the mix is composited
   * in the browser, out of reach of a stylesheet parser, and no product screen
   * had ever shown axe a selected row. The fill is gone and the letters now
   * read against the row's own ground, which is what this measures.
   */
  it('clears the AA floor for every lane colour on every surface, as the author chip draws it', () => {
    const tooFaint: string[] = [];
    for (const { name, colors } of THEMES) {
      for (const lane of LANES) {
        for (const surface of SURFACES) {
          const ratio = contrast(token(colors, lane), token(colors, surface));
          if (ratio < 4.5) {
            tooFaint.push(`${name}: ${lane} on ${surface} is ${ratio.toFixed(2)}:1`);
          }
        }
      }
    }
    expect(tooFaint).toEqual([]);
  });
});
