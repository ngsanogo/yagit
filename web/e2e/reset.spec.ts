import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Resetting the branch HEAD is on to a selected commit.
 *
 * Soft, mixed and hard are three promises about the three trees; this file
 * proves the start exists: a click on a commit asks the daemon what a mixed
 * reset would do, the dialog can switch to hard, shows the exact command, and
 * that command is what git receives.
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
 * Linear history on main: base → one → two. Resetting to "one" drops "two".
 */
async function openResettableRepository(page: Page, name: string): Promise<string> {
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

  writeFileSync(join(path, 'notes.md'), 'one\n');
  git('add', '-A');
  git('commit', '-m', 'one');

  writeFileSync(join(path, 'extra.md'), 'two\n');
  git('add', '-A');
  git('commit', '-m', 'two');

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

test('resets hard to a commit after showing the command', async ({ page }) => {
  const path = await openResettableRepository(page, 'rs-hard');
  const git = gitIn(path);
  const one = git('rev-parse', 'main~1').toString().trim();

  await page.getByRole('list', { name: 'Commits' }).getByText('one', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  await expect(pane).toBeVisible();
  await pane.getByRole('button', { name: /^Reset/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(`Reset main to ${one.slice(0, 7)}?`);
  await expect(dialog).toContainText(`git reset --mixed ${one}`);

  await dialog.getByRole('radio', { name: 'Hard' }).click();
  await expect(dialog).toContainText(`git reset --hard ${one}`);
  await expect(dialog).toContainText('This will permanently discard');
  await expect(dialog).toContainText('1 commit');

  await dialog.getByRole('button', { name: 'Reset --hard' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: `Reset main to ${one.slice(0, 7)} (hard)` }),
  ).toBeVisible();

  expect(git('rev-parse', 'HEAD').toString().trim()).toBe(one);
  expect(git('status', '--porcelain').toString()).toBe('');
});

test('a detached HEAD refuses the button and says why', async ({ page }) => {
  const path = await openResettableRepository(page, 'rs-detached');
  const git = gitIn(path);

  git('switch', '--detach', 'HEAD');

  await page.reload();
  await page.getByRole('tab', { name: /rs-detached/ }).click();
  await page.getByRole('list', { name: 'Commits' }).getByText('one', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  const button = pane.getByRole('button', { name: /^Reset/ });
  await expect(button).toBeVisible();
  await expect(button).toBeDisabled();
  await expect(pane.getByText('HEAD is detached, so there is no branch to reset')).toBeAttached();
});

test('a commit not on the branch is refused rather than offered', async ({ page }) => {
  const path = await openResettableRepository(page, 'rs-foreign');
  const git = gitIn(path);

  git('switch', '-c', 'side', 'main~2');
  writeFileSync(join(path, 'side.md'), 'only side\n');
  git('add', '-A');
  git('commit', '-m', 'side only');
  git('switch', 'main');

  await page.reload();
  await page.getByRole('tab', { name: /rs-foreign/ }).click();

  // Scope to every ref so the side commit is visible, then select it.
  await page.getByRole('radio', { name: /All references/ }).click();
  await page.getByRole('list', { name: 'Commits' }).getByText('side only', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  await pane.getByRole('button', { name: /^Reset/ }).click();

  await expect(page.getByRole('status').filter({ hasText: 'not on the branch' })).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});
