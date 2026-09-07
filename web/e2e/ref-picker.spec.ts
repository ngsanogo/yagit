import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Drawing the graph from the references you choose.
 *
 * What only a browser talking to a daemon shows: ticking a box changes which
 * commits the list holds, and the choice survives the request that carries it
 * — the refs travel as repeated `ref=` parameters and the daemon walks exactly
 * those. See docs/adr/0033.
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

/**
 * A topic that left main and was never merged back.
 *
 *	main:  root ─── "main moved on"
 *	          ╲
 *	topic:     ─── "the topic tip"
 *
 * The shape neither older scope answers: main alone cannot show the
 * divergence, and every ref is the same picture here only because this
 * repository is small.
 */
async function openDiverged(page: Page, name: string): Promise<void> {
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

  git('commit', '--allow-empty', '-m', 'root');
  git('checkout', '-b', 'topic');
  git('commit', '--allow-empty', '-m', 'the topic tip');
  git('checkout', 'main');
  git('commit', '--allow-empty', '-m', 'main moved on');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: new RegExp(`^${name} `) }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();
}

test('draws the graph from the references that are ticked', async ({ page }) => {
  await openDiverged(page, 'chosen-refs');

  const history = page.getByRole('region', { name: /^History/ });

  // The current branch: the topic's own commit is not in the walk.
  await expect(history).toContainText('main moved on');
  await expect(history).not.toContainText('the topic tip');

  // Switching to the picker opens it on the branch HEAD is on, so the first
  // click is a narrowing or a widening rather than a repair.
  await page.getByRole('radio', { name: 'Selected' }).click();
  await expect(page.getByRole('button', { name: /reference/ })).toBeVisible();
  await expect(history).toContainText('main moved on');
  await expect(history).not.toContainText('the topic tip');

  await page.getByRole('button', { name: /reference/ }).click();
  const picker = page.getByRole('dialog', { name: 'Draw the graph from' });
  await expect(picker).toBeVisible();

  await picker.getByRole('checkbox', { name: 'topic' }).check();
  await page.keyboard.press('Escape');
  await expect(picker).toBeHidden();

  // Both branches now, and the divergence between them is the picture.
  await expect(history).toContainText('the topic tip');
  await expect(history).toContainText('main moved on');
});

// The daemon refuses `scope=refs` with no ref — an empty walk drawn is
// indistinguishable from an empty repository — so the control makes that
// unreachable rather than answering a refusal.
test('will not let the last reference be unticked', async ({ page }) => {
  await openDiverged(page, 'last-reference');

  await page.getByRole('radio', { name: 'Selected' }).click();
  await page.getByRole('button', { name: /reference/ }).click();

  const picker = page.getByRole('dialog', { name: 'Draw the graph from' });
  const main = picker.getByRole('checkbox', { name: 'main' });
  await expect(main).toBeChecked();
  await expect(main).toBeDisabled();
  await expect(picker.getByText(/One reference has to stay ticked/)).toBeVisible();
});

// The filter is what makes the list usable on a repository with a thousand
// tags, which is the reason the control is checkboxes and not a select.
test('filters the reference list by name', async ({ page }) => {
  await openDiverged(page, 'filter-references');

  await page.getByRole('radio', { name: 'Selected' }).click();
  await page.getByRole('button', { name: /reference/ }).click();

  const picker = page.getByRole('dialog', { name: 'Draw the graph from' });
  await expect(picker.getByRole('checkbox', { name: 'topic' })).toBeVisible();

  const filter = picker.getByLabel('Filter');
  await filter.fill('top');
  await expect(filter).toHaveValue('top');
  await expect(picker.getByRole('checkbox', { name: 'topic' })).toBeVisible();
  await expect(picker.getByRole('checkbox', { name: 'main' })).toBeHidden();

  await filter.fill('nothing matches this');
  await expect(picker.getByText('No reference matches that.')).toBeVisible();
});
