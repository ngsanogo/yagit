import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Checking out, through the interface, against a real daemon and a real git.
 *
 * The unit tests prove which command each row would send; these prove the
 * command reaches git, that HEAD ends up where the button said, and that the
 * screen agrees afterwards. Every step between those is a place where the
 * right command can be run against the wrong reference — and the step this
 * suite exists for is the last one: a checkout changes the history being
 * walked, the files being listed and the branch being named, and all three are
 * drawn from caches that have to be dropped at the same moment.
 *
 * Its own fixture per test, unlike the history suite's shared one. These tests
 * move HEAD, and a repository left on another branch is a repository every
 * test after it is wrong about.
 */

/** The repositories these tests have asked the daemon to open. */
const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/**
 * A repository with two branches and a tag, checked out on main.
 *
 *   main     first ─── second on main    ← checked out, tag v1.0 on the tip
 *   side     first ─── second on side
 *
 * `notes.md` differs between the two, which is what makes a checkout with
 * uncommitted work in that file something git has to refuse.
 *
 * No subject here reads "on <something>": the label above the history says
 * "on main", and a commit whose subject matched it would make every assertion
 * about the label ambiguous between the two.
 */
async function openBranchedRepository(page: Page, name: string): Promise<string> {
  await openWorkbench(page);

  // A scratch name, so the sweep in global-setup can recognise it: these are
  // built from nothing by the test that wants them, and a name it cannot
  // classify is one it leaves on disk forever.
  const path = scratchFixture(name);
  rmSync(path, { recursive: true, force: true });
  mkdirSync(path, { recursive: true });

  // An empty configuration: whoever runs this may sign every commit by
  // default, and the test would then pass or fail depending on whose machine
  // it is.
  const git = (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });

  const write = (line: string) => writeFileSync(join(path, 'notes.md'), line + '\n');

  git('init', '-b', 'main');
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');

  write('first');
  git('add', '-A');
  git('commit', '-m', 'first');

  git('checkout', '-b', 'side');
  write('the side branch wrote this');
  git('commit', '-am', 'second on side');

  git('checkout', 'main');
  write('main wrote this');
  git('commit', '-am', 'second on main');
  git('tag', 'v1.0');

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

/** What the interface says the repository is on, above the history. */
function branchLabel(page: Page) {
  return page.getByText(/^on \S+$/);
}

/**
 * Checks a reference out from the sidebar.
 *
 * The pointer is put on the row before the click, and that is the interaction
 * rather than a workaround for it. A row's own click takes the history
 * somewhere; the button laid over its right edge is transparent and inert
 * until the row is hovered, so that a click near that edge reaches the row
 * rather than an invisible target. A person moving a mouse to the button
 * crosses the row on the way and never notices. A test that jumps straight to
 * the coordinates does not, which is why it says so here.
 *
 * `force` on the hover only: the button is inert at that moment by design, so
 * the pointer lands on the row underneath — which is what the reveal listens
 * to. The click that follows is checked the ordinary way, and fails if the
 * reveal did not happen.
 */
async function checkOutFromSidebar(page: Page, accessibleName: string): Promise<void> {
  const action = page.getByRole('button', { name: accessibleName });
  await action.hover({ force: true });
  await action.click();
}

test('checking out a branch moves HEAD, and says which command did it', async ({ page }) => {
  await openBranchedRepository(page, 'co-branch');

  await expect(branchLabel(page)).toHaveText('on main');

  await checkOutFromSidebar(page, 'Check out side');

  // The header, the toast and the sidebar's own marker all move together, and
  // they are read from three different places: the polled status, the answer
  // to the switch, and the reference list the answer replaced.
  await expect(page.getByText('Now on side')).toBeVisible();
  await expect(branchLabel(page)).toHaveText('on side');
  await expect(page.locator('button[aria-current="true"]')).toContainText('side');

  // `git switch`, never `git checkout`: the log panel is where somebody learns
  // the command, and `git checkout side` would be a command that means
  // something else in a repository holding a file called `side`.
  await page.getByRole('button', { name: 'Git log' }).click();
  // The session log lists every worker's commands; this suite shares one daemon.
  await expect(
    page
      .getByRole('region', { name: 'Git log' })
      .getByText('git switch --no-guess -- side')
      .first(),
  ).toBeVisible();
});

test('checking out a tag detaches HEAD, and the screen says so', async ({ page }) => {
  await openBranchedRepository(page, 'co-tag');

  // The accessible name says what the visible label cannot fit: this one does
  // not leave the repository on a branch.
  await checkOutFromSidebar(page, 'Check out v1.0, leaving HEAD detached');

  // Exactly, and the case is why: the sidebar grows a "Detached HEAD" heading
  // at the same moment, and a loose match would be two elements or one
  // depending on which of the two answers landed first.
  await expect(page.getByText('detached HEAD', { exact: true })).toBeVisible();

  // `for-each-ref` lists no HEAD, so the sidebar grows a group of its own for
  // it. Without that, the screen would name every branch and none of them
  // would be the one the repository is at.
  await expect(page.getByRole('heading', { name: 'Detached HEAD' })).toBeVisible();
});

test('a commit in the history can be checked out from its panel', async ({ page }) => {
  await openBranchedRepository(page, 'co-commit');

  const commits = page.getByRole('list', { name: 'Commits' });
  await commits.getByText('first', { exact: true }).click();

  // The panel's button, not the sidebar's: a row of the history is a commit
  // and nothing else, so this is the one checkout that can only ever detach.
  await page
    .getByRole('region', { name: 'Commit' })
    .getByRole('button', { name: /^Check out/ })
    .click();

  await expect(page.getByText('detached HEAD', { exact: true })).toBeVisible();

  // The history is walked from HEAD under the default scope, so a checkout
  // that went back two commits leaves one commit reachable. A stale count here
  // is the cache the checkout failed to drop.
  await expect(page.getByRole('heading', { name: 'History — 1' })).toBeVisible();
});

test('a checkout git refuses says what is in the way', async ({ page }) => {
  const path = await openBranchedRepository(page, 'co-refused');

  // The file differs between the branches, so this change cannot be carried
  // across — git refuses the whole switch rather than overwrite it.
  writeFileSync(join(path, 'notes.md'), 'work nobody has committed\n');

  await checkOutFromSidebar(page, 'Check out side');

  const refusal = page.getByRole('status').filter({ hasText: 'Could not check out side' });
  await expect(refusal).toBeVisible();
  // git's own words, whole. Nothing else on the screen can say which file is
  // in the way, and a "something went wrong" here would leave the user with a
  // button that does nothing and no way to find out why.
  await expect(refusal).toContainText('notes.md');
  await expect(refusal).toContainText('git switch --no-guess -- side');

  // And the repository did not move.
  await expect(branchLabel(page)).toHaveText('on main');
});
