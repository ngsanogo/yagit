import { expect, test, type Locator, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench } from './session';

/**
 * Cloning through the interface, against a real git and a local bare remote.
 *
 * What only a browser talking to a daemon can show: the plan's exact command,
 * progress lines while git runs, and the new tab that lands afterwards.
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

/** A bare remote with one commit, under the scratch fixture root. */
function bareRemote(name: string): { root: string; server: string } {
  const root = scratchFixture(name);
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });

  const server = join(root, 'server.git');
  const seed = join(root, 'seed');
  mkdirSync(seed, { recursive: true });

  const git = gitIn(root);
  git('init', '--bare', '-b', 'main', server);
  git('init', '-b', 'main', seed);
  const inSeed = gitIn(seed);
  inSeed('config', 'user.name', 'Ada Lovelace');
  inSeed('config', 'user.email', 'ada@example.com');
  writeFileSync(join(seed, 'notes.md'), 'first\n');
  inSeed('add', '-A');
  inSeed('commit', '-m', 'first');
  inSeed('remote', 'add', 'origin', server);
  inSeed('push', '--set-upstream', 'origin', 'main:refs/heads/main');

  return { root, server };
}

async function openCloneForm(page: Page): Promise<Locator> {
  await openWorkbench(page);
  await page.getByRole('button', { name: 'Open repository' }).click();

  // Scoped to the dialog: with no repository open, the empty state behind it
  // offers the same Open/Clone control, so an unscoped locator matches two —
  // and which of those a run sees depends on what the other workers happen to
  // have open in the daemon they share.
  const dialog = page.getByRole('dialog', { name: 'Add a repository' });
  await dialog.getByRole('radio', { name: 'Clone' }).click();
  await expect(dialog.getByRole('heading', { name: 'Clone a repository' })).toBeVisible();

  // Returned rather than dropped, so the fields are read inside the dialog for
  // the same reason the radio was. getByLabel matches a substring of the
  // accessible name, so an unscoped `URL` also finds the submodule panel's
  // "Copy the submodule URLs…" button behind the dialog — a repository another
  // spec left open in the shared daemon is enough to make it ambiguous.
  return dialog;
}

test('clones a repository, shows progress, and opens the new tab', async ({ page }) => {
  const { root, server } = bareRemote('clone-ok');
  const destination = join(root, 'clone-ok');

  const form = await openCloneForm(page);

  await form.getByLabel('URL').fill(server);
  await form.getByLabel('Destination').fill(destination);
  await page.getByRole('button', { name: 'Clone…' }).click();

  await expect(page.getByText('yagit will run')).toBeVisible();
  await expect(page.getByText(/git clone --progress --/)).toBeVisible();

  await page.getByRole('button', { name: 'Clone', exact: true }).click();

  await expect(page.getByRole('tab', { name: /clone-ok/ })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  opened.push(destination);
});

test('a missing remote shows git’s refusal on the confirmation', async ({ page }) => {
  const { root } = bareRemote('clone-miss');
  const destination = join(root, 'clone-miss');
  const missing = join(root, 'no-such.git');

  const form = await openCloneForm(page);

  await form.getByLabel('URL').fill(missing);
  await form.getByLabel('Destination').fill(destination);
  await page.getByRole('button', { name: 'Clone…' }).click();
  await page.getByRole('button', { name: 'Clone', exact: true }).click();

  await expect(page.getByRole('alert')).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole('alert')).toContainText(/fatal|does not appear|not found/i);
});

test('a path outside the root is refused before the confirmation', async ({ page }) => {
  const form = await openCloneForm(page);

  await form.getByLabel('URL').fill('https://example.test/x.git');
  await form.getByLabel('Destination').fill('/tmp/yagit-clone-escape');
  await page.getByRole('button', { name: 'Clone…' }).click();

  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('alert')).toContainText(/outside|allowed root|forbidden/i);
});
