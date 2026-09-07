import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Cherry-picking a selected commit onto the branch HEAD is on.
 *
 * The finish side — abort, continue, conflict UI — is already covered by
 * conflicts.spec.ts. What this file proves is that the start exists: a click
 * on a commit asks the daemon what picking it would do, shows the exact
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
 * main and side edited different files, so picking side's tip onto main is a
 * clean apply — a new commit under the daemon's identity.
 */
async function openPickableRepository(page: Page, name: string): Promise<string> {
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

  git('switch', '-c', 'side');
  writeFileSync(join(path, 'other.md'), 'side only\n');
  git('add', '-A');
  git('commit', '-m', 'side only');

  git('switch', 'main');
  writeFileSync(join(path, 'notes.md'), 'main\n');
  git('add', '-A');
  git('commit', '-m', 'main edit');

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

test('cherry-picks a commit after showing the command', async ({ page }) => {
  const path = await openPickableRepository(page, 'cp-apply');
  const git = gitIn(path);
  const side = git('rev-parse', 'side').toString().trim();
  const before = git('rev-parse', 'HEAD').toString().trim();

  // The side commit is not on main's walk; draw every ref so it has a row.
  await page.getByRole('radio', { name: /All references/ }).click();
  await page.getByRole('list', { name: 'Commits' }).getByText('side only', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  await expect(pane).toBeVisible();
  await pane.getByRole('button', { name: /Cherry-pick/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText(`Cherry-pick ${side.slice(0, 7)} onto main?`);
  await expect(dialog).toContainText(`git cherry-pick --no-edit --no-ff -- ${side}`);
  await expect(dialog).toContainText('new commit');
  await expect(dialog).toContainText('hooks and signing');

  await dialog.getByRole('button', { name: 'Cherry-pick' }).click();
  await expect(dialog).toHaveCount(0);

  await expect(
    page.getByRole('status').filter({ hasText: `Cherry-picked ${side.slice(0, 7)} onto main` }),
  ).toBeVisible();

  const head = git('rev-parse', 'HEAD').toString().trim();
  expect(head).not.toBe(before);
  expect(head).not.toBe(side);
  expect(git('log', '-1', '--pretty=format:%s').toString()).toBe('side only');
  expect(
    git('rev-list', '-1', '--parents', 'HEAD').toString().trim().split(/\s+/).slice(1),
  ).toEqual([before]);
});

test('fast-forwards onto a commit after showing --ff', async ({ page }) => {
  const path = await openPickableRepository(page, 'cp-ff');
  const git = gitIn(path);

  git('switch', '-c', 'ahead');
  writeFileSync(join(path, 'extra.md'), 'new\n');
  git('add', '-A');
  git('commit', '-m', 'one step ahead');
  const picked = git('rev-parse', 'HEAD').toString().trim();
  git('switch', 'main');

  await page.reload();
  await page.getByRole('tab', { name: /cp-ff/ }).click();
  await page.getByRole('radio', { name: /All references/ }).click();
  await page
    .getByRole('list', { name: 'Commits' })
    .getByText('one step ahead', { exact: true })
    .click();

  const pane = page.getByRole('region', { name: 'Commit' });
  await pane.getByRole('button', { name: /Cherry-pick/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText(`git cherry-pick --no-edit --ff -- ${picked}`);
  await expect(dialog).toContainText('Nothing is committed');

  await dialog.getByRole('button', { name: 'Cherry-pick' }).click();
  await expect(dialog).toHaveCount(0);

  expect(git('rev-parse', 'HEAD').toString().trim()).toBe(picked);
  await expect(
    page.getByRole('status').filter({ hasText: `Fast-forwarded main to ${picked.slice(0, 7)}` }),
  ).toBeVisible();
});

test('a commit already on the branch toasts instead of opening a dialog', async ({ page }) => {
  await openPickableRepository(page, 'cp-have');

  // The tip of main is on main already, so the plan is the whole answer.
  await page.getByRole('list', { name: 'Commits' }).getByText('main edit', { exact: true }).click();
  const pane = page.getByRole('region', { name: 'Commit' });
  await pane.getByRole('button', { name: /Cherry-pick/ }).click();

  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'already contains' })).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'changes nothing' })).toBeVisible();
});

test('a detached HEAD refuses the button and says why', async ({ page }) => {
  const path = await openPickableRepository(page, 'cp-detached');
  const git = gitIn(path);

  // Detached, not on a branch: git has nowhere to land a pick, and the panel
  // says so before the click rather than after — see branchMenuItems, which
  // draws the same line for merge and rebase.
  git('switch', '--detach', 'HEAD');

  await page.reload();
  await page.getByRole('tab', { name: /cp-detached/ }).click();
  await page.getByRole('list', { name: 'Commits' }).getByText('main edit', { exact: true }).click();

  const pane = page.getByRole('region', { name: 'Commit' });
  const button = pane.getByRole('button', { name: /Cherry-pick/ });
  await expect(button).toBeVisible();
  await expect(button).toBeDisabled();
  await expect(
    pane.getByText('HEAD is detached, so there is no branch to cherry-pick onto'),
  ).toBeAttached();
});
