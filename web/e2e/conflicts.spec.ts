import { expect, test, type Page } from '@playwright/test';

import { execFileSync } from 'node:child_process';
import { mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

import { violations } from './accessibility';
import { scratchFixture } from './fixtures';
import { closeRepositoryAt, openWorkbench, sessionToken } from './session';

/**
 * A merge that stopped, resolved without leaving the application.
 *
 * The one flow where every layer has to be right at once: git leaves markers
 * in a file, the daemon reads that file as text, the browser parses the
 * markers, a click rewrites the buffer, a save writes it back, and `git add`
 * ends the conflict. A unit test can prove each of those and none of them
 * proves the chain — and before this existed the chain was broken in the
 * middle, because `git diff` on an unmerged path answers in a format the diff
 * parser was never written for.
 */

/**
 * The repositories these tests have asked the daemon to open.
 *
 * Given back afterwards, and it is not tidiness. The daemon outlives the run
 * whenever `./do test e2e` reuses a `./do dev`, and the sweep in global-setup
 * refuses to delete a directory the daemon still holds — so a fixture nobody
 * closes is one nothing can ever remove, and a git repository per test would
 * accumulate under `.yagit/e2e/` for good.
 */
const opened: string[] = [];

test.afterEach(async ({ page }) => {
  for (const path of opened.splice(0)) {
    await closeRepositoryAt(page, path);
  }
});

/**
 * A repository whose merge of `feature` into `main` stopped on notes.md.
 *
 *   base    one   two    three
 *   main    one   MAIN   three   ← checked out
 *   feature one   SIDE   three
 *
 * `stop` is the command that leaves it stopped. A merge by default, and a
 * cherry-pick for the one thing a merge cannot show: git takes all three
 * instructions for a replay and only `--abort` for a merge, so the banner over
 * a merge has no Continue to refuse.
 */
async function openConflictedRepository(
  page: Page,
  name: string,
  stop: [string, ...string[]] = ['merge', 'feature'],
): Promise<string> {
  await openWorkbench(page);

  // A scratch name, so the sweep can recognise it. These are built from
  // nothing by the test that wants them, so last run's copy is of no use to
  // this one — and a name the sweep cannot classify is one it leaves alone.
  const path = scratchFixture(name);
  rmSync(path, { recursive: true, force: true });
  mkdirSync(path, { recursive: true });

  // An isolated configuration, for the same reason as every other fixture
  // here: whoever runs this may sign every commit by default, and the test
  // would then pass or fail depending on whose machine it is.
  const git = (...args: string[]) =>
    execFileSync('git', args, {
      cwd: path,
      env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null' },
    });

  const write = (lines: string[]) => writeFileSync(join(path, 'notes.md'), lines.join('\n') + '\n');

  git('init', '-b', 'main');
  git('config', 'user.name', 'Ada Lovelace');
  git('config', 'user.email', 'ada@example.com');

  write(['one', 'two', 'three']);
  git('add', '-A');
  git('commit', '-m', 'first');

  git('checkout', '-b', 'feature');
  write(['one', 'SIDE', 'three']);
  git('commit', '-am', 'the feature branch');

  git('checkout', 'main');
  write(['one', 'MAIN', 'three']);
  git('commit', '-am', 'main');

  // Expected to fail. execFileSync throws on a non-zero exit, and a non-zero
  // exit is the state this fixture exists to be in.
  const succeeded = `the ${stop[0]} succeeded, so there is no conflict to resolve`;
  try {
    git(...stop);
    throw new Error(succeeded);
  } catch (cause) {
    if (cause instanceof Error && cause.message === succeeded) {
      throw cause;
    }
  }

  const response = await page.request.post('/api/repos', {
    headers: { 'X-Yagit-Token': sessionToken() },
    data: { path },
  });
  expect(response.ok(), await response.text()).toBeTruthy();
  opened.push(path);

  await page.reload();
  await page.getByRole('tab', { name: new RegExp(name) }).click();

  return path;
}

test('says what the repository is in the middle of, from either view', async ({ page }) => {
  await openConflictedRepository(page, 'cf-banner');

  // `git status --porcelain` never reports this. The banner is only possible
  // because the daemon reads the same marker files git itself reads.
  const banner = page.getByRole('status').filter({ hasText: 'Merging' });
  await expect(banner).toBeVisible();
  await expect(banner).toContainText('1 file still conflicted');

  // Above the switch and outside both faces: somebody reading the history
  // during a stopped merge needs to know it is stopped just as much as
  // somebody staring at the file list.
  await page.getByRole('radio', { name: /Changes/ }).click();
  await expect(banner).toBeVisible();
});

test('files the conflict apart from the ordinary changes', async ({ page }) => {
  await openConflictedRepository(page, 'cf-list');
  await page.getByRole('radio', { name: /Changes/ }).click();

  await expect(page.getByRole('heading', { name: /1 conflicted/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /^notes\.md, both modified/ })).toBeVisible();
});

test('shows both sides of each conflict, with git own labels', async ({ page }) => {
  await openConflictedRepository(page, 'cf-sides');
  await page.getByRole('radio', { name: /Changes/ }).click();
  await page.getByRole('button', { name: /^notes\.md, / }).click();

  await expect(page.getByText('Conflict 1 of 1')).toBeVisible();
  // git writes HEAD and the branch name after its markers. Shown verbatim:
  // rewording them is how an interface tells somebody their branch is called
  // something it is not.
  await expect(page.getByText('HEAD', { exact: true })).toBeVisible();
  await expect(page.getByText('feature', { exact: true })).toBeVisible();
  await expect(page.getByText('MAIN', { exact: true })).toBeVisible();
  await expect(page.getByText('SIDE', { exact: true })).toBeVisible();
  // Beside each side, and again under the whole-file buttons — two places,
  // because taking the whole file never looks at the region, and taking a
  // region never looks at the toolbar.
  await expect(page.getByText('ours — the branch you are on')).toHaveCount(2);
  await expect(page.getByText('theirs — the branch coming in')).toHaveCount(2);
});

// git swaps ours and theirs during a rebase. A note that kept merge language
// would tell somebody "Keep ours" is their work, then throw that work away.
test('names ours as the branch being replayed onto during a rebase', async ({ page }) => {
  await openConflictedRepository(page, 'cf-rebase-sides', ['rebase', 'feature']);
  await page.getByRole('radio', { name: /Changes/ }).click();
  await page.getByRole('button', { name: /^notes\.md, / }).click();

  await expect(page.getByText('ours — the branch being replayed onto')).toHaveCount(2);
  await expect(page.getByText('theirs — the commit being replayed')).toHaveCount(2);
  await expect(page.getByText('ours — the branch you are on')).toHaveCount(0);
});

test('resolves a conflict by choosing a side, saving, and staging', async ({ page }) => {
  const path = await openConflictedRepository(page, 'cf-resolve');
  await page.getByRole('radio', { name: /Changes/ }).click();
  await page.getByRole('button', { name: /^notes\.md, / }).click();

  // Nothing may be staged while a marker is still in the buffer: `git add` on
  // a file full of `<<<<<<<` succeeds, and git commits it without a word.
  const markResolved = page.getByRole('button', { name: 'Mark resolved', exact: true }).last();
  await expect(markResolved).toBeDisabled();

  await page.getByRole('button', { name: 'Keep theirs' }).click();

  // The choice never leaves the browser: it rewrites the buffer, and the file
  // on disk still has its markers until the save.
  expect(readFileSync(join(path, 'notes.md'), 'utf8')).toContain('<<<<<<<');
  await expect(page.getByText('Every conflict is resolved')).toBeVisible();

  await page.getByRole('button', { name: 'Save' }).click();
  await expect.poll(() => readFileSync(join(path, 'notes.md'), 'utf8')).toBe('one\nSIDE\nthree\n');

  await expect(markResolved).toBeEnabled();
  await markResolved.click();

  // `git add` is what collapses the three index stages into one. The banner
  // is the proof it happened: the merge is still in progress and nothing is
  // left conflicted.
  await expect(page.getByRole('status').filter({ hasText: 'Merging' })).toContainText(
    'Nothing is left conflicted',
  );
});

test('takes one side of the whole file through git', async ({ page }) => {
  const path = await openConflictedRepository(page, 'cf-whole');
  await page.getByRole('radio', { name: /Changes/ }).click();
  await page.getByRole('button', { name: /^notes\.md, / }).click();

  // `git checkout --ours`, then `git add` — the daemon runs both, because the
  // first writes the file and only the second ends the conflict.
  await page.getByRole('button', { name: 'Ours', exact: true }).click();

  await expect.poll(() => readFileSync(join(path, 'notes.md'), 'utf8')).toBe('one\nMAIN\nthree\n');
  await expect(page.getByRole('status').filter({ hasText: 'Merging' })).toContainText(
    'Nothing is left conflicted',
  );
});

test('offers git own message for the merge, in the box', async ({ page }) => {
  await openConflictedRepository(page, 'cf-message');
  await page.getByRole('radio', { name: /Changes/ }).click();

  // git already wrote this into MERGE_MSG. Asking somebody to type it again
  // from memory is the interface losing their work — so it goes in as real,
  // editable text, and the box says whose words they are.
  await expect(page.getByLabel('Commit message')).toHaveValue("Merge branch 'feature'");
  await expect(
    page.getByText('git wrote this message for the operation in progress'),
  ).toBeVisible();

  // The comment block git writes under the subject is stripped by `git
  // stripspace`. yagit commits with --cleanup=whitespace, which keeps '#'
  // lines, so one left in would be committed verbatim.
  await expect(page.getByLabel('Commit message')).not.toHaveValue(/Conflicts:/);
});

test('suggests a summary from what is staged, and one key accepts it', async ({ page }) => {
  const path = await openConflictedRepository(page, 'cf-suggest');
  await page.getByRole('radio', { name: /Changes/ }).click();

  // Out of the merge first: a prepared message wins over a suggestion, which
  // is the whole point of the distinction between them.
  execFileSync('git', ['merge', '--abort'], { cwd: path });
  writeFileSync(join(path, 'notes.md'), 'one\nEDITED\nthree\n');
  execFileSync('git', ['add', '-A'], { cwd: path });

  const box = page.getByLabel('Commit message');
  await expect(box).toHaveAttribute('placeholder', 'Update notes.md');
  await expect(box).toHaveValue('');

  // Behind the glass until somebody takes it. A guess nobody read is not a
  // commit message.
  await box.click();
  await page.keyboard.press('Tab');
  await expect(box).toHaveValue('Update notes.md');
});

test('calls the merge off from the banner, having named what that destroys', async ({ page }) => {
  const path = await openConflictedRepository(page, 'cf-abort');

  const banner = page.getByRole('status').filter({ hasText: 'Merging' });
  await banner.getByRole('button', { name: 'Abort' }).click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();

  // The line the daemon assembled, from the state it read for itself. A dialog
  // that built its own text would be a second definition of the command, and
  // the stale half of it would promise one thing while git received another.
  await expect(dialog).toContainText('git merge --abort');
  await expect(dialog).toContainText('every conflict resolved since the merge began');

  await dialog.getByRole('button', { name: 'Abort' }).click();

  // The banner goes in the same frame, because the route answers with the
  // status it left behind rather than leaving the panel to poll for it.
  await expect(banner).toHaveCount(0);
  await expect.poll(() => readFileSync(join(path, 'notes.md'), 'utf8')).toBe('one\nMAIN\nthree\n');
});

test('says why Continue is refused, where a refused button can be read', async ({ page }) => {
  await openConflictedRepository(page, 'cf-refused', ['cherry-pick', 'feature']);

  const banner = page.getByRole('status').filter({ hasText: 'Cherry-picking' });
  const carryOn = banner.getByRole('button', { name: 'Continue' });
  await expect(carryOn).toBeDisabled();

  // A `title` on the button itself would never be read by anybody: a disabled
  // Button drops pointer events, so the browser fires no hover on it and shows
  // no native tooltip. The sentence hangs off the span around it instead, and
  // reaches the button through aria-describedby.
  expect(await carryOn.getAttribute('aria-describedby')).not.toBeNull();

  await carryOn.hover({ force: true });
  await expect(page.getByRole('tooltip')).toContainText('1 file still conflicted');
});

test('the conflict view has no accessibility violations', async ({ page }) => {
  await openConflictedRepository(page, 'cf-axe');
  await page.getByRole('radio', { name: /Changes/ }).click();
  await page.getByRole('button', { name: /^notes\.md, / }).click();
  await expect(page.getByText('Conflict 1 of 1')).toBeVisible();

  expect(await violations(page)).toEqual([]);
});
