import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Making and unmaking branches, through the interface, against a real git.
 *
 * The unit tests say which row offers what; these say the command reaches git
 * and that git did what the button claimed. Each exists for one reason and
 * would be pointless anywhere else:
 *
 * The name that looks like an option. `git branch -m release`, in a repository
 * on main, renames main to release — it creates nothing, exits 0, and takes
 * away the branch the person was standing on. Nothing but running git can show
 * that the separator held, because what proves it is the branch that is still
 * there afterwards.
 *
 * The command in the delete confirmation. It is answered by the daemon and
 * drawn by the dialog, so the only place the two can be checked against each
 * other is a browser talking to a daemon.
 *
 * The two merges. A merge is one of two different commands depending on where
 * the branches stand, the daemon decides which by reading them, and what
 * proves the right one ran is the shape of the history afterwards: one parent
 * where the dialog promised no commit, two where it promised one.
 *
 * The two rebases. Same question, and a worse answer to get wrong: the two
 * arrangements below are the ones a one-directional commit count read as each
 * other, so one of them promised a rewrite git refuses to perform and the
 * other called a rewrite "changes nothing". What proves which one ran is the
 * SHA of the branch afterwards — moved and rewritten, or moved and identical.
 *
 * Its own fixture per test: these rename and delete branches, and a repository
 * left in another shape is a repository every test after it is wrong about.
 */

/** The repositories these tests have asked the daemon to open. */
const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/**
 * git in one repository, reading no configuration of the machine's own.
 *
 * Whoever runs this may sign every commit by default or have merge.ff set to
 * something of their own, and a fixture built through their settings is a test
 * that passes or fails depending on whose laptop it is.
 */
function gitIn(path: string) {
  return (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });
}

/** What a commit's parents are, which is how a fast-forward is told apart. */
function parentsOf(path: string, reference: string): string[] {
  const line = gitIn(path)('rev-list', '-1', '--parents', reference).toString().trim();
  return line.split(/\s+/).slice(1);
}

/**
 * A repository on main, with one other branch holding a commit main cannot
 * reach.
 *
 *   main   first ─── second on main   ← checked out
 *   side   first ─── second on side
 *
 * `side` being unmerged is the point: it is what makes `git branch -d` refuse
 * and `-D` the command the confirmation has to show.
 */
async function openBranchedRepository(page: Page, name: string): Promise<string> {
  await openWorkbench(page);

  const path = scratchFixture(name);
  rmSync(path, { recursive: true, force: true });
  mkdirSync(path, { recursive: true });

  const git = gitIn(path);
  const write = (line: string) => writeFileSync(join(path, 'notes.md'), line + '\n');

  git('init', '-b', 'main');

  // Written into the repository rather than passed per command, because the
  // DAEMON commits here too — a merge that is not a fast-forward is a commit
  // it makes — and the daemon under test runs with whatever configuration the
  // machine has. An identity it cannot find is a 128 before any of the work;
  // a global commit.gpgsign is a merge that waits on a passphrase nobody can
  // type. Both are answered here, where the daemon will read them.
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');
  git('config', 'commit.gpgsign', 'false');

  write('first');
  git('add', '-A');
  git('commit', '-m', 'first');

  git('switch', '-c', 'side');
  write('the side branch wrote this');
  git('commit', '-am', 'second on side');

  git('switch', 'main');
  write('main wrote this');
  git('commit', '-am', 'second on main');

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

/** The sidebar row for a branch, by the name drawn on it. */
function branchRow(page: Page, name: string) {
  // Exact: accessible-name matching is a substring search, and "More actions
  // for side" is inside "More actions for sidetrack" — which is exactly the
  // pair the rename test ends with.
  return page.getByRole('button', { name: `More actions for ${name}`, exact: true });
}

/**
 * Opens a row's menu.
 *
 * The hover is the interaction and not a workaround for it: the actions laid
 * over a row's right edge are transparent and inert until the row is hovered,
 * so a click near that edge reaches the row rather than an invisible target.
 * `force` on the hover only — the trigger is inert at that moment by design,
 * so the pointer lands on the row underneath, which is what the reveal listens
 * to. The click that follows is checked the ordinary way.
 */
async function openRowMenu(page: Page, name: string) {
  const trigger = branchRow(page, name);
  await trigger.hover({ force: true });
  await trigger.click();
  await expect(page.getByRole('menu')).toBeVisible();
}

test('creates a branch and stands on it', async ({ page }) => {
  await openBranchedRepository(page, 'br-create');

  await expect(branchLabel(page)).toHaveText('on main');

  await page.getByRole('button', { name: 'New branch' }).click();
  // The dialog says where the branch will start, because "it will start at
  // main" is the difference between telling somebody what is about to happen
  // and assuming they know.
  await expect(page.getByRole('dialog')).toContainText('It will start at main.');

  await page.getByLabel('Branch name').fill('feature/lanes');
  await page.getByRole('button', { name: 'Create' }).click();

  await expect(branchLabel(page)).toHaveText('on feature/lanes');
  await expect(branchRow(page, 'feature/lanes')).toBeAttached();
});

test('creates a branch and stays where it was', async ({ page }) => {
  await openBranchedRepository(page, 'br-parked');

  await page.getByRole('button', { name: 'New branch' }).click();
  await page.getByLabel('Branch name').fill('parked');
  // Two different commands, not a flag on one, and the box is what chooses
  // between them before the click rather than after it.
  await page.getByRole('checkbox', { name: 'Check it out' }).uncheck();
  await page.getByRole('button', { name: 'Create' }).click();

  await expect(branchRow(page, 'parked')).toBeAttached();
  await expect(branchLabel(page)).toHaveText('on main');
});

test('renames a branch from its row menu', async ({ page }) => {
  await openBranchedRepository(page, 'br-rename');

  await openRowMenu(page, 'side');
  await page.getByRole('menuitem', { name: 'Rename…' }).click();

  // Opened on the name it has: most renames change part of a name, and an
  // empty box makes somebody retype the part they were keeping.
  const field = page.getByLabel('New name');
  await expect(field).toHaveValue('side');

  await field.fill('sidetrack');
  await page.getByRole('dialog').getByRole('button', { name: 'Rename' }).click();

  await expect(branchRow(page, 'sidetrack')).toBeAttached();
  await expect(branchRow(page, 'side')).toHaveCount(0);
});

test('deletes a branch after showing the command and what goes with it', async ({ page }) => {
  await openBranchedRepository(page, 'br-delete');

  await openRowMenu(page, 'side');
  await page.getByRole('menuitem', { name: 'Delete…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  // The exact line, and it is the daemon's: the browser never assembles it,
  // so this is the one place the line shown and the line git receives can be
  // checked against each other. The separator is in it, which is the whole
  // reason the command is worth reading.
  await expect(dialog).toContainText('git branch -D -- side');
  await expect(dialog).toContainText('the branch side');
  await expect(dialog).toContainText('any commits nothing else still points at');

  await dialog.getByRole('button', { name: 'Delete' }).click();

  await expect(branchRow(page, 'side')).toHaveCount(0);
  await expect(branchLabel(page)).toHaveText('on main');
});

test('puts a deleted branch back, at the commit it pointed at', async ({ page }) => {
  const path = await openBranchedRepository(page, 'br-restore');
  const sideTip = gitIn(path)('rev-parse', 'side').toString().trim();

  await openRowMenu(page, 'side');
  await page.getByRole('menuitem', { name: 'Delete…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
  await expect(branchRow(page, 'side')).toHaveCount(0);

  // The offer names the branch rather than the commit: it is the thing coming
  // back, and nothing in the reflog knows it ever left.
  await page.getByRole('button', { name: 'Restore side' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('git branch -- side');
  await dialog.getByRole('button', { name: 'Restore branch' }).click();

  await expect(branchRow(page, 'side')).toBeAttached();
  expect(gitIn(path)('rev-parse', 'side').toString().trim()).toBe(sideTip);
  // A restore makes a branch and moves nobody: HEAD stays where it was.
  await expect(branchLabel(page)).toHaveText('on main');
});

test('offers the delete on the branch HEAD is on, and refuses it', async ({ page }) => {
  await openBranchedRepository(page, 'br-current');

  await openRowMenu(page, 'main');

  // Shown rather than dropped, because git refuses them and the reason is
  // worth reading — and reachable rather than skipped, because the arrows are
  // the only way through a menu and an item they step over is one a screen
  // reader can never be told about. All three of the row's acting items are
  // refused on the branch HEAD is on: deleting it, merging it into itself, and
  // rebasing it onto itself.
  const rebase = page.getByRole('menuitem', { name: 'Rebase current branch onto this…' });
  await expect(rebase).toHaveAttribute('aria-disabled', 'true');
  await expect(rebase).toContainText('onto itself');

  const refused = page.getByRole('menuitem', { name: 'Delete…' });
  await expect(refused).toHaveAttribute('aria-disabled', 'true');
  await expect(refused).toContainText('cannot be deleted');

  const merge = page.getByRole('menuitem', { name: 'Merge into current branch…' });
  await expect(merge).toHaveAttribute('aria-disabled', 'true');
  await expect(merge).toContainText('into itself');

  await page.keyboard.press('ArrowDown');
  await expect(merge).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(rebase).toBeFocused();
  await page.keyboard.press('ArrowDown');
  await expect(refused).toBeFocused();

  await page.keyboard.press('Enter');
  // Nothing ran, and the menu is still open: a menu that shut on a click that
  // did nothing would read as an action that silently failed.
  await expect(page.getByRole('menu')).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('a branch name that is also a git option is refused, and nothing is renamed', async ({
  page,
}) => {
  await openBranchedRepository(page, 'br-dashed');

  await page.getByRole('button', { name: 'New branch' }).click();
  await page.getByLabel('Branch name').fill('-m');
  await page.getByRole('button', { name: 'Create' }).click();

  // git's own words, whole: the sentence, the command that produced it and the
  // raw stderr under it. Without the separator this request would instead have
  // run `git switch --create` with no name and `-m` read as an option — or, in
  // the create-only path, `git branch -m <start>`, which renames the branch the
  // person was standing on and reports success for one nobody created.
  const refusal = page.getByRole('alert').filter({ hasText: 'Could not create -m' });
  await expect(refusal).toBeVisible();
  await expect(refusal).toContainText('git switch --create -m');
  await expect(refusal.locator('pre')).toContainText("fatal: '-m' is not a valid branch name");

  // The proof: main is still main, and it is still what HEAD is on.
  await expect(branchLabel(page)).toHaveText('on main');
  await expect(branchRow(page, 'main')).toBeAttached();
});

test('fast-forwards a branch after showing the command', async ({ page }) => {
  const path = await openBranchedRepository(page, 'br-ff');
  const git = gitIn(path);

  // A branch one commit past main, with main holding nothing of its own since:
  // the merge is the pointer moving, and no commit is written at all.
  git('switch', '-c', 'pickup');
  writeFileSync(join(path, 'pickup.md'), 'only on pickup\n');
  git('add', '-A');
  git('commit', '-m', 'pickup commit');
  git('switch', 'main');

  await page.reload();
  await page.getByRole('tab', { name: /scratch-br-ff / }).click();

  await openRowMenu(page, 'pickup');
  await page.getByRole('menuitem', { name: 'Merge into current branch…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Merge pickup into main?');
  // The flag is the whole point of the round trip: without it the same click
  // is a merge commit on a machine whose merge.ff is false, and the line on
  // screen would be describing a different operation.
  await expect(dialog).toContainText('git merge --ff-only -- refs/heads/pickup');
  await expect(dialog).toContainText('Nothing is committed');

  await dialog.getByRole('button', { name: 'Merge' }).click();

  await expect(dialog).toHaveCount(0);
  await expect(branchLabel(page)).toHaveText('on main');
  await expect(
    page.getByRole('status').filter({ hasText: 'Fast-forwarded main to pickup' }),
  ).toBeVisible();

  // What the toast claims, checked against git: main is the commit pickup
  // names, and it got there without a merge commit.
  const main = git('rev-parse', 'main').toString().trim();
  expect(main).toBe(git('rev-parse', 'pickup').toString().trim());
  expect(parentsOf(path, 'main')).toHaveLength(1);
});

test('creates a merge commit when a fast-forward was possible but refused', async ({ page }) => {
  const path = await openBranchedRepository(page, 'br-merge-anyway');
  const git = gitIn(path);

  git('switch', '-c', 'pickup');
  writeFileSync(join(path, 'pickup.md'), 'only on pickup\n');
  git('add', '-A');
  git('commit', '-m', 'pickup commit');
  git('switch', 'main');

  await page.reload();
  await page.getByRole('tab', { name: /scratch-br-merge-anyway / }).click();

  await openRowMenu(page, 'pickup');
  await page.getByRole('menuitem', { name: 'Merge into current branch…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('git merge --ff-only -- refs/heads/pickup');

  await dialog.getByText('Create a merge commit anyway').click();
  await expect(dialog).toContainText('git merge --no-ff');

  await dialog.getByRole('button', { name: 'Merge' }).click();

  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole('status').filter({ hasText: 'Merged pickup into main' }),
  ).toBeVisible();
  expect(parentsOf(path, 'main')).toHaveLength(2);
});

// The other half of the same question, and the one where git commits: two
// branches that both moved, merged by the daemon under the user's identity.
test('records a merge commit when the branches have both moved', async ({ page }) => {
  const path = await openBranchedRepository(page, 'br-nonff');
  const git = gitIn(path);

  // A branch off main touching a file main never touches, and a commit on main
  // after it: divergent, and mergeable without a conflict.
  git('switch', '-c', 'elsewhere');
  writeFileSync(join(path, 'elsewhere.md'), 'only over here\n');
  git('add', '-A');
  git('commit', '-m', 'elsewhere commit');

  git('switch', 'main');
  writeFileSync(join(path, 'main.md'), 'only on main\n');
  git('add', '-A');
  git('commit', '-m', 'third on main');

  await page.reload();
  await page.getByRole('tab', { name: /br-nonff/ }).click();

  await openRowMenu(page, 'elsewhere');
  await page.getByRole('menuitem', { name: 'Merge into current branch…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Merge elsewhere into main?');
  await expect(dialog).toContainText(
    `git merge --no-ff --no-edit -m "Merge branch 'elsewhere' into main" -- refs/heads/elsewhere`,
  );
  // The sentence says what the command cannot: a commit is about to be made,
  // under whatever hooks and signing configuration this repository has.
  await expect(dialog).toContainText('merge commit');

  await dialog.getByRole('button', { name: 'Merge' }).click();

  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole('status').filter({ hasText: 'Merged elsewhere into main' }),
  ).toBeVisible();

  // Two parents, and the second is the branch that was brought in — which is
  // the only proof that the merge happened rather than something that looked
  // like it.
  const parents = parentsOf(path, 'main');
  expect(parents).toHaveLength(2);
  expect(parents[1]).toBe(git('rev-parse', 'elsewhere').toString().trim());
});

// The arrangement a one-directional count called "changes nothing": main has
// no commit of its own, so git moves it and rewrites the work tree under it.
// Nothing is replayed, so nothing is destroyed and the dialog says neither.
test('fast-forwards the current branch onto another after showing the command', async ({
  page,
}) => {
  const path = await openBranchedRepository(page, 'rb-ff');
  const git = gitIn(path);

  // A branch one commit past main, with main holding nothing of its own since.
  git('switch', '-c', 'ahead');
  writeFileSync(join(path, 'ahead.md'), 'only on ahead\n');
  git('add', '-A');
  git('commit', '-m', 'ahead commit');
  git('switch', 'main');

  await page.reload();
  await page.getByRole('tab', { name: /rb-ff/ }).click();

  await openRowMenu(page, 'ahead');
  await page.getByRole('menuitem', { name: 'Rebase current branch onto this…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText('Rebase main onto ahead?');
  // No --no-ff: on an outcome that writes nothing the flag would mean "rebase
  // forced", which rewrites the branch the sentence said would not move.
  await expect(dialog).toContainText(
    'git rebase --merge --no-autosquash --no-autostash --no-rebase-merges ' +
      '--no-update-refs -- refs/heads/ahead',
  );
  // Both halves of what a fast-forward is: no history rewritten, and every
  // file under the branch replaced anyway.
  await expect(dialog).toContainText('Nothing is replayed and no commit is rewritten');
  await expect(dialog).toContainText("the files in your work tree become ahead's");
  // And no red panel, because nothing goes: a warning over an operation that
  // loses nothing is how people learn to read past the one that does.
  await expect(dialog).not.toContainText('This will permanently discard');

  await dialog.getByRole('button', { name: 'Rebase' }).click();

  await expect(dialog).toHaveCount(0);
  await expect(branchLabel(page)).toHaveText('on main');
  await expect(
    page.getByRole('status').filter({ hasText: 'Fast-forwarded main to ahead' }),
  ).toBeVisible();

  // What the toast claims, checked against git: main is the commit ahead
  // names, and it got there without a new object being written.
  expect(git('rev-parse', 'main').toString().trim()).toBe(
    git('rev-parse', 'ahead').toString().trim(),
  );
});

// The other half, and the one that destroys something: both branches have
// moved, so main's own commit is written again on a new base under a new hash.
// The dialog has to say so before the button does it.
test('replays the current branch onto another, and names what it rewrites', async ({ page }) => {
  const path = await openBranchedRepository(page, 'rb-replay');
  const git = gitIn(path);

  // A branch off the first commit touching a file main never touches:
  // divergent, and replayable without a conflict.
  git('switch', '-c', 'elsewhere', 'main~1');
  writeFileSync(join(path, 'elsewhere.md'), 'only over here\n');
  git('add', '-A');
  git('commit', '-m', 'elsewhere commit');
  git('switch', 'main');

  const before = git('rev-parse', 'main').toString().trim();

  await page.reload();
  await page.getByRole('tab', { name: /rb-replay/ }).click();

  await openRowMenu(page, 'elsewhere');
  await page.getByRole('menuitem', { name: 'Rebase current branch onto this…' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Rebase main onto elsewhere?');
  await expect(dialog).toContainText(
    'git rebase --merge --no-autosquash --no-autostash --no-rebase-merges ' +
      '--no-update-refs --no-ff -- refs/heads/elsewhere',
  );
  // The sentence says what the command cannot: these commits do not survive as
  // they are.
  await expect(dialog).toContainText('new hashes');
  // And the panel names what goes, which is the whole of ConfirmDialog's
  // destructive contract.
  await expect(dialog).toContainText('This will permanently discard');
  await expect(dialog).toContainText('the 1 commit main points at now');

  await dialog.getByRole('button', { name: 'Rebase' }).click();

  await expect(dialog).toHaveCount(0);
  await expect(
    page.getByRole('status').filter({ hasText: 'Rebased main onto elsewhere' }),
  ).toBeVisible();

  // One parent, and it is the branch that was rebased onto — which is the only
  // proof the replay happened rather than something that looked like it. And a
  // different commit from the one main was: the dialog promised a rewrite.
  const after = git('rev-parse', 'main').toString().trim();
  expect(after).not.toBe(before);
  const parents = parentsOf(path, 'main');
  expect(parents).toHaveLength(1);
  expect(parents[0]).toBe(git('rev-parse', 'elsewhere').toString().trim());
});
