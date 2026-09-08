import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Reverting a selected commit on the branch HEAD is on.
 *
 * The finish side — abort, continue, conflict UI — is already covered by
 * conflicts.spec.ts. What this file proves is that the start exists: a click
 * on a commit asks the daemon what reverting it would do, shows the exact
 * command, and that command is what git receives.
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
 * Linear history on main: base → one → two. Reverting "one" is a clean apply.
 */
async function openRevertableRepository(page: Page, name: string): Promise<string> {
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

test('reverts a commit after showing the command', async ({ page }) => {
  const path = await openRevertableRepository(page, 'rv-apply');
  const git = gitIn(path);
  const one = git('rev-parse', 'main~1').toString().trim();
  const before = git('rev-parse', 'HEAD').toString().trim();

  await page.getByRole('list', { name: 'Commits' }).getByText('one', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  await expect(pane).toBeVisible();
  await pane.getByRole('button', { name: /^Revert/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(`Revert ${one.slice(0, 7)} on main?`);
  await expect(dialog).toContainText(`git revert --no-edit --no-reference -- ${one}`);
  await expect(dialog).toContainText('new commit');
  await expect(dialog).toContainText('hooks and signing');

  await dialog.getByRole('button', { name: 'Revert' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: `Reverted ${one.slice(0, 7)} on main` }),
  ).toBeVisible();

  const head = git('rev-parse', 'HEAD').toString().trim();
  expect(head).not.toBe(before);
  expect(head).not.toBe(one);
  expect(git('log', '-1', '--pretty=format:%s').toString()).toBe('Revert "one"');
  expect(
    git('rev-list', '-1', '--parents', 'HEAD').toString().trim().split(/\s+/).slice(1),
  ).toEqual([before]);
});

test('a detached HEAD refuses the button and says why', async ({ page }) => {
  const path = await openRevertableRepository(page, 'rv-detached');
  const git = gitIn(path);

  git('switch', '--detach', 'HEAD');

  await page.reload();
  await page.getByRole('tab', { name: /rv-detached/ }).click();
  await page.getByRole('list', { name: 'Commits' }).getByText('one', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  const button = pane.getByRole('button', { name: /^Revert/ });
  await expect(button).toBeVisible();
  await expect(button).toBeDisabled();
  await expect(
    pane.getByText('HEAD is detached, so there is no branch to revert on'),
  ).toBeAttached();
});

test('a root commit is refused rather than offered', async ({ page }) => {
  await openRevertableRepository(page, 'rv-root');

  await page.getByRole('list', { name: 'Commits' }).getByText('base', { exact: true }).click();
  const pane = page.getByRole('region', { name: 'Commit' });
  await pane.getByRole('button', { name: /^Revert/ }).click();

  // The toast first: it is what waits for the daemon's answer. Asserting the
  // absent dialog before the round trip has finished would pass on a dialog
  // that was still on its way.
  await expect(page.getByRole('alert').filter({ hasText: 'root' })).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});
