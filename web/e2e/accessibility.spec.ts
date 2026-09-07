import { expect, test, type Page } from '@playwright/test';

import { violations } from './accessibility';
import { openDesignSystem } from './session';

/**
 * The tests in design-system.spec.ts pin one contract each. That is the right
 * way to keep a fix fixed, and the wrong way to find the next one — each test
 * only knows what its author already knew.
 *
 * axe-core is the other half. It runs the WCAG rule set against the real
 * rendered DOM, so it reads what a screen reader would read rather than what
 * the JSX says, and it reports the whole page rather than the part someone
 * thought to check.
 *
 * There is deliberately no eslint-plugin-jsx-a11y beside it. No published
 * version accepts eslint 10, which this project runs, and forcing the peer
 * dependency leaves a silent hole: the plugin reads JSX, so a missing
 * accessible name that comes from a runtime prop, a contrast ratio, or a focus
 * order is invisible to it and is precisely what axe measures here.
 */

async function openShowcase(page: Page) {
  await openDesignSystem(page);
  // The scan has to run against a mounted page: axe on an empty root finds
  // nothing and passes, which is the most expensive kind of green.
  await expect(page.getByRole('heading', { name: 'Type scale' })).toBeVisible();
}

test('the design system has no accessibility violations, in the dark theme', async ({ page }) => {
  await openShowcase(page);

  expect(await violations(page)).toEqual([]);
});

test('the design system has no accessibility violations, in the light theme', async ({ page }) => {
  await openShowcase(page);
  await page.getByRole('button', { name: 'Light' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  // The light theme is checked as strictly as the dark one. Contrast is the
  // rule most likely to differ between them, and it is the one a theme with an
  // unfinished pass fails silently — the page still renders, it is just
  // unreadable for some of the people looking at it.
  expect(await violations(page)).toEqual([]);
});

test('an open dialog has no accessibility violations', async ({ page }) => {
  await openShowcase(page);
  await page.getByRole('button', { name: 'Open a destructive confirmation' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();

  // A modal is where accessibility goes wrong worst: it is the state a screen
  // reader user cannot leave, and the state nobody scans because it takes a
  // click to reach.
  expect(await violations(page)).toEqual([]);
});

test('an open row menu has no accessibility violations, in either theme', async ({ page }) => {
  await openShowcase(page);

  // A menu is a small tree of roles that exists only while it is open — menu
  // owning menuitem, named, over the page rather than in it — and none of it is
  // in the DOM for the whole-page scans above to read.
  //
  // On the Delete item of two different rows, and in both themes, because
  // that is where the colours are: text-danger over its own danger-soft
  // highlight is a pairing that appears nowhere else, and contrast is the rule
  // most likely to differ between a theme and the one it was designed against.
  //
  // main's Delete is the same item REFUSED — git will not delete the branch
  // HEAD is on — which is a second set of roles, names and descriptions over
  // the same colours. What this scan does NOT cover there is the contrast:
  // axe exempts a control marked aria-disabled from the rule, on the reasoning
  // that nobody needs to read a control they cannot use. That reasoning does
  // not hold for this menu, which keeps the item precisely so it can be read,
  // so the sentence gets its own measured test below.
  const scanTheOpenMenu = async (row: string) => {
    await page.getByRole('button', { name: `More actions for ${row}` }).click();
    await expect(page.getByRole('menu')).toBeVisible();

    // Opening lands on the first item; one step reaches Delete, on both rows.
    await page.keyboard.press('ArrowDown');
    const destructive = page.getByRole('menuitem', { name: 'Delete…', exact: true });
    await expect(destructive).toBeFocused();

    // Waited out, not slept through. The highlight arrives over 90ms of
    // transition, and axe reads the background it finds at the moment it runs:
    // scanning too early measured text-danger against the menu's own surface
    // and passed, which is the pairing this scan is not about.
    await destructive.evaluate((element) =>
      Promise.all(element.getAnimations().map((animation) => animation.finished)),
    );

    expect(await violations(page)).toEqual([]);

    await page.keyboard.press('Escape');
    await expect(page.getByRole('menu')).toHaveCount(0);
  };

  const scanBothRows = async () => {
    await scanTheOpenMenu('feature/lane-assignment');
    await scanTheOpenMenu('main');
  };

  await scanBothRows();

  // The toggle names the theme it switches to, not the one showing.
  await page.getByRole('button', { name: 'Light' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  await scanBothRows();
});

/**
 * The one thing axe will not measure here, measured.
 *
 * A refused menu item exists to be read: it stays on the list, the arrows land
 * on it, and it draws the sentence saying why the action is off — on the item
 * rather than in a tooltip, because a menu clips overflow. axe skips contrast
 * on anything aria-disabled, so every one of those sentences could go to 1.7:1
 * with the suite green, and one of them did: `aria-disabled:opacity-45` on the
 * button dimmed the sentence with it, and 45% of any ink over this menu's
 * surface is unreadable in the light theme.
 *
 * Measured rather than asserted against a class name. What matters is the
 * ratio a person sees, which is the composite of the text's own colour, every
 * opacity between it and the page, and whatever actually paints behind it —
 * and a test that checked for `text-ink-subtle` would have passed throughout.
 */
test('the sentence on a refused menu item is readable, in both themes', async ({ page }) => {
  await openShowcase(page);

  const readTheReason = async () => {
    await page.getByRole('button', { name: 'More actions for main' }).click();
    const refused = page.getByRole('menuitem', { name: 'Delete…', exact: true });
    await expect(refused).toHaveAttribute('aria-disabled', 'true');

    const reason = refused.getByText('HEAD is on main, so it cannot be deleted');
    await expect(reason).toBeVisible();

    const ratio = await reason.evaluate((node: HTMLElement) => {
      // The browser's own colour conversion rather than a reimplementation of
      // it. Computed styles come back in whatever space the token was written
      // in — oklch() throughout this design system — so pulling three numbers
      // out of the string and treating them as RGB reports a ratio of 1.0003
      // for every pairing on the page. A canvas resolves and composites the
      // same way the compositor does.
      const canvas = document.createElement('canvas');
      canvas.width = 2;
      canvas.height = 1;
      const ctx = canvas.getContext('2d', { willReadFrequently: true });
      if (ctx === null) {
        throw new Error('no 2d context to resolve colours in');
      }

      const sample = (colour: string, alpha: number, x: number, over?: string) => {
        ctx.clearRect(x, 0, 1, 1);
        ctx.globalAlpha = 1;
        if (over !== undefined) {
          ctx.fillStyle = over;
          ctx.fillRect(x, 0, 1, 1);
        }
        ctx.globalAlpha = alpha;
        ctx.fillStyle = colour;
        ctx.fillRect(x, 0, 1, 1);
        return Array.from(ctx.getImageData(x, 0, 1, 1).data);
      };

      // A canvas that cannot read oklch() would report every colour as fully
      // transparent, and this test would then measure white on white and pass.
      if ((sample('oklch(50% 0.1 200)', 1, 0)[3] ?? 0) !== 255) {
        throw new Error('this browser cannot resolve oklch() on a canvas');
      }

      // Every opacity between the text and the page multiplies, and none of it
      // appears in the element's own computed colour.
      let opacity = 1;
      let surface = 'white';
      let painted = false;

      for (let el: HTMLElement | null = node; el !== null; el = el.parentElement) {
        const style = getComputedStyle(el);
        opacity *= Number(style.opacity);

        // The first ancestor that actually paints is what shows through the
        // text; everything above it is covered by that one.
        if (!painted && (sample(style.backgroundColor, 1, 0)[3] ?? 0) > 0) {
          surface = style.backgroundColor;
          painted = true;
        }
      }

      const behind = sample(surface, 1, 0);
      const read = sample(getComputedStyle(node).color, opacity, 1, surface);

      const luminance = (pixel: number[]) => {
        const [r = 0, g = 0, b = 0] = pixel;
        const linear = (value: number) => {
          const channel = value / 255;
          return channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
        };
        return 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);
      };

      const high = Math.max(luminance(read), luminance(behind));
      const low = Math.min(luminance(read), luminance(behind));
      return (high + 0.05) / (low + 0.05);
    });

    await page.keyboard.press('Escape');
    await expect(page.getByRole('menu')).toHaveCount(0);
    return ratio;
  };

  // 4.5:1, the AA floor for text this size: the reason is 11px, which is
  // nowhere near the 18.66px that would let 3:1 stand in.
  expect(await readTheReason()).toBeGreaterThanOrEqual(4.5);

  await page.getByRole('button', { name: 'Light' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  // The theme that failed. Dark got 3.2:1 under the old dimming and light got
  // 1.6:1, so a single-theme test would have called this half fixed.
  expect(await readTheReason()).toBeGreaterThanOrEqual(4.5);
});
