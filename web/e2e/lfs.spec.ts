import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Git LFS, as the interface sees it.
 *
 * These tests assume nothing about the machine they run on. Whether git-lfs is
 * installed is exactly the thing the panel is for, so the assertions are about
 * what the panel says in either case — and about the half that needs no
 * git-lfs at all: a committed pointer recognised in a diff.
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

/** A pointer file exactly as the LFS specification pins it. */
const pointer =
  'version https://git-lfs.github.com/spec/v1\n' +
  'oid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\n' +
  'size 3145728\n';

async function openWithLFS(page: Page, name: string): Promise<string> {
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

  // Written by hand rather than by `git lfs track`, so the test says the same
  // thing on a machine that has never had git-lfs installed.
  writeFileSync(join(path, '.gitattributes'), '*.psd filter=lfs diff=lfs merge=lfs -text\n');
  writeFileSync(join(path, 'cover.psd'), pointer);
  git('add', '-A');
  git('commit', '-m', 'store the cover with LFS');

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

test('lists the patterns this repository routes through LFS', async ({ page }) => {
  await openWithLFS(page, 'lfs-patterns');

  const panel = page.getByRole('region', { name: 'Large files' });
  await expect(panel).toBeVisible();
  // Read out of .gitattributes, which is why it is listed whether or not
  // git-lfs is on this machine.
  await expect(panel.getByText('*.psd')).toBeVisible();
});

// The half of LFS that needs no git-lfs: a diff of a pointer is three readable
// lines that say nothing a person wanted to know, and the pane says so.
test('names a committed pointer in the diff rather than showing it raw', async ({ page }) => {
  await openWithLFS(page, 'lfs-pointer');

  await page
    .getByRole('region', { name: /^History/ })
    .getByText('store the cover with LFS')
    .click();

  const commit = page.getByRole('region', { name: 'Commit' });
  await expect(commit).toContainText('cover.psd');
  // The file is ADDED by this commit, so the note is the one for a new file
  // and not the one about a left-hand side there is none of.
  await expect(commit).toContainText('Added, stored with Git LFS');
  // The size the reader came for, in the unit git itself prints.
  await expect(commit).toContainText('3.00 MiB');
});

// The mirror of the submodule case, and the one that was a dead end in both
// directions: the form for a first pattern lived inside a panel a repository
// with no pattern is not given, and untracking a last one took the panel away
// with the pattern still untrackable.
test('a repository using no LFS is still offered a way to start', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('lfs-none');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, 'lfs-none');
  gitIn(root)('init', '-b', 'main', path);
  const git = gitIn(path);
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');
  writeFileSync(join(path, 'readme.md'), 'no large files here\n');
  git('add', '-A');
  git('commit', '-m', 'first');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: /lfs-none/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  await expect(page.getByRole('region', { name: 'Large files' })).toHaveCount(0);

  // Present either way. Whether it can be pressed depends on git-lfs being on
  // the machine, which is the one thing about LFS this suite refuses to
  // assume — so the assertion is that the button is there and says which.
  const track = page.getByRole('button', { name: 'Track large files' });
  await expect(track).toBeVisible();

  if (await track.isEnabled()) {
    await track.click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('Track a pattern with Git LFS');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  }
});
