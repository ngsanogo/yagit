import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Searching the history from the interface.
 *
 * What only a browser talking to a daemon shows: a query typed into a box
 * reaches git as a literal string rather than as a pattern, and following a
 * result takes the graph to that commit instead of filtering it.
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
 * Three commits, each the only match for one of the four fields.
 *
 * The middle subject is the point of the whole feature: `fix(api)` is a broken
 * pattern to a regex engine and an ordinary thing for a person to type.
 */
async function openSearchable(page: Page, name: string): Promise<string> {
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

  writeFileSync(join(path, 'parser.go'), 'func tokenise() {}\n');
  git('add', '-A');
  git('commit', '-m', 'add the parser');

  writeFileSync(join(path, 'api.go'), 'func handler() {}\n');
  git('add', '-A');
  git('commit', '-m', 'fix(api): a paren (');

  writeFileSync(join(path, 'notes.md'), 'later\n');
  git('add', '-A');
  git('commit', '-m', 'the newest commit');

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

test('finds a commit by message and takes the graph to it', async ({ page }) => {
  await openSearchable(page, 'search-message');

  await page.getByRole('button', { name: 'Search…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Search the history' });
  await expect(dialog).toBeVisible();

  const box = dialog.getByLabel('Search');
  await expect(box).toHaveValue('');
  await box.fill('the parser');
  await expect(box).toHaveValue('the parser');
  await dialog.getByRole('button', { name: 'Search', exact: true }).click();

  const result = dialog.getByRole('button', { name: /add the parser/ });
  await expect(result).toBeVisible();
  await result.click();

  // The dialog closes and the commit is the one the panel below is showing —
  // the graph moved to the result rather than being filtered down to it.
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('region', { name: 'Commit' })).toContainText('add the parser');
  await expect(page.getByRole('region', { name: /^History/ })).toContainText('the newest commit');
});

// A search box is not a regex box: `fix(api)` is what people type, and it is
// an unbalanced pattern to `git log --grep` without --fixed-strings.
test('takes the query literally, parentheses and all', async ({ page }) => {
  await openSearchable(page, 'search-literal');

  await page.getByRole('button', { name: 'Search…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Search the history' });

  const box = dialog.getByLabel('Search');
  await expect(box).toHaveValue('');
  await box.fill('fix(api)');
  await expect(box).toHaveValue('fix(api)');
  await dialog.getByRole('button', { name: 'Search', exact: true }).click();

  await expect(dialog.getByRole('button', { name: /fix\(api\)/ })).toBeVisible();
  await expect(dialog.getByText(/--fixed-strings/)).toBeVisible();
});

test('searches by file, and says nothing matched when nothing does', async ({ page }) => {
  await openSearchable(page, 'search-file');

  await page.getByRole('button', { name: 'Search…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Search the history' });

  await dialog.getByRole('radio', { name: 'File' }).click();
  const box = dialog.getByLabel('Search');
  await expect(box).toHaveValue('');
  await box.fill('parser.go');
  await expect(box).toHaveValue('parser.go');
  await dialog.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(dialog.getByRole('button', { name: /add the parser/ })).toBeVisible();

  await box.fill('no-such-file.txt');
  await expect(box).toHaveValue('no-such-file.txt');
  await dialog.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(dialog.getByText('Nothing matched')).toBeVisible();
  // The question is on screen, so a search that found nothing can be told from
  // one that asked the wrong thing.
  await expect(dialog.getByText(/:\(literal\)no-such-file\.txt/)).toBeVisible();
});
