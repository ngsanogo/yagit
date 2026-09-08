import { readFileSync } from 'node:fs';

import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { Button, type ButtonVariant } from './Button';

/**
 * The difference between refused and working, pinned.
 *
 * These two states used to be one attribute, and the cost was measured rather
 * than guessed: a browser blurs an element the moment it becomes disabled, so
 * pressing Fetch put a keyboard user on <body> for the length of the fetch and
 * delivered the result somewhere they were no longer standing. Nothing in the
 * end-to-end suite asserts on a transient loading state — every toBeDisabled()
 * there is a genuine refusal — so the invariant has no other guard.
 *
 * Rendered to markup rather than into a browser: what has to hold is which
 * attributes reach the element, and markup is the whole of that. The click
 * guard that goes with them is exercised end-to-end.
 */

const TOKENS = readFileSync(new URL('../design/tokens.css', import.meta.url), 'utf8');

/**
 * The utilities on the element, as a list.
 *
 * Asserted against the list rather than against the markup, because the two
 * names that matter here are one a substring of the other: `disabled:opacity-45`
 * is inside `aria-disabled:opacity-45`, the variant this file exists to keep out,
 * and a `toContain` on the whole string cannot tell them apart.
 */
function classesOf(html: string): string[] {
  const attribute = /class="([^"]*)"/.exec(html)?.[1];
  if (attribute === undefined) {
    throw new Error(`rendered without a class attribute: ${html}`);
  }
  return attribute.split(/\s+/).filter((utility) => utility !== '');
}

describe('a button that is working', () => {
  it('keeps its place in the tab order and says it is busy', () => {
    const html = renderToStaticMarkup(<Button loading>Fetch</Button>);

    expect(html).not.toContain('disabled=""');
    expect(html).toContain('aria-disabled="true"');
    expect(html).toContain('aria-busy="true"');
  });

  it('keeps its label, so the button can still be read while the wait runs', () => {
    expect(renderToStaticMarkup(<Button loading>Fetch</Button>)).toContain('Fetch');
  });

  it('is not dimmed like a refusal', () => {
    // The dim is bound to the attribute rather than to the state, and 45% of
    // any ink over the light theme's white is around 2:1. A control kept on
    // screen to be read through a minute-long push cannot be drawn that way.
    const classes = classesOf(renderToStaticMarkup(<Button loading>Fetch</Button>));

    expect(classes).toContain('disabled:opacity-45');
    expect(classes).not.toContain('aria-disabled:opacity-45');
  });
});

describe('a button that is refused', () => {
  it('takes the attribute, the dim and the dropped pointer events', () => {
    // Tooltip hangs the sentence explaining a refusal on the span around the
    // button precisely because these pointer events are gone.
    const html = renderToStaticMarkup(<Button disabled>Push</Button>);

    expect(html).toContain('disabled=""');
    expect(classesOf(html)).toContain('disabled:pointer-events-none');
    expect(html).not.toContain('aria-busy');
  });
});

describe('the danger variant', () => {
  it('states its hover and its press as tokens', () => {
    // A brightness filter knows nothing about the theme: on the light page it
    // moved the destructive button towards the paper while the primary button
    // beside it moved away, and it took the label's contrast down on the way.
    const html = renderToStaticMarkup(<Button variant="danger">Discard changes</Button>);

    expect(html).not.toContain('brightness');
    expect(classesOf(html)).toContain('hover:bg-danger-hover');
    expect(classesOf(html)).toContain('active:bg-danger-active');
  });
});

/**
 * Every ground a button paints is a colour the tokens file actually declares.
 *
 * Tailwind builds a colour utility from a theme variable, and generates
 * nothing at all for a name it cannot find — no error, no warning, no rule in
 * the stylesheet. So `hover:bg-danger-hover` against a tokens.css without that
 * token is not a wrong hover, it is no hover: the one button in the product
 * that destroys work answers the pointer with nothing, and every instrument
 * the project has stays green. This is the same class of silent failure the
 * assertions in tokens.css's own test were each written after.
 *
 * Both ways out of a failure here are fine. Declare the colour, or paint a
 * colour that exists — what must not survive is a component naming a token
 * nobody defined.
 */
describe('the colours the variants paint', () => {
  const variants: readonly ButtonVariant[] = ['primary', 'secondary', 'ghost', 'danger'];

  for (const variant of variants) {
    it(`names backgrounds tokens.css defines, for ${variant}`, () => {
      const painted = classesOf(renderToStaticMarkup(<Button variant={variant}>Run</Button>))
        // A utility is its variants and then itself: `hover:bg-accent-hover`.
        .map((utility) => utility.slice(utility.lastIndexOf(':') + 1))
        .filter((utility) => utility.startsWith('bg-'))
        // `bg-danger/35` is the same colour at another opacity.
        .map((utility) => utility.slice('bg-'.length).replace(/\/.*$/, ''));

      expect(painted, `the ${variant} button paints no background at all`).not.toHaveLength(0);
      expect(
        painted.filter((colour) => !TOKENS.includes(`--color-${colour}:`)),
        'tokens.css declares no colour under this name, so Tailwind emits no utility for it',
      ).toEqual([]);
    });
  }
});
