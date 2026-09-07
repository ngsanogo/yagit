import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Fetching, pulling and pushing, through the interface, against a real git and
 * a real remote.
 *
 * The remote is a bare repository on the same disk. git treats a path exactly
 * as it treats a URL — the same refspecs, the same fast-forward rules, the
 * same refusals — so nothing here touches a network, and everything these
 * tests are about is exercised as it would be over ssh.
 *
 * What only a browser talking to a daemon can show, and what these exist for:
 *
 * The counts on the buttons. They are read from `git status`, polled while the
 * window has focus, and they are the only thing on the screen that says this
 * repository has fallen behind. A unit test can say what the bar draws for a
 * given status; only this can say the status arrives.
 *
 * The command in the publish dialog. It is answered by the daemon and drawn by
 * the dialog, so the only place the two can be checked against each other is
 * here — and the destination in it is read from an upstream the browser never
 * sees.
 *
 * Its own fixture per test: these push and pull, and a repository left in
 * another shape is a repository every test after it is wrong about.
 */

/** The repositories these tests have asked the daemon to open. */
const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/** git in a directory, with an empty configuration. */
function gitIn(cwd: string) {
  return (...args: string[]) =>
    execFileSync('git', args, {
      cwd,
      // Whoever runs this may sign every commit by default, and the test would
      // then pass or fail depending on whose machine it is.
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });
}

interface Cloned {
  /** The repository yagit has open. */
  work: string;
  /** The bare repository it pushes to: the "other machine". */
  server: string;
  /** A second checkout of the same server: somebody else, working. */
  elsewhere: string;
}

/**
 * Builds a server, a clone yagit opens, and a second clone standing in for
 * somebody else — then opens the first through the API.
 *
 *	server.git   first          ← bare
 *	work         first          ← on main, following origin/main, open in yagit
 *	elsewhere    first          ← another checkout of the same server
 */
async function openClone(page: Page, name: string): Promise<Cloned> {
  await openWorkbench(page);

  const root = scratchFixture(name);
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });

  const server = join(root, 'server.git');
  // Named after the test rather than "work": the tab bar holds every
  // repository this file has opened, and three tabs called work are three the
  // suite cannot tell apart.
  const work = join(root, name);
  const other = join(root, 'elsewhere');

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

  git('clone', server, other);
  const inOther = gitIn(other);
  inOther('config', 'user.name', 'Grace Hopper');
  inOther('config', 'user.email', 'grace@example.com');

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path: work },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(work);

  await page.reload();
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  return { work, server, elsewhere: other };
}

/** What the server has at a ref, as forty characters. */
function serverHas(server: string, ref: string): string {
  return execFileSync('git', ['rev-parse', '--verify', ref], { cwd: server, encoding: 'utf8' })
    .toString()
    .trim();
}

function localHas(work: string, revision: string): string {
  return execFileSync('git', ['rev-parse', '--verify', revision], { cwd: work, encoding: 'utf8' })
    .toString()
    .trim();
}

/** A commit made by somebody else, and pushed. */
function commitElsewhere(elsewhere: string, line: string) {
  const git = gitIn(elsewhere);
  writeFileSync(join(elsewhere, 'notes.md'), line + '\n');
  git('commit', '-am', line);
  git('push', 'origin', 'main:refs/heads/main');
}

test('fetches, then pulls what somebody else pushed', async ({ page }) => {
  const { work, elsewhere } = await openClone(page, 'rm-pull');

  const pull = page.getByRole('button', { name: 'Pull' });
  const fetch = page.getByRole('button', { name: 'Fetch' });

  // Level to begin with, and the button is still live: `git pull` fetches
  // before it integrates, so it is the one of the three that always does
  // something. What is absent is the count.
  await expect(pull).toBeEnabled();
  await expect(pull).not.toContainText('↓');

  commitElsewhere(elsewhere, 'second, from elsewhere');
  await fetch.click();

  // The count is the whole point of the fetch: it is the only thing on this
  // screen that says the repository has fallen behind.
  await expect(pull).toBeEnabled();
  await expect(pull).toContainText('↓1');

  await pull.click();

  await expect(page.getByRole('status').filter({ hasText: 'Pulled' })).toBeVisible();
  await expect(pull).not.toContainText('↓');
  expect(localHas(work, 'main')).toBe(localHas(work, 'origin/main'));
});

test('pushes a commit and stops offering to', async ({ page }) => {
  const { work, server } = await openClone(page, 'rm-push');

  const git = gitIn(work);
  writeFileSync(join(work, 'notes.md'), 'second\n');
  git('commit', '-am', 'second');

  const push = page.getByRole('button', { name: 'Push' });
  // The commit was made outside yagit, so what brings it on screen is the
  // watch on the git directory and the status poll behind it.
  await expect(push).toContainText('↑1');

  await push.click();

  await expect(
    page.getByRole('status').filter({ hasText: 'Pushed main to origin/main' }),
  ).toBeVisible();
  expect(serverHas(server, 'refs/heads/main')).toBe(localHas(work, 'main'));
  await expect(push).toBeDisabled();
});

test('publishes a branch, showing the command and recording where it went', async ({ page }) => {
  const { work, server } = await openClone(page, 'rm-publish');

  const git = gitIn(work);
  git('switch', '-c', 'feature');
  writeFileSync(join(work, 'notes.md'), 'on the feature branch\n');
  git('commit', '-am', 'a feature');

  // A different word for a different operation: this branch exists nowhere
  // else, so there is no upstream to push to and one has to be chosen.
  const publish = page.getByRole('button', { name: 'Publish branch' });
  await expect(publish).toBeVisible();
  await publish.click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  // The exact line, and it is the daemon's: the browser never assembles it, so
  // this is the one place the line shown and the line git receives can be
  // checked against each other. --set-upstream is the half that makes every
  // later push need no answer.
  await expect(dialog).toContainText(
    'git push --progress --set-upstream -- origin feature:refs/heads/feature',
  );

  await dialog.getByRole('button', { name: 'Publish' }).click();

  await expect(
    page.getByRole('status').filter({ hasText: 'Pushed feature to origin' }),
  ).toBeVisible();
  expect(serverHas(server, 'refs/heads/feature')).toBe(localHas(work, 'feature'));

  // And the branch follows it now, which is what turns the next push into an
  // ordinary one.
  await expect(page.getByRole('button', { name: 'Push', exact: false })).toBeVisible();
});

test('names what a force push overwrites before running it', async ({ page }) => {
  const { work, server } = await openClone(page, 'rm-force');

  const git = gitIn(work);
  writeFileSync(join(work, 'notes.md'), 'second\n');
  git('commit', '-am', 'second');
  git('push', 'origin', 'main:refs/heads/main');

  // The ordinary reason to force: the same commit, said differently.
  git('commit', '--amend', '-m', 'second, reworded');
  const rewritten = localHas(work, 'main');

  await expect(page.getByRole('button', { name: 'Push' })).toContainText('↑1');

  await page.getByRole('button', { name: 'More remote actions' }).click();
  await page.getByRole('menuitem', { name: 'Force push…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  // Never a bare --force. The lease is what turns "I have rewritten history"
  // into "I have rewritten history, and nobody has pushed since".
  await expect(dialog).toContainText(
    'git push --progress --force-with-lease --force-if-includes -- origin main:refs/heads/main',
  );
  await expect(dialog).toContainText('any commit on origin/main that main does not have');

  await dialog.getByRole('button', { name: 'Force push' }).click();

  await expect(page.getByRole('status').filter({ hasText: 'Pushed main' })).toBeVisible();
  expect(serverHas(server, 'refs/heads/main')).toBe(rewritten);
});

test('says under each button what it will do, including the refused ones', async ({ page }) => {
  const { work } = await openClone(page, 'rm-described');

  const git = gitIn(work);
  writeFileSync(join(work, 'notes.md'), 'second\n');
  git('commit', '-am', 'second');

  // Every one of the three, and the third is the one that can be lost: the
  // description is attached with cloneElement, which a component swallows
  // where an element would keep it. Nothing on screen shows the difference.
  for (const [name, sentence] of [
    ['Fetch', 'nothing here moves'],
    ['Pull', 'Fetch origin/main and bring in anything new'],
    ['Push', 'Send 1 commit to origin/main'],
  ] as const) {
    const button = page.getByRole('button', { name, exact: false }).first();
    const describedBy = await button.getAttribute('aria-describedby');
    expect(describedBy, `${name} has no description`).not.toBeNull();
    await expect(page.locator(`#${describedBy ?? ''}`)).toContainText(sentence);
  }
});

test("a refused remote action explains itself without setting the menu's width", async ({
  page,
}) => {
  const { work } = await openClone(page, 'rm-unpublished');

  // A branch nobody has pushed: pulling and force pushing are both refused by
  // the same fact, and both say so in the longest sentence the menu holds.
  gitIn(work)('switch', '-c', 'a-branch-that-follows-nothing');
  await expect(page.getByRole('button', { name: 'Publish' })).toBeVisible();

  await page.getByRole('button', { name: 'More remote actions' }).click();
  const menu = page.getByRole('menu');
  await expect(menu).toBeVisible();

  const refused = page.getByRole('menuitem', { name: 'Force push…', exact: true });
  await expect(refused).toHaveAttribute('aria-disabled', 'true');
  await expect(refused).toContainText('follows no branch on a remote');

  // The width is the point. A fixed popover shrinks to fit, so without a
  // ceiling the sentence above is one unwrapped 400px line hanging off a 28px
  // trigger — and the placement then slides the menu away from the button it
  // is anchored to. max-w-xs is 20rem; the assertion allows the border and
  // padding around it and nothing like a second sentence's worth.
  const box = await menu.boundingBox();
  expect(box, 'the open menu has no box to measure').not.toBeNull();
  expect(box?.width ?? 0).toBeLessThanOrEqual(20 * 16 + 4);

  // And the sentence is wrapped rather than clipped: an item taller than one
  // line is what a ceiling that actually applied looks like.
  const line = await refused.evaluate((item) => parseFloat(getComputedStyle(item).lineHeight));
  expect((await refused.boundingBox())?.height ?? 0).toBeGreaterThan(line * 2);
});

test('refuses the network operations that have no meaning on a detached HEAD', async ({ page }) => {
  const { work } = await openClone(page, 'rm-detached');

  gitIn(work)('switch', '--detach', 'HEAD');

  // Fetching still applies — it writes under refs/remotes and nowhere else —
  // and that is exactly why it is the one that stays.
  await expect(page.getByRole('button', { name: 'Fetch' })).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Pull' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Push' })).toBeDisabled();
});

test('adds, renames and removes a remote through Manage remotes', async ({ page }) => {
  const { work, server } = await openClone(page, 'rm-manage');
  const root = join(work, '..');
  const mirror = join(root, 'mirror.git');
  gitIn(root)('init', '--bare', '-b', 'main', mirror);

  await page.getByRole('button', { name: 'More remote actions' }).click();
  await page.getByRole('menuitem', { name: 'Manage remotes…' }).click();

  const dialog = page.getByRole('dialog', { name: 'Remotes' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('origin')).toBeVisible();

  await dialog.getByRole('button', { name: 'Add remote' }).click();
  const add = page.getByRole('dialog', { name: 'Add remote' });
  await add.getByLabel('Name').fill('mirror');
  await add.getByLabel('URL').fill(mirror);
  await add.getByRole('button', { name: 'Add' }).click();

  const remotesDialog = page.getByRole('dialog', { name: 'Remotes' });
  // Exact: the URL path also contains "mirror".
  const mirrorRow = remotesDialog
    .locator('li')
    .filter({ has: page.getByText('mirror', { exact: true }) });
  await expect(mirrorRow).toBeVisible();
  await mirrorRow.getByRole('button', { name: 'Rename' }).click();
  const rename = page.getByRole('dialog', { name: /Rename mirror/ });
  await rename.getByLabel('New name').fill('backup');
  await rename.getByRole('button', { name: 'Rename' }).click();

  const backupRow = remotesDialog
    .locator('li')
    .filter({ has: page.getByText('backup', { exact: true }) });
  await expect(backupRow).toBeVisible();
  await backupRow.getByRole('button', { name: 'Remove…' }).click();
  const confirm = page.getByRole('dialog', { name: /Remove remote backup/ });
  await expect(confirm.getByText(/git remote remove -- backup/)).toBeVisible();
  await confirm.getByRole('button', { name: 'Remove' }).click();

  await expect(remotesDialog.getByText('backup', { exact: true })).toHaveCount(0);
  await expect(remotesDialog.getByText('origin', { exact: true })).toBeVisible();

  // On disk: only origin remains, and the server it points at is unchanged.
  const listed = execFileSync('git', ['remote'], { cwd: work, encoding: 'utf8' }).toString().trim();
  expect(listed).toBe('origin');
  expect(serverHas(server, 'refs/heads/main')).toHaveLength(40);
});

test('changes a remote URL through Manage remotes', async ({ page }) => {
  const { work } = await openClone(page, 'rm-seturl');
  const root = join(work, '..');
  const alternate = join(root, 'alternate.git');
  gitIn(root)('init', '--bare', '-b', 'main', alternate);

  await page.getByRole('button', { name: 'More remote actions' }).click();
  await page.getByRole('menuitem', { name: 'Manage remotes…' }).click();

  const remotesDialog = page.getByRole('dialog', { name: 'Remotes' });
  const originRow = remotesDialog
    .locator('li')
    .filter({ has: page.getByText('origin', { exact: true }) });
  await originRow.getByRole('button', { name: 'Edit URL…' }).click();

  const edit = page.getByRole('dialog', { name: /Edit URL for origin/ });
  await edit.getByLabel('New URL').fill(alternate);
  await expect(edit).toContainText('git remote set-url -- origin');
  await edit.getByRole('button', { name: 'Save' }).click();

  await expect(remotesDialog.getByText(alternate)).toBeVisible();

  const url = execFileSync('git', ['remote', 'get-url', 'origin'], {
    cwd: work,
    encoding: 'utf8',
  })
    .toString()
    .trim();
  expect(url).toBe(alternate);
});

/** Opens a branch row's menu — same hover pattern as branches.spec.ts. */
async function openBranchMenu(page: Page, name: string) {
  const trigger = page.getByRole('button', { name: `More actions for ${name}`, exact: true });
  await trigger.hover({ force: true });
  await trigger.click();
  await expect(page.getByRole('menu')).toBeVisible();
}

test('sets and unsets a branch upstream through the sidebar', async ({ page }) => {
  const { work } = await openClone(page, 'rm-upstream');
  gitIn(work)('switch', '-c', 'solo');

  await page.reload();
  await page.getByRole('tab', { name: /rm-upstream/ }).click();

  await openBranchMenu(page, 'solo');
  await page.getByRole('menuitem', { name: 'Set upstream…' }).click();

  const setDialog = page.getByRole('dialog', { name: /Set upstream for solo/ });
  await setDialog.getByLabel('Branch on remote').fill('main');
  await expect(setDialog).toContainText('git branch --set-upstream-to=origin/main');
  await setDialog.getByRole('button', { name: 'Save' }).click();

  await expect(
    page.getByRole('status').filter({ hasText: 'solo now follows origin/main' }),
  ).toBeVisible();

  await openBranchMenu(page, 'solo');
  await page.getByRole('menuitem', { name: 'Change upstream…' }).click();
  await page.getByRole('button', { name: 'Cancel' }).click();

  await openBranchMenu(page, 'solo');
  await page.getByRole('menuitem', { name: 'Unset upstream…' }).click();

  const unset = page.getByRole('dialog', { name: /Unset upstream for solo/ });
  await expect(unset).toContainText('git branch --unset-upstream -- solo');
  await unset.getByRole('button', { name: 'Unset upstream' }).click();

  await expect(
    page.getByRole('status').filter({ hasText: 'solo follows nothing now' }),
  ).toBeVisible();

  const upstream = execFileSync(
    'git',
    ['for-each-ref', '--format=%(upstream:short)', 'refs/heads/solo'],
    { cwd: work, encoding: 'utf8' },
  )
    .toString()
    .trim();
  expect(upstream).toBe('');
});

test('a repository with no remote offers Add remote', async ({ page }) => {
  await openWorkbench(page);

  const root = scratchFixture('rm-none');
  rmSync(root, { recursive: true, force: true });
  mkdirSync(root, { recursive: true });
  const path = join(root, 'rm-none');

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
  await page.getByRole('tab', { name: /rm-none/ }).click();
  await expect(page.getByRole('heading', { name: /^History/ })).toBeVisible();

  await expect(page.getByRole('button', { name: 'Fetch' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Add remote' }).click();
  await expect(page.getByRole('dialog', { name: 'Add remote' })).toBeVisible();
});
