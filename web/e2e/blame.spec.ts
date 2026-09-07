import { expect, test } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Blame from a commit's patch header — who last touched each line.
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

test('opens blame from a commit patch and reaches the introducing commit', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('file-blame');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, 'file-blame');

  const git = gitIn(root);
  git('init', '-b', 'main', path);
  const inRepo = gitIn(path);
  inRepo('config', 'user.name', 'Ada Lovelace');
  inRepo('config', 'user.email', 'ada@example.com');
  writeFileSync(join(path, 'notes.md'), 'one\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'add one');
  writeFileSync(join(path, 'notes.md'), 'one\ntwo\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'add two');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: /file-blame/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  await page.getByRole('button', { name: /add two/ }).click();
  const commit = page.getByRole('region', { name: 'Commit' });
  await expect(commit).toBeVisible();
  await commit.getByRole('button', { name: 'Blame' }).click();

  const blame = page.getByRole('region', { name: /Blame — notes\.md/ });
  await expect(blame).toBeVisible();
  const twoRow = blame.getByRole('row').filter({ has: page.getByRole('cell', { name: 'two' }) });
  await expect(twoRow.getByRole('cell', { name: 'Ada Lovelace' })).toBeVisible();

  // The short SHA — not the line-number history button — opens that commit.
  await twoRow.getByRole('button', { name: /^[0-9a-f]{7,}$/ }).click();
  await expect(page.getByRole('region', { name: 'Commit' })).toBeVisible();
  await expect(page.getByText('add two').first()).toBeVisible();
});
