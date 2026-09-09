import { expect, test } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Undo from the HEAD reflog — tip commit, checkout back to a branch, and a
 * reset put back.
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

async function openFixture(
  page: import('@playwright/test').Page,
  name: string,
  setup: (path: string) => void,
) {
  await openWorkbench(page);
  const root = scratchFixture(name);
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, name);
  const git = gitIn(root);
  git('init', '-b', 'main', path);
  const inRepo = gitIn(path);
  inRepo('config', 'user.name', 'Ada Lovelace');
  inRepo('config', 'user.email', 'ada@example.com');
  setup(path);
  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);
  await page.reload();
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  return path;
}

test('undoes the tip commit and leaves its changes staged', async ({ page }) => {
  await openFixture(page, 'undo-commit', (path) => {
    const inRepo = gitIn(path);
    writeFileSync(join(path, 'notes.md'), 'one\n');
    inRepo('add', '-A');
    inRepo('commit', '-m', 'add one');
    writeFileSync(join(path, 'notes.md'), 'two\n');
    inRepo('add', '-A');
    inRepo('commit', '-m', 'add two');
  });

  await page.getByRole('button', { name: /Undo last commit/ }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/reset --soft/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Undo commit' }).click();

  await expect(page.getByRole('status').getByText(/Undid/)).toBeVisible();
  await page.getByRole('radio', { name: 'Changes' }).click();
  await expect(page.getByText('notes.md').first()).toBeVisible();
});

test('undoes a checkout back onto the previous branch', async ({ page }) => {
  await openFixture(page, 'undo-checkout', (path) => {
    const inRepo = gitIn(path);
    writeFileSync(join(path, 'notes.md'), 'one\n');
    inRepo('add', '-A');
    inRepo('commit', '-m', 'add one');
    inRepo('switch', '-c', 'side');
    writeFileSync(join(path, 'notes.md'), 'side\n');
    inRepo('add', '-A');
    inRepo('commit', '-m', 'work on the side branch');
    inRepo('switch', 'main');
  });

  await expect(page.getByText(/^on \S+$/)).toHaveText('on main');
  await page.getByRole('button', { name: 'Undo checkout' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByText(/switch --no-guess/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Undo checkout' }).click();

  await expect(page.getByRole('status').filter({ hasText: 'Now on side' })).toBeVisible();
  await expect(page.getByText(/^on \S+$/)).toHaveText('on side');
});

test('undoes a reset and puts the branch back where it was', async ({ page }) => {
  await openFixture(page, 'undo-reset', (path) => {
    const inRepo = gitIn(path);
    writeFileSync(join(path, 'notes.md'), 'one\n');
    inRepo('add', '-A');
    inRepo('commit', '-m', 'add one');
    writeFileSync(join(path, 'notes.md'), 'two\n');
    inRepo('add', '-A');
    inRepo('commit', '-m', 'the commit a reset dropped');
    inRepo('reset', '--hard', 'HEAD~1');
  });

  await expect(page.getByText('add one').first()).toBeVisible();

  await page.getByRole('button', { name: 'Undo reset' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByText(/reset --soft/)).toBeVisible();
  // The one thing a soft reset back cannot promise, said before it runs.
  await expect(dialog.getByText(/does not come back/)).toBeVisible();
  await dialog.getByRole('button', { name: 'Undo reset' }).click();

  await expect(page.getByRole('status').filter({ hasText: /Undid reset/ })).toBeVisible();
  await expect(page.getByText('the commit a reset dropped').first()).toBeVisible();
});
