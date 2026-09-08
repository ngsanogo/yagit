import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * The repositories a repository pins, from the interface.
 *
 * The panel exists only where there is one, so the first thing these prove is
 * that it appears at all — and then the state a row has to make legible, which
 * is the half a list of paths would not.
 */

const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/**
 * git in one repository, with the file transport allowed.
 *
 * git refuses a submodule clone over `file://` by default — the fix for
 * CVE-2022-39253 — and reads that setting outside the repository, because the
 * clone runs outside one. Set on the fixture's own commands and never by
 * yagit: turning a security control off for every user is not a client's
 * decision to make.
 */
function gitIn(cwd: string) {
  return (...args: string[]) =>
    execFileSync('git', ['-c', 'protocol.file.allow=always', ...args], {
      cwd,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });
}

/** A repository pinning one other, opened in the workbench. */
async function openSuperproject(page: Page, name: string): Promise<string> {
  await openWorkbench(page);

  const root = scratchFixture(name);
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });

  const library = join(root, 'lib');
  gitIn(root)('init', '-b', 'main', library);
  const inLibrary = gitIn(library);
  inLibrary('config', 'user.name', 'Ada Lovelace');
  inLibrary('config', 'user.email', 'ada@example.com');
  inLibrary('config', 'commit.gpgsign', 'false');
  writeFileSync(join(library, 'lib.txt'), 'one\n');
  inLibrary('add', '-A');
  inLibrary('commit', '-m', 'the library');

  const path = join(root, name);
  gitIn(root)('init', '-b', 'main', path);
  const git = gitIn(path);
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');
  writeFileSync(join(path, 'readme.md'), 'the superproject\n');
  git('add', '-A');
  git('commit', '-m', 'first');
  git('submodule', 'add', '--', library, 'vendor/lib');
  git('commit', '-m', 'pin the library');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();
  return path;
}

test('shows what is pinned, and says when a checkout is missing', async ({ page }) => {
  const path = await openSuperproject(page, 'sm-panel');
  const panel = page.getByRole('region', { name: /^Submodules/ });

  await expect(panel).toContainText('vendor/lib');
  await expect(panel).toContainText('at ');

  // What a fresh clone looks like: the gitlink is there and the directory is
  // empty. The panel has to say so rather than showing a commit — and the
  // commit it would wrongly show is the SUPERPROJECT's, because an empty
  // directory inside a work tree resolves HEAD against the repository above it.
  gitIn(path)('submodule', 'deinit', '--force', '--', 'vendor/lib');
  await page.reload();
  await page.getByRole('tab', { name: /sm-panel/ }).click();
  await expect(panel).toContainText('not fetched');

  // The button that would fill it appears exactly here, and not before.
  await expect(panel.getByRole('button', { name: 'Update all submodules' })).toBeVisible();

  // Clicking it is not exercised here: the fixture's library is a local path,
  // and git refuses a submodule clone over the file transport unless the
  // MACHINE's config allows it — which the daemon under test does not set and
  // must not. What the button then runs is covered where that config can be
  // written, in internal/api/submodules_test.go.
});

test('removes a submodule through the pair of commands git needs', async ({ page }) => {
  const path = await openSuperproject(page, 'sm-remove');
  const panel = page.getByRole('region', { name: /^Submodules/ });

  await panel.getByRole('button', { name: 'Actions for the submodule vendor/lib' }).click();
  await page.getByRole('menuitem', { name: 'Remove…' }).click();

  const dialog = page.getByRole('dialog');
  // Both lines, because git has no `submodule remove` and the confirmation
  // must not pretend it does.
  await expect(dialog).toContainText('git submodule deinit -- vendor/lib');
  await expect(dialog).toContainText('git rm -- vendor/lib');
  await expect(dialog).toContainText('.git/modules');
  await dialog.getByRole('button', { name: 'Remove' }).click();

  // The panel goes with the last submodule.
  await expect(panel).toBeHidden({ timeout: 30_000 });
  expect(existsSync(join(path, 'vendor', 'lib', 'lib.txt'))).toBe(false);
});

test('no panel at all for a repository that pins nothing', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('sm-none');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, 'sm-none');
  gitIn(root)('init', '-b', 'main', path);
  const git = gitIn(path);
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');
  writeFileSync(join(path, 'readme.md'), 'nothing pinned\n');
  git('add', '-A');
  git('commit', '-m', 'first');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: /sm-none/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  await expect(page.getByRole('region', { name: /^Submodules/ })).toHaveCount(0);

  // And the operation is still reachable, which is the whole point of hiding
  // the panel rather than the feature: the button used to live ON the panel,
  // so a repository pinning nothing had no way to pin a first one.
  const add = page.getByRole('button', { name: 'Add submodule' });
  await expect(add).toBeVisible();
  await add.click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('New submodule');
  await dialog.getByRole('button', { name: 'Cancel' }).click();
});
