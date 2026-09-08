import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench } from './session';

/**
 * Making a repository through the interface.
 *
 * The one thing only a browser talking to a daemon can show here: the first
 * branch's name is a setting on the daemon's machine, so the field fills from
 * the plan and the command names what it fills with.
 */

const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/** A fixture directory that exists, to make repositories inside. */
function scratchParent(name: string): string {
  const root = scratchFixture(name);
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  return root;
}

async function openInitForm(page: Page): Promise<void> {
  await openWorkbench(page);
  await page.getByRole('button', { name: 'Add repository' }).click();

  // Scoped to the dialog, like the clone form beside it: the empty state
  // behind it offers the same control, and which one an unscoped locator finds
  // depends on what other workers have open in the daemon they share.
  const dialog = page.getByRole('dialog', { name: 'Add a repository' });
  await dialog.getByRole('radio', { name: 'Create' }).click();
  await expect(dialog.getByRole('heading', { name: 'Make a repository' })).toBeVisible();
}

test('makes a repository on a named branch and opens it', async ({ page }) => {
  const parent = scratchParent('init-named');
  const destination = join(parent, 'init-named');

  await openInitForm(page);

  await page.getByLabel('Folder').fill(destination);
  await page.getByLabel('First branch').fill('trunk');
  await page.getByRole('button', { name: 'Create…' }).click();

  await expect(page.getByText('yagit will run')).toBeVisible();
  await expect(page.getByText(/git init --initial-branch=trunk --/)).toBeVisible();

  await page.getByRole('button', { name: 'Create', exact: true }).click();

  await expect(page.getByRole('tab', { name: /init-named/ })).toBeVisible({ timeout: 30_000 });
  opened.push(destination);

  const head = execFileSync('git', ['symbolic-ref', 'HEAD'], {
    cwd: destination,
    env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
  })
    .toString()
    .trim();
  expect(head).toBe('refs/heads/trunk');
});

test('fills the first branch from the daemon when the field is left empty', async ({ page }) => {
  const parent = scratchParent('init-default');
  const destination = join(parent, 'init-default');

  await openInitForm(page);

  const branch = page.getByLabel('First branch');
  await expect(branch).toHaveValue('');
  await page.getByLabel('Folder').fill(destination);
  await page.getByRole('button', { name: 'Create…' }).click();

  // The daemon read init.defaultBranch and answered with a name; whatever it
  // is, the command pins it and the field now shows it.
  await expect(page.getByText(/git init --initial-branch=\S+ --/)).toBeVisible();
  await page.getByRole('button', { name: 'Back' }).click();
  await expect(branch).not.toHaveValue('');
});

test('a folder outside the root is refused before the confirmation', async ({ page }) => {
  await openInitForm(page);

  await page.getByLabel('Folder').fill('/tmp/yagit-init-escape');
  await page.getByRole('button', { name: 'Create…' }).click();

  await expect(page.getByRole('alert')).toBeVisible();
  await expect(page.getByRole('alert')).toContainText(/outside|allowed root|forbidden/i);
});
