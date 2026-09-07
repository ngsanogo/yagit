import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { violations } from './accessibility';
import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * Staging, discarding and committing, through the interface, against a real
 * daemon and a real git.
 *
 * The unit tests prove the patch builder produces the right bytes; these prove
 * the bytes reach git, that git accepts them, and that what comes back is what
 * the screen then shows. Every step between those two is a place where the
 * right patch can be applied to the wrong file.
 */
/**
 * The repositories these tests have asked the daemon to open.
 *
 * Given back afterwards, and it is not tidiness. The daemon outlives the run
 * whenever `./do test e2e` reuses a `./do dev`, and the sweep in global-setup
 * refuses to delete a directory the daemon still holds — so a fixture nobody
 * closes is one nothing can ever remove.
 */
const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/**
 * A repository with one commit and a working directory in every state that
 * matters.
 *
 *   edited.txt      committed, then changed on disk
 *   untracked.txt   git has never seen it
 *   deleted.txt     committed, then removed
 *
 * Built fresh for each test rather than shared. These tests change the
 * working directory, which is exactly the state a shared fixture cannot have —
 * and a repository the daemon already holds cannot be rebuilt under it: it
 * refuses one whose git directory was replaced, correctly.
 */
async function openChangedRepository(page: Page, name: string): Promise<void> {
  await openWorkbench(page);

  // A scratch name, so the sweep can recognise it: these are rebuilt from
  // nothing by the test that wants them, and a name it cannot classify is one
  // it leaves on disk forever.
  const path = scratchFixture(name);
  rmSync(path, { recursive: true, force: true });
  mkdirSync(path, { recursive: true });

  // An empty configuration: whoever runs this may sign every commit by
  // default, and the test would then pass or fail depending on whose machine
  // it is. The identity is given here for the same reason.
  const git = (...args: string[]) =>
    execFileSync(
      'git',
      ['-c', 'user.name=Ada Lovelace', '-c', 'user.email=ada@example.com', ...args],
      {
        cwd: path,
        env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
      },
    );

  const write = (file: string, lines: string[]) =>
    writeFileSync(join(path, file), lines.join('\n') + '\n');

  git('init', '-b', 'main');
  write('edited.txt', ['one', 'two', 'three']);
  write('deleted.txt', ['goodbye']);
  git('add', '-A');
  git('commit', '-m', 'first: the committed state');

  // The identity again, on the repository itself: the daemon commits without
  // -c, and the isolated configuration leaves git with no author at all.
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');

  write('edited.txt', ['one', 'TWO', 'three', 'four']);
  write('untracked.txt', ['brand new']);
  rmSync(join(path, 'deleted.txt'));

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();

  // The workbench opens on the first repository the daemon lists, which on a
  // machine running several of these in parallel is not the one just made.
  await page.getByRole('tab', { name: new RegExp(name) }).click();
  await page.getByRole('radio', { name: /Changes/ }).click();
}

/**
 * A file's row in one of the two lists.
 *
 * Anchored at the start of the accessible name, which begins with the path.
 * A looser match would also find the row's own Stage and Discard buttons,
 * which name the same file — and finding three buttons where one was meant is
 * how a test starts asserting about whichever one came first.
 */
function fileRow(page: Page, path: string) {
  return page.getByRole('button', { name: new RegExp(`^${path.replace('.', '\\.')}, `) });
}

test('lists what differs, on the side it differs on', async ({ page }) => {
  await openChangedRepository(page, 'wd-lists');

  await expect(page.getByRole('heading', { name: /Changes — 0 staged, 3 unstaged/ })).toBeVisible();

  // All three states reach the list, and the untracked one is there as a FILE
  // rather than as the directory git would otherwise collapse it into.
  await expect(fileRow(page, 'edited.txt')).toBeVisible();
  await expect(fileRow(page, 'untracked.txt')).toBeVisible();
  await expect(fileRow(page, 'deleted.txt')).toBeVisible();

  // The count on the switch is what makes uncommitted work visible from the
  // history too, so it is not merely decoration.
  await expect(page.getByRole('radio', { name: /Changes/ })).toContainText('3');
});

test('shows a file diff, and stages the whole file', async ({ page }) => {
  await openChangedRepository(page, 'wd-stage-file');

  await fileRow(page, 'edited.txt').click();

  // The diff is the unstaged one: the index still holds the committed
  // content, and this is the difference between it and the disk.
  await expect(page.getByText('TWO', { exact: true })).toBeVisible();
  await expect(page.getByText('four', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: 'Stage edited.txt', exact: true }).click();

  await expect(page.getByRole('heading', { name: /Changes — 1 staged, 2 unstaged/ })).toBeVisible();
});

/**
 * The operation the whole patch builder exists for.
 *
 * One of the two changes in the file is staged and the other is not, which
 * leaves the same path in BOTH lists at once — the state no single status verb
 * can express, and the reason the interface has two lists rather than one with
 * a checkbox.
 */
test('stages part of a file, leaving the rest behind', async ({ page }) => {
  await openChangedRepository(page, 'wd-stage-lines');

  await fileRow(page, 'edited.txt').click();

  // The added line, and only that one. Its neighbours — the removal of "two"
  // and the addition of "TWO" — stay in the work tree.
  await page.getByRole('button', { name: /Added line: four/ }).click();
  await expect(page.getByText('1 selected')).toBeVisible();

  await page.getByRole('button', { name: 'Stage line' }).click();

  // Both lists hold edited.txt now.
  await expect(page.getByRole('heading', { name: /Changes — 1 staged, 3 unstaged/ })).toBeVisible();

  // And the staged half is the line that was chosen, not the whole file: the
  // substitution is still waiting in the unstaged list, on the same path.
  await expect(page.getByRole('heading', { name: 'Staged', exact: true })).toBeVisible();
  // exact, or "Stage edited.txt" also matches "Unstage edited.txt" — and the
  // whole point of this test is that both buttons exist at once.
  await expect(page.getByRole('button', { name: 'Unstage edited.txt', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Stage edited.txt', exact: true })).toBeVisible();
});

test('stages a hunk from its own button', async ({ page }) => {
  await openChangedRepository(page, 'wd-stage-hunk');

  await fileRow(page, 'edited.txt').click();
  await page.getByRole('button', { name: 'Stage hunk' }).click();

  // The fixture's file has one hunk, so its hunk is its whole change.
  await expect(page.getByRole('heading', { name: /Changes — 1 staged, 2 unstaged/ })).toBeVisible();
});

test('names what a discard destroys, and shows the command', async ({ page }) => {
  await openChangedRepository(page, 'wd-discard');

  await fileRow(page, 'untracked.txt').hover();
  await page.getByRole('button', { name: /Discard the changes to untracked\.txt/ }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  // An untracked file is not the same loss as a modified one — the file
  // itself goes — and the wording has to say so.
  await expect(dialog).toContainText('the file itself');

  // The exact command, which is the project's rule for anything destructive —
  // and exact means the pathspec too. `:(literal)` is what keeps a file named
  // `*` from making this line delete every untracked file in the work tree,
  // and a dialog that hid it would be showing a command git never receives.
  // The daemon composes this string; the browser only draws it.
  const command = "git clean --force -- ':(literal)untracked.txt'";
  await expect(dialog).toContainText(command);

  await dialog.getByRole('button', { name: 'Discard' }).click();

  await expect(page.getByRole('heading', { name: /Changes — 0 staged, 2 unstaged/ })).toBeVisible();
  await expect(fileRow(page, 'untracked.txt')).toBeHidden();

  // The other half of the promise: what the dialog showed is what ran. The two
  // strings have one source, and this is where that stops being a claim.
  await page.getByRole('button', { name: 'Git log' }).click();
  await expect(page.getByText(command).first()).toBeVisible();
});

test('commits what is staged, and the history says so', async ({ page }) => {
  await openChangedRepository(page, 'wd-commit');

  await fileRow(page, 'edited.txt').click();
  await page.getByRole('button', { name: 'Stage edited.txt', exact: true }).click();
  await expect(page.getByRole('heading', { name: /1 staged/ })).toBeVisible();

  await page.getByRole('textbox', { name: 'Commit message' }).fill('second: from the interface');
  await page.getByRole('button', { name: /^Commit 1 to main$/ }).click();

  await expect(page.getByRole('heading', { name: /Changes — 0 staged, 2 unstaged/ })).toBeVisible();

  // The notification says it happened, and then it goes. A stack that only
  // ever grew would stand over the screen for the rest of the session, and a
  // danger toast — which stays on purpose, because it carries the stderr —
  // would do it permanently. Waited out rather than dismissed: the timer is
  // the behaviour under test.
  const committed = page.getByText('Committed', { exact: true });
  await expect(committed).toBeVisible();
  await expect(committed).toBeHidden({ timeout: 10_000 });

  // The history is the other half of the proof: the commit exists, and the
  // event stream and the cache between them put it on screen.
  await page.getByRole('radio', { name: 'History' }).click();
  await expect(page.getByRole('heading', { name: 'History — 2' })).toBeVisible();
  await expect(page.getByText('second: from the interface')).toBeVisible();
});

/**
 * Amend rewrites a commit that already exists, and nothing exercised it.
 *
 * The half worth proving is not that git accepts `--amend`. It is that the box
 * offers a commit the button otherwise refuses — nothing is staged here, and a
 * reword is a perfectly good reason to be in it — that it asks before
 * rewriting, and that the history ends up one commit long rather than two.
 *
 * The dialog's wording is pinned because it is a promise about a command, and
 * the command it promises is asserted against the log of what actually ran.
 * That string is written out twice in this project, in the dialog and in the
 * daemon, which is safe here and only here: it holds no user input, so a test
 * that runs it once has covered every input it can have.
 */
test('amends the last commit, after showing the command it will run', async ({ page }) => {
  await openChangedRepository(page, 'wd-amend');

  // Nothing is staged, and the box says so — for an ordinary commit that is a
  // refusal, and the sentence names the one thing it is not a refusal for.
  await expect(page.getByText('Nothing is staged')).toBeVisible();

  await page.getByRole('checkbox', { name: 'Amend the last commit' }).check();

  const box = page.getByRole('textbox', { name: 'Commit message' });
  // The placeholder changes with the checkbox: the box is no longer where a
  // commit is written, it is where the last one is rewritten.
  await expect(box).toHaveAttribute('placeholder', 'Reword the last commit…');

  // And the box starts from the message being replaced, which git is asked for
  // when the checkbox is ticked and which arrives a request later. Waiting for
  // it is not politeness: a fill selects what is in the box and then inserts
  // over the selection, and a value written in between collapses that
  // selection — so the insert appends. The amend then committed one subject
  // reading "first: the committed statefirst: reworded from the interface",
  // which is how this test failed roughly one run in fifteen.
  await expect(box).toHaveValue('first: the committed state');

  await box.fill('first: reworded from the interface');
  await expect(box).toHaveValue('first: reworded from the interface');

  await page.getByRole('button', { name: 'Amend', exact: true }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('heading', { name: 'Replace the last commit?' })).toBeVisible();

  const command = 'git commit --file=- --cleanup=whitespace --amend';
  await expect(dialog).toContainText(command);

  await dialog.getByRole('button', { name: 'Amend' }).click();

  // Replaced, not added. Two commits here would mean --amend never reached
  // git, and the message alone could not tell the difference.
  await page.getByRole('radio', { name: 'History' }).click();
  await expect(page.getByRole('heading', { name: 'History — 1' })).toBeVisible();
  await expect(page.getByText('first: reworded from the interface')).toBeVisible();
  await expect(page.getByText('first: the committed state')).toBeHidden();

  // And what the dialog promised is what ran.
  await page.getByRole('button', { name: 'Git log' }).click();
  await expect(page.getByText(command).first()).toBeVisible();
});

/**
 * The command log is a promise, not a debugging aid: the user must be able to
 * learn git by watching yagit work. It is only true if what ran is shown.
 */
test('shows the git commands it ran', async ({ page }) => {
  await openChangedRepository(page, 'wd-log');

  await fileRow(page, 'edited.txt').click();
  await page.getByRole('button', { name: 'Stage edited.txt', exact: true }).click();

  await page.getByRole('button', { name: 'Git log' }).click();

  const log = page.getByRole('heading', { name: 'Git log' });
  await expect(log).toBeVisible();
  // first(): the log is the daemon's, not this test's, and the other specs
  // running beside it stage the same fixture file. One entry is the claim.
  //
  // `:(literal)` is part of what ran, so it is part of what is shown. Every
  // path yagit hands git is a literal pathspec — without it a file named `*`
  // is a wildcard by the time git reads it — and a log panel that tidied it
  // away would be teaching the reader a command that does something else.
  await expect(page.getByText("git add -- ':(literal)edited.txt'").first()).toBeVisible();
});

/**
 * The product screen, scanned like the showcase.
 *
 * accessibility.spec.ts covers the design system, where every component is
 * shown alone. This is the other half: components combined — two lists of
 * buttons, a diff of several hundred clickable lines, a form — in an
 * arrangement no showcase predicted. Contrast, names and roles all survive the
 * showcase and can still fail here.
 */
test('the changes view has no accessibility violations', async ({ page }) => {
  await openChangedRepository(page, 'wd-axe');
  await fileRow(page, 'edited.txt').click();

  // Against a mounted diff: axe on a pane still loading finds nothing and
  // passes, which is the most expensive kind of green.
  await expect(page.getByText('TWO', { exact: true })).toBeVisible();

  expect(await violations(page)).toEqual([]);
});
