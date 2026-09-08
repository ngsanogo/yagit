import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { violations } from './accessibility';
import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Rewriting the commits after a selected one, from a plan.
 *
 * The plan is written ON the confirmation, which makes this the one dialog
 * here whose answer is not a yes — so what these tests prove is that the rows
 * are the question: that they open on the history as it stands, that editing
 * one changes what the dialog says will happen, and that the list which
 * reaches git is the list that was on screen.
 *
 * The daemon has no editor and never opens one, so a plan whose steps are all
 * carried out ends in a rewritten history and a toast. A plan holding a stop
 * ends in a banner instead, which is the other test here.
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
 * Linear history on main: base → one → two → three, each commit touching a
 * file of its own so any order of them applies cleanly.
 */
async function openPlannableRepository(page: Page, name: string): Promise<string> {
  await openWorkbench(page);

  const path = scratchFixture(name);
  rmSync(path, { recursive: true, force: true });
  mkdirSync(path, { recursive: true });

  const git = gitIn(path);
  git('init', '-b', 'main');
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');

  for (const subject of ['base', 'one', 'two', 'three']) {
    writeFileSync(join(path, `${subject}.md`), `${subject}\n`);
    git('add', '-A');
    git('commit', '-m', subject);
  }

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

/** Opens the plan over the commits after "base". */
async function openPlan(page: Page) {
  await page.getByRole('list', { name: 'Commits' }).getByText('base', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  await expect(pane).toBeVisible();
  // Behind the commit's menu, with reset: the two operations that move the
  // branch left the header when five chips of identical weight stopped saying
  // which of them rewrites history. The trigger names the commit, so the item
  // does not have to.
  await pane.getByRole('button', { name: /^More actions for/ }).click();
  await page.getByRole('menuitem', { name: 'Rewrite the commits after this' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  return dialog;
}

/** The subjects on main, newest first. */
function subjects(path: string): string[] {
  return gitIn(path)('log', '--pretty=format:%s').toString().trim().split('\n');
}

test('drops a commit from the plan, and git drops it too', async ({ page }) => {
  const path = await openPlannableRepository(page, 'ir-drop');
  const dialog = await openPlan(page);

  // Opens on the history as it stands, which is a plan that changes nothing —
  // so the button is refused until a row is edited.
  await expect(dialog).toContainText('would change nothing');
  await expect(dialog.getByRole('button', { name: 'Run the plan' })).toBeDisabled();

  await dialog.getByRole('combobox', { name: /What to do with .*two/ }).selectOption('drop');

  await expect(dialog).toContainText('main keeps 2 commits of 3');
  await expect(dialog).toContainText('1 commit dropped');
  await expect(dialog).toContainText('This will permanently discard');

  await dialog.getByRole('button', { name: 'Run the plan' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: 'Rewrote 3 commits on main' }),
  ).toBeVisible();
  expect(subjects(path)).toEqual(['three', 'one', 'base']);
});

test('reorders the plan, and git replays it in that order', async ({ page }) => {
  const path = await openPlannableRepository(page, 'ir-reorder');
  const dialog = await openPlan(page);

  // "three" is the last row; two moves up put it at the top of the plan.
  const up = dialog.getByRole('button', { name: /^Move .* up$/ });
  await up.nth(2).click();
  await up.nth(1).click();

  await expect(dialog).toContainText('3 commits written again under new hashes');

  await dialog.getByRole('button', { name: 'Run the plan' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(page.getByRole('status').filter({ hasText: 'Rewrote 3 commits' })).toBeVisible();
  expect(subjects(path)).toEqual(['two', 'one', 'three', 'base']);
});

test('combining folds a commit into the one above it', async ({ page }) => {
  const path = await openPlannableRepository(page, 'ir-combine');
  const dialog = await openPlan(page);

  await dialog.getByRole('combobox', { name: /What to do with .*two/ }).selectOption('fixup');
  await expect(dialog).toContainText('1 commit folded into the one above it');
  await expect(dialog).toContainText('Its changes survive; its message is discarded.');

  await dialog.getByRole('button', { name: 'Run the plan' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Rewrote 3 commits' })).toBeVisible();

  // The message is gone and the change is not: "two" survives inside "one".
  expect(subjects(path)).toEqual(['three', 'one', 'base']);
  expect(gitIn(path)('show', '--stat', '--pretty=format:', 'HEAD~1').toString()).toContain(
    'two.md',
  );
});

// The one instruction that ends in a stop on purpose. git exits ZERO there, so
// the answer has to say it stopped — a success toast over a repository waiting
// for an amend would send the user away from the screen that can finish it.
test('a plan that stops says so, and the banner carries on', async ({ page }) => {
  await openPlannableRepository(page, 'ir-stop');
  const dialog = await openPlan(page);

  await dialog.getByRole('combobox', { name: /What to do with .*two/ }).selectOption('edit');
  await expect(dialog).toContainText('git stops once');

  await dialog.getByRole('button', { name: 'Run the plan' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: 'Stopped at 2 of 3 on main' }),
  ).toBeVisible();

  // The banner is the way on, and it is drawn because a rebase really is in
  // progress — the same one that already knew how to finish a conflict.
  await expect(page.getByText(/Rebasing/)).toBeVisible();
  await expect(page.getByRole('button', { name: /^Continue/ })).toBeVisible();
});

test('a combine at the top of the plan is refused with the reason', async ({ page }) => {
  await openPlannableRepository(page, 'ir-no-target');
  const dialog = await openPlan(page);

  await dialog.getByRole('combobox', { name: /What to do with .*one/ }).selectOption('fixup');

  // git answers this one AFTER starting the rebase, leaving a repository
  // stopped inside a plan that could never have run. The dialog answers first.
  await expect(dialog).toContainText('nothing above it to combine into');
  await expect(dialog.getByRole('button', { name: 'Run the plan' })).toBeDisabled();
});

test('the plan dialog has no accessibility violations', async ({ page }) => {
  await openPlannableRepository(page, 'ir-axe');
  const dialog = await openPlan(page);
  await dialog.getByRole('combobox', { name: /What to do with .*two/ }).selectOption('drop');

  expect(await violations(page)).toEqual([]);
});
