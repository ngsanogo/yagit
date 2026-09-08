import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { violations } from './accessibility';
import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Setting the work tree aside, and putting it back.
 *
 * The round trip is the first test and the rest are the traps. A stash is
 * addressed by POSITION, so the list has to renumber the moment the stack
 * moves — draw it stale and the next click sends a number naming somebody
 * else's work. And `git stash push` without --include-untracked leaves every
 * untracked file where it is while exiting 0, so the dialog has to say which
 * files are actually going.
 */

const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

function gitIn(path: string) {
  return (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });
}

/**
 * One commit, one changed tracked file and one untracked file: the smallest
 * work tree in which the two counts on the dialog differ.
 */
async function openStashableRepository(page: Page, name: string): Promise<string> {
  await openWorkbench(page);

  const path = scratchFixture(name);
  rmSync(path, { recursive: true, force: true });
  mkdirSync(path, { recursive: true });

  const git = gitIn(path);
  git('init', '-b', 'main');
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');

  writeFileSync(join(path, 'notes.md'), 'base\n');
  git('add', '-A');
  git('commit', '-m', 'base');

  writeFileSync(join(path, 'notes.md'), 'changed\n');
  writeFileSync(join(path, 'scratch.txt'), 'untracked\n');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  return path;
}

function stashPanel(page: Page) {
  return page.getByRole('region', { name: /^Stashes/ });
}

/**
 * Opens a stash row's menu.
 *
 * The trigger is drawn only for a row under the pointer or holding focus, and
 * it takes clicks only then too — the pair moves together deliberately. An
 * invisible control that still answered a click was how a press meant for the
 * row opened the menu holding Drop, so hovering the row first is not a test
 * convenience, it is the gesture.
 */
async function stashActions(page: Page, ref: string) {
  const trigger = stashPanel(page).getByRole('button', { name: `Actions for ${ref}` });
  // Two levels up from the trigger: its own wrapper is the box that fades, and
  // the row above that is the group the hover is read from.
  await trigger.locator('../..').hover();
  await trigger.click();
}

test('stashes the work tree and puts it back', async ({ page }) => {
  const path = await openStashableRepository(page, 'st-round-trip');
  const git = gitIn(path);

  const panel = stashPanel(page);
  await expect(panel).toContainText('Nothing stashed');

  await panel.getByRole('button', { name: /^Stash changes/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  // One tracked file goes; the untracked one is named as staying behind,
  // which is the whole reason the two are counted apart.
  await expect(dialog).toContainText('1 file goes into a stash filed under main');
  await expect(dialog).toContainText('1 untracked file stays where it is');

  await dialog.getByRole('textbox', { name: 'Message' }).fill('half the rewrite');
  await dialog.getByRole('button', { name: 'Stash' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: 'Stashed “half the rewrite”' }),
  ).toBeVisible();
  await expect(stashPanel(page)).toContainText('half the rewrite');

  // The tracked change is saved; the untracked file is exactly where it was.
  expect(git('status', '--porcelain').toString()).toBe('?? scratch.txt\n');

  // And back again. Pop is what the dialog opens on.
  await stashActions(page, 'stash@{0}');
  await page.getByRole('menuitem', { name: 'Put back…' }).click();

  const putBack = page.getByRole('dialog');
  await expect(putBack).toContainText("git stash pop 'stash@{0}'");
  await expect(putBack).toContainText('leaves the stack');
  await putBack.getByRole('button', { name: 'Pop' }).click();
  await expect(putBack).toHaveCount(0);

  await expect(page.getByRole('status').filter({ hasText: 'it has left the stack' })).toBeVisible();
  await expect(stashPanel(page)).toContainText('Nothing stashed');
  expect(git('status', '--porcelain').toString()).toBe(' M notes.md\n?? scratch.txt\n');
});

test('including untracked files changes both the count and what is left behind', async ({
  page,
}) => {
  const path = await openStashableRepository(page, 'st-untracked');

  await stashPanel(page)
    .getByRole('button', { name: /^Stash changes/ })
    .click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('1 file goes into a stash');

  // click rather than check: the box is CONTROLLED by the plan, so it does
  // not flip until the daemon has answered with the new counts — which is the
  // point of it, and what check()'s synchronous state assertion cannot wait
  // for. The counts and the sentence are re-read, never recomputed here.
  await dialog.getByRole('checkbox').click();
  await expect(dialog).toContainText('2 files go into a stash');
  await expect(dialog.getByRole('checkbox')).toBeChecked();
  await expect(dialog).not.toContainText('stays where it is');

  await dialog.getByRole('button', { name: 'Stash' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(stashPanel(page)).not.toContainText('Nothing stashed');
  expect(gitIn(path)('status', '--porcelain').toString()).toBe('');
  expect(existsSync(join(path, 'scratch.txt'))).toBe(false);
});

test('a work tree with nothing tracked to save can still be stashed through the box', async ({
  page,
}) => {
  // The case git turns into a silent no-op: `git stash push` here exits 0 and
  // saves nothing. The dialog has to open anyway — it is the only place the
  // flag can be turned on — and refuse the click until it is.
  const path = await openStashableRepository(page, 'st-only-untracked');
  const git = gitIn(path);
  git('checkout', '--', 'notes.md');

  await page.reload();
  await page.getByRole('tab', { name: /st-only-untracked/ }).click();

  await stashPanel(page)
    .getByRole('button', { name: /^Stash changes/ })
    .click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Nothing tracked has changed here');
  await expect(dialog.getByRole('button', { name: 'Stash' })).toBeDisabled();

  await dialog.getByRole('checkbox').click();
  await expect(dialog.getByRole('button', { name: 'Stash' })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Stash' }).click();
  await expect(dialog).toHaveCount(0);

  expect(git('status', '--porcelain').toString()).toBe('');
});

test('a clean work tree refuses the button and says why', async ({ page }) => {
  const path = await openStashableRepository(page, 'st-clean');
  const git = gitIn(path);
  git('checkout', '--', 'notes.md');
  rmSync(join(path, 'scratch.txt'));

  await page.reload();
  await page.getByRole('tab', { name: /st-clean/ }).click();

  const button = stashPanel(page).getByRole('button', {
    name: /^Stash changes/,
  });
  await expect(button).toBeDisabled();
  await expect(
    stashPanel(page).getByText('The work tree matches HEAD, so there is nothing to stash'),
  ).toBeAttached();
});

test('a stash shows what it holds', async ({ page }) => {
  await openStashableRepository(page, 'st-inspect');

  await stashPanel(page)
    .getByRole('button', { name: /^Stash changes/ })
    .click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('textbox', { name: 'Message' }).fill('what it holds');
  await dialog.getByRole('button', { name: 'Stash' }).click();
  await expect(dialog).toHaveCount(0);

  await stashPanel(page).getByText('what it holds').click();

  const pane = page.getByRole('region', { name: 'Stash', exact: true });
  await expect(pane).toBeVisible();
  await expect(pane).toContainText('what it holds');
  await expect(pane).toContainText('1 file');
  await expect(pane).toContainText('notes.md');
});

test('dropping names what it takes, and the position it acts on', async ({ page }) => {
  const path = await openStashableRepository(page, 'st-drop');
  const git = gitIn(path);

  // Two stashes, so the one dropped is not the one on top — which is what
  // makes the position worth naming.
  git('stash', 'push', '-m', 'the older one');
  writeFileSync(join(path, 'notes.md'), 'again\n');
  git('stash', 'push', '-m', 'the newer one');

  await page.reload();
  await page.getByRole('tab', { name: /st-drop/ }).click();
  await expect(stashPanel(page)).toContainText('the older one');

  await stashActions(page, 'stash@{1}');
  await page.getByRole('menuitem', { name: 'Drop…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Drop “the older one”?');
  await expect(dialog).toContainText("git stash drop 'stash@{1}'");
  await expect(dialog).toContainText('This will permanently discard');
  // Not "gone": the commit outlives the entry, and the name is what can still
  // reach it.
  await expect(dialog).toContainText('until git collects it');

  await dialog.getByRole('button', { name: 'Drop' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: 'Dropped “the older one”' }),
  ).toBeVisible();
  await expect(stashPanel(page)).not.toContainText('the older one');
  // And the survivor has been renumbered, which is the fact every plan in this
  // family is checked against.
  await expect(stashPanel(page)).toContainText('the newer one');
  expect(git('stash', 'list').toString().trim()).toBe('stash@{0}: On main: the newer one');
});

test('a stash that no longer holds that position is refused rather than applied', async ({
  page,
}) => {
  // The refusal the whole family is built around. The dialog is drawn against
  // stash@{0}; a terminal pushes another stash; the command on screen would
  // still run, and it would restore the wrong work.
  const path = await openStashableRepository(page, 'st-moved');
  const git = gitIn(path);
  git('stash', 'push', '-m', 'the one that was approved');

  await page.reload();
  await page.getByRole('tab', { name: /st-moved/ }).click();

  await stashActions(page, 'stash@{0}');
  await page.getByRole('menuitem', { name: 'Put back…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('the one that was approved');

  // Somebody else stashes while the confirmation is open. Everything below
  // stash@{0} has just moved down by one.
  writeFileSync(join(path, 'notes.md'), 'meanwhile\n');
  git('stash', 'push', '-m', 'pushed from somewhere else');

  await dialog.getByRole('button', { name: 'Pop' }).click();

  await expect(
    page.getByRole('alert').filter({ hasText: 'no longer at that position' }),
  ).toBeVisible();
  expect(git('stash', 'list').toString().split('\n').filter(Boolean)).toHaveLength(2);
});

test('a populated stash panel has no accessibility violations', async ({ page }) => {
  // The workbench scan in workbench.spec.ts covers this panel empty, which is
  // the state its fixture is in. Rows are the markup that scan never sees: a
  // button carrying two lines and a badge, with a menu laid over its right
  // edge, which is where an unlabelled control or an unreadable pair of
  // colours would hide.
  const path = await openStashableRepository(page, 'st-axe');
  const git = gitIn(path);
  git('stash', 'push', '-m', 'one to draw');

  await page.reload();
  await page.getByRole('tab', { name: /st-axe/ }).click();
  await expect(stashPanel(page)).toContainText('one to draw');

  expect(await violations(page)).toEqual([]);
});
