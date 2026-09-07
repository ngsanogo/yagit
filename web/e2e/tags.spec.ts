import { expect, test } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Creating and deleting tags through the interface.
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

async function openTaggedRepo(
  page: import('@playwright/test').Page,
  name: string,
): Promise<string> {
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
  writeFileSync(join(path, 'notes.md'), 'first\n');
  inRepo('add', '-A');
  inRepo('commit', '-m', 'first');

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

/** The sidebar overflow trigger for a tag — exact, like branch rows. */
function tagActions(page: import('@playwright/test').Page, name: string) {
  return page.getByRole('button', { name: `More actions for ${name}`, exact: true });
}

test('creates an annotated tag and lists it under Tags', async ({ page }) => {
  await openTaggedRepo(page, 'tag-create');

  await page.getByRole('button', { name: 'New tag' }).click();
  const dialog = page.getByRole('dialog', { name: 'New tag' });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('Name').fill('v1.0.0');
  await dialog.getByLabel('Message').fill('first release');
  await dialog.getByRole('button', { name: 'Create' }).click();

  // The row's accessible name includes the short SHA; the menu trigger names
  // the tag alone.
  await expect(tagActions(page, 'v1.0.0')).toBeVisible();
});

test('creates a lightweight tag without a message', async ({ page }) => {
  await openTaggedRepo(page, 'tag-light');

  await page.getByRole('button', { name: 'New tag' }).click();
  const dialog = page.getByRole('dialog', { name: 'New tag' });
  await dialog.getByRole('radio', { name: 'Lightweight' }).click();
  await expect(dialog.getByLabel('Message')).toHaveCount(0);
  await dialog.getByLabel('Name').fill('v0.9.0');
  await dialog.getByRole('button', { name: 'Create' }).click();

  await expect(tagActions(page, 'v0.9.0')).toBeVisible();
});

test('deletes a tag after showing the command', async ({ page }) => {
  await openTaggedRepo(page, 'tag-delete');

  await page.getByRole('button', { name: 'New tag' }).click();
  const create = page.getByRole('dialog', { name: 'New tag' });
  await create.getByLabel('Name').fill('v0.1.0');
  await create.getByLabel('Message').fill('early');
  await create.getByRole('button', { name: 'Create' }).click();
  await expect(tagActions(page, 'v0.1.0')).toBeVisible();

  // Hover with force: the trigger is inert until the row receives attention.
  const trigger = tagActions(page, 'v0.1.0');
  await trigger.hover({ force: true });
  await trigger.click();
  await page.getByRole('menuitem', { name: 'Delete…' }).click();

  const confirm = page.getByRole('dialog', { name: /Delete tag v0\.1\.0/ });
  await expect(confirm.getByText(/git tag -d -- v0\.1\.0/)).toBeVisible();
  await confirm.getByRole('button', { name: 'Delete' }).click();

  await expect(tagActions(page, 'v0.1.0')).toHaveCount(0);
});

test('pushes a tag to origin after showing the command', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('tag-push');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const server = join(root, 'server.git');
  const work = join(root, 'tag-push');

  const git = gitIn(root);
  git('init', '--bare', '-b', 'main', server);
  git('init', '-b', 'main', work);
  const inWork = gitIn(work);
  inWork('config', 'user.name', 'Ada Lovelace');
  inWork('config', 'user.email', 'ada@example.com');
  inWork('remote', 'add', 'origin', server);
  writeFileSync(join(work, 'notes.md'), 'first\n');
  inWork('add', '-A');
  inWork('commit', '-m', 'first');
  inWork('push', '--set-upstream', 'origin', 'main:refs/heads/main');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path: work },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(work);

  await page.reload();
  await page.getByRole('tab', { name: /tag-push/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  await page.getByRole('button', { name: 'New tag' }).click();
  const create = page.getByRole('dialog', { name: 'New tag' });
  await create.getByLabel('Name').fill('v2.0.0');
  await create.getByLabel('Message').fill('shipped');
  await create.getByRole('button', { name: 'Create' }).click();
  await expect(tagActions(page, 'v2.0.0')).toBeVisible();

  const trigger = tagActions(page, 'v2.0.0');
  await trigger.hover({ force: true });
  await trigger.click();
  await page.getByRole('menuitem', { name: 'Push…' }).click();

  const push = page.getByRole('dialog', { name: /Push v2\.0\.0/ });
  await expect(
    push.getByText(/git push -- origin refs\/tags\/v2\.0\.0:refs\/tags\/v2\.0\.0/),
  ).toBeVisible();
  await push.getByRole('button', { name: 'Push' }).click();

  await expect(
    page.getByRole('status').filter({ hasText: 'Pushed v2.0.0 to origin' }),
  ).toBeVisible();

  const onServer = execFileSync('git', ['rev-parse', '--verify', 'refs/tags/v2.0.0'], {
    cwd: server,
    encoding: 'utf8',
  })
    .toString()
    .trim();
  expect(onServer).toHaveLength(40);
});
