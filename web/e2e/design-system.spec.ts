import { expect, test, type Page } from '@playwright/test';

import { openDesignSystem, openWorkbench } from './session';

/**
 * What these tests check, and what unit tests cannot reach: that the whole
 * chain holds up — daemon, proxy to Vite, token exchange, React mounting,
 * design tokens applied.
 */

test('opens on a session cookie, with no token in the address', async ({ page }) => {
  // The workbench, not the design system: this is about where a person lands.
  await openWorkbench(page);

  await expect(page.getByRole('heading', { name: 'yagit', level: 1 })).toBeVisible();

  // The cookie is the whole credential. Nothing puts the secret in the address
  // bar, so nothing has to remember to take it out again.
  expect(new URL(page.url()).searchParams.has('token')).toBe(false);
});

test('renders the design system with no console errors', async ({ page }) => {
  const problems: string[] = [];
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
  page.on('console', (message) => {
    if (message.type() !== 'error') {
      return;
    }
    const from = message.location().url;

    // One daemon serves every worker, and a repository is addressed by an
    // opaque id that stops existing the moment somebody closes it. A sibling
    // test doing that turns this page's requests for the refs, the commits and
    // the status of that repository into three 404s the browser logs by
    // itself. It is the suite sharing a daemon — and, in real use, a second
    // yagit window — not the design system rendering badly.
    if (/\/api\/repos\/[^/]+\//.test(from) && message.text().includes('404')) {
      return;
    }

    // With the resource, not just the status. "Failed to load resource: 404"
    // on its own names nothing, and the first time this test caught something
    // real the message was the same twelve words three times over.
    problems.push(`console: ${message.text()} <- ${from}`);
  });

  await openDesignSystem(page);
  await expect(page.getByRole('heading', { name: 'Type scale' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Graph lanes' })).toBeVisible();

  expect(problems).toEqual([]);
});

/**
 * The tokens have to exist as real CSS variables: the graph canvas reads them
 * at runtime. A missing variable would give transparent strokes: an invisible
 * graph, and no error message.
 */
test('exposes the ten lane colors as CSS variables', async ({ page }) => {
  await openDesignSystem(page);

  const laneColors = await page.evaluate(() => {
    const styles = getComputedStyle(document.documentElement);
    return Array.from({ length: 10 }, (_, index) =>
      styles.getPropertyValue(`--color-lane-${index + 1}`).trim(),
    );
  });

  expect(laneColors.filter((color) => color === '')).toEqual([]);
  expect(new Set(laneColors).size).toBe(10);
});

test('switches between the two themes', async ({ page }) => {
  await openDesignSystem(page);

  await page.getByRole('button', { name: 'Light' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

  await page.getByRole('button', { name: 'Dark' }).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

/**
 * The project's central promise: no destructive operation without showing the
 * exact command and naming what will be lost.
 */
test('a destructive confirmation shows the exact command and what will be lost', async ({
  page,
}) => {
  await openDesignSystem(page);

  await page.getByRole('button', { name: 'Open a destructive confirmation' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('git reset --hard origin/main');
  await expect(dialog).toContainText('This will permanently discard');
  await expect(dialog).toContainText('3 local commits');
});

/**
 * The base components carry the accessibility of every screen that will use
 * them, so their contract is pinned here rather than left to a later audit.
 * Each of these failed before the fix that introduced it.
 */

test('the confirmation dialog carries an accessible name', async ({ page }) => {
  await openDesignSystem(page);
  await page.getByRole('button', { name: 'Open a destructive confirmation' }).click();

  // Chromium reported role "dialog" with an empty name: a screen reader
  // announced "dialog" and nothing else.
  await expect(page.getByRole('dialog', { name: 'Reset main to origin/main?' })).toBeVisible();

  // <header> and <footer> inside a dialog map to the banner and contentinfo
  // landmarks — page-level landmarks, announced from inside a modal. Scoped to
  // the dialog: the page has a banner of its own, and that one is legitimate.
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('banner')).toHaveCount(0);
  await expect(dialog.getByRole('contentinfo')).toHaveCount(0);
});

test('Escape closes the confirmation, once', async ({ page }) => {
  await openDesignSystem(page);
  await page.getByRole('button', { name: 'Open a destructive confirmation' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();

  // Reopening has to work: a dialog that mismanages its own close ends up
  // stuck, and the second open is where that shows.
  await page.getByRole('button', { name: 'Open a destructive confirmation' }).click();
  await expect(dialog).toBeVisible();
});

test('the tab bar is one stop, and the arrows move within it', async ({ page }) => {
  await openDesignSystem(page);

  const tabs = page.getByRole('tab');
  // A tablist is a single stop in the page's tab order — the selected tab —
  // and the arrows walk the rest. All of them tabbable meant ten open
  // repositories cost ten presses of Tab to walk past.
  await expect(tabs.filter({ has: page.locator('[tabindex="0"]') })).toHaveCount(0);
  await expect(tabs.first()).toHaveAttribute('tabindex', '0');
  await expect(tabs.nth(1)).toHaveAttribute('tabindex', '-1');

  await tabs.first().focus();
  await page.keyboard.press('ArrowRight');
  await expect(tabs.nth(1)).toBeFocused();

  await page.keyboard.press('End');
  await expect(tabs.last()).toBeFocused();

  // Wraps: a dead end at the last tab is what people report as "the arrows
  // stopped working".
  await page.keyboard.press('ArrowRight');
  await expect(tabs.first()).toBeFocused();
});

test('the tablist owns nothing but tabs, and Delete is why that is enough', async ({ page }) => {
  await openDesignSystem(page);

  const tablist = page.getByRole('tablist');

  // The wrapper around each tab is presentational, which promotes its children
  // into the tablist — including the close button beside each tab. A tablist
  // may own nothing but tabs, and Chromium said so: "Element has children which
  // are not allowed: button[aria-label]".
  await expect(tablist.getByRole('button')).toHaveCount(0);
  await expect(tablist.getByRole('tab')).not.toHaveCount(0);

  // The other half of the same decision, asserted together on purpose. Hiding
  // the close button is only acceptable while the keyboard has its own way to
  // close a tab; remove the shortcut and this test says so.
  await expect(tablist.getByRole('tab').first()).toHaveAttribute('aria-keyshortcuts', 'Delete');
});

test('the segmented control is one stop, and the arrows choose within it', async ({ page }) => {
  await openDesignSystem(page);

  const group = page.getByRole('radiogroup', { name: 'Refs the graph is drawn from' });
  const options = group.getByRole('radio');

  // A radio group is a single stop in the page's tab order — the checked
  // option — and the arrows walk the rest. Two options tabbable would be two
  // presses of Tab to walk past a question with one answer.
  await expect(options.first()).toHaveAttribute('tabindex', '0');
  await expect(options.last()).toHaveAttribute('tabindex', '-1');

  await options.first().focus();
  await page.keyboard.press('ArrowRight');

  // The arrows select as they move, which is this pattern's own rule: for a
  // radio group the selection is the focus, and one that needed Space after
  // every arrow would leave a state nothing had chosen.
  await expect(options.last()).toBeFocused();
  await expect(options.last()).toBeChecked();

  // Wraps: a dead end at the last option is what people report as "the arrows
  // stopped working".
  await page.keyboard.press('ArrowRight');
  await expect(options.first()).toBeChecked();

  // Home and End, which the pattern also promises. Two options make them look
  // like a slower ArrowLeft and ArrowRight; the group is written to take three
  // and they are the only way to cross one in a press.
  await page.keyboard.press('End');
  await expect(options.last()).toBeChecked();
  await page.keyboard.press('Home');
  await expect(options.first()).toBeChecked();
});

/**
 * The row menu, and what a menu has to get right.
 *
 * Every one of these is a contract the popover API does not hand over on its
 * own: it gives the top layer, Escape and light dismiss, and leaves placement,
 * the arrow keys and the meaning of a second click on the trigger to be
 * written — which is where menus go wrong.
 *
 * The showcase draws two reference rows. `main` is the one HEAD is on, so its
 * Delete is refused; the feature branch offers both of its actions.
 */
const ROW = 'feature/lane-assignment';

test('the row menu opens on the keyboard, and Escape gives focus back', async ({ page }) => {
  await openDesignSystem(page);

  const trigger = page.getByRole('button', { name: `More actions for ${ROW}` });
  // Nothing is announced before it opens. A menu rendered and hidden by
  // opacity would be in the tree the whole time, offering a Delete on every
  // row a screen reader walked past.
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');

  // Down opens it and lands on the first item, which is what aria-haspopup
  // promised on the reader's behalf.
  await trigger.focus();
  await page.keyboard.press('ArrowDown');

  const items = page.getByRole('menuitem');
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  await expect(items.first()).toBeFocused();

  await page.keyboard.press('ArrowDown');
  await expect(items.last()).toBeFocused();

  // Wraps, like every other list in this design system: a dead end at the end
  // is what people report as "the arrows stopped working".
  await page.keyboard.press('ArrowDown');
  await expect(items.first()).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(items.last()).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  // And focus is back on the button rather than at the top of the document,
  // which is where a menu that forgets to put it back leaves the reader.
  await expect(trigger).toBeFocused();
});

test('the up arrow opens the row menu at its last item', async ({ page }) => {
  await openDesignSystem(page);

  // Both arrows open a menu button, at the end each one points to. Reaching
  // the last item of a five-item menu by pressing Down five times is how a
  // keyboard user learns to stop using the keyboard.
  await page.getByRole('button', { name: `More actions for ${ROW}` }).focus();
  await page.keyboard.press('ArrowUp');

  await expect(page.getByRole('menuitem').last()).toBeFocused();
});

test('Enter opens the row menu, and Enter takes the item it lands on', async ({ page }) => {
  await openDesignSystem(page);

  const trigger = page.getByRole('button', { name: `More actions for ${ROW}` });
  await trigger.focus();

  // A keyboard press on a button arrives as a click with no pointer behind it,
  // which is the other half of the toggle the test below pins. Getting one of
  // the two right is how a menu ends up openable only with a mouse.
  await page.keyboard.press('Enter');
  await expect(page.getByRole('menuitem').first()).toBeFocused();

  await page.keyboard.press('Enter');
  await expect(page.getByRole('menu')).toHaveCount(0);
  // Taking an item is not a dead end either: whatever the reader does next
  // starts from the row they were on.
  await expect(trigger).toBeFocused();
});

test('a second click on the trigger closes the row menu', async ({ page }) => {
  await openDesignSystem(page);

  const trigger = page.getByRole('button', { name: `More actions for ${ROW}` });

  await trigger.click();
  await expect(page.getByRole('menu')).toBeVisible();

  // The regression this pins: the browser light-dismisses the popover on
  // pointerup, so by the time the click arrives the menu is already closed. A
  // trigger that toggled on what it could see would open it straight back up,
  // and the button would never close its own menu.
  await trigger.click();
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
});

/**
 * Puts a row where the placement being tested is the one that has to happen.
 *
 * Not `scrollIntoViewIfNeeded`: it scrolls by the least it can get away with,
 * so a row it leaves flush against the bottom of the window gets its menu
 * above — correct, and the opposite of what a test naming "below" expects.
 */
async function scrollRowTo(page: Page, where: 'center' | 'end') {
  await page
    .getByRole('button', { name: `More actions for ${ROW}` })
    .evaluate((element, block) => element.scrollIntoView({ block }), where);
}

test('the row menu is drawn over the page, not inside the row', async ({ page }) => {
  await openDesignSystem(page);
  await scrollRowTo(page, 'center');

  const trigger = page.getByRole('button', { name: `More actions for ${ROW}` });
  await trigger.click();

  const menu = page.getByRole('menu');
  await expect(menu).toBeVisible();

  // The top layer is the whole reason this is a popover: the lists it opens
  // over scroll inside their panel, and a menu drawn in that flow is cut off
  // by the first overflow above it. `:popover-open` is the browser's own
  // answer to whether it is there.
  expect(await menu.evaluate((element) => element.matches(':popover-open'))).toBe(true);

  // And placed against its row, rather than left where the top layer puts a
  // popover nobody positioned: the corner of the window. Below the trigger,
  // right edges aligned, which is the corner a row's actions sit in.
  const anchor = await trigger.boundingBox();
  const box = await menu.boundingBox();
  if (anchor === null || box === null) {
    throw new Error('the trigger and its menu are both on screen at this point');
  }

  expect(box.y).toBeGreaterThanOrEqual(anchor.y + anchor.height);
  expect(box.y - (anchor.y + anchor.height)).toBeLessThan(8);
  expect(box.x + box.width).toBeCloseTo(anchor.x + anchor.width, 0);
});

test('a row at the bottom of the window opens its menu upwards', async ({ page }) => {
  await openDesignSystem(page);
  await scrollRowTo(page, 'end');

  const trigger = page.getByRole('button', { name: `More actions for ${ROW}` });
  await trigger.click();

  const menu = page.getByRole('menu');
  await expect(menu).toBeVisible();

  // Below is the default and not the rule. A menu drawn below the last row on
  // screen is a menu off the bottom of the window, and the popover is in the
  // top layer, where the page cannot scroll to reach it.
  const anchor = await trigger.boundingBox();
  const box = await menu.boundingBox();
  if (anchor === null || box === null) {
    throw new Error('the trigger and its menu are both on screen at this point');
  }

  expect(box.y + box.height).toBeLessThanOrEqual(anchor.y);
  expect(box.y).toBeGreaterThanOrEqual(0);
});

test('a scroll closes the row menu rather than leaving it behind', async ({ page }) => {
  await openDesignSystem(page);

  await page.getByRole('button', { name: `More actions for ${ROW}` }).click();
  await expect(page.getByRole('menu')).toBeVisible();

  // Placed once, against where the row was. Following the row would still owe
  // an answer for the row leaving its panel, and a menu floating over a
  // different row is a worse answer than no menu.
  await page.evaluate(() => window.scrollBy(0, 240));
  await expect(page.getByRole('menu')).toHaveCount(0);
});

test('Tab leaves the row menu rather than being trapped inside it', async ({ page }) => {
  await openDesignSystem(page);

  await page.getByRole('button', { name: 'More actions for main' }).focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('menuitem').first()).toBeFocused();

  // A menu is a detour, not a place to live. Trapping focus in one is what
  // turns "I opened this by mistake" into "I cannot get out of this", and the
  // tab order behind it is where the reader was going: the next row.
  await page.keyboard.press('Tab');
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(page.getByRole('button', { name: `More actions for ${ROW}` })).toBeFocused();
});

test('the arrows reach an action the row refuses, and it does nothing', async ({ page }) => {
  await openDesignSystem(page);

  // main is checked out, and git will not delete the branch HEAD is on.
  const trigger = page.getByRole('button', { name: 'More actions for main' });
  await trigger.focus();
  await page.keyboard.press('ArrowDown');

  const rename = page.getByRole('menuitem', { name: 'Rename…' });
  // exact, and it is the assertion rather than tidiness. A menuitem is named
  // by its contents, so the reason drawn under the label joined the accessible
  // name and every refused item in the application was quietly renamed:
  // "Delete… HEAD is on main, so it cannot be deleted" — announced, and then
  // announced again as the description. Only Playwright's substring default
  // kept this suite green over it. aria-label is what puts the name back and
  // leaves the sentence to aria-describedby.
  const refused = page.getByRole('menuitem', { name: 'Delete…', exact: true });
  await expect(rename).toBeFocused();

  // Reached, not skipped. The arrows are the only way through a menu, so an
  // item they step over is one a screen reader can never be told about — and
  // "why can I not delete this branch" is exactly the question the refused
  // item is there to answer. aria-disabled is what says so; the disabled
  // attribute would have taken the item out of the keyboard's reach to say it.
  await page.keyboard.press('ArrowDown');
  await expect(refused).toBeFocused();
  await expect(refused).toHaveAttribute('aria-disabled', 'true');

  // And the sentence is on the item, where it can be read — drawn rather than
  // hidden in a tooltip a menu would clip, and pointed at by aria-describedby
  // so it is said once as a description and not twice as a name.
  const reason = refused.getByText('HEAD is on main, so it cannot be deleted');
  await expect(reason).toBeVisible();
  await expect(refused).toHaveAttribute('aria-describedby', await reason.evaluate((s) => s.id));

  // And it does nothing when taken — including not closing the menu, which
  // would read as an action that silently failed.
  await page.keyboard.press('Enter');
  await expect(page.getByRole('menu')).toBeVisible();
  await expect(refused).toBeFocused();
});

test('a row with no actions has no menu button to press', async ({ page }) => {
  await openDesignSystem(page);

  // The showcase gives one row an empty list. A menu with nothing in it would
  // open onto nothing: focus has no item to land on, and the arrow keys are
  // read by the trigger only while it is closed — so the popover would sit
  // there with no way in and no way out but Escape. Refused at the button,
  // where a person can see it.
  await expect(page.getByRole('button', { name: 'More actions for HEAD' })).toBeDisabled();
});

test('Shift+Tab out of the row menu lands back on its button', async ({ page }) => {
  await openDesignSystem(page);

  const trigger = page.getByRole('button', { name: `More actions for ${ROW}` });
  await trigger.focus();
  await page.keyboard.press('ArrowDown');
  await expect(page.getByRole('menuitem').first()).toBeFocused();

  // Backwards is not forwards. Tab carries on into the page past the trigger,
  // which is where a reader going that way was headed; Shift+Tab is somebody
  // backing out, and stepping over the button they opened the menu with would
  // lose them the place they started from.
  await page.keyboard.press('Shift+Tab');
  await expect(page.getByRole('menu')).toHaveCount(0);
  await expect(trigger).toBeFocused();
});

test('a tooltip stays out of the accessibility tree until it is shown', async ({ page }) => {
  await openDesignSystem(page);

  // An element at opacity 0 is still announced. Only visibility takes it out.
  await expect(page.getByRole('tooltip')).toHaveCount(0);

  const anchor = page.getByRole('button', { name: 'Hover me' });
  await anchor.hover();
  await expect(page.getByRole('tooltip')).toBeVisible();

  // And it has to describe its anchor, or it is a bubble attached to nothing.
  const describedBy = await anchor.getAttribute('aria-describedby');
  expect(describedBy).not.toBeNull();
});
