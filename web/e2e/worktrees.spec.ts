import { expect, test, type Locator, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * The linked checkouts a repository has, from the interface.
 *
 * A worktree is never a tab of its own — the registry's identity is the git
 * directory, which every checkout of one repository shares — so what the panel
 * proves is the rest: which exist, which branch each holds, and that making and
 * unmaking one goes through a command shown first.
 */

const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

function gitIn(cwd: string) {
  return (...args: string[]) =>
    execFileSync('git', args, {
      cwd,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });
}

/** A repository with a second branch, in a fixture root of its own. */
async function openWithASideBranch(
  page: Page,
  name: string,
): Promise<{ root: string; path: string }> {
  await openWorkbench(page);

  const root = scratchFixture(name);
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, name);

  gitIn(root)('init', '-b', 'main', path);
  const git = gitIn(path);
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');
  writeFileSync(join(path, 'notes.md'), 'first\n');
  git('add', '-A');
  git('commit', '-m', 'first');
  git('branch', 'side');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();
  return { root, path };
}

/**
 * Opens a row's actions menu.
 *
 * The trigger is drawn only for a row under the pointer or holding focus, and
 * it takes clicks only then too — the pair moves together deliberately. An
 * invisible control that still answered a click was how a press meant for the
 * row opened the menu holding Remove, so hovering the row first is not a test
 * convenience, it is the gesture. StashPanel's rows are read the same way.
 */
async function rowActions(panel: Locator, name: string) {
  const trigger = panel.getByRole('button', { name });
  // Two levels up from the trigger: its own wrapper is the box that fades, and
  // the row above that is the group the hover is read from.
  await trigger.locator('../..').hover();
  await trigger.click();
}

test('lists the main tree, then makes and removes a linked one', async ({ page }) => {
  const { root } = await openWithASideBranch(page, 'wt-panel');
  const destination = join(root, 'wt-panel-side');

  const panel = page.getByRole('region', { name: /^Worktrees/ });
  await expect(panel).toContainText('wt-panel');
  await expect(panel).toContainText('main tree');
  await expect(panel).toContainText('this tab');

  await panel.getByRole('button', { name: /^Add worktree/ }).click();
  const dialog = page.getByRole('dialog', { name: 'New worktree' });
  await expect(dialog).toBeVisible();

  const folder = dialog.getByLabel('Folder');
  await expect(folder).toHaveValue('');
  await folder.fill(destination);
  await expect(folder).toHaveValue(destination);

  const reference = dialog.getByLabel('Reference');
  await expect(reference).toHaveValue('');
  await reference.fill('side');
  await expect(reference).toHaveValue('side');

  // The command before the checkout, like every other operation here.
  await dialog.getByRole('button', { name: 'Continue' }).click();
  await expect(dialog.getByText(/git worktree add --/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Create' }).click();

  await expect(panel).toContainText('wt-panel-side', { timeout: 30_000 });
  await expect(panel).toContainText('branch side');
  expect(existsSync(destination)).toBe(true);

  // And unmaking it, through the confirmation that names what goes.
  await rowActions(panel, 'Actions for the worktree on side');
  await page.getByRole('menuitem', { name: 'Remove…' }).click();
  const confirm = page.getByRole('dialog');
  await expect(confirm).toContainText('git worktree remove --');
  await expect(confirm).toContainText(destination);
  await confirm.getByRole('button', { name: 'Remove' }).click();

  await expect(panel).not.toContainText('wt-panel-side', { timeout: 30_000 });
  expect(existsSync(destination)).toBe(false);
  // The branch it held is untouched: a worktree is a checkout, not the work.
  expect(gitIn(join(root, 'wt-panel'))('rev-parse', '--verify', 'refs/heads/side')).toBeTruthy();
});

test('refuses to remove the main working tree, and says why', async ({ page }) => {
  await openWithASideBranch(page, 'wt-main');

  const panel = page.getByRole('region', { name: /^Worktrees/ });
  await rowActions(panel, 'Actions for the worktree on main');

  const refused = page.getByRole('menuitem', { name: 'Remove…' });
  await expect(refused).toBeDisabled();
  await expect(refused).toContainText('will not remove it');
});
