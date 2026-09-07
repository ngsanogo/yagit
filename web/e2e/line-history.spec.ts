import { expect, test } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Line history from a blame gutter — commits that changed one line.
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

test('opens line history from a blame line number', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('line-history');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, 'line-history');

  const git = gitIn(root);
  git('init', '-b', 'main', path);
  const inRepo = gitIn(path);
  inRepo('config', 'user.name', 'Ada Lovelace');
  inRepo('config', 'user.email', 'ada@example.com');
  writeFileSync(join(path, 'notes.md'), 'one\ntwo\nthree\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'add three lines');
  writeFileSync(join(path, 'notes.md'), 'one\ntwo changed\nthree\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'change line two');
  writeFileSync(join(path, 'notes.md'), 'one\ntwo changed\nthree\nfour\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'add line four');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: /line-history/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  await page.getByRole('button', { name: /change line two/ }).click();
  const commit = page.getByRole('region', { name: 'Commit' });
  await expect(commit).toBeVisible();
  await commit.getByRole('button', { name: 'Blame' }).click();

  const blame = page.getByRole('region', { name: /Blame — notes\.md/ });
  await expect(blame).toBeVisible();
  await blame.getByRole('button', { name: 'History of line 2' }).click();

  const history = page.getByRole('region', { name: /History — notes\.md:2/ });
  await expect(history).toBeVisible();
  await expect(history.getByText('change line two')).toBeVisible();
  await expect(history.getByText('add three lines')).toBeVisible();
  await expect(history.getByText('add line four')).toHaveCount(0);

  await history.getByRole('button', { name: /add three lines/ }).click();
  await expect(page.getByRole('region', { name: 'Commit' })).toBeVisible();
  await expect(page.getByText('add three lines').first()).toBeVisible();
});
