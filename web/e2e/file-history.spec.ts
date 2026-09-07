import { expect, test } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * File history from a commit's patch header — following renames.
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

test('opens file history from a commit patch and follows a rename', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('file-history');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, 'file-history');

  const git = gitIn(root);
  git('init', '-b', 'main', path);
  const inRepo = gitIn(path);
  inRepo('config', 'user.name', 'Ada Lovelace');
  inRepo('config', 'user.email', 'ada@example.com');
  writeFileSync(join(path, 'notes.md'), 'first\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'add notes');
  inRepo('mv', 'notes.md', 'README.md');
  inRepo('commit', '-m', 'rename to README');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: /file-history/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  // Newest commit first in the list.
  await page.getByRole('button', { name: /rename to README/ }).click();
  await expect(page.getByRole('heading', { name: 'Commit' })).toBeVisible();

  const patch = page.getByRole('region', { name: 'Commit' });
  await patch.getByRole('button', { name: 'History' }).click();

  const history = page.getByRole('region', { name: /History — README\.md/ });
  await expect(history).toBeVisible();
  await expect(history.getByText('rename to README')).toBeVisible();
  await expect(history.getByText('add notes')).toBeVisible();

  await history.getByRole('button', { name: /add notes/ }).click();
  await expect(page.getByRole('heading', { name: 'Commit' })).toBeVisible();
  await expect(page.getByText('add notes').first()).toBeVisible();
});
